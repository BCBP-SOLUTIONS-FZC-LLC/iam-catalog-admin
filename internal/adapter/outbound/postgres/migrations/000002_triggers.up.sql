-- Lifecycle-enforcement triggers, ported verbatim from the O&M LLD (LLD
-- §5.1): touch_row (record_version/updated_at bump), prevent_department_delete
-- (D-4/OP-3 hard-block, this service's own copy — no shared function exists
-- across databases), prevent_department_code_change (D-10),
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

CREATE OR REPLACE FUNCTION public.prevent_system_department_name_change() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.is_system AND OLD.name <> NEW.name THEN
        RAISE EXCEPTION 'system department name is immutable (code: %, old: %, attempted: %)', OLD.code, OLD.name, NEW.name;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_system_department_name_immutable
    BEFORE UPDATE OF name ON public.departments
    FOR EACH ROW EXECUTE FUNCTION public.prevent_system_department_name_change();
