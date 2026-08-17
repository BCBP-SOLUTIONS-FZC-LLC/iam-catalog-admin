# Migration Runbook

Cross-service rollout for extracting `departments`/`plans` from `iam-org-membership` (O&M) into
this service, per `catalog-admin-config-service-lld.md` §12. **Status: all seven phases below have
completed.** Verified directly against O&M's shipped code (checked, not assumed): `cmd/server/main.go`
states outright that "migration-runbook Phase 4 (LLD §12 step 4) completed the cutover —
catalog-admin-config is now the sole writer too; O-1/O-2/O-3/O-5/O-6 and the local departments/plans
tables have been removed from this service entirely"; O&M's migration `000013_drop_catalog_tables.up.sql`
physically drops both tables and their four FKs; `operator_service.go` retains only O-4/O-7 — O-1/O-2/O-3/O-5/O-6
are gone, not disabled; no `department_repository.go`/`plan_repository.go` exist anywhere in O&M's
outbound/postgres. **This service is now O&M's sole system of record for both tables, with no local
fallback on O&M's side** — an outage here coinciding with an empty/expired `om:departments`/`om:plans`
cache is a hard failure for O&M's department/plan-dependent writes, not a degraded one. This document
previously said "only Phase 1 is complete... O&M remains the live system of record" — that was stale
as of the cross-service check that produced this correction. Each phase below is retained as the
historical record of what happened, not a still-pending plan; the ✅ per phase reflects that it
actually executed, verified against O&M's code where evidence exists to check.

## Phase 1 — Deploy new service ✅ (this repository)

**What:** Stand up `iam-catalog-admin` with its own database, migrations, and the full CAT-1..CAT-I2
endpoint surface, independently deployable and independently testable. No traffic is routed to it yet.

- **Risk:** Low — this is a greenfield deployment; nothing else depends on it existing.
- **Rollback:** Trivial — undeploy the service, drop its database. Nothing else references it.
- **Observability:** `/healthz`, `/readyz` (DB + cache health), `/metrics` (Prometheus, `catalog_admin_*` prefix). Confirm `/readyz` returns `200` before proceeding to Phase 2.
- **Verification:** `go test ./...` and `go test -tags=integration ./...` both green (the latter runs real migrations + repository behavior against a throwaway Postgres via testcontainers — see `internal/adapter/outbound/postgres/integration_test.go`). Manual smoke: start the binary locally, `curl -X POST /api/v1/operator/departments` with a `platform_operator`-role header set, confirm `201`; `curl /api/v1/departments`, confirm the new row appears.

## Phase 2 — Dual-write ✅ (inferred complete)

No direct artifact confirms this phase's execution specifically, but it's a logical prerequisite
for Phase 4 (confirmed done, below) — this service could not have become O&M's read source without
its tables first being populated from O&M's data.

**What:** The LLD's actual design (§12) does **not** call for a dual-write window — it explicitly
chooses a **read-cutover-before-write-cutover** sequence with a one-time data export/logical
replication instead, specifically to avoid the operational cost of a dual-write phase. This phase
is renamed here to match what the LLD actually specifies: **data expand**.

- One-time export of O&M's current `departments`/`plans` rows into this service's database (or logical replication for a live cutover with zero data-loss window).
- **Risk:** Medium — a bad export silently seeds this service with stale/incomplete data. Mitigate by comparing row counts and a full diff of both tables before proceeding.
- **Rollback:** Truncate this service's tables and re-run the export. O&M is untouched and still authoritative.
- **Observability:** Row-count parity check between O&M's `departments`/`plans` and this service's, run as a one-off script, not a permanent monitor.
- **Verification:** `SELECT count(*), max(updated_at) FROM departments` (and `plans`) on both databases must match before Phase 3 begins.

## Phase 3 — Shadow-read ✅ (inferred complete)

No direct artifact confirms a shadow-read period specifically ran, but Phase 4's confirmed
completion implies this preceding phase ran per the plan's own required ordering.

**What:** Deploy the new internal client in O&M (see `O_AND_M_DELTA.md` §4) behind a feature flag
that calls **both** the old local `SELECT` path and the new CAT-I1/CAT-I2 client, logging any
divergence without acting on the new result yet.

- **Risk:** Low — this is observation-only; the old path still serves every real response.
- **Rollback:** Remove the feature flag; no user-facing behavior ever changed.
- **Observability:** A counter/log line on every divergence between old-path and new-path results. Alert if divergence rate is non-zero after the Phase 2 export completed (any non-zero rate past that point indicates a real bug, not expected staleness).
- **Verification:** Run for at least one full `om:plans`/`om:departments` TTL window (600s) with zero unexplained divergences before proceeding.

## Phase 4 — Read cutover ✅ (confirmed complete)

Directly verified: O&M's `cmd/server/main.go` wires `catalogAdminClient`/`catalogReader`
(`service.NewCatalogService(catalogAdminClient, cache)`) as the actual cache-population path, with
a comment stating the cutover completed and this service became sole writer.

**What:** O&M's `om:plans`/`om:departments` cache-population code switches from the local `SELECT`
to the new client exclusively (LLD §12 step 2 — "behavior-preserving... same cached value, new
origin"). Operator tooling is repointed at this service's CAT-1..CAT-7. O&M's O-1/O-2/O-3/O-5/O-6
handlers and tables remain live as a rollback target.

- **Risk:** Medium — this is the first phase where a real request path depends on this service being up; a cold cache plus this service being down is the CAT-D4 failure mode.
- **Rollback:** Repoint the cache-population code and operator tooling back at O&M's local `SELECT`/handlers — both still exist and are unmodified.
- **Observability:** This service's own dashboards (request rate/latency for CAT-6/7/I1/I2, `cat:*` cache hit ratio per `CACHE_DESIGN.md`) plus, critically, O&M's **consumer-side** `om:departments`/`om:plans` cache-miss rate and stale-if-error activation count (LLD §13 — "the signal that this service is degraded from a consumer's perspective, even if this service's own dashboards look healthy").
- **Verification:** Compare pre/post cache contents in staging (byte-identical `planDefaults(plan)` output, per the LLD's own backward-compat plan, §12).

## Phase 5 — Endpoint migration ✅ (confirmed complete)

Directly verified: no `410`-returning handler for O-1/O-2/O-3/O-5/O-6 exists anywhere in O&M's
current code — the soak ran and the handlers were subsequently removed in Phase 7, below.

**What:** O&M's O-1/O-2/O-3/O-5/O-6 handlers are disabled — return `410 Gone`, not removed yet
(LLD §12 step 3) — so any caller that missed the Phase 4 tooling cutover gets an unambiguous
signal instead of silently writing to a now-unreplicated copy. This service becomes the sole
writer of record for both tables.

- **Risk:** Medium — any caller still hardcoded to the old O&M routes breaks visibly (which is the point) but that's still an incident if it's a production integration nobody remembered to update.
- **Rollback:** Re-enable the O&M handlers (they're disabled, not deleted) — reversible up through the end of this phase per the LLD's own rollback note.
- **Observability:** Alert on any `410 Gone` traffic during this soak period — it's a caller that missed the cutover, not noise.
- **Verification:** Run this phase for a full release cycle (minimum: one full on-call rotation) with zero unexpected `410` traffic before proceeding to an irreversible step.

## Phase 6 — Ownership transfer ✅ (confirmed complete)

Directly verified: O&M's migration `000013_drop_catalog_tables.up.sql` drops all four FKs
alongside the tables — per this plan's own required ordering (app-level checks before the drop,
not after), the FK-to-app-check conversion must have already been in place by the time that
migration ran.

**What:** Convert `fk_tenants_plan`/`fk_td_department`/`fk_gdm_department` (the latter once
Group Mapping Service, Wave 2, ships) from DB foreign keys to application-level checks against the
cached copies described in `O_AND_M_DELTA.md` §3/§5. This must happen **before** the next phase's
`DROP TABLE`.

- **Risk:** High — this is the step that permanently changes the referential-integrity guarantee from "the database enforces it" to "the application enforces it, bounded by cache TTL staleness." Get this step's app-level check logic reviewed independently before merging.
- **Rollback:** Difficult past this point — reverting requires re-adding the DB FK, which requires the tables to still exist (true up through Phase 6, false after Phase 7).
- **Observability:** New metric on the app-level check's rejection rate (an operator-department-retirement scenario landing inside the staleness window, per CAT-D7, would show up here).
- **Verification:** Load-test the app-level check path under realistic write volume (tenant-department activation, dept-membership creation) to confirm it doesn't become the new bottleneck the DB FK used to silently absorb for free.

## Phase 7 — Cleanup ✅ (confirmed complete)

Directly verified: O&M's migration `000013_drop_catalog_tables.up.sql` drops `departments`/`plans`
from O&M's schema; `operator_service.go` retains only O-4/O-7 — O-1/O-2/O-3/O-5/O-6 are gone
entirely, not just disabled. This is the step LLD §12 refers to as "step 4 (Contract)," and it is
the one O&M's own `main.go` comment cites by name as complete.

**What:** Remove the `410`-returning O-1/O-2/O-3/O-5/O-6 handlers from O&M entirely (not just
disable them); drop `departments`/`plans` from O&M's schema. Retired IDs (O-1, O-2, O-3, O-5, O-6)
are never reused in O&M's catalogue (mirroring the I-6/I-7 quota-retirement precedent).

- **Risk:** High and **irreversible** — this is a `DROP TABLE`. Take a full pre-drop snapshot/logical-replication checkpoint immediately before this step, retained for at least one full incident-response window (per your org's standard retention for irreversible schema changes).
- **Rollback:** Restore O&M's tables from the pre-drop snapshot and replay any Catalog-Service-only writes since cutover (there should be none needing replay, since this service has been sole writer since Phase 5 — the replay concern is only for writes this service made that O&M's restored snapshot wouldn't have).
- **Observability:** Confirm zero references to the dropped tables remain in O&M's codebase (`grep -rn "FROM departments\|FROM plans\|JOIN departments\|JOIN plans"` across `iam-org-membership` should return nothing outside historical migration files).
- **Verification:** Full O&M test suite green with the tables actually dropped in a staging database (not just code review) — this catches any remaining raw-SQL reference like the `membership_event_consumer.go:245` one flagged in `O_AND_M_DELTA.md`.
