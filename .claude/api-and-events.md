# API Contract

**All routes under `/api/v1`.** No `/api/v2` yet.

**Three route classes, all under the same `/api/v1` prefix** (CAT-D8 — internal routes are
mesh-only by *auth*, not by URL namespace; the original v1.0/v1.1 LLD spec of a prefix-free
`/internal/*` namespace was never built and is retroactively superseded):

- **Public routes** (`GET /api/v1/departments[/:id]`) — any authenticated caller, no
  `platform_operator` requirement (CAT-6/CAT-7).
- **Operator routes** (`/api/v1/operator/...`) — `platform_operator` role required (CAT-1
  through CAT-5).
- **Internal routes** (`/api/v1/internal/...`) — `iam-system` role required, mesh-only
  (CAT-I1/CAT-I2). `iam-org-membership`'s already-built `CatalogAdminClient` calls these paths
  verbatim — changing the route now would be a breaking cross-repo contract change.

**Identity model:**
- Trusts `x-user-id`, `x-tenant-id`, `x-tenant-roles` headers (injected by the gateway after JWT
  validation, or by the mesh for internal calls). No JWT parsing in this service.
- `x-tenant-id` is required and 401-enforced on **every** route (`IdentityBridgeMiddleware`) even
  though this service has no tenant concept and never reads the parsed value again after parsing
  (CAT-D12) — kept for platform-wide header-contract uniformity; the gateway injects it
  regardless of which backend is being called. It is also logged, unredacted, as `tenant_id` on
  every request line (LLD §13.3, corrected v1.23 — previously this doc claimed the opposite).
- `IdentityBridgeMiddleware` does the identity-**parsing** half only — there is no RLS/tenant-GUC
  half to bridge, unlike `iam-org-membership`'s `GUCBridgeMiddleware` (neither `departments` nor
  `plans` carries a `tenant_id` column at all).
- Every write endpoint re-checks its required role **inside the handler** in addition to the
  route-group middleware (`requireOperator`/`requireSystem` — defense-in-depth, mirrors O&M's
  AUTH-6 pattern).

**Content-Type enforcement:** `RequireJSONContentType()` runs on the `/api/v1` group. Any
POST/PUT/PATCH with a non-empty body must declare `Content-Type: application/json` or the request
is rejected with **415 `unsupported_media_type`** before the handler runs. GET/DELETE and
empty-body requests bypass.

**Body size cap:** 1 MB, enforced twice — a `Content-Length` pre-check (immediate
**413 `request_entity_too_large`**) and `http.MaxBytesReader` as a second line of defence for
chunked requests without a declared length.

**405 handling:** Gin's `HandleMethodNotAllowed` is enabled and its default empty-body handler is
overridden to return the standard JSON error envelope (`405 method_not_allowed`) with the `Allow`
header preserved. This is the same wire shape CAT-3 (below) uses, just with a wider trigger
surface — any route hit with a disallowed verb, not only `DELETE /operator/departments/:id`.

**Key endpoints (9 total):**

| ID | Method | Path | Auth | Notes |
|---|---|---|---|---|
| CAT-1 | `POST` | `/operator/departments` | `platform_operator` | Create a department. `code`/`name` required; `is_system` optional (default `false`). |
| CAT-2 | `PATCH` | `/operator/departments/:id` | `platform_operator` | Rename and/or retire (`name`/`is_active`); `code`/`is_system` rejected in the body with `422 field_immutable`. Optimistic-locked on `record_version`. |
| CAT-3 | `DELETE` | `/operator/departments/:id` | `platform_operator` | **Always `405`** — hard delete is blocked forever; retire via CAT-2 `is_active=false` instead (D-4/OP-3). |
| CAT-4 | `GET` | `/operator/plans` / `/operator/plans/:code` | `platform_operator` | List or single-tier read. `:code` must be `starter`/`pro`/`enterprise`, else `404 plan_not_found`. |
| CAT-5 | `PATCH` | `/operator/plans/:code` | `platform_operator` | PATCH-only (PLAN-4) — no create/delete, the tier set is fixed to the `tenant_plan` ENUM. Optimistic-locked on `record_version`. |
| CAT-6 | `GET` | `/departments` | any authenticated caller | Public catalog listing. `?active_only=true` filters in-memory after the cache read. |
| CAT-7 | `GET` | `/departments/:id` | any authenticated caller | Public single-department read. Not cache-fronted. |
| CAT-I1 | `GET` | `/internal/departments` | mesh-only (`iam-system`) | Bulk read for Core's `om:departments` / Group Mapping's `gm:departments` cache population. |
| CAT-I2 | `GET` | `/internal/plans` | mesh-only (`iam-system`) | Bulk read for Core's `om:plans` cache population. |

Both internal endpoints are **read-only, side-effect-free, and idempotent** by construction — no
ordering, retry, or idempotency-key concern on either.

**CAT-I1 response shape:**
```jsonc
{
  "departments": [
    { "id": "uuid", "code": "ENGINEERING", "name": "Engineering", "is_system": true, "is_active": true, "record_version": 3 }
  ],
  "as_of": "2026-08-13T10:00:00Z"
}
```

**CAT-I2 response shape** — reuses `PlanResponse` per item *and* carries a top-level
`record_versions` map (so a consumer can check "is my cached copy of tier X stale" in one lookup
without scanning `plans[]` first — redundant with the per-item field but harmless):
```jsonc
{
  "plans": [
    { "code": "starter", "display_name": "Starter", "workflow_template_limit": 5, "tender_limit": 10,
      "trial_duration_days": 30, "sso_enabled": false, "custom_branding": "none", "feature_set": {}, "record_version": 3 }
  ],
  "record_versions": { "starter": 3, "pro": 2, "enterprise": 5 }
}
```

**Error envelope shape (LLD §20):** every 4xx/5xx response uses a flat JSON shape shared with
every other IAM service:

```jsonc
{
  "error":      "system_department_cannot_be_retired",
  "code":       "system_department_cannot_be_retired",
  "status":     422,
  "message":    "system department cannot be retired",
  "trace_id":   "4bf92f3577b34da6a3ce929d0e0e4736",
  "request_id": "req_01hz..."
}
```

There is deliberately **no nested `details` field** — an earlier revision had `Details
[]ValidationError` that Swagger advertised but no code path ever populated (removed). Some codes
carry **extra top-level fields**, merged flat by `errorResponseWithDetails`
(`internal/adapter/inbound/http/middleware.go`) from a handler/service's
`domain.DomainError.WithDetails(map[string]any{...})` call — e.g. `optimistic_lock_conflict` adds
`record_version`, `field_immutable`/`system_name_immutable` add `field`,
`invalid_feature_value` adds `key`. See [development-guide.md](development-guide.md#appendix--error-codes)
for the full table.

`error` and `code` always carry the same value, **including when a handler overrides `code` with a
more specific sub-code** (`invalid_uuid` over `validation_error`, `duplicate_code` over
`conflict`) — fixed at the shared `errorResponseWithDetails` builder (CAT-D9), not per call site,
so the guarantee holds for every current and future sub-code.

**Concurrency:** every mutable response includes `record_version`. `PATCH` uses optimistic locking
— a version mismatch (row exists but a different `record_version`) returns `409
optimistic_lock_conflict` with the row's actual current value in the `record_version` field, so a
client can retry without a second round-trip. `record_version` is a monotonic counter bumped by
the `touch_row()` DB trigger, not a timestamp.

# Caching

**Cache:** Valkey (Redis-compatible) via `go-redis/v9`. **Advisory-only** (LLD §8, CAT-FAIL-1):
every miss, timeout, or outage falls through to Postgres, which is the sole source of truth. A
`nil` `port.Cache` is a valid configuration.

| Key | Value | TTL | Invalidated by |
|-----|-------|-----|-----------------|
| `cat:departments` | Full department catalog (JSON array, unfiltered — `active_only` applied in-memory after the read) | 60 s (`CATALOG_TTL_SECONDS`, default 60) | Any successful CAT-1 or CAT-2 write |
| `cat:plans` | Full plan catalog (JSON array) | 60 s | Any successful CAT-5 write |

Both keys cache the **whole catalog** as a single JSON blob shared by the public list, the
operator list, and the internal bulk read — there is no per-`active_only` or per-row cache
variant, keeping invalidation to exactly one key per table. Single-row reads (CAT-7, CAT-4's
`:code` read) are **not** cache-fronted — they go straight to Postgres (LLD §8 only names
whole-catalog keys).

**Read-through algorithm** (`department_service.go`'s `listAllCached`, `plan_service.go`'s
`List`):
```
GET cat:<table>
  hit  → unmarshal and return
  miss → SELECT * FROM <table> ORDER BY code
         → marshal + SET cat:<table> with jittered-free 60s TTL
         → return
```

**Invalidation:** `DELETE cat:<table>` runs **post-commit**, after a successful CAT-1/CAT-2/CAT-5
write. A `DELETE` failure is logged (`internal/adapter/outbound/valkey/cache.go`) but never fails
the write — the stale entry self-heals within its own 60 s TTL (CAT-FAIL-1).

**`/readyz` fails on a Valkey-only outage** even though the cache is "advisory" per-request
(CAT-D11) — `router.go`'s `readyz` checks `cache.Health(ctx)` independently of Postgres and
returns `503` if it fails, pulling the pod out of rotation even while Postgres is fully healthy.
This is a deliberate trade-off: without it, every request would silently fall through to Postgres
for the outage's duration, amplifying DB load with no external signal until Postgres itself
buckles.

**Consumer-side caches** (owned by Core Org & Membership and the Group Mapping Service — **not**
implemented in this repository; documented here only for the full propagation picture):

| Key | Owner | TTL | Populated from |
|---|---|---|---|
| `om:departments` | Core | 600 s | CAT-I1 |
| `om:plans` | Core | 600 s | CAT-I2 |
| `gm:departments` | Group Mapping Service | 600 s | CAT-I1 |
| `om:departments:stale` / `om:plans:stale` / `gm:departments:stale` | same as above | 24 h | Refreshed opportunistically on every successful CAT-I1/CAT-I2 call; served **only** when the primary key has expired **and** the live call to this service fails |

**Cross-service propagation is TTL-only, not push-based (CAT-D3/CAT-D7)** — a write here does
**not** actively invalidate any consumer's cache. Expected propagation delay is ~5 min
(`600s / 2`, the average case); worst case is the full 600 s (10 min), or up to 24 h under the
stale-if-error fallback. This bound applies to `departments` too, even though it now governs a
correctness-adjacent access gate (D-5/TD-1 — no new tenant-department activation against a
retired department) rather than only an entitlement value — accepted because department
retirement is a rare, planned operator action (CAT-D7). See `CACHE_DESIGN.md` for the full
rationale.

# Events

**This service publishes and consumes no events whatsoever** (LLD §10, invariants CAT-EVT-1
through CAT-EVT-5) — a structural simplification versus every other service in the IAM stack
(all of which carry the transactional-outbox pattern). Confirmed deliberate, not an oversight:
neither table has a `tenant_id` to key a lifecycle cascade off of, and neither a `plans` edit nor
a department-catalog edit has ever appeared in the platform HLD's event catalogue.

- **No inbound SQS consumers.** No `TenantOffboarded`-style subscription — this service has
  nothing tenant-scoped to scrub. Consequently **no `processed_events` table** either (nothing to
  dedup).
- **No outbound SNS topics.** No outbox table, no outbox-runner worker, no `platform-events`
  publisher wiring anywhere in this codebase.
- **No `api/asyncapi.yaml`** and **not registered** with `platform-schemagov` — there is no event
  type for `schema-gov extract`/`validate`/`register` to act on. No CI job is wired to
  `schema-registry.yml`.
- **No AWS Glue Schema Registry entry**, no `github.com/aws/aws-sdk-go-v2/...` dependency in
  `go.mod` at all — no S3, no SNS, no SQS, no Glue.
- **Write visibility to consumers is governed entirely by the cache-TTL mechanism above**, not by
  event propagation — there is no sub-TTL delivery guarantee and none is promised (CAT-EVT-4).

If a genuine event need ever arises (e.g. a future `DepartmentCatalogChanged` notification in
place of today's TTL-only propagation, tracked as a deferred option under CAT-D3), it must re-enter
the normal `schema-gov` pipeline and receive an HLD §9.4 catalogue entry like any other IAM event —
no ad hoc event type may bypass that governance (CAT-EVT-5).

**Audit:** every write (CAT-1/CAT-2/CAT-5) is audit-relevant by the same convention the platform
applies to other direct (non-bus-event) admin actions, but **no durable, queryable audit record
exists beyond structured request logging** (CAT-D10) — the platform's Audit Log Service (HLD §5.7)
has a "direct audit write" category for exactly this kind of entry, but no LLD on the platform
specifies that mechanism's actual contract (no ingest endpoint, no client port, no schema) and no
`iam-audit-log` repository exists to call yet. This is a platform-wide gap, not something specific
to this service — do not build a local one-off audit table against a contract that doesn't exist
yet; wire this service to the real contract once it's specified.
