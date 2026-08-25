# Key Request Flows

This service has no cross-service synchronous calls (LLD §4 — pure leaf) and no multi-row
transactions (CAT-FAIL-3 — every write is single-row, single-table via `pgcommon.RunInTx`). Every
flow below is a single HTTP request, one DB round-trip (plus, for reads, one optional cache
round-trip).

## CAT-1 — Create department (`POST /operator/departments`)

1. `requireOperator` re-check (defense-in-depth on top of `RequireOperatorRole` middleware).
2. `DepartmentService.Create` validates `code`/`name` are non-blank.
3. `DepartmentRepository.Insert` — `INSERT ... RETURNING`, `id` generated client-side
   (`uuid.New()`) if not set. A unique-violation on `uq_departments_code` (via
   `pgcommon.IsUniqueViolation`) maps to `409 conflict` with sub-code `duplicate_code`.
4. `catalog_admin_writes_total{table="departments",op="insert"}` incremented — **note:** both
   `DepartmentRepository.Insert` and `DepartmentHandler.Create` increment this counter on the same
   successful call (the handler increments again after the service call returns), so this counter
   currently reflects 2 per actual insert. Same pattern on CAT-2 (`Update`)/CAT-5 (`Update`) — both
   the repository and the handler increment `catalog_admin_writes_total`. Worth knowing when
   reading this metric or its Grafana panel; treat it as a rate/trend signal, not an exact count.
5. `cat:departments` cache key deleted (post-commit, best-effort).

## CAT-2 — Patch department (`PATCH /operator/departments/:id`)

1. `requireOperator` re-check.
2. Raw JSON body is read **before** DTO binding to check for `code`/`is_system` keys — either
   present returns `422 field_immutable` immediately (a request-shape check the DTO struct alone
   couldn't perform, since it would just silently drop unknown-to-it fields).
3. `DepartmentService.Patch` requires at least one of `name`/`is_active` (else `400
   no_mutable_field`); rejects a blank `name`.
4. `DepartmentRepository.Update` issues `UPDATE ... WHERE id=$1 AND record_version=$2 RETURNING`.
   Three DB-level outcomes are possible on the same call:
   - **Success** — row returned, `touch_row()` trigger bumped `record_version`/`updated_at`.
   - **Version conflict or missing row** — `pgx.ErrNoRows`; a follow-up probe
     (`SELECT record_version WHERE id=$1`) disambiguates: no row → `404 department_not_found`; a
     row with a different version → `409 optimistic_lock_conflict` (`record_version` in the body).
   - **CHECK violation from a trigger** — `chk_system_department_active` (retiring a system
     department) or `chk_system_department_name_immutable` (renaming a system department's `name`)
     bubble up as a raw `*pgconn.PgError`. `DepartmentService.Patch` matches these **structurally**
     via `pgcommon.IsCheckViolation(err)` + `pgcommon.ConstraintName(err)` — never a message
     substring search — and maps them to `422 system_department_cannot_be_retired` /
     `422 system_name_immutable{field:"name"}` respectively.
5. On success: `catalog_admin_writes_total{table="departments",op="update"}` incremented,
   `cat:departments` invalidated.

## CAT-3 — Delete department (`DELETE /operator/departments/:id`)

Always returns `405 method_not_allowed` — `DepartmentService.DeleteBlocked()` is a pure function
with no DB call at all. Unlike some sibling services' *conditional* 422 block (only `is_system`
departments), this service blocks the verb **unconditionally** for every department, so `405` (the
method itself is disallowed) is the correct status, not a 422 domain-rule check. The DB-level
`prevent_department_delete()` trigger is the true last line of defense in case this handler is
ever bypassed.

## CAT-5 — Patch plan (`PATCH /operator/plans/:code`)

1. `requireOperator` re-check.
2. `:code` validated against the `tenant_plan` ENUM values before anything else (`404
   plan_not_found` for an unrecognized tier).
3. Handler-level `parseLimit` (see [api-and-events.md](api-and-events.md)) resolves
   `workflow_template_limit`/`tender_limit`'s three-state `json.RawMessage` into a `**int`.
4. `PlanService.Patch` re-validates every field the DB's own CHECK constraints would reject
   (non-negative limits/trial days, `custom_branding` enum membership) so violations surface as
   `400 validation_error` rather than a raw constraint error reaching the client. `feature_set`
   values are validated as scalars only (`400 invalid_feature_value{key}` for a nested
   object/array) — mirrors the same defense Core applies to its own `tenants.feature_flags`
   override delta.
5. `PlanRepository.Update` builds a dynamic `SET` clause from only the non-nil patch fields —
   `UPDATE plans SET ... WHERE code=$1 AND record_version=$2 RETURNING`. Same
   ErrNoRows-then-probe disambiguation as CAT-2 for `404 plan_not_found` vs.
   `409 optimistic_lock_conflict`.
6. On success: `catalog_admin_writes_total{table="plans",op="update"}` incremented, `cat:plans`
   invalidated.

## CAT-6/CAT-7 — Public department reads

- **CAT-6** (`GET /departments`): `DepartmentService.List` reads through `cat:departments`
  (60 s TTL); `?active_only=true` is applied **in-memory after** the cache read, so both the
  filtered and unfiltered views share one cache entry.
- **CAT-7** (`GET /departments/:id`): not cache-fronted — `DepartmentRepository.FindByID`
  straight to Postgres.

## CAT-I1/CAT-I2 — Internal bulk reads (mesh-only)

- **CAT-I1** (`GET /internal/departments`): `requireSystem` re-check, then
  `DepartmentService.List(ctx, activeOnly=false)` — the **same** cache entry and code path CAT-6
  uses, just always unfiltered. Response adds `as_of` (server `time.Now().UTC()` at response-build
  time, not a cache timestamp).
- **CAT-I2** (`GET /internal/plans`): `requireSystem` re-check, then `PlanService.List` — the
  same `cat:plans`-backed path CAT-4's list uses. Response additionally builds a
  `record_versions map[string]int64` alongside the `plans[]` array.

Both are read-only, side-effect-free, idempotent `GET`s over an immutable-until-next-write
snapshot — no ordering, retry, or idempotency-key concern.

# Concurrency & Consistency

## Optimistic concurrency

Both mutable resources (`departments`, `plans`) use `record_version` — a monotonic **counter**,
never a timestamp (timestamps collide under sub-millisecond concurrent writes) — bumped by the
`touch_row()` trigger on every genuine `UPDATE` (guarded by `WHEN (OLD.* IS DISTINCT FROM
NEW.*)`, so a no-op `PATCH` never inflates the version). Every mutable response includes
`record_version` so clients can round-trip it. A conflict returns `409` with the row's *actual*
current version, letting the client retry without a second read.

## Transaction discipline

There is **no `RunInTx`/event-injection seam of the kind `iam-user-profile` has** — every
repository method wraps its own single statement in its own transaction via `withPool`
(`internal/adapter/outbound/postgres/db.go`, thin wrapper over `pgcommon.RunInTx`). This is
possible because every write in this service is single-row, single-table (CAT-FAIL-3) — there is
no cross-aggregate side effect, no outbox enqueue, nothing that needs a wider transaction boundary.

## Cache consistency

Cache invalidation (`DELETE cat:<table>`) always runs **after** the DB write commits
successfully, never before or interleaved with it — a pre-commit `DELETE` would race a concurrent
reader into repopulating the cache with the stale pre-write value. A `DELETE` failure is logged but
never rolls back or fails the write (CAT-FAIL-1) — the 60 s TTL self-heals regardless.

## Failure invariants (LLD §11.2)

| # | Invariant |
|---|-----------|
| CAT-FAIL-1 | This service's own cache (`cat:*`) is advisory; Postgres is the source of truth. A cache miss or Valkey outage falls through to Postgres, never to a wrong answer. |
| CAT-FAIL-2 | A Catalog Service outage never produces an *incorrect* authorization/entitlement decision downstream — at worst a **stale** one, bounded by TTL then the 24 h stale-if-error ceiling, because every consumer treats this service's data as a cached snapshot, never a live dependency of the request path it protects. |
| CAT-FAIL-3 | Writes are all-or-nothing per row (`RunInTx`, single-table); there is no multi-row transaction anywhere in this service's write surface, so no partial-write failure mode beyond standard Postgres transaction semantics. |
| CAT-FAIL-4 | This service has no outbox, no SNS publisher, no SQS consumer — the entire "outbox crash/redelivery/DLQ" failure class that exists elsewhere in the IAM stack does not exist here. |

## Failure matrix (LLD §11.1)

| Scenario | Detection | Effect on this service | Effect on callers |
|---|---|---|---|
| This service's DB down | `/readyz` fails | Marks itself not-ready; writes rejected `503` | Core/Group Mapping serve from cache (TTL, then stale-if-error); no impact to Core's I-8 hot path |
| This service fully down (pod-level) | Envoy circuit-breaker / connection refused | n/a | Same as above — cache/stale-if-error absorbs it; a CAT-1/2/5 write fails `503` until recovery, blocking only operator actions |
| CAT-I1/CAT-I2 call times out | Caller-side timeout (50 ms budget) | No effect | Caller falls through to its `:stale` key; if that's *also* empty (e.g. a brand-new Core replica on first boot), the caller's admin/JIT write path returns `503 catalog_unavailable` — never a silently wrong answer |
| Optimistic-lock conflict on CAT-2/CAT-5 | `record_version` mismatch | `409 optimistic_lock_conflict` | Caller re-fetches and retries |
| A CAT-1/2/5 write succeeds but the cache `DEL` fails | Logged error, post-commit | Self-heals within 60 s TTL | None |

# Migration History (Wave 1 — complete)

This service was extracted from `iam-org-membership` (Core) per ADR-0007 Wave 1. **All four
expand/contract steps have executed and are verified against Core's actual shipped code** (not
just its docs) — this is not a still-pending plan.

1. **Expand** — `catalog_admin` database + `departments`/`plans` tables created (byte-identical
   shape to Core's originals); Core's rows exported/replicated in. Core's own tables/triggers
   remained fully live throughout.
2. **Cut over reads** — Core's `om:plans`/`om:departments` cache-population code now calls
   CAT-I2/CAT-I1 instead of a local `SELECT` (same cached value, new origin).
3. **Cut over writes** — this service became writer of record for both tables; Core's old
   operator handlers (O-1/O-2/O-3/O-5/O-6) went through a documented `410 Gone` soak.
4. **Contract** — `fk_tenants_plan`/`fk_td_department`/`fk_gdm_department` converted from DB FKs
   to application-level checks against the `om:departments`/`gm:departments`/`om:plans` caches
   (§8); the `410`-returning handlers were removed from Core entirely; `departments`/`plans`
   dropped from Core's schema (Core's migration `000013_drop_catalog_tables.up.sql`); retired IDs
   (O-1, O-2, O-3, O-5, O-6) were not reused.

**This service is now Core's sole system of record for both tables, with no fallback on Core's
side.** Core no longer has local `departments`/`plans` tables at all — a `CatalogAdminClient`
call failure coinciding with an empty/expired `om:*` cache past the 24 h stale-if-error ceiling is
now a hard failure for Core's department/plan-dependent writes, not a degraded-but-functional one.
**Rollback past this point requires restoring Core's tables from the pre-drop snapshot and
replaying any Catalog-Service-only writes since cutover** — reversibility ended when step 4
executed; see `MIGRATION_RUNBOOK.md` and LLD §12 for the full historical record and Appendix C
(§22) for the (now largely historical) recovery runbooks.
