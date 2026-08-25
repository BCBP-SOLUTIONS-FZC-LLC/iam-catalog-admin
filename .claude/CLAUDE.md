# CLAUDE.md

This file provides guidance to Claude Code when working with the **Catalog / Admin Config
Service** (`iam-catalog-admin`), a Go microservice in the IAM subsystem.

## What This Repo Is

`iam-catalog-admin` is a **private Go service** (module:
`github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin`, Go 1.26.6) that is the authoritative system
of record for the platform's two **global, non-tenant-scoped** reference catalogs:

- **`departments`** — the operator-managed list of departments a tenant may activate (5 seeded
  system departments — Engineering, Design, Procurement, Finance, Legal — plus operator-added
  entries).
- **`plans`** — the three-tier (`starter`/`pro`/`enterprise`) baseline entitlement catalog every
  tenant's *effective* feature set is computed against. This service owns the baseline only; Core
  Org & Membership (`iam-org-membership`) owns the per-tenant override delta
  (`tenants.feature_flags`) and performs the merge at its own I-8 read time.

Extracted from `iam-org-membership` per ADR-0007 Wave 1 (the lowest-risk cut: neither table
carries a `tenant_id`, neither is RLS-protected, neither participates in Core's I-8 hot-path SQL
join). **The migration is complete** — this service is Core's sole system of record for both
tables; Core's local `departments`/`plans` tables and O-1/O-2/O-3/O-5/O-6 handlers have been
dropped entirely (LLD §12).

**This is a pure leaf service** (LLD §4): no outbound synchronous call to any other IAM service, no
outbox/SNS/SQS, no RLS/tenant-context GUC (neither table has a `tenant_id`), no PII. Writes are
rare and operator-driven; reads are cached and always allowed to be briefly stale (LLD §8, §11).

**Does NOT own:** the per-tenant feature-flag override delta (Core owns it), any tenant data, any
event/audit trail beyond structured request logging (LLD §10.7/CAT-D10 — no Audit Log Service
contract exists yet to call).

**Source of truth for:** `GET /api/v1/internal/departments` (CAT-I1) and
`GET /api/v1/internal/plans` (CAT-I2) — consumed by Core Org & Membership (`om:departments`,
`om:plans`) and the Group Mapping Service (`gm:departments`) to populate their own local caches,
replacing the DB FKs each lost on the physical split (LLD §7).

## Common Commands

```bash
make setup           # Copy .env-example → .env; install .githooks/pre-commit
make tidy            # go mod tidy
make fmt             # gofmt -w .
make fmt-check       # Verify gofmt formatting without modifying files (mirrors CI)
make vet             # go vet ./...
make lint            # Alias for vet — no golangci-lint config yet
make mod-verify      # go mod verify
make vuln-check      # govulncheck ./internal/... ./pkg/...
make test            # Unit tests only (no Docker required)
make test-unit       # Unit tests only (no Docker required)
make test-postgres   # Postgres-backed repository/migration tests (requires Docker, -tags=integration)
make test-e2e        # Full-stack HTTP tests against real Postgres+Valkey (requires Docker, -tags=e2e)
make test-integration # Alias for test-postgres — this service has no events/outbox, so
                      # "integration" means "Postgres-backed", not cross-service (SNS/SQS)
make test-ci         # unit + postgres + e2e with merged coverage (used in CI)
make race            # Unit + white-box tests with -race
make run             # Run the server locally (go run; kills APP_PORT first)
make build           # Compile to bin/catalog-admin-config
make cover           # Merged coverage HTML report across unit+postgres+e2e (requires Docker)
make cover-func      # Merged coverage summary by function (terminal)
make ci              # tidy + fmt-check + vet + lint + test-ci + build
make swag            # Regenerate docs/swagger/ from handler annotations
make swag-check      # Fail if Swagger regeneration would change docs/swagger/ (CI drift gate)
make docker-up       # Start local Postgres (port 5535) + Valkey (port 6381)
make docker-down     # Stop local containers
make install-hooks   # Install local git hooks (.githooks -> .git/hooks); also run by make setup
make clean           # Remove bin/, .coverage/, coverage.out, coverage.html
```

To run a single test:
```bash
go test ./test/unit/... -run TestDepartmentService_Patch -v
go test ./test/postgres/... -tags=integration -run TestRoles_AppRoleHasNoBYPASSRLS -v
go test ./test/e2e/... -tags=e2e -run TestDepartmentCreate -v
```

**Test package groups** (mirrors `iam-org-membership`'s tiered layout, scaled down — no
`test/integration` tier at all, since this service has no events/outbox to cross-service-test):
- `test/unit/` — fast unit tests (mocks, no Docker), plus **white-box tests** living directly
  alongside the production packages they exercise (`internal/adapter/inbound/http/*_test.go`,
  `internal/adapter/outbound/postgres/*_test.go`, `internal/adapter/outbound/valkey/*_test.go`,
  `internal/adapter/outbound/metrics/*_test.go`, `internal/core/domain/*_test.go`,
  `internal/core/service/*_test.go`, `pkg/**/*_test.go`) — these reach package-private helpers
  (`isDBUnavailableSQLState`, `requireOperator`, `withPool`, `envOr`, …) a black-box `test/unit`
  package can't reach.
- `test/postgres/` — Postgres + constraints/trigger integration (testcontainers), `-tags=integration`.
- `test/e2e/` — full-stack HTTP tests against real Postgres+Valkey, `-tags=e2e`.

**Coverage note:** `make test-ci` runs all three tiers with `-coverpkg=./internal/...,./pkg/...`
into separate `.coverage/{unit,postgres,e2e}.out` profiles, then `scripts/merge_coverage.py` merges
them (max-count strategy) into `coverage.out` — a Postgres-only file only shows real coverage once
`test/postgres`'s profile is merged with `test/unit`'s. CI's coverage gate is **≥ 98%** (this
service's own established baseline — a much smaller surface than `iam-user-profile`'s 95%, with no
outbox/RLS/event machinery to leave deliberately uncovered). Currently at **99.6%**, verified via
`make cover-func` — see CHANGELOG.md's `[Unreleased]` section for what's landed since the gate was
set.

**Testcontainers note:** Postgres integration tests spin up a real container via
`testcontainers-go`; set `TESTCONTAINERS_RYUK_DISABLED=true` in CI to skip the reaper sidecar.

**Parallelism:** every test in `test/postgres`/`test/e2e` calls `t.Parallel()` — each one already
builds its own fully isolated Postgres container (and, for e2e, its own `miniredis` instance and
`httptest.Server`), so running them concurrently (bounded by `GOMAXPROCS`) is safe and turns the
per-test ~1s container-boot cost from a serial tax into a parallel one. Measured on a 10-CPU box:
`test/postgres` 31s → 8s, `test/e2e` 220s → 50s. A new test in either package should call
`t.Parallel()` too, right after `t.Helper()`/before any setup — omitting it silently falls back to
running that one test serially against the others, not a correctness bug but a quiet regression in
suite wall-time.

## Architecture

Hexagonal / ports-and-adapters, same layering as `iam-org-membership` scaled down to two
aggregates. Enforced in CI via `go-arch-lint` (`.go-arch-lint.yml`).

```
iam-catalog-admin/
├── cmd/
│   └── catalog-admin-config/
│       ├── main.go                    # composition root — no GUCProvider (no RLS/tenant GUC),
│       │                              #   no outbox/SQS drain step in shutdown (LLD §10)
│       ├── main_test.go               # TestValidatePostgresConfig_*, TestValidateRequiredEnv_*
│       └── swagger_info.go            # swaggo @title/@version metadata
├── internal/
│   ├── core/
│   │   ├── domain/                    # entities, value objects, domain errors (no external deps)
│   │   │   ├── department.go          # Department — global operator catalog row (LLD §5.1)
│   │   │   ├── plan.go                # TenantPlan/BrandingLevel enums, Plan, PlanPatch (LLD §5.2)
│   │   │   └── errors.go              # sentinel errors, DomainError, NewError, WithDetails
│   │   ├── port/                      # interfaces required by the core
│   │   │   ├── department_repository.go
│   │   │   ├── plan_repository.go
│   │   │   └── cache.go               # Cache — advisory-only (CAT-FAIL-1); nil is a valid config
│   │   └── service/                   # use cases
│   │       ├── department_service.go  # CAT-1/2/3/6/7 + CAT-I1's listing half; cat:departments cache
│   │       └── plan_service.go        # CAT-4/5 + CAT-I2's listing half; cat:plans cache
│   └── adapter/
│       ├── inbound/
│       │   └── http/                  # Gin handlers, DTOs, middleware
│       │       ├── department_handler.go  # CAT-1/2/3/6/7
│       │       ├── plan_handler.go        # CAT-4/5
│       │       ├── internal_handler.go    # CAT-I1/I2 — mesh-only bulk reads
│       │       ├── router.go              # single source of truth for routes/middleware (LLD-aligned)
│       │       ├── middleware.go          # IdentityBridgeMiddleware, RequireOperatorRole,
│       │       │                          #   RequireSystemRole, RequireJSONContentType,
│       │       │                          #   NormalizeAuthErrors, HandleError, domainErrorStatus
│       │       ├── dto.go                 # ErrorResponse, request/response DTOs
│       │       ├── errors.go              # newErrorResponse
│       │       ├── swagger_initializer.go # /swagger/*any handler + swaggo entrypoint
│       │       └── swagger_theme.go       # custom Swagger UI CSS
│       └── outbound/
│           ├── postgres/              # repository impls + migrations/
│           │   ├── department_repository.go
│           │   ├── plan_repository.go
│           │   ├── db.go              # ApplyStatementTimeout, MigrationDSNFromEnv, withPool, wrapConnErr
│           │   ├── logger.go          # NewDomainLogger — bridges pgcommon's slow-query logger
│           │   ├── migrate.go         # RunMigrations (embed.FS, bypasses PgBouncer)
│           │   └── migrations/        # 000001_init_schema (single consolidated migration; pre-prod)
│           ├── valkey/                # cache impl (go-redis/v9)
│           │   └── cache.go           # DepartmentsKey/PlansKey constants, hit/miss metrics
│           └── metrics/                # custom Prometheus counters/summary (catalog_admin_* prefix)
│               └── metrics.go
├── pkg/
│   └── requestctx/
│       └── context.go                 # RequestContext{UserID, TenantID, Roles, ClientIP, UserAgent},
│                                       #   HasRole, IsOperator, IsSystem
├── docs/
│   ├── swagger/                       # generated by `make swag` — checked in to repo
│   ├── architecture/
│   │   ├── README.md                  # index of mermaid diagrams
│   │   └── mermaid/                   # layer-model.mmd, write-flow.mmd, cache-strategy.mmd
│   └── lld/
│       └── iam-lld-catalog-admin-config-service.md  # the authoritative LLD (currently v1.26)
├── scripts/                           # local dev + build tooling only
│   ├── merge_coverage.py              # merges per-suite coverage profiles for the CI gate
│   └── patch-swagger-extensions.py    # post-processes docs/swagger during `make swag`
├── deploy/
│   ├── helm/                          # Helm chart + values.yaml (2 replicas, no reconciler CronJob)
│   └── monitoring/
│       ├── app-alerts.yml             # PrometheusRule — IAMCatalogAdminDown, etc.
│       └── dashboards/
│           └── catalog-admin-requests-writes.json  # the one Grafana dashboard this repo ships
├── test/
│   ├── unit/                          # fast unit tests (mocks, no Docker)
│   ├── postgres/                      # constraints/trigger integration (testcontainers)
│   ├── e2e/                           # full-stack HTTP tests
│   └── fixtures/                      # shared test helpers
├── Dockerfile  docker-compose.yml  Makefile  go.mod  .go-arch-lint.yml
├── ARCHITECTURE.md                    # detailed architecture documentation
├── CACHE_DESIGN.md                    # cat:* cache design + consumer-side cache reference (LLD §8)
├── MIGRATION_RUNBOOK.md               # Wave-1 rollout phases (historical — migration is complete)
├── IMPLEMENTATION_GAP_ANALYSIS.md     # requirement-by-requirement comparison vs. source LLD
├── EVENT_COMPATIBILITY_REPORT.md      # confirms this service publishes/consumes no events
├── O_AND_M_DELTA.md                   # the concrete code change required in iam-org-membership
├── CHANGELOG.md                       # Keep a Changelog format; read [Unreleased] for latest fixes
└── README.md                          # onboarding + quick-start
```

### Shared library dependencies

```go
require (
    github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon v1.3.0
    github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon   v1.2.1
)
```

- `platform-gincommon` — HTTP middleware, logging, tracing (OTel OTLP), `ObservabilityMiddlewares`,
  `ProtectedMiddlewares`, `HealthHandler`.
- `platform-pgcommon` — PostgreSQL pool (`pgx/v5`) via `pgcommon.ConfigFromEnv()`, migrations
  (`pkg/migrate.Runner`), `RunInTx`, `IsUniqueViolation`/`IsCheckViolation`/`ConstraintName` helpers.

**Not a dependency:** `platform-events` (no outbox/SNS/SQS — LLD §10, CAT-EVT-1/2), any AWS SDK
package (no S3/SNS/SQS/Glue — LLD §10.5), `iam-keycloakclient`.

### Dependency rules (enforced in CI via `go-arch-lint`, see `.go-arch-lint.yml`)

- `domain` — no internal imports at all; pure value types only.
- `port` — may depend on `domain` only.
- `service` — may depend on `domain` + `port` only.
- `requestctx` — no internal deps.
- `observability` (`internal/adapter/outbound/metrics`) — no internal deps; imported by *both*
  the inbound HTTP adapter and the outbound valkey adapter (cross-cutting).
- `adapters_inbound`/`adapters_outbound` — may depend on `service`/`port`/`domain`/`requestctx`/
  `observability`; nothing in `core/` ever imports `adapter/`.
- `cmd` — the composition root; may depend on everything.

`deepScan: false` is deliberate (`.go-arch-lint.yml` comment): a deep scan would follow method
calls through the composition root and flag every DI wire in `main.go` as a cross-layer
dependency, which is how Clean Architecture is *supposed* to work there — import-level checks only.

## Key Files to Know

- **`cmd/catalog-admin-config/main.go`** — composition root. No `GUCProvider` on
  `pgcommon.NewPool` (no RLS/tenant GUC to bridge). Uses `pgcommon.ConfigFromEnv()` (not a
  hand-rolled DSN parse — closes a real bug where a typo'd `PG_MAX_CONNS` used to silently become
  `0` connections). `validateRequiredEnv` (Valkey/migration-DSN routing) runs before Postgres
  connects; `validatePostgresConfig` (DSN presence + prod/staging SSL-mode escalation) runs after
  `ConfigFromEnv` resolves. `CATALOG_TTL_SECONDS` parse failure logs a warning and falls back to
  60s rather than silently discarding the error (fixed — see CHANGELOG `[Unreleased]`). Metrics
  served on a **separate port** (`METRICS_PORT`, default 9090) from the API port (`APP_PORT`,
  default 8081) so NetworkPolicy can grant scrape access without also granting API access.
  Shutdown order: HTTP `Shutdown` → metrics server `Shutdown` → `cancelBackground` →
  `shutdownTracing` → `gincommon.Shutdown` — no outbox/SQS-consumer drain step (this service has
  neither).
- **`internal/adapter/inbound/http/router.go`** — single source of truth for every route,
  method, and middleware (`NewRouter`); both `main.go` and `test/e2e`'s harness call this same
  function, so route drift between them is structurally impossible. Disables
  `RedirectTrailingSlash` (an empty `:code`/`:id` path segment must 404, not silently redirect to
  the parent list route). Overrides Gin's default empty-body 405 with the standard JSON error
  envelope. 1 MB body cap enforced twice: `Content-Length` pre-check (immediate 413) +
  `http.MaxBytesReader` (catches chunked requests with no declared length).
- **`internal/core/domain/errors.go`** — sentinel errors (`ErrValidation`, `ErrDepartmentNotFound`,
  `ErrOptimisticLockConflict`, `ErrFieldImmutable`, `ErrSystemNameImmutable`,
  `ErrSystemDepartmentCannotBeRetired`, `ErrMethodNotAllowed`, `ErrNoMutableField`,
  `ErrInvalidFeatureValue`, `ErrDBUnavailable`, `ErrDependencyUnavailable`, …), `DomainError`,
  `NewError`, `WithDetails` — mirrors `iam-org-membership`'s pattern, trimmed to this service's
  two-aggregate error catalogue.
- **`internal/core/service/department_service.go`** — `List` serves both CAT-6 (public,
  `active_only` filtering applied **in-memory after** the cache read) and CAT-I1 (internal bulk,
  `activeOnly=false`) off the **same** `cat:departments` cache entry. `Patch` translates two
  distinct Postgres CHECK-violation constraint names
  (`chk_system_department_active` → `system_department_cannot_be_retired`,
  `chk_system_department_name_immutable` → `system_name_immutable`) into domain errors via
  `pgcommon.IsCheckViolation`/`ConstraintName` — never a raw-message substring search.
  `DeleteBlocked` unconditionally returns `405 method_not_allowed` (CAT-3) — departments are
  **never** physically deletable, retire via `is_active=false` instead.
- **`internal/core/service/plan_service.go`** — `Patch` mirrors every DB-level `CHECK` constraint
  at the service layer (non-negative limits, `custom_branding` enum) so violations surface as
  `400 validation_error` rather than a raw constraint error. `feature_set` values must be scalars
  (string/bool/number/nil) — a nested object/array returns `400 invalid_feature_value` with the
  offending `key`. Never reads or writes `tenants.feature_flags` — the per-tenant override merge
  happens exclusively in Core at I-8 read time (PLAN-6).
- **`internal/adapter/inbound/http/plan_handler.go`** — `Patch`'s `parseLimit` distinguishes three
  JSON states for `workflow_template_limit`/`tender_limit` via `json.RawMessage`: field absent (no
  `**int` at all → no `SET` clause), explicit `null` (`**int` → `nil *int` → `NULL` in DB =
  unlimited), explicit integer (`**int` → `*int` → value = capped). This is why `domain.PlanPatch`
  uses a **double pointer** for these two fields (CAT-D6).
- **`internal/adapter/inbound/http/department_handler.go`** — `Patch` pre-reads the raw JSON body
  to reject `code`/`is_system` in the request with `422 field_immutable` **before** binding to the
  DTO struct (which would otherwise silently drop them) — a request-shape check, distinct from the
  DB-trigger-enforced `system_name_immutable`/`system_department_cannot_be_retired` checks above.
- **`internal/adapter/inbound/http/middleware.go`** — `IdentityBridgeMiddleware` (role-check-only
  identity parsing — no RLS GUC to bridge, unlike `iam-org-membership`'s `GUCBridgeMiddleware`);
  `RequireOperatorRole`/`RequireSystemRole` (route-group gates, each re-checked in-handler too via
  `requireOperator`/`requireSystem` — defense-in-depth); `HandleError`/`domainErrorStatus` (maps
  `DomainError.Code` → HTTP status); `isDBUnavailableSQLState` (SQLSTATE class `08`/`53`/`57`/`58`
  → `503 db_unavailable`); `NormalizeAuthErrors` (rewrites `platform-gincommon`'s bare 401s to
  include this service's `code` field).
- **`internal/adapter/outbound/postgres/db.go`** — `withPool` wraps every repository call in its
  own single-statement transaction via `pgcommon.RunInTx` — there is **no** higher-level
  `TxRunner`/event-injection seam here (unlike `iam-user-profile`), because every write in this
  service is single-row, single-table (CAT-FAIL-3). `wrapConnErr` converts non-protocol DB errors
  into `ErrDependencyUnavailable`, passing `DomainError`/`pgconn.PgError`/`pgx.ErrNoRows`/context
  cancellations through unchanged.
- **`internal/adapter/outbound/postgres/migrate.go`** — `RunMigrations` uses the **direct**
  (non-PgBouncer) DSN via `MigrationDSNFromEnv`, because `pg_advisory_lock` is session-scoped and
  breaks under transaction pooling.
- **`internal/adapter/outbound/valkey/cache.go`** — `DepartmentsKey`/`PlansKey` constants
  (`cat:departments`/`cat:plans`). `Get` records a hit/miss metric per key; a real error (not
  `redis.Nil`) is excluded from the hit-ratio metric — `Health()` is the signal for Valkey being
  down. Tight default timeouts (50 ms read/write, 100 ms dial) so a slow Valkey degrades to a fast
  miss rather than stalling the request.
- **`internal/adapter/outbound/metrics/metrics.go`** — `catalog_admin_*` business metrics,
  registered onto `gincommon.MetricsRegisterer()`/`MetricsConstLabels()` (same registry, same
  `{service, version}` labels as `http_requests_total`). `Register` is idempotent via
  `sync.Once` — needed because `test/e2e` builds a fresh `RouterConfig` per test via the same
  composition-root call path.
- **`pkg/requestctx/context.go`** — `RequestContext{UserID, TenantID, Roles, ClientIP, UserAgent}`,
  `HasRole`, `IsOperator()` (`platform_operator` role), `IsSystem()` (`iam-system` role).
- **`docs/lld/iam-lld-catalog-admin-config-service.md`** — the authoritative LLD (currently v1.26).
  Read this before making any contract-level change; it carries a decision register (CAT-D1
  through CAT-D12, §14) and a full error taxonomy (§20) that must stay in sync with the code.

## See Also

Detailed reference docs in `.claude/`:
- [Database Schema](database.md) — tables, roles/grants, migrations, triggers
- [API, Caching & Events](api-and-events.md) — endpoints, cache keys, why this service has no events
- [Request Flows & Concurrency](flows-and-concurrency.md) — write flows, optimistic locking, the Wave-1 migration history
- [Operations](operations.md) — security, observability, configuration, CI/CD
- [Development Guide](development-guide.md) — design decisions, extending, workflow, troubleshooting, error codes

Supplementary docs in the repo root:
- **`README.md`** — onboarding, prerequisites, quick-start, local dev setup
- **`ARCHITECTURE.md`** — detailed architecture narrative
- **`CACHE_DESIGN.md`** — `cat:*` cache design + the consumer-side (`om:*`/`gm:*`) cache reference
- **`MIGRATION_RUNBOOK.md`** — the Wave-1 rollout phases (historical; migration is complete)
- **`IMPLEMENTATION_GAP_ANALYSIS.md`** — requirement-by-requirement comparison vs. the source LLD
- **`EVENT_COMPATIBILITY_REPORT.md`** — confirms this service publishes/consumes no events
- **`O_AND_M_DELTA.md`** — the concrete code change this extraction required in `iam-org-membership`
- **`CHANGELOG.md`** — Keep a Changelog format; check `[Unreleased]` first for the latest fixes/audits
