package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/freeasyman/lingce-api/internal/scriptutil"
	"github.com/jackc/pgx/v5/pgxpool"
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
	fmt.Println("删除错误的厂家数据")
	fmt.Println("==========================================")
	fmt.Println()

	// 1. 显示当前厂家列表
	fmt.Println("[1] 当前厂家列表:")
	rows, _ := pool.Query(ctx, `
		SELECT id, code, name, created_at
		FROM badge_manufacturers
		WHERE deleted_at IS NULL
		ORDER BY id
	`)
	for rows.Next() {
		var id int64
		var code, name string
		var createdAt string
		rows.Scan(&id, &code, &name, &createdAt)
		fmt.Printf("  ID: %d | Code: %s | Name: %s | Created: %s\n", id, code, name, createdAt)
	}
	rows.Close()

	// 2. 检查是否有设备使用这些厂家
	var huaweiCount, xiaomiCount int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM badge_devices WHERE manufacturer_code = 'huawei' AND deleted_at IS NULL`).Scan(&huaweiCount)
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM badge_devices WHERE manufacturer_code = 'xiaomi' AND deleted_at IS NULL`).Scan(&xiaomiCount)

	fmt.Printf("\n[2] 使用情况检查:\n")
	fmt.Printf("  华为 (huawei): %d 台设备\n", huaweiCount)
	fmt.Printf("  小米 (xiaomi): %d 台设备\n", xiaomiCount)

	if huaweiCount > 0 || xiaomiCount > 0 {
		fmt.Println("\n  ⚠️  警告：有设备正在使用这些厂家，无法删除")
		return
	}

	// 3. 删除华为和小米厂家
	fmt.Println("\n[3] 删除错误的厂家...")
	result1, err := pool.Exec(ctx, `DELETE FROM badge_manufacturers WHERE code = 'huawei'`)
	if err != nil {
		log.Fatalf("删除华为失败: %v", err)
	}
	fmt.Printf("  ✅ 已删除华为厂家 (影响 %d 行)\n", result1.RowsAffected())

	result2, err := pool.Exec(ctx, `DELETE FROM badge_manufacturers WHERE code = 'xiaomi'`)
	if err != nil {
		log.Fatalf("删除小米失败: %v", err)
	}
	fmt.Printf("  ✅ 已删除小米厂家 (影响 %d 行)\n", result2.RowsAffected())

	// 4. 显示删除后的厂家列表
	fmt.Println("\n[4] 删除后的厂家列表:")
	rows2, _ := pool.Query(ctx, `
		SELECT id, code, name
		FROM badge_manufacturers
		WHERE deleted_at IS NULL
		ORDER BY id
	`)
	for rows2.Next() {
		var id int64
		var code, name string
		rows2.Scan(&id, &code, &name)
		fmt.Printf("  ID: %d | Code: %s | Name: %s\n", id, code, name)
	}
	rows2.Close()

	fmt.Println("\n==========================================")
	fmt.Println("✅ 厂家数据清理完成！")
	fmt.Println("==========================================")
	fmt.Println()
	fmt.Println("现在下拉列表中只会显示真实的厂家数据")
}
