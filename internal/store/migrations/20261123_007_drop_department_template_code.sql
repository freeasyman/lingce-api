-- 科室→模板单列映射机制退役：匹配统一按 科室 code + 就诊类型（统一解析器）。
ALTER TABLE public.departments DROP COLUMN IF EXISTS template_code;
ALTER TABLE public.organization_department_dict DROP COLUMN IF EXISTS template_code;
