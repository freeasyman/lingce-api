-- 工牌：把单一 status 拆成 position(在哪) + availability(能不能用)
-- 兼容策略：status 保留，触发器与两列双向同步；老前端/老接口行为不变。
-- 顺序：加列 → 回填 → 约束 → 索引(单独) → 触发器（必须在回填之后建，避免回填改写 status）

-- 1) 加列（瞬时，可逆）
ALTER TABLE badge_devices ADD COLUMN IF NOT EXISTS position text;
ALTER TABLE badge_devices ADD COLUMN IF NOT EXISTS availability text;

-- 2) 回填
--    position 只按事实映射；legacy retired 不等同于报废，统一落到 inactive 待人工定性。
--    availability 用证据判断：健康快照 + 曾验收通过 + 近 30 天心跳；推不出来一律 pending_check。
UPDATE badge_devices
SET position = CASE status
      WHEN 'assigned' THEN 'in_use'
      WHEN 'retired'  THEN 'inactive'
      ELSE 'warehouse' END,
    availability = CASE
      WHEN health_status = 'error' THEN 'unusable'
      WHEN accepted_at IS NOT NULL
       AND health_status IN ('healthy', 'warning')
       AND last_online_at IS NOT NULL
       AND last_online_at > now() - interval '30 days' THEN 'available'
      ELSE 'pending_check' END
WHERE deleted_at IS NULL;

UPDATE badge_devices
SET position = COALESCE(position, CASE status WHEN 'assigned' THEN 'in_use' ELSE 'warehouse' END),
    availability = COALESCE(availability, 'pending_check')
WHERE position IS NULL OR availability IS NULL;

-- 3) 约束（NOT VALID + VALIDATE，避免长时间持锁）
ALTER TABLE badge_devices DROP CONSTRAINT IF EXISTS ck_badge_devices_position;
ALTER TABLE badge_devices ADD CONSTRAINT ck_badge_devices_position
  CHECK (position IN ('in_transit','warehouse','in_use','to_return','vendor','scrapped','lost','inactive')) NOT VALID;
ALTER TABLE badge_devices VALIDATE CONSTRAINT ck_badge_devices_position;

ALTER TABLE badge_devices DROP CONSTRAINT IF EXISTS ck_badge_devices_availability;
ALTER TABLE badge_devices ADD CONSTRAINT ck_badge_devices_availability
  CHECK (availability IN ('pending_check','pending_config','available','unusable')) NOT VALID;
ALTER TABLE badge_devices VALIDATE CONSTRAINT ck_badge_devices_availability;

ALTER TABLE badge_devices DROP CONSTRAINT IF EXISTS nn_badge_devices_position;
ALTER TABLE badge_devices ADD CONSTRAINT nn_badge_devices_position CHECK (position IS NOT NULL) NOT VALID;
ALTER TABLE badge_devices VALIDATE CONSTRAINT nn_badge_devices_position;

ALTER TABLE badge_devices DROP CONSTRAINT IF EXISTS nn_badge_devices_availability;
ALTER TABLE badge_devices ADD CONSTRAINT nn_badge_devices_availability CHECK (availability IS NOT NULL) NOT VALID;
ALTER TABLE badge_devices VALIDATE CONSTRAINT nn_badge_devices_availability;

ALTER TABLE badge_devices ALTER COLUMN position SET NOT NULL;
ALTER TABLE badge_devices ALTER COLUMN availability SET NOT NULL;

-- 4) 索引另跑（CONCURRENTLY 不能进事务）：
--    CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_badge_devices_position_availability
--      ON badge_devices(position, availability) WHERE deleted_at IS NULL;

-- 5) 双向同步
CREATE OR REPLACE FUNCTION badge_devices_sync_legacy_status() RETURNS trigger AS $$
BEGIN
  IF NEW.position IS DISTINCT FROM OLD.position OR NEW.availability IS DISTINCT FROM OLD.availability THEN
    -- 新接口写两列 → 反推旧 status
    NEW.status := CASE
      WHEN NEW.position = 'in_use' THEN 'assigned'
      WHEN NEW.position IN ('to_return', 'vendor') THEN 'returned'
      WHEN NEW.position IN ('scrapped', 'lost', 'inactive') THEN 'retired'
      WHEN NEW.position = 'warehouse' AND NEW.availability = 'available' THEN 'in_stock'
      WHEN NEW.position = 'warehouse' AND NEW.availability = 'unusable' THEN 'returned'
      ELSE 'pending_acceptance'
    END;
  ELSIF NEW.status IS DISTINCT FROM OLD.status THEN
    -- 老接口写 status → 同步两列
    NEW.position := CASE NEW.status
      WHEN 'assigned' THEN 'in_use'
      WHEN 'retired'  THEN 'inactive'
      ELSE 'warehouse' END;
    NEW.availability := CASE NEW.status
      WHEN 'in_stock' THEN 'available'
      WHEN 'assigned' THEN 'available'
      WHEN 'pending_acceptance' THEN 'pending_check'
      WHEN 'returned' THEN 'pending_check'
      ELSE 'unusable' END;
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;

-- 6) 新增设备兼容：老代码 INSERT 只写 status，插入触发器补齐两列。
--    没有这一步，ADD COLUMN ... SET NOT NULL 会让所有新设备导入失败。
CREATE OR REPLACE FUNCTION badge_devices_init_position() RETURNS trigger AS $$
BEGIN
  IF NEW.position IS NULL THEN
    NEW.position := CASE NEW.status
      WHEN 'assigned' THEN 'in_use'
      WHEN 'retired'  THEN 'inactive'
      WHEN 'in_use'   THEN 'in_use'
      ELSE 'warehouse' END;
  END IF;
  IF NEW.availability IS NULL THEN
    NEW.availability := CASE NEW.status
      WHEN 'in_stock' THEN 'available'
      WHEN 'assigned' THEN 'available'
      WHEN 'in_use'   THEN 'available'
      WHEN 'pending_acceptance' THEN 'pending_check'
      WHEN 'returned' THEN 'pending_check'
      ELSE 'unusable' END;
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_badge_devices_init_position ON badge_devices;
CREATE TRIGGER trg_badge_devices_init_position
  BEFORE INSERT ON badge_devices
  FOR EACH ROW EXECUTE FUNCTION badge_devices_init_position();

DROP TRIGGER IF EXISTS trg_badge_devices_sync_status ON badge_devices;
CREATE TRIGGER trg_badge_devices_sync_status
  BEFORE UPDATE ON badge_devices
  FOR EACH ROW EXECUTE FUNCTION badge_devices_sync_legacy_status();
