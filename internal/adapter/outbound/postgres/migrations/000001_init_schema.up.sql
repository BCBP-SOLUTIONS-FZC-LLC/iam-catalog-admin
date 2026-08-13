-- Initial schema for the Catalog / Admin Config Service (LLD §5).
-- departments and plans are global, non-tenant-scoped reference tables —
-- no tenant_id column, no RLS (LLD §9). Shape is byte-identical to the
-- source O&M LLD's departments/plans tables.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE public.tenant_plan AS ENUM ('starter', 'pro', 'enterprise');
CREATE TYPE public.branding_level AS ENUM ('none', 'logo');

-- ── departments (LLD §5.1) ──────────────────────────────────────────────

CREATE TABLE public.departments (
    id             uuid        NOT NULL DEFAULT gen_random_uuid(),
    code           text        NOT NULL,                        -- immutable (D-10)
    name           text        NOT NULL,
    is_system      boolean     NOT NULL DEFAULT false,
    is_active      boolean     NOT NULL DEFAULT true,
    record_version bigint      NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT departments_pkey                    PRIMARY KEY (id),
    CONSTRAINT uq_departments_code                 UNIQUE (code),
    CONSTRAINT departments_code_not_empty          CHECK (code <> ''),
    CONSTRAINT departments_name_not_empty          CHECK (name <> ''),
    CONSTRAINT chk_system_department_active        CHECK (NOT (is_system = true AND is_active = false)),
    CONSTRAINT departments_record_version_positive CHECK (record_version > 0)
);

CREATE INDEX idx_departments_active ON public.departments (is_active) WHERE is_active = true;

INSERT INTO public.departments (code, name, is_system) VALUES
    ('ENGINEERING', 'Engineering', true),
    ('DESIGN',      'Design',      true),
    ('PROCUREMENT', 'Procurement', true),
    ('FINANCE',     'Finance',     true),
    ('LEGAL',       'Legal',       true)
ON CONFLICT (code) DO NOTHING;

-- ── plans (LLD §5.2) ─────────────────────────────────────────────────────

CREATE TABLE public.plans (
    code                    public.tenant_plan     NOT NULL,
    display_name            text                   NOT NULL,
    workflow_template_limit int,                                                -- NULL = unlimited (CAT-D6)
    tender_limit            int,                                                -- NULL = unlimited (CAT-D6)
    trial_duration_days     int                    NOT NULL,
    sso_enabled             boolean                NOT NULL DEFAULT false,
    custom_branding         public.branding_level  NOT NULL DEFAULT 'none',
    feature_set             jsonb                  NOT NULL DEFAULT '{}'::jsonb,
    record_version          bigint                 NOT NULL DEFAULT 1,
    created_at              timestamptz            NOT NULL DEFAULT now(),
    updated_at              timestamptz            NOT NULL DEFAULT now(),
    CONSTRAINT plans_pkey                    PRIMARY KEY (code),
    CONSTRAINT plans_display_name_not_empty  CHECK (display_name <> ''),
    CONSTRAINT plans_wf_limit_nonneg         CHECK (workflow_template_limit IS NULL OR workflow_template_limit >= 0),
    CONSTRAINT plans_tender_limit_nonneg     CHECK (tender_limit IS NULL OR tender_limit >= 0),
    CONSTRAINT plans_trial_days_nonneg       CHECK (trial_duration_days >= 0),
    CONSTRAINT plans_record_version_positive CHECK (record_version > 0)
);

INSERT INTO public.plans (code, display_name, workflow_template_limit, tender_limit, trial_duration_days, sso_enabled, custom_branding, feature_set) VALUES
    ('starter',    'Starter',     5,    10,   30, false, 'none', '{}'::jsonb),
    ('pro',        'Pro',         50,   100,  30, false, 'logo', '{}'::jsonb),
    ('enterprise', 'Enterprise',  NULL, NULL, 30, true,  'logo', '{"require_mfa_all_users_allowed": true}'::jsonb)
ON CONFLICT (code) DO NOTHING;
