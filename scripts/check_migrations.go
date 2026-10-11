package main

import (
	"context"
	"fmt"
	"log"

	"github.com/BurntSushi/toml"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Database struct {
		Host     string `toml:"host"`
		Port     int    `toml:"port"`
		Name     string `toml:"name"`
		User     string `toml:"user"`
		SSLMode  string `toml:"sslmode"`
		MaxConns int    `toml:"max_conns"`
		MinConns int    `toml:"min_conns"`
	} `toml:"database"`
}

type Secrets struct {
	Database struct {
		Password string `toml:"password"`
	} `toml:"database"`
}

func main() {
	// 加载配置
	var config Config
	if _, err := toml.DecodeFile("./configs/dev.toml", &config); err != nil {
		log.Fatalf("Failed to load config: %v\n", err)
	}

	var secrets Secrets
	if _, err := toml.DecodeFile("./secrets.local.toml", &secrets); err != nil {
		log.Fatalf("Failed to load secrets: %v\n", err)
	}

	connString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		config.Database.User,
		secrets.Database.Password,
		config.Database.Host,
		config.Database.Port,
		config.Database.Name,
		config.Database.SSLMode,
	)

	pool, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	defer pool.Close()

	// 检查 goose_db_version 表
	fmt.Println("=== Migration 版本状态 ===")
	rows, err := pool.Query(context.Background(), `
		SELECT version_id, is_applied, tstamp
		FROM goose_db_version
		WHERE version_id >= 20260917000
		ORDER BY version_id
	`)
	if err != nil {
		log.Fatalf("Query failed: %v\n", err)
	}
	defer rows.Close()

	for rows.Next() {
		var versionID int64
		var isApplied bool
		var tstamp string
		if err := rows.Scan(&versionID, &isApplied, &tstamp); err != nil {
			log.Fatalf("Scan failed: %v\n", err)
		}
		fmt.Printf("Version: %d, Applied: %v, Time: %s\n", versionID, isApplied, tstamp)
	}

	// 检查 followup_tasks 表是否存在
	fmt.Println("\n=== 检查 followup_tasks 表 ===")
	var exists bool
	err = pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'followup_tasks'
		)
	`).Scan(&exists)
	if err != nil {
		log.Fatalf("Query failed: %v\n", err)
	}
	fmt.Printf("followup_tasks 表存在: %v\n", exists)

	// 检查 recording_tasks 表是否存在
	fmt.Println("\n=== 检查 recording_tasks 表 ===")
	err = pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'recording_tasks'
		)
	`).Scan(&exists)
	if err != nil {
		log.Fatalf("Query failed: %v\n", err)
	}
	fmt.Printf("recording_tasks 表存在: %v\n", exists)
}
