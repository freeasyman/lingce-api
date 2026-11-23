-- 电子病历模板：栏位级覆盖（租户专科模板的固定句式 / 显示名 / 填写方式）
--
-- 背景：栏目名称、输入类型、结构目前都来自全局 emr_fields（listSections 用 JOIN 读取）。
-- 眼视光专科模板需要在「同一 code、仅该模板」下替换句式（structure.template）、
-- 显示名（physical_exam -> 眼部检查）和填写方式（结构化 -> 长文本）。
--
-- 方案：在 emr_template_fields 增加三个可空覆盖列。老行为 NULL，读取时回退全局，
-- 因此不影响任何已有模板、病历与质检；只有显式填写覆盖的模板才走覆盖值。
--
-- 幂等：可重复执行。

ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS name_override      TEXT;
ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS input_type_override VARCHAR(32);
ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS structure_json     JSONB;
