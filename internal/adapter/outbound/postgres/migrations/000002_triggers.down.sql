DROP TRIGGER IF EXISTS trg_system_department_name_immutable ON public.departments;
DROP FUNCTION IF EXISTS public.prevent_system_department_name_change();

DROP TRIGGER IF EXISTS trg_department_code_immutable ON public.departments;
DROP FUNCTION IF EXISTS public.prevent_department_code_change();

DROP TRIGGER IF EXISTS trg_prevent_department_delete ON public.departments;
DROP FUNCTION IF EXISTS public.prevent_department_delete();

DROP TRIGGER IF EXISTS trg_touch_plans ON public.plans;
DROP TRIGGER IF EXISTS trg_touch_departments ON public.departments;
DROP FUNCTION IF EXISTS public.touch_row();
