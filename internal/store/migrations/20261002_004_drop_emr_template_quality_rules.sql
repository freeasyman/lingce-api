-- 模板质量规则绑定不再是当前配置来源。
-- 正式质检唯一读取 emr_fields.quality_rules，并通过 emr_quality_rules.field.* 取得规则身份。
-- 模板管理和模板发布校验已不再读写本表，因此删除旧的第二套规则来源。

DROP TABLE IF EXISTS public.emr_template_quality_rules;
