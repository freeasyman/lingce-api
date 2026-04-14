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

	fmt.Println("设备数据示例（前3台）:")
	fmt.Println("==========================================")
	rows, _ := pool.Query(context.Background(), `
		SELECT device_no, manufacturer_code, manufacturer_name, status,
		       tenant_id, tenant_name, employee_id, employee_name, battery_level
		FROM badge_devices
		WHERE deleted_at IS NULL
		LIMIT 3
	`)
	defer rows.Close()

	for rows.Next() {
		var deviceNo, mfgCode, status string
		var mfgName, tenantName, empName *string
		var tenantID, empID *int64
		var battery *int
		rows.Scan(&deviceNo, &mfgCode, &mfgName, &status, &tenantID, &tenantName, &empID, &empName, &battery)

		fmt.Printf("\n设备号: %s\n", deviceNo)
		fmt.Printf("  厂家: %s (%s)\n", strPtr(mfgName), mfgCode)
		fmt.Printf("  状态: %s\n", status)
		fmt.Printf("  租户: %s (ID: %s)\n", strPtr(tenantName), int64Ptr(tenantID))
		fmt.Printf("  员工: %s (ID: %s)\n", strPtr(empName), int64Ptr(empID))
		fmt.Printf("  电量: %s%%\n", intPtr(battery))
	}
}

func strPtr(s *string) string {
	if s == nil { return "无" }
	return *s
}

func int64Ptr(i *int64) string {
	if i == nil { return "无" }
	return fmt.Sprintf("%d", *i)
}

func intPtr(i *int) string {
	if i == nil { return "无" }
	return fmt.Sprintf("%d", *i)
}
