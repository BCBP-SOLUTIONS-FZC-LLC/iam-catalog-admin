# iam-catalog-admin

Catalog / Admin Config Service — the authoritative system of record for the platform's two
**global, non-tenant-scoped** reference catalogs: the department catalog and the plan entitlement
catalog. Extracted from Org & Membership (`iam-org-membership`) per ADR-0007 Wave 1 — the
lowest-risk cut of that decomposition, since neither table ever carried a `tenant_id`, was
RLS-protected, or participated in Core's I-8 hot-path SQL join.

**Repository:** `github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin`
**Module:** Go 1.26.6 · private module · deployed as a single binary from one image (`cmd/catalog-admin-config`)
**Design:** Refines ADR-0007's decomposition rationale; LLD **v1.35** (`docs/lld/iam-lld-catalog-admin-config-service.md`). The seven-phase Wave-1 rollout this extraction followed is complete (LLD §19) — this repo is `iam-org-membership`'s sole system of record for both tables, with no local fallback on its side.

---

## Mental model

Two tables, no tenants, no events. `departments` is the operator-managed list of departments a
tenant may activate (Engineering, Design, Procurement, Finance, Legal, plus operator-added
entries). `plans` is the per-tier baseline entitlement catalog (`starter`/`pro`/`enterprise`) that
every tenant's *effective* feature set is computed against — this service owns the baseline only;
Core Org & Membership owns the per-tenant override delta (`tenants.feature_flags`) and performs the
merge at its own I-8 read time. This service never sees tenant data and is not on any hot path —
writes are rare and operator-driven; reads are cached and served from a snapshot that is always
allowed to be briefly stale (LLD §6, §9).

| This service owns | It does NOT own |
|---|---|
| `departments` — the global department catalog, operator-managed lifecycle (create, rename, retire; never hard-deleted) | Credentials, MFA, JWT issuance (Keycloak) |
| `plans` — the three-tier baseline entitlement catalog (limits, SSO flag, branding level, `feature_set`) | The per-tenant feature-flag override delta (`tenants.feature_flags` — Core Org & Membership) |
| — | Pricing / billing data (Billing Service) |
| — | Usage metering against the entitlement ceilings this service defines (Usage & Metering Service) |
| — | The I-8 hot-path entitlement merge itself (AuthZ Enrichment, via Core) |
| — | Tenant-facing department/plan *management* (`GET /api/v1/tenants/:id/departments` stays in Core) |

---

## Why this service exists

`iam-org-membership` originally owned both tables directly. ADR-0007 splits three low-risk,
tenant-independent concerns out of that service into their own leaf services; this is Wave 1, the
lowest-risk cut — neither table carries a `tenant_id`, neither is RLS-protected, and neither
participates in Org & Membership's I-8 hot-path SQL join. Centralizing both catalogs behind one
small service, independent of the tenant/membership core, means an operator-driven schema edit
(a new department, a plan-tier limit change) can never touch — or be blocked by — anything on the
platform's tightest read path.

This is enforced structurally: `internal/core/service` depends only on `internal/core/port`
interfaces, never on a vendor DB type directly, and `.go-arch-lint.yml` encodes the full
dependency-direction ruleset CI checks on every PR.

---

## API overview

**9 active endpoints** across three route prefixes. Source of truth: `internal/adapter/inbound/http/router.go`; the generated REST contract is `docs/swagger/swagger.yaml` (`make swag`), not hand-authored. This service's own `CAT-*` ID space has had no retirements yet — see [VERSIONING.md](VERSIONING.md) for what happens once one does.

**Middleware chain:** `ProtectedMiddlewares` (gateway identity headers) → `IdentityBridgeMiddleware` (role parse) → a route-group role gate (`RequireOperatorRole`/`RequireSystemRole`, where applicable) → an in-handler defense-in-depth re-check before any business logic runs. The service trusts gateway-injected headers (`x-user-id`, `x-tenant-id`, `x-tenant-roles`) under mesh mTLS — no JWT parsing.

### Public routes (2) — `/api/v1/*`

| ID | Method & Path | Purpose |
|---|---|---|
| CAT-6 | `GET /departments` | Global catalog listing, `active_only` filter applied in-memory after the cache read |
| CAT-7 | `GET /departments/:id` | Single department read |

### Internal routes (2) — `/api/v1/internal/*`, mesh-mTLS + NetworkPolicy only

| ID | Method & Path | Caller | Purpose |
|---|---|---|---|
| CAT-I1 | `GET /internal/departments` | `iam-org-membership`, Group Mapping Service (future) | Bulk read for consumer cache population (`om:departments`/`gm:departments`) |
| CAT-I2 | `GET /internal/plans` | `iam-org-membership` | Bulk read for consumer cache population (`om:plans`) |

### Operator routes (5) — `/api/v1/operator/*`, `platform_operator` only

| ID | Method & Path | Purpose |
|---|---|---|
| CAT-1 | `POST /operator/departments` | Create a department |
| CAT-2 | `PATCH /operator/departments/:id` | Rename / retire; optimistic-locked |
| CAT-3 | `DELETE /operator/departments/:id` | Always `405` — hard delete is blocked forever |
| CAT-4 | `GET /operator/plans[/:code]` | List or single-tier read |
| CAT-5 | `PATCH /operator/plans/:code` | PATCH-only — no create/delete, tier set is fixed |

CAT-I1/CAT-I2 are "mesh-only" by auth (`iam-system` role), not by path — unlike some other
services' internal routes, they still live under the shared `/api/v1` base path (full paths:
`GET /api/v1/internal/departments`, `GET /api/v1/internal/plans`), matching `main.go`'s actual
route nesting and the contract `iam-org-membership`'s `CatalogAdminClient` is already built
against.

Every write endpoint re-checks the `platform_operator` role inside the handler in addition to the
route-group middleware (defense-in-depth). Full request/response shapes: `internal/adapter/inbound/http/dto.go`
and the handler doc-comments in the same package (`@Summary`/`@Failure` swaggo annotations —
`make swag` regenerates the checked-in `docs/swagger/`, served live at `/swagger/*any`, see
`DOCS_ENABLED` below).

---

## Input validation

Domain-rule failures are raised as a `*domain.DomainError` wrapping one of the 16 sentinel error
values in `internal/core/domain/errors.go`; `HandleError`/`domainErrorStatus`
(`internal/adapter/inbound/http/middleware.go`) maps `DomainError.Code` to a frozen HTTP status. A
raw `*pgconn.PgError` that escapes the repository layer untranslated is classified by SQLSTATE
class — `08`/`53` via `pgcommon.IsConnectionException`/`IsInsufficientResources`, `57`/`58` via a
small local fallback (pgcommon has no dedicated helper for those two yet) — into `503
db_unavailable`; anything else falls back to `503 dependency_unavailable` and is logged.

```json
{
  "error": "optimistic_lock_conflict",
  "code": "optimistic_lock_conflict",
  "status": 409,
  "message": "record version conflict",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "request_id": "req_01hz...",
  "record_version": 4
}
```

`DomainError.Details` merges into the flat envelope at the top level (`record_version`, `field`,
`key`) rather than a nested `details` object — see LLD §17 for the complete error taxonomy.

### Notable validation rules

| Rule | Enforcement |
|---|---|
| `code`/`is_system` can never be changed via `PATCH` (CAT-2) | The handler pre-reads the raw JSON body and rejects either field with `422 field_immutable` *before* DTO binding would otherwise silently drop them |
| A system department (`is_system=true`) can never be retired or renamed | DB triggers (`prevent_system_department_name_change`, the `chk_system_department_active` CHECK) raise `422 system_name_immutable`/`422 system_department_cannot_be_retired` — a service-layer backstop for the same rule |
| A department is never hard-deletable | `DELETE` always returns `405 method_not_allowed`; the withheld `DELETE` grant and the `prevent_department_delete` trigger back this up even if the route check were ever bypassed |
| `feature_set` values must be scalar (string/bool/number/null) | A nested object/array returns `422 invalid_feature_value`, naming the offending `key` (PLAN-6(d)) |
| Optimistic locking | Both tables carry `record_version` (`touch_row` DB trigger); a version mismatch returns `409 optimistic_lock_conflict` with the current version in the body |
| `plans`' tier set is fixed to the `tenant_plan` ENUM | No create/delete API — a new tier is an ENUM migration plus a seed row, never a plain API call (PLAN-4) |

---

## Architecture

Hexagonal / ports-and-adapters, same layering as `iam-org-membership` scaled down to two
aggregates and no tenant context.

```
iam-catalog-admin/
├── cmd/
│   └── catalog-admin-config/          # Composition root: pool wiring, migrations, router registration, graceful shutdown
├── internal/
│   ├── core/
│   │   ├── domain/                    # Department, Plan, BrandingLevel, TenantPlan, DomainError catalogue — no external deps
│   │   ├── port/                      # DepartmentRepository, PlanRepository, Cache
│   │   └── service/                   # DepartmentService, PlanService — business logic + cat:* cache read-through
│   └── adapter/
│       ├── inbound/
│       │   └── http/                  # Gin handlers (CAT-*), DTOs, middleware, router.go, Swagger UI
│       └── outbound/
│           ├── postgres/              # Repository impls + single consolidated 000001_init_schema migration
│           ├── valkey/                # Cache adapter (go-redis/v9) — advisory only
│           └── metrics/               # catalog_admin_*-prefixed Prometheus counters/histogram
├── pkg/requestctx/                    # Typed RequestContext{UserID, TenantID, Roles}
├── docs/
│   ├── lld/                           # LLD v1.35 — §16 Decision Register + open-question register, §17 error taxonomy
│   └── swagger/                       # Generated REST contract (make swag) — not hand-authored
├── deploy/                            # Helm chart, monitoring alerts, SLO burn-rate rules
└── test/                              # test/unit (black-box), test/postgres (real Postgres), test/e2e, test/dbseed
```

See `ARCHITECTURE.md` for the full layer diagram, package dependency graph, request/write flows,
cache strategy, and the explicit list of things this service deliberately does **not** have (RLS,
tenant context, outbox, events) and why.

### Dependency rules (enforced by `go-arch-lint` in CI)

| Component | May depend on |
|---|---|
| `domain` | Nothing internal |
| `port` | `domain` only |
| `service` | `domain`, `port` only |
| `requestctx` | Nothing internal |
| `observability` (`adapter/outbound/metrics`) | Nothing internal — a documented cross-cutting leaf, imported by both the inbound HTTP adapter and the outbound Valkey adapter |
| `adapters_inbound`/`adapters_outbound` | `service`, `port`, `domain`, `requestctx`, `observability` |
| `docs_swagger` | Nothing internal |
| `cmd` | Everything above |

`go-arch-lint check --project-path .` currently passes clean with **zero violations**, run in
`validate-test.yml` on every PR (not part of `make ci` — see CI below).

### Storage

PostgreSQL only — no messaging, no cache-adjacent AWS service. No Row-Level Security: neither
table has a `tenant_id` column, so there is nothing to isolate. A single `catalog_admin_app` DB
role with `SELECT`/`INSERT`/`UPDATE` on both tables (no `DELETE` grant — hard delete is also
trigger-blocked, defense in depth). Advisory-only Valkey cache (`cat:departments`/`cat:plans`,
60s TTL) fronts Postgres for the two public read routes.

### Shared library dependencies

| Library | Version | Purpose |
|---|---|---|
| `platform-gincommon` | v1.3.0 | HTTP middleware, Zap logging, OTel tracing, Prometheus metrics |
| `platform-pgcommon` | v1.3.0 | pgx/v5 pool, migrations, structured error helpers (`IsConnectionException`/`IsInsufficientResources`/`IsPgError`) — used **without** the `GUCProvider` option, since there is no RLS/tenant GUC to bridge |

**Not used, deliberately:** `platform-events` (no outbox/SNS/SQS — this service publishes and
consumes no events, LLD §7).

---

## Integrating with other services

### 1. Prerequisites

Mesh mTLS reachability to this service; no auth beyond the gateway-header contract
(`x-user-id: iam-system`, `x-tenant-id: <any well-formed UUID>`, `x-tenant-roles: iam-system`) for
the two internal bulk endpoints. There is no JWT parsing in this service — the gateway/mesh has
already done it.

### 2. Core Org & Membership — the primary consumer

Core Org & Membership is the primary consumer, via an outbound client
(`internal/adapter/outbound/catalogadmin` in that repo) wrapped by a caching decorator
(`service.CatalogService`) that implements the read side with a 600s primary / 24h
stale-if-error two-tier cache. See `CACHE_DESIGN.md` for the TTL/invalidation rules a new
consumer should replicate.

- **Bulk read** — `GET /api/v1/internal/departments` / `GET /api/v1/internal/plans` (CAT-I1/CAT-I2).
  Cache the response for ~600s; on a live-call failure, fall back to your own longer-lived
  stale-if-error copy before ever failing an admin/JIT write path outright (LLD §9, CAT-FAIL-2).
- **Optimistic locking** — every write (CAT-2, CAT-5) takes `record_version` and returns `409
  optimistic_lock_conflict` with the current version on mismatch; re-fetch and retry.

### 3. Group Mapping Service — future, Wave 2

Once ADR-0007 Wave 2 ships, the Group Mapping Service will call CAT-I1 the same way Core does,
populating its own `gm:departments` cache with the identical primary/stale-if-error pattern —
there is no separate endpoint for it to integrate against.

### 4. Handling errors

Every non-2xx response is the flat envelope shown above. See
`docs/lld/iam-lld-catalog-admin-config-service.md` §17 for the complete error taxonomy. A `503
catalog_unavailable` a *consumer* returns means this service (or its own cache) had nothing to
serve at the time of the call — that code is emitted by the consumer, not by this service itself.

### 5. Rate limits

No generic per-caller/per-endpoint rate limiter exists in `internal/adapter/inbound/http/` —
inbound throttling, if any, is the gateway's concern. Writes are rare and operator-driven by
design, so there is no invite-style business-logic throttle the way `iam-org-membership` has for
P-6.

---

## Local development

### Prerequisites

- Go 1.26.6+ (must match `go.mod`)
- Docker (for Postgres + Valkey — `make docker-up`, and for the testcontainers-backed
  `test-postgres`/`test-e2e` tiers)
- `GOPRIVATE=github.com/BCBP-SOLUTIONS-FZC-LLC/*` for the private `platform-*` modules

### Setup

```sh
git clone https://github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin
cd iam-catalog-admin
make setup       # copy .env-example → .env; install .githooks/pre-commit (run once before anything else)
make docker-up   # start Postgres + Valkey
make run         # start the server on :8081 (metrics on :9090; kills the port first)
```

Local ports are offset from `iam-org-membership`'s own local stack (which uses `5534`/`6380`) so
both services can run side by side: Postgres on `5535`, Valkey on `6381`.

### Common commands

Run `make help` for the full list.

| Command | Description |
|---|---|
| `make setup` | Copy `.env-example` → `.env`; install `.githooks/pre-commit` |
| `make tidy` / `make fmt` / `make fmt-check` / `make vet` | Go basics; `fmt-check` mirrors CI, does not modify files |
| `make lint` | Alias for `make vet` — no `golangci-lint` config yet |
| `make mod-verify` | `go mod verify` |
| `make vuln-check` | `govulncheck ./internal/... ./pkg/...` |
| `make test-unit` | Black-box `test/unit` + white-box adapter tests — no Docker |
| `make test-postgres` | Real Postgres via testcontainers — migrations + repositories + triggers |
| `make test-e2e` | Full HTTP stack (real router + middleware) against real Postgres + Valkey |
| `make test-ci` | All three tiers with `-race` + merged coverage, as CI runs it |
| `make race` | Unit + white-box tests with `-race` |
| `make run` | Run the server locally (kills `APP_PORT` first) |
| `make build` | Compile to `bin/catalog-admin-config` |
| `make cover` / `make cover-func` | Merged unit+postgres+e2e coverage report (requires Docker) |
| `make ci` | `tidy` + `fmt-check` + `vet` + `lint` + `test-ci` + `build` |
| `make docker-up` / `make docker-down` | Start/stop Postgres + Valkey |
| `make swag` / `make swag-check` | Regenerate / verify freshness of the Swagger REST contract |
| `make install-hooks` | Install `.githooks/pre-commit`; also run by `make setup` |
| `make clean` | Remove `bin/`, `.coverage/`, coverage files |

### Running a single test

```sh
go test ./test/unit/... -run TestDepartmentService_Patch_OptimisticLockConflict -v
go test ./test/postgres/... -tags=integration -run TestPlanRepository_SeededTiersAndPatch -v
go test ./test/e2e/... -tags=e2e -run TestE2E023_PatchDepartment_SystemDeptCannotBeRetired -v
```

### Calling the API locally

```sh
BASE=http://localhost:8081
OP='-H "x-user-id: 11111111-1111-1111-1111-111111111111" -H "x-tenant-id: 22222222-2222-2222-2222-222222222222" -H "x-tenant-roles: platform_operator"'

curl -s $OP -X POST $BASE/api/v1/operator/departments \
  -H "Content-Type: application/json" -d '{"code":"LEGAL_OPS","name":"Legal Ops"}'

curl -s -H "x-user-id: 1" -H "x-tenant-id: 22222222-2222-2222-2222-222222222222" -H "x-tenant-roles: tenant_admin" \
  $BASE/api/v1/departments
```

### Developer tools

Interactive Swagger UI (custom BCBP theme) is served at `/swagger/*any`.

```bash
open http://localhost:8081/swagger/index.html
```

`DOCS_ENABLED`/`DOCS_AUTH_TOKEN` gate the docs surface in production (constant-time bearer-token
check); it is always mounted outside production.

---

## Testing domain events locally

**Not applicable.** This service publishes no events and consumes no events (LLD §7, invariants
CAT-EVT-1..5) — there is no outbox, no SNS topic, no SQS queue, and nothing to trigger or inspect
locally. A write becomes visible to consumers purely through the `cat:*`/`om:*`/`gm:*` cache-TTL
mechanism described in `CACHE_DESIGN.md`.

---

## Testing

Three tiers, matching `iam-org-membership`'s layout exactly (scaled down — no cross-service
integration tier, since this service has no events to test):

- **`test/unit/`** (package `unit_test`) — hand-rolled fakes implementing `port.DepartmentRepository`
  / `port.PlanRepository` / `port.Cache` directly, no mocking library, no DB.
- **`internal/**/*_test.go`** (white-box) — handler, adapter, domain, and helper tests that need
  package-private access (`isOperatorOrSystemErrorSQLState`, `requireOperator`, `withPool`, `envOr`, ...).
- **`test/postgres/`** (package `postgres_test`, tag `integration`) — real Postgres via
  testcontainers; doubles as the migration test (applies every migration from scratch every run).
- **`test/e2e/`** (package `e2e_test`, tag `e2e`) — boots the actual production router (same
  middleware chain, same route table as `main.go`) behind an `httptest.Server`, against a real
  Postgres + a real Valkey-protocol server (miniredis), and drives it with plain `net/http` calls
  carrying gateway-style headers.

Every test in `test/postgres`/`test/e2e` calls `t.Parallel()` — each spins up its own fully
isolated Postgres container (e2e also gets its own `miniredis` + `httptest.Server`), so they run
concurrently rather than paying the ~1s container-boot cost one test at a time. `test/postgres`
runs in ~8s and `test/e2e` in ~50s on a 10-CPU machine (down from ~31s/~220s before
parallelization).

### Canonical tests (do not break)

- **`test/e2e/roles_test.go`'s `TestRoles_AppRoleHasNoBYPASSRLS`** — queries `pg_roles.rolbypassrls`
  against a real Postgres and fails the build if `catalog_admin_app` ever holds it, closing the one
  privilege-escalation path a role-grant regression could open.
- **`internal/adapter/outbound/postgres/repo_whitebox_test.go`'s `TestDeptUpdateFromTx_OCCConflict`
  / `TestPlanUpdateFromTx_OCCConflict`** — exercise the exact optimistic-lock probe path `Update()`
  runs in production (both functions are called directly by `Update()`, not duplicated inline), so
  a regression here is a regression in the shipped code path, not just a copy of it.

### Coverage

`.github/scripts/coverage-gate.sh` reads `go tool cover -func=coverage.out`'s total and fails below
`COVERAGE_THRESHOLD` (pinned to **98%** in `validate-test.yml` — this service's own established
baseline). Coverage is measured over `./internal/...` and `./pkg/...`, merged across the
unit/postgres/e2e suites via `scripts/merge_coverage.py` (max-count strategy). The current merged
total is **100.0%**, comfortably above the enforced floor. `cmd/catalog-admin-config/main.go` is
verified by the e2e tier's actual server boot, not unit coverage.

---

## Environment variables

The service will not start without the variables marked **required**
(`cmd/catalog-admin-config/main.go::validateRequiredEnv`).

| Variable | Example | Default | Purpose |
|---|---|---|---|
| `APP_ENV` | `production` | `dev` | `dev` relaxes prod-only checks (e.g. `rediss://` requirement) |
| `APP_NAME` | `catalog-admin-config` | `catalog-admin-config` | Prometheus label, OTel service name |
| `APP_PORT` | `8081` | `8081` | HTTP listen port (API) |
| `METRICS_PORT` | `9090` | `9090` | `/metrics` listen port — a separate `http.Server` from `APP_PORT`, so a NetworkPolicy can grant Prometheus scrape access without also granting API access |
| `BUILD_VERSION` | `v1.0.0-abc123` | `dev` | CI-injected build tag |
| `CATALOG_TTL_SECONDS` | `60` | `60` | `cat:departments`/`cat:plans` cache TTL (LLD §6/§12) |
| `DATABASE_URL` | `postgres://...` | — | Full DSN; overrides individual `PG_*` vars. **Required** unless `PG_HOST`+`PG_USER`+`PG_PASSWORD` are all set |
| `PG_HOST`/`PG_PORT`/`PG_USER`/`PG_PASSWORD`/`PG_DBNAME`/`PG_SSLMODE` | — | `localhost`/`5432`/—/—/`catalog_admin`/`require` | DSN parts, used when `DATABASE_URL` is unset |
| `PG_MAX_CONNS`/`PG_MIN_CONNS` | `10`/`0` | `10`/`0` | Pool sizing |
| `PG_BOUNCER_MODE` | `true` | `false` | Set when fronted by PgBouncer transaction pooling |
| `MIGRATION_DATABASE_URL` | ... | — | **Required if** `PG_BOUNCER_MODE=true` (and `DATABASE_URL` is unset) — migrations must bypass PgBouncer (session-scoped advisory lock) |
| `VALKEY_URL` | `rediss://...` | — | **Required.** Cache endpoint; must be `rediss://` in `production`/`staging` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | — | Provider always installed; OTLP export is a no-op until set |
| `DOCS_ENABLED` | `true` | `false` | Toggles the Swagger UI at `/swagger/*any` (`make swag` regenerates `docs/swagger/` from handler annotations). Active by default outside `production`; in `production` it takes `DOCS_ENABLED=true` and is bearer-token-gated by `DOCS_AUTH_TOKEN` (a warning is logged if enabled there without one) |
| `DOCS_AUTH_TOKEN` | `s3cr3t` | — | Bearer token required to reach `/swagger/*any` when `DOCS_ENABLED=true` in `production` |

---

## Security

| Topic | Guidance |
|---|---|
| **No RLS, no tenant context anywhere** | Neither table has a `tenant_id`; there is nothing to isolate. `IdentityBridgeMiddleware` parses the gateway-injected identity for role checks only — it does not bridge into any Postgres GUC (contrast with `iam-org-membership`, which does) |
| **Write authorization is a service-layer role check, not a DB mechanism** | `RequireOperatorRole`/`RequireSystemRole` middleware plus an in-handler re-check on every write and every internal route (defense-in-depth) |
| **Cache poisoning surface is bounded** | Valkey is advisory-only (CAT-FAIL-1) — even a compromised `cat:departments`/`cat:plans` entry propagates no further than any other stale-data scenario every consumer already tolerates |
| **No Keycloak credential of any kind** | This service never calls the Keycloak Admin API — it has no admin-API dependency of any kind |
| **No PII** | Neither table has ever held personal data |

---

## Observability

**SLOs.** CAT-I1/CAT-I2 (the two routes real consumers depend on): **≤30 ms p99**. CAT-6/CAT-7 (public reads): **15 ms p99** cache hit / **40 ms p99** cache miss. CAT-1/CAT-2/CAT-4/CAT-5 (writes): **100 ms p99**.

### Metrics

`catalog_admin_`-prefixed (`internal/adapter/outbound/metrics/metrics.go`), matching the fleet's
`{service-name}_*` convention (e.g. `iam-tender-acl`'s `tender_acl_*`):
`catalog_admin_requests_total{route,status}` / `catalog_admin_request_duration_seconds{route}`
(this service's own business-level request rollup, a **Histogram** — LLD §11.2, CAT-D13 §16 —
feeding both the §11.1 SLOs above and `deploy/monitoring/slo-rules.yml`'s burn-rate alerts),
`catalog_admin_writes_total{table,op}`, `catalog_admin_optimistic_lock_conflicts_total{table}`
(feeds the §11.5 alert), `catalog_admin_cache_hits_total{key}` /
`catalog_admin_cache_misses_total{key}`, plus the generic HTTP request metrics
(`http_requests_total`/`http_request_duration_seconds`) `platform-gincommon` registers
automatically for every route, and `pgcommon_*` pool/query instruments.

### Tracing and logs

`gincommon.InitTracingFromEnv()` (always installed; OTLP export is a no-op until
`OTEL_EXPORTER_OTLP_ENDPOINT` is set), plus a `db.query` span per query via `NewOTelTracer`.
Structured logs (Zap) via `platform-gincommon/pkg/logger`.

---

## Deployment

### Container image

Single binary (`bin/catalog-admin-config`), no second process — this service has nothing to
reconcile, unlike `iam-org-membership`'s `cmd/reconciler`.

```sh
make build   # bin/catalog-admin-config
```

Two-stage `Dockerfile`: `golang:1.26.6-alpine` builder (base image pinned to a SHA digest), runtime
is `gcr.io/distroless/static-debian12:nonroot` (no shell, non-root UID 65532) — only the one
compiled binary is copied in. `EXPOSE 8081 9090`.

### Helm chart

`deploy/helm/` renders one `Deployment` — no `CronJob`, unlike `iam-org-membership`'s seven. HPA:
`minReplicas: 2` / `maxReplicas: 4`, CPU 70% / memory 75%. PDB `minAvailable: 1`. Resources: CPU
`100m`/`500m`, Memory `128Mi`/`256Mi`. `terminationGracePeriodSeconds: 45` (30s HTTP drain + 15s
buffer — no outbox to drain). A writable `/tmp` `emptyDir` is mounted despite
`readOnlyRootFilesystem: true`, since anything in the process or its dependencies that assumes a
writable temp dir would otherwise fail at the syscall.

### Migration safety

Since this service has never been deployed, the schema is one consolidated `000001_init_schema`
migration (`internal/adapter/outbound/postgres/migrations/`) rather than an incremental history
with dead expand/contract steps to carry forward. `RunMigrations` runs under its own 60s timeout so
a wedged advisory lock from a prior crashed pod fails fast with a diagnosable log line.

---

## CI

Five workflow files:

- **`ci.yml`** — orchestrator. Runs `validate-test.yml` and `validate-quality.yml` in parallel with
  a Docker build (Hadolint → Buildx cached build → Trivy CVE scan). On push to `main`: builds+pushes
  to GHCR, signed keylessly via Cosign.
- **`validate-test.yml`** (reusable) — `make test-ci` (unit + postgres + e2e, `-race`, merged
  coverage) → coverage threshold gate (**98%**, pinned explicitly) → `go-arch-lint` (currently
  clean) → Swagger staleness check (`check-swagger-stale.sh`).
- **`validate-quality.yml`** (reusable) — `go mod verify` → `gofmt` check → `go mod tidy` drift
  check → `go vet` → `govulncheck`.
- **`changelog-check.yml`** — fails a PR touching `internal/`, `deploy/`, or `cmd/` without a
  `CHANGELOG.md` update.
- **`release.yml`** — tag-triggered release pipeline: re-validate → build+cross-compile → Docker
  build/push/sign → **non-skippable** deploy-gate (live Helm deploy + 2-minute error-rate check) →
  GitHub Release publish. See [VERSIONING.md](VERSIONING.md) for the full maintainer process.

**Required GitHub Actions repository secret: `GO_PRIVATE_TOKEN`** — every workflow that runs
`go mod download` needs it to fetch the private `platform-gincommon`/`platform-pgcommon` modules.

---

## Docker

### What the bundled `docker-compose.yml` starts

| Container | Image | Host port(s) | Purpose |
|---|---|---|---|
| `postgres` | `postgres:16-alpine` | `5535 → 5432` | Primary store |
| `valkey` | `valkey/valkey:8-alpine` | `6381 → 6379` | Advisory cache |

Unlike `iam-org-membership`'s compose file, there is no `app` service here at all — local dev
always runs the service itself via `make run`, not `docker compose up`, since there's no PgBouncer
or event-bus emulator dependency this compose file would otherwise need to also provision. Host
ports are deliberately offset from sibling services so multiple stacks can run side by side.

### Building the service image

```bash
docker build -t iam-catalog-admin:local --secret id=go_private_token,src=<(echo "$GO_PRIVATE_TOKEN") .
```

### Health and readiness

| Endpoint | Returns | Checks |
|---|---|---|
| `GET /healthz` | `200 {"status":"ok"}` | Pure liveness — never inspects a dependency |
| `GET /readyz` | `200`/`503` | Postgres pool and Valkey, each via its own `Health(ctx)` — fails on a Valkey-only outage too (CAT-D11), pulling the pod from rotation before every cache miss compounds DB load |

### Minimum required environment variables

```bash
# PostgreSQL
PG_HOST=localhost
PG_USER=catalog_admin_app
PG_PASSWORD=<password>
PG_DBNAME=catalog_admin

# Valkey
VALKEY_URL=localhost:6381
```

---

## Cross-service dependencies

This service has no synchronous outbound dependency of its own — it is a pure leaf. Every row
below describes the posture **its consumers** take on a call *to* this service, not a call this
service makes.

| Operation | Caller | Posture | On failure |
|---|---|---|---|
| `GET /api/v1/internal/plans` (CAT-I2) | `iam-org-membership` (I-8 projection layer) | cache + last-known-good | Warm cache → zero impact; cold + this service down → Core serves `om:plans:stale`, never blocks I-8 |
| `GET /api/v1/internal/departments` (CAT-I1) | `iam-org-membership` (admin/JIT writes) | cache + last-known-good | Admin/JIT write path degrades; never on I-8 |
| `GET /api/v1/internal/departments` (CAT-I1) | Group Mapping Service (future) | cache + last-known-good | Same posture as Core's own use |
| CAT-1..5 (operator writes) | Operator tooling | human-driven, low frequency | N/A — no downstream to degrade |

---

## Out of scope

| Concern | Where it lives |
|---|---|
| Credentials, password policy, MFA enforcement, JWT issuance | Keycloak |
| The per-tenant feature-flag override delta (`tenants.feature_flags`) and its merge with this service's baseline | Core Org & Membership |
| Pricing / currency | Billing Service |
| Usage metering against this service's own entitlement ceilings | Usage & Metering Service |
| I-8 hot-path entitlement resolution | AuthZ Enrichment, via Core |
| Tenant-facing department/plan *management* (`GET /api/v1/tenants/:id/departments`) | Core Org & Membership |
| Event publishing of any kind | Nobody — this service publishes and consumes no events (LLD §7) |

---

## Contributing

Small enough that there's no separate `CONTRIBUTING.md` yet. The short version: add a migration
under `internal/adapter/outbound/postgres/migrations/` for schema changes, keep the
domain/port/service/adapter layering (no service importing an adapter package), and add tests in
the same tier the equivalent existing test lives in (see **Testing** above) before opening a PR.

| Document | Description |
|---|---|
| [`.claude/CLAUDE.md`](.claude/CLAUDE.md) | Top-level guidance for Claude Code working in this repo |
| [`.claude/database.md`](.claude/database.md) | Tables, roles/grants, migrations, triggers |
| [`.claude/api-and-events.md`](.claude/api-and-events.md) | Endpoints, cache keys, why this service has no events |
| [`.claude/flows-and-concurrency.md`](.claude/flows-and-concurrency.md) | Write flows, optimistic locking, the Wave-1 migration history |
| [`.claude/operations.md`](.claude/operations.md) | Security, observability, configuration, CI/CD |
| [`.claude/development-guide.md`](.claude/development-guide.md) | Design decisions, extending, workflow, troubleshooting, error codes |
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | Layer model, request/write flows, cache strategy, threat model, key invariants |
| [`VERSIONING.md`](VERSIONING.md) | SemVer scope, release process, compatibility matrix |
| [`CACHE_DESIGN.md`](CACHE_DESIGN.md) | Cache keys, TTLs, invalidation |
| [`docs/lld/iam-lld-catalog-admin-config-service.md`](docs/lld/iam-lld-catalog-admin-config-service.md) | Full LLD v1.35 — §16 Decision Register + open-question register, §17 error taxonomy, §19 migration strategy |

---

## License / ownership

Internal BCBP Solutions FZC LLC service. Owned by the platform/billing-admin team (ADR-0007's
team-fit rationale — low pager load, operator-driven write volume).
