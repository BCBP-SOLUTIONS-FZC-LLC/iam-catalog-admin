# Changelog

All notable changes to `iam-catalog-admin` are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

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
