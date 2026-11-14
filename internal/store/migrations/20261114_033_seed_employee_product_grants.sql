-- 033：按岗位默认产品给存量员工补基础权限
--
-- 背景：门户「员工产品」只解决「看得到产品」，产品内权限没写，
-- 导致员工看得到卡片却进不去（403 access is disabled）。
-- 这里按岗位默认产品补一条基础权限（本人范围），已有记录不动。

-- 听写病历：医生、康复师
INSERT INTO emr_permission_assignments (tenant_id, employee_id, enabled, emr_role_code, scope, abilities_json, created_by, updated_by)
SELECT e.tenant_id, e.id, TRUE, 'doctor', 'self',
       '["record.read","record.create","record.edit","record.submit","record.history.read"]'::jsonb, 0, 0
FROM employees e
JOIN institution_employee_roles ier ON ier.employee_id = e.id AND ier.tenant_id = e.tenant_id
WHERE e.deleted_at IS NULL
  AND lower(trim(COALESCE(ier.role_code, ''))) IN ('doctor', 'therapist')
  AND NOT EXISTS (
      SELECT 1 FROM emr_permission_assignments x WHERE x.tenant_id = e.tenant_id AND x.employee_id = e.id
  )
ON CONFLICT (tenant_id, employee_id) DO NOTHING;

-- 随访顾问：医生、客服
INSERT INTO followup_permission_assignments (tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
SELECT e.tenant_id, e.id, TRUE, 'followup_staff',
       '["followup.task.read","followup.task.execute"]'::jsonb, 0, 0
FROM employees e
JOIN institution_employee_roles ier ON ier.employee_id = e.id AND ier.tenant_id = e.tenant_id
WHERE e.deleted_at IS NULL
  AND lower(trim(COALESCE(ier.role_code, ''))) IN ('doctor', 'customer_service')
  AND NOT EXISTS (
      SELECT 1 FROM followup_permission_assignments x WHERE x.tenant_id = e.tenant_id AND x.employee_id = e.id
  )
ON CONFLICT (tenant_id, employee_id) DO NOTHING;

-- 合规卫士：医务
INSERT INTO compliance_permission_assignments (tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
SELECT e.tenant_id, e.id, TRUE, 'compliance_staff',
       '["compliance.finding.read","compliance.rule.read","compliance.run.read"]'::jsonb, 0, 0
FROM employees e
JOIN institution_employee_roles ier ON ier.employee_id = e.id AND ier.tenant_id = e.tenant_id
WHERE e.deleted_at IS NULL
  AND lower(trim(COALESCE(ier.role_code, ''))) = 'medical_affairs'
  AND NOT EXISTS (
      SELECT 1 FROM compliance_permission_assignments x WHERE x.tenant_id = e.tenant_id AND x.employee_id = e.id
  )
ON CONFLICT (tenant_id, employee_id) DO NOTHING;

-- 赢客专家：咨询师
INSERT INTO deal_expert_permission_assignments (tenant_id, employee_id, enabled, role_code, abilities_json, data_scope, created_by, updated_by)
SELECT e.tenant_id, e.id, TRUE, 'deal_staff',
       '["deal.consultation.read","deal.task.read","deal.task.execute","deal.workbench.read"]'::jsonb, 'self', 0, 0
FROM employees e
JOIN institution_employee_roles ier ON ier.employee_id = e.id AND ier.tenant_id = e.tenant_id
WHERE e.deleted_at IS NULL
  AND lower(trim(COALESCE(ier.role_code, ''))) = 'consultant'
  AND NOT EXISTS (
      SELECT 1 FROM deal_expert_permission_assignments x WHERE x.tenant_id = e.tenant_id AND x.employee_id = e.id
  )
ON CONFLICT (tenant_id, employee_id) DO NOTHING;

-- 客户总监：客服、运营
INSERT INTO patient_ops_permission_assignments (tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
SELECT e.tenant_id, e.id, TRUE, 'patient_ops_staff',
       '["patient_ops.tasks.read_all"]'::jsonb, 0, 0
FROM employees e
JOIN institution_employee_roles ier ON ier.employee_id = e.id AND ier.tenant_id = e.tenant_id
WHERE e.deleted_at IS NULL
  AND lower(trim(COALESCE(ier.role_code, ''))) IN ('customer_service', 'operations')
  AND NOT EXISTS (
      SELECT 1 FROM patient_ops_permission_assignments x WHERE x.tenant_id = e.tenant_id AND x.employee_id = e.id
  )
ON CONFLICT (tenant_id, employee_id) DO NOTHING;
