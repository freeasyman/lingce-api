-- 版本号风格统一：v1 → 1.0（与平台原有模板一致）。
UPDATE public.emr_templates SET version_no = '1.0', updated_at = NOW() WHERE version_no = 'v1';
