package billing_test

// 第 2 批集成测试（真实 PostgreSQL）：
//   · 暂停 → 扣量/预检被拒（SUBSCRIPTION_PAUSED）→ 恢复 → 扣量通过；
//   · 作废订单：只允许未收款未核销、幂等、写 system_action_logs；
//   · 人工调额度：正向/负向、负数越界拒绝、审批人阈值、correction 流水与消耗明细。
//
// 与既有用例一致：未设置 BILLING_TEST_DATABASE_URL / TEST_DATABASE_URL 时 Skip；
// 每个用例自建租户并在结束后清理。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
)

// createLifecycleInstitution 建一个带归属（渠道+销售）的机构。
func createLifecycleInstitution(t *testing.T, env *billingTestEnv, name string) *billing.Institution {
	t.Helper()
	channelName := "生命周期渠道-" + env.suffix
	salesName := "生命周期销售-" + env.suffix
	var channelID int64
	if err := env.pool.QueryRow(env.ctx, `
INSERT INTO ops_organizations (name, type, status) VALUES ($1, 'agency', 'active') RETURNING id`,
		channelName).Scan(&channelID); err != nil {
		t.Fatalf("创建渠道失败: %v", err)
	}
	env.channelIDs = append(env.channelIDs, channelID)
	env.seedSales(channelID, salesName)
	return env.createInstitution(name, "口腔", []string{"口腔科"}, channelName, salesName)
}

// activateLifecycleInstitution 建一个已正式开通的机构（默认只开 yingke）。
func activateLifecycleInstitution(t *testing.T, env *billingTestEnv, products ...string) *billing.Institution {
	t.Helper()
	if len(products) == 0 {
		products = []string{"yingke"}
	}
	institution := createLifecycleInstitution(t, env, "生命周期测试机构")
	endDate := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	activated, err := env.svc.ActivateFormal(env.ctx, institution.ID, billing.FormalActivationRequest{
		ProductCodes: products,
		EndDate:      endDate,
	}, env.adminID)
	if err != nil {
		t.Fatalf("正式开通失败: %v", err)
	}
	return activated
}

// cleanupLifecycleArtifacts 删除本用例新增的审计日志（tenant 会被 env.cleanup 删掉）。
// 注册顺序保证在 env.cleanup 之前执行。
func cleanupLifecycleArtifacts(t *testing.T, env *billingTestEnv) {
	t.Helper()
	t.Cleanup(func() {
		if len(env.tenantIDs) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := env.pool.Exec(ctx, `DELETE FROM system_action_logs WHERE tenant_id = ANY($1)`, env.tenantIDs); err != nil {
			t.Logf("清理 system_action_logs 失败: %v", err)
		}
	})
}

func lifecycleDeductService(env *billingTestEnv) *billing.Service {
	return billing.NewService(billing.NewStore(env.pool))
}

func lifecycleDeduct(env *billingTestEnv, tenantID int64, key string, hours float64) error {
	_, err := lifecycleDeductService(env).Deduct(env.ctx, billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    "yingke",
		Items:          []billing.DeductItem{{Unit: "hour", Amount: hours}},
		IdempotencyKey: key,
		Reason:         "第2批生命周期联调",
	})
	return err
}

func entitlementSnapshot(t *testing.T, env *billingTestEnv, tenantID int64, productCode, unit string) (granted, used, balance, status string) {
	t.Helper()
	if err := env.pool.QueryRow(env.ctx, `
SELECT granted::text, used::text, balance::text, status
FROM product_entitlements
WHERE tenant_id = $1 AND product_code = $2 AND unit = $3 AND quota_type = 'official'
ORDER BY edition, id
LIMIT 1`, tenantID, productCode, unit).Scan(&granted, &used, &balance, &status); err != nil {
		t.Fatalf("读取额度失败: %v", err)
	}
	return granted, used, balance, status
}

func ledgerCount(t *testing.T, env *billingTestEnv, tenantID int64) int64 {
	t.Helper()
	var count int64
	if err := env.pool.QueryRow(env.ctx, `SELECT count(*) FROM entitlement_ledger WHERE tenant_id = $1`, tenantID).Scan(&count); err != nil {
		t.Fatalf("读取流水条数失败: %v", err)
	}
	return count
}

// ---------------- 暂停 / 恢复 ----------------

func TestInstitutionSuspendResumeAndDeductGate(t *testing.T) {
	env := newBillingTestEnv(t)
	cleanupLifecycleArtifacts(t, env)
	institution := activateLifecycleInstitution(t, env)
	tenantID := institution.ID
	if institution.Status != billing.InstitutionStatusActive {
		t.Fatalf("开通后 status 应为 active，实际 %s", institution.Status)
	}
	endBefore := institution.End

	// 扣量通过，证明基线可用。
	if err := lifecycleDeduct(env, tenantID, "batch2:suspend:before", 1); err != nil {
		t.Fatalf("暂停前扣量应成功: %v", err)
	}

	// 暂停：原因必填 + 状态变 paused + 落库。
	if _, err := env.svc.SuspendInstitution(env.ctx, tenantID, billing.SuspendRequest{}, env.adminID); err == nil {
		t.Fatal("暂停原因为空应被拒绝")
	}
	paused, err := env.svc.SuspendInstitution(env.ctx, tenantID,
		billing.SuspendRequest{Reason: "机房租约纠纷"}, env.adminID)
	if err != nil {
		t.Fatalf("暂停失败: %v", err)
	}
	if paused.Status != billing.InstitutionStatusPaused || !paused.Open {
		t.Fatalf("暂停后 status/open 不对: %s/%v", paused.Status, paused.Open)
	}
	if paused.PausedReason != "机房租约纠纷" || paused.PausedAt == "" {
		t.Fatalf("暂停原因/时间未返回: %+v", paused)
	}
	if paused.End != endBefore {
		t.Fatalf("暂停不应延长/改变到期日：%s -> %s", endBefore, paused.End)
	}
	var dbPausedAt *time.Time
	var dbReason string
	if err := env.pool.QueryRow(env.ctx, `
SELECT paused_at, paused_reason FROM tenants WHERE id = $1`, tenantID).Scan(&dbPausedAt, &dbReason); err != nil {
		t.Fatalf("读取 tenants 暂停列失败: %v", err)
	}
	if dbPausedAt == nil || dbReason != "机房租约纠纷" {
		t.Fatalf("暂停未落库: at=%v reason=%q", dbPausedAt, dbReason)
	}

	// 暂停期间：扣量与预检都必须拒绝，且不写流水。
	ledgerBefore := ledgerCount(t, env, tenantID)
	err = lifecycleDeduct(env, tenantID, "batch2:suspend:blocked", 1)
	if !errors.Is(err, billing.ErrSubscriptionPaused) {
		t.Fatalf("暂停期间扣量应返回 ErrSubscriptionPaused，实际 %v", err)
	}
	var checkErr error
	if checkErr = lifecycleDeductService(env).CheckQuota(env.ctx, tenantID, "yingke",
		[]billing.DeductItem{{Unit: "hour", Amount: 1}}); !errors.Is(checkErr, billing.ErrSubscriptionPaused) {
		t.Fatalf("暂停期间预检应返回 ErrSubscriptionPaused，实际 %v", checkErr)
	}
	if after := ledgerCount(t, env, tenantID); after != ledgerBefore {
		t.Fatalf("被拒绝的扣量不应写流水：%d -> %d", ledgerBefore, after)
	}

	// 通知函生成不受暂停影响。
	documents := billing.NewDocumentService(env.svc, billing.NewStore(env.pool))
	if _, err := documents.Generate(env.ctx, tenantID, env.adminID, billing.GenerateDocumentRequest{Kind: "formal"}); err != nil {
		t.Fatalf("暂停期间生成通知函不应失败: %v", err)
	}

	// 重复暂停：幂等、不报错、原因不变。
	again, err := env.svc.SuspendInstitution(env.ctx, tenantID,
		billing.SuspendRequest{Reason: "另一个原因"}, env.adminID)
	if err != nil {
		t.Fatalf("重复暂停应幂等成功: %v", err)
	}
	if again.PausedReason != "机房租约纠纷" {
		t.Fatalf("重复暂停不应覆盖原原因: %q", again.PausedReason)
	}

	// 恢复：状态回到 active、到期日不变、扣量恢复。
	resumed, err := env.svc.ResumeInstitution(env.ctx, tenantID, env.adminID)
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if resumed.Status != billing.InstitutionStatusActive || resumed.PausedAt != "" {
		t.Fatalf("恢复后状态不对: %+v", resumed)
	}
	if resumed.End != endBefore {
		t.Fatalf("恢复后到期日不应变化：%s -> %s", endBefore, resumed.End)
	}
	if err := lifecycleDeduct(env, tenantID, "batch2:suspend:after", 1); err != nil {
		t.Fatalf("恢复后扣量应成功: %v", err)
	}
	// 重复恢复：幂等。
	if _, err := env.svc.ResumeInstitution(env.ctx, tenantID, env.adminID); err != nil {
		t.Fatalf("重复恢复应幂等成功: %v", err)
	}

	// 审计日志：暂停 + 恢复各一条。
	var logs int64
	if err := env.pool.QueryRow(env.ctx, `
SELECT count(*) FROM system_action_logs
WHERE tenant_id = $1 AND action_code IN ('institution.suspend', 'institution.resume')`,
		tenantID).Scan(&logs); err != nil {
		t.Fatalf("读取审计日志失败: %v", err)
	}
	if logs < 2 {
		t.Fatalf("暂停/恢复审计日志应各至少 1 条，实际 %d", logs)
	}
}

// ---------------- 作废订单 ----------------

func seedBillingOrder(t *testing.T, env *billingTestEnv, tenantID int64, suffix string, paid bool, activated bool) int64 {
	t.Helper()
	paidAmount := "0"
	status := "created"
	if paid {
		paidAmount = "100.00"
		status = "paid"
	}
	var orderID int64
	if err := env.pool.QueryRow(env.ctx, `
INSERT INTO billing_orders (order_no, tenant_id, order_type, items, total_amount, payable_amount,
       source, status, reason, paid_amount, activated_at)
VALUES ($1, $2, 'single', '[]'::jsonb, 100, 100, 'direct', $3, '第2批作废测试', $4::numeric,
        CASE WHEN $5 THEN NOW() ELSE NULL END)
RETURNING id`, "VOID-"+env.suffix+"-"+suffix, tenantID, status, paidAmount, activated).Scan(&orderID); err != nil {
		t.Fatalf("插入测试订单失败: %v", err)
	}
	return orderID
}

func TestInstitutionOrderVoid(t *testing.T) {
	env := newBillingTestEnv(t)
	cleanupLifecycleArtifacts(t, env)
	institution := createLifecycleInstitution(t, env, "作废订单测试机构")
	tenantID := institution.ID

	voidableID := seedBillingOrder(t, env, tenantID, "ok", false, false)
	paidID := seedBillingOrder(t, env, tenantID, "paid", true, false)
	activatedID := seedBillingOrder(t, env, tenantID, "activated", false, true)

	items, err := env.svc.ListVoidableOrders(env.ctx, tenantID, env.adminID)
	if err != nil {
		t.Fatalf("查询可作废订单失败: %v", err)
	}
	if len(items) != 1 || items[0].ID != voidableID {
		t.Fatalf("可作废订单应只有 #%d，实际 %+v", voidableID, items)
	}

	if _, err := env.svc.VoidOrder(env.ctx, voidableID, billing.VoidOrderRequest{}, env.adminID); err == nil {
		t.Fatal("作废原因为空应被拒绝")
	}
	voided, err := env.svc.VoidOrder(env.ctx, voidableID,
		billing.VoidOrderRequest{Reason: "录单错误"}, env.adminID)
	if err != nil {
		t.Fatalf("作废订单失败: %v", err)
	}
	if voided.Status != billing.OrderStatusVoid {
		t.Fatalf("作废后状态应为 void，实际 %s", voided.Status)
	}

	// 已收款 / 已核销订单拒绝。
	if _, err := env.svc.VoidOrder(env.ctx, paidID, billing.VoidOrderRequest{Reason: "x"}, env.adminID); !errors.Is(err, billing.ErrOrderNotVoidable) {
		t.Fatalf("已收款订单应拒绝作废，实际 %v", err)
	} else if !strings.Contains(err.Error(), "已收款") {
		t.Fatalf("已收款订单错误提示应说明原因，实际 %v", err)
	}
	if _, err := env.svc.VoidOrder(env.ctx, activatedID, billing.VoidOrderRequest{Reason: "x"}, env.adminID); !errors.Is(err, billing.ErrOrderNotVoidable) {
		t.Fatalf("已核销订单应拒绝作废，实际 %v", err)
	}

	// 已作废再作废：幂等 200，不重复写日志。
	var logsBefore int64
	if err := env.pool.QueryRow(env.ctx, `
SELECT count(*) FROM system_action_logs WHERE tenant_id = $1 AND action_code = 'order.void'`, tenantID).Scan(&logsBefore); err != nil {
		t.Fatalf("读取作废日志失败: %v", err)
	}
	if logsBefore != 1 {
		t.Fatalf("首次作废应写 1 条日志，实际 %d", logsBefore)
	}
	again, err := env.svc.VoidOrder(env.ctx, voidableID,
		billing.VoidOrderRequest{Reason: "重复提交"}, env.adminID)
	if err != nil {
		t.Fatalf("重复作废应幂等成功: %v", err)
	}
	if again.Status != billing.OrderStatusVoid {
		t.Fatalf("重复作废后状态不对: %s", again.Status)
	}
	var logsAfter int64
	if err := env.pool.QueryRow(env.ctx, `
SELECT count(*) FROM system_action_logs WHERE tenant_id = $1 AND action_code = 'order.void'`, tenantID).Scan(&logsAfter); err != nil {
		t.Fatalf("读取作废日志失败: %v", err)
	}
	if logsAfter != logsBefore {
		t.Fatalf("重复作废不应新增日志：%d -> %d", logsBefore, logsAfter)
	}

	// 作废后不再出现在可作废列表。
	items, err = env.svc.ListVoidableOrders(env.ctx, tenantID, env.adminID)
	if err != nil {
		t.Fatalf("再次查询可作废订单失败: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("作废后不应再出现在可作废列表: %+v", items)
	}
}

// ---------------- 人工调额度 ----------------

func TestInstitutionEntitlementCorrection(t *testing.T) {
	env := newBillingTestEnv(t)
	cleanupLifecycleArtifacts(t, env)
	institution := activateLifecycleInstitution(t, env)
	tenantID := institution.ID

	beforeGranted, _, beforeBalance, _ := entitlementSnapshot(t, env, tenantID, "yingke", "hour")
	if beforeGranted != "400.00" || beforeBalance != "400.00" {
		t.Fatalf("开通后额度不对: granted=%s balance=%s", beforeGranted, beforeBalance)
	}

	// 正向 +10：不需要审批人（< 1 包 80 小时）。
	afterAdd, err := env.svc.CorrectEntitlement(env.ctx, tenantID, billing.EntitlementCorrectionRequest{
		ProductCode: "yingke", Unit: "hour", Amount: "10", Reason: "补录交付差异",
	}, env.adminID)
	if err != nil {
		t.Fatalf("正向调额度失败: %v", err)
	}
	if got := afterAdd.Products["yingke"]["hour"]; got.Granted != "410.00" || got.Balance != "410.00" || got.Used != "0.00" {
		t.Fatalf("正向调额度后额度不对: %+v", got)
	}
	var grantType, grantSource, grantReason, grantAmount string
	if err := env.pool.QueryRow(env.ctx, `
SELECT type, source, reason, amount::text FROM entitlement_ledger
WHERE tenant_id = $1 AND source = 'correction'
ORDER BY id DESC LIMIT 1`, tenantID).Scan(&grantType, &grantSource, &grantReason, &grantAmount); err != nil {
		t.Fatalf("读取正向更正流水失败: %v", err)
	}
	if grantType != "grant" || grantSource != "correction" || grantAmount != "10.00" {
		t.Fatalf("正向更正流水不对: type=%s source=%s amount=%s", grantType, grantSource, grantAmount)
	}
	if !strings.Contains(grantReason, "补录交付差异") {
		t.Fatalf("更正流水原因应包含运营填写的原因: %q", grantReason)
	}

	// 负向 -5：改 granted/balance，used 不变。
	afterSub, err := env.svc.CorrectEntitlement(env.ctx, tenantID, billing.EntitlementCorrectionRequest{
		ProductCode: "yingke", Unit: "hour", Amount: "-5", Reason: "录错回退",
	}, env.adminID)
	if err != nil {
		t.Fatalf("负向调额度失败: %v", err)
	}
	if got := afterSub.Products["yingke"]["hour"]; got.Granted != "405.00" || got.Balance != "405.00" || got.Used != "0.00" {
		t.Fatalf("负向调额度后额度不对: %+v", got)
	}
	var subType, subAmount string
	if err := env.pool.QueryRow(env.ctx, `
SELECT type, amount::text FROM entitlement_ledger
WHERE tenant_id = $1 AND source = 'correction'
ORDER BY id DESC LIMIT 1`, tenantID).Scan(&subType, &subAmount); err != nil {
		t.Fatalf("读取负向更正流水失败: %v", err)
	}
	if subType != "usage" || subAmount != "-5.00" {
		t.Fatalf("负向更正流水不对: type=%s amount=%s", subType, subAmount)
	}

	// 负数不能把余额减成负数。
	if _, err := env.svc.CorrectEntitlement(env.ctx, tenantID, billing.EntitlementCorrectionRequest{
		ProductCode: "yingke", Unit: "hour", Amount: "-1000", Reason: "超额回退",
	}, env.adminID); err == nil {
		t.Fatal("减少超过余额应被拒绝")
	} else if _, ok := err.(*billing.ValidationError); !ok {
		t.Fatalf("减少超过余额应返回 ValidationError，实际 %T %v", err, err)
	}
	_, _, balanceAfterReject, _ := entitlementSnapshot(t, env, tenantID, "yingke", "hour")
	if balanceAfterReject != "405.00" {
		t.Fatalf("被拒绝的负向调整不应改余额，实际 %s", balanceAfterReject)
	}

	// 达到 1 个增量包（80 小时）需要审批人。
	if _, err := env.svc.CorrectEntitlement(env.ctx, tenantID, billing.EntitlementCorrectionRequest{
		ProductCode: "yingke", Unit: "hour", Amount: "80", Reason: "补一年包",
	}, env.adminID); err == nil || !strings.Contains(err.Error(), "审批人") {
		t.Fatalf("达到 1 包且无审批人应被拒绝，实际 %v", err)
	}
	withApprover, err := env.svc.CorrectEntitlement(env.ctx, tenantID, billing.EntitlementCorrectionRequest{
		ProductCode: "yingke", Unit: "hour", Amount: "80", Reason: "补一年包", Approver: "胡晓敏",
	}, env.adminID)
	if err != nil {
		t.Fatalf("带审批人的大额调额度失败: %v", err)
	}
	if got := withApprover.Products["yingke"]["hour"]; got.Balance != "485.00" {
		t.Fatalf("大额调整后余额应为 485，实际 %s", got.Balance)
	}
	var approverReason string
	if err := env.pool.QueryRow(env.ctx, `
SELECT reason FROM entitlement_ledger
WHERE tenant_id = $1 AND source = 'correction'
ORDER BY id DESC LIMIT 1`, tenantID).Scan(&approverReason); err != nil {
		t.Fatalf("读取审批人流水失败: %v", err)
	}
	if !strings.Contains(approverReason, "审批人 胡晓敏") {
		t.Fatalf("流水 reason 应带审批人: %q", approverReason)
	}

	// 消耗明细里能看到更正（正负都带符号）。
	detail, err := env.svc.GetInstitution(env.ctx, tenantID)
	if err != nil {
		t.Fatalf("读取机构详情失败: %v", err)
	}
	corrections := make([]billing.InstitutionConsumption, 0, 4)
	for _, row := range detail.Consumptions {
		if row.Source == "correction" {
			corrections = append(corrections, row)
		}
	}
	if len(corrections) < 3 {
		t.Fatalf("消耗明细应至少包含 3 条更正，实际 %d（%+v）", len(corrections), corrections)
	}
	if corrections[0].Deliverable != "人工调额度" {
		t.Fatalf("更正明细的 deliverable 应为「人工调额度」，实际 %q", corrections[0].Deliverable)
	}
	// 最新一条是 +80，上一条是 -5，再上一条是 +10；金额保留符号，前端按符号展示。
	if corrections[0].Amount != "80.00" || corrections[1].Amount != "-5.00" || corrections[2].Amount != "10.00" {
		t.Fatalf("更正明细金额不对：%+v", corrections)
	}

	// 不存在的产品/单位 → 409。
	if _, err := env.svc.CorrectEntitlement(env.ctx, tenantID, billing.EntitlementCorrectionRequest{
		ProductCode: "tingxie", Unit: "hour", Amount: "1", Reason: "补录",
	}, env.adminID); !errors.Is(err, billing.ErrProductNotActive) {
		t.Fatalf("未开通产品调额度应返回 ErrProductNotActive，实际 %v", err)
	}

	// 审计日志。
	var logs int64
	if err := env.pool.QueryRow(env.ctx, `
SELECT count(*) FROM system_action_logs
WHERE tenant_id = $1 AND action_code = 'institution.entitlement_correction'`, tenantID).Scan(&logs); err != nil {
		t.Fatalf("读取调额度日志失败: %v", err)
	}
	if logs != 3 {
		t.Fatalf("成功调额度 3 次应写 3 条日志，实际 %d", logs)
	}

}
