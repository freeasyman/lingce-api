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
	fmt.Println("检查旧数据表中的租户/员工关联")
	fmt.Println("==========================================")
	fmt.Println()

	// 检查 badge_devices 表中有 tenant_id 的设备
	var withTenantID, withEmployeeID int
	pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM badge_devices
		WHERE tenant_id IS NOT NULL AND deleted_at IS NULL
	`).Scan(&withTenantID)

	pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM badge_devices
		WHERE employee_id IS NOT NULL AND deleted_at IS NULL
	`).Scan(&withEmployeeID)

	fmt.Printf("badge_devices 表中:\n")
	fmt.Printf("  - 有 tenant_id 的设备: %d\n", withTenantID)
	fmt.Printf("  - 有 employee_id 的设备: %d\n", withEmployeeID)
	fmt.Println()

	// 如果有关联，显示示例
	if withTenantID > 0 || withEmployeeID > 0 {
		fmt.Println("示例数据（前3台有关联的设备）:")
		fmt.Println("------------------------------------------")
		rows, _ := pool.Query(context.Background(), `
			SELECT device_no, tenant_id, employee_id, status
			FROM badge_devices
			WHERE (tenant_id IS NOT NULL OR employee_id IS NOT NULL)
			  AND deleted_at IS NULL
			LIMIT 3
		`)
		defer rows.Close()

		for rows.Next() {
			var deviceNo, status string
			var tenantID, empID *int64
			rows.Scan(&deviceNo, &tenantID, &empID, &status)
			fmt.Printf("设备: %s (状态: %s)\n", deviceNo, status)
			if tenantID != nil {
				fmt.Printf("  tenant_id: %d\n", *tenantID)
			}
			if empID != nil {
				fmt.Printf("  employee_id: %d\n", *empID)
			}
			fmt.Println()
		}
	}

	// 检查是否有其他旧的工牌相关表
	fmt.Println("检查其他可能的旧表:")
	fmt.Println("------------------------------------------")

	tables := []string{
		"badge_assignments",
		"badge_allocations",
		"badge_employee_assignments",
		"employee_badges",
		"badge_records",
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
		} else {
			fmt.Printf("  ✗ %s 表不存在\n", table)
		}
	}
}
