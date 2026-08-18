# Changelog

All notable changes to `iam-catalog-admin` are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

### Fixed — `CATALOG_TTL_SECONDS` parse error was silently discarded

- **A fresh LLD-vs-code audit found `cmd/catalog-admin-config/main.go`'s `CATALOG_TTL_SECONDS`
  parsing still discarded its error** (`catalogTTLSeconds, _ := strconv.Atoi(...)`) — the same bug
  class just fixed for `PG_MAX_CONNS` via `pgcommon.ConfigFromEnv()` two lines above in the same
  function. Blast radius was contained (`WithCacheTTL` already ignores non-positive durations,
  falling back to the 60s default), but a malformed value produced no warning at all, contradicting
  LLD §15's "configuration is validated at startup, failing fast on an invalid value" claim. Fixed:
  a parse failure now logs a warning (the bad value and the error) and falls back to 60s explicitly,
  instead of silently discarding it — warn, not panic, since a cache TTL misconfiguration is
  low-stakes compared to Postgres connectivity. LLD bumped to v1.15 with this and three doc-only
  fixes found in the same pass (a revision-history ordering bug, a §20 error-envelope example
  missing `code`/`message`, and two minor §9/§13.5 wording corrections). No wire-contract change.
  Verified: `go build`/`go vet ./...`, `go test ./... -count=1`.

### Fixed — shared-library reuse audit found real duplication against `platform-gincommon`/`platform-pgcommon`

- **`pgcommon.ConfigFromEnv()` adopted** (`cmd/catalog-admin-config/main.go`), replacing a hand-rolled
  `PG_*` env-parsing block with a real bug: `strconv.Atoi(PG_MAX_CONNS)`'s error was discarded,
  silently defaulting to a broken `0` on any malformed value instead of failing loudly.
  `ConfigFromEnv` returns `[]pgcommon.ConfigWarning` instead of swallowing parse problems; a new
  `validatePostgresConfig` (replacing the Postgres-specific half of the old `validateRequiredEnv`)
  logs every warning unconditionally and escalates `PG_SSLMODE`/`DATABASE_URL` insecure-config
  warnings to a startup panic only in `production`/`staging`, preserving the existing fail-fast
  posture. Checked all six IAM services before adopting this — none currently call `ConfigFromEnv`
  (only referenced in `iam-org-membership`'s own docs, never its code); this service adopts it first.
  `internal/adapter/outbound/postgres/db.go`'s old `DSNFromEnv`/`envOrDB` are gone;
  `ApplyStatementTimeout`/`MigrationDSNFromEnv(appDSN)` remain as this service's own thin helpers on
  top of the `pgcommon`-resolved DSN. Also discovered: `pgcommon` rejects `PG_MIN_CONNS=0` as invalid
  (not just empty), so the previously-shipped `values.yaml`/`.env-example` explicit
  `PG_MIN_CONNS: "0"` would have warned on every startup and silently become `2` — removed the
  explicit override in both files in favor of `pgcommon`'s own default, with an explanatory comment.
- **Hand-rolled Postgres error matching replaced with `pgcommon.IsUniqueViolation`/`IsCheckViolation`/
  `ConstraintName`.** `department_repository.go`'s `Insert` (unique violation → `409 duplicate_code`)
  and `department_service.go`'s `Patch` (check violation → `422
  system_department_cannot_be_retired`/`422 system_name_immutable`) now match structurally via
  `errors.As`-backed helpers instead of a local `isCheckViolation`/`contains`/`indexOf` string-search
  set — deleted entirely, along with its now-dead test file
  (`internal/core/service/ischeckviolation_test.go`). The `chk_system_department_name_immutable`
  trigger (`prevent_system_department_name_change`) previously raised a bare `RAISE EXCEPTION` with
  no SQLSTATE/constraint metadata, matchable only by a message substring search; new migration
  `000004_department_name_immutable_errcode` gives it `ERRCODE = 'check_violation'` (SQLSTATE 23514,
  the same symbolic name `iam-org-membership`'s own `000007_pending_invitations_expiry_guard` uses
  for an equivalent trigger-raised check) plus a synthetic `CONSTRAINT`/`DETAIL`, so
  `pgcommon.ConstraintName(err)` resolves it exactly like a real CHECK constraint.
- **`/healthz` now serves `gincommon.HealthHandler()` directly** (`internal/adapter/inbound/http/router.go`),
  replacing a local reimplementation that reproduced its `{"status":"ok"}` body by hand.
- New/changed tests: `main_test.go` gained `TestValidatePostgresConfig_*` (6 cases), replacing the
  deleted `TestValidateRequiredEnv_ProdRejectsSSLModeDisable`/
  `_ProdRejectsDatabaseURLWithSSLModeDisable`; `dsn_test.go` rewritten for `ApplyStatementTimeout`/
  `MigrationDSNFromEnv`'s new shapes; `TestDepartmentService_Patch_NameImmutableCheckViolation` now
  constructs a real `*pgconn.PgError{Code: "23514", ConstraintName: ...}` instead of a plain
  `errors.New(...)` string (which would never have satisfied `errors.As` once the code switched to
  `pgcommon.IsCheckViolation`). LLD §4.1/§4.3 updated (v1.14) to name every newly-adopted symbol.
  Verified: `go build`/`go vet ./...`/`go vet -tags e2e ./...`, `go test ./... -race`, the full live
  `-tags e2e` suite (39 tests), and manual Docker-based smoke tests against real Postgres/Redis
  containers. No wire-contract change.

### Fixed — migration-status claim was badly stale (discovered via cross-service check)

- **LLD §12, `MIGRATION_RUNBOOK.md`, and `IMPLEMENTATION_GAP_ANALYSIS.md` all said "Phases 2 through
  7 have not been executed... this service is not yet receiving production traffic" — false, per a
  direct check against `iam-org-membership`'s (O&M) actual shipped code, not its docs.** O&M's
  `cmd/server/main.go` states the cutover completed and this service is sole writer; O&M's migration
  `000013_drop_catalog_tables.up.sql` drops both `departments`/`plans` tables and their four FKs;
  `operator_service.go` retains only O-4/O-7 — O-1/O-2/O-3/O-5/O-6 are gone entirely, not disabled;
  no `department_repository.go`/`plan_repository.go` remain anywhere in O&M. **O&M now has no local
  fallback** — an outage here coinciding with an empty/expired `om:departments`/`om:plans` cache
  (past the 24h stale-if-error ceiling) is a hard failure for O&M's writes, not a degraded one; the
  previous "not yet in production" framing actively obscured this. All three documents corrected;
  `MIGRATION_RUNBOOK.md`'s phases marked ✅ throughout, distinguishing directly-verified phases from
  ones inferred complete by logical necessity. No code changed — this was a documentation-only
  correction about a fact entirely outside this repository's own boundary.

### Added — CI now actually verifies `catalog_admin_app` has no `BYPASSRLS`

- **LLD §9 claimed "CI verifies this the same way MIG-3/MIG-5 verify it for O&M today" — no test or
  CI job in this repository ever did.** Added `TestRoles_AppRoleHasNoBYPASSRLS`
  (`test/e2e/roles_test.go`), mirroring `iam-org-membership`'s own
  `TestRLS_Case1b_AppRoleHasNoBYPASSRLS`: queries `pg_roles.rolbypassrls` for `catalog_admin_app`
  against a real Postgres testcontainer and fails if it's ever true. Runs automatically under the
  existing `make test-ci`/`./test/e2e/...` gate — no CI pipeline changes needed. Verified passing
  live against a container before merging.

### Fixed — Two flaky metrics-assertion tests

- **`TestRequestMetricsMiddleware_RecordsRouteAndStatus`/`_RecordsErrorStatus`
  (`internal/adapter/inbound/http/request_metrics_test.go`) asserted an absolute counter value
  against `catalog_admin_requests_total`, a package-level global not reset between test
  invocations** — harmless under default CI (`-count=1`) but would flake under any repeated run
  (`-count=N>1`, common in flake-hunting). Now assert a before/after delta instead. Verified stable
  under `-count=3`.

### Fixed — Production-readiness pass: structured logging, TLS enforcement, alert coverage

- **`valkey/cache.go`'s `Delete` and `middleware.go`'s unhandled-500 log line now go through a
  structured logger instead of stdlib `log.Printf`.** Both previously bypassed
  `platform-gincommon`'s JSON logging entirely. Both packages gained a minimal `Logger` interface
  (`Error(msg string, fields map[string]interface{})`) and a package-level `SetLogger`, installed
  once from `main.go` (mirrors `metrics.Register()`'s existing idiom); falls back to `log.Printf`
  if `SetLogger` was never called, so existing tests that don't set a logger are unaffected.
  `middleware.go`'s line additionally carries `trace_id`/`request_id`, via `gincommon`'s own
  `*gin.Context` helpers (`TraceIDFromContext`/`RequestIDFromContext`) — available there since
  `HandleError` already has the `*gin.Context`. `cache.go`'s line does not attempt the same:
  those helpers require a `*gin.Context`, which the Valkey adapter never has (only the plain
  `context.Context` that flows down from it), and reaching for the raw OTel API to reconstruct a
  trace ID for one advisory log line wasn't worth a new import in an otherwise dependency-light
  package. New tests: `TestHandleError_GenericFallback_UsesStructuredLoggerWhenSet`,
  `TestCache_Delete_FailureUsesStructuredLoggerWhenSet`.
- **`validateRequiredEnv` now enforces `PG_SSLMODE != disable` in production/staging**, mirroring
  the existing `VALKEY_URL` must-be-`rediss://` check. Previously Postgres TLS was only a Helm
  default (`values.yaml`'s `PG_SSLMODE: require`), not code-enforced — an accidental override could
  silently disable it in prod with nothing catching it. Covered by `main_test.go` (new — first
  tests for the `main` package, 7 cases covering `validateRequiredEnv`'s full branch set).
- **The two top-level server goroutines in `main.go` now `recover()` and log-then-`os.Exit(1)`
  on panic**, instead of relying on the Go runtime's default (a crash with a bare stack trace on
  stderr, no structured log line). Same eventual outcome (process exits, Kubernetes restarts the
  pod) with a diagnosable log line first.
- **Added `IAMCatalogAdminInternalErrorRate`** (`deploy/monitoring/app-alerts.yml` and
  `deploy/helm/templates/prometheusrule.yaml`) — LLD §13.5's CAT-I1/CAT-I2-scoped error-rate alert
  (>10% over 5 minutes, SEV-2), previously missing; the two existing error-rate alerts
  (`IAMCatalogAdminHighErrorRate`/`CriticalErrorRate`) are all-route and don't isolate the
  internal/bulk-read routes Core and Group Mapping depend on for cache population. Uses
  `catalog_admin_requests_total{route=~"/api/v1/internal/.*"}` (LLD §13.2's own route+exact-status
  metric), not the generic `http_requests_total` the other alerts in this group use.

### Fixed — error envelope's `error`/`code` field mismatch (LLD CAT-D9)

- **`errorResponseWithDetails` (`internal/adapter/inbound/http/middleware.go`) now re-syncs the
  response body's `error` field to the final, post-`WithDetails` `code`.** Previously, a
  handler-level sub-code attached via `WithDetails({"code": ...})` on top of a generic sentinel —
  `duplicate_code` over `conflict` (CAT-1's unique-violation path), `invalid_uuid` over
  `validation_error` (`parseUUIDParam`) — overwrote `code` in the response body but left `error` on
  the original sentinel, so the two fields silently disagreed. Fixed once at the shared envelope
  builder, not per call site, so the guarantee holds for every current and future sub-code. New
  test: `TestHandleError_SubCodeOverride_SyncsErrorField`.

### Changed — audit trail: built, then reverted, after checking the actual HLD

- **A local `audit_log` Postgres table was built to close the original "no audit trail" gap
  (LLD §10.7), then fully removed** after discovering the HLD (`iam-hld-tender-saas-v1.41.md` §5.7)
  defines a real, separate Audit Log Service — and that no LLD on the platform, including
  `org_membership_lld_5.md` (checked directly), specifies how a service actually integrates with
  it. Building a local stand-in against no known contract risked being the wrong shape once the
  real one is specified; CAT-1/CAT-2/CAT-5 writes remain covered only by structured request logging,
  the same posture O&M's own operator writes have. Net code diff for this repository is zero, but
  documented in full in LLD v1.9/v1.10 (CAT-D10 decision record, CAT-Q7 open item) since it's a
  meaningful reversal worth a durable trail, not just a silent no-op.

### Changed — Metrics implemented per LLD §13.2's exact names/labels

- **Most of §13.2's five specified metrics were missing, and the two that existed used a
  different prefix.** Previously `internal/adapter/outbound/metrics/metrics.go` registered only
  `catadmin_cache_hits_total{key}`/`catadmin_cache_misses_total{key}` — under a `catadmin_` prefix
  the code's own comment called out as a deliberate deviation from §13.2's `catalog_admin_`
  convention — and `catalog_admin_writes_total{table,op}`/
  `catalog_admin_optimistic_lock_conflicts_total{table}` didn't exist at all, directly undercutting
  §13.5's "optimistic-lock-conflict rate sustained > 0 for > 15 minutes" alert (no metric to fire
  on). All five now exist under `catalog_admin_`:
  - `catalog_admin_requests_total{route,status}` / `catalog_admin_request_duration_seconds{route,quantile}`
    (a `SummaryVec`, not `HistogramVec` — §13.2 names the label `quantile`, which only a Summary
    exposes) recorded by a new `requestMetricsMiddleware` (`internal/adapter/inbound/http/router.go`),
    scoped to `/api/v1` and running before auth so rejected requests are counted too — in addition
    to, not instead of, `platform-gincommon`'s generic `http_requests_total`/
    `http_request_duration_seconds`.
  - `catalog_admin_writes_total{table,op}` and `catalog_admin_optimistic_lock_conflicts_total{table}`,
    incremented directly in `DepartmentRepository`/`PlanRepository`'s `Insert`/`Update`
    (`internal/adapter/outbound/postgres/*_repository.go`) at the exact point each outcome is known.
  - `catalog_admin_cache_hits_total`/`catalog_admin_cache_misses_total` re-prefixed (not
    restructured — a hit-ratio-as-two-counters is derivable via PromQL, so no separate ratio gauge).
- **Added the missing §13.5 optimistic-lock-conflict alert** (`IAMCatalogAdminOptimisticLockConflicts`,
  new `catalog_admin_writes` rule group) to both `deploy/helm/templates/prometheusrule.yaml` and its
  static mirror `deploy/monitoring/app-alerts.yml` — the alert LLD §13.5 specifies now has an actual
  metric to fire on. Renamed the existing `catadmin_availability`/`catadmin_errors`/`catadmin_latency`
  rule groups to `catalog_admin_*` for consistency.
- Verified live: ran the service, made real HTTP requests (200/401/201/200/409), and confirmed every
  new metric increments correctly on `/metrics` (`METRICS_PORT`) with the exact label shapes above.

### Changed — cat:departments/cat:plans cache TTL externalized

- **`departmentsCacheTTL`/`plansCacheTTL` (both hardcoded `60 * time.Second` constants) replaced
  by a `CATALOG_TTL_SECONDS` env var** (default `60`, matching LLD §15's explicit
  `catalogAdminConfig.cache.catalogTtlSeconds` example — previously the one place in this service
  where that stated externalization philosophy wasn't actually applied in code, unlike
  `PG_MAX_CONNS` for `db.pool.maxConns`). `DepartmentService`/`PlanService` gained a `cacheTTL`
  field (defaulting to the old 60s constant) and a `WithCacheTTL(d time.Duration)` fluent setter —
  chosen over adding a constructor parameter so the ~30 existing `NewDepartmentService`/
  `NewPlanService` test call sites needed no changes. `cmd/catalog-admin-config/main.go` reads
  `CATALOG_TTL_SECONDS` once and applies it to both services via `.WithCacheTTL(...)`.
- `deploy/helm/values.yaml`: added `env.CATALOG_TTL_SECONDS: "60"`.

### Changed — Metrics moved to a dedicated port

- **`/metrics` no longer shares the API's gin router/port (`APP_PORT`, 8081)** — it now serves
  from a second, independent `http.Server` on its own port (`METRICS_PORT`, default `9090`),
  mirroring `iam-org-membership`'s, `iam-tender-acl`'s, and `iam-user-profile`'s identical split.
  Previously a `NetworkPolicy` grant for the monitoring namespace (`networkPolicy.metricsIngress`)
  had to open the *same* port the tenant-facing/gateway API used, since NetworkPolicy filters by
  port, not path — meaning any pod matching that selector had L3/L4 reach to the whole API
  surface, not just `/metrics`. The split closes that gap: `networkpolicy.yaml`'s
  `metricsIngress` rule now targets `service.metricsPort` exclusively.
- `deploy/helm/values.yaml`: added `service.metricsPort: 9090` and `env.METRICS_PORT`;
  `deployment.yaml`/`service.yaml` now declare a second `metrics` container/service port;
  `servicemonitor.yaml`'s endpoint now scrapes the `metrics` port instead of `http`.
  `Dockerfile` now `EXPOSE`s both `8081` and `9090`.

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
