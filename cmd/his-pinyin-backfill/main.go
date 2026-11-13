// his-pinyin-backfill 按租户回填收费项目/药品/患者的空拼音（全拼 + 首字母）。
//
//	go run ./cmd/his-pinyin-backfill -tenant-id 5001 -dry-run
//	go run ./cmd/his-pinyin-backfill -tenant-id 5001
//	go run ./cmd/his-pinyin-backfill -tenant-id 5001 -table patient
//
// 幂等：只处理 pinyin 或 initials 为空的行，重复执行不会改动已回填数据。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/freeasyman/lingce-api/internal/his"
	"github.com/jackc/pgx/v5/pgxpool"
)

type emptyRow struct {
	ID       int64
	TenantID int64
	Name     string
}

// backfillBatchSize 每批合并的行数；避免全量租户逐行往返导致超时。
const backfillBatchSize = 500

func main() {
	tenantID := flag.Int64("tenant-id", 0, "目标租户 ID（0 = 扫描全部租户）")
	table := flag.String("table", "all", "charge|drug|patient|all")
	dryRun := flag.Bool("dry-run", false, "只统计与预览，不写库")
	dbURL := flag.String("db-url", "", "PostgreSQL DSN（默认 LINGCE_DEV_DB_URL）")
	flag.Parse()

	if *dbURL == "" {
		*dbURL = os.Getenv("LINGCE_DEV_DB_URL")
	}
	if *dbURL == "" {
		fmt.Fprintln(os.Stderr, "需要 -db-url 或 LINGCE_DEV_DB_URL")
		os.Exit(1)
	}
	switch *table {
	case "charge", "drug", "patient", "all":
	default:
		fmt.Fprintln(os.Stderr, "未知 -table:", *table)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "连接失败:", err)
		os.Exit(1)
	}
	defer pool.Close()

	total := 0
	if *table == "charge" || *table == "all" {
		count, err := backfill(ctx, pool, "his_charge_items", *tenantID, *dryRun)
		if err != nil {
			fmt.Fprintln(os.Stderr, "收费项目回填失败:", err)
			os.Exit(1)
		}
		fmt.Printf("收费项目 his_charge_items: %d 行%s\n", count, dryRunSuffix(*dryRun))
		total += count
	}
	if *table == "drug" || *table == "all" {
		count, err := backfill(ctx, pool, "his_drugs", *tenantID, *dryRun)
		if err != nil {
			fmt.Fprintln(os.Stderr, "药品回填失败:", err)
			os.Exit(1)
		}
		fmt.Printf("药品 his_drugs: %d 行%s\n", count, dryRunSuffix(*dryRun))
		total += count
	}
	if *table == "patient" || *table == "all" {
		count, err := backfill(ctx, pool, "customers", *tenantID, *dryRun)
		if err != nil {
			fmt.Fprintln(os.Stderr, "患者回填失败:", err)
			os.Exit(1)
		}
		fmt.Printf("患者 customers: %d 行%s\n", count, dryRunSuffix(*dryRun))
		total += count
	}
	fmt.Printf("合计 %d 行，tenant=%d dry_run=%v\n", total, *tenantID, *dryRun)
}

func dryRunSuffix(dryRun bool) string {
	if dryRun {
		return "（dry-run，未写库）"
	}
	return ""
}

// backfill 扫描指定表空拼音行并批量回填；表名由调用方固定，不接受外部输入。
func backfill(ctx context.Context, pool *pgxpool.Pool, table string, tenantID int64, dryRun bool) (int, error) {
	// 患者表同样存在空姓名（截图线索/匿名档案），空姓名没有可回填的拼音，
	// 排除后重复执行不会反复命中，保持幂等。
	where := "($1 = 0 OR tenant_id = $1) AND (pinyin = '' OR initials = '')"
	if table == "customers" {
		where += " AND COALESCE(BTRIM(name), '') <> ''"
	}
	query := fmt.Sprintf(`
		SELECT id, tenant_id, name FROM %s
		WHERE %s
		ORDER BY tenant_id, id`, table, where)
	rows, err := pool.Query(ctx, query, tenantID)
	if err != nil {
		return 0, err
	}
	items := make([]emptyRow, 0, 256)
	for rows.Next() {
		var item emptyRow
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if dryRun || len(items) == 0 {
		for index, item := range items {
			if index >= 5 {
				break
			}
			pinyin, initials := his.PinyinOf(item.Name)
			fmt.Printf("  [dry-run] %s id=%d tenant=%d %q -> %s/%s\n", table, item.ID, item.TenantID, item.Name, pinyin, initials)
		}
		return len(items), nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// 批量 UPDATE：全量租户（3 万+ 行）逐行往返会触发 120s 超时；
	// 用 unnest 把一批行合成一条语句，仍按 id+tenant 精确匹配且保持幂等条件。
	updateSQL := fmt.Sprintf(`
		UPDATE %s AS t
		SET pinyin = v.pinyin, initials = v.initials
		FROM (
			SELECT unnest($1::bigint[]) AS id,
			       unnest($2::bigint[]) AS tenant_id,
			       unnest($3::text[]) AS pinyin,
			       unnest($4::text[]) AS initials
		) v
		WHERE t.id = v.id AND t.tenant_id = v.tenant_id
		  AND (t.pinyin = '' OR t.initials = '')`, table)
	updated := 0
	for start := 0; start < len(items); start += backfillBatchSize {
		end := start + backfillBatchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[start:end]
		ids := make([]int64, 0, len(batch))
		tenantIDs := make([]int64, 0, len(batch))
		pinyins := make([]string, 0, len(batch))
		initialList := make([]string, 0, len(batch))
		for _, item := range batch {
			pinyin, initials := his.PinyinOf(item.Name)
			ids = append(ids, item.ID)
			tenantIDs = append(tenantIDs, item.TenantID)
			pinyins = append(pinyins, pinyin)
			initialList = append(initialList, initials)
		}
		tag, err := tx.Exec(ctx, updateSQL, ids, tenantIDs, pinyins, initialList)
		if err != nil {
			return 0, err
		}
		updated += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return updated, nil
}
