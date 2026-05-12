BEGIN;

ALTER TABLE IF EXISTS inst_menus ADD COLUMN IF NOT EXISTS is_feature_assignable BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE IF EXISTS inst_menus ADD COLUMN IF NOT EXISTS is_default_for_admin BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE IF EXISTS inst_menus ADD COLUMN IF NOT EXISTS feature_code VARCHAR(128);
ALTER TABLE IF EXISTS inst_menus ADD COLUMN IF NOT EXISTS feature_name VARCHAR(128);

-- Align feature-group actionable menu codes with institution frontend route codes.
-- Safe to run repeatedly in dev/prod.

INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
SELECT 'frontdesk_recordings', '录音列表', '/frontdesk-recordings', 1151, true, NOW()
WHERE NOT EXISTS (
  SELECT 1 FROM inst_menus WHERE code = 'frontdesk_recordings'
);

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

INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
SELECT 'knowledge', '知识条目', '/knowledge', 1450, true, NOW()
WHERE NOT EXISTS (
  SELECT 1 FROM inst_menus WHERE code = 'knowledge'
);

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

COMMIT;
