BEGIN;

-- 1) Normalize obvious syntactic variants:
--    - menu:xxx -> xxx
--    - /a/b -> a_b
WITH normalized AS (
  SELECT
    group_id,
    item_type,
    item_code,
    CASE
      WHEN item_type = 'menu' AND item_code LIKE 'menu:%' THEN substring(item_code FROM 6)
      WHEN item_type = 'menu' AND item_code LIKE '/%' THEN lower(regexp_replace(replace(ltrim(item_code, '/'), '/', '_'), '[^a-zA-Z0-9_]', '_', 'g'))
      ELSE item_code
    END AS normalized_code
  FROM tenant_feature_group_items
)
UPDATE tenant_feature_group_items t
SET item_code = n.normalized_code,
    feature_code = n.normalized_code
FROM normalized n
WHERE t.group_id = n.group_id
  AND t.item_type = n.item_type
  AND t.item_code = n.item_code
  AND t.item_type = 'menu'
  AND n.normalized_code <> n.item_code;

WITH normalized AS (
  SELECT
    tenant_id,
    item_type,
    item_code,
    CASE
      WHEN item_type = 'menu' AND item_code LIKE 'menu:%' THEN substring(item_code FROM 6)
      WHEN item_type = 'menu' AND item_code LIKE '/%' THEN lower(regexp_replace(replace(ltrim(item_code, '/'), '/', '_'), '[^a-zA-Z0-9_]', '_', 'g'))
      ELSE item_code
    END AS normalized_code
  FROM tenant_feature_overrides
)
UPDATE tenant_feature_overrides t
SET item_code = n.normalized_code,
    feature_code = n.normalized_code
FROM normalized n
WHERE t.tenant_id = n.tenant_id
  AND t.item_type = n.item_type
  AND t.item_code = n.item_code
  AND t.item_type = 'menu'
  AND n.normalized_code <> n.item_code;

-- 2) Apply curated legacy-to-current mappings (includes old feature-like menu keys)
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
  ('doctor_center', 'doctor_recordings'),
  ('doctor_list', 'doctor_recordings_ability'),
  ('doctors', 'doctor_recordings_ability'),
  ('medical_doctor_list', 'doctor_recordings_ability'),
  ('medical_recording_center', 'doctor_recordings'),
  ('medical_recording_list', 'doctor_recordings'),
  ('medical_recording_settings', 'doctor_recordings'),
  ('medical_team_trends', 'doctor_recordings_team_trends'),
  ('medical_weekly_summary', 'doctor_recordings_weekly_summary'),
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
  ('tasks_generated', 'tasks'),
  ('tasks_recording', 'tasks'),
  ('tasks_recording_list', 'tasks'),
  ('tasks_recording_dashboard', 'tasks_board'),
  ('tasks_recording_partnerships', 'tasks_partnerships'),
  ('tasks_recording_assign', 'tasks_assignment'),
  ('system', 'departments'),
  ('system', 'roles'),
  ('system', 'menus');

WITH source_rows AS (
  SELECT
    t.group_id,
    'menu'::varchar(16) AS item_type,
    m.new_code AS item_code,
    bool_or(COALESCE(t.is_enabled, true)) AS is_enabled
  FROM tenant_feature_group_items t
  JOIN tmp_feature_code_mapping m ON m.old_code = t.item_code
  WHERE t.item_type = 'menu'
  GROUP BY t.group_id, m.new_code
)
INSERT INTO tenant_feature_group_items (group_id, item_type, item_code, feature_code, is_enabled, created_at)
SELECT group_id, item_type, item_code, item_code, is_enabled, NOW()
FROM source_rows
ON CONFLICT (group_id, item_type, item_code)
DO UPDATE SET is_enabled = tenant_feature_group_items.is_enabled OR EXCLUDED.is_enabled;

UPDATE tenant_feature_overrides o
SET item_code = m.new_code,
    feature_code = m.new_code
FROM tmp_feature_code_mapping m
WHERE o.item_type = 'menu'
  AND o.item_code = m.old_code
  AND o.item_code <> m.new_code;

-- 3) Remove menu items that still don't exist in current institution menu dictionary.
DELETE FROM tenant_feature_group_items t
WHERE t.item_type = 'menu'
  AND NOT EXISTS (
    SELECT 1 FROM inst_menus m
    WHERE COALESCE(m.is_active, true) = true
      AND m.code = t.item_code
  );

DELETE FROM tenant_feature_overrides t
WHERE t.item_type = 'menu'
  AND NOT EXISTS (
    SELECT 1 FROM inst_menus m
    WHERE COALESCE(m.is_active, true) = true
      AND m.code = t.item_code
  );

COMMIT;
