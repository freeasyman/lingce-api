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
	fmt.Println("修复工牌状态")
	fmt.Println("==========================================")
	fmt.Println()

	// 1. 显示修复前状态
	fmt.Println("[1] 修复前状态分布:")
	rows, _ := pool.Query(ctx, `
		SELECT status, COUNT(*) as count
		FROM badge_devices
		WHERE deleted_at IS NULL
		GROUP BY status
		ORDER BY count DESC
	`)
	for rows.Next() {
		var status string
		var count int
		rows.Scan(&status, &count)
		fmt.Printf("  %s: %d 台\n", status, count)
	}
	rows.Close()

	// 2. 检查有多少设备是 in_use 但没有实际分配
	var inUseNoAssignment int
	pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM badge_devices
		WHERE deleted_at IS NULL
		  AND status = 'in_use'
		  AND tenant_id IS NULL
		  AND employee_id IS NULL
	`).Scan(&inUseNoAssignment)

	fmt.Printf("\n[2] 状态为 'in_use' 但未分配的设备: %d 台\n", inUseNoAssignment)

	if inUseNoAssignment == 0 {
		fmt.Println("   ✅ 所有 'in_use' 设备都有分配记录，无需修复")
		return
	}

	fmt.Println("   这些设备应该改为 'available' (可用) 状态")

	// 3. 执行修复
	fmt.Println("\n[3] 执行修复...")
	result, err := pool.Exec(ctx, `
		UPDATE badge_devices
		SET status = 'available'
		WHERE deleted_at IS NULL
		  AND status = 'in_use'
		  AND tenant_id IS NULL
		  AND employee_id IS NULL
	`)
	if err != nil {
		log.Fatalf("修复失败: %v", err)
	}

	fmt.Printf("   ✅ 已将 %d 台设备状态改为 'available'\n", result.RowsAffected())

	// 4. 显示修复后状态
	fmt.Println("\n[4] 修复后状态分布:")
	rows2, _ := pool.Query(ctx, `
		SELECT status, COUNT(*) as count
		FROM badge_devices
		WHERE deleted_at IS NULL
		GROUP BY status
		ORDER BY count DESC
	`)
	for rows2.Next() {
		var status string
		var count int
		rows2.Scan(&status, &count)
		fmt.Printf("  %s: %d 台\n", status, count)
	}
	rows2.Close()

	// 5. 显示示例设备
	fmt.Println("\n[5] 可分配设备示例（前5台）:")
	fmt.Println("------------------------------------------")
	rows3, _ := pool.Query(ctx, `
		SELECT device_no, manufacturer_name, status, battery_level
		FROM badge_devices
		WHERE deleted_at IS NULL AND status = 'available'
		LIMIT 5
	`)
	for rows3.Next() {
		var deviceNo string
		var mfgName *string
		var status string
		var battery *int
		rows3.Scan(&deviceNo, &mfgName, &status, &battery)

		mfg := "未知"
		if mfgName != nil {
			mfg = *mfgName
		}
		bat := "未知"
		if battery != nil {
			bat = fmt.Sprintf("%d%%", *battery)
		}

		fmt.Printf("  %s | %s | %s | 电量: %s\n", deviceNo, mfg, status, bat)
	}
	rows3.Close()

	fmt.Println("\n==========================================")
	fmt.Println("✅ 状态修复完成！")
	fmt.Println("==========================================")
	fmt.Println()
	fmt.Println("现在可以:")
	fmt.Println("  1. 访问「设备分配」页面")
	fmt.Println("  2. 在「分配设备」标签中看到这些可用设备")
	fmt.Println("  3. 勾选设备并点击「批量分配」")
	fmt.Println("  4. 选择租户和员工进行分配")
}
