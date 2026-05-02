BEGIN;

-- Migrate legacy menu item codes in tenant_feature_group_items
-- to the current institution frontend menu-code system.
CREATE TEMP TABLE tmp_feature_code_mapping (
  old_code varchar(128) NOT NULL,
  new_code varchar(128) NOT NULL
) ON COMMIT DROP;

INSERT INTO tmp_feature_code_mapping (old_code, new_code)
VALUES
    ('content-center', 'content_create'),
    ('content-list', 'content_library'),
    ('content-recording-seeds', 'content_seeds'),
    ('content-topics', 'content_topics'),
    ('content-workbench', 'content_create'),

    ('customer-list', 'customers'),
    ('customers', 'customers'),

    ('doctor_center', 'doctor_recordings'),
    ('doctor_list', 'doctor_recordings_ability'),
    ('doctors', 'doctor_recordings_ability'),
    ('medical_doctor_list', 'doctor_recordings_ability'),
    ('medical_recording_center', 'doctor_recordings'),
    ('medical_recording_list', 'doctor_recordings'),
    ('medical_recording_settings', 'doctor_recordings'),
    ('medical_team_trends', 'doctor_recordings_team_trends'),
    ('medical_weekly_summary', 'doctor_recordings_weekly_summary'),

    ('menus', 'menus'),
    ('roles', 'roles'),
    ('system', 'departments'),
    ('system', 'roles'),
    ('system', 'menus'),

    ('organization', 'organization_profile'),

    ('recording_badge_management', 'badges_overview'),
    ('recordings_smart_badge_devices', 'badges_overview'),
    ('recordings_smart_badge_overview', 'badges_overview'),
    ('recordings_smart_badge_recording_control', 'badges_recording_control'),
    ('recordings_smart_badge_tickets', 'badges_tickets'),
    ('recording_stats', 'badges_recording_stats'),

    ('recording_center', 'consultant_recordings'),
    ('recording_list', 'consultant_recordings'),
    ('recording_team_ability', 'consultant_recordings_team_ability'),
    ('recording_dashboard', 'consultant_recordings_dashboard'),

    ('tasks', 'tasks'),
    ('tasks_generated', 'tasks'),
    ('tasks_recording', 'tasks'),
    ('tasks_recording_list', 'tasks'),
    ('tasks_recording_dashboard', 'tasks_board'),
    ('tasks_recording_partnerships', 'tasks_partnerships'),
    ('tasks_recording_assign', 'tasks_assignment');

WITH
source_rows AS (
  SELECT
    t.group_id,
    'menu'::varchar(16) AS item_type,
    m.new_code,
    bool_or(COALESCE(t.is_enabled, true)) AS is_enabled
  FROM tenant_feature_group_items t
  JOIN tmp_feature_code_mapping m
    ON m.old_code = t.item_code
  WHERE t.item_type = 'menu'
  GROUP BY t.group_id, m.new_code
)
INSERT INTO tenant_feature_group_items (group_id, item_type, item_code, feature_code, is_enabled, created_at)
SELECT
  s.group_id,
  s.item_type,
  s.new_code,
  s.new_code,
  s.is_enabled,
  NOW()
FROM source_rows s
ON CONFLICT (group_id, item_type, item_code)
DO UPDATE SET
  is_enabled = tenant_feature_group_items.is_enabled OR EXCLUDED.is_enabled;

DELETE FROM tenant_feature_group_items
WHERE item_type = 'menu'
  AND item_code IN (
    SELECT DISTINCT old_code
    FROM tmp_feature_code_mapping
    WHERE old_code <> new_code
  );

COMMIT;
