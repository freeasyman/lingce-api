-- 025：旧字典表归档
--
-- billing_department_dict / clinical_department_dict 的数据已合并进 organization_department_dict，
-- 代码已全部改读新表。这里把旧表改名归档（保留数据可追溯，不再被代码引用）。

ALTER TABLE IF EXISTS billing_department_dict RENAME TO billing_department_dict_legacy;
ALTER TABLE IF EXISTS clinical_department_dict RENAME TO clinical_department_dict_legacy;
