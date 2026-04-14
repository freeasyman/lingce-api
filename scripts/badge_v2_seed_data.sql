-- Badge V2 种子数据脚本
-- 用途：为测试环境添加示例数据

-- 1. 添加厂家数据（如果不存在）
INSERT INTO badge_manufacturers (code, name, contact_person, contact_phone, api_endpoint, is_active, config, created_at, updated_at)
VALUES
  ('xiaomi', '小米', '张三', '13800138000', 'https://api.xiaomi.com/badge', true, '{}'::jsonb, NOW(), NOW()),
  ('huawei', '华为', '李四', '13900139000', 'https://api.huawei.com/badge', true, '{}'::jsonb, NOW(), NOW()),
  ('default', '默认厂家', '测试', '10000000000', 'https://api.example.com', true, '{}'::jsonb, NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

-- 2. 添加测试设备数据（如果数据库为空）
-- 注意：只在 badge_devices 表为空时插入
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM badge_devices LIMIT 1) THEN
    -- 插入 draft 状态设备（刚导入，待验收）
    INSERT INTO badge_devices (
      device_no, manufacturer_code, manufacturer_name, hardware_model,
      status, health_status, import_batch_no, metadata,
      created_at, updated_at
    ) VALUES
      ('XM001', 'xiaomi', '小米', 'X1', 'draft', 'unknown', 'BATCH-TEST-001', '{}'::jsonb, NOW(), NOW()),
      ('XM002', 'xiaomi', '小米', 'X1', 'draft', 'unknown', 'BATCH-TEST-001', '{}'::jsonb, NOW(), NOW()),
      ('XM003', 'xiaomi', '小米', 'X2', 'draft', 'unknown', 'BATCH-TEST-001', '{}'::jsonb, NOW(), NOW());

    -- 插入 available 状态设备（已验收，可分配）
    INSERT INTO badge_devices (
      device_no, manufacturer_code, manufacturer_name, hardware_model,
      status, health_status, battery_level, last_online_at, last_check_at,
      metadata, created_at, updated_at
    ) VALUES
      ('XM004', 'xiaomi', '小米', 'X1', 'available', 'healthy', 85, NOW() - INTERVAL '2 hours', NOW() - INTERVAL '1 hour', '{}'::jsonb, NOW(), NOW()),
      ('XM005', 'xiaomi', '小米', 'X1', 'available', 'healthy', 92, NOW() - INTERVAL '1 hour', NOW() - INTERVAL '30 minutes', '{}'::jsonb, NOW(), NOW()),
      ('HW001', 'huawei', '华为', 'H1', 'available', 'warning', 15, NOW() - INTERVAL '15 hours', NOW() - INTERVAL '2 hours', '{}'::jsonb, NOW(), NOW());

    -- 插入 in_use 状态设备（使用中）
    -- 注意：需要先有 tenant 和 employee 数据
    INSERT INTO badge_devices (
      device_no, manufacturer_code, manufacturer_name, hardware_model,
      status, health_status, battery_level, last_online_at, last_check_at,
      tenant_id, tenant_name, employee_id, employee_name, employee_phone, assigned_at,
      metadata, created_at, updated_at
    )
    SELECT
      'XM006', 'xiaomi', '小米', 'X2',
      'in_use', 'healthy', 78, NOW() - INTERVAL '3 hours', NOW() - INTERVAL '1 hour',
      t.id, t.name, e.id, e.full_name, e.phone, NOW() - INTERVAL '7 days',
      '{}'::jsonb, NOW(), NOW()
    FROM tenants t
    CROSS JOIN employees e
    WHERE t.deleted_at IS NULL AND e.deleted_at IS NULL
    LIMIT 1;

    -- 插入异常设备（用于监控页面测试）
    INSERT INTO badge_devices (
      device_no, manufacturer_code, manufacturer_name, hardware_model,
      status, health_status, battery_level, last_online_at, last_check_at,
      health_check_result, metadata, created_at, updated_at
    ) VALUES
      ('XM007', 'xiaomi', '小米', 'X1', 'available', 'error', 5, NOW() - INTERVAL '30 hours', NOW() - INTERVAL '3 hours',
       '{"online": false, "battery_low": true, "offline_hours": 30, "recording_test": {"start": true, "stop": true, "callback": false}}'::jsonb,
       '{}'::jsonb, NOW(), NOW()),
      ('HW002', 'huawei', '华为', 'H1', 'in_use', 'warning', 12, NOW() - INTERVAL '18 hours', NOW() - INTERVAL '5 hours',
       '{"online": false, "battery_low": true, "offline_hours": 18, "recording_test": {"start": true, "stop": true, "callback": true}}'::jsonb,
       '{}'::jsonb, NOW(), NOW());

    RAISE NOTICE '✅ 已插入测试设备数据';
  ELSE
    RAISE NOTICE 'ℹ️  数据库已有设备数据，跳过插入';
  END IF;
END $$;

-- 3. 确保所有设备的必填字段有值
UPDATE badge_devices
SET
  health_status = COALESCE(health_status, 'unknown'),
  manufacturer_code = COALESCE(NULLIF(manufacturer_code, ''), 'default'),
  metadata = COALESCE(metadata, '{}'::jsonb)
WHERE deleted_at IS NULL;

-- 4. 状态映射（如果有旧数据）
UPDATE badge_devices
SET status = CASE
  WHEN status = 'pending_acceptance' THEN 'draft'
  WHEN status = 'pending_assignment' THEN 'available'
  WHEN status = 'maintenance' THEN 'broken'
  WHEN status = 'retired' THEN 'scrapped'
  WHEN status NOT IN ('draft', 'available', 'in_use', 'returned', 'broken', 'scrapped') THEN 'available'
  ELSE status
END
WHERE deleted_at IS NULL;

-- 5. 显示统计信息
DO $$
DECLARE
  total_count INTEGER;
  draft_count INTEGER;
  available_count INTEGER;
  in_use_count INTEGER;
  error_count INTEGER;
BEGIN
  SELECT COUNT(*) INTO total_count FROM badge_devices WHERE deleted_at IS NULL;
  SELECT COUNT(*) INTO draft_count FROM badge_devices WHERE deleted_at IS NULL AND status = 'draft';
  SELECT COUNT(*) INTO available_count FROM badge_devices WHERE deleted_at IS NULL AND status = 'available';
  SELECT COUNT(*) INTO in_use_count FROM badge_devices WHERE deleted_at IS NULL AND status = 'in_use';
  SELECT COUNT(*) INTO error_count FROM badge_devices WHERE deleted_at IS NULL AND health_status = 'error';

  RAISE NOTICE '';
  RAISE NOTICE '========================================';
  RAISE NOTICE '设备统计';
  RAISE NOTICE '========================================';
  RAISE NOTICE '总设备数: %', total_count;
  RAISE NOTICE '草稿状态: %', draft_count;
  RAISE NOTICE '可用状态: %', available_count;
  RAISE NOTICE '使用中: %', in_use_count;
  RAISE NOTICE '异常设备: %', error_count;
  RAISE NOTICE '========================================';
END $$;
