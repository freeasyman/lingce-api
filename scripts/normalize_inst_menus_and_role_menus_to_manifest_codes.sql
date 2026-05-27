BEGIN;

-- 1) Ensure inst_menus has manifest-based menu codes used by tenant feature policy.
CREATE TEMP TABLE tmp_manifest_menus (
  code varchar(64) PRIMARY KEY,
  name varchar(128) NOT NULL,
  path varchar(255) NOT NULL,
  order_index integer NOT NULL
) ON COMMIT DROP;

INSERT INTO tmp_manifest_menus (code, name, path, order_index) VALUES
  ('learning_center_benchmarks', '标杆学习', '/learning-center/benchmarks', 901),
  ('management_dashboard_overview', '团队总览', '/management-dashboard/overview', 951),
  ('management_dashboard_tracking', '变化追踪', '/management-dashboard/tracking', 952),
  ('management_dashboard_benchmarks', '标杆库', '/management-dashboard/benchmarks', 953),
  ('management_dashboard_risks', '风险与待办', '/management-dashboard/risks', 954),
  ('management_dashboard_coaching_tasks', '辅导任务', '/management-dashboard/coaching-tasks', 955),
  ('management_dashboard_meetings_morning', '早会', '/management-dashboard/meetings/morning', 956),
  ('management_dashboard_meetings_weekly', '周会', '/management-dashboard/meetings/weekly', 957),
  ('doctor_recordings', '录音列表', '/doctor-recordings', 1001),
  ('doctor_recordings_ability', '医生列表', '/doctor-recordings/ability', 1002),
  ('doctor_recordings_team_trends', '团队趋势', '/doctor-recordings/team-trends', 1003),
  ('doctor_recordings_weekly_summary', '本周小结', '/doctor-recordings/weekly-summary', 1004),
  ('consultant_recordings', '录音列表', '/consultant-recordings', 1101),
  ('consultant_recordings_team_ability', '团队能力', '/consultant-recordings/team-ability', 1102),
  ('consultant_recordings_dashboard', '经营看板', '/consultant-recordings/dashboard', 1103),
  ('frontdesk_recordings', '录音列表', '/frontdesk-recordings', 1151),
  ('therapist_recordings', '录音列表', '/therapist-recordings', 1152),
  ('tasks', '任务列表', '/tasks', 1201),
  ('tasks_board', '任务看板', '/tasks/board', 1202),
  ('tasks_partnerships', '主责人与执行人配置', '/tasks/partnerships', 1203),
  ('tasks_assignment', '任务分配', '/tasks/assignment', 1204),
  ('customers', '客户列表', '/customers', 1301),
  ('content_create', '内容创作', '/content/create', 1401),
  ('content_seeds', '选题素材', '/content/seeds', 1402),
  ('content_library', '内容库', '/content/library', 1403),
  ('content_topics', '话题管理', '/content/topics', 1404),
  ('knowledge', '知识条目', '/knowledge', 1450),
  ('badges_overview', '设备概览', '/badges/overview', 1501),
  ('badges_recording_control', '录音控制', '/badges/recording-control', 1502),
  ('badges_recording_stats', '录音统计', '/badges/recording-stats', 1503),
  ('badges_tickets', '报修工单', '/badges/tickets', 1504),
  ('departments', '团队管理', '/departments', 1601),
  ('roles', '角色权限', '/roles', 1602),
  ('menus', '菜单管理', '/menus', 1603);

INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
SELECT t.code, t.name, t.path, t.order_index, true, NOW()
FROM tmp_manifest_menus t
LEFT JOIN inst_menus m ON m.code = t.code
WHERE m.id IS NULL;

UPDATE inst_menus m
SET name = t.name,
    path = t.path,
    order_index = t.order_index,
    is_active = true
FROM tmp_manifest_menus t
WHERE m.code = t.code;

-- 2) Map legacy menu codes to new manifest codes for role-menu bindings.
CREATE TEMP TABLE tmp_code_mapping (
  old_code varchar(64) NOT NULL,
  new_code varchar(64) NOT NULL
) ON COMMIT DROP;

INSERT INTO tmp_code_mapping (old_code, new_code) VALUES
  ('doctor_center', 'doctor_recordings'),
  ('doctor_list', 'doctor_recordings_ability'),
  ('doctors', 'doctor_recordings_ability'),
  ('medical_doctor_list', 'doctor_recordings_ability'),
  ('medical_recording_center', 'doctor_recordings'),
  ('medical_recording_list', 'doctor_recordings'),
  ('medical_recording_settings', 'doctor_recordings'),
  ('medical_team_trends', 'doctor_recordings_team_trends'),
  ('medical_weekly_summary', 'doctor_recordings_weekly_summary'),
  ('recording_center', 'consultant_recordings'),
  ('recording_list', 'consultant_recordings'),
  ('recording_team_ability', 'consultant_recordings_team_ability'),
  ('recording_dashboard', 'consultant_recordings_dashboard'),
  ('frontdesk', 'frontdesk_recordings'),
  ('frontdesk_recordings', 'frontdesk_recordings'),
  ('frontdesk_recording_list', 'frontdesk_recordings'),
  ('tasks_generated', 'tasks'),
  ('tasks_recording', 'tasks'),
  ('tasks_recording_list', 'tasks'),
  ('tasks_recording_dashboard', 'tasks_board'),
  ('tasks_recording_partnerships', 'tasks_partnerships'),
  ('tasks_recording_assign', 'tasks_assignment'),
  ('content-center', 'content_create'),
  ('content-workbench', 'content_create'),
  ('content-recording-seeds', 'content_seeds'),
  ('content-list', 'content_library'),
  ('content-topics', 'content_topics'),
  ('knowledge-list', 'knowledge'),
  ('knowledge_center', 'knowledge'),
  ('customer-list', 'customers'),
  ('recording_badge_management', 'badges_overview'),
  ('recordings_smart_badge_devices', 'badges_overview'),
  ('recordings_smart_badge_overview', 'badges_overview'),
  ('recordings_smart_badge_recording_control', 'badges_recording_control'),
  ('recording_stats', 'badges_recording_stats'),
  ('recordings_smart_badge_tickets', 'badges_tickets'),
  ('system', 'departments'),
  ('system', 'roles'),
  ('system', 'menus');

WITH mapped AS (
  SELECT DISTINCT
    rm.tenant_id,
    rm.role_code,
    newm.id AS new_menu_id
  FROM inst_role_menus rm
  JOIN inst_menus oldm ON oldm.id = rm.menu_id
  JOIN tmp_code_mapping map ON map.old_code = oldm.code
  JOIN inst_menus newm ON newm.code = map.new_code
)
INSERT INTO inst_role_menus (tenant_id, role_code, menu_id, created_at)
SELECT tenant_id, role_code, new_menu_id, NOW()
FROM mapped
ON CONFLICT (tenant_id, role_code, menu_id) DO NOTHING;

COMMIT;
