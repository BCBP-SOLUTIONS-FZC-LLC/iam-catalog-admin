# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.x     | ✅ Active |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Email: vijay@bcbpsolutions.com
Subject: `[iam-catalog-admin] Security vulnerability`

Include in your report:
- Description of the vulnerability and the affected component (HTTP handler, cache, database layer, etc.)
- Steps to reproduce
- Potential impact (privilege escalation, denial of service, data integrity, etc.)
- Suggested fix or patch (if any)

### Response timeline

| Step | Target |
|------|--------|
| Initial acknowledgement | 48 hours |
| Severity assessment | 5 business days |
| Patch release (critical/high) | 14 days |
| Public disclosure | After patch ships |

We follow responsible disclosure. Reporters will be credited in release notes unless anonymity is requested.

## Scope

This service holds no tenant data and no PII — `departments` and `plans` are a global,
non-tenant-scoped reference catalog (LLD §9: neither table carries a `tenant_id`, neither
is RLS-protected). Areas of particular sensitivity are correspondingly narrower than a
tenant-scoped IAM service:

- **`platform_operator` role-gating** — CAT-1/CAT-2 (department writes) and CAT-4/CAT-5
  (plan writes) are gated behind the `platform_operator` role at both the middleware
  (`RequireOperatorRole`) and handler (`requireOperator`) layers; a bypass would let any
  authenticated caller rewrite the department/plan catalog every tenant reads from.
- **`iam-system` mesh-only routes** — CAT-I1/CAT-I2 (`GET /api/v1/internal/*`) are intended
  to be reachable only from inside the cluster mesh (NetworkPolicy) and are additionally
  gated on the reserved `iam-system` principal (`RequireSystemRole`, `requireSystem`) as
  defense-in-depth; a bypass would expose the full catalog to any caller reachable at the
  Service, not just other IAM services.
- **Optimistic-locking (`record_version`) bypass** — every write is version-checked; a bug
  that skipped this check could let a stale client silently clobber a concurrent operator's
  change to shared reference data every tenant depends on.
- **Cache poisoning (`cat:departments` / `cat:plans`)** — Valkey is advisory-only (CAT-FAIL-1:
  every miss/timeout/outage falls through to Postgres), but a compromised cache that served
  fabricated department/plan data would misinform every consumer service's own read-through
  cache (`om:departments`/`om:plans`) for up to their configured TTL.

## Not applicable to this service

Unlike a tenant-scoped IAM service, this service has no Row-Level Security policy, no GUC
bridging, no outbound event publishing (no outbox, no SNS/SQS), no S3/Glue dependency, and
no LLM/RAG integration — see `ARCHITECTURE.md`'s "What this service deliberately does not
have" table for the full list and rationale.
