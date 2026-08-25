# iam-catalog-admin

Catalog / Admin Config Service — the authoritative system of record for the platform's two
**global, non-tenant-scoped** reference catalogs: the department catalog and the plan entitlement
catalog. Extracted from Org & Membership (`iam-org-membership`) per ADR-0007 Wave 1. Full design:
`catalog-admin-config-service-lld.md`.

## Mental model

Two tables, no tenants, no events. `departments` is the operator-managed list of departments a
tenant may activate (Engineering, Design, Procurement, Finance, Legal, plus operator-added
entries). `plans` is the per-tier baseline entitlement catalog (`starter`/`pro`/`enterprise`) that
every tenant's *effective* feature set is computed against — this service owns the baseline only;
Core Org & Membership owns the per-tenant override delta (`tenants.feature_flags`) and performs the
merge at its own I-8 read time. This service never sees tenant data and is not on any hot path —
writes are rare and operator-driven; reads are cached and served from a snapshot that is always
allowed to be briefly stale (LLD §8, §11).

## Why this service exists

`iam-org-membership` originally owned both tables directly. ADR-0007 splits three low-risk,
tenant-independent concerns out of that service into their own leaf services; this is Wave 1, the
lowest-risk cut — neither table carries a `tenant_id`, neither is RLS-protected, and neither
participates in Org & Membership's I-8 hot-path SQL join. The seven-phase rollout this extraction
followed is complete (LLD §12/§16) — this service is O&M's sole system of record for both tables.

## API overview

All endpoints require `x-user-id` and `x-tenant-id` headers injected by the API gateway (or the
mesh for internal calls) — the same trust model every other IAM service uses. Base path: `/api/v1`.

| ID | Method & path | Auth | Notes |
|---|---|---|---|
| CAT-1 | `POST /operator/departments` | `platform_operator` | Create a department |
| CAT-2 | `PATCH /operator/departments/:id` | `platform_operator` | Rename / retire; optimistic-locked |
| CAT-3 | `DELETE /operator/departments/:id` | `platform_operator` | Always `405` — hard delete is blocked forever |
| CAT-4 | `GET /operator/plans[/:code]` | `platform_operator` | List or single-tier read |
| CAT-5 | `PATCH /operator/plans/:code` | `platform_operator` | PATCH-only — no create/delete, tier set is fixed |
| CAT-6 | `GET /departments` | any authenticated caller | Public catalog listing |
| CAT-7 | `GET /departments/:id` | any authenticated caller | Public single-department read |
| CAT-I1 | `GET /internal/departments` | mesh-only (`iam-system`) | Bulk read for consumer cache population |
| CAT-I2 | `GET /internal/plans` | mesh-only (`iam-system`) | Bulk read for consumer cache population |

CAT-I1/CAT-I2 are "mesh-only" by auth (`iam-system` role), not by path — unlike some other
services' internal routes, they still live under the shared `/api/v1` base path (full paths:
`GET /api/v1/internal/departments`, `GET /api/v1/internal/plans`), matching `main.go`'s actual
route nesting and the contract `iam-org-membership`'s `CatalogAdminClient` is already built
against. See §2 below for the fully-qualified paths a mesh caller should use.

Every write endpoint re-checks the `platform_operator` role inside the handler in addition to the
route-group middleware (defense-in-depth, mirrors O&M's AUTH-6 pattern). Full request/response
shapes and error codes: `internal/adapter/inbound/http/dto.go` and the handler doc-comments in the
same package (`@Summary`/`@Failure` swaggo annotations — `make swag` regenerates the checked-in
`docs/swagger/`, served live at `/swagger/*any`, see `DOCS_ENABLED` below).

## Architecture

Hexagonal / ports-and-adapters, same layering as `iam-org-membership` scaled down to two
aggregates:

```
cmd/catalog-admin-config/main.go     composition root
internal/core/domain/                Department, Plan, DomainError catalogue
internal/core/port/                  DepartmentRepository, PlanRepository, Cache
internal/core/service/               DepartmentService, PlanService — business logic + caching
internal/adapter/inbound/http/       Gin handlers, DTOs, middleware, error model
internal/adapter/outbound/postgres/  repositories + migrations
internal/adapter/outbound/valkey/    Cache implementation
internal/adapter/outbound/metrics/   Prometheus (catalog_admin_* prefix)
pkg/requestctx/                      gateway-identity → role-check helper
```

See `ARCHITECTURE.md` for the full layer diagram, request lifecycle, cache design, and the
explicit list of things this service deliberately does **not** have (RLS, tenant context, outbox,
events) and why.

### Dependency rule

This service is a pure leaf: it makes no outbound synchronous call to any other IAM service. Every
arrow into it is a caller (operator tooling, Core Org & Membership, the Group Mapping Service).

### Storage

PostgreSQL only. No Row-Level Security — neither table has a `tenant_id` column, so there is
nothing to isolate. A single `catalog_admin_app` DB role with `SELECT`/`INSERT`/`UPDATE` on both
tables (no `DELETE` grant — hard delete is also trigger-blocked, defense in depth).

### Shared library dependencies

Same technology baseline as every other IAM service: `platform-gincommon` (middleware,
observability, identity-header parsing) and `platform-pgcommon` (pooled Postgres, migrations,
transactions). **Not used**: `platform-events` (no outbox/SNS/SQS — this service publishes and
consumes no events, LLD §10).

## Integrating with other services

Core Org & Membership is the primary consumer, via a new outbound client
(`internal/adapter/outbound/catalogadmin` in that repo) wrapped by a caching decorator
(`service.CatalogService`) that implements the read side with a 600s primary / 24h
stale-if-error two-tier cache. See `CACHE_DESIGN.md` for the TTL/invalidation rules a new
consumer should replicate.

1. **Prerequisites** — mesh mTLS reachability to this service; no auth beyond the gateway-header
   contract (`x-user-id: iam-system`, `x-tenant-id: <any well-formed UUID>`, `x-tenant-roles:
   iam-system`) for the two internal bulk endpoints.
2. **Bulk read** — `GET /api/v1/internal/departments` / `GET /api/v1/internal/plans` (CAT-I1/CAT-I2).
   Cache the response for ~600s; on a live-call failure, fall back to your own longer-lived
   stale-if-error copy before ever failing an admin/JIT write path outright (LLD §11, CAT-FAIL-2).
3. **Handling errors** — `503 catalog_unavailable` means this service (or its own cache)
   had nothing to serve; treat it exactly like any other dependency outage, never as a signal to
   guess at department/plan data.
4. **Optimistic locking** — every write (CAT-2, CAT-5) takes `record_version` and returns `409
   optimistic_lock_conflict` with the current version on mismatch; re-fetch and retry.

## Local development

### Prerequisites

- Go 1.26.6+ (must match `go.mod`)
- Docker (for Postgres + Valkey — `make docker-up`, and for the testcontainers-backed
  `test-postgres`/`test-e2e` tiers)
- `GOPRIVATE=github.com/BCBP-SOLUTIONS-FZC-LLC/*` for the private `platform-*` modules

### Setup

```sh
make setup       # copy .env-example → .env; install .githooks/pre-commit (run once before anything else)
make docker-up   # start Postgres + Valkey
make run         # start the server on :8081
```

Local ports are offset from `iam-org-membership`'s own local stack (which uses `5533`/`6380`) so
both services can run side by side: Postgres on `5535`, Valkey on `6381`.

### Common commands

Run `make help` for the full list.

| Command | Description |
|---|---|
| `make test-unit` | Black-box `test/unit` + white-box adapter tests — no Docker |
| `make test-postgres` | Real Postgres via testcontainers — migrations + repositories + triggers |
| `make test-e2e` | Full HTTP stack (real router + middleware) against real Postgres + Valkey |
| `make test-ci` | All three tiers with coverage, as CI runs it |
| `make cover` / `make cover-func` | Merged unit+postgres+e2e coverage report (requires Docker) |
| `make swag` | Regenerate `docs/swagger/` from handler annotations |
| `make swag-check` | Fail if `make swag` would change `docs/swagger/` (drift check; not yet wired into CI) |
| `make install-hooks` | Install `.githooks/pre-commit` (tidy + fmt-check + lint); also run by `make setup` |

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

## Testing

Three tiers, matching `iam-org-membership`'s layout exactly (scaled down — no cross-service
integration tier, since this service has no events to test):

- **`test/unit/`** (package `unit_test`) — hand-rolled fakes implementing `port.DepartmentRepository`
  / `port.PlanRepository` / `port.Cache` directly, no mocking library, no DB.
- **`internal/**/*_test.go`** (white-box) — handler, adapter, domain, and helper tests that need
  package-private access (`isDBUnavailableSQLState`, `requireOperator`, `withPool`, `envOr`, ...).
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

### Coverage

≥98% merged coverage across the unit+postgres+e2e tiers (CI gate: 98%, currently 99.6% —
verified via `make cover-func`). `cmd/catalog-admin-config/main.go` is verified by the e2e
tier's actual server boot, not unit coverage.

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
| `CATALOG_TTL_SECONDS` | `60` | `60` | `cat:departments`/`cat:plans` cache TTL (LLD §8/§15) |
| `DATABASE_URL` | `postgres://...` | — | Full DSN; overrides individual `PG_*` vars. **Required** unless `PG_HOST`+`PG_USER`+`PG_PASSWORD` are all set |
| `PG_HOST`/`PG_PORT`/`PG_USER`/`PG_PASSWORD`/`PG_DBNAME`/`PG_SSLMODE` | — | `localhost`/`5432`/—/—/`catalog_admin`/`require` | DSN parts, used when `DATABASE_URL` is unset |
| `PG_MAX_CONNS`/`PG_MIN_CONNS` | `10`/`0` | `10`/`0` | Pool sizing |
| `PG_BOUNCER_MODE` | `true` | `false` | Set when fronted by PgBouncer transaction pooling |
| `MIGRATION_DATABASE_URL` | ... | — | **Required if** `PG_BOUNCER_MODE=true` — migrations must bypass PgBouncer (session-scoped advisory lock) |
| `VALKEY_URL` | `rediss://...` | — | **Required.** Cache endpoint; must be `rediss://` in `production`/`staging` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | — | Opt-in tracing; tracing is a no-op if unset |
| `DOCS_ENABLED` | `true` | `false` | Toggles the Swagger UI at `/swagger/*any` (`make swag` regenerates `docs/swagger/` from handler annotations). Active by default outside `production`; in `production` it takes `DOCS_ENABLED=true` and is bearer-token-gated by `DOCS_AUTH_TOKEN` (a warning is logged if enabled there without one) |
| `DOCS_AUTH_TOKEN` | `s3cr3t` | — | Bearer token required to reach `/swagger/*any` when `DOCS_ENABLED=true` in `production` |

## Security

- **No RLS, no tenant context anywhere.** Neither table has a `tenant_id`; there is nothing to
  isolate. `IdentityBridgeMiddleware` parses the gateway-injected identity for role checks only —
  it does not bridge into any Postgres GUC (contrast with `iam-org-membership`, which does).
- **Write authorization is a service-layer role check**, not a DB mechanism —
  `RequireOperatorRole`/`RequireSystemRole` middleware plus an in-handler re-check on every write
  and every internal route (defense-in-depth).
- **No PII.** Neither table has ever held personal data.

## Observability

- **Metrics** (`catalog_admin_*` prefix, matching the fleet's `{service-name}_*` convention, e.g.
  `iam-tender-acl`'s `tender_acl_*`): `catalog_admin_requests_total{route,status}` /
  `catalog_admin_request_duration_seconds{route,quantile}` (this service's own business-level
  request rollup, LLD §13.2), `catalog_admin_writes_total{table,op}`,
  `catalog_admin_optimistic_lock_conflicts_total{table}` (feeds the §13.5 alert),
  `catalog_admin_cache_hits_total{key}` / `catalog_admin_cache_misses_total{key}`, plus the generic
  HTTP request metrics (`http_requests_total`/`http_request_duration_seconds`) `platform-gincommon`
  registers automatically for every route.
- **Tracing**: OTel, opt-in via `OTEL_EXPORTER_OTLP_ENDPOINT`.
- **Logs**: structured (Zap) via `platform-gincommon/pkg/logger`.
- **Health**: `/healthz` (always 200), `/readyz` (fails if Postgres or Valkey is down).

## Deployment

Single binary, no second process (no reconciler, no batch jobs — this service has nothing to
reconcile). `Dockerfile` builds a distroless image; `docker-compose.yml` is for local dev only.

```sh
make build   # bin/catalog-admin-config
```

## Cross-service dependencies

| Direction | Service | Nature |
|---|---|---|
| Inbound | Operator tooling | CAT-1/2/3/4/5, human-driven, low frequency |
| Inbound | `iam-org-membership` | CAT-I1/CAT-I2, cache-population on a miss (`om:departments`/`om:plans`) |
| Inbound | Group Mapping Service (future, Wave 2) | CAT-I1, same pattern (`gm:departments`) |
| Outbound | *(none)* | This service is a pure leaf — see LLD §4 |

## Out of scope

Explicitly not this service's concern (LLD §3): pricing/billing data, usage metering, the
per-tenant feature-flag override delta (`tenants.feature_flags` stays in Core), and any event
publishing (this service publishes and consumes no events, LLD §10).

## Contributing

Small enough that there's no separate `CONTRIBUTING.md` yet. The short version: add a migration
under `internal/adapter/outbound/postgres/migrations/` for schema changes, keep the
domain/port/service/adapter layering (no service importing an adapter package), and add tests in
the same tier the equivalent existing test lives in (see **Testing** above) before opening a PR.

## License / ownership

Internal BCBP Solutions FZC LLC service. Owned by the platform/billing-admin team (ADR-0007's
team-fit rationale — low pager load, operator-driven write volume).

## Docs

- `ARCHITECTURE.md` — layers, request lifecycle, cache design, data model, what's deliberately absent
- `CACHE_DESIGN.md` — cache keys, TTLs, invalidation
- `CHANGELOG.md` — Keep a Changelog format; check `[Unreleased]` first for the latest fixes/audits
- `docs/lld/iam-lld-catalog-admin-config-service.md` — the authoritative LLD (decision register,
  full error taxonomy, revision history covering the now-complete Wave-1 extraction/cutover)
