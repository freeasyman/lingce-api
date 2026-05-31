package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/freeasyman/lingce-api/internal/scriptutil"
)

func main() {
	configPath := flag.String("config", "./configs/dev.toml", "path to TOML config file")
	flag.Parse()

	_, pool, err := scriptutil.OpenPool(context.Background(), *configPath)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	fmt.Println("==========================================")
	fmt.Println("工牌状态分析")
	fmt.Println("==========================================")
	fmt.Println()

	// 1. 当前设备状态分布
	fmt.Println("[1] 当前设备状态分布:")
	rows, _ := pool.Query(context.Background(), `
		SELECT status, COUNT(*) as count
		FROM badge_devices
		WHERE deleted_at IS NULL
		GROUP BY status
		ORDER BY count DESC
	`)
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		rows.Scan(&status, &count)
		fmt.Printf("  %s: %d 台\n", status, count)
	}

	// 2. V2 状态说明
	fmt.Println("\n[2] V2 工牌状态流转规则:")
	fmt.Println("------------------------------------------")
	fmt.Println("  draft (草稿)")
	fmt.Println("    - 刚导入的设备")
	fmt.Println("    - 需要验收后才能使用")
	fmt.Println("    - 操作: 可以「批量验收」")
	fmt.Println()
	fmt.Println("  available (可用)")
	fmt.Println("    - 已验收，可以分配")
	fmt.Println("    - ✅ 可以「批量分配」给租户/员工")
	fmt.Println()
	fmt.Println("  in_use (使用中)")
	fmt.Println("    - 已分配给租户/员工")
	fmt.Println("    - ✅ 可以「批量回收」")
	fmt.Println("    - ❌ 不能再次分配（需要先回收）")
	fmt.Println()
	fmt.Println("  returned (已归还)")
	fmt.Println("    - 已回收的设备")
	fmt.Println("    - 可以重新分配")
	fmt.Println()
	fmt.Println("  broken (故障)")
	fmt.Println("    - 设备故障")
	fmt.Println("    - 需要维修")
	fmt.Println()
	fmt.Println("  scrapped (报废)")
	fmt.Println("    - 设备报废")
	fmt.Println("    - 不能再使用")

	// 3. 当前问题分析
	fmt.Println("\n[3] 当前问题分析:")
	fmt.Println("------------------------------------------")

	var inUseCount int
	pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM badge_devices
		WHERE deleted_at IS NULL AND status = 'in_use'
	`).Scan(&inUseCount)

	if inUseCount > 0 {
		fmt.Printf("  ⚠️  有 %d 台设备状态为 'in_use' (使用中)\n", inUseCount)
		fmt.Println("  ❌ 使用中的设备不能直接分配")
		fmt.Println("  💡 需要先「回收」，然后才能重新分配")
		fmt.Println()
		fmt.Println("  解决方案:")
		fmt.Println("    方案1: 在「设备分配」页面的「回收设备」标签中批量回收")
		fmt.Println("    方案2: 将部分设备状态改为 'available'（可用）")
	}

	var availableCount int
	pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM badge_devices
		WHERE deleted_at IS NULL AND status = 'available'
	`).Scan(&availableCount)

	if availableCount > 0 {
		fmt.Printf("\n  ✅ 有 %d 台设备状态为 'available' (可用)\n", availableCount)
		fmt.Println("  这些设备可以直接分配")
	} else {
		fmt.Println("\n  ❌ 没有 'available' (可用) 状态的设备")
		fmt.Println("  需要先将一些设备改为可用状态")
	}

	// 4. 修复建议
	fmt.Println("\n[4] 修复建议:")
	fmt.Println("------------------------------------------")
	fmt.Println("  如果这些设备实际上没有分配给任何人，可以:")
	fmt.Println()
	fmt.Println("  选项1: 将所有设备改为 'available' (推荐)")
	fmt.Println("    UPDATE badge_devices")
	fmt.Println("    SET status = 'available'")
	fmt.Println("    WHERE deleted_at IS NULL AND status = 'in_use'")
	fmt.Println("      AND tenant_id IS NULL AND employee_id IS NULL;")
	fmt.Println()
	fmt.Println("  选项2: 将部分设备改为 'available' (测试用)")
	fmt.Println("    UPDATE badge_devices")
	fmt.Println("    SET status = 'available'")
	fmt.Println("    WHERE deleted_at IS NULL AND status = 'in_use'")
	fmt.Println("      AND tenant_id IS NULL AND employee_id IS NULL")
	fmt.Println("    LIMIT 10;")

	fmt.Println("\n==========================================")
}
