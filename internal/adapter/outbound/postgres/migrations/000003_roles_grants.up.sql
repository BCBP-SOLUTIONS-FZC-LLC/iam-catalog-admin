-- Single runtime DB role (LLD §9): catalog_admin_app has no BYPASSRLS grant
-- (there is nothing to bypass — neither table is RLS-protected) and no
-- migrator/admin_readonly split is needed (no cross-tenant data exists to
-- read around). SELECT is broad (mesh-readable, LLD §9); INSERT/UPDATE are
-- restricted to this role only. No DELETE grant — hard-delete is triggered
-- to fail regardless (OP-3/D-4), but the grant is withheld too, in defense
-- in depth, and to close the SELECT-only gap found in O&M's own grants
-- migration (department_repository writes were previously ungranted).
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
