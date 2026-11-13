// his-pinyin-load 从 TSV 回填拼音（key \t name \t pinyin \t initials）。
//
//	go run ./cmd/his-pinyin-load -table charge -file charge_pinyin.tsv
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

func main() {
	table := flag.String("table", "charge", "charge|drug|icd")
	file := flag.String("file", "", "TSV 路径")
	dbURL := flag.String("db-url", os.Getenv("LINGCE_DEV_DB_URL"), "PostgreSQL DSN")
	flag.Parse()
	if *file == "" || *dbURL == "" {
		fmt.Fprintln(os.Stderr, "需要 -file 与 -db-url")
		os.Exit(1)
	}
	f, err := os.Open(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开失败:", err)
		os.Exit(1)
	}
	defer f.Close()

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, *dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "连接失败:", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "事务失败:", err)
		os.Exit(1)
	}
	defer tx.Rollback(ctx)

	switch *table {
	case "charge", "drug", "icd":
	default:
		fmt.Fprintln(os.Stderr, "未知 -table:", *table)
		os.Exit(1)
	}

	// 读取 TSV 到内存，COPY 进临时表后一次性 UPDATE（33k 行逐条 UPDATE 走远程 RDS 太慢）。
	type row struct {
		id       int64
		pinyin   string
		initials string
	}
	rowsData := make([]row, 0, 40000)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 4 {
			continue
		}
		var id int64
		if _, err := fmt.Sscan(parts[0], &id); err != nil {
			continue
		}
		rowsData = append(rowsData, row{id: id, pinyin: parts[2], initials: parts[3]})
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE tmp_pinyin (id BIGINT PRIMARY KEY, pinyin TEXT, initials TEXT) ON COMMIT DROP`); err != nil {
		fmt.Fprintln(os.Stderr, "建临时表失败:", err)
		os.Exit(1)
	}
	copied, err := tx.CopyFrom(ctx, pgx.Identifier{"tmp_pinyin"}, []string{"id", "pinyin", "initials"},
		pgx.CopyFromSlice(len(rowsData), func(i int) ([]any, error) {
			return []any{rowsData[i].id, rowsData[i].pinyin, rowsData[i].initials}, nil
		}))
	if err != nil {
		fmt.Fprintln(os.Stderr, "COPY 失败:", err)
		os.Exit(1)
	}
	var updateSQL string
	switch *table {
	case "charge":
		updateSQL = `UPDATE his_charge_items t SET pinyin = s.pinyin, initials = s.initials FROM tmp_pinyin s WHERE t.id = s.id`
	case "drug":
		updateSQL = `UPDATE his_drugs t SET pinyin = s.pinyin, initials = s.initials FROM tmp_pinyin s WHERE t.id = s.id`
	case "icd":
		updateSQL = `INSERT INTO his_icd10_pinyin (catalog_id, pinyin, initials)
			SELECT id, pinyin, initials FROM tmp_pinyin
			ON CONFLICT (catalog_id) DO UPDATE SET pinyin = EXCLUDED.pinyin, initials = EXCLUDED.initials`
	}
	if _, err := tx.Exec(ctx, updateSQL); err != nil {
		fmt.Fprintln(os.Stderr, "批量更新失败:", err)
		os.Exit(1)
	}
	if err := tx.Commit(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "提交失败:", err)
		os.Exit(1)
	}
	fmt.Println("回填完成", copied)
}
