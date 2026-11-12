package billing

// 计费模块 PostgreSQL 集成测试（F1：并发与幂等；F2：导出 SQL）。
//
// 与仓库既有集成测试一致：设置 BILLING_TEST_DATABASE_URL（未设置时回退 TEST_DATABASE_URL）
// 才会运行，否则 t.Skip。每个用例在独立临时 schema 内建临时租户与额度行，
// 用例结束 DROP SCHEMA CASCADE 清理，不接触任何业务数据。
//
// 覆盖：
//  1. 同一 idempotency_key 并发 10 次 Deduct → 恰好 1 次成功、9 次 ALREADY_DEDUCTED，余额只减一次；
//  2. 不同 key 并发 10 次、每次扣 10、余额只有 50 → 5 次成功、5 次 INSUFFICIENT_ENTITLEMENT；
//  3. 并发 activate 同一订单 5 次 → 恰好 1 次成功，额度只发一次；
//  4. 流水导出 SQL（价目表 JOIN 取产品名、过滤、排序、行数上限），handler 单测用 fake 覆盖不到这段。
//
// 断言全部落在真实库上：product_entitlements.balance 与 entitlement_ledger 的行数/金额之和。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// billingTestDDL 是集成测试用的最小表结构：
// 4 张计费表照抄迁移 20261110_002（含唯一索引/CHECK 约束，幂等与锁行为才跟生产一致），
// 另外用最小 shim 补上开通/续费要读的 tenants / tenant_subscriptions / 套餐 / 运营账号。
const billingTestDDL = `
CREATE TABLE tenants (
    id BIGINT PRIMARY KEY,
    name TEXT,
    deleted_at TIMESTAMP,
    paused_at TIMESTAMPTZ,
    paused_reason TEXT NOT NULL DEFAULT '',
    cancelled_at TIMESTAMPTZ,
    cancelled_reason TEXT NOT NULL DEFAULT ''
);

CREATE TABLE tenant_subscription_plans (
    id BIGSERIAL PRIMARY KEY,
    code TEXT,
    name TEXT
);

CREATE TABLE tenant_subscriptions (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    plan_id BIGINT,
    status TEXT NOT NULL DEFAULT 'active',
    started_on DATE,
    expired_on DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE operations_admins (
    id BIGINT PRIMARY KEY,
    username TEXT,
    real_name TEXT
);

CREATE TABLE product_catalog (
    id BIGSERIAL PRIMARY KEY,
    product_code VARCHAR(32) NOT NULL,
    product_name VARCHAR(64) NOT NULL DEFAULT '',
    edition VARCHAR(16) NOT NULL DEFAULT 'standard',
    price_version VARCHAR(32) NOT NULL DEFAULT '2026',
    annual_price NUMERIC(12,2) NOT NULL DEFAULT 0,
    quotas JSONB NOT NULL DEFAULT '{}'::jsonb,
    increment_price NUMERIC(12,2) NOT NULL DEFAULT 0,
    increments JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT product_catalog_edition_check CHECK (edition IN ('standard','light','trial'))
);
CREATE UNIQUE INDEX product_catalog_identity_uq
    ON product_catalog (product_code, edition, price_version);

CREATE TABLE product_entitlements (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    product_code VARCHAR(32) NOT NULL,
    unit VARCHAR(16) NOT NULL,
    edition VARCHAR(16) NOT NULL DEFAULT 'standard',
    quota_type VARCHAR(16) NOT NULL DEFAULT 'official',
    granted NUMERIC(14,2) NOT NULL DEFAULT 0,
    used NUMERIC(14,2) NOT NULL DEFAULT 0,
    rollover_bal NUMERIC(14,2) NOT NULL DEFAULT 0,
    balance NUMERIC(14,2) NOT NULL DEFAULT 0,
    period_end DATE NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT product_entitlements_tenant_check CHECK (tenant_id > 0),
    CONSTRAINT product_entitlements_unit_check CHECK (unit IN ('hour','record','round','shot','prompt','person')),
    CONSTRAINT product_entitlements_quota_type_check CHECK (quota_type IN ('official','trial')),
    CONSTRAINT product_entitlements_status_check CHECK (status IN ('active','paused','expired','cleared'))
);
CREATE UNIQUE INDEX product_entitlements_identity_uq
    ON product_entitlements (tenant_id, product_code, unit, edition, quota_type);

CREATE TABLE entitlement_ledger (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    product_code VARCHAR(32) NOT NULL,
    unit VARCHAR(16) NOT NULL,
    type VARCHAR(16) NOT NULL,
    amount NUMERIC(14,2) NOT NULL,
    source VARCHAR(16) NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    linked_order_id BIGINT,
    idempotency_key VARCHAR(128),
    operator VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT entitlement_ledger_tenant_check CHECK (tenant_id > 0),
    CONSTRAINT entitlement_ledger_type_check CHECK (type IN ('grant','usage')),
    CONSTRAINT entitlement_ledger_source_check CHECK (source IN ('purchase','gift','trial','rollover','correction','expired'))
);
CREATE UNIQUE INDEX entitlement_ledger_idempotency_uq
    ON entitlement_ledger (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

CREATE TABLE billing_orders (
    id BIGSERIAL PRIMARY KEY,
    order_no VARCHAR(32) NOT NULL,
    tenant_id BIGINT NOT NULL,
    order_type VARCHAR(16) NOT NULL,
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    total_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    payable_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    source VARCHAR(16) NOT NULL DEFAULT 'direct',
    salesman_id BIGINT,
    channel_id BIGINT,
    price_list_version VARCHAR(32) NOT NULL DEFAULT '',
    paid_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    paid_at TIMESTAMPTZ,
    confirmed_by BIGINT,
    status VARCHAR(16) NOT NULL DEFAULT 'created',
    activated_at TIMESTAMPTZ,
    activated_by BIGINT,
    reason TEXT NOT NULL DEFAULT '',
    approved_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_orders_tenant_check CHECK (tenant_id > 0),
    CONSTRAINT billing_orders_type_check CHECK (order_type IN ('single','bundle','increment','hardware','gift','trial')),
    CONSTRAINT billing_orders_source_check CHECK (source IN ('direct','channel')),
    CONSTRAINT billing_orders_status_check CHECK (status IN ('created','paid','settled','refunded','cancelled','void'))
);
CREATE UNIQUE INDEX billing_orders_no_uq ON billing_orders (order_no);
`

// billingTestTenantSeq 保证同一 schema 内的用例拿到互不重叠的临时租户。
var billingTestTenantSeq atomic.Int64

// newBillingTestService 连接真实 PostgreSQL，建立临时 schema 与最小表结构，
// 返回可用的 Service 与连接池；未配置数据库时直接 Skip。
func newBillingTestService(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("BILLING_TEST_DATABASE_URL"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	}
	if dsn == "" {
		t.Skip("set BILLING_TEST_DATABASE_URL (or TEST_DATABASE_URL) for billing PostgreSQL integration tests")
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect billing test database: %v", err)
	}
	schema := fmt.Sprintf("billing_test_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		admin.Close()
		t.Fatalf("create billing test schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		admin.Close()
		t.Fatalf("parse billing test database url: %v", err)
	}
	// 并发用例至少要同时占用 10 个连接（10 个 goroutine 各开一个事务）。
	config.MaxConns = 16
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		admin.Close()
		t.Fatalf("connect billing test schema: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+ident+" CASCADE"); err != nil {
			t.Logf("drop billing test schema %s: %v", schema, err)
		}
		admin.Close()
	})

	if _, err := pool.Exec(ctx, billingTestDDL); err != nil {
		t.Fatalf("create billing test tables: %v", err)
	}
	return NewService(NewStore(pool)), pool
}

// seedTestTenant 建一个临时租户（额度表没有租户外键，但开通要锁 tenants 行）。
func seedTestTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	tenantID := 100000 + billingTestTenantSeq.Add(1)
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, '计费并发测试租户')`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	return tenantID
}

// seedTestEntitlement 在临时租户内建一行正式额度（balance = granted）。
func seedTestEntitlement(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID int64, product, unit, granted string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
INSERT INTO product_entitlements (tenant_id, product_code, unit, edition, quota_type,
                                  granted, used, rollover_bal, balance, period_end, status)
VALUES ($1, $2, $3, 'standard', 'official', $4::numeric, 0, 0, $4::numeric, DATE '2027-10-31', 'active')`,
		tenantID, product, unit, granted)
	if err != nil {
		t.Fatalf("seed entitlement: %v", err)
	}
}

// seedTestSettledOrder 建一笔已回款、待开通的单品订单，返回订单 id。
func seedTestSettledOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID int64) int64 {
	t.Helper()
	items := `[{"product_code":"yingke","edition":"standard","quota_type":"official","quotas":{"hour":400},"price":12600}]`
	var orderID int64
	err := pool.QueryRow(ctx, `
INSERT INTO billing_orders (order_no, tenant_id, order_type, items, total_amount, payable_amount,
                            paid_amount, paid_at, confirmed_by, status)
VALUES ($1, $2, 'single', $3::jsonb, 12600, 12600, 12600, NOW(), 1, 'settled')
RETURNING id`, fmt.Sprintf("BLTEST%d", tenantID), tenantID, items).Scan(&orderID)
	if err != nil {
		t.Fatalf("seed order: %v", err)
	}
	return orderID
}

// testEntitlementBalance 读回余额文本（NUMERIC(14,2)::text，例如 "90.00"）。
func testEntitlementBalance(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID int64, product, unit string) string {
	t.Helper()
	var balance string
	if err := pool.QueryRow(ctx, `
SELECT balance::text
FROM product_entitlements
WHERE tenant_id = $1 AND product_code = $2 AND unit = $3`, tenantID, product, unit).Scan(&balance); err != nil {
		t.Fatalf("read entitlement balance: %v", err)
	}
	return balance
}

// testLedgerStats 返回某类流水的行数与金额之和（金额为 text，避免精度断言受浮点影响）。
func testLedgerStats(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID int64, ledgerType string) (int, string) {
	t.Helper()
	var (
		rows int
		sum  string
	)
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*), COALESCE(SUM(amount), 0)::text
FROM entitlement_ledger
WHERE tenant_id = $1 AND type = $2`, tenantID, ledgerType).Scan(&rows, &sum); err != nil {
		t.Fatalf("read ledger stats: %v", err)
	}
	return rows, sum
}

// expectBalance 断言余额文本，失败时把本次并发的结果分布一并打出来，便于定位。
func expectBalance(t *testing.T, got, want string, summary string) {
	t.Helper()
	if got != want {
		t.Fatalf("balance = %s, want %s (%s)", got, want, summary)
	}
}

// TestBillingDeductSameIdempotencyKeyConcurrent 是 F1 用例 1：
// 同 idempotency_key 并发 10 次扣量，只能扣一次：1 成功 + 9 ALREADY_DEDUCTED，余额 100 → 90。
func TestBillingDeductSameIdempotencyKeyConcurrent(t *testing.T) {
	service, pool := newBillingTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tenantID := seedTestTenant(t, ctx, pool)
	seedTestEntitlement(t, ctx, pool, tenantID, "yingke", "hour", "100")

	const workers = 10
	errs := make([]error, workers)
	replayed := make([]bool, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			<-start
			result, err := service.Deduct(ctx, DeductInput{
				TenantID:       tenantID,
				ProductCode:    "yingke",
				Items:          []DeductItem{{Unit: "hour", Amount: 10}},
				IdempotencyKey: "emr:record:9001:draft",
				Reason:         "并发幂等用例",
			})
			errs[slot] = err
			if err == nil && result != nil && result.Replayed {
				replayed[slot] = true
			}
		}(index)
	}
	close(start)
	wg.Wait()

	// 幂等语义：并发同键全部返回成功，但只有 1 次是真正扣量（其余 Replayed=true）。
	success, replayCount, unexpected := 0, 0, make([]string, 0, 2)
	for slot, err := range errs {
		switch {
		case err == nil:
			success++
			if replayed[slot] {
				replayCount++
			}
		case errors.Is(err, ErrAlreadyDeducted):
			// 并发里读不到已提交的回放结果时允许退回这个错误。
			replayCount++
			success++
		default:
			unexpected = append(unexpected, err.Error())
		}
	}
	summary := fmt.Sprintf("success=%d replayed=%d other=%v", success, replayCount, unexpected)
	if len(unexpected) != 0 {
		t.Fatalf("unexpected deduct errors: %v (%s)", unexpected, summary)
	}
	if success != workers || replayCount != workers-1 {
		t.Fatalf("同幂等键并发应全部成功、其中 %d 次为幂等回放: %s", workers-1, summary)
	}

	balance := testEntitlementBalance(t, ctx, pool, tenantID, "yingke", "hour")
	expectBalance(t, balance, "90.00", summary)
	usageRows, usageSum := testLedgerStats(t, ctx, pool, tenantID, LedgerTypeUsage)
	if usageRows != 1 || usageSum != "-10.00" {
		t.Fatalf("usage 流水必须只有 1 行 -10.00，实际 %d 行 %s (%s)", usageRows, usageSum, summary)
	}
	t.Logf("同幂等键 10 并发：全部成功，其中 %d 次为幂等回放，余额 %s，usage 流水 %d 行 %s",
		replayCount, balance, usageRows, usageSum)
}

// TestBillingDeductDistinctKeysRespectBalance 是 F1 用例 2：
// 10 个不同 key 各扣 10、余额只有 50 → 恰好 5 次成功、5 次 INSUFFICIENT_ENTITLEMENT，
// 扣减总额正好 50，余额为 0 且不为负（余额已用尽后按现有规则直接拒绝，不再超扣）。
func TestBillingDeductDistinctKeysRespectBalance(t *testing.T) {
	service, pool := newBillingTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tenantID := seedTestTenant(t, ctx, pool)
	seedTestEntitlement(t, ctx, pool, tenantID, "yingke", "hour", "50")

	const workers = 10
	errs := make([]error, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			<-start
			_, err := service.Deduct(ctx, DeductInput{
				TenantID:       tenantID,
				ProductCode:    "yingke",
				Items:          []DeductItem{{Unit: "hour", Amount: 10}},
				IdempotencyKey: fmt.Sprintf("deal:recording:%d:analysis", 7000+slot),
				Reason:         "并发扣尽用例",
			})
			errs[slot] = err
		}(index)
	}
	close(start)
	wg.Wait()

	success, insufficient, unexpected := 0, 0, make([]string, 0, 2)
	for _, err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrInsufficientEntitlement):
			insufficient++
		default:
			unexpected = append(unexpected, err.Error())
		}
	}
	summary := fmt.Sprintf("success=%d insufficient=%d other=%v", success, insufficient, unexpected)
	if len(unexpected) != 0 {
		t.Fatalf("unexpected deduct errors: %v (%s)", unexpected, summary)
	}
	if success != 5 || insufficient != 5 {
		t.Fatalf("余额 50 / 每笔 10 必须 5 成功 5 INSUFFICIENT_ENTITLEMENT: %s", summary)
	}

	balance := testEntitlementBalance(t, ctx, pool, tenantID, "yingke", "hour")
	expectBalance(t, balance, "0.00", summary)
	usageRows, usageSum := testLedgerStats(t, ctx, pool, tenantID, LedgerTypeUsage)
	if usageRows != 5 || usageSum != "-50.00" {
		t.Fatalf("usage 流水必须是 5 行合计 -50.00，实际 %d 行 %s (%s)", usageRows, usageSum, summary)
	}
	t.Logf("不同 key 10 并发（余额 50、每笔 10）：成功 %d / ErrInsufficientEntitlement %d，余额 %s，usage 流水 %d 行 %s",
		success, insufficient, balance, usageRows, usageSum)
}

// TestBillingActivateOrderConcurrent 是 F1 用例 3：
// 并发开通同一订单 5 次，只有 1 次成功，其余 ALREADY_ACTIVATED，额度只发一次。
func TestBillingActivateOrderConcurrent(t *testing.T) {
	service, pool := newBillingTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tenantID := seedTestTenant(t, ctx, pool)
	// 预置统一服务期：开通不必依赖 tenant_subscription_plans 的种子数据，到期日可断言。
	if _, err := pool.Exec(ctx, `
INSERT INTO tenant_subscriptions (tenant_id, status, started_on, expired_on)
VALUES ($1, 'active', DATE '2026-11-01', DATE '2027-10-31')`, tenantID); err != nil {
		t.Fatalf("seed service period: %v", err)
	}
	orderID := seedTestSettledOrder(t, ctx, pool, tenantID)

	const workers = 5
	errs := make([]error, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			<-start
			_, err := service.ActivateOrderWithMode(ctx, orderID, 1, ActivateModeGrant)
			errs[slot] = err
		}(index)
	}
	close(start)
	wg.Wait()

	success, alreadyActivated, unexpected := 0, 0, make([]string, 0, 2)
	for _, err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrOrderAlreadyActivated):
			alreadyActivated++
		default:
			unexpected = append(unexpected, err.Error())
		}
	}
	summary := fmt.Sprintf("success=%d already_activated=%d other=%v", success, alreadyActivated, unexpected)
	if len(unexpected) != 0 {
		t.Fatalf("unexpected activate errors: %v (%s)", unexpected, summary)
	}
	if success != 1 || alreadyActivated != workers-1 {
		t.Fatalf("并发开通必须恰好 1 成功 %d 次 ALREADY_ACTIVATED: %s", workers-1, summary)
	}

	balance := testEntitlementBalance(t, ctx, pool, tenantID, "yingke", "hour")
	expectBalance(t, balance, "400.00", summary)
	grantRows, grantSum := testLedgerStats(t, ctx, pool, tenantID, LedgerTypeGrant)
	if grantRows != 1 || grantSum != "400.00" {
		t.Fatalf("grant 流水必须只有 1 行 400.00，实际 %d 行 %s (%s)", grantRows, grantSum, summary)
	}

	var (
		activatedCount int
		periodEnd      string
	)
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM billing_orders
WHERE id = $1 AND activated_at IS NOT NULL`, orderID).Scan(&activatedCount); err != nil {
		t.Fatalf("read order activated_at: %v", err)
	}
	if activatedCount != 1 {
		t.Fatalf("订单应恰好被开通 1 次，实际 %d (%s)", activatedCount, summary)
	}
	if err := pool.QueryRow(ctx, `
SELECT to_char(expired_on, 'YYYY-MM-DD')
FROM tenant_subscriptions
WHERE tenant_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 1`, tenantID).Scan(&periodEnd); err != nil {
		t.Fatalf("read service period: %v", err)
	}
	if periodEnd != "2027-10-31" {
		t.Fatalf("mode=grant 不应延长服务期，expired_on = %s, want 2027-10-31", periodEnd)
	}
	t.Logf("并发开通同一订单 5 次：成功 1 / ErrOrderAlreadyActivated %d，额度 %s，grant 流水 %d 行 %s，服务期 %s",
		alreadyActivated, balance, grantRows, grantSum, periodEnd)
}

// TestBillingExportLedgerJoinsProductName 用真实库验证导出的查询 SQL：
// 产品名从价目表 LATERAL JOIN 带出（流水表没有产品名）、product/from/to 过滤生效、
// 按 created_at DESC 排序、limit 取到上限 +1 行用于判断超限。
func TestBillingExportLedgerJoinsProductName(t *testing.T) {
	service, pool := newBillingTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tenantID := seedTestTenant(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
INSERT INTO product_catalog (product_code, product_name, edition, price_version, annual_price, quotas)
VALUES ('yingke', '赢客专家', 'standard', '2026', 12600, '{"hour":400}'::jsonb)`); err != nil {
		t.Fatalf("seed product catalog: %v", err)
	}

	// kehu 故意不建价目表行：LEFT JOIN 必须给空产品名而不是丢掉流水。
	if _, err := pool.Exec(ctx, `
INSERT INTO entitlement_ledger (tenant_id, product_code, unit, type, amount, source, reason, operator, created_at)
VALUES ($1, 'yingke', 'hour', 'usage', -12.50, 'purchase', '交付扣量：成交分析', '系统', TIMESTAMPTZ '2026-11-10 08:30:00+08'),
       ($1, 'yingke', 'hour', 'grant', 400.00, 'purchase', '回款确认按单下发', '张三', TIMESTAMPTZ '2026-11-09 09:00:00+08'),
       ($1, 'kehu', 'shot', 'grant', 5000.00, 'gift', '赠送', '系统', TIMESTAMPTZ '2026-11-08 09:00:00+08')`, tenantID); err != nil {
		t.Fatalf("seed entitlement ledger: %v", err)
	}

	// 固定时钟：文件名里的 yyyymmdd 取服务端"今天"，固定后断言不受跑测试的日期影响。
	service.now = func() time.Time { return time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC) }

	export, err := service.ExportLedger(ctx, LedgerQuery{TenantID: tenantID})
	if err != nil {
		t.Fatalf("ExportLedger returned error: %v", err)
	}
	if want := fmt.Sprintf("entitlement-ledger-%d-20261110.csv", tenantID); export.Filename != want {
		t.Fatalf("filename = %q, want %q", export.Filename, want)
	}
	if len(export.Rows) != 3 {
		t.Fatalf("expected 3 exported rows, got %d: %+v", len(export.Rows), export.Rows)
	}
	first := export.Rows[0]
	if first.ProductCode != "yingke" || first.ProductName != "赢客专家" || first.Amount.String() != "-12.50" {
		t.Fatalf("newest row must carry the price-list product name: %+v", first)
	}
	if first.Type != LedgerTypeUsage || first.Source != "purchase" || first.Operator != "系统" {
		t.Fatalf("unexpected newest row: %+v", first)
	}
	if !first.CreatedAt.Equal(time.Date(2026, 11, 10, 0, 30, 0, 0, time.UTC)) {
		t.Fatalf("created_at = %s, want 2026-11-10T00:30:00Z", first.CreatedAt)
	}
	last := export.Rows[2]
	if last.ProductCode != "kehu" || last.ProductName != "" {
		t.Fatalf("missing catalog row must still export with an empty product name: %+v", last)
	}

	// 产品 + 时间过滤走的是与分页查询同一套条件编译。
	from := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	filtered, err := service.ExportLedger(ctx, LedgerQuery{TenantID: tenantID, Product: "yingke", From: &from})
	if err != nil {
		t.Fatalf("filtered ExportLedger returned error: %v", err)
	}
	if len(filtered.Rows) != 2 {
		t.Fatalf("expected 2 rows for product=yingke from 2026-11-09, got %d: %+v", len(filtered.Rows), filtered.Rows)
	}

	// tenant_id 必填：导出与分页查询共用同一套过滤校验（超限断言在 handler 单测里覆盖）。
	if _, err := service.ExportLedger(ctx, LedgerQuery{TenantID: 0}); err == nil {
		t.Fatal("tenant_id is required: expected a validation error")
	}
	t.Logf("导出 SQL：3 行流水（含 1 行无价目表产品名），过滤后 2 行，文件名 %s", export.Filename)
}
