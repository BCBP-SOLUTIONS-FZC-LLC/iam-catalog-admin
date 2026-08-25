-- Initial schema for the Catalog / Admin Config Service (LLD §5).
-- departments and plans are global, non-tenant-scoped reference tables —
-- no tenant_id column, no RLS (LLD §9). Shape is byte-identical to the
-- source O&M LLD's departments/plans tables.
--
-- Single consolidated migration (pre-production; this service has not yet
-- been deployed, so no environment has applied a prior multi-file history
-- that needs preserving) covering schema, lifecycle-enforcement triggers,
-- and the runtime DB role/grants.

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

-- ── lifecycle-enforcement triggers, ported from the O&M LLD (LLD §5.1) ───
-- touch_row (record_version/updated_at bump), prevent_department_delete
-- (D-4/OP-3 hard-block, this service's own copy — no shared function
-- exists across databases), prevent_department_code_change (D-10),
-- prevent_system_department_name_change (D-11). Also applies the same
-- touch_row bump to plans on UPDATE.

CREATE OR REPLACE FUNCTION public.touch_row() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    NEW.record_version := OLD.record_version + 1;
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_touch_departments
    BEFORE UPDATE ON public.departments
    FOR EACH ROW WHEN (OLD.* IS DISTINCT FROM NEW.*)
    EXECUTE FUNCTION public.touch_row();

CREATE TRIGGER trg_touch_plans
    BEFORE UPDATE ON public.plans
    FOR EACH ROW WHEN (OLD.* IS DISTINCT FROM NEW.*)
    EXECUTE FUNCTION public.touch_row();

CREATE OR REPLACE FUNCTION public.prevent_department_delete() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'departments cannot be deleted (code: %, id: %); retire via is_active = false', OLD.code, OLD.id;
END;
$$;
CREATE TRIGGER trg_prevent_department_delete
    BEFORE DELETE ON public.departments
    FOR EACH ROW EXECUTE FUNCTION public.prevent_department_delete();

CREATE OR REPLACE FUNCTION public.prevent_department_code_change() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.code <> NEW.code THEN
        RAISE EXCEPTION 'department code is immutable (old: %, attempted: %)', OLD.code, NEW.code;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_department_code_immutable
    BEFORE UPDATE OF code ON public.departments
    FOR EACH ROW EXECUTE FUNCTION public.prevent_department_code_change();

-- Raises with a structured SQLSTATE (23514, check_violation) and a synthetic
-- constraint name — rather than a bare message string — so the Go side
-- (DepartmentService.Patch) can match it via pgcommon.IsCheckViolation/
-- ConstraintName, the same structured helpers used for the real
-- chk_system_department_active CHECK constraint above. This can't become an
-- actual CHECK constraint: it depends on comparing OLD.name to NEW.name,
-- which a CHECK constraint has no way to express (CHECK only sees the new
-- row).
CREATE OR REPLACE FUNCTION public.prevent_system_department_name_change() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.is_system AND OLD.name <> NEW.name THEN
        RAISE EXCEPTION USING
            ERRCODE = 'check_violation',
            CONSTRAINT = 'chk_system_department_name_immutable',
            MESSAGE = 'system department name is immutable',
            DETAIL = format('code=%s, old=%s, attempted=%s', OLD.code, OLD.name, NEW.name);
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_system_department_name_immutable
    BEFORE UPDATE OF name ON public.departments
    FOR EACH ROW EXECUTE FUNCTION public.prevent_system_department_name_change();

-- ── runtime DB role/grants (LLD §9) ──────────────────────────────────────
-- Single runtime DB role: catalog_admin_app has no BYPASSRLS grant (there
-- is nothing to bypass — neither table is RLS-protected) and no
-- migrator/admin_readonly split is needed (no cross-tenant data exists to
-- read around). SELECT is broad (mesh-readable, LLD §9); INSERT/UPDATE are
-- restricted to this role only. No DELETE grant — hard-delete is triggered
-- to fail regardless (OP-3/D-4), but the grant is withheld too, in defense
-- in depth.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'catalog_admin_app') THEN
        CREATE ROLE catalog_admin_app LOGIN NOBYPASSRLS;
    ELSE
        ALTER ROLE catalog_admin_app NOBYPASSRLS;
    END IF;
END
$$;

GRANT SELECT, INSERT, UPDATE ON public.departments TO catalog_admin_app;
GRANT SELECT, UPDATE ON public.plans TO catalog_admin_app;
GRANT USAGE ON SCHEMA public TO catalog_admin_app;
