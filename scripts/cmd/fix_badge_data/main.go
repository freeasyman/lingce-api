package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/freeasyman/lingce-api/internal/scriptutil"
)

func main() {
	configPath := flag.String("config", "./configs/dev.toml", "path to TOML config file")
	flag.Parse()

	ctx := context.Background()
	_, pool, err := scriptutil.OpenPool(ctx, *configPath)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	fmt.Println("==========================================")
	fmt.Println("工牌设备数据修复脚本")
	fmt.Println("==========================================")
	fmt.Println()

	// 1. 填充厂家名称
	fmt.Println("[1] 填充厂家名称...")
	result, err := pool.Exec(ctx, `
		UPDATE badge_devices bd
		SET manufacturer_name = bm.name
		FROM badge_manufacturers bm
		WHERE bd.manufacturer_code = bm.code
		  AND (bd.manufacturer_name IS NULL OR bd.manufacturer_name = '')
		  AND bd.deleted_at IS NULL
	`)
	if err != nil {
		log.Printf("   警告: %v", err)
	} else {
		fmt.Printf("   ✅ 已更新 %d 台设备的厂家名称\n", result.RowsAffected())
	}

	// 2. 填充租户名称
	fmt.Println("\n[2] 填充租户名称...")
	result, err = pool.Exec(ctx, `
		UPDATE badge_devices bd
		SET tenant_name = t.name
		FROM tenants t
		WHERE bd.tenant_id = t.id
		  AND (bd.tenant_name IS NULL OR bd.tenant_name = '')
		  AND bd.deleted_at IS NULL
		  AND t.deleted_at IS NULL
	`)
	if err != nil {
		log.Printf("   警告: %v", err)
	} else {
		fmt.Printf("   ✅ 已更新 %d 台设备的租户名称\n", result.RowsAffected())
	}

	// 3. 填充员工名称和电话
	fmt.Println("\n[3] 填充员工信息...")
	result, err = pool.Exec(ctx, `
		UPDATE badge_devices bd
		SET
			employee_name = e.full_name,
			employee_phone = e.phone
		FROM employees e
		WHERE bd.employee_id = e.id
		  AND (bd.employee_name IS NULL OR bd.employee_name = '')
		  AND bd.deleted_at IS NULL
		  AND e.deleted_at IS NULL
	`)
	if err != nil {
		log.Printf("   警告: %v", err)
	} else {
		fmt.Printf("   ✅ 已更新 %d 台设备的员工信息\n", result.RowsAffected())
	}

	// 4. 设置默认电量（如果为空）
	fmt.Println("\n[4] 设置默认电量...")
	result, err = pool.Exec(ctx, `
		UPDATE badge_devices
		SET battery_level = 50
		WHERE battery_level IS NULL
		  AND deleted_at IS NULL
	`)
	if err != nil {
		log.Printf("   警告: %v", err)
	} else {
		fmt.Printf("   ✅ 已设置 %d 台设备的默认电量\n", result.RowsAffected())
	}

	// 5. 设置默认在线时间
	fmt.Println("\n[5] 设置默认在线时间...")
	result, err = pool.Exec(ctx, `
		UPDATE badge_devices
		SET last_online_at = NOW() - INTERVAL '2 hours'
		WHERE last_online_at IS NULL
		  AND deleted_at IS NULL
	`)
	if err != nil {
		log.Printf("   警告: %v", err)
	} else {
		fmt.Printf("   ✅ 已设置 %d 台设备的在线时间\n", result.RowsAffected())
	}

	// 6. 显示统计
	fmt.Println("\n==========================================")
	fmt.Println("修复后统计")
	fmt.Println("==========================================")

	var total, withManufacturer, withTenant, withEmployee, withBattery int
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL").Scan(&total)
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND manufacturer_name IS NOT NULL AND manufacturer_name != ''").Scan(&withManufacturer)
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND tenant_name IS NOT NULL AND tenant_name != ''").Scan(&withTenant)
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND employee_name IS NOT NULL AND employee_name != ''").Scan(&withEmployee)
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND battery_level IS NOT NULL").Scan(&withBattery)

	fmt.Printf("总设备数: %d\n", total)
	fmt.Printf("有厂家信息: %d (%.1f%%)\n", withManufacturer, float64(withManufacturer)/float64(total)*100)
	fmt.Printf("有租户信息: %d (%.1f%%)\n", withTenant, float64(withTenant)/float64(total)*100)
	fmt.Printf("有员工信息: %d (%.1f%%)\n", withEmployee, float64(withEmployee)/float64(total)*100)
	fmt.Printf("有电量信息: %d (%.1f%%)\n", withBattery, float64(withBattery)/float64(total)*100)

	fmt.Println("\n==========================================")
	fmt.Println("✅ 数据修复完成！")
	fmt.Println("==========================================")
}
