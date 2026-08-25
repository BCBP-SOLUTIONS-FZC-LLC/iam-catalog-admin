# Changelog

All notable changes to `iam-catalog-admin` are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

### Added

- Test coverage uplift: 17 previously-uncovered branches across the unit/whitebox tier (cache TTL
  setters, `PlanService.Patch` validation, `HandleError`'s 413 path, `readyz` failure paths,
  request-metrics/docs-route fallbacks, OCC-conflict repository paths, the Postgres domain
  logger). Merged coverage 94.9% → 98.9% (242 test scenarios).
- `TestRoles_AppRoleHasNoBYPASSRLS` (`test/e2e/roles_test.go`) — verifies `catalog_admin_app` has
  no `BYPASSRLS` against a real Postgres container; runs under `make test-ci`.
- `IAMCatalogAdminInternalErrorRate` alert (`deploy/monitoring/app-alerts.yml`) — CAT-I1/CAT-I2
  error-rate alert (>10% over 5 minutes, SEV-2), scoped to `catalog_admin_requests_total{route=~"/api/v1/internal/.*"}`.
- `IAMCatalogAdminOptimisticLockConflicts` alert, paired with the `catalog_admin_optimistic_lock_conflicts_total`
  metric.
- `CATALOG_TTL_SECONDS` env var (default `60`) externalizes the `cat:departments`/`cat:plans`
  cache TTL via `DepartmentService`/`PlanService.WithCacheTTL`; a parse failure logs a warning and
  falls back to 60s rather than discarding the error.

### Changed

- **`make test-postgres`/`make test-e2e` (and therefore `make test-ci`) run dramatically faster.**
  Every test in `test/postgres`/`test/e2e` spins up its own isolated Postgres testcontainer (and,
  for e2e, its own miniredis instance and `httptest.Server`) — fully independent, but none of the
  240 test functions called `t.Parallel()`, so Go ran them one at a time per package, paying the
  ~1s container-boot cost serially. Added `t.Parallel()` to every top-level test in both packages
  (safe: no shared package-level state, no fixed ports, no `os.Setenv`, no subtests). Measured on a
  10-CPU box: `test/postgres` 31s → 8s, `test/e2e` 220s → 50s, full `make test-ci` (with `-race`)
  down to ~73s.
- `pgcommon.ConfigFromEnv()` adopted for Postgres config, replacing a hand-rolled `PG_*` parser
  that silently defaulted `PG_MAX_CONNS` to `0` on a malformed value. `validatePostgresConfig`
  logs every config warning and escalates `PG_SSLMODE`/`DATABASE_URL` insecure-config warnings to
  a startup panic in `production`/`staging`.
- Postgres error matching (`Insert`'s unique-violation, `Patch`'s check-violation paths) now uses
  `pgcommon.IsUniqueViolation`/`IsCheckViolation`/`ConstraintName` instead of hand-rolled
  message-substring search. `prevent_system_department_name_change`'s trigger now raises with a
  structured `ERRCODE = 'check_violation'` + synthetic `CONSTRAINT` name so it matches the same
  way a real CHECK constraint does.
- `/healthz` now serves `gincommon.HealthHandler()` directly instead of a local reimplementation.
- All five §13.2 metrics now exist under the `catalog_admin_` prefix (previously a mix of
  `catadmin_`-prefixed and missing metrics): `catalog_admin_requests_total`/`_request_duration_seconds`,
  `_writes_total`, `_optimistic_lock_conflicts_total`, `_cache_hits_total`/`_cache_misses_total`.
- `/metrics` now serves from a dedicated `METRICS_PORT` (default `9090`), separate from the API's
  `APP_PORT` (`8081`), so NetworkPolicy can grant scrape access without also granting API access.
- `valkey/cache.go`'s `Delete` and `middleware.go`'s unhandled-500 log line now go through a
  structured logger (`SetLogger`) instead of stdlib `log.Printf`.
- `validateRequiredEnv` now enforces `PG_SSLMODE != disable` in production/staging, mirroring the
  existing `VALKEY_URL` TLS check.
- The two top-level server goroutines in `main.go` now `recover()` and log-then-`os.Exit(1)` on
  panic instead of crashing with a bare stack trace.
- `errorResponseWithDetails` now re-syncs the response body's `error` field to the final,
  post-`WithDetails` `code`, so a handler-level sub-code (e.g. `duplicate_code` over `conflict`)
  no longer leaves `error`/`code` disagreeing.
- Postgres migrations consolidated into a single file, `000001_init_schema` — this service has not
  yet been deployed, so there is no environment holding a prior multi-file migration history that
  needs preserving.

### Fixed

- Two flaky metrics-assertion tests (`TestRequestMetricsMiddleware_RecordsRouteAndStatus`/
  `_RecordsErrorStatus`) asserted an absolute counter value against a package-level global;
  now assert a before/after delta.
- Stale docs (LLD, `MIGRATION_RUNBOOK.md`, `IMPLEMENTATION_GAP_ANALYSIS.md`) claimed this service
  was "not yet receiving production traffic" — false; `iam-org-membership`'s cutover to this
  service as sole system of record for `departments`/`plans` is already complete. Doc-only
  correction.

### Removed

- A local `audit_log` Postgres table, built to close the "no audit trail" gap (LLD §10.7), was
  reverted after confirming the platform's real Audit Log Service (HLD §5.7) has no specified
  integration contract yet. CAT-1/CAT-2/CAT-5 writes remain covered only by structured request
  logging until that contract exists.
- `MIGRATION_RUNBOOK.md`, `IMPLEMENTATION_GAP_ANALYSIS.md`, `EVENT_COMPATIBILITY_REPORT.md`, and
  `O_AND_M_DELTA.md` — four point-in-time docs superseded by the now-complete Wave-1
  extraction/cutover (LLD §12/§16) and already duplicated in the LLD's own decision register
  (§14) and revision history. Every cross-reference to them elsewhere in the repo updated or
  removed in the same pass (LLD v1.27).

---

## [0.1.0]

### Added

- Initial extraction of the Catalog / Admin Config Service from `iam-org-membership`
  (ADR-0007) — global `departments` and `plans` reference tables, with no `tenant_id`
  and no RLS (LLD §9).
- CAT-1/CAT-2 department create/patch, CAT-3 delete-blocked, CAT-6/CAT-7 department
  read; CAT-4/CAT-5 plan list/get/patch; CAT-I1/CAT-I2 mesh-only bulk reads for
  `iam-org-membership`'s read-cutover.
- Two-tier `cat:departments`/`cat:plans` Valkey cache (60s TTL), advisory-only
  (CAT-FAIL-1 — every miss/timeout falls through to Postgres).
- `iam-org-membership` read-cutover: `CatalogReader`/`CatalogAdminClient` ports, a
  stale-if-error two-tier cache (`om:departments`/`om:plans` + 24h `:stale` fallback),
  and rewired `DepartmentService`/`DeptMembershipService`/`ProvisioningService`/
  `AuthZService` consumers.
- Full test suite (unit/postgres/e2e tiers, black-box `test/unit`+`test/postgres`
  matching `iam-org-membership`'s layout) at 98%+ combined statement coverage.
- Helm chart (`deploy/helm/`) and CI/CD pipeline (`.github/workflows/`), both ported
  from `iam-user-profile`'s conventions and scaled to this service's shape (no
  outbox/SNS/SQS/S3/Glue, no RLS/GUC, single binary).
