// Command billing-legacy-backfill 给存量租户补发计费额度（阶段 0 上线前的一次性运营工具）。
//
// 规则（已与业务确认）：
//   - 老客户默认六个产品全部发放「标准档 + 正式额度」，额度值取 product_catalog
//     标准档每单位的 quotas；
//   - 到期日取该租户现有订阅的到期日（tenant_subscriptions.expired_on）；
//   - 来源标记 legacy_migration + 批次号，例如 MIG-20261112-01；
//   - 预演清单每个产品一个勾选项、默认全勾，运营可用 --exclude-products 逐个取消；
//   - 支持 dry-run（只出报表不写库）、按批次撤销、重复执行不重复发放。
//
// 用法（默认 dry-run，必须显式 --apply 才写库）：
//
//	# 1) 预演：输出每个租户 × 产品的发放清单，并写一份可勾选的 CSV
//	go run ./cmd/billing-legacy-backfill \
//	  --dsn 'postgresql://yiliiang@127.0.0.1:5432/lingce_billing_test?sslmode=disable' \
//	  --batch MIG-20261112-01 \
//	  --csv /tmp/legacy-backfill-preview.csv
//
//	# 2) 取消某几个产品（运营在预演清单上取消勾选）：重复预演确认
//	go run ./cmd/billing-legacy-backfill --dsn '...' --batch MIG-20261112-01 \
//	  --exclude-products yingke,huoke
//
//	# 3) 正式执行
//	go run ./cmd/billing-legacy-backfill --dsn '...' --batch MIG-20261112-01 --apply
//
//	# 4) 按批次撤销（同样默认 dry-run，加 --apply 才写库）
//	go run ./cmd/billing-legacy-backfill --dsn '...' --batch MIG-20261112-01 --revoke
//	go run ./cmd/billing-legacy-backfill --dsn '...' --batch MIG-20261112-01 --revoke --apply
//
//	# 5) 只处理指定租户（抽查/返工）
//	go run ./cmd/billing-legacy-backfill --dsn '...' --batch MIG-20261112-01 --tenant-ids 1,2,3
//
// 幂等与追溯：
//   - 每条发放写一条 entitlement_ledger（type=grant, source=legacy_migration,
//     operator=legacy_migration:<批次>, reason=legacy_migration 存量补发），
//     idempotency_key = legacy:<批次>:<租户>:<产品>:<单位>；唯一索引保证重复执行
//     不会再发第二次（流水插入成功才加额度）。
//   - 撤销写补偿流水（type=usage, source=correction, operator=legacy_migration:<批次>:revoke,
//     idempotency_key = legacy-revoke:<批次>:<租户>:<产品>:<单位>），并把额度行扣回到
//     当前余额能承受的范围（不会把余额扣成负数；已消费部分不回滚）。
//
// 安全：脚本拒绝连接生产库（阿里云 RDS lingce_prod / 任何 *.aliyuncs.com），
// 只允许本地或测试库；本阶段不对生产库执行。
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// 生产库 RDS 固定特征：出现即拒绝，避免误连。
	prodHostFragment = "pgm-2ze5k275270fch1pmo.pg.rds.aliyuncs.com"
	prodDatabaseName = "lingce_prod"

	ledgerSourceLegacy     = "legacy_migration"
	ledgerSourceCorrection = "correction"
	operatorPrefix         = "legacy_migration"
	reasonGrant            = "legacy_migration 存量补发"
	reasonRevoke           = "legacy_migration 批次撤销"
)

var batchPattern = regexp.MustCompile(`^MIG-[0-9]{8}-[0-9]{2,3}$`)

// options 是命令行参数。
type options struct {
	DSN            string
	Batch          string
	Exclude        []string
	TenantIDs      []int64
	PriceVersion   string
	CSVPath        string
	Apply          bool
	Revoke         bool
	IncludeExpired bool
	Verbose        bool
}

func main() {
	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "参数错误:", err)
		fmt.Fprintln(os.Stderr, "\n用法见 go doc ./cmd/billing-legacy-backfill 或文件头注释。")
		os.Exit(2)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, opts.DSN)
	if err != nil {
		fmt.Fprintln(os.Stderr, "连接数据库失败:", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := run(ctx, pool, opts, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "执行失败:", err)
		os.Exit(1)
	}
}

func parseOptions(args []string) (options, error) {
	fs := flag.NewFlagSet("billing-legacy-backfill", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		opts       options
		excludeRaw string
		tenantRaw  string
	)
	fs.StringVar(&opts.DSN, "dsn", "", "数据库连接串（必填，只允许本地/测试库）")
	fs.StringVar(&opts.Batch, "batch", "", "批次号，格式 MIG-YYYYMMDD-NN（必填）")
	fs.StringVar(&excludeRaw, "exclude-products", "", "取消勾选的产品编码，逗号分隔，例如 yingke,huoke")
	fs.StringVar(&tenantRaw, "tenant-ids", "", "只处理指定租户，逗号分隔；为空表示全部")
	fs.StringVar(&opts.PriceVersion, "price-version", "", "价目版本；为空取每个产品最新标准档")
	fs.StringVar(&opts.CSVPath, "csv", "", "预演清单 CSV 输出路径（可选）")
	fs.BoolVar(&opts.Apply, "apply", false, "真正写库；不加该参数只做 dry-run 预演")
	fs.BoolVar(&opts.Revoke, "revoke", false, "按批次撤销（配合 --apply 才写库）")
	fs.BoolVar(&opts.IncludeExpired, "include-expired", false, "连订阅已过期的租户也补发（默认跳过）")
	fs.BoolVar(&opts.Verbose, "verbose", false, "逐租户打印明细")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("不认识的参数: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(opts.DSN) == "" {
		return options{}, errors.New("--dsn 必填")
	}
	if err := guardDSN(opts.DSN); err != nil {
		return options{}, err
	}
	opts.Batch = strings.TrimSpace(opts.Batch)
	if !batchPattern.MatchString(opts.Batch) {
		return options{}, fmt.Errorf("--batch 格式应为 MIG-YYYYMMDD-NN，例如 MIG-20261112-01，实际 %q", opts.Batch)
	}
	opts.Exclude = splitCSV(excludeRaw)
	for index := range opts.Exclude {
		opts.Exclude[index] = strings.ToLower(opts.Exclude[index])
	}
	for _, value := range splitCSV(tenantRaw) {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return options{}, fmt.Errorf("--tenant-ids 里的 %q 不是正整数", value)
		}
		opts.TenantIDs = append(opts.TenantIDs, id)
	}
	return opts, nil
}

// guardDSN 拒绝连接生产库；本阶段不对生产库执行任何写操作。
func guardDSN(dsn string) error {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("解析 --dsn 失败: %w", err)
	}
	host := strings.ToLower(config.ConnConfig.Host)
	database := strings.ToLower(config.ConnConfig.Database)
	if strings.Contains(host, prodHostFragment) || strings.Contains(host, "aliyuncs.com") || database == prodDatabaseName {
		return fmt.Errorf("拒绝执行：目标 host=%q database=%q 看起来是生产库；本脚本只允许本地/测试库", host, database)
	}
	return nil
}

// ---------- 数据装载 ----------

type catalogProduct struct {
	Code  string
	Name  string
	Units []catalogUnit
}

type catalogUnit struct {
	Unit string
	Qty  billing.Decimal
}

type tenantRow struct {
	ID        int64
	Name      string
	ExpiredOn string // YYYY-MM-DD
}

func (t tenantRow) expiredDate() (time.Time, error) {
	return time.Parse("2006-01-02", t.ExpiredOn)
}

type grantLine struct {
	ProductCode string
	ProductName string
	Unit        string
	Qty         billing.Decimal
}

type tenantPlan struct {
	Tenant tenantRow
	Lines  []grantLine
}

type skippedTenant struct {
	Tenant tenantRow
	Reason string
}

// loadCatalog 读取标准档价目（每产品取最新版本），把 quotas 拆成逐单位发放明细。
func loadCatalog(ctx context.Context, svc *billing.Service, priceVersion string) ([]catalogProduct, error) {
	items, err := svc.ListCatalog(ctx, billing.CatalogFilter{
		Edition:      billing.EditionStandard,
		PriceVersion: priceVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("读取标准档价目失败: %w", err)
	}
	products := make([]catalogProduct, 0, len(items))
	for _, item := range items {
		var quotas map[string]billing.Decimal
		if len(item.Quotas) > 0 {
			if err := json.Unmarshal(item.Quotas, &quotas); err != nil {
				return nil, fmt.Errorf("解析产品 %s 的 quotas 失败: %w", item.ProductCode, err)
			}
		}
		units := make([]catalogUnit, 0, len(quotas))
		for unit, qty := range quotas {
			units = append(units, catalogUnit{Unit: unit, Qty: qty})
		}
		sort.Slice(units, func(i, j int) bool { return units[i].Unit < units[j].Unit })
		if len(units) == 0 {
			return nil, fmt.Errorf("产品 %s 标准档没有配置任何额度", item.ProductCode)
		}
		products = append(products, catalogProduct{Code: item.ProductCode, Name: item.ProductName, Units: units})
	}
	sort.Slice(products, func(i, j int) bool { return products[i].Code < products[j].Code })
	if len(products) == 0 {
		return nil, errors.New("价目表没有标准档记录，无法补发")
	}
	return products, nil
}

// loadTenants 读所有未删除租户及其最新订阅到期日；没有到期日的租户跳过（无法确定服务期）。
func loadTenants(ctx context.Context, pool *pgxpool.Pool, tenantIDs []int64) ([]tenantRow, []skippedTenant, error) {
	query := `
SELECT t.id, t.name, to_char(s.expired_on, 'YYYY-MM-DD')
FROM tenants t
JOIN LATERAL (
    SELECT expired_on
      FROM tenant_subscriptions s
     WHERE s.tenant_id = t.id
     ORDER BY s.created_at DESC NULLS LAST, s.id DESC
     LIMIT 1
) s ON TRUE
WHERE t.deleted_at IS NULL
  AND s.expired_on IS NOT NULL
ORDER BY t.id`
	args := []any{}
	if len(tenantIDs) > 0 {
		query = `
SELECT t.id, t.name, to_char(s.expired_on, 'YYYY-MM-DD')
FROM tenants t
JOIN LATERAL (
    SELECT expired_on
      FROM tenant_subscriptions s
     WHERE s.tenant_id = t.id
     ORDER BY s.created_at DESC NULLS LAST, s.id DESC
     LIMIT 1
) s ON TRUE
WHERE t.deleted_at IS NULL
  AND s.expired_on IS NOT NULL
  AND t.id = ANY($1::bigint[])
ORDER BY t.id`
		args = append(args, tenantIDs)
	}
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("读取租户订阅失败: %w", err)
	}
	defer rows.Close()
	tenants := make([]tenantRow, 0, 64)
	for rows.Next() {
		var item tenantRow
		if err := rows.Scan(&item.ID, &item.Name, &item.ExpiredOn); err != nil {
			return nil, nil, fmt.Errorf("扫描租户失败: %w", err)
		}
		tenants = append(tenants, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("遍历租户失败: %w", err)
	}

	// 没有订阅到期日的租户无法按规则补发，单独报出来。
	missing, err := loadTenantsWithoutSubscription(ctx, pool, tenantIDs)
	if err != nil {
		return nil, nil, err
	}
	return tenants, missing, nil
}

func loadTenantsWithoutSubscription(ctx context.Context, pool *pgxpool.Pool, tenantIDs []int64) ([]skippedTenant, error) {
	query := `
SELECT t.id, t.name
FROM tenants t
WHERE t.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM tenant_subscriptions s
       WHERE s.tenant_id = t.id AND s.expired_on IS NOT NULL
  )
ORDER BY t.id`
	args := []any{}
	if len(tenantIDs) > 0 {
		query = `
SELECT t.id, t.name
FROM tenants t
WHERE t.deleted_at IS NULL
  AND t.id = ANY($1::bigint[])
  AND NOT EXISTS (
      SELECT 1 FROM tenant_subscriptions s
       WHERE s.tenant_id = t.id AND s.expired_on IS NOT NULL
  )
ORDER BY t.id`
		args = append(args, tenantIDs)
	}
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("读取无订阅租户失败: %w", err)
	}
	defer rows.Close()
	var items []skippedTenant
	for rows.Next() {
		var item skippedTenant
		if err := rows.Scan(&item.Tenant.ID, &item.Tenant.Name); err != nil {
			return nil, fmt.Errorf("扫描无订阅租户失败: %w", err)
		}
		item.Tenant.ExpiredOn = ""
		item.Reason = "没有 tenant_subscriptions 到期日"
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历无订阅租户失败: %w", err)
	}
	return items, nil
}

// buildPlans 生成发放计划：默认六个产品全发，--exclude-products 逐个取消。
func buildPlans(tenants []tenantRow, products []catalogProduct, exclude map[string]bool, includeExpired bool, today time.Time) ([]tenantPlan, []skippedTenant) {
	plans := make([]tenantPlan, 0, len(tenants))
	var skipped []skippedTenant
	for _, tenant := range tenants {
		expired, err := tenant.expiredDate()
		if err != nil {
			skipped = append(skipped, skippedTenant{Tenant: tenant, Reason: "订阅到期日不是合法日期"})
			continue
		}
		if expired.Before(today) && !includeExpired {
			skipped = append(skipped, skippedTenant{Tenant: tenant, Reason: "订阅已过期（" + tenant.ExpiredOn + "）；如需补发加 --include-expired"})
			continue
		}
		plan := tenantPlan{Tenant: tenant}
		for _, product := range products {
			if exclude[product.Code] {
				continue
			}
			for _, unit := range product.Units {
				plan.Lines = append(plan.Lines, grantLine{
					ProductCode: product.Code,
					ProductName: product.Name,
					Unit:        unit.Unit,
					Qty:         unit.Qty,
				})
			}
		}
		if len(plan.Lines) == 0 {
			skipped = append(skipped, skippedTenant{Tenant: tenant, Reason: "所有产品都被取消勾选"})
			continue
		}
		plans = append(plans, plan)
	}
	return plans, skipped
}

// ---------- 主流程 ----------

func run(ctx context.Context, pool *pgxpool.Pool, opts options, out io.Writer) error {
	if opts.Revoke {
		return runRevoke(ctx, pool, opts, out)
	}
	svc := billing.NewService(billing.NewStore(pool))
	products, err := loadCatalog(ctx, svc, opts.PriceVersion)
	if err != nil {
		return err
	}
	exclude, err := normalizeExclusions(opts.Exclude, products)
	if err != nil {
		return err
	}
	tenants, missing, err := loadTenants(ctx, pool, opts.TenantIDs)
	if err != nil {
		return err
	}
	today, err := time.Parse("2006-01-02", time.Now().Format("2006-01-02"))
	if err != nil {
		return err
	}
	plans, skipped := buildPlans(tenants, products, exclude, opts.IncludeExpired, today)
	skipped = append(missing, skipped...)

	printCatalog(out, products, exclude)
	printPreview(out, plans, skipped, opts, today)
	if opts.CSVPath != "" {
		if err := writePreviewCSV(opts.CSVPath, plans, products, exclude); err != nil {
			return err
		}
		fmt.Fprintf(out, "预演清单已写入 %s\n", opts.CSVPath)
	}
	if !opts.Apply {
		fmt.Fprintln(out, "\n[dry-run] 未写库。确认清单无误后加 --apply 正式执行。")
		return nil
	}

	stats, err := applyPlans(ctx, pool, opts.Batch, plans, opts.Verbose, out)
	if err != nil {
		fmt.Fprintf(out, "\n批次 %s 执行中断：%v\n已提交的租户是完整事务，修复后重跑即可（幂等，不会重复发放）。\n", opts.Batch, err)
		return err
	}
	fmt.Fprintf(out, "\n[apply] 批次 %s 完成：租户=%d 发放流水=%d 已存在跳过=%d 额度行=%d\n",
		opts.Batch, stats.tenants, stats.ledgerInserted, stats.ledgerSkipped, stats.entitlements)
	return nil
}

type applyStats struct {
	tenants        int
	ledgerInserted int
	ledgerSkipped  int
	entitlements   int
}

const insertLegacyGrantSQL = `
INSERT INTO entitlement_ledger
    (tenant_id, product_code, unit, type, amount, source, reason, idempotency_key, operator)
VALUES ($1, $2, $3, 'grant', $4::numeric, $5, $6, $7, $8)
ON CONFLICT DO NOTHING`

const upsertLegacyEntitlementSQL = `
INSERT INTO product_entitlements
    (tenant_id, product_code, unit, edition, quota_type, granted, used, rollover_bal, balance, period_end, status)
VALUES ($1, $2, $3, 'standard', 'official', $4::numeric, 0, 0, $4::numeric, $5::date, 'active')
ON CONFLICT (tenant_id, product_code, unit, edition, quota_type)
DO UPDATE SET granted = product_entitlements.granted + EXCLUDED.granted,
              balance = product_entitlements.balance + EXCLUDED.balance,
              period_end = GREATEST(product_entitlements.period_end, EXCLUDED.period_end),
              status = 'active',
              updated_at = NOW()`

// applyPlans 逐租户一个事务：先插幂等流水，插入成功才加额度。
func applyPlans(ctx context.Context, pool *pgxpool.Pool, batch string, plans []tenantPlan, verbose bool, out io.Writer) (applyStats, error) {
	stats := applyStats{}
	for _, plan := range plans {
		inserted, skipped, err := applyOneTenant(ctx, pool, batch, plan, verbose, out)
		stats.ledgerInserted += inserted
		stats.ledgerSkipped += skipped
		stats.entitlements += inserted
		if err != nil {
			return stats, err
		}
		stats.tenants++
		if verbose {
			fmt.Fprintf(out, "  已处理 tenant=%d（%s），额度行 %d（新增 %d / 跳过 %d）\n",
				plan.Tenant.ID, plan.Tenant.Name, len(plan.Lines), inserted, skipped)
		}
	}
	return stats, nil
}

func applyOneTenant(ctx context.Context, pool *pgxpool.Pool, batch string, plan tenantPlan, verbose bool, out io.Writer) (int, int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("开启租户 %d 事务失败: %w", plan.Tenant.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT id FROM tenants WHERE id = $1 FOR UPDATE`, plan.Tenant.ID); err != nil {
		return 0, 0, fmt.Errorf("锁定租户 %d 失败: %w", plan.Tenant.ID, err)
	}
	operator := operatorPrefix + ":" + batch
	inserted, skipped := 0, 0
	for _, line := range plan.Lines {
		key := legacyGrantKey(batch, plan.Tenant.ID, line.ProductCode, line.Unit)
		tag, err := tx.Exec(ctx, insertLegacyGrantSQL,
			plan.Tenant.ID, line.ProductCode, line.Unit, line.Qty.String(),
			ledgerSourceLegacy,
			reasonGrant+"（批次 "+batch+"）",
			key,
			operator,
		)
		if err != nil {
			return inserted, skipped, fmt.Errorf("租户 %d 产品 %s/%s 写流水失败: %w", plan.Tenant.ID, line.ProductCode, line.Unit, err)
		}
		if tag.RowsAffected() == 0 {
			skipped++
			if verbose {
				fmt.Fprintf(out, "  跳过（已发放） tenant=%d %s/%s\n", plan.Tenant.ID, line.ProductCode, line.Unit)
			}
			continue
		}
		if _, err := tx.Exec(ctx, upsertLegacyEntitlementSQL,
			plan.Tenant.ID, line.ProductCode, line.Unit, line.Qty.String(), plan.Tenant.ExpiredOn,
		); err != nil {
			return inserted, skipped, fmt.Errorf("租户 %d 产品 %s/%s 写额度失败: %w", plan.Tenant.ID, line.ProductCode, line.Unit, err)
		}
		inserted++
	}
	if err := tx.Commit(ctx); err != nil {
		return inserted, skipped, fmt.Errorf("提交租户 %d 失败: %w", plan.Tenant.ID, err)
	}
	return inserted, skipped, nil
}

// ---------- 撤销 ----------

type revokeLine struct {
	TenantID    int64
	ProductCode string
	Unit        string
	GrantedQty  billing.Decimal
	Balance     billing.Decimal
	Used        billing.Decimal
	Rollover    billing.Decimal
	Exists      bool
	RevokeQty   billing.Decimal
	AlreadyDone bool
}

func runRevoke(ctx context.Context, pool *pgxpool.Pool, opts options, out io.Writer) error {
	lines, err := loadRevokeLines(ctx, pool, opts.Batch, opts.TenantIDs)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		fmt.Fprintf(out, "批次 %s 没有找到 legacy_migration 发放流水，无需撤销。\n", opts.Batch)
		return nil
	}
	fmt.Fprintf(out, "批次 %s 撤销预演：共 %d 条发放记录\n\n", opts.Batch, len(lines))
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "租户ID\t产品\t单位\t原发放\t当前余额\t已使用\t可撤销\t状态")
	for _, line := range lines {
		status := "可撤销"
		switch {
		case line.AlreadyDone:
			status = "已撤销"
		case !line.Exists:
			status = "额度行不存在"
		case line.RevokeQty.String() == "0":
			status = "余额为0，仅补撤销标记"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			line.TenantID, line.ProductCode, line.Unit,
			moneyText(line.GrantedQty.String()), moneyText(line.Balance.String()),
			moneyText(line.Used.String()), moneyText(line.RevokeQty.String()), status)
	}
	_ = tw.Flush()
	if !opts.Apply {
		fmt.Fprintln(out, "\n[dry-run] 未写库。确认后加 --apply 执行撤销。")
		return nil
	}
	revoked := 0
	skipped := 0
	for _, line := range lines {
		if line.AlreadyDone || !line.Exists {
			skipped++
			continue
		}
		if err := revokeOne(ctx, pool, opts.Batch, line); err != nil {
			return fmt.Errorf("撤销 tenant=%d %s/%s 失败: %w", line.TenantID, line.ProductCode, line.Unit, err)
		}
		revoked++
	}
	fmt.Fprintf(out, "\n[apply] 批次 %s 撤销完成：撤销=%d 跳过=%d\n", opts.Batch, revoked, skipped)
	return nil
}

const selectBatchGrantsSQL = `
SELECT tenant_id, product_code, unit, SUM(amount)::text
FROM entitlement_ledger
WHERE type = 'grant'
  AND source = $1
  AND operator = $2
  AND ($3::bigint[] IS NULL OR tenant_id = ANY($3::bigint[]))
GROUP BY tenant_id, product_code, unit
ORDER BY tenant_id, product_code, unit`

const selectRevokeMarkerSQL = `
SELECT 1 FROM entitlement_ledger
WHERE tenant_id = $1 AND idempotency_key = $2`

const selectEntitlementForUpdateSQL = `
SELECT granted::text, used::text, rollover_bal::text, balance::text
FROM product_entitlements
WHERE tenant_id = $1 AND product_code = $2 AND unit = $3
  AND edition = 'standard' AND quota_type = 'official'
FOR UPDATE`

const insertRevokeLedgerSQL = `
INSERT INTO entitlement_ledger
    (tenant_id, product_code, unit, type, amount, source, reason, idempotency_key, operator)
VALUES ($1, $2, $3, 'usage', $4::numeric, $5, $6, $7, $8)
ON CONFLICT DO NOTHING`

const updateRevokedEntitlementSQL = `
UPDATE product_entitlements
SET granted = $4::numeric,
    rollover_bal = $5::numeric,
    balance = $6::numeric,
    status = $7,
    updated_at = NOW()
WHERE tenant_id = $1 AND product_code = $2 AND unit = $3
  AND edition = 'standard' AND quota_type = 'official'`

func loadRevokeLines(ctx context.Context, pool *pgxpool.Pool, batch string, tenantIDs []int64) ([]revokeLine, error) {
	rows, err := pool.Query(ctx, selectBatchGrantsSQL, ledgerSourceLegacy, operatorPrefix+":"+batch, nullableIDs(tenantIDs))
	if err != nil {
		return nil, fmt.Errorf("读取批次发放流水失败: %w", err)
	}
	defer rows.Close()
	var lines []revokeLine
	for rows.Next() {
		var (
			line    revokeLine
			granted string
		)
		if err := rows.Scan(&line.TenantID, &line.ProductCode, &line.Unit, &granted); err != nil {
			return nil, fmt.Errorf("扫描批次发放流水失败: %w", err)
		}
		line.GrantedQty = billing.Decimal(granted)

		var marker int
		key := legacyRevokeKey(batch, line.TenantID, line.ProductCode, line.Unit)
		err := pool.QueryRow(ctx, selectRevokeMarkerSQL, line.TenantID, key).Scan(&marker)
		if err == nil {
			line.AlreadyDone = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("检查撤销标记失败: %w", err)
		}

		var grantedText, usedText, rolloverText, balanceText string
		err = pool.QueryRow(ctx,
			`SELECT granted::text, used::text, rollover_bal::text, balance::text
			   FROM product_entitlements
			  WHERE tenant_id=$1 AND product_code=$2 AND unit=$3
			    AND edition='standard' AND quota_type='official'`,
			line.TenantID, line.ProductCode, line.Unit,
		).Scan(&grantedText, &usedText, &rolloverText, &balanceText)
		if err == nil {
			line.Exists = true
			line.Balance = billing.Decimal(balanceText)
			line.Used = billing.Decimal(usedText)
			line.Rollover = billing.Decimal(rolloverText)
			line.RevokeQty = computeRevokeQty(line.GrantedQty, line.Balance)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("读取额度行失败: %w", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历批次发放流水失败: %w", err)
	}
	return lines, nil
}

func revokeOne(ctx context.Context, pool *pgxpool.Pool, batch string, line revokeLine) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		grantedText, usedText, rolloverText, balanceText string
	)
	if err := tx.QueryRow(ctx, selectEntitlementForUpdateSQL, line.TenantID, line.ProductCode, line.Unit).
		Scan(&grantedText, &usedText, &rolloverText, &balanceText); err != nil {
		return fmt.Errorf("锁定额度行失败: %w", err)
	}
	granted, used, rollover, balance, err := parseDecimals(grantedText, usedText, rolloverText, balanceText)
	if err != nil {
		return err
	}
	grantedQty, err := parseDecimal(line.GrantedQty.String())
	if err != nil {
		return err
	}
	revokeQty, err := parseDecimal(computeRevokeQty(line.GrantedQty, billing.Decimal(balanceText)).String())
	if err != nil {
		return err
	}

	key := legacyRevokeKey(batch, line.TenantID, line.ProductCode, line.Unit)
	tag, err := tx.Exec(ctx, insertRevokeLedgerSQL,
		line.TenantID, line.ProductCode, line.Unit,
		"-"+decText(revokeQty),
		ledgerSourceCorrection,
		reasonRevoke+"（批次 "+batch+"）",
		key,
		operatorPrefix+":"+batch+":revoke",
	)
	if err != nil {
		return fmt.Errorf("写撤销流水失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil // 并发/重复撤销：唯一键已经挡住
	}

	newGranted := granted - grantedQty
	if newGranted < used {
		newGranted = used
	}
	newBalance := balance - revokeQty
	if newBalance < 0 {
		newBalance = 0
	}
	newRollover := rollover
	if newRollover > newBalance {
		newRollover = newBalance
	}
	status := "active"
	if newBalance <= 0 {
		if used <= 0 {
			status = "cleared"
		} else {
			status = "paused"
		}
	}
	if _, err := tx.Exec(ctx, updateRevokedEntitlementSQL,
		line.TenantID, line.ProductCode, line.Unit,
		moneyText(decText(newGranted)), moneyText(decText(newRollover)), moneyText(decText(newBalance)), status,
	); err != nil {
		return fmt.Errorf("回写撤销后额度失败: %w", err)
	}
	return tx.Commit(ctx)
}

// computeRevokeQty 撤销额度：最多扣掉当前余额能承受的部分，绝不把余额扣成负数。
func computeRevokeQty(grantedQty, balance billing.Decimal) billing.Decimal {
	granted, err := parseDecimal(grantedQty.String())
	if err != nil {
		return billing.Decimal("0")
	}
	available, err := parseDecimal(balance.String())
	if err != nil || available <= 0 {
		return billing.Decimal("0")
	}
	if granted < available {
		return billing.Decimal(decText(granted))
	}
	return billing.Decimal(decText(available))
}

// ---------- 输出 ----------

func printCatalog(out io.Writer, products []catalogProduct, exclude map[string]bool) {
	fmt.Fprintf(out, "标准档产品目录：%d 个产品（期望 6 个）\n", len(products))
	for _, product := range products {
		parts := make([]string, 0, len(product.Units))
		for _, unit := range product.Units {
			parts = append(parts, unit.Unit+"="+moneyText(unit.Qty.String()))
		}
		mark := "✓ 发放"
		if exclude[product.Code] {
			mark = "✗ 已取消"
		}
		fmt.Fprintf(out, "  %-10s %-8s %s  %s\n", product.Code, product.Name, strings.Join(parts, ", "), mark)
	}
	if len(products) != 6 {
		fmt.Fprintf(out, "  ⚠ 目录产品数不是 6，请确认价目表种子是否完整\n")
	}
	fmt.Fprintln(out)
}

func printPreview(out io.Writer, plans []tenantPlan, skipped []skippedTenant, opts options, today time.Time) {
	rows := 0
	for _, plan := range plans {
		rows += len(plan.Lines)
	}
	fmt.Fprintf(out, "预演：批次 %s，今天 %s，待补发租户 %d 个、租户×产品×单位 %d 行\n\n",
		opts.Batch, today.Format("2006-01-02"), len(plans), rows)

	// 按产品汇总，运营一眼能看出每个产品发了多少。
	type productSummary struct {
		tenants int
		units   map[string]float64
	}
	summaries := map[string]*productSummary{}
	for _, plan := range plans {
		for _, line := range plan.Lines {
			summary := summaries[line.ProductCode]
			if summary == nil {
				summary = &productSummary{units: map[string]float64{}}
				summaries[line.ProductCode] = summary
			}
			summary.tenants++
			value, _ := strconv.ParseFloat(line.Qty.String(), 64)
			summary.units[line.Unit] += value
		}
	}
	codes := make([]string, 0, len(summaries))
	for code := range summaries {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "产品\t额度项\t合计额度")
	for _, code := range codes {
		summary := summaries[code]
		fmt.Fprintf(tw, "%s\t%d\t%s\n", code, summary.tenants, formatUnitTotals(summary.units))
	}
	_ = tw.Flush()

	if opts.Verbose {
		fmt.Fprintln(out, "\n逐租户明细：")
		for _, plan := range plans {
			fmt.Fprintf(out, "  tenant=%d %s 到期=%s 行数=%d\n", plan.Tenant.ID, plan.Tenant.Name, plan.Tenant.ExpiredOn, len(plan.Lines))
		}
	}
	if len(skipped) > 0 {
		fmt.Fprintf(out, "\n跳过 %d 个租户：\n", len(skipped))
		limit := len(skipped)
		if limit > 20 {
			limit = 20
		}
		for _, item := range skipped[:limit] {
			fmt.Fprintf(out, "  tenant=%d %s：%s\n", item.Tenant.ID, item.Tenant.Name, item.Reason)
		}
		if len(skipped) > limit {
			fmt.Fprintf(out, "  ... 其余 %d 个省略\n", len(skipped)-limit)
		}
	}
}

func formatUnitTotals(units map[string]float64) string {
	names := make([]string, 0, len(units))
	for unit := range units {
		names = append(names, unit)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, unit := range names {
		parts = append(parts, unit+"="+moneyText(strconv.FormatFloat(units[unit], 'f', 2, 64)))
	}
	return strings.Join(parts, ", ")
}

// writePreviewCSV 输出可勾选清单：每个租户 × 每个标准档产品一行，checked=true 表示默认发放。
// 运营把某行改成 false 后，用 --exclude-products 传对应产品编码重跑预演即可。
func writePreviewCSV(path string, plans []tenantPlan, products []catalogProduct, exclude map[string]bool) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建 CSV 失败: %w", err)
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{"checked", "tenant_id", "tenant_name", "expired_on", "product_code", "product_name", "quantities", "period_end"}); err != nil {
		return err
	}
	for _, plan := range plans {
		for _, product := range products {
			quantities := make([]string, 0, len(product.Units))
			for _, unit := range product.Units {
				quantities = append(quantities, unit.Unit+"="+moneyText(unit.Qty.String()))
			}
			checked := "true"
			if exclude[product.Code] {
				checked = "false"
			}
			if err := writer.Write([]string{
				checked,
				strconv.FormatInt(plan.Tenant.ID, 10),
				plan.Tenant.Name,
				plan.Tenant.ExpiredOn,
				product.Code,
				product.Name,
				strings.Join(quantities, ";"),
				plan.Tenant.ExpiredOn,
			}); err != nil {
				return err
			}
		}
	}
	return writer.Error()
}

// ---------- 小工具 ----------

func normalizeExclusions(raw []string, products []catalogProduct) (map[string]bool, error) {
	known := make(map[string]bool, len(products))
	for _, product := range products {
		known[product.Code] = true
	}
	exclude := make(map[string]bool, len(raw))
	for _, code := range raw {
		if !known[code] {
			return nil, fmt.Errorf("--exclude-products 里的 %q 不在标准档产品目录中（可选：%s）", code, strings.Join(catalogCodes(products), ","))
		}
		exclude[code] = true
	}
	return exclude, nil
}

func catalogCodes(products []catalogProduct) []string {
	codes := make([]string, 0, len(products))
	for _, product := range products {
		codes = append(codes, product.Code)
	}
	return codes
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func nullableIDs(ids []int64) any {
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func legacyGrantKey(batch string, tenantID int64, product, unit string) string {
	return fmt.Sprintf("legacy:%s:%d:%s:%s", batch, tenantID, product, unit)
}

func legacyRevokeKey(batch string, tenantID int64, product, unit string) string {
	return fmt.Sprintf("legacy-revoke:%s:%d:%s:%s", batch, tenantID, product, unit)
}

func parseDecimal(raw string) (int64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("解析金额 %q 失败: %w", raw, err)
	}
	return int64(math.Round(value * 100)), nil
}

func parseDecimals(values ...string) (int64, int64, int64, int64, error) {
	if len(values) != 4 {
		return 0, 0, 0, 0, errors.New("需要四个金额")
	}
	granted, err := parseDecimal(values[0])
	if err != nil {
		return 0, 0, 0, 0, err
	}
	used, err := parseDecimal(values[1])
	if err != nil {
		return 0, 0, 0, 0, err
	}
	rollover, err := parseDecimal(values[2])
	if err != nil {
		return 0, 0, 0, 0, err
	}
	balance, err := parseDecimal(values[3])
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return granted, used, rollover, balance, nil
}

func decText(cents int64) string {
	return strconv.FormatFloat(float64(cents)/100, 'f', 2, 64)
}

func moneyText(raw string) string {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return raw
	}
	return strconv.FormatFloat(value, 'f', 2, 64)
}
