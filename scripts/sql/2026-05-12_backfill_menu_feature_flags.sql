BEGIN;

ALTER TABLE IF EXISTS inst_menus ADD COLUMN IF NOT EXISTS is_feature_assignable BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE IF EXISTS inst_menus ADD COLUMN IF NOT EXISTS is_default_for_admin BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE IF EXISTS inst_menus ADD COLUMN IF NOT EXISTS feature_code VARCHAR(128);
ALTER TABLE IF EXISTS inst_menus ADD COLUMN IF NOT EXISTS feature_name VARCHAR(128);

INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
SELECT 'frontdesk_recordings', '录音列表', '/frontdesk-recordings', 1151, true, NOW()
WHERE NOT EXISTS (SELECT 1 FROM inst_menus WHERE code = 'frontdesk_recordings');

INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
SELECT 'knowledge', '知识条目', '/knowledge', 1450, true, NOW()
WHERE NOT EXISTS (SELECT 1 FROM inst_menus WHERE code = 'knowledge');

UPDATE inst_menus
SET is_feature_assignable = false,
    is_default_for_admin = false,
    feature_code = NULL,
    feature_name = NULL;

UPDATE inst_menus
SET name = '录音列表',
    path = '/doctor-recordings',
    is_active = true,
    is_feature_assignable = true,
    is_default_for_admin = true,
    feature_code = 'medical_recording_center',
    feature_name = '医疗录音'
WHERE code = 'doctor_recordings';

UPDATE inst_menus
SET name = '医生列表',
    path = '/doctor-recordings/ability',
    is_active = true,
    is_feature_assignable = true,
    is_default_for_admin = true,
    feature_code = 'medical_recording_center',
    feature_name = '医疗录音'
WHERE code = 'doctor_recordings_ability';

UPDATE inst_menus
SET name = '团队趋势',
    path = '/doctor-recordings/team-trends',
    is_active = true,
    is_feature_assignable = true,
    is_default_for_admin = true,
    feature_code = 'medical_recording_center',
    feature_name = '医疗录音'
WHERE code = 'doctor_recordings_team_trends';

UPDATE inst_menus
SET name = '本周小结',
    path = '/doctor-recordings/weekly-summary',
    is_active = true,
    is_feature_assignable = true,
    is_default_for_admin = true,
    feature_code = 'medical_recording_center',
    feature_name = '医疗录音'
WHERE code = 'doctor_recordings_weekly_summary';

UPDATE inst_menus
SET is_active = true,
    is_feature_assignable = true,
    is_default_for_admin = true,
    feature_code = 'recording_center',
    feature_name = '咨询录音'
WHERE code IN ('consultant_recordings', 'consultant_recordings_team_ability', 'consultant_recordings_dashboard');

UPDATE inst_menus
SET name = '录音列表',
    path = '/frontdesk-recordings',
    order_index = COALESCE(order_index, 1151),
    is_active = true,
    is_feature_assignable = true,
    is_default_for_admin = true,
    feature_code = 'frontdesk_recording_center',
    feature_name = '前台录音'
WHERE code = 'frontdesk_recordings';

UPDATE inst_menus
SET is_active = true,
    is_feature_assignable = true,
    is_default_for_admin = true,
    feature_code = 'tasks_recording',
    feature_name = '录音任务'
WHERE code IN ('tasks', 'tasks_board', 'tasks_partnerships', 'tasks_assignment');

UPDATE inst_menus
SET is_active = true,
    is_feature_assignable = true,
    is_default_for_admin = true,
    feature_code = 'customer_center',
    feature_name = '客户中心'
WHERE code = 'customers';

UPDATE inst_menus
SET is_active = true,
    is_feature_assignable = true,
    is_default_for_admin = true,
    feature_code = 'content_center',
    feature_name = '内容中心'
WHERE code IN ('content_create', 'content_seeds', 'content_library');

UPDATE inst_menus
SET name = '知识条目',
    path = '/knowledge',
    order_index = COALESCE(order_index, 1450),
    is_active = true,
    is_feature_assignable = true,
    is_default_for_admin = true,
    feature_code = 'knowledge_center',
    feature_name = '知识库'
WHERE code = 'knowledge';

UPDATE inst_menus
SET is_active = true,
    is_feature_assignable = true,
    is_default_for_admin = true,
    feature_code = 'smart_badge',
    feature_name = '工牌管理'
WHERE code IN ('badges_overview', 'badges_recording_control', 'badges_recording_stats', 'badges_tickets');

UPDATE inst_menus
SET is_active = true,
    is_feature_assignable = true,
    is_default_for_admin = true,
    feature_code = 'system_management',
    feature_name = '系统管理'
WHERE code IN ('departments', 'roles', 'menus');

UPDATE tenant_feature_group_items
SET item_code = CASE item_code
  WHEN 'knowledge-list' THEN 'knowledge'
  WHEN 'knowledge_center' THEN 'knowledge'
  WHEN 'customer-list' THEN 'customers'
  WHEN 'content-workbench' THEN 'content_create'
  WHEN 'content-list' THEN 'content_library'
  WHEN 'frontdesk' THEN 'frontdesk_recordings'
  WHEN 'frontdesk_recording_list' THEN 'frontdesk_recordings'
  WHEN 'recording_list' THEN 'consultant_recordings'
  WHEN 'recording_team_ability' THEN 'consultant_recordings_team_ability'
  WHEN 'recording_dashboard' THEN 'consultant_recordings_dashboard'
  WHEN 'medical_recording_list' THEN 'doctor_recordings'
  WHEN 'medical_doctor_list' THEN 'doctor_recordings_ability'
  WHEN 'medical_team_trends' THEN 'doctor_recordings_team_trends'
  WHEN 'medical_weekly_summary' THEN 'doctor_recordings_weekly_summary'
  WHEN 'tasks_recording_list' THEN 'tasks'
  WHEN 'tasks_recording_dashboard' THEN 'tasks_board'
  WHEN 'tasks_recording_partnerships' THEN 'tasks_partnerships'
  WHEN 'tasks_recording_assign' THEN 'tasks_assignment'
  WHEN 'recordings_smart_badge_overview' THEN 'badges_overview'
  WHEN 'recordings_smart_badge_recording_control' THEN 'badges_recording_control'
  WHEN 'recording_stats' THEN 'badges_recording_stats'
  WHEN 'recordings_smart_badge_tickets' THEN 'badges_tickets'
  WHEN 'organization' THEN 'departments'
  ELSE item_code
END
WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu';

UPDATE tenant_feature_overrides
SET item_code = CASE item_code
  WHEN 'knowledge-list' THEN 'knowledge'
  WHEN 'knowledge_center' THEN 'knowledge'
  WHEN 'customer-list' THEN 'customers'
  WHEN 'content-workbench' THEN 'content_create'
  WHEN 'content-list' THEN 'content_library'
  WHEN 'frontdesk' THEN 'frontdesk_recordings'
  WHEN 'frontdesk_recording_list' THEN 'frontdesk_recordings'
  WHEN 'recording_list' THEN 'consultant_recordings'
  WHEN 'recording_team_ability' THEN 'consultant_recordings_team_ability'
  WHEN 'recording_dashboard' THEN 'consultant_recordings_dashboard'
  WHEN 'medical_recording_list' THEN 'doctor_recordings'
  WHEN 'medical_doctor_list' THEN 'doctor_recordings_ability'
  WHEN 'medical_team_trends' THEN 'doctor_recordings_team_trends'
  WHEN 'medical_weekly_summary' THEN 'doctor_recordings_weekly_summary'
  WHEN 'tasks_recording_list' THEN 'tasks'
  WHEN 'tasks_recording_dashboard' THEN 'tasks_board'
  WHEN 'tasks_recording_partnerships' THEN 'tasks_partnerships'
  WHEN 'tasks_recording_assign' THEN 'tasks_assignment'
  WHEN 'recordings_smart_badge_overview' THEN 'badges_overview'
  WHEN 'recordings_smart_badge_recording_control' THEN 'badges_recording_control'
  WHEN 'recording_stats' THEN 'badges_recording_stats'
  WHEN 'recordings_smart_badge_tickets' THEN 'badges_tickets'
  WHEN 'organization' THEN 'departments'
  ELSE item_code
END
WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu';

DELETE FROM tenant_feature_group_items
WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
  AND COALESCE(NULLIF(item_code, ''), '') NOT IN (
    SELECT code FROM inst_menus WHERE COALESCE(is_feature_assignable, false) = true
  );

DELETE FROM tenant_feature_overrides
WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
  AND COALESCE(NULLIF(item_code, ''), '') NOT IN (
    SELECT code FROM inst_menus WHERE COALESCE(is_feature_assignable, false) = true
  );

INSERT INTO inst_role_menus (tenant_id, role_code, menu_id, created_at)
SELECT DISTINCT r.tenant_id, 'admin', m.id, NOW()
FROM institution_roles r
JOIN inst_menus m
  ON COALESCE(m.is_active, true) = true
 AND COALESCE(m.is_default_for_admin, false) = true
WHERE lower(r.code) = 'admin'
  AND NOT EXISTS (
    SELECT 1
    FROM inst_role_menus rm
    WHERE rm.tenant_id = r.tenant_id
      AND lower(rm.role_code) = 'admin'
      AND rm.menu_id = m.id
  );

COMMIT;
