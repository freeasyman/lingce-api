-- 给既有的患者运营管理员补充“转交待办”权限；新库由 20261019_007 种子后再执行本迁移。
UPDATE patient_ops_permission_assignments
SET abilities_json = abilities_json || '["patient_ops.tasks.assign"]'::jsonb,
    updated_at = NOW()
WHERE enabled = TRUE
  AND role_code = 'patient_ops_admin'
  AND NOT (abilities_json ? 'patient_ops.tasks.assign');
