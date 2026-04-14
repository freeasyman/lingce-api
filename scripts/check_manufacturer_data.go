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
	fmt.Println("检查旧版本厂家数据")
	fmt.Println("==========================================")
	fmt.Println()

	// 1. 检查 badge_manufacturers 表
	fmt.Println("[1] badge_manufacturers 表:")
	rows, _ := pool.Query(context.Background(), `
		SELECT id, code, name, contact_person, contact_phone, api_endpoint, is_active
		FROM badge_manufacturers
		ORDER BY id
	`)
	defer rows.Close()

	for rows.Next() {
		var id int64
		var code, name string
		var contact, phone, endpoint *string
		var active bool
		rows.Scan(&id, &code, &name, &contact, &phone, &endpoint, &active)

		fmt.Printf("\nID: %d\n", id)
		fmt.Printf("  代码: %s\n", code)
		fmt.Printf("  名称: %s\n", name)
		if contact != nil {
			fmt.Printf("  联系人: %s\n", *contact)
		}
		if phone != nil {
			fmt.Printf("  电话: %s\n", *phone)
		}
		if endpoint != nil {
			fmt.Printf("  API: %s\n", *endpoint)
		}
		fmt.Printf("  状态: %v\n", active)
	}

	// 2. 检查设备使用的厂家代码分布
	fmt.Println("\n==========================================")
	fmt.Println("[2] 设备使用的厂家代码分布:")
	fmt.Println("==========================================")

	rows2, _ := pool.Query(context.Background(), `
		SELECT manufacturer_code, COUNT(*) as count
		FROM badge_devices
		WHERE deleted_at IS NULL
		GROUP BY manufacturer_code
		ORDER BY count DESC
	`)
	defer rows2.Close()

	for rows2.Next() {
		var code string
		var count int
		rows2.Scan(&code, &count)
		fmt.Printf("  %s: %d 台设备\n", code, count)
	}

	// 3. 检查是否有旧的厂家相关表
	fmt.Println("\n==========================================")
	fmt.Println("[3] 检查其他可能的厂家表:")
	fmt.Println("==========================================")

	tables := []string{
		"badge_vendors",
		"badge_suppliers",
		"manufacturers",
		"device_manufacturers",
	}

	for _, table := range tables {
		var exists bool
		pool.QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT FROM information_schema.tables
				WHERE table_name = $1
			)
		`, table).Scan(&exists)

		if exists {
			var count int
			pool.QueryRow(context.Background(), fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
			fmt.Printf("  ✓ %s 表存在，记录数: %d\n", table, count)

			// 显示表结构
			rows3, _ := pool.Query(context.Background(), fmt.Sprintf(`
				SELECT column_name, data_type
				FROM information_schema.columns
				WHERE table_name = '%s'
				ORDER BY ordinal_position
				LIMIT 10
			`, table))
			fmt.Println("    字段:")
			for rows3.Next() {
				var colName, colType string
				rows3.Scan(&colName, &colType)
				fmt.Printf("      - %s (%s)\n", colName, colType)
			}
			rows3.Close()
		} else {
			fmt.Printf("  ✗ %s 表不存在\n", table)
		}
	}

	// 4. 检查设备表中是否有其他厂家相关字段
	fmt.Println("\n==========================================")
	fmt.Println("[4] badge_devices 表中的厂家相关字段:")
	fmt.Println("==========================================")

	rows4, _ := pool.Query(context.Background(), `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_name = 'badge_devices'
		  AND (column_name LIKE '%manufacturer%' OR column_name LIKE '%vendor%' OR column_name LIKE '%supplier%')
		ORDER BY ordinal_position
	`)
	defer rows4.Close()

	for rows4.Next() {
		var colName, colType string
		rows4.Scan(&colName, &colType)
		fmt.Printf("  - %s (%s)\n", colName, colType)
	}
}
