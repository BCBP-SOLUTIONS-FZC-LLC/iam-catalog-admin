# Changelog

All notable changes to `iam-catalog-admin` are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

**This service has not yet been deployed to any environment** — `[Unreleased]` is pre-production
development history, not a release note for users of a running system. Kept brief; the full
rationale for any entry lives in `docs/lld/iam-lld-catalog-admin-config-service.md`'s revision
history (§ "Revision history").

---

## [Unreleased]

### Added

- Test coverage uplift to 98.9% (242 scenarios).
- `TestRoles_AppRoleHasNoBYPASSRLS` — verifies `catalog_admin_app` has no `BYPASSRLS`.
- `IAMCatalogAdminInternalErrorRate` and `IAMCatalogAdminOptimisticLockConflicts` alerts.
- `CATALOG_TTL_SECONDS` env var for the `cat:departments`/`cat:plans` cache TTL.

### Changed

- `test/postgres`/`test/e2e` parallelized (`t.Parallel()`) — `make test-ci` ~3x faster.
- Postgres config resolved via `pgcommon.ConfigFromEnv()`; error matching via
  `pgcommon.IsUniqueViolation`/`IsCheckViolation`/`ConstraintName`.
- `/healthz` now serves `gincommon.HealthHandler()` directly.
- Metrics unified under the `catalog_admin_` prefix; `/metrics` moved to a dedicated
  `METRICS_PORT`.
- `cache.go`/`middleware.go` log through the structured logger instead of stdlib fallbacks; server
  goroutines `recover()` and log-then-exit on panic.
- Postgres migrations consolidated into a single `000001_init_schema` file.

### Fixed

- `errors.go` now sources `trace_id` via `gincommon.TraceIDFromContext`, matching `middleware.go`,
  instead of the raw OTel SDK API.
- `main.go`'s Postgres DSN assembly (`postgres.DSNFromEnv`) no longer corrupts `DATABASE_URL` when
  combined with `PG_STATEMENT_TIMEOUT`.
- Flaky metrics-assertion tests now assert before/after deltas instead of absolute counter values.

### Removed

- Reverted a local `audit_log` table — no Audit Log Service ingest contract exists yet.
- Superseded rollout/gap-analysis docs; content is now in the LLD's own decision register.

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
