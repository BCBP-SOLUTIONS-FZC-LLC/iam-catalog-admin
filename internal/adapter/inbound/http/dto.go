package http

// ── Error envelope (mirrors iam-org-membership's dto.go / LLD-inherited §17 shape) ──

// ErrorResponse is the flat JSON error envelope shared with every other
// IAM service so clients parse one shape platform-wide.
//
// Some error codes carry additional, code-specific top-level fields beyond
// the ones below — e.g. optimistic_lock_conflict adds record_version,
// field_immutable adds field (LLD §17) — set via domain.DomainError.WithDetails
// and merged flatly onto the response body by errorResponseWithDetails
// (middleware.go), not nested under a details key. There is deliberately no
// generic details field on this struct: an earlier revision had one
// (Details []ValidationError) that Swagger advertised but no code path ever
// populated, since every actual newErrorResponse call site passed nil —
// removed rather than left to describe a shape that never appears on the wire.
type ErrorResponse struct {
	Error     string `json:"error" example:"not_found"`
	Status    int    `json:"status" example:"404"`
	TraceID   string `json:"trace_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Code      string `json:"code" example:"not_found"`
	Message   string `json:"message" example:"resource not found"`
}

// ── Departments (CAT-1, CAT-2, CAT-6, CAT-7) ─────────────────────────────

// DepartmentCreateRequest is CAT-1's request body.
type DepartmentCreateRequest struct {
	Code     string `json:"code" binding:"required"`
	Name     string `json:"name" binding:"required"`
	IsSystem bool   `json:"is_system"`
}

// DepartmentPatchRequest is CAT-2's request body.
type DepartmentPatchRequest struct {
	Name          *string `json:"name,omitempty"`
	IsActive      *bool   `json:"is_active,omitempty"`
	RecordVersion int64   `json:"record_version"`
}

// DepartmentResponse is the wire shape for a single department, used by
// CAT-1, CAT-2, CAT-6, CAT-7, and (embedded in a list) CAT-I1.
type DepartmentResponse struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	IsSystem      bool   `json:"is_system"`
	IsActive      bool   `json:"is_active"`
	RecordVersion int64  `json:"record_version"`
}

// DepartmentListResponse wraps CAT-6's public list payload.
type DepartmentListResponse struct {
	Items []DepartmentResponse `json:"items"`
}

// InternalDepartmentsResponse is CAT-I1's bulk-read response shape (LLD §5.4).
type InternalDepartmentsResponse struct {
	Departments []DepartmentResponse `json:"departments"`
	AsOf        string               `json:"as_of"`
}

// ── Plans (CAT-4, CAT-5) ──────────────────────────────────────────────────

// PlanResponse is the wire shape for a single plan.
type PlanResponse struct {
	Code                  string         `json:"code" enums:"starter,pro,enterprise"`
	DisplayName           string         `json:"display_name"`
	WorkflowTemplateLimit *int           `json:"workflow_template_limit" extensions:"x-nullable=true"`
	TenderLimit           *int           `json:"tender_limit" extensions:"x-nullable=true"`
	TrialDurationDays     int            `json:"trial_duration_days"`
	SSOEnabled            bool           `json:"sso_enabled"`
	CustomBranding        string         `json:"custom_branding" enums:"none,logo"`
	FeatureSet            map[string]any `json:"feature_set"`
	RecordVersion         int64          `json:"record_version"`
}

// PlansListResponse wraps CAT-4's list payload.
type PlansListResponse struct {
	Items []PlanResponse `json:"items"`
}

// PlanPatchRequest is CAT-5's request body. WorkflowTemplateLimit/TenderLimit
// are nullable integers: omit the field entirely to leave it unchanged, send
// null to set it to unlimited, or send an integer >= 0 to set a cap.
// The handler parses these via json.RawMessage (plan_handler.go's parseLimit)
// to distinguish the three states; this struct is the Swagger-doc shape only.
type PlanPatchRequest struct {
	DisplayName           *string        `json:"display_name,omitempty"`
	WorkflowTemplateLimit *int           `json:"workflow_template_limit,omitempty" extensions:"x-nullable=true"`
	TenderLimit           *int           `json:"tender_limit,omitempty" extensions:"x-nullable=true"`
	TrialDurationDays     *int           `json:"trial_duration_days,omitempty"`
	SSOEnabled            *bool          `json:"sso_enabled,omitempty"`
	CustomBranding        *string        `json:"custom_branding,omitempty" enums:"none,logo"`
	FeatureSet            map[string]any `json:"feature_set,omitempty"`
	RecordVersion         int64          `json:"record_version"`
}

// InternalPlansResponse is CAT-I2's bulk-read response shape (LLD §5.4).
type InternalPlansResponse struct {
	Plans          []PlanResponse   `json:"plans"`
	RecordVersions map[string]int64 `json:"record_versions"`
}
