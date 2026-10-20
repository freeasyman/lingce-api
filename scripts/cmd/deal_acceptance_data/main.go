// 赢客专家验收数据：重复导入或清理。
//
// 用法（必须显式提供数据库连接，不内置任何口令）：
//
//	DATABASE_URL='postgresql://user:pass@host:5432/lingce_dev?sslmode=disable' \
//	  go run ./scripts/cmd/deal_acceptance_data seed
//	DATABASE_URL='...' go run ./scripts/cmd/deal_acceptance_data clean
//
// 只操作验收租户 9001 的固定 ID 区间，不触碰其他租户的数据。
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/lib/pq"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: deal_acceptance_data [seed|clean]")
		os.Exit(2)
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required; read it from configs/secrets.local.toml and pass it explicitly")
		os.Exit(2)
	}
	files := map[string][]string{
		"seed":  {"scripts/sql/20261014_seed_deal_acceptance.sql", "scripts/sql/20261015_seed_deal_e001_e008.sql"},
		"clean": {"scripts/sql/20261015_clean_deal_acceptance.sql"},
	}
	paths, ok := files[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown action %q\n", os.Args[1])
		os.Exit(2)
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open database:", err)
		os.Exit(1)
	}
	defer db.Close()

	for _, path := range paths {
		body, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			fmt.Fprintln(os.Stderr, "read", path, ":", err)
			os.Exit(1)
		}
		if _, err := db.Exec(string(body)); err != nil {
			fmt.Fprintln(os.Stderr, "apply", path, ":", err)
			os.Exit(1)
		}
		fmt.Println("applied", path)
	}
}
