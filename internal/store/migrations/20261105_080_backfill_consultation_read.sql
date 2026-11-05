-- 新增「查看接诊记录」(consultation.read) 能力后，为已有启用 EMR 的账号补齐该能力，
-- 避免升级后医生/归档审核/只读等账号突然看不到接诊记录、听不了录音。
-- 管理员(emr_admin) 由后端 Access.Has 全能力兜底，但这里一并补齐也无妨。

UPDATE emr_permission_assignments
SET abilities_json = abilities_json || '["consultation.read"]'::jsonb,
    updated_at = NOW()
WHERE enabled = TRUE
  AND NOT (abilities_json @> '["consultation.read"]'::jsonb);
