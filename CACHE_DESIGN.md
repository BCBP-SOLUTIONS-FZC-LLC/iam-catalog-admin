# Cache Design

Per `catalog-admin-config-service-lld.md` §8. This document covers only the cache layer this
repository owns (`cat:*`). The consumer-side caches (`om:departments`, `om:plans`,
`gm:departments`, and their `:stale` fallbacks) are owned by Core Org & Membership and the Group
Mapping Service respectively.

## This service's own cache

| Cache Key | TTL | Source Of Truth | Invalidation Event | Fallback Behavior |
|---|---|---|---|---|
| `cat:departments` | 60s | This service's own `departments` table (Postgres) | Any successful `CAT-1` (create) or `CAT-2` (patch) write | Cache miss, timeout, or Valkey outage → read straight from Postgres, populate cache on success, and **never** fail the request because Valkey is unavailable (CAT-FAIL-1) |
| `cat:plans` | 60s | This service's own `plans` table (Postgres) | Any successful `CAT-5` (patch) write | Same as above |

Implementation: `internal/core/service/department_service.go` (`listAllCached`) and
`internal/core/service/plan_service.go` (`List`). Both cache the **full catalog** as a single JSON
blob — `cat:departments` is not filtered by `active_only` before caching; the `active_only=true`
filter (CAT-6's query param) is applied in-memory *after* the cache read, so both the public list
and the internal bulk read (CAT-I1) share one cache entry rather than each maintaining its own.

Single-row reads (CAT-7's `GET /departments/:id`, CAT-4's `GET /operator/plans/:code`) are **not**
cache-fronted — they go straight to Postgres. The LLD's own cache table (§8) only names
whole-catalog keys; there was no whole-catalog-plus-per-row caching requirement to satisfy, and
skipping it keeps the cache invalidation logic simple (one key to evict per table, not N+1).

Cache hit/miss is recorded per key via `internal/adapter/outbound/metrics` (`catalog_admin_cache_hits_total`,
`catalog_admin_cache_misses_total`), instrumented at the Valkey adapter layer
(`internal/adapter/outbound/valkey/cache.go`) rather than in the core service layer, so the
hexagonal boundary (`core/service` → `port.Cache`, no adapter imports) stays intact.

## Why TTL-only, no active invalidation (CAT-D3)

Same rationale as the source LLD: this service does not know who its consumers are (Core,
Group Mapping, any future consumer) and does not maintain a webhook registry to push
invalidations to them. A write here is visible to this service's *own* `GET` endpoints
immediately (cache is invalidated post-commit, before any read can observe a stale value from
*this* service) — cross-service staleness is bounded only by each consumer's own TTL, which is
their cache, not this service's problem to solve beyond keeping its own `cat:*` keys short-lived
(60s) so its own read-replica-shielding purpose doesn't add meaningfully to that bound.

## Consumer-side caches (for context — not implemented in this repo)

| Key | Owner | TTL | Populated from |
|---|---|---|---|
| `om:departments` | Core (O&M) | 600s | `GET /api/v1/internal/departments` (CAT-I1) |
| `om:plans` | Core (O&M) | 600s | `GET /api/v1/internal/plans` (CAT-I2) |
| `gm:departments` | Group Mapping Service | 600s | CAT-I1 |
| `om:departments:stale` / `om:plans:stale` / `gm:departments:stale` | same as above | 24h | refreshed opportunistically on every successful CAT-I1/CAT-I2 call; served only when the primary key has expired **and** the live call to this service fails |

These are out of scope for this repository (they live in `iam-org-membership` and the
Group Mapping Service's own codebases) and are called out here only so the full propagation
picture is legible from one document.
