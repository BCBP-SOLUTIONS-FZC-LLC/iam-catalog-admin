# Key Design Decisions

The LLD's decision register (§16, CAT-D1 through CAT-D13) is the authoritative record; the
highlights most relevant to day-to-day work:

1. **`departments` + `plans` extracted as one combined service (CAT-D1)** — both share the
   identical "global, no-RLS, operator-write-only, mesh-readable" shape and have no relationship
   to each other's write path; splitting further would multiply deployment overhead with no
   isolation benefit.

2. **This service owns the plan *baseline* only, never the per-tenant override (CAT-D2).**
   `tenants.feature_flags` (the override delta) stays in Core; the effective-value merge happens
   exclusively in Core at I-8 read time. Do not add a merge here even if it looks convenient.

3. **Nullable `workflow_template_limit`/`tender_limit`, `NULL = unlimited` (CAT-D6)** — not a `-1`
   sentinel. `domain.PlanPatch` uses a **double pointer** (`**int`) for both fields specifically to
   distinguish "field absent from the PATCH body" from "field explicitly set to `null`" from
   "field set to a value" — see `plan_handler.go`'s `parseLimit`.

4. **CAT-I1/CAT-I2 keep the shared `/api/v1` prefix (CAT-D8)** — the original LLD spec called for
   a prefix-free `/internal/*` namespace, but that was never built, and `iam-org-membership`'s
   already-deployed `CatalogAdminClient` calls `/api/v1/internal/departments`/`.../plans`
   verbatim. The LLD was retroactively corrected to match the shipped, integrated contract rather
   than the other way around. "Mesh-only" is an authorization/network property (`iam-system` role
   + NetworkPolicy), never a URL-namespace property — don't try to "fix" this path.

5. **TTL-only cross-service cache propagation, no push/webhook invalidation (CAT-D3/CAT-D7)** —
   continues the existing `om:plans` precedent rather than introducing new coupling (this service
   would otherwise need to know its consumers' identities and invalidation endpoints). Deferred,
   not rejected — revisit only if the ~10-minute worst-case propagation window proves operationally
   painful in practice.

6. **`error` always matches `code` in the error envelope, even after a sub-code override
   (CAT-D9).** Fixed at the shared `errorResponseWithDetails` builder, not per call site — if you
   add a new sub-code via `WithDetails({"code": "..."})`, you get this guarantee for free; don't
   re-implement it locally.

7. **`/readyz` fails on a Valkey-only outage despite the cache being "advisory" (CAT-D11).**
   "Advisory" is true per-request (a miss/DEL failure falls through to Postgres with no wrong
   answer); it is *not* true for a sustained outage at the service level — without failing
   readiness, every request would silently fall through to Postgres for the outage's duration,
   amplifying DB load with no external signal. This is intentional; do not "fix" `readyz` to
   ignore cache health without re-reading CAT-D11's trade-off first.

8. **`x-tenant-id` stays required on every route even though nothing reads it afterward
   (CAT-D12).** Every other IAM service requires the same gateway-injected header set, and the
   gateway injects `x-tenant-id` uniformly regardless of backend — carving out a per-service
   exception was judged more coupling risk than the cost of one unused, harmlessly-logged header.

9. **No local audit-log table (CAT-D10).** The platform's Audit Log Service (HLD §5.7) has a
   "direct audit write" category that CAT-1/2/5 writes would belong to, but no LLD anywhere
   specifies that mechanism's actual contract yet. A local stand-in table was briefly built and
   then removed — building one against no known contract risks being the wrong shape once the
   real one ships. Don't resurrect a local audit table; wait for the real contract.

10. **Single DB role, no migrator/app split (LLD §9).** Unlike every RLS-scoped sibling service,
    `catalog_admin_app` runs both migrations and app traffic — there's no cross-tenant data to
    read around, so the usual migrator/app isolation buys nothing here.

11. **CHECK-violation matching via `pgcommon.IsCheckViolation`/`ConstraintName`, never a message
    substring search.** `prevent_system_department_name_change`'s trigger (which can't be a real
    CHECK constraint, since it compares `OLD` vs `NEW`) raises with a structured SQLSTATE +
    synthetic constraint name so the Go side can match it the same way it matches a real CHECK
    constraint.

# Extending the Service

- **New department/plan field:** Add column via a new migration (`internal/adapter/outbound/postgres/migrations/`)
  → domain entity (`internal/core/domain/{department,plan}.go`) → repository scan/insert/update
  SQL → DTO (`internal/adapter/inbound/http/dto.go`) → handler mapping function
  (`departmentToResponse`/`planToResponse`) → swaggo annotations → `make swag` → tests at every
  layer. Update the "Data ownership" table in the LLD (§5.4) if the new field changes an ownership
  boundary.
- **New endpoint:** Add the route in `router.go`'s `registerAPIRoutes` (pick the right group —
  public `v1`, `op` for `platform_operator`, `internal` for `iam-system`) → handler method → service
  method → repository method. Give it a `CAT-` ID and add it to the LLD's endpoint catalogue (§6)
  and this repo's `README.md` API table — this service's numbering convention (CAT-1..CAT-I2) is
  deliberately its own, not the legacy `O-` numbering from the source LLD (CAT-D5).
- **New domain error / error code:** Add a sentinel to `internal/core/domain/errors.go`, map it to
  an HTTP status in `domainErrorStatus` (`internal/adapter/inbound/http/middleware.go`), and add a
  row to the LLD's Appendix A error taxonomy (§20) and this doc's error-code table below — that
  table's own history shows it has drifted from the real code before (missing `duplicate_code`,
  then six more codes) and been caught only by a deliberate reconciliation pass; don't let it drift
  again.
- **New CHECK constraint that can't actually be a CHECK** (compares `OLD` vs `NEW`, like
  `chk_system_department_name_immutable`): write it as a `BEFORE UPDATE OF <col>` trigger that
  `RAISE EXCEPTION USING ERRCODE = 'check_violation', CONSTRAINT = '<synthetic_name>', ...` — never
  a bare `RAISE EXCEPTION 'message'` — so `pgcommon.IsCheckViolation`/`ConstraintName` can match it
  structurally on the Go side (see `prevent_system_department_name_change`'s own comment in
  `000001_init_schema.up.sql` for the full reasoning).
- **New cache key:** Add the constant to `internal/adapter/outbound/valkey/cache.go` (mirror
  `DepartmentsKey`/`PlansKey`), read-through in the relevant service method, invalidate
  post-commit in the write path, and add a row to `CACHE_DESIGN.md`'s table and
  [api-and-events.md](api-and-events.md#caching).
- **New business metric:** Add to `internal/adapter/outbound/metrics/metrics.go`'s `build()`
  function (both the `var` declaration and the `prometheus.New*Vec` construction), pre-initialize
  known label values in `registerOnto` so dashboards show `0` rather than "no data" before the
  first event, and document it in [operations.md](operations.md#metrics).
- **If this service ever needs to publish or consume an event:** re-enter the normal
  `platform-schemagov` pipeline from scratch (CAT-EVT-5) — add `api/asyncapi.yaml`, register with
  `schema-gov`, add the HLD §9.4 catalogue entry. Do not wire up an ad hoc SNS/SQS call without
  that governance; this service currently has zero AWS SDK dependencies and that should stay true
  unless a real event requirement lands.

# Development Workflow

1. **New feature:** Write a failing test in `test/unit/` (or a white-box test alongside the
   package it exercises, e.g. `internal/core/service/*_extended_test.go`) → implement in the
   service → wire in the handler → update swaggo annotations → `make swag` → `make test-postgres`
   if the change touches a migration or a DB-trigger interaction → `make test-e2e` for a
   full-stack check.
2. **DB schema/trigger change:** Write the migration SQL (`.up.sql` + `.down.sql`) → run
   `make test-postgres` → verify constraint/trigger tests in `test/postgres/constraints_test.go`.
3. **Cache invalidation change:** Update both the cache-writer (service method, post-write) and
   any TTL constant → check `CACHE_DESIGN.md` and cache tests in
   `internal/adapter/outbound/valkey/cache_test.go`.
4. **Endpoint change:** Update swaggo annotations in the handler first → `make swag` → `make
   swag-check` to confirm no drift → implement + tests → keep the response shape backwards
   compatible or mint a new `/api/v2` path (none exists yet).
5. **Any contract-level change (new field, new error code, new endpoint):** Update the LLD
   (`docs/lld/iam-lld-catalog-admin-config-service.md`) in the same change — this repo's history
   (CAT-D8, CAT-D9, the CHANGELOG's "LLD-vs-code audit" entries) shows the LLD and code have
   drifted from each other more than once; each time, a dedicated audit pass found and fixed it.
   Treat the LLD as living documentation that must track the code, not a one-time artifact.
6. **Before opening a PR:** `make ci` (tidy + fmt-check + vet + lint + test-ci + build) locally
   mirrors what `validate-quality.yml`/`validate-test.yml` run in CI, including the ≥98% coverage
   gate and the `go-arch-lint` architecture check.

# Troubleshooting

**Optimistic lock conflict (409) on CAT-2/CAT-5:** Concurrent edit detected — the client must
re-read the current `record_version` (returned in the `409` body) and retry.

**System department can't be retired / renamed (422):** Working as designed (D-7/D-9/D-11) — an
`is_system=true` department can never have `is_active` set to `false` or its `name` changed
through the API. There is no bypass; changing this requires an operator migration.

**`405` on `DELETE /operator/departments/:id`:** Working as designed (CAT-3, D-4/OP-3) — no
department is ever hard-deletable, `is_system` or not. Retire via `PATCH .../:id`
`{"is_active": false}` instead.

**`CATALOG_TTL_SECONDS` seems ignored / cache TTL looks like 60s regardless of config:** Check the
startup log for a `"invalid CATALOG_TTL_SECONDS, falling back to 60s default"` warning — a
malformed value (non-integer, or `WithCacheTTL`'s own `d <= 0` guard) silently falls back rather
than crashing, since cache TTL misconfiguration is low-stakes. Fix the env var and restart.

**Cross-tenant / stale department or plan data visible in Core or Group Mapping Service:**
Not a bug in this service by itself — check whether it's within the documented TTL propagation
window first (up to 600 s normally, up to 24 h under the consumer's stale-if-error fallback,
CAT-D3/CAT-D7). If it's been longer than that, the problem is on the **consumer** side (`om:*`/
`gm:*` cache population or TTL config in Core/Group Mapping), not here — this service has no
push/webhook invalidation to debug.

**Migration fails with an advisory-lock error:** Confirm `MIGRATION_DATABASE_URL` (or the fallback
DSN) bypasses PgBouncer — `pg_advisory_lock` is session-scoped and breaks under transaction
pooling. If `PG_BOUNCER_MODE=true` and `MIGRATION_DATABASE_URL` is unset, `main.go` should have
already refused to start (`validateRequiredEnv`); if it didn't, that's the actual bug to chase.

**`go-arch-lint` fails in CI:** Check which component's `mayDependOn` list in `.go-arch-lint.yml`
you've violated — most commonly, something under `internal/core/` importing an `adapter/` package
directly, or `internal/core/domain` picking up a dependency on anything at all. `deepScan: false`
means only import-level violations are caught, not DI wiring through `main.go`.

**Coverage gate (≥98%) fails in CI:** Run `make cover-func` locally to see the per-function
breakdown; `.coverage/{unit,postgres,e2e}.out` are merged via `scripts/merge_coverage.py`
(max-count strategy) into `coverage.out`, so a branch only exercised by `test/postgres` (e.g. a
trigger-raised CHECK violation) won't show as covered from `test/unit` alone — run the full
`make test-ci` before concluding a branch is truly uncovered.

**A new `test/postgres`/`test/e2e` test runs slower than its siblings, or `make test-ci` regresses
back toward its pre-parallelization time:** Check the new test calls `t.Parallel()` as its first
statement (after `t.Helper()` if it has one) — every existing test in both packages does, and each
already builds its own isolated Postgres container/pool (e2e also its own `miniredis` +
`httptest.Server`), so there's no shared state that would make parallel execution unsafe. A test
missing `t.Parallel()` still passes, it just runs serially against the others — a silent wall-time
regression, not a test failure, so it won't surface as a CI red X.

---

# Appendix — Error Codes

Every error response uses the flat `ErrorResponse` envelope (see
[api-and-events.md](api-and-events.md) for the full shape). `code == error` always, including
after a sub-code override (CAT-D9). Status mapping lives in `domainErrorStatus`
(`internal/adapter/inbound/http/middleware.go`) — any code not covered there falls through to 500.

| `code` | HTTP | Extra field(s) | Meaning |
|---|---|---|---|
| `validation_error` | 400 | — | A request body fails a field-level check — malformed JSON, missing `code`/`name` (CAT-1), a negative limit/trial-days field, or an unrecognized `custom_branding` (CAT-5). The generic 400 — `no_mutable_field`/`invalid_feature_value`/`invalid_uuid` are its more specific siblings. |
| `unsupported_media_type` | 415 | — | A POST/PUT/PATCH `Content-Type` isn't `application/json` (`RequireJSONContentType`). |
| `request_entity_too_large` | 413 | — | Request body exceeds the 1 MB cap (`Content-Length` pre-check or `http.MaxBytesReader`). |
| `invalid_uuid` | 400 | — | A path parameter (e.g. CAT-2/CAT-7's `:id`) is missing or not a valid UUID (`parseUUIDParam`). A sub-code over `validation_error`. |
| `no_mutable_field` | 400 | — | CAT-2 body has neither `name` nor `is_active`; or CAT-5 body has no mutable field set at all. |
| `invalid_feature_value` | 400 | `key` | CAT-5's `feature_set` contains a non-scalar value (nested object/array barred). |
| `missing_identity_headers` | 401 | — | `x-user-id`/`x-tenant-id` absent or not a valid UUID (`IdentityBridgeMiddleware`), or a defense-in-depth re-check finds no identity context at all. |
| `insufficient_role` | 403 | — | Caller lacks the role a route requires — `platform_operator` for CAT-1–5, `iam-system` for CAT-I1/I2. |
| `department_not_found` | 404 | — | No `departments` row for the given ID (CAT-2, CAT-7). |
| `plan_not_found` | 404 | — | No `plans` row for the given tier code (CAT-4, CAT-5), including an unrecognized code. |
| `field_immutable` | 422 | `field` (`"code"` or `"is_system"`) | CAT-2 body includes `code` or `is_system` (D-2/D-10, D-7/D-11). |
| `system_department_cannot_be_retired` | 422 | — | CAT-2 attempted to set `is_active=false` on an `is_system=true` department (D-7, D-9/D-11). |
| `system_name_immutable` | 422 | `field` (always `"name"`) | CAT-2 attempted a `name` change on an `is_system=true` department (D-11). Distinct from `field_immutable` — this is about *which department*, not which field. |
| `optimistic_lock_conflict` | 409 | `record_version` (int) | CAT-2/CAT-5's `record_version` didn't match the current row. |
| `duplicate_code` | 409 | — | CAT-1's `code` already exists (unique violation on `uq_departments_code`, D-10). Sub-code over `conflict`. |
| `db_unavailable` | 503 | — | A raw Postgres error whose SQLSTATE class (`08`/`53`/`57`/`58`) indicates connectivity/resource exhaustion, not a logic error. |
| `dependency_unavailable` | 503 | — | A DB error that isn't a recognized SQL-protocol error, `DomainError`, `pgx.ErrNoRows`, or a context cancellation — catch-all for "something else broke the DB connection." |
| `catalog_unavailable` | 503 | — | Emitted by a **consumer** (Core, Group Mapping), never by this service — the downstream consequence of this service being unavailable while a consumer's cache is also empty/stale-if-error-expired (§11.1). |
| `method_not_allowed` | 405 | — | `DELETE /operator/departments/:id` (CAT-3), or any route hit with a disallowed HTTP verb generically (the router's `NoMethod` handler). |

`cache_unavailable` is declared in `internal/core/domain/errors.go` and status-mapped, but **no
code path ever constructs it** — cache failures are advisory and fall through to Postgres
silently, never surfacing as a client-facing error. Don't be surprised it's unreachable; that's by
design (CAT-FAIL-1).

---

**LLD version:** v1.27 (`docs/lld/iam-lld-catalog-admin-config-service.md`) — check its revision
history table before assuming any section number or claim above is still current; this service's
own history shows the LLD gets corrected via dedicated audit passes fairly often.
