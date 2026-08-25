REVOKE ALL ON public.plans FROM catalog_admin_app;
REVOKE ALL ON public.departments FROM catalog_admin_app;
REVOKE USAGE ON SCHEMA public FROM catalog_admin_app;

DROP TRIGGER IF EXISTS trg_system_department_name_immutable ON public.departments;
DROP FUNCTION IF EXISTS public.prevent_system_department_name_change();

DROP TRIGGER IF EXISTS trg_department_code_immutable ON public.departments;
DROP FUNCTION IF EXISTS public.prevent_department_code_change();

DROP TRIGGER IF EXISTS trg_prevent_department_delete ON public.departments;
DROP FUNCTION IF EXISTS public.prevent_department_delete();

DROP TRIGGER IF EXISTS trg_touch_plans ON public.plans;
DROP TRIGGER IF EXISTS trg_touch_departments ON public.departments;
DROP FUNCTION IF EXISTS public.touch_row();

DROP TABLE IF EXISTS public.plans;
DROP TABLE IF EXISTS public.departments;
DROP TYPE IF EXISTS public.branding_level;
DROP TYPE IF EXISTS public.tenant_plan;
