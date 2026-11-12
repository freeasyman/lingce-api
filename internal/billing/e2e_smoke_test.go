package billing_test

// 计费缴费「第 0 阶段」端到端冒烟测试（真实 PostgreSQL）。
//
// 运行前置：本地测试库已按 scripts/billing/setup-billing-test-db.sh 建好并跑完迁移
// （4 张计费表 + product_catalog 12 行 + tenant_subscriptions 服务期列）。
//
// 运行方式：
//
//	BILLING_TEST_DATABASE_URL='postgresql://yiliiang:billing_local_test@127.0.0.1:5432/lingce_billing_test?sslmode=disable' \
//	  go test ./internal/billing/ -run TestBillingEndToEnd -v
//
// 与 concurrency_test.go 的约定一致：未设置 BILLING_TEST_DATABASE_URL / TEST_DATABASE_URL
// 时直接 Skip。用例自建独立租户（时间戳 id），结束按租户逐表清理，不碰其它业务数据。
//
// 覆盖链路（每一步都断言真实库里的余额/流水/服务期）：
//
//	录单 → 回款确认 → 开通（建服务期 + 发额度）
//	→ 交付扣量（按 emrrecord 听写病历钩子的真实输入调用，验证 结转 → 试用 → 正式）
//	→ 幂等（同 idempotency_key 重复扣只扣一次）
//	→ 续费结转（只结转一次）
//	→ 续费脱期 > 30 天（不结转）
//	→ 到期清零
//
// 关于「6 个产品钩子」：六个钩子（emrrecord/content/followup/dealexpert/patientops/
// complianceguard）的扣量入口都是包内未导出的私有方法，且需要业务上下文（真实录音、
// 质检报告等），无法在计费包内构造。这里用与 emrrecord 钩子完全一致的 DeductInput
// （产品 tingxie、record+hour、幂等键 emr:record:<id>:draft、原因「听写病历-病历草稿生成」）
// 驱动同一个 Service.Deduct；钩子到 Deduct 的转发本身由 internal/emrrecord 的既有单测覆盖。

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------- 用例主体 ----------

func TestBillingEndToEnd(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("BILLING_TEST_DATABASE_URL"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	}
	if dsn == "" {
		t.Skip("set BILLING_TEST_DATABASE_URL (or TEST_DATABASE_URL) for billing PostgreSQL integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect billing test database: %v", err)
	}
	t.Cleanup(pool.Close)

	// 迁移必须已经把计费表和价目种子建好；缺表说明跑错了库或没跑迁移。
	assertMigratedSchema(t, ctx, pool)

	// 本地 schema-only 基线不含业务种子；tenant_subscription_plans 的 standard 套餐
	// 在各环境都有（id=1），这里幂等补一条，保证开通能读到 plan_id。
	ensureStandardPlan(t, ctx, pool)

	tenantID := seedSmokeTenant(t, ctx, pool)
	t.Cleanup(func() {
		cleanupSmokeTenant(t, pool, tenantID)
	})

	f := &smokeFlow{
		t:        t,
		ctx:      ctx,
		pool:     pool,
		svc:      billing.NewService(billing.NewStore(pool)),
		tenantID: tenantID,
		product:  "tingxie",
	}

	f.run()
}

func (f *smokeFlow) run() {
	t := f.t

	// ---------- 1. 录单：试用单 + 标准单 ----------
	f.section("录单：试用订单（tingxie trial，hour 5 / record 2）")
	trialOrder := f.createOrder(billing.OrderTypeTrial, billing.EditionTrial, "", billing.QuantityInputs{
		"hour": "5", "record": "2",
	}, "冒烟-试用")
	f.expectOrder(trialOrder, billing.OrderStatusCreated, "0", "0")

	f.section("开通：试用单直接开通（建服务期 + 发试用额度）")
	trialActivation := f.activate(trialOrder)
	if trialActivation.ServicePeriod == nil || trialActivation.ServicePeriod.ExpiredOn == nil {
		t.Fatalf("开通后服务期为空: %+v", trialActivation.ServicePeriod)
	}
	startedOn, expiredOn := f.period()
	today := time.Now().Format("2006-01-02")
	f.expectEqual("服务期开始日=今天", today, startedOn)
	f.expectEqual("服务期到期日=今天+12个月-1天", addServiceYear(today), expiredOn)
	f.expectEntitlement("hour", billing.QuotaTypeTrial, "5", "0", "0", "5", "active")
	f.expectEntitlement("record", billing.QuotaTypeTrial, "2", "0", "0", "2", "active")
	f.expectLedgerTail(2, []ledgerRow{
		{Unit: "hour", Type: "grant", Amount: "5", Source: "trial"},
		{Unit: "record", Type: "grant", Amount: "2", Source: "trial"},
	}) // 按 product/unit 排序：hour 在前

	f.section("录单：单独品订单（tingxie standard，hour 10 / record 5）")
	paidOrder := f.createOrder(billing.OrderTypeSingle, billing.EditionStandard, billing.QuotaTypeOfficial,
		billing.QuantityInputs{"hour": "10", "record": "5"}, "")
	f.expectOrder(paidOrder, billing.OrderStatusCreated, "21600", "21600")

	// ---------- 2. 回款确认 ----------
	f.section("回款确认：全款 21600")
	settled := f.settle(paidOrder, "21600", 1)
	f.expectOrder(settled, billing.OrderStatusSettled, "21600", "21600")
	if settled.PaidAt == nil {
		t.Fatal("回款确认后 paid_at 为空")
	}

	// ---------- 3. 开通（标准单不延服务期） ----------
	f.section("开通：标准单发正式额度，复用统一服务期（不延期）")
	stdActivation := f.activate(settled)
	if stdActivation.ServicePeriod == nil || stdActivation.ServicePeriod.ExpiredOn == nil {
		t.Fatalf("开通后服务期为空: %+v", stdActivation.ServicePeriod)
	}
	if got := *stdActivation.ServicePeriod.ExpiredOn; got != expiredOn {
		t.Fatalf("后开通的标准单不应延长服务期: got %s want %s", got, expiredOn)
	}
	f.expectEntitlement("hour", billing.QuotaTypeOfficial, "10", "0", "0", "10", "active")
	f.expectEntitlement("record", billing.QuotaTypeOfficial, "5", "0", "0", "5", "active")

	// ---------- 4. 交付扣量（emrrecord 钩子输入） ----------
	f.section("交付扣量：按听写病历钩子输入扣 record 1 / hour 2（只消费试用）")
	hookKey := fmt.Sprintf("emr:record:%d:draft", f.tenantID)
	hookResult := f.deduct(hookKey, "听写病历-病历草稿生成",
		billing.DeductItem{Unit: "record", Amount: 1},
		billing.DeductItem{Unit: "hour", Amount: 2},
	)
	f.expectLine(hookResult, "record", 1, 0, 1, 0)
	f.expectLine(hookResult, "hour", 2, 0, 2, 0)
	f.expectEntitlement("hour", billing.QuotaTypeTrial, "5", "2", "0", "3", "active")
	f.expectEntitlement("record", billing.QuotaTypeTrial, "2", "1", "0", "1", "active")
	f.expectEntitlement("hour", billing.QuotaTypeOfficial, "10", "0", "0", "10", "active")
	f.expectEntitlement("record", billing.QuotaTypeOfficial, "5", "0", "0", "5", "active")

	// ---------- 5. 幂等 ----------
	f.section("幂等：同一 idempotency_key 重复扣只扣一次")
	ledgerBefore := len(f.ledger())
	replay, err := f.svc.Deduct(f.ctx, billing.DeductInput{
		TenantID:       f.tenantID,
		ProductCode:    f.product,
		Items:          []billing.DeductItem{{Unit: "record", Amount: 1}, {Unit: "hour", Amount: 2}},
		IdempotencyKey: hookKey,
		Reason:         "听写病历-病历草稿生成",
	})
	if err != nil {
		t.Fatalf("重复扣应幂等成功（返回首次结果），实际: %v", err)
	}
	if !replay.Replayed {
		t.Fatalf("重复扣应标记 Replayed=true，实际 %+v", replay)
	}
	if got := len(f.ledger()); got != ledgerBefore {
		t.Fatalf("重复扣不应新增流水: before=%d after=%d", ledgerBefore, got)
	}
	f.expectEntitlement("hour", billing.QuotaTypeTrial, "5", "2", "0", "3", "active")
	f.expectEntitlement("record", billing.QuotaTypeTrial, "2", "1", "0", "1", "active")

	// ---------- 6. 继续扣量，制造续费前余额 ----------
	f.section("扣量：hour 6（先用完试用 3，再用正式 3）")
	second := f.deduct(fmt.Sprintf("emr:record:%d:quality:run-1", f.tenantID), "听写病历-完整质控",
		billing.DeductItem{Unit: "hour", Amount: 6})
	f.expectLine(second, "hour", 6, 0, 3, 3)
	f.expectEntitlement("hour", billing.QuotaTypeTrial, "5", "5", "0", "0", "active")
	f.expectEntitlement("hour", billing.QuotaTypeOfficial, "10", "3", "0", "7", "active")

	// ---------- 7. 续费结转（第一次） ----------
	f.section("续费（mode=renew）：服务期顺延 + 结转上期未用完额度")
	renewOrder := f.createOrder(billing.OrderTypeSingle, billing.EditionStandard, billing.QuotaTypeOfficial,
		billing.QuantityInputs{"hour": "10", "record": "5"}, "")
	f.settle(renewOrder, "21600", 1)
	beforeRenewExpired := expiredOn
	renew1 := f.renew(renewOrder)
	if renew1.ServicePeriod == nil || renew1.ServicePeriod.ExpiredOn == nil {
		t.Fatalf("续费后服务期为空: %+v", renew1.ServicePeriod)
	}
	expectRenewedExpired := addServiceYear(beforeRenewExpired)
	f.expectEqual("续费后到期日顺延 12 个月-1 天", expectRenewedExpired, *renew1.ServicePeriod.ExpiredOn)
	// 正式 hour：旧余额 7 → 结转 7，新发 10，余额 17。
	f.expectEntitlement("hour", billing.QuotaTypeOfficial, "10", "0", "7", "17", "active")
	f.expectEntitlement("record", billing.QuotaTypeOfficial, "5", "0", "5", "10", "active")
	// 试用行不结转，直接清零并写 expired 流水（record 还剩 1）。
	f.expectEntitlement("hour", billing.QuotaTypeTrial, "0", "0", "0", "0", "cleared")
	f.expectEntitlement("record", billing.QuotaTypeTrial, "0", "0", "0", "0", "cleared")
	f.expectLedgerContains([]ledgerRow{
		{Unit: "hour", Type: "grant", Amount: "7", Source: "rollover"},
		{Unit: "hour", Type: "grant", Amount: "10", Source: "purchase"},
		{Unit: "record", Type: "usage", Amount: "-1", Source: "expired"},
	})

	// ---------- 8. 续费后再发试用，用于验证三段扣量顺序 ----------
	f.section("开通：续费后追加试用额度（hour 2）")
	trialTopUp := f.createOrder(billing.OrderTypeTrial, billing.EditionTrial, "",
		billing.QuantityInputs{"hour": "2"}, "冒烟-续费后试用")
	f.activate(trialTopUp)
	f.expectEntitlement("hour", billing.QuotaTypeTrial, "2", "0", "0", "2", "active")

	f.section("扣量：hour 1（只动结转）")
	one := f.deduct(fmt.Sprintf("emr:record:%d:quality:run-2", f.tenantID), "听写病历-完整质控",
		billing.DeductItem{Unit: "hour", Amount: 1})
	f.expectLine(one, "hour", 1, 1, 0, 0)
	f.expectEntitlement("hour", billing.QuotaTypeOfficial, "10", "0", "6", "16", "active")

	// ---------- 9. 续费结转（第二次，验证「只结转一次」） ----------
	f.section("续费（第二次）：旧结转 6 不再滚存，只结转本周期自己剩下的 10")
	renewOrder2 := f.createOrder(billing.OrderTypeSingle, billing.EditionStandard, billing.QuotaTypeOfficial,
		billing.QuantityInputs{"hour": "10", "record": "5"}, "")
	f.settle(renewOrder2, "21600", 1)
	renew2 := f.renew(renewOrder2)
	if renew2.ServicePeriod == nil || renew2.ServicePeriod.ExpiredOn == nil {
		t.Fatalf("第二次续费后服务期为空: %+v", renew2.ServicePeriod)
	}
	// 旧余额 16、旧结转 6 → 新结转 = 16-6 = 10；granted 10 → 余额 20。
	f.expectEntitlement("hour", billing.QuotaTypeOfficial, "10", "0", "10", "20", "active")
	// record：旧余额 10、旧结转 5 → 新结转 5；granted 5 → 余额 10。
	f.expectEntitlement("record", billing.QuotaTypeOfficial, "5", "0", "5", "10", "active")
	f.expectLedgerContains([]ledgerRow{
		{Unit: "hour", Type: "usage", Amount: "-6", Source: "expired"},
		{Unit: "hour", Type: "grant", Amount: "10", Source: "rollover"},
	})
	// 上一轮追加的试用 hour 2 被清零。
	f.expectEntitlement("hour", billing.QuotaTypeTrial, "0", "0", "0", "0", "cleared")

	// ---------- 10. 扣量顺序：结转 → 试用 → 正式 ----------
	f.section("扣量顺序：先发试用 2，再一次性扣 hour 8（结转 8）")
	trialTopUp2 := f.createOrder(billing.OrderTypeTrial, billing.EditionTrial, "",
		billing.QuantityInputs{"hour": "2"}, "冒烟-三段扣量")
	f.activate(trialTopUp2)
	eight := f.deduct(fmt.Sprintf("emr:record:%d:quality:run-3", f.tenantID), "听写病历-完整质控",
		billing.DeductItem{Unit: "hour", Amount: 8})
	f.expectLine(eight, "hour", 8, 8, 0, 0)
	f.expectEntitlement("hour", billing.QuotaTypeOfficial, "10", "0", "2", "12", "active")

	f.section("扣量顺序：hour 3 = 结转 2 + 试用 1 + 正式 0")
	three := f.deduct(fmt.Sprintf("emr:record:%d:quality:run-4", f.tenantID), "听写病历-完整质控",
		billing.DeductItem{Unit: "hour", Amount: 3})
	f.expectLine(three, "hour", 3, 2, 1, 0)
	f.expectEntitlement("hour", billing.QuotaTypeOfficial, "10", "0", "0", "10", "active")
	f.expectEntitlement("hour", billing.QuotaTypeTrial, "2", "1", "0", "1", "active")

	f.section("扣量顺序：hour 2 = 试用 1 + 正式 1")
	two := f.deduct(fmt.Sprintf("emr:record:%d:quality:run-5", f.tenantID), "听写病历-完整质控",
		billing.DeductItem{Unit: "hour", Amount: 2})
	f.expectLine(two, "hour", 2, 0, 1, 1)
	f.expectEntitlement("hour", billing.QuotaTypeOfficial, "10", "1", "0", "9", "active")
	f.expectEntitlement("hour", billing.QuotaTypeTrial, "2", "2", "0", "0", "active")

	// ---------- 11. 脱期超过 30 天：不结转 ----------
	f.section("续费脱期 31 天：上期余额清零，不结转")
	setSubscriptionExpiredOn(f.t, f.ctx, f.pool, f.tenantID, "CURRENT_DATE - 31")
	lateOrder := f.createOrder(billing.OrderTypeSingle, billing.EditionStandard, billing.QuotaTypeOfficial,
		billing.QuantityInputs{"hour": "10", "record": "5"}, "")
	f.settle(lateOrder, "21600", 1)
	late := f.renew(lateOrder)
	if late.ServicePeriod == nil || late.ServicePeriod.ExpiredOn == nil {
		t.Fatalf("脱期续费后服务期为空: %+v", late.ServicePeriod)
	}
	f.expectEqual("脱期续费从今天重新起算", time.Now().Format("2006-01-02"), *late.ServicePeriod.StartedOn)
	f.expectEqual("脱期续费到期日=今天+12个月-1天", addServiceYear(time.Now().Format("2006-01-02")), *late.ServicePeriod.ExpiredOn)
	f.expectEntitlement("hour", billing.QuotaTypeOfficial, "10", "0", "0", "10", "active")
	f.expectEntitlement("record", billing.QuotaTypeOfficial, "5", "0", "0", "5", "active")
	f.expectLedgerContains([]ledgerRow{
		{Unit: "hour", Type: "usage", Amount: "-9", Source: "expired"},
		{Unit: "record", Type: "usage", Amount: "-10", Source: "expired"},
	})
	if txt := f.ledgerReason("hour", "-9"); !strings.Contains(txt, "脱期超过30天") {
		t.Fatalf("脱期清零流水原因不对: %q", txt)
	}

	// ---------- 12. 到期清零 ----------
	f.section("到期清零：服务期到期后清空全部额度并写 expired 流水")
	setSubscriptionExpiredOn(f.t, f.ctx, f.pool, f.tenantID, "CURRENT_DATE - 1")
	processed, err := f.svc.ExpireDueSubscriptions(f.ctx)
	if err != nil {
		t.Fatalf("到期处理失败: %v", err)
	}
	if processed < 1 {
		t.Fatalf("到期处理应至少处理本次租户，实际 processed=%d", processed)
	}
	f.expectEntitlement("hour", billing.QuotaTypeOfficial, "0", "0", "0", "0", "cleared")
	f.expectEntitlement("record", billing.QuotaTypeOfficial, "0", "0", "0", "0", "cleared")
	var subscriptionStatus string
	if err := f.pool.QueryRow(f.ctx, `SELECT status FROM tenant_subscriptions WHERE tenant_id=$1`, f.tenantID).Scan(&subscriptionStatus); err != nil {
		t.Fatalf("读取订阅状态失败: %v", err)
	}
	f.expectEqual("订阅状态=expired", "expired", subscriptionStatus)
	f.expectLedgerContains([]ledgerRow{
		{Unit: "hour", Type: "usage", Amount: "-10", Source: "expired"},
		{Unit: "record", Type: "usage", Amount: "-5", Source: "expired"},
	})
	if err := f.svc.CheckQuota(f.ctx, f.tenantID, f.product,
		[]billing.DeductItem{{Unit: "hour", Amount: 1}}); !errors.Is(err, billing.ErrInsufficientEntitlement) {
		t.Fatalf("到期清零后预检应返回额度不足，实际: %v", err)
	}
	if _, err := f.svc.Deduct(f.ctx, billing.DeductInput{
		TenantID: f.tenantID, ProductCode: f.product,
		Items:          []billing.DeductItem{{Unit: "hour", Amount: 1}},
		IdempotencyKey: fmt.Sprintf("emr:record:%d:expired", f.tenantID),
	}); !errors.Is(err, billing.ErrInsufficientEntitlement) {
		t.Fatalf("到期清零后扣量应返回额度不足，实际: %v", err)
	}
	f.section("冒烟链路全部通过")
}

// ---------- 步骤与断言工具 ----------

type smokeFlow struct {
	t        *testing.T
	ctx      context.Context
	pool     *pgxpool.Pool
	svc      *billing.Service
	tenantID int64
	product  string
	step     int
}

type ledgerRow struct {
	Unit   string
	Type   string
	Amount string
	Source string
	Reason string
}

type entSnapshot struct {
	Granted   string
	Used      string
	Rollover  string
	Balance   string
	PeriodEnd string
	Status    string
}

func (f *smokeFlow) section(title string) {
	f.step++
	f.t.Logf("── 步骤 %d: %s", f.step, title)
}

func (f *smokeFlow) expect(cond bool, format string, args ...any) {
	f.t.Helper()
	if !cond {
		f.t.Fatalf(format, args...)
	}
}

func (f *smokeFlow) expectEqual(label, want, got string) {
	f.t.Helper()
	if want != got {
		f.t.Fatalf("%s: want %q, got %q", label, want, got)
	}
}

func (f *smokeFlow) expectOrder(order billing.Order, status, total, payable string) {
	f.t.Helper()
	f.expectEqual("订单状态", status, order.Status)
	f.expectEqual("订单总额", money(total), money(order.TotalAmount.String()))
	f.expectEqual("订单应付", money(payable), money(order.PayableAmount.String()))
}

func (f *smokeFlow) createOrder(orderType, edition, quotaType string, quotas billing.QuantityInputs, reason string) billing.Order {
	f.t.Helper()
	order, err := f.svc.CreateOrder(f.ctx, billing.CreateOrderRequest{
		TenantID:  f.tenantID,
		OrderType: orderType,
		Items: []billing.CreateOrderItem{{
			ProductCode: f.product,
			Edition:     edition,
			QuotaType:   quotaType,
			Quotas:      quotas,
		}},
		Reason: reason,
	})
	if err != nil {
		f.t.Fatalf("录单失败（%s/%s）: %v", orderType, edition, err)
	}
	return order
}

func (f *smokeFlow) settle(order billing.Order, amount string, adminID int64) billing.Order {
	f.t.Helper()
	paid := billing.Decimal(amount)
	settled, err := f.svc.SettleOrder(f.ctx, order.ID, billing.SettleOrderRequest{PaidAmount: &paid}, adminID)
	if err != nil {
		f.t.Fatalf("回款确认失败（order=%d）: %v", order.ID, err)
	}
	return settled
}

func (f *smokeFlow) activate(order billing.Order) *billing.ActivationResult {
	f.t.Helper()
	result, err := f.svc.ActivateOrder(f.ctx, order.ID, 1)
	if err != nil {
		f.t.Fatalf("开通失败（order=%d）: %v", order.ID, err)
	}
	return result
}

func (f *smokeFlow) renew(order billing.Order) *billing.ActivationResult {
	f.t.Helper()
	result, err := f.svc.ActivateOrderWithMode(f.ctx, order.ID, 1, billing.ActivateModeRenew)
	if err != nil {
		f.t.Fatalf("续费失败（order=%d）: %v", order.ID, err)
	}
	return result
}

func (f *smokeFlow) deduct(key, reason string, items ...billing.DeductItem) *billing.DeductResult {
	f.t.Helper()
	result, err := f.svc.Deduct(f.ctx, billing.DeductInput{
		TenantID:       f.tenantID,
		ProductCode:    f.product,
		Items:          items,
		IdempotencyKey: key,
		Reason:         reason,
	})
	if err != nil {
		f.t.Fatalf("扣量失败（key=%s）: %v", key, err)
	}
	return result
}

func (f *smokeFlow) expectLine(result *billing.DeductResult, unit string, amount, rollover, trial, official float64) {
	f.t.Helper()
	for _, line := range result.Lines {
		if line.Unit != unit {
			continue
		}
		f.expectNear("扣量明细 "+unit+" amount", amount, line.Amount)
		f.expectNear("扣量明细 "+unit+" rollover", rollover, line.Rollover)
		f.expectNear("扣量明细 "+unit+" trial", trial, line.Trial)
		f.expectNear("扣量明细 "+unit+" official", official, line.Official)
		if sum := line.Rollover + line.Trial + line.Official; math.Abs(sum-line.Amount) > 1e-9 {
			f.t.Fatalf("扣量明细 %s 三段之和 %.4f != amount %.4f", unit, sum, line.Amount)
		}
		return
	}
	f.t.Fatalf("扣量结果缺少单位 %s 的明细: %+v", unit, result.Lines)
}

func (f *smokeFlow) expectNear(label string, want, got float64) {
	f.t.Helper()
	if math.Abs(want-got) > 1e-9 {
		f.t.Fatalf("%s: want %.4f, got %.4f", label, want, got)
	}
}

func (f *smokeFlow) entitlement(unit, quotaType string) entSnapshot {
	f.t.Helper()
	var (
		snapshot entSnapshot
		status   string
	)
	err := f.pool.QueryRow(f.ctx, `
SELECT granted::text, used::text, rollover_bal::text, balance::text,
       to_char(period_end, 'YYYY-MM-DD'), status
FROM product_entitlements
WHERE tenant_id=$1 AND product_code=$2 AND unit=$3 AND quota_type=$4`,
		f.tenantID, f.product, unit, quotaType,
	).Scan(&snapshot.Granted, &snapshot.Used, &snapshot.Rollover, &snapshot.Balance, &snapshot.PeriodEnd, &status)
	if err != nil {
		f.t.Fatalf("读取额度失败（%s/%s）: %v", unit, quotaType, err)
	}
	snapshot.Status = status
	return snapshot
}

func (f *smokeFlow) expectEntitlement(unit, quotaType, granted, used, rollover, balance, status string) {
	f.t.Helper()
	got := f.entitlement(unit, quotaType)
	f.expectEqual(fmt.Sprintf("%s/%s granted", unit, quotaType), money(granted), money(got.Granted))
	f.expectEqual(fmt.Sprintf("%s/%s used", unit, quotaType), money(used), money(got.Used))
	f.expectEqual(fmt.Sprintf("%s/%s rollover_bal", unit, quotaType), money(rollover), money(got.Rollover))
	f.expectEqual(fmt.Sprintf("%s/%s balance", unit, quotaType), money(balance), money(got.Balance))
	f.expectEqual(fmt.Sprintf("%s/%s status", unit, quotaType), status, got.Status)
}

func (f *smokeFlow) period() (started, expired string) {
	f.t.Helper()
	period, err := f.svc.GetServicePeriod(f.ctx, f.tenantID)
	if err != nil {
		f.t.Fatalf("读取服务期失败: %v", err)
	}
	if period == nil || period.StartedOn == nil || period.ExpiredOn == nil {
		f.t.Fatalf("服务期为空: %+v", period)
	}
	return *period.StartedOn, *period.ExpiredOn
}

func (f *smokeFlow) ledger() []ledgerRow {
	f.t.Helper()
	rows, err := f.pool.Query(f.ctx, `
SELECT unit, type, amount::text, source, reason
FROM entitlement_ledger
WHERE tenant_id=$1 AND product_code=$2
ORDER BY id`, f.tenantID, f.product)
	if err != nil {
		f.t.Fatalf("读取流水失败: %v", err)
	}
	defer rows.Close()
	var items []ledgerRow
	for rows.Next() {
		var row ledgerRow
		if err := rows.Scan(&row.Unit, &row.Type, &row.Amount, &row.Source, &row.Reason); err != nil {
			f.t.Fatalf("扫描流水失败: %v", err)
		}
		row.Amount = money(row.Amount)
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatalf("遍历流水失败: %v", err)
	}
	return items
}

// expectLedgerTail 断言最后 n 条流水（按写入顺序）与期望一致。
func (f *smokeFlow) expectLedgerTail(n int, want []ledgerRow) {
	f.t.Helper()
	all := f.ledger()
	if len(all) < n || len(want) != n {
		f.t.Fatalf("流水条数不足: total=%d want tail=%d", len(all), n)
	}
	tail := all[len(all)-n:]
	for i := range want {
		got, exp := tail[i], want[i]
		exp.Amount = money(exp.Amount)
		if got.Unit != exp.Unit || got.Type != exp.Type || got.Amount != exp.Amount || got.Source != exp.Source {
			f.t.Fatalf("第 %d 条流水不符: got %+v want %+v（全部流水 %+v）", i, got, exp, all)
		}
	}
}

// expectLedgerContains 断言流水里存在指定条目（不要求位置）。
func (f *smokeFlow) expectLedgerContains(want []ledgerRow) {
	f.t.Helper()
	all := f.ledger()
	for _, exp := range want {
		exp.Amount = money(exp.Amount)
		found := false
		for _, got := range all {
			if got.Unit == exp.Unit && got.Type == exp.Type && got.Amount == exp.Amount && got.Source == exp.Source {
				found = true
				break
			}
		}
		if !found {
			f.t.Fatalf("流水缺少 %+v；当前流水 %+v", exp, all)
		}
	}
}

func (f *smokeFlow) ledgerReason(unit, amount string) string {
	f.t.Helper()
	var reason string
	if err := f.pool.QueryRow(f.ctx, `
SELECT reason
FROM entitlement_ledger
WHERE tenant_id=$1 AND product_code=$2 AND unit=$3 AND amount=$4::numeric
ORDER BY id DESC LIMIT 1`, f.tenantID, f.product, unit, amount).Scan(&reason); err != nil {
		f.t.Fatalf("读取流水原因失败: %v", err)
	}
	return reason
}

// ---------- 数据库 fixture ----------

func assertMigratedSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var tables int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM information_schema.tables
WHERE table_schema='public'
  AND table_name IN ('product_catalog','product_entitlements','entitlement_ledger','billing_orders')`).Scan(&tables); err != nil {
		t.Fatalf("检查计费表失败: %v", err)
	}
	if tables != 4 {
		t.Fatalf("计费 4 张表未全部建好（%d/4），请先跑 scripts/billing/setup-billing-test-db.sh", tables)
	}
	var catalogRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM product_catalog`).Scan(&catalogRows); err != nil {
		t.Fatalf("检查价目表失败: %v", err)
	}
	if catalogRows != 12 {
		t.Fatalf("product_catalog 应有 12 行（6 产品 × 标准/试用），实际 %d", catalogRows)
	}
}

func ensureStandardPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO tenant_subscription_plans (code, name, duration_days)
VALUES ('standard', '标准版', 365)
ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatalf("补 standard 套餐失败: %v", err)
	}
}

func seedSmokeTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	tenantID := 600000000 + time.Now().UnixNano()%100000000
	code := fmt.Sprintf("billing-smoke-%d", tenantID)
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id, name, code) VALUES ($1, $2, $3)`, tenantID, "计费冒烟租户", code); err != nil {
		t.Fatalf("创建测试租户失败: %v", err)
	}
	return tenantID
}

// cleanupSmokeTenant 只删除本用例创建的租户及其计费数据，不动任何其它租户。
func cleanupSmokeTenant(t *testing.T, pool *pgxpool.Pool, tenantID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	statements := []string{
		`DELETE FROM entitlement_ledger WHERE tenant_id=$1`,
		`DELETE FROM product_entitlements WHERE tenant_id=$1`,
		`DELETE FROM billing_orders WHERE tenant_id=$1`,
		`DELETE FROM tenant_subscription_events WHERE tenant_id=$1`,
		`DELETE FROM tenant_subscriptions WHERE tenant_id=$1`,
		`DELETE FROM tenants WHERE id=$1`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement, tenantID); err != nil {
			t.Logf("清理测试数据失败（%s）: %v", statement, err)
		}
	}
}

// setSubscriptionExpiredOn 模拟时间流逝：直接改服务期到期日（expr 形如 CURRENT_DATE - 31）。
func setSubscriptionExpiredOn(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID int64, expr string) {
	t.Helper()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
UPDATE tenant_subscriptions
SET expired_on = %s, status = 'active', updated_at = NOW()
WHERE tenant_id = $1`, expr), tenantID); err != nil {
		t.Fatalf("调整服务期失败: %v", err)
	}
}

// ---------- 时间/金额工具 ----------

func money(value string) string {
	number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return value
	}
	return fmt.Sprintf("%.2f", number)
}

// addServiceYear 复刻 billing.addServiceYear：+12 个月（日夹到月末）-1 天。
func addServiceYear(date string) string {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		panic(err)
	}
	targetMonth := time.Date(parsed.Year(), parsed.Month()+12, 1, 0, 0, 0, 0, time.UTC)
	lastDay := time.Date(targetMonth.Year(), targetMonth.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	day := parsed.Day()
	if day > lastDay {
		day = lastDay
	}
	anniversary := time.Date(targetMonth.Year(), targetMonth.Month(), day, 0, 0, 0, 0, time.UTC)
	return anniversary.AddDate(0, 0, -1).Format("2006-01-02")
}
