package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load("configs/.env")
	_ = godotenv.Load(".env")

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	fmt.Println("==========================================")
	fmt.Println("根据 manufacturer_id 修复厂家信息")
	fmt.Println("==========================================")
	fmt.Println()

	// 1. 显示修复前的状态
	fmt.Println("[1] 修复前状态:")
	rows, _ := pool.Query(ctx, `
		SELECT bd.manufacturer_id, bd.manufacturer_code, bd.manufacturer_name, COUNT(*) as count
		FROM badge_devices bd
		WHERE bd.deleted_at IS NULL
		GROUP BY bd.manufacturer_id, bd.manufacturer_code, bd.manufacturer_name
		ORDER BY count DESC
	`)
	for rows.Next() {
		var id *int64
		var code, name *string
		var count int
		rows.Scan(&id, &code, &name, &count)
		fmt.Printf("  ID=%v, code=%v, name=%v: %d 台\n",
			ptrToStr(id), ptrToStr(code), ptrToStr(name), count)
	}
	rows.Close()

	// 2. 执行修复：根据 manufacturer_id 更新 code 和 name
	fmt.Println("\n[2] 执行修复...")
	result, err := pool.Exec(ctx, `
		UPDATE badge_devices bd
		SET
			manufacturer_code = bm.code,
			manufacturer_name = bm.name
		FROM badge_manufacturers bm
		WHERE bd.manufacturer_id = bm.id
		  AND bd.deleted_at IS NULL
	`)
	if err != nil {
		log.Fatalf("修复失败: %v", err)
	}

	fmt.Printf("   ✅ 已更新 %d 台设备的厂家信息\n", result.RowsAffected())

	// 3. 显示修复后的状态
	fmt.Println("\n[3] 修复后状态:")
	rows2, _ := pool.Query(ctx, `
		SELECT bd.manufacturer_id, bd.manufacturer_code, bd.manufacturer_name, COUNT(*) as count
		FROM badge_devices bd
		WHERE bd.deleted_at IS NULL
		GROUP BY bd.manufacturer_id, bd.manufacturer_code, bd.manufacturer_name
		ORDER BY count DESC
	`)
	for rows2.Next() {
		var id *int64
		var code, name *string
		var count int
		rows2.Scan(&id, &code, &name, &count)
		fmt.Printf("  ID=%v, code=%v, name=%v: %d 台\n",
			ptrToStr(id), ptrToStr(code), ptrToStr(name), count)
	}
	rows2.Close()

	// 4. 显示示例设备
	fmt.Println("\n[4] 设备示例（前3台）:")
	fmt.Println("------------------------------------------")
	rows3, _ := pool.Query(ctx, `
		SELECT device_no, manufacturer_code, manufacturer_name
		FROM badge_devices
		WHERE deleted_at IS NULL
		LIMIT 3
	`)
	for rows3.Next() {
		var deviceNo, code string
		var name *string
		rows3.Scan(&deviceNo, &code, &name)
		fmt.Printf("  %s -> %s (%s)\n", deviceNo, ptrToStr(name), code)
	}
	rows3.Close()

	fmt.Println("\n==========================================")
	fmt.Println("✅ 厂家信息修复完成！")
	fmt.Println("==========================================")
	fmt.Println("\n现在刷新前端页面，应该能看到正确的厂家信息了")
}

func ptrToStr(p interface{}) string {
	switch v := p.(type) {
	case *int64:
		if v == nil {
			return "NULL"
		}
		return fmt.Sprintf("%d", *v)
	case *string:
		if v == nil {
			return "NULL"
		}
		return *v
	default:
		return "NULL"
	}
}
