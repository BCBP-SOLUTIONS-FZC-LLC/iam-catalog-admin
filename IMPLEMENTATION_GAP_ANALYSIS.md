# Implementation Gap Analysis

Comparing `catalog-admin-config-service-lld.md` against the current state of `iam-org-membership`
(O&M) as of this extraction, and recording what this repository (`iam-catalog-admin`) implements
to close each gap. "Current State" describes O&M's code *before* this extraction; "Required
Change" describes what this repo does about it. Affected files are repo-relative to
`iam-catalog-admin` unless prefixed `iam-org-membership/`.

## Departments (CAT-1, CAT-2, CAT-3, CAT-6, CAT-7)

| Requirement | Current State (O&M) | Required Change | Risk | Affected Files |
|---|---|---|---|---|
| CAT-1 `POST /operator/departments` | Implemented as O-1 (`operator_service.go:50-60`, `operator_handler.go:43-59`) | Ported to `DepartmentService.Create` / `DepartmentHandler.Create` | Low — logic unchanged | `internal/core/service/department_service.go`, `internal/adapter/inbound/http/department_handler.go` |
| CAT-2 `PATCH /operator/departments/:id` | Implemented as O-2, including the manual raw-JSON immutable-field check | Ported verbatim, including the raw-JSON check (load-bearing — DTO field omission alone doesn't reject `code`/`is_system`) | Low | same files as above |
| CAT-3 `DELETE /operator/departments/:id` → 405 | Implemented as O-3 | Ported verbatim | Low | same files |
| CAT-6 `GET /departments` (public list) | **Missing.** No route exists anywhere in O&M's `cmd/server/main.go` or handler files, despite the source LLD's own D-8 invariant requiring it. | **New code.** `DepartmentHandler.List`, any authenticated caller, optional `?active_only=true` | Medium — untested against real traffic patterns; no prior behavior to regress against, but also no production precedent to copy | `internal/adapter/inbound/http/department_handler.go` |
| CAT-7 `GET /departments/:id` (public get) | **Missing**, same as CAT-6 | **New code.** `DepartmentHandler.Get` | Medium, same rationale | same file |
| CAT-I1 `GET /api/v1/internal/departments` (bulk) | **Missing** — O&M reads its own local `departments` table directly wherever it needs one; no bulk-export endpoint exists because there was never a second process to export to. | **New code.** `InternalHandler.Departments`, mesh-only (`iam-system`). Path is `/api/v1/internal/departments` (CAT-D8, LLD v1.2) — matches O&M's already-built `CatalogAdminClient`, not the pre-CAT-D8 `/internal/departments` convention this row previously documented. | Low, was Medium — the cutover has since completed (verified against O&M's shipped `cmd/server/main.go`/`catalogadminclient`); this endpoint is O&M's live `om:departments` cache-population path today, not a not-yet-adopted seam. O&M has no local fallback anymore, so this endpoint being down (with O&M's cache also cold/expired) is now a real production incident on O&M's side, not a hypothetical. | `internal/adapter/inbound/http/internal_handler.go`, `internal/adapter/inbound/http/router.go` |
| D-1..D-11 invariants | Enforced via DB triggers + `chk_system_department_active` CHECK, all in O&M's migrations | Ported unchanged (byte-identical DDL/triggers) | Low — verified end-to-end against a real Postgres in `internal/adapter/outbound/postgres/integration_test.go` | `internal/adapter/outbound/postgres/migrations/000001_init_schema.up.sql`, `000002_triggers.up.sql` |
| O&M cache-invalidation gap on O-1/O-2 | **Bug, not spec.** O&M's `operator_service.go` never calls `cache.Delete` on department writes, despite the source LLD documenting both as `Cached: invalidates`. | **Fixed, not reproduced.** `DepartmentService.Create`/`Patch` correctly invalidate `cat:departments` after every write. | Low (this is strictly an improvement) | `internal/core/service/department_service.go` |
| Postgres grants gap (`org_membership_app` had only `SELECT` on `departments`/`plans` in O&M, yet performed INSERT/UPDATE) | Latent bug, masked in dev because the app role is also the migration owner | **Fixed, not reproduced.** `catalog_admin_app` is granted `SELECT, INSERT, UPDATE` on `departments` explicitly. | Low (improvement) | `internal/adapter/outbound/postgres/migrations/000003_roles_grants.up.sql` |

## Plans (CAT-4, CAT-5) and the feature-flags boundary

| Requirement | Current State (O&M) | Required Change | Risk | Affected Files |
|---|---|---|---|---|
| CAT-4 `GET /operator/plans` (list) | Implemented as O-5 (list only) | Ported to `PlanService.List` / `PlanHandler.List` | Low | `internal/core/service/plan_service.go`, `internal/adapter/inbound/http/plan_handler.go` |
| CAT-4 `GET /operator/plans/:code` (single get) | **Missing.** Only the list route is registered in O&M; no `GET /operator/plans/:code` exists despite the endpoint-ID table implying both. | **New code.** `PlanService.GetByCode` / `PlanHandler.Get` | Low — read-only, additive | same files |
| CAT-5 `PATCH /operator/plans/:code` | Implemented as O-6, including the tri-state (`absent`/`null`/`value`) limit parsing | Ported verbatim, including `parseLimit` | Low | same files |
| PLAN-1..PLAN-6 | Enforced partly at the DB (PLAN-1's `fk_tenants_plan`) and partly in `operator_service.go`'s validation | PLAN-1..5 unchanged (not this service's concern to re-verify); PLAN-6 (baseline⊕override merge) **stays in O&M** — this service supplies only the left operand | Medium — `fk_tenants_plan` cannot be enforced once `plans` moves databases; O&M must add an app-level check (see `O_AND_M_DELTA.md`) | n/a here; `iam-org-membership/internal/adapter/outbound/postgres/migrations/000001_schema.up.sql:219` |
| O-4 feature-flags (`tenants.feature_flags`) | Lives in `OperatorService.SetFeatureFlags` / `OperatorHandler.SetFeatureFlags`, has zero coupling to the `plans` table on its write path | **Not moved.** Confirmed zero dependency on `plans` in the write path — only the *read-time merge* (`authz_service.go`) depends on plan data, and that stays in O&M too. | Low — this extraction doesn't touch O-4 at all | n/a — explicitly out of scope |
| `om:plans` cache never actually populated in O&M (only ever `Delete`d, never `Set`) | Documented in the LLD as a working read-through cache; actual code only evicts | **N/A to this service** — this service's own `cat:plans` key IS correctly read-through cached (see `CACHE_DESIGN.md`). Whether O&M's consumer-side `om:plans` gets a working read-through implementation is tracked in `O_AND_M_DELTA.md`, since that's O&M's code, not this service's. | Medium (O&M-side) | `iam-org-membership/internal/core/service/operator_service.go:216-218` |
| Raw cross-table SQL in O&M's `membership_event_consumer.go:245` (`SELECT trial_duration_days FROM plans WHERE code = t.plan` inside a `tenants` UPDATE) | Present, bypasses `PlanRepository` entirely | **Breaks on cutover.** Must be rewritten in O&M to call this service (or read from `om:plans`) before the `tenants` UPDATE. Not fixed here — documented in `O_AND_M_DELTA.md`. | High — this is the one call site that will silently produce wrong `trial_ends_at` values if `plans` moves databases and this line isn't updated | `iam-org-membership/internal/adapter/inbound/consumer/membership_event_consumer.go:245` |

## Security (LLD §9)

| Requirement | Status | Notes |
|---|---|---|
| No RLS, no tenant_id anywhere | **Implemented.** No RLS migration, no GUC bridge, no `tenant_id` column. | `internal/adapter/outbound/postgres/migrations/`, `internal/adapter/inbound/http/middleware.go` (`IdentityBridgeMiddleware` parses identity only, no GUC) |
| `catalog_admin_app` has no BYPASSRLS | **Implemented.** `NOBYPASSRLS` explicit in the role migration (there's nothing to bypass, but the grant is withheld defensively, matching the LLD's stated posture). | `internal/adapter/outbound/postgres/migrations/000003_roles_grants.up.sql` |
| Write auth is a service-layer role check | **Implemented.** `RequireOperatorRole` middleware + in-handler `requireOperator` re-check (AUTH-6-equivalent defense-in-depth), mirroring O&M's pattern exactly. | `internal/adapter/inbound/http/middleware.go` |
| Internal routes mesh-only | **Implemented** at the app layer (`RequireSystemRole`/`requireSystem`, checks `iam-system`). **Not implemented** here: the actual mTLS/NetworkPolicy boundary — that's cluster configuration, out of this repo's scope, same as it is for every other IAM service. | `internal/adapter/inbound/http/middleware.go` |
| No PII | **N/A — confirmed true by inspection.** Neither table has ever held personal data. | n/a |

## Events (LLD §10)

| Invariant | Status |
|---|---|
| CAT-EVT-1 (no outbox) | **Implemented** — no `platform-events` dependency, no outbox table, no runner. |
| CAT-EVT-2 (no SQS consumer) | **Implemented** — no consumer package exists. |
| CAT-EVT-3 (no schema-gov registration) | **Implemented** — no `api/asyncapi.yaml`, no schema-gov Makefile targets. |
| CAT-EVT-4 (TTL-only propagation) | **Implemented** — see `CACHE_DESIGN.md`. |
| CAT-EVT-5 (future events re-enter governance) | **N/A today** — documented as a forward-looking constraint in `EVENT_COMPATIBILITY_REPORT.md`. |

## Audit (LLD §10.7)

**Not implemented — a real gap, and a deliberate decision not to paper over it with a local table.**
The HLD (`iam-hld-tender-saas-v1.41.md` §5.7) defines an Audit Log Service (its own `audit` RDS
database, 3 replicas, SNS-consumer architecture) and its §9.4 catalog-scope note names a "direct
audit write" category — `TenantSettingChanged` and siblings, entry types persisted directly rather
than via a bus event — that CAT-1/CAT-2/CAT-5 writes belong to by the same pattern. But no LLD on
the platform specifies that direct-write mechanism's actual contract (no ingest endpoint, no client
port, no schema), and no `iam-audit-log` repository exists to call. Checked directly against
`org_membership_lld_5.md` — the platform's most mature, most rigorously self-audited LLD, which
documents three outbound client ports (`UserProfileClient`, `WorkflowClient`,
`RealmProvisionerClient`) in full detail and ran a dedicated "second audit pass (config/ports)"
(rev 1.25) that explicitly confirmed all outbound clients were "defined+configured symmetrically"
with "no other referenced port/config undocumented" — and it has **no audit client either**,
despite referencing "writes a `TenantSettingChanged` audit entry" dozens of times. This is a
platform-wide gap between the HLD's stated architecture and every service's actual integration, not
something specific to this repository, and not something this repository can close alone by
inventing a client against a contract nobody has specified. A local `audit_log` table was briefly
built and then removed: an interim table against no known contract risks being the wrong shape once
the real one exists, so CAT-1/CAT-2/CAT-5 writes remain covered only by structured request logging
(`gincommon.ObservabilityMiddlewares`) — the same posture O&M's own operator writes have. Flagging
this for the platform team, unchanged in substance from before: once the Audit Log Service's
ingest contract is actually specified, this service should integrate directly. See LLD **CAT-D10**
(§14) and **CAT-Q7** (§19) for the full reasoning and open-item tracking.

## Metrics (LLD §13.2)

**Now implemented with the exact names/labels §13.2 specifies**, in
`internal/adapter/outbound/metrics/metrics.go`. Previously this package registered only two
counters (`cache_hits_total`/`cache_misses_total`) under a `catadmin_` prefix the code's own
comment called out as a deliberate deviation from §13.2's `catalog_admin_` convention, and
`catalog_admin_writes_total`/`catalog_admin_optimistic_lock_conflicts_total` didn't exist at all —
directly undercutting §13.5's "optimistic-lock-conflict rate sustained > 0 for > 15 minutes" alert,
which had no metric to fire on. All five now exist under the `catalog_admin_` prefix:
`catalog_admin_requests_total{route,status}` and `catalog_admin_request_duration_seconds{route,quantile}`
(a `SummaryVec`, not a `HistogramVec` — §13.2 names the label `quantile`, which only a Summary
exposes) are recorded by `requestMetricsMiddleware` (`internal/adapter/inbound/http/router.go`),
scoped to the `/api/v1` group and running before auth so rejected requests are still counted;
`catalog_admin_writes_total{table,op}` and `catalog_admin_optimistic_lock_conflicts_total{table}`
are incremented directly in `DepartmentRepository`/`PlanRepository`'s `Insert`/`Update`
(`internal/adapter/outbound/postgres/*_repository.go`) at the exact point each outcome is known;
`catalog_admin_cache_hits_total`/`catalog_admin_cache_misses_total` (`valkey/cache.go`) were
re-prefixed, not restructured — a hit-ratio-as-two-counters is derivable in PromQL and was judged
not worth a redundant gauge. These are this service's own business-level metrics, in addition to
(not instead of) the generic `http_requests_total`/`http_request_duration_seconds` platform-gincommon
already registers automatically with different labels (`status_class`/`error_class`, `le` buckets).

## Migration plan (LLD §12)

Covered in full in `MIGRATION_RUNBOOK.md`. **Status: complete, not still-pending as this section
previously said.** Verified directly against O&M's shipped code: `cmd/server/main.go` states the
cutover completed and this service is sole writer; O&M's migration `000013_drop_catalog_tables.up.sql`
drops both tables and their four FKs; `operator_service.go` retains only O-4/O-7 (O-1/O-2/O-3/O-5/O-6
are gone, not disabled); no `department_repository.go`/`plan_repository.go` remain anywhere in O&M.
This service is now O&M's sole system of record for both tables, with **no local fallback on O&M's
side** — an outage here coinciding with an empty/expired `om:departments`/`om:plans` cache (past the
24 h stale-if-error ceiling) is a hard failure for O&M's department/plan-dependent writes, not a
degraded one. This section previously said "no cutover has happened; this service is not yet
receiving production traffic" — that was stale as of a cross-service check against O&M's actual
code, not this repository's own.
