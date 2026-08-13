# Org & Membership (O&M) Delta

**This document is descriptive only — no code in `iam-org-membership` has been changed.** Per
explicit scoping decision, this pass documents exactly what must change in that repository so a
follow-up engineer (or a follow-up task against that repo) can execute it; it does not touch
`iam-org-membership`'s code, tests, or migrations.

## 1. Endpoints removed from O&M

| Old ID | Route | Disposition |
|---|---|---|
| O-1 | `POST /api/v1/operator/departments` | Replaced by CAT-1 in this service. O&M's handler must first return `410 Gone` (soak period), then be deleted entirely (LLD §12 step 3-4). |
| O-2 | `PATCH /api/v1/operator/departments/:id` | Replaced by CAT-2. Same `410`-then-delete sequencing. |
| O-3 | `DELETE /api/v1/operator/departments/:id` | Replaced by CAT-3. Same sequencing (it currently always 405s anyway, so the `410` soak is low-risk). |
| O-5 | `GET /api/v1/operator/plans` | Replaced by CAT-4. Same sequencing. |
| O-6 | `PATCH /api/v1/operator/plans/:code` | Replaced by CAT-5. Same sequencing. |

**Not moving** (confirmed explicitly out of scope, per the LLD and this extraction's own
boundary): O-4 (`PATCH /api/v1/operator/tenants/:id/feature-flags`) and O-7
(`POST /api/v1/operator/tenants/:id/reassign-owner`) stay in O&M unchanged.

Files: `internal/adapter/inbound/http/operator_handler.go` (delete `CreateDepartment`,
`PatchDepartment`, `DeleteDepartmentBlocked`, `ListPlans`, `PatchPlan` — keep `SetFeatureFlags`,
`ReassignOwner`, `requireOperator`), `cmd/server/main.go` (remove the 5 corresponding route
registrations at lines 512-517, keep O-4/O-7's two lines).

## 2. Repositories removed from O&M

| Component | Disposition |
|---|---|
| `internal/adapter/outbound/postgres/department_repository.go` | Delete. Superseded by this service's own copy. |
| `internal/adapter/outbound/postgres/plan_repository.go` | Delete. Superseded by this service's own copy. |
| `internal/core/port/department_repository.go` (the `DepartmentRepository` interface — **not** `TenantDepartmentRepository`, which stays) | Delete the `DepartmentRepository` interface only. `TenantDepartmentRepository` is tenant-scoped and stays in O&M. |
| `internal/core/port/plan_repository.go` | Delete. |
| `internal/core/domain/plan.go` | Delete `Plan`/`PlanPatch`/`BrandingLevel` (they move to this service). **Keep** `internal/core/domain/tenant.go`'s `TenantPlan` enum and `Tenant.FeatureFlags` — those stay, per §3 below. |
| `internal/core/service/operator_service.go` | Split: delete the department methods (`CreateDepartment`, `PatchDepartment`, `DeleteDepartmentBlocked`) and the plan methods (`ListPlans`, `PatchPlan`) and the `plans`/`depts` struct fields; **keep** `SetFeatureFlags` and `ReassignOwner` and the `tenants`/`tenRoles`/`memBs` fields. |
| `internal/core/service/department_service.go` | **Keep, but change its dependency.** `DepartmentService` (P-3/P-24/P-25, tenant-activation) currently calls `catalog.FindByID` directly against the local `PlanRepository`-equivalent. Once the catalog moves, this must become a call against a local cache (`om:departments`) with a fallback to a new HTTP client (see §4). |
| `internal/core/service/dept_membership_service.go`, `provisioning_service.go`, `group_mapping_service.go` | **Keep, but change their dependency the same way** — each holds a `catalog port.DepartmentRepository` field used for D-5/TD-1 pre-flight checks; all three must be repointed at the new cache-backed client. |

## 3. Tables removed from O&M (Phase 4 of the LLD's migration plan — after cutover, not immediately)

`departments` and `plans` are dropped from O&M's schema **only after** the app-level checks in §4
below are live (LLD §12 step 4's explicit ordering — contract before drop). Concretely:

- `fk_tenants_plan` (on `tenants`) — becomes an app-level check against `om:plans`' cached keys, belt-and-suspenders on top of the still-local `tenant_plan` ENUM copy.
- `fk_td_department` (on `tenant_departments`) — becomes an app-level check against `om:departments`.
- New migration in O&M: `DROP TABLE departments, plans;` plus the associated triggers/enums this service now owns (`tenant_plan`/`branding_level` types stay in O&M as **local copies** — a Postgres ENUM can't be shared across databases, per LLD §5.2 — only the *tables* drop).

## 4. New internal client added

O&M needs a new outbound HTTP client package, `internal/adapter/outbound/catalogadmin/`, mirroring
the existing `realmprovisioner`/`userprofile`/`workflow` client pattern
(`internal/adapter/outbound/{realmprovisioner,userprofile,workflow}/`) — thin, fail-open-aware,
with a 50ms internal-call budget per the LLD's `CAT-I1`/`CAT-I2` latency expectations. It exposes:

```go
type Client interface {
    Departments(ctx context.Context) ([]Department, error) // calls GET /api/v1/internal/departments
    Plans(ctx context.Context) ([]Plan, error)              // calls GET /api/v1/internal/plans
}
```

Called from the cache-population path described in §5, and from the three services in §2's last
row in place of their current direct `DepartmentRepository`/`PlanRepository` calls, on a cache
miss.

## 5. Cache changes

| Key | Change |
|---|---|
| `om:departments` (**new** — never existed in O&M under any name) | Populate on miss by calling the new client's `Departments()`, cache full catalog, TTL 600s. |
| `om:plans` (**existing name, new source**) | Currently only ever `Delete`d, never populated (a pre-existing O&M bug, not introduced by this extraction — see `IMPLEMENTATION_GAP_ANALYSIS.md`). Must become a real read-through cache backed by the new client's `Plans()` call, TTL 600s (unchanged). |
| `om:departments:stale` / `om:plans:stale` | **New.** 24h TTL, refreshed opportunistically on every successful client call, served only when the primary key has expired *and* the live call fails (CAT-D4). |

`internal/adapter/outbound/valkey/cache.go` needs new key builders (`DepartmentsKey()`,
`DepartmentsStaleKey()`, `PlansStaleKey()`) alongside the existing `PlansKey()`.

## 6. Migration order

Per LLD §12, executed as a single ordered sequence (not all at once):

1. **Expand** — this repository's schema is created and seeded from O&M's current `departments`/`plans` rows (one-time export or logical replication). O&M's tables/handlers stay live.
2. **Cut over reads** — deploy this service; point operator tooling at CAT-1..CAT-7; switch O&M's `om:plans`/`om:departments` population to call CAT-I1/CAT-I2 (§4, §5 above). Verify via pre/post cache-content comparison in staging.
3. **Cut over writes** — this service becomes writer of record. O&M's O-1/O-2/O-3/O-5/O-6 handlers return `410 Gone` (not deleted yet).
4. **Contract** — convert `fk_tenants_plan`/`fk_td_department` to app-level checks (§3); remove the `410` handlers entirely; drop `departments`/`plans` from O&M's schema. Fix `membership_event_consumer.go:245`'s raw cross-table SQL (see `IMPLEMENTATION_GAP_ANALYSIS.md`) *before* this step, since it will silently break the moment `plans` leaves O&M's database.

**Rollback** is reversible through the end of step 3 (repoint tooling/cache-population back at
O&M, which still has live tables). After step 4, rollback requires restoring O&M's dropped tables
from a pre-drop snapshot and replaying any writes this service took since cutover.

## 7. Synchronous call documentation (for the new internal client, §4)

| | |
|---|---|
| Path | O&M/Group-Mapping → this service, mesh-internal, `GET /api/v1/internal/departments` or `/plans` |
| Timeout | 50ms client-side budget (LLD §7 states ≤30ms p99 server-side; the client budget leaves headroom for network) |
| Retry policy | **None specified by the LLD** — a single attempt; on failure, fall through to the `:stale` cache key (§5), then to `503 catalog_service_unavailable` if that's also empty (e.g. a brand-new replica's first boot) |
| Fallback behavior | Serve `om:departments:stale`/`om:plans:stale` (24h TTL) with a logged warning; never a silent wrong answer (LLD §11, CAT-FAIL-2) |
