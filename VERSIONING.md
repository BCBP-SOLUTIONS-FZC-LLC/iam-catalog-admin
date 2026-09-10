# Versioning and releases

This repository is a **deployed Go microservice**, not a library other services `go get` — `pkg/requestctx` is an internal helper for this process, and the `go.mod` module path exists so this repo's own code compiles, not for import by sibling repos (contrast [`platform-events`](https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-events)'s `VERSIONING.md`, which this document is modeled on but differs from for that reason). What gets versioned here is the **container image** (`ghcr.io/bcbp-solutions-fzc-llc/iam-catalog-admin`) and the **Helm chart** (`deploy/helm/`) that deploys it, plus the runtime contract Core Org & Membership (`iam-org-membership`), the Group Mapping Service (once ADR-0007 Wave 2 ships), and operators depend on. Versions are published with **Git tags** and described in [CHANGELOG.md](./CHANGELOG.md).

This document follows the same SemVer / image-tag / maintainer-process layout as [`iam-org-membership/VERSIONING.md`](https://github.com/BCBP-SOLUTIONS-FZC-LLC/iam-org-membership/blob/main/VERSIONING.md), scaled down for a single-binary, no-events leaf service.

## Semantic versioning (SemVer)

We use [SemVer 2.0.0](https://semver.org/): `MAJOR.MINOR.PATCH` (e.g. `v1.2.3`).

| Bump | When you change | Examples |
|------|-----------------|----------|
| **MAJOR** | Breaking change in the runtime contract (§ below) | Removing/renaming a CAT-* route, a documented request/response field, a required env var, a domain error **code**, or an incompatible `values.yaml` restructure |
| **MINOR** | New backward-compatible capability | A new endpoint (new ID, never reuse a retired one), a new optional env var / Helm value, a new `catalog_admin_*` metric, an additive response field |
| **PATCH** | Backward-compatible fix | Bug fix (e.g. the migration-DSN malformation fix, CAT-D13's Summary→Histogram metric-type correction), performance improvement, dependency bump with no observable behavior change, documentation-only correction |

### What counts as the runtime contract

This service has no Go package for another service to import — its "public API" is the wire/deployment contract every caller and operator depends on. The LLD's own §16 Decision Register and §17 error taxonomy are the closest thing to a frozen-name registry; treat both as frozen in the spirit this section describes.

| In scope (SemVer applies) | Out of scope (may change without MAJOR) |
|---------------------------|------------------------------------------|
| The 9 active routes across three prefixes — `/api/v1/*` (CAT-6, CAT-7; 2), `/api/v1/operator/*` (CAT-1..5; 5), `/api/v1/internal/*` (CAT-I1, CAT-I2; 2) — method, path, and documented request/response field names (LLD §5, README.md § API overview) | `internal/*` package structure, exported Go identifiers, file layout — nothing here is imported by another module. Gin's internal route-group structure is not part of the wire contract |
| Domain error **codes** (`internal/core/domain/errors.go`, 16 sentinels, e.g. `optimistic_lock_conflict`, `system_department_cannot_be_retired`, `invalid_feature_value`) and their HTTP statuses (`HandleError`/`domainErrorStatus`, LLD §17) | Exact error `message` wording |
| Required env var **names and semantics** (`.env-example`; genuinely required outside dev: `DATABASE_URL`/`PG_*` or the pieces `pgcommon.ConfigFromEnv` needs, `VALKEY_URL`, `MIGRATION_DATABASE_URL` when `PG_BOUNCER_MODE=true`) | Env var **defaults** (`CATALOG_TTL_SECONDS=60`, `PG_MAX_CONNS=10`, etc.) — tunable without a MAJOR bump unless the new default itself breaks a documented invariant |
| `deploy/helm/values.yaml` top-level key names and shapes consumers actually set (`image.*`, `env`, `envFromSecret`, `secretValues`, `replicaCount`, `autoscaling.*`, `service.*`, `networkPolicy.*`, `ingress.*`, `serviceAccount.*`) | Chart internals (`_helpers.tpl`, template structure) not exposed as a `values.yaml` key |
| Prometheus metric **names** (`internal/adapter/outbound/metrics/metrics.go`) — `catalog_admin_requests_total`, `catalog_admin_request_duration_seconds` (a Histogram — CAT-D13, LLD §16), `catalog_admin_writes_total`, `catalog_admin_optimistic_lock_conflicts_total`, `catalog_admin_cache_hits_total`, `catalog_admin_cache_misses_total` — dashboards and `deploy/monitoring/slo-rules.yml`'s burn-rate alerts key off these exact names | Metric **label cardinality** beyond what's documented, histogram bucket boundaries; passthrough `http_*`/`pgcommon_*` names owned by `platform-gincommon`/`platform-pgcommon` |
| `GET /healthz` / `/readyz` (HTTP) — existence and meaning (`/readyz` checks Postgres and Valkey independently, CAT-D11) | `GET /swagger/*any` content shape — a documentation surface, not a contract a caller must hold stable. `METRICS_PORT`'s default value is a Helm default, not a SemVer surface |

**No event contract exists to version.** This service publishes no events and consumes no events (LLD §7, invariants CAT-EVT-1..5) — there is no `api/asyncapi.yaml`, no event `type` name, and no `platform-schemagov` registration in scope here, unlike every sibling service that carries a transactional outbox.

**Retired IDs:** none yet in this service's own `CAT-*` numbering — CAT-1 through CAT-I2 have all shipped and none has been removed. (The `O-1`/`O-2`/`O-3`/`O-5`/`O-6` IDs these routes replaced were retired in `iam-org-membership`'s own numbering when ADR-0007 moved them here — that retirement belongs to that repo's `VERSIONING.md`, not this one.) Should a `CAT-*` ID ever be retired, it is permanently unregistered here, never reused for a new endpoint, matching the platform-wide convention.

This service has no third-party admin-API client (no Keycloak Admin API, no AWS SDK) to track in a compatibility row the way `iam-realm-provisioner` tracks `gocloak` — it is a pure leaf with zero outbound synchronous calls to any other service (LLD §3, §18).

### Guarantees

- **Pre-`v1.0.0` (current status):** per [SemVer §4](https://semver.org/#spec-item-4), anything may change at any time while the major version is `0` — this service has never been deployed to any environment (per [CHANGELOG.md](CHANGELOG.md)'s own statement) and has not yet committed to a stable *service* contract via a Git tag. The table above still indicates what's *more* disruptive than what within `v0.x`, but a downstream caller should not yet assume `v0.x` compatibility across a MINOR bump the way it could once `v1.0.0` ships.
- **MAJOR (once `v1` ships):** we avoid breaking changes to the runtime contract within `v1.x`. A breaking change ships as `v2.0.0` with migration notes in the CHANGELOG.
- **MINOR:** safe to redeploy without changing caller URLs or Helm values, unless you opt into a new capability.
- **PATCH:** drop-in image replacement; upgrade recommended for security fixes (every image is CVE-scanned, see below).

## Supported releases

| Version | Status | Image tag | Notes |
|---------|--------|-----------|-------|
| *(none tagged)* | **Unreleased** | — | [CHANGELOG.md](CHANGELOG.md) lives entirely under `[Unreleased]` — no version-numbered section exists yet, since nothing has actually released. `deploy/helm/Chart.yaml` already carries `version: 0.1.0`/`appVersion: "0.1.0"` as a pre-staged value, not a claim that `0.1.0` shipped. No live deployment yet, so no support window has started |
| `< v0.1.0` | — | — | No tagged Git releases |

Unlike some sibling services' `CHANGELOG.md`, this repo's `[Unreleased]` section carries no known cross-repo contract blocker today — this service publishes no events, so there is no consumer-side subscription that could have drifted out from under a rename the way an event-name change would elsewhere. Cutting the first tag is a matter of following the maintainer process below, not resolving an outstanding cross-service dependency first.

This service has no formal support-window policy yet, since nothing is running in production against a tagged release. Once a `v1.0.0` ships to a real environment, this section will define how long a superseded major line receives security-only fixes (expect the same platform convention `iam-org-membership`/`iam-realm-provisioner` use: security fixes only, for a period the platform team sets).

## Consume a release

This service is **not** consumed via `go get` — do not add this module as a dependency of another Go service. It is consumed as a **container image** on the mesh (public `/api/v1/*`, operator `/api/v1/operator/*`, internal `/api/v1/internal/*`), via the **Helm chart** in `deploy/helm/`. `iam-org-membership` calls it over HTTP (`CatalogAdminClient` → CAT-I1/CAT-I2); the Group Mapping Service will do the same once ADR-0007 Wave 2 ships.

### Pull and verify the image

Once a tag exists:

```bash
docker pull ghcr.io/bcbp-solutions-fzc-llc/iam-catalog-admin:v0.1.0
```

**Today** only `main`-branch images are published (see CI below). Every image pushed from `ci.yml` is signed keylessly via Sigstore/Cosign (no long-lived key) — verify before deploying:

```bash
# main-branch builds (current — signed by ci.yml's push job)
cosign verify \
  --certificate-identity-regexp "^https://github\.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/.github/workflows/.*@refs/heads/(main|master)$" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/bcbp-solutions-fzc-llc/iam-catalog-admin@<digest>
```

Tagged releases, published by `.github/workflows/release.yml`, use `@refs/tags/` in the identity regexp instead of `@refs/heads/(main|master)`:

```bash
# tagged releases (signed by release.yml's docker job)
cosign verify \
  --certificate-identity-regexp "^https://github\.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/.github/workflows/.*@refs/tags/.*$" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/bcbp-solutions-fzc-llc/iam-catalog-admin@<digest>
```

### Image tag scheme

`release.yml`'s `docker/metadata-action` step produces, per tag push (same scheme as `iam-org-membership`/`iam-realm-provisioner`):

| Tag pattern | Produced for | Example (from `v1.2.3`) |
|-------------|--------------|--------------------------|
| `vMAJOR.MINOR.PATCH` | Every release | `v1.2.3` |
| `vMAJOR.MINOR` | Every release | `v1.2` |
| `vMAJOR` | Every release | `v1` |
| `latest` | Stable releases only (no pre-release suffix) | `latest` |

Separately, `ci.yml` pushes the image on every merge to `main` with git SHA / branch tags — pin production by a **release tag or digest**, not `latest` or an untagged `main` build.

| Pin style | Use when |
|-----------|----------|
| `vMAJOR.MINOR.PATCH` | Production; exact reproducibility (recommended — also enables digest verification) |
| `vMAJOR.MINOR` | Accept PATCH updates automatically |
| `vMAJOR` | Accept MINOR/PATCH updates automatically — not recommended before `v1.0.0` |
| `latest` / untagged `main` | Local/dev experiments only; never production |

### Deploy via Helm

```bash
helm upgrade iam-catalog-admin ./deploy/helm \
  --install \
  --namespace iam \
  --set image.tag=v0.1.0 \
  --set secretValues.DATABASE_URL="$DATABASE_URL" \
  --set secretValues.MIGRATION_DATABASE_URL="$MIGRATION_DATABASE_URL" \
  --set secretValues.VALKEY_URL="$VALKEY_URL"
```

`image.tag` (`deploy/helm/values.yaml`) defaults to the chart's own `appVersion` (`Chart.yaml`) when unset. Chart `version`/`appVersion` are bumped manually alongside the Git tag (see the maintainer process below); they are **not** derived automatically from the tag by any workflow today.

The chart deploys **one** `Deployment` — no `CronJob`, unlike `iam-org-membership`'s seven — from the single image, which carries only the `catalog-admin-config` binary (the image's `ENTRYPOINT`; there is no second binary the way `iam-org-membership` ships a `reconciler`, since this service has nothing to reconcile).

## Maintainer release process

`.github/workflows/release.yml` implements the validate → build → docker (CVE scan/sign) → **deploy-gate** → publish pipeline, modeled on `iam-org-membership`'s release workflow — with the same deliberate characteristic worth knowing before you tag: **the deploy-gate here is not optional.** The `deploy-gate` job fails immediately if the `KUBECONFIG_B64` secret isn't set, and the final `publish` job (the one that creates the GitHub Release) requires `deploy-gate` to have *succeeded* — not merely run. Practically: **you cannot complete a tagged release of this service today without a real cluster to deploy to and verify against.** `ci.yml`'s push-to-`main` path (image to GHCR + Cosign, unversioned SHA/branch tags) keeps running independently of tagging — it is not replaced by `release.yml`.

When cutting a tagged release:

1. **Merge** all changes for the release to `main`.

2. **Update `CHANGELOG.md`:** move `[Unreleased]` entries into a new `## [0.1.0] - YYYY-MM-DD` section. `release.yml`'s `build` job runs `.github/scripts/verify-changelog-entry.sh`, which fails the release if this section is missing — cut it *before* tagging.

3. **Confirm `deploy/helm/Chart.yaml`'s `version`/`appVersion`** still match `X.Y.Z` — both already read `0.1.0` for the first release, so this step is a confirmation, not a bump, the first time around. For every release after the first, nothing does this automatically — a consumer who deploys the chart without setting `image.tag` explicitly gets whatever `appVersion` was last committed, so a forgotten bump here silently ships a stale image.

4. **Run `make ci` locally** to confirm everything is green before tagging:
   ```bash
   make ci   # tidy + fmt-check + vet + lint + test-ci + build
   ```
   Note that `make ci` does **not** currently include `go-arch-lint` — `go-arch-lint check --project-path .` is run separately in `validate-test.yml`, where it currently passes clean with zero violations. Unlike some sibling services, there is no known outstanding arch-lint violation to work around here; still worth confirming manually before tagging, since `make ci` alone won't catch a regression.

5. **Create and push an annotated tag** — this is what triggers `release.yml`:
   ```bash
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

6. **The release workflow** (`.github/workflows/release.yml`) runs:
   - `validate-test` / `validate-quality` — the same reusable gates `ci.yml` uses, re-run at the exact tagged commit. **`validate-test.yml`'s coverage gate is pinned to a 98% threshold** (`.github/scripts/coverage-gate.sh`, `COVERAGE_THRESHOLD: '98'`) — this repo's own established baseline, currently measured at 99.9%, so this gate is healthy and passing today.
   - `build` — verifies the tag matches HEAD (`verify-release-tag.sh`), verifies the CHANGELOG entry exists (`verify-changelog-entry.sh`), cross-compiles the `catalog-admin-config` binary (`prepare-release-binary.sh`) for 5 platforms — a convenience for non-Docker runs, not the primary distribution artifact (this service is deployed as a container, and the container build is a separate step).
   - `docker` — builds and pushes the semver-tagged image (see tag scheme above), CVE-scans it (fails the release on CRITICAL/HIGH), generates a CycloneDX SBOM and SLSA provenance, signs with Cosign, then verifies the signature.
   - `deploy-gate` (requires `KUBECONFIG_B64`; **not skippable** — see above) — live Helm deploy (namespace from `K8S_NAMESPACE`, defaulting to `default`; set to `iam` to match this service's LLD §13.1 topology), deployed-digest verification against what was pushed, `kubectl rollout status`, then a 2-minute Prometheus-backed error-rate check (`http_requests_total{status_class="5xx"}`, this service's own `platform-gincommon`-emitted HTTP metric, threshold 1%) that auto-rolls-back via `helm rollback` on failure. The error-rate check itself is skipped (not failed) if `PROMETHEUS_URL` isn't set, but the job as a whole still requires `KUBECONFIG_B64`.
   - `publish` — creates the GitHub Release with the CHANGELOG section as notes, plus the binaries/checksums/SBOM/provenance attached. Runs only if `build`, `docker`, **and** `deploy-gate` all succeeded.

7. **Notify consumers** — `iam-org-membership` (`CatalogAdminClient`, CAT-I1/CAT-I2 caller), the Group Mapping Service (once it's live), and whoever owns the Helm deployment — with upgrade notes if MINOR or MAJOR. A route ID, response field, or error code change is the highest-blast-radius part of this contract, since both existing/future consumers cache the full catalog locally and re-derive their own local shape from it.

### Pre-release tags (optional)

| Tag pattern | Meaning |
|-------------|---------|
| `v1.1.0-rc.1` | Release candidate; not for production unless approved |
| `v1.1.0-beta.1` | Early integration testing |

Both should match a `v[0-9]*.[0-9]*.[0-9]*-*` tag trigger and produce a signed, scanned image — but never a `latest` tag (see the tag scheme table above).

## Compatibility matrix

| iam-catalog-admin | Go (`go.mod`) | Shared platform libraries | `platform-schemagov` (`schema-gov` CLI) |
|---|---|---|---|
| Unreleased (`main`) | `1.26.6` | `platform-gincommon` v1.3.0, `platform-pgcommon` v1.3.0 | N/A — this service has no events to govern (LLD §7) |

`platform-events` is deliberately not in this table — it is not a dependency of this service at all, unlike every sibling that carries a transactional outbox. None of the libraries above is re-exported — a consumer of this *service* never needs them as a direct dependency of *this* module. Callers depend on the HTTP contract, not on any Go package here. This service has no Keycloak Admin API dependency of its own (see the note above the Guarantees section) — Keycloak server version is entirely Realm Provisioner's and Keycloak ops' concern, invisible to this compatibility matrix.

## Related files

| File | Purpose |
|------|---------|
| [CHANGELOG.md](./CHANGELOG.md) | User-facing history per version — currently entirely under `[Unreleased]`, since nothing has shipped yet |
| [README.md](./README.md) | Mental model, API overview, local dev, deployment, contributing |
| [docs/lld/iam-lld-catalog-admin-config-service.md](./docs/lld/iam-lld-catalog-admin-config-service.md) | The runtime contract in full — §16 Decision Register + open-question sign-off register, §17 error taxonomy, §18 integration details, §19 migration strategy |
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Layer model, request/write flows, cache strategy, key invariants, threat model |
| [.github/workflows/ci.yml](./.github/workflows/ci.yml) | Current image build / Trivy / Cosign-on-`main` pipeline |
| [.github/workflows/release.yml](./.github/workflows/release.yml) | Tag-triggered release pipeline (validate → build → docker → deploy-gate → publish) |
| [deploy/helm/Chart.yaml](./deploy/helm/Chart.yaml) | Helm chart version / app version |
| [go.mod](./go.mod) | Module path and minimum Go version — not a consumable package |
