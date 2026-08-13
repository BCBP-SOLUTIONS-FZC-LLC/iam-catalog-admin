// Package main global Swagger annotations. `swag init` reads this file
// (via `-g swagger_info.go`) to build the top-level OpenAPI/Swagger
// specification — title, version, host, base path, security schemes,
// and tag descriptions. Per-handler `// @…` annotations live next to
// each handler function under internal/adapter/inbound/http/.
//
// Mirrors the sibling iam-org-membership pattern so developers moving
// between services see the same layout. Regenerate the spec with:
//
//	make swag
//
// The generated files under docs/swagger/ are checked into the repo;
// CI's make swag-check fails a PR whose annotations diverge from them.
//
// @title           IAM Catalog / Admin Config API
// @version         1.0
// @description     Authoritative system of record for the platform's two global, non-tenant-scoped reference catalogs — the department catalog and the plan entitlement catalog. Extracted from Org & Membership (`iam-org-membership`) per ADR-0007 Wave 1.
// @description
// @description     **No tenant scope.** Neither departments nor plans carries a tenant_id — this service has no RLS/tenant-context GUC (LLD §9). It is a pure leaf: no outbound synchronous calls to any other IAM service, no outbox/SNS/SQS (LLD §10, CAT-EVT-1/2).
// @description
// @description     **Route prefixes.**  `/api/v1/departments` — any authenticated caller (JWT via gateway), read-only.  `/api/v1/operator/*` — platform operators (`platform_operator` role): the only writers for departments, and both reads and writes for plans (no public plan-read route).  `/api/v1/internal/*` — in-mesh services (mTLS, `iam-system` role), bulk catalog reads for other IAM services' caches.
// @description
// @description     Every mutation echoes `record_version` for optimistic-locking round-trip.
//
// @contact.name   BCBP Solutions
// @contact.email  sharmila.dayalan@bcbpsolutions.com
//
// @license.name   Proprietary
//
// @host      localhost:8080
// @BasePath  /api/v1
//
// @securityDefinitions.apikey UserID
// @in                         header
// @name                       x-user-id
// @description                Authenticated user UUID injected by the API gateway. Use `iam-system` for in-mesh service calls hitting `/api/v1/internal/*`.
//
// @securityDefinitions.apikey TenantID
// @in                         header
// @name                       x-tenant-id
// @description                Tenant UUID injected by the API gateway. Present on every request per the gateway contract even though this service's own data is tenant-agnostic.
//
// @securityDefinitions.apikey TenantRoles
// @in                         header
// @name                       x-tenant-roles
// @description                Comma-separated tenant-role list injected by the API gateway. Operator routes require `platform_operator`; internal routes require `iam-system`.
//
// @tag.name         departments
// @tag.description  Global department catalog (CAT-6, CAT-7)
//
// @tag.name         operator
// @tag.description  Platform operator writes — departments (CAT-1, CAT-2, CAT-3) and plans (CAT-4, CAT-5)
//
// @tag.name         internal
// @tag.description  In-mesh bulk catalog reads (CAT-I1, CAT-I2)
package main
