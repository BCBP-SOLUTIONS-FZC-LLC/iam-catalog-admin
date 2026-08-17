CREATE OR REPLACE FUNCTION public.prevent_system_department_name_change() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.is_system AND OLD.name <> NEW.name THEN
        RAISE EXCEPTION 'system department name is immutable (code: %, old: %, attempted: %)', OLD.code, OLD.name, NEW.name;
    END IF;
    RETURN NEW;
END;
$$;
