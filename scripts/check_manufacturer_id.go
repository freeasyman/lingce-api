package main
import (
	"context"
	"fmt"
	"os"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)
func main() {
	_ = godotenv.Load("configs/.env")
	pool, _ := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	defer pool.Close()

	fmt.Println("==========================================")
	fmt.Println("检查设备的 manufacturer_id 字段")
	fmt.Println("==========================================")
	fmt.Println()

	// 1. 统计 manufacturer_id 分布
	fmt.Println("[1] manufacturer_id 分布:")
	rows, _ := pool.Query(context.Background(), `
		SELECT manufacturer_id, COUNT(*) as count
		FROM badge_devices
		WHERE deleted_at IS NULL
		GROUP BY manufacturer_id
		ORDER BY count DESC
	`)
	defer rows.Close()

	for rows.Next() {
		var id *int64
		var count int
		rows.Scan(&id, &count)
		if id != nil {
			fmt.Printf("  manufacturer_id = %d: %d 台设备\n", *id, count)
		} else {
			fmt.Printf("  manufacturer_id = NULL: %d 台设备\n", count)
		}
	}

	// 2. 如果有 manufacturer_id，显示对应的厂家信息
	fmt.Println("\n[2] 根据 manufacturer_id 查找对应厂家:")
	rows2, _ := pool.Query(context.Background(), `
		SELECT DISTINCT bd.manufacturer_id, bm.code, bm.name
		FROM badge_devices bd
		LEFT JOIN badge_manufacturers bm ON bd.manufacturer_id = bm.id
		WHERE bd.deleted_at IS NULL AND bd.manufacturer_id IS NOT NULL
	`)
	defer rows2.Close()

	hasManufacturerID := false
	for rows2.Next() {
		hasManufacturerID = true
		var id int64
		var code, name *string
		rows2.Scan(&id, &code, &name)
		if code != nil && name != nil {
			fmt.Printf("  ID %d -> %s (%s)\n", id, *name, *code)
		} else {
			fmt.Printf("  ID %d -> 未找到对应厂家\n", id)
		}
	}

	if !hasManufacturerID {
		fmt.Println("  所有设备的 manufacturer_id 都是 NULL")
	}

	// 3. 显示示例设备数据
	fmt.Println("\n[3] 设备数据示例（前5台）:")
	fmt.Println("------------------------------------------")
	rows3, _ := pool.Query(context.Background(), `
		SELECT device_no, manufacturer_id, manufacturer_code, manufacturer_name
		FROM badge_devices
		WHERE deleted_at IS NULL
		LIMIT 5
	`)
	defer rows3.Close()

	for rows3.Next() {
		var deviceNo, mfgCode string
		var mfgID *int64
		var mfgName *string
		rows3.Scan(&deviceNo, &mfgID, &mfgCode, &mfgName)

		fmt.Printf("\n设备: %s\n", deviceNo)
		if mfgID != nil {
			fmt.Printf("  manufacturer_id: %d\n", *mfgID)
		} else {
			fmt.Printf("  manufacturer_id: NULL\n")
		}
		fmt.Printf("  manufacturer_code: %s\n", mfgCode)
		if mfgName != nil {
			fmt.Printf("  manufacturer_name: %s\n", *mfgName)
		} else {
			fmt.Printf("  manufacturer_name: NULL\n")
		}
	}

	// 4. 建议修复方案
	fmt.Println("\n==========================================")
	fmt.Println("修复建议")
	fmt.Println("==========================================")

	var withID, withoutID int
	pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM badge_devices
		WHERE deleted_at IS NULL AND manufacturer_id IS NOT NULL
	`).Scan(&withID)

	pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM badge_devices
		WHERE deleted_at IS NULL AND manufacturer_id IS NULL
	`).Scan(&withoutID)

	if withID > 0 {
		fmt.Printf("\n有 %d 台设备有 manufacturer_id，可以根据 ID 更新 code 和 name\n", withID)
		fmt.Println("执行修复脚本可以自动更新")
	}

	if withoutID > 0 {
		fmt.Printf("\n有 %d 台设备没有 manufacturer_id，已使用 'default' 作为默认值\n", withoutID)
	}
}
