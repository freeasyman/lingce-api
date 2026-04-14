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

	var count int
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM badge_manufacturers").Scan(&count)
	fmt.Printf("厂家数量: %d\n", count)

	rows, _ := pool.Query(context.Background(), "SELECT code, name FROM badge_manufacturers LIMIT 5")
	defer rows.Close()
	fmt.Println("\n厂家列表:")
	for rows.Next() {
		var code, name string
		rows.Scan(&code, &name)
		fmt.Printf("  - %s (%s)\n", name, code)
	}

	// 检查设备的厂家信息
	var deviceCount int
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM badge_devices WHERE manufacturer_name IS NOT NULL AND manufacturer_name != ''").Scan(&deviceCount)
	fmt.Printf("\n有厂家名称的设备: %d\n", deviceCount)
}
