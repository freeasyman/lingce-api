// Command institution-product-admin-backfill 给存量机构的主管理员补齐六个产品的
// 默认管理员权限（EMR / 合规卫士 / 随访顾问 / 客户总监 / 赢客专家 / 获客先锋）。
//
// 背景：机构建档时只写了 employees + institution_employee_roles(role_code=admin)，
// 没有往各产品自己的 *_permission_assignments 表写记录，导致主管理员登录每个产品
// 都被判「无权限」。新建机构已由 tenant.Store.CreateTenant 默认发放；本命令负责
// 存量机构回填。
//
// 幂等与安全：
//   - 每张表都是 INSERT ... ON CONFLICT (tenant_id, employee_id) DO NOTHING；
//   - 已有人工配置（哪怕被停用/降为 staff）一律不覆盖；
//   - 默认 dry-run，必须显式 --apply 才写库；
//   - 拒绝连接生产库（库名包含 prod 时直接退出）；
//   - 支持 --tenant-ids 只回填指定租户。
//
// 用法：
//
//	# 1) 预演全部启用机构
//	go run ./cmd/institution-product-admin-backfill --config ./configs/dev.toml
//
//	# 2) 正式执行（可重复执行，第二次全部 exists）
//	go run ./cmd/institution-product-admin-backfill --config ./configs/dev.toml --apply
//
//	# 3) 只回填指定租户
//	go run ./cmd/institution-product-admin-backfill --config ./configs/dev.toml \
//	  --tenant-ids 5001 --apply
//
//	# 4) 直接指定库（自动化/本地测试）
//	go run ./cmd/institution-product-admin-backfill \
//	  --dsn 'postgresql://yiliiang@127.0.0.1:5432/lingce_billing_test?sslmode=disable' \
//	  --tenant-ids 5001 --apply
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/internal/permission"
	"github.com/jackc/pgx/v5/pgxpool"
)

type options struct {
	ConfigPath      string
	DSN             string
	TenantIDs       []int64
	Apply           bool
	IncludeInactive bool
}

type tenantTarget struct {
	ID     int64
	Name   string
	Active bool
}

type totals struct {
	Tenants         int
	Admins          int
	Granted         int
	Existing        int
	SkippedTenants  int
	SkippedProducts int
	Failures        int
}

func main() {
	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "参数错误:", err)
		os.Exit(2)
	}
	ctx := context.Background()
	dsn := strings.TrimSpace(opts.DSN)
	if dsn == "" {
		cfg, err := config.Load(opts.ConfigPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "加载配置失败:", err)
			os.Exit(1)
		}
		if strings.EqualFold(strings.TrimSpace(cfg.App.Env), "production") {
			fmt.Fprintf(os.Stderr, "拒绝执行：配置 %s 的 app.env=production\n", opts.ConfigPath)
			os.Exit(1)
		}
		dsn = cfg.Database.DSN()
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "连接数据库失败:", err)
		os.Exit(1)
	}
	defer pool.Close()

	target, err := describeTarget(pool)
	if err != nil {
		fmt.Fprintln(os.Stderr, "解析数据库目标失败:", err)
		os.Exit(1)
	}
	if err := assertNotProduction(target.Database); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	mode := "预演（不写库）"
	if opts.Apply {
		mode = "正式执行（写库）"
	}
	fmt.Printf("数据库: %s/%s\n模式: %s\n\n", target.Host, target.Database, mode)

	tenants, err := loadTenants(ctx, pool, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "加载机构失败:", err)
		os.Exit(1)
	}
	report := run(ctx, pool, tenants, opts.Apply, os.Stdout)
	if report.Failures > 0 {
		fmt.Fprintf(os.Stderr, "\n有 %d 个机构处理失败，请检查上面的错误。\n", report.Failures)
		os.Exit(1)
	}
}

func parseOptions(args []string) (options, error) {
	fs := flag.NewFlagSet("institution-product-admin-backfill", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		configPath      = fs.String("config", "./configs/dev.toml", "API TOML 配置路径（--dsn 为空时使用）")
		dsn             = fs.String("dsn", "", "数据库 DSN；为空则从 --config 读取")
		tenantIDs       = fs.String("tenant-ids", "", "只处理指定租户，逗号分隔，例如 5001,5002")
		apply           = fs.Bool("apply", false, "真正写库；默认只预演")
		includeInactive = fs.Bool("include-inactive", false, "未指定租户时也处理已停用机构")
	)
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opts := options{
		ConfigPath:      strings.TrimSpace(*configPath),
		DSN:             strings.TrimSpace(*dsn),
		Apply:           *apply,
		IncludeInactive: *includeInactive,
	}
	if opts.ConfigPath == "" && opts.DSN == "" {
		return options{}, fmt.Errorf("--config 与 --dsn 至少提供一个")
	}
	ids, err := parseTenantIDs(*tenantIDs)
	if err != nil {
		return options{}, err
	}
	opts.TenantIDs = ids
	return opts, nil
}

func parseTenantIDs(raw string) ([]int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	seen := map[int64]struct{}{}
	result := make([]int64, 0)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("非法租户 ID %q", part)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

type dbTarget struct {
	Host     string
	Database string
}

func describeTarget(pool *pgxpool.Pool) (dbTarget, error) {
	cfg := pool.Config().ConnConfig
	return dbTarget{Host: cfg.Host, Database: cfg.Database}, nil
}

// assertNotProduction 只按库名拦截生产库；dev.toml 的 lingce_dev 与测试库放行。
func assertNotProduction(database string) error {
	normalized := strings.ToLower(strings.TrimSpace(database))
	if normalized == "" {
		return fmt.Errorf("无法识别目标数据库名，拒绝执行")
	}
	if normalized == "prod" || strings.HasPrefix(normalized, "lingce_prod") || strings.Contains(normalized, "production") {
		return fmt.Errorf("拒绝连接生产库 %q：本命令只允许开发/测试库", database)
	}
	return nil
}

func loadTenants(ctx context.Context, pool *pgxpool.Pool, opts options) ([]tenantTarget, error) {
	if len(opts.TenantIDs) > 0 {
		rows, err := pool.Query(ctx, `
			SELECT id, name,
			       CASE WHEN is_active::text IN ('1','t','true','TRUE','yes') THEN TRUE ELSE FALSE END
			FROM tenants
			WHERE id = ANY($1::bigint[])
			ORDER BY id
		`, opts.TenantIDs)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := make([]tenantTarget, 0, len(opts.TenantIDs))
		for rows.Next() {
			var item tenantTarget
			if err := rows.Scan(&item.ID, &item.Name, &item.Active); err != nil {
				return nil, err
			}
			result = append(result, item)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		found := map[int64]struct{}{}
		for _, item := range result {
			found[item.ID] = struct{}{}
		}
		for _, id := range opts.TenantIDs {
			if _, ok := found[id]; !ok {
				fmt.Printf("提示: 租户 %d 不存在，已跳过\n", id)
			}
		}
		return result, nil
	}
	query := `
		SELECT id, name,
		       CASE WHEN is_active::text IN ('1','t','true','TRUE','yes') THEN TRUE ELSE FALSE END
		FROM tenants
		WHERE deleted_at IS NULL
	`
	if !opts.IncludeInactive {
		query += ` AND CASE WHEN is_active::text IN ('1','t','true','TRUE','yes') THEN TRUE ELSE FALSE END`
	}
	query += ` ORDER BY id`
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]tenantTarget, 0)
	for rows.Next() {
		var item tenantTarget
		if err := rows.Scan(&item.ID, &item.Name, &item.Active); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func run(ctx context.Context, pool *pgxpool.Pool, tenants []tenantTarget, apply bool, out io.Writer) totals {
	var summary totals
	summary.Tenants = len(tenants)
	writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	defer writer.Flush()

	for _, tenant := range tenants {
		start := time.Now()
		status := "启用"
		if !tenant.Active {
			status = "停用"
		}
		fmt.Fprintf(out, "== 租户 %d %s [%s] ==\n", tenant.ID, tenant.Name, status)

		tx, err := pool.Begin(ctx)
		if err != nil {
			fmt.Fprintf(out, "  ERROR 开启事务失败: %v\n", err)
			summary.Failures++
			continue
		}
		admin, err := permission.FindInstitutionPrimaryAdmin(ctx, tx, tenant.ID)
		if err != nil {
			_ = tx.Rollback(ctx)
			fmt.Fprintf(out, "  ERROR 查找主管理员失败: %v\n", err)
			summary.Failures++
			continue
		}
		if admin == nil {
			_ = tx.Rollback(ctx)
			fmt.Fprintf(out, "  SKIP 没有可用的机构管理员\n")
			summary.SkippedTenants++
			continue
		}
		summary.Admins++
		fmt.Fprintf(out, "  主管理员: %d %s (%s)\n", admin.EmployeeID, admin.Name, permission.PrimaryAdminSourceLabel(admin.Source))

		results, err := permission.EnsureInstitutionAdminGrants(ctx, tx, tenant.ID, admin.EmployeeID, admin.EmployeeID, !apply)
		if err != nil {
			_ = tx.Rollback(ctx)
			fmt.Fprintf(out, "  ERROR 默认授权失败: %v\n", err)
			summary.Failures++
			continue
		}
		if apply {
			if err := tx.Commit(ctx); err != nil {
				fmt.Fprintf(out, "  ERROR 提交失败: %v\n", err)
				summary.Failures++
				continue
			}
		} else {
			_ = tx.Rollback(ctx)
		}
		for _, item := range results {
			reason := ""
			if item.Reason != "" {
				reason = "  (" + item.Reason + ")"
			}
			fmt.Fprintf(writer, "  %s\t%s%s\n", item.Product, item.Status, reason)
			switch item.Status {
			case permission.GrantStatusGranted:
				summary.Granted++
			case permission.GrantStatusExists:
				summary.Existing++
			case permission.GrantStatusSkipped:
				summary.SkippedProducts++
			}
		}
		_ = writer.Flush()
		fmt.Fprintf(out, "  用时: %s\n\n", time.Since(start).Round(time.Millisecond))
	}

	fmt.Fprintf(out, "汇总: 机构=%d 主管理员=%d 新发放=%d 已存在=%d 跳过机构=%d 跳过产品=%d 失败=%d\n",
		summary.Tenants, summary.Admins, summary.Granted, summary.Existing, summary.SkippedTenants, summary.SkippedProducts, summary.Failures)
	if !apply {
		fmt.Fprintln(out, "模式=预演；确认无误后加 --apply 正式写库。")
	}
	return summary
}
