-- 027：默认全开放——把所有标准部门开给存量机构
--
-- 新建机构已由 ensureDefaultDepartmentsTx 全量落库；这里给存量机构补齐。
-- 只补缺失项（按 dict_code 或 code 判重），不动已有部门与自定义部门。

INSERT INTO departments (tenant_id, name, code, dict_code, parent_id, is_active, created_at, updated_at)
SELECT t.id, od.name, od.code, od.code, NULL, true, NOW(), NOW()
FROM tenants t
CROSS JOIN organization_department_dict od
WHERE t.deleted_at IS NULL
  AND od.is_active
  AND NOT EXISTS (
      SELECT 1 FROM departments d
      WHERE d.tenant_id = t.id
        AND d.deleted_at IS NULL
        AND (d.dict_code = od.code OR d.code = od.code)
  )
ON CONFLICT DO NOTHING;
