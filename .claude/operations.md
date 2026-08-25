# Security

## No RLS, no tenant isolation to enforce

Neither `departments` nor `plans` carries a `tenant_id` — there is no row-level security policy,
no GUC to inject, and no cross-tenant leakage risk to reason about for these two tables. This is
the single biggest structural difference from every RLS-scoped sibling service.

## Trust boundary

- Trusts `x-user-id`/`x-tenant-id`/`x-tenant-roles` headers because mTLS guarantees they
  originate from Envoy after JWT validation (or from the mesh directly for `iam-system` calls).
  No JWT parsing in this service.
- `x-tenant-id` is required and 401-enforced on every route even though it's never read again
  after parsing (CAT-D12) — platform-wide header-contract uniformity, not a functional need here.
- Internal routes (`/api/v1/internal/*`) are additionally gated by Kubernetes NetworkPolicy —
  `RequireSystemRole` (the `iam-system` role check) is defense-in-depth on top of that, not the
  primary boundary.

## Write authorization is a service-layer check, not a DB mechanism

Every write route (CAT-1, CAT-2, CAT-5) re-checks `rc.Roles` for `platform_operator` **inside the
handler** in addition to the `RequireOperatorRole` route-group middleware — mirrors the source
LLD's OP-1/OP-6/OP-7 enforcement pattern. The DB grants `SELECT` broadly (mesh-readable) and
restricts `INSERT`/`UPDATE` to the single `catalog_admin_app` role (no migrator/app split — see
[database.md](database.md#roles-lld-9)). No role is granted `DELETE` at all on either table —
hard-delete is trigger-blocked regardless (D-4/OP-3), and the grant is withheld too, in defense in
depth.

**CI verifies `NOBYPASSRLS` directly against a real Postgres** —
`TestRoles_AppRoleHasNoBYPASSRLS` (`test/e2e/roles_test.go`).

## No PII

Neither table has ever held personal data — this service's GDPR posture is "there is none to
have." See §18.1–18.4 of the LLD for the full (short) treatment.

# Observability

## SLOs (LLD §13.1)

| Endpoint | Target |
|---|---|
| `GET /departments[/:id]` (CAT-6/7), cache hit | 15 ms p99 |
| `GET /departments[/:id]` (CAT-6/7), cache miss | 40 ms p99 |
| `GET /internal/departments` (CAT-I1) | ≤30 ms p99 |
| `GET /internal/plans` (CAT-I2) | ≤30 ms p99 |
| `POST /operator/departments` (CAT-1) / `PATCH .../:id` (CAT-2) | 100 ms p99 |
| `GET`/`PATCH /operator/plans[/:code]` (CAT-4/5) | 100 ms p99 |
| Availability | 99.9% monthly |

These figures are this LLD's own proposal (no direct precedent in the source LLD for the
operator-facing endpoints) and are tracked as an open confirmation item (CAT-Q5, LLD §19) rather
than a fully agreed contract.

## Metrics

All `catalog_admin_*` collectors are registered onto `gincommon.MetricsRegisterer()` with
`gincommon.MetricsConstLabels()` (same registry/labels as `http_requests_total`), via
`metrics.Register` (`internal/adapter/outbound/metrics/metrics.go`) — idempotent via `sync.Once`.

- `catalog_admin_requests_total{route, status}` — every CAT-1 through CAT-I2 call's terminal
  outcome, recorded by `requestMetricsMiddleware` (`router.go`) scoped to the `/api/v1` group.
  `route` is `c.FullPath()` (the matched route template), never a raw path, so cardinality stays
  bounded regardless of how many UUIDs are requested.
- `catalog_admin_request_duration_seconds{route}` — a **Summary** (quantiles `0.5`/`0.9`/`0.99`),
  not a Histogram, because the LLD names the label `quantile` specifically. Feeds the SLOs above
  directly.
- `catalog_admin_writes_total{table, op}` — `departments`/`plans` inserts and updates. **Known
  quirk:** both the repository method and the handler increment this counter on the same
  successful write (see [flows-and-concurrency.md](flows-and-concurrency.md)), so its absolute
  value is ~2× the actual write count — treat it as a rate/trend signal.
- `catalog_admin_cache_hits_total{key}` / `catalog_admin_cache_misses_total{key}` — this
  service's own `cat:departments`/`cat:plans` cache, instrumented at the Valkey adapter layer (not
  the core service layer, to keep the hexagonal boundary intact). Shipped as two counters, not a
  ratio gauge — derive hit ratio via
  `sum(rate(catalog_admin_cache_hits_total[5m])) / (sum(rate(catalog_admin_cache_hits_total[5m])) + sum(rate(catalog_admin_cache_misses_total[5m])))`.
  A real Valkey error (not a plain miss) is excluded from this metric — `/readyz`'s cache check is
  the signal for Valkey actually being down.
- `catalog_admin_optimistic_lock_conflicts_total{table}` — CAT-2/CAT-5's `409` rate. A sustained
  nonzero rate is itself diagnostic of a caller retry-storm or tooling bug — writes are rare
  enough in steady state that any sustained rate is unexpected.
- Generic HTTP metrics from `gincommon.ObservabilityMiddlewares` (`http_requests_total`,
  `http_request_duration_seconds`) — shared shape across the whole IAM fleet, distinct from (and
  in addition to) the `catalog_admin_*` metrics above.

**Not emitted by this service, but relevant to full observability:** `om:departments`/`om:plans`/
`gm:departments` cache-miss rate and stale-if-error activation count are Core's and Group Mapping
Service's own metrics — don't look for them on this service's `/metrics`.

`/metrics` is served on a **dedicated port** (`METRICS_PORT`, default `9090`), separate from the
API port (`APP_PORT`, default `8081`) — so a NetworkPolicy can grant the monitoring namespace
scrape access without also granting API access.

## Tracing and logging

OTel Go SDK, W3C Trace Context (`gincommon.InitTracingFromEnv()`, opt-in via
`OTEL_EXPORTER_OTLP_ENDPOINT`). A typical trace: `inbound.http → core.service.<use_case> →
outbound.postgres → outbound.valkey (cat:* read/DEL)` — no `outbound.*` call to any other IAM
service (this is a pure leaf). Structured JSON logs carry `trace_id`, `request_id`, `tenant_id`
(logged on **every** request line, not just writes — the header is required but its parsed value
is never used again, CAT-D12/LLD §13.3), and the `platform_operator` principal's `sub` for every
CAT-1/CAT-2/CAT-5 write.

## Dashboards

A "Catalog / Admin Config" Grafana folder:

- **Requests & Writes** — the only dashboard this repo actually ships:
  `deploy/monitoring/dashboards/catalog-admin-requests-writes.json`. Request rate/latency for
  CAT-1 through CAT-I2 by route, cache hit ratio, optimistic-lock-conflict rate.
- **Consumer Health** (cross-service, sourced from Core's and Group Mapping's own metrics — not
  built here) — `om:*`/`gm:*` cache-miss rate and stale-if-error activation count; the signal that
  this service is degraded from a consumer's perspective even when every panel above looks
  healthy.
- **Migration Soak** (Wave-1 only) — retired; the Contract step has completed and the `410 Gone`
  routes it would have watched no longer exist in Core at all.

## Alerts (`deploy/monitoring/app-alerts.yml` / `prometheusrule.yaml`)

| Condition | Severity | Alert name |
|---|---|---|
| `/readyz` failing (via scrape-absence, `absent(up==1)` for 2m) | critical (SEV-2) | `IAMCatalogAdminDown` |
| Optimistic-lock-conflict rate sustained > 0 for > 15 min | warning (SEV-3) | `IAMCatalogAdminOptimisticLockConflicts` |
| CAT-I1/CAT-I2 error rate > 10% over 5 min | critical (SEV-2) | `IAMCatalogAdminInternalErrorRate` |
| Generic 5%/20% error rate on all routes | warning/critical | `IAMCatalogAdminHighErrorRate` / `CriticalErrorRate` |
| p99 latency over 250 ms / 1 s on all routes | warning/critical | `IAMCatalogAdminHighLatency` / `CriticalLatency` |
| Fewer than 2 pods reporting `up` for 5 min | warning | `IAMCatalogAdminReplicasMissing` |

`IAMCatalogAdminDown` uses scrape-absence, not a literal `/readyz` HTTP check — the same proxy
every sibling service in this platform uses (no blackbox-exporter probe anywhere in this stack). A
pod failing readiness is removed from Service endpoints and, if sustained, stops being scraped as
`up`, which this catches. **Deliberately no `prometheus-adapter` custom-metrics HPA rule** — this
service scales on CPU/memory only (`deploy/helm/templates/hpa.yaml`); at MVP scale, request-rate
growth is a manual "add replicas" decision, not an automated scale target.

# Configuration

**Key env vars** (`.env-example` is the authoritative local-dev list):

| Variable | Default | Notes |
|---|---|---|
| `APP_ENV` | `dev` | `dev`/`staging`/`production` — non-`dev` sets `gin.ReleaseMode` and escalates certain config warnings to startup panics |
| `APP_NAME` | `catalog-admin-config` | Service name, used in logs |
| `APP_PORT` | `8081` | API HTTP listen port |
| `METRICS_PORT` | `9090` | **Separate** listener for `/metrics` — never shares a port with the API |
| `BUILD_VERSION` | `dev` | Injected via `-ldflags` at build time |
| `DOCS_ENABLED` | `false` | Gates Swagger UI in production (always on outside production) |
| `DOCS_AUTH_TOKEN` | — | If set alongside `DOCS_ENABLED=true` in production, gates `/swagger/*` behind `Authorization: Bearer <token>` |
| `CATALOG_TTL_SECONDS` | `60` | `cat:departments`/`cat:plans` cache TTL. Malformed value logs a warning and falls back to 60s (not silently discarded — see CHANGELOG) |
| `DATABASE_URL` | — | Full DSN; takes precedence over `PG_*` vars |
| `PG_HOST` / `PG_PORT` / `PG_USER` / `PG_PASSWORD` / `PG_DBNAME` | — | Used if `DATABASE_URL` unset |
| `PG_SSLMODE` | `require` (prod) / `disable` (local) | Escalated to a startup panic in prod/staging if insecure |
| `PG_MAX_CONNS` | `10` | Sized for read-heavy, write-rare workload |
| `PG_MIN_CONNS` | *(unset — pgcommon default 2)* | Do not set to `0`; `pgcommon.ConfigFromEnv` rejects it and warns |
| `PG_BOUNCER_MODE` | `true` (prod) / `false` (local) | If `true`, `MIGRATION_DATABASE_URL` (or `DATABASE_URL`) is **required** — checked at startup |
| `MIGRATION_DATABASE_URL` | — | Direct (non-PgBouncer) DSN for migrations — `pg_advisory_lock` is session-scoped |
| `PG_STATEMENT_TIMEOUT` | — | e.g. `5s`; appended to the DSN via `ApplyStatementTimeout` |
| `PG_SLOW_QUERY_THRESHOLD` | `200ms` | Slow-query log threshold |
| `VALKEY_URL` | `localhost:6381` (local) | Redis connection string; **must** use `rediss://` in production/staging (checked at startup) |
| `OTEL_SERVICE_NAME` | — | Service name for OTel |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | If unset, tracing is not initialized at all (fully opt-in) |

No outbox/SNS/SQS/S3/Glue vars exist — this service has no events (LLD §10). No RLS/GUC vars
exist — no tenant context (LLD §9).

**Secrets** (`deploy/helm/values.yaml`'s `envFromSecret`): `DATABASE_URL`,
`MIGRATION_DATABASE_URL`, `VALKEY_URL` — three connection-string secrets, managed via the
platform's External Secrets Operator convention (or the chart's own `Secret` template as a
fallback). This service has **no third-party credential to rotate** in the sense every other IAM
service means it (no Keycloak Admin API, no external IdP secret, no AWS SDK dependency at all).

Startup validation fails fast (panics) rather than starting in a partially-configured state — see
`validateRequiredEnv`/`validatePostgresConfig` in `cmd/catalog-admin-config/main.go`.

# Deployment and Scaling

- **Topology:** `iam` namespace, 2 replicas by default (`replicaCount: 2`,
  `autoscaling.minReplicas: 2`/`maxReplicas: 4`), behind a PodDisruptionBudget
  (`minAvailable: 1`). No public ingress by default (`ingress.enabled: false`) — every route is
  either `platform_operator`-gated operator tooling or mesh-internal `iam-system`-gated; CAT-6/7
  (public department reads) can be exposed via HTTPRoute if/when needed.
- **No IRSA role** — no AWS SDK dependency to authorize (LLD §15).
- **Termination:** `terminationGracePeriodSeconds: 45` — 30 s HTTP drain (`main.go`'s shutdown
  timeout) + 15 s buffer. No outbox/SQS-consumer drain step, since this service has neither.
- **Scaling triggers** (LLD §16.3): CPU > 70% or memory > 75% (the shipped HPA targets); a
  sustained `cat:*` cache-hit-ratio drop should prompt investigating Valkey health *before* adding
  replicas (a cache-layer problem, not a compute one, given the small, highly-cacheable payload
  shape). No per-tenant state to shard or rebalance — this service has no concept of "tenant" at
  all, so its scaling story is the simplest in the IAM stack.
- **Probes:** `startupProbe`/`livenessProbe` hit `/healthz` (liveness only — always `200` if the
  process is up); `readinessProbe` hits `/readyz` (checks both Postgres and Valkey — see
  [api-and-events.md](api-and-events.md#caching) for why Valkey health gates readiness even
  though the cache is "advisory" per-request).

# CI/CD

GitHub Actions (`.github/workflows/`). Trimmed from `iam-user-profile`'s pipeline: **no Swagger
staleness check** in `validate-test.yml` (this repo's own `make swag-check` exists but isn't yet
wired as a separate CI step the way the sibling repo's is), and **no event-schema/AsyncAPI sync
step anywhere** — this service publishes no events (LLD §10).

1. **`ci.yml`** (push/PR) — parallel: `validate-test` + `validate-quality` + `build-image`
   (Hadolint + `.dockerignore` check as its first steps, fail-fast before the expensive build) →
   `trivy` (CVE scan) + `smoke` (image smoke tests) → `pr-summary` (PR only) / `push` (push to
   `main`/`master` only, builds + pushes to GHCR, Cosign keyless-signs and self-verifies).
   `paths-ignore: ['**.md', 'docs/architecture/**']` skips the whole pipeline for docs-only
   commits.
2. **`validate-test.yml`** (reusable) — `make test-ci` (race + coverage across unit/postgres/e2e,
   `TESTCONTAINERS_RYUK_DISABLED=true`) → coverage gate **≥ 98%**
   (`.github/scripts/coverage-gate.sh`) → `go-arch-lint` architecture check
   (`.github/scripts/arch-lint.sh`, enforcing `.go-arch-lint.yml`'s hexagonal rules).
3. **`validate-quality.yml`** (reusable) — `gofmt` check, `go mod tidy` drift check, `go vet`,
   `make lint` (currently just an alias for `vet` — no golangci-lint config yet), `govulncheck`,
   Dockerfile base-image digest-pin check (rejects a floating `FROM golang:...` tag without
   `@sha256:...`).
4. **`release.yml`** (tag push `v*`) — validate → build (cross-compiles all target platforms,
   **blocks on a missing `CHANGELOG.md` entry** for the tag) → `docker` (build + push + Trivy
   CRITICAL/HIGH scan that **fails the release** on a hit + CycloneDX SBOM + SLSA provenance +
   Cosign sign/verify) → **`deploy-gate`** (Helm upgrade to the `iam` namespace, verifies the
   deployed image digest matches what was signed, waits for rollout, then a 2-minute Prometheus
   5xx-rate check — auto-`helm rollback` on failure) → `publish` (GitHub Release; only runs if
   `deploy-gate` succeeded — a skipped gate means the release was never actually deployed).
5. **`changelog-check.yml`** (PR-only, paths `internal/**`/`deploy/**`/`cmd/**`) — fails the PR
   outright if it doesn't also modify `CHANGELOG.md`, catching a missing entry earlier than
   `release.yml`'s own tag-time check above.

**Architecture lint** (`go-arch-lint`, `.go-arch-lint.yml`) is a **blocking** CI gate, not just a
local dev tool — a cross-layer import (e.g. `core/service` importing an `adapter/` package
directly) fails the build. See [CLAUDE.md](CLAUDE.md#dependency-rules-enforced-in-ci-via-go-arch-lint)
for the actual dependency rules.
