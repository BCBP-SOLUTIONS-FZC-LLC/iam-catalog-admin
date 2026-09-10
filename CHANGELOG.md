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

- Initial extraction of the Catalog / Admin Config Service from `iam-org-membership`
  (ADR-0007) — global `departments` and `plans` reference tables, with no `tenant_id`
  and no RLS (LLD §10).
- CAT-1/CAT-2 department create/patch, CAT-3 delete-blocked, CAT-6/CAT-7 department
  read; CAT-4/CAT-5 plan list/get/patch; CAT-I1/CAT-I2 mesh-only bulk reads for
  `iam-org-membership`'s read-cutover.
- Two-tier `cat:departments`/`cat:plans` Valkey cache (60s TTL), advisory-only
  (CAT-FAIL-1 — every miss/timeout falls through to Postgres).
- `iam-org-membership` read-cutover: `CatalogReader`/`CatalogAdminClient` ports, a
  stale-if-error two-tier cache (`om:departments`/`om:plans` + 24h `:stale` fallback),
  and rewired `DepartmentService`/`DeptMembershipService`/`ProvisioningService`/
  `AuthZService` consumers.
- Helm chart (`deploy/helm/`) and CI/CD pipeline (`.github/workflows/`), both ported
  from `iam-user-profile`'s conventions and scaled to this service's shape (no
  outbox/SNS/SQS/S3/Glue, no RLS/GUC, single binary).
- Test coverage uplift to 98.9% (242 scenarios).
- `TestRoles_AppRoleHasNoBYPASSRLS` — verifies `catalog_admin_app` has no `BYPASSRLS`.
- `IAMCatalogAdminInternalErrorRate` and `IAMCatalogAdminOptimisticLockConflicts` alerts.
- `CATALOG_TTL_SECONDS` env var for the `cat:departments`/`cat:plans` cache TTL.
- `deploy/monitoring/slo-rules.yml` — multi-window burn-rate SLO alerts for the three §11.1
  route-class latency targets, mirroring `iam-org-membership`'s own SLO-rules pattern.
- `.github/workflows/validate-test.yml`'s Swagger staleness check (`check-swagger-stale.sh`
  existed but was never wired into CI).
- Helm chart: `imagePullSecrets`, a writable `/tmp` `emptyDir` (required by
  `readOnlyRootFilesystem: true`), `podAnnotations`/`podLabels`/`nodeSelector`/`tolerations`
  pass-through, `serviceAccount.name` override — parity with `iam-org-membership`'s chart.

### Changed

- `catalog_admin_request_duration_seconds` is now a Histogram, not a Summary (CAT-D13) — a
  Summary's client-side quantiles can't feed a burn-rate SLO's error-budget ratio.
- All third-party Go dependencies bumped to latest (`go get -u ./...` + `go mod tidy`): `pgx/v5`
  v5.10.0→v5.11.0, `go-redis/v9` v9.8.0→v9.22.0, `testify` v1.11.1→v1.12.1, `swaggo/swag`
  v1.8.12→v1.16.6, `go.opentelemetry.io/otel` v1.45.0→v1.46.0, plus indirect transitive bumps.
  `docs/swagger/*` regenerated (`make swag`) and diffed — no OpenAPI-surface change.

- Logs, metrics, and traces now follow `iam-org-membership`/`iam-realm-provisioner`:
  `gincommon.InitTracingFromEnv()` always (OTLP export still no-op until a collector is set),
  `ObservabilityMiddlewares` primed before collector registration, `pgmetrics.InitWithRegisterer`
  on gincommon's registerer, and `pgCfg.Tracer = NewOTelTracer` for `db.query` spans.
- Database connection, config, and error classification now follow the same siblings:
  `platform-pgcommon` bumped to v1.3.0; `wrapConnErr` remaps SQLSTATE 08/53/57/58 via
  `IsConnectionException`/`IsInsufficientResources`/`IsPgError`; inbound HTTP no longer
  imports `pgconn`; `MigrationDSNFromEnv()` takes no `appDSN` argument; test seed/assert
  SQL goes through `test/dbseed` (`pgcommon.NewPool`/`WithConn`) instead of a raw `pgxpool`.
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
- Two hand-rolled test-only `pgx.Rows` mocks (`test/dbseed/pool.go`'s `bufferedRows`,
  `repo_whitebox_test.go`'s `mockRows`) updated for pgx v5.11.0's new `TypeMap()` interface method —
  no production code implements `pgx.Rows` directly, so no production behavior changed.
- `postgres.DSNFromEnv`'s no-`DATABASE_URL` branch (the `PG_*`-vars path, always applying
  `ApplyStatementTimeout`) had no direct test — closed the gap with
  `TestDSNFromEnv_NoDatabaseURL_AppliesStatementTimeout`, taking merged coverage to 100%.

### Removed

- Reverted a local `audit_log` table — no Audit Log Service ingest contract exists yet.
- Superseded rollout/gap-analysis docs; content is now in the LLD's own decision register.
