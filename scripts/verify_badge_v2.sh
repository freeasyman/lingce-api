#!/usr/bin/env bash
# 快速验证工牌 V2 数据

echo "=========================================="
echo "工牌管理 V2 - 数据验证"
echo "=========================================="
echo ""

# 检查后端服务
echo "[1] 检查后端服务..."
if lsof -i :18080 | grep -q LISTEN; then
  echo "   ✅ 后端服务运行中 (端口 18080)"
else
  echo "   ❌ 后端服务未运行"
  exit 1
fi

# 检查前端服务
echo ""
echo "[2] 检查前端服务..."
if lsof -i :3002 | grep -q LISTEN; then
  echo "   ✅ 前端服务运行中 (端口 3002)"
else
  echo "   ⚠️  前端服务未运行"
  echo "   请运行: cd /Users/yiliiang/Documents/lingce-web/apps/operation && npm run dev"
fi

echo ""
echo "=========================================="
echo "数据库统计"
echo "=========================================="

# 直接查询数据库
cd /Users/yiliiang/Documents/lingce-api
go run - <<'EOF'
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

	var total, draft, available, inUse, returned, broken, scrapped, errorCount int
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL").Scan(&total)
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND status = 'draft'").Scan(&draft)
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND status = 'available'").Scan(&available)
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND status = 'in_use'").Scan(&inUse)
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND status = 'returned'").Scan(&returned)
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND status = 'broken'").Scan(&broken)
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND status = 'scrapped'").Scan(&scrapped)
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND health_status = 'error'").Scan(&errorCount)

	fmt.Printf("总设备数: %d\n", total)
	fmt.Printf("\n按状态分布:\n")
	fmt.Printf("  - 草稿 (draft): %d\n", draft)
	fmt.Printf("  - 可用 (available): %d\n", available)
	fmt.Printf("  - 使用中 (in_use): %d\n", inUse)
	fmt.Printf("  - 已归还 (returned): %d\n", returned)
	fmt.Printf("  - 故障 (broken): %d\n", broken)
	fmt.Printf("  - 报废 (scrapped): %d\n", scrapped)
	fmt.Printf("\n健康状态:\n")
	fmt.Printf("  - 异常设备: %d\n", errorCount)
}
EOF

echo ""
echo "=========================================="
echo "访问地址"
echo "=========================================="
echo ""
echo "前端页面："
echo "  - 仪表板: http://localhost:3002/badges-v2/dashboard"
echo "  - 设备库存: http://localhost:3002/badges-v2/inventory"
echo "  - 设备分配: http://localhost:3002/badges-v2/assignment"
echo "  - 设备监控: http://localhost:3002/badges-v2/monitoring"
echo ""
echo "后端 API："
echo "  - 基础地址: http://localhost:18080"
echo "  - 设备列表: GET /api/v2/badges/devices"
echo "  - 仪表板: GET /api/v2/badges/dashboard"
echo ""
echo "=========================================="
echo "✅ 验证完成"
echo "=========================================="
