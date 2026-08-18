-- Gives prevent_system_department_name_change's RAISE EXCEPTION a
-- structured SQLSTATE (23514, check_violation) and a constraint name,
-- instead of a bare message string. Closes a gap found in a
-- shared-library reuse audit: the Go-side match for this trigger
-- (DepartmentService.Patch) was grep'ing the raw error message for
-- "system department name is immutable" instead of using
-- pgcommon.IsCheckViolation/ConstraintName — the same structured helpers
-- already used for the real chk_system_department_active CHECK
-- constraint just above it in the same function.
--
-- This can't become an actual CHECK constraint (it depends on comparing
-- OLD.name to NEW.name, which a CHECK constraint has no way to express —
-- CHECK only sees the new row). RAISE EXCEPTION can still report the same
-- SQLSTATE class a CHECK constraint would (ERRCODE = 'check_violation'),
-- plus a synthetic CONSTRAINT name so the Go side can distinguish this
-- case from chk_system_department_active by name, exactly as it already
-- does for that real constraint.

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
