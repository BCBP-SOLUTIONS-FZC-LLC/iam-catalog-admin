package domain

import "time"

// TenantPlan mirrors the tenant_plan enum in this service's DB (LLD §5.2).
// Core (iam-org-membership) keeps its own local copy of this same enum on
// tenants.plan — a Postgres ENUM type cannot be shared across databases
// (LLD §5.2) — so referential integrity there is an app-level check, not a
// cross-database FK.
type TenantPlan string

const (
	PlanStarter    TenantPlan = "starter"
	PlanPro        TenantPlan = "pro"
	PlanEnterprise TenantPlan = "enterprise"
)

// BrandingLevel mirrors the branding_level enum (LLD §5.2).
type BrandingLevel string

const (
	BrandingNone BrandingLevel = "none"
	BrandingLogo BrandingLevel = "logo"
)

// Plan is the global operator entitlement catalog row (LLD §5.2). PK is the
// code. Operator PATCH-only (CAT-5) — the tier set is fixed to the ENUM, no
// create/delete API (PLAN-4).
//
// WorkflowTemplateLimit / TenderLimit are nil pointers meaning "unlimited"
// (LLD §5.2/CAT-D6 — nullable-limit representation over a -1 sentinel).
// NULL in the DB, unset/null in JSON.
type Plan struct {
	Code                  TenantPlan
	DisplayName           string
	WorkflowTemplateLimit *int // nil = unlimited
	TenderLimit           *int // nil = unlimited
	TrialDurationDays     int
	SSOEnabled            bool
	CustomBranding        BrandingLevel
	// FeatureSet is the baseline entitlement flags. Effective per-tenant
	// value = FeatureSet ⊕ Core's tenants.feature_flags override delta
	// (PLAN-6, computed exclusively in Core at I-8 read time — LLD §5.3).
	// This service owns the left operand only.
	FeatureSet    map[string]any
	RecordVersion int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PlanPatch is the partial-update payload for CAT-5.
type PlanPatch struct {
	DisplayName           *string
	WorkflowTemplateLimit **int // double pointer distinguishes "unset" from "set to nil (unlimited)"
	TenderLimit           **int
	TrialDurationDays     *int
	SSOEnabled            *bool
	CustomBranding        *BrandingLevel
	FeatureSet            map[string]any

	RecordVersion int64
}
