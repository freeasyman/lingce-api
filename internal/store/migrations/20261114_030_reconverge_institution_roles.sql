-- 030：新机构角色再收敛（幂等重跑 020 的口径）
--
-- 背景：020 之后新建的机构仍从旧 inst_roles 复制，出现 14 个历史角色。
-- 这里给所有租户补齐 7 岗位 + admin，并把历史分配码迁到标准码、停用非标准角色。

INSERT INTO institution_roles (tenant_id, name, code, is_active)
SELECT t.id, v.name, v.code, TRUE
FROM tenants t
CROSS JOIN (VALUES
    ('医生','doctor'),('咨询师','consultant'),('客服','customer_service'),
    ('宣传','publicity'),('康复师','therapist'),('医务','medical_affairs'),
    ('运营','operations'),('机构管理员','admin')
) AS v(name, code)
WHERE t.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM institution_roles r
      WHERE r.tenant_id = t.id AND r.code = v.code AND r.deleted_at IS NULL
  );

UPDATE institution_employee_roles SET role_code = 'doctor'          WHERE role_code = 'doctor_assistant';
UPDATE institution_employee_roles SET role_code = 'consultant'      WHERE role_code IN ('consultant_leader', 'sales_manager');
UPDATE institution_employee_roles SET role_code = 'customer_service' WHERE role_code IN ('receptionist', 'reception_manager', 'followup', 'staff', 'employee');
UPDATE institution_employee_roles SET role_code = 'publicity'       WHERE role_code IN ('editor', 'marketing_manager');
UPDATE institution_employee_roles SET role_code = 'operations'      WHERE role_code IN ('operating_manager', 'operating_supervisor');

UPDATE institution_employee_roles ier
SET role_id = r.id
FROM institution_roles r
WHERE r.tenant_id = ier.tenant_id
  AND r.code = ier.role_code
  AND r.deleted_at IS NULL
  AND ier.role_code IN ('doctor','consultant','customer_service','publicity','therapist','medical_affairs','operations','admin');

UPDATE institution_roles
SET is_active = FALSE, deleted_at = NOW()
WHERE deleted_at IS NULL
  AND code NOT IN ('doctor','consultant','customer_service','publicity','therapist','medical_affairs','operations','admin')
  AND code <> 'director'
  AND NOT EXISTS (
      SELECT 1 FROM institution_employee_roles ier
      WHERE ier.tenant_id = institution_roles.tenant_id AND ier.role_code = institution_roles.code
  );
