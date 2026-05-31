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
