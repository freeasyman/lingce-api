-- 020：机构身份收敛为 7 岗位 + admin（Phase 0 · 身份迁移）
--
-- 背景：旧 role_code 有 20 个不同值（定义表 + 分配表两套、含层级角色/灵策内部/孤儿）。
-- 目标：收敛到 7 岗位 + admin 身份（8 个 code）。
-- 目标 code：doctor/consultant/customer_service/publicity/therapist/medical_affairs/operations/admin

-- 1) 给所有未删除租户补齐 8 个目标 code 定义
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

-- 2) 迁移分配表 role_code（director/lingce_sales 不自动迁移，留人工）
UPDATE institution_employee_roles SET role_code = 'doctor'          WHERE role_code = 'doctor_assistant';
UPDATE institution_employee_roles SET role_code = 'consultant'      WHERE role_code IN ('consultant_leader', 'sales_manager');
UPDATE institution_employee_roles SET role_code = 'customer_service' WHERE role_code IN ('receptionist', 'reception_manager', 'followup', 'staff', 'employee');
UPDATE institution_employee_roles SET role_code = 'publicity'       WHERE role_code IN ('editor', 'marketing_manager');
UPDATE institution_employee_roles SET role_code = 'operations'      WHERE role_code IN ('operating_manager', 'operating_supervisor');

-- 3) role_id 对齐到新 code 定义
UPDATE institution_employee_roles ier
SET role_id = r.id
FROM institution_roles r
WHERE r.tenant_id = ier.tenant_id
  AND r.code = ier.role_code
  AND r.deleted_at IS NULL
  AND ier.role_code IN ('doctor','consultant','customer_service','publicity','therapist','medical_affairs','operations','admin');

-- 4) 停用非目标 code（director/lingce_sales 除外，留人工处理）
UPDATE institution_roles
SET is_active = FALSE, deleted_at = NOW()
WHERE deleted_at IS NULL
  AND code NOT IN ('doctor','consultant','customer_service','publicity','therapist','medical_affairs','operations','admin')
  AND code <> 'director';
