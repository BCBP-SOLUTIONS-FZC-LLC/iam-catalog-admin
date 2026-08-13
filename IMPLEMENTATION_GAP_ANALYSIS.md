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
| CAT-I1 `GET /internal/departments` (bulk) | **Missing** — O&M reads its own local `departments` table directly wherever it needs one; no bulk-export endpoint exists because there was never a second process to export to. | **New code.** `InternalHandler.Departments`, mesh-only (`iam-system`) | Medium — this is the seam O&M/Group-Mapping must switch to (see `O_AND_M_DELTA.md`); until that cutover happens, this endpoint has no real caller | `internal/adapter/inbound/http/internal_handler.go` |
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

## Audit (LLD §10.6)

**Partially implemented — a genuine gap, not a design choice.** The LLD states every write should
produce "an audit-log entry via the same mechanism O&M uses for non-eventful writes," but
inspection of `platform-gincommon`/`platform-pgcommon` and O&M's own code found **no dedicated
audit-log table or service anywhere in the platform** — O&M's own O-1/O-2/O-3 writes have never
had one either, despite the same LLD line implying they should. This service currently relies on
structured request logging (`gincommon.ObservabilityMiddlewares`' `LoggingMiddleware`) as the only
durable record of a write, which is *not* the same guarantee as a queryable audit trail. **This is
carried forward as a known limitation, not fixed here**, since fixing it would mean inventing a
new platform-wide audit mechanism — explicitly out of scope ("Do not invent new frameworks").
Flagging this for the platform team: if audit-log is genuinely required, it needs a shared
`platform-audit` library adopted by every IAM service, not a one-off table in this service.

## Migration plan (LLD §12)

Covered in full in `MIGRATION_RUNBOOK.md`. Summary of what's true today: this repository
implements Phase 1 ("Expand" — schema + service exist and are independently deployable) and is
ready for Phase 2 ("Cut over reads"), but **Phases 2 through 7 have not been executed** — O&M's
tables, triggers, and O-1/O-2/O-3/O-5/O-6 handlers remain live and authoritative. No cutover has
happened; this service is not yet receiving production traffic.
