package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file
	_ = godotenv.Load("configs/.env")
	_ = godotenv.Load(".env")

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	fmt.Println("==========================================")
	fmt.Println("工牌管理 V2 - 种子数据脚本")
	fmt.Println("==========================================")
	fmt.Println()

	// 1. 添加厂家数据
	fmt.Println("[1] 添加厂家数据...")
	_, err = pool.Exec(ctx, `
		INSERT INTO badge_manufacturers (code, name, contact_person, contact_phone, api_endpoint, is_active, config, created_at, updated_at)
		VALUES
		  ('xiaomi', '小米', '张三', '13800138000', 'https://api.xiaomi.com/badge', true, '{}'::jsonb, NOW(), NOW()),
		  ('huawei', '华为', '李四', '13900139000', 'https://api.huawei.com/badge', true, '{}'::jsonb, NOW(), NOW()),
		  ('default', '默认厂家', '测试', '10000000000', 'https://api.example.com', true, '{}'::jsonb, NOW(), NOW())
		ON CONFLICT (code) DO NOTHING
	`)
	if err != nil {
		log.Printf("   警告: 厂家数据插入失败（可能已存在）: %v", err)
	} else {
		fmt.Println("   ✅ 厂家数据已添加")
	}

	// 2. 检查是否已有设备数据
	var deviceCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL").Scan(&deviceCount)
	if err != nil {
		log.Fatalf("Failed to count devices: %v", err)
	}

	fmt.Printf("\n[2] 检查现有设备数量: %d\n", deviceCount)

	if deviceCount > 0 {
		fmt.Println("   ℹ️  数据库已有设备数据，跳过插入测试数据")
		fmt.Println("   如需重新插入，请先清空 badge_devices 表")
	} else {
		fmt.Println("\n[3] 插入测试设备数据...")

		// 插入 draft 状态设备
		_, err = pool.Exec(ctx, `
			INSERT INTO badge_devices (
			  device_no, manufacturer_code, manufacturer_name, hardware_model,
			  status, health_status, import_batch_no, metadata,
			  created_at, updated_at
			) VALUES
			  ('XM001', 'xiaomi', '小米', 'X1', 'draft', 'unknown', 'BATCH-TEST-001', '{}'::jsonb, NOW(), NOW()),
			  ('XM002', 'xiaomi', '小米', 'X1', 'draft', 'unknown', 'BATCH-TEST-001', '{}'::jsonb, NOW(), NOW()),
			  ('XM003', 'xiaomi', '小米', 'X2', 'draft', 'unknown', 'BATCH-TEST-001', '{}'::jsonb, NOW(), NOW())
		`)
		if err != nil {
			log.Fatalf("Failed to insert draft devices: %v", err)
		}
		fmt.Println("   ✅ 已插入 3 台草稿状态设备")

		// 插入 available 状态设备
		_, err = pool.Exec(ctx, `
			INSERT INTO badge_devices (
			  device_no, manufacturer_code, manufacturer_name, hardware_model,
			  status, health_status, battery_level, last_online_at, last_check_at,
			  metadata, created_at, updated_at
			) VALUES
			  ('XM004', 'xiaomi', '小米', 'X1', 'available', 'healthy', 85, NOW() - INTERVAL '2 hours', NOW() - INTERVAL '1 hour', '{}'::jsonb, NOW(), NOW()),
			  ('XM005', 'xiaomi', '小米', 'X1', 'available', 'healthy', 92, NOW() - INTERVAL '1 hour', NOW() - INTERVAL '30 minutes', '{}'::jsonb, NOW(), NOW()),
			  ('HW001', 'huawei', '华为', 'H1', 'available', 'warning', 15, NOW() - INTERVAL '15 hours', NOW() - INTERVAL '2 hours', '{}'::jsonb, NOW(), NOW())
		`)
		if err != nil {
			log.Fatalf("Failed to insert available devices: %v", err)
		}
		fmt.Println("   ✅ 已插入 3 台可用状态设备")

		// 插入 in_use 状态设备（需要租户和员工数据）
		var tenantID, employeeID int64
		var tenantName, employeeName, employeePhone string
		err = pool.QueryRow(ctx, `
			SELECT t.id, t.name, e.id, e.full_name, e.phone
			FROM tenants t
			CROSS JOIN employees e
			WHERE t.deleted_at IS NULL AND e.deleted_at IS NULL
			LIMIT 1
		`).Scan(&tenantID, &tenantName, &employeeID, &employeeName, &employeePhone)

		if err == nil {
			_, err = pool.Exec(ctx, `
				INSERT INTO badge_devices (
				  device_no, manufacturer_code, manufacturer_name, hardware_model,
				  status, health_status, battery_level, last_online_at, last_check_at,
				  tenant_id, tenant_name, employee_id, employee_name, employee_phone, assigned_at,
				  metadata, created_at, updated_at
				) VALUES
				  ($1, 'xiaomi', '小米', 'X2', 'in_use', 'healthy', 78, NOW() - INTERVAL '3 hours', NOW() - INTERVAL '1 hour',
				   $2, $3, $4, $5, $6, NOW() - INTERVAL '7 days',
				   '{}'::jsonb, NOW(), NOW())
			`, "XM006", tenantID, tenantName, employeeID, employeeName, employeePhone)
			if err != nil {
				log.Printf("   警告: 使用中设备插入失败: %v", err)
			} else {
				fmt.Println("   ✅ 已插入 1 台使用中设备")
			}
		} else {
			fmt.Println("   ⚠️  未找到租户/员工数据，跳过使用中设备")
		}

		// 插入异常设备
		_, err = pool.Exec(ctx, `
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
			   '{}'::jsonb, NOW(), NOW())
		`)
		if err != nil {
			log.Fatalf("Failed to insert error devices: %v", err)
		}
		fmt.Println("   ✅ 已插入 2 台异常设备")
	}

	// 4. 确保所有设备的必填字段有值
	fmt.Println("\n[4] 更新必填字段...")
	_, err = pool.Exec(ctx, `
		UPDATE badge_devices
		SET
		  health_status = COALESCE(health_status, 'unknown'),
		  manufacturer_code = COALESCE(NULLIF(manufacturer_code, ''), 'default'),
		  metadata = COALESCE(metadata, '{}'::jsonb)
		WHERE deleted_at IS NULL
	`)
	if err != nil {
		log.Printf("   警告: 更新必填字段失败: %v", err)
	} else {
		fmt.Println("   ✅ 必填字段已更新")
	}

	// 5. 状态映射（如果有旧数据）
	fmt.Println("\n[5] 执行状态映射...")
	result, err := pool.Exec(ctx, `
		UPDATE badge_devices
		SET status = CASE
		  WHEN status = 'pending_acceptance' THEN 'draft'
		  WHEN status = 'pending_assignment' THEN 'available'
		  WHEN status = 'maintenance' THEN 'broken'
		  WHEN status = 'retired' THEN 'scrapped'
		  WHEN status NOT IN ('draft', 'available', 'in_use', 'returned', 'broken', 'scrapped') THEN 'available'
		  ELSE status
		END
		WHERE deleted_at IS NULL
	`)
	if err != nil {
		log.Printf("   警告: 状态映射失败: %v", err)
	} else {
		fmt.Printf("   ✅ 状态映射完成（影响 %d 行）\n", result.RowsAffected())
	}

	// 6. 显示统计信息
	fmt.Println("\n==========================================")
	fmt.Println("设备统计")
	fmt.Println("==========================================")

	var total, draft, available, inUse, errorCount int
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL").Scan(&total)
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND status = 'draft'").Scan(&draft)
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND status = 'available'").Scan(&available)
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND status = 'in_use'").Scan(&inUse)
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND health_status = 'error'").Scan(&errorCount)

	fmt.Printf("总设备数: %d\n", total)
	fmt.Printf("草稿状态: %d\n", draft)
	fmt.Printf("可用状态: %d\n", available)
	fmt.Printf("使用中: %d\n", inUse)
	fmt.Printf("异常设备: %d\n", errorCount)
	fmt.Println("==========================================")
	fmt.Println()
	fmt.Println("✅ 种子数据脚本执行完成！")
	fmt.Println()
	fmt.Println("现在可以访问前端页面查看数据：")
	fmt.Println("  - 仪表板: http://localhost:3002/badges-v2/dashboard")
	fmt.Println("  - 设备库存: http://localhost:3002/badges-v2/inventory")
	fmt.Println("  - 设备分配: http://localhost:3002/badges-v2/assignment")
	fmt.Println("  - 设备监控: http://localhost:3002/badges-v2/monitoring")
}
