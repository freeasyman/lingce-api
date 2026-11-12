package billing_test

// 收款单与核销集成测试：一笔收款核销两个租户的订单、部分核销、重复核销幂等、
// 核销金额不能超过应收、数据权限外的订单不能核销。

import (
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
)

// seedChannelWithSales 建一个渠道 + 一个销售，返回渠道名/销售名。
func (e *billingTestEnv) seedChannelWithSales(prefix string) (string, string) {
	e.t.Helper()
	channelName := prefix + "渠道-" + e.suffix
	salesName := prefix + "销售-" + e.suffix
	var channelID int64
	if err := e.pool.QueryRow(e.ctx, `
INSERT INTO ops_organizations (name, type, status) VALUES ($1, 'agency', 'active') RETURNING id`, channelName).Scan(&channelID); err != nil {
		e.t.Fatalf("创建渠道失败: %v", err)
	}
	e.channelIDs = append(e.channelIDs, channelID)
	e.seedSales(channelID, salesName)
	return channelName, salesName
}

// activateFormalUnpaid 正式开通一个产品（挂账，便于后续核销）。
func (e *billingTestEnv) activateFormalUnpaid(tenantID int64, productCode string) {
	e.t.Helper()
	endDate := time.Now().AddDate(1, 0, -1).Format("2006-01-02")
	if _, err := e.svc.ActivateFormal(e.ctx, tenantID, billing.FormalActivationRequest{
		ProductCodes: []string{productCode},
		EndDate:      endDate,
		Payment:      billing.ActivationPayment{Mode: "unpaid"},
	}, e.adminID); err != nil {
		e.t.Fatalf("正式开通（挂账）失败: %v", err)
	}
}

// firstOrderID 取机构最早一张订单 id。
func (e *billingTestEnv) firstOrderID(tenantID int64) int64 {
	e.t.Helper()
	var orderID int64
	if err := e.pool.QueryRow(e.ctx, `
SELECT id FROM billing_orders WHERE tenant_id = $1 ORDER BY id ASC LIMIT 1`, tenantID).Scan(&orderID); err != nil {
		e.t.Fatalf("读取订单失败: %v", err)
	}
	return orderID
}

// orderPaymentState 读订单的 已收金额/状态。
func (e *billingTestEnv) orderPaymentState(orderID int64) (string, string) {
	e.t.Helper()
	var paid, status string
	if err := e.pool.QueryRow(e.ctx, `
SELECT paid_amount::text, status FROM billing_orders WHERE id = $1`, orderID).Scan(&paid, &status); err != nil {
		e.t.Fatalf("读取订单回款状态失败: %v", err)
	}
	return money(paid), status
}

func TestBillingPaymentAllocation(t *testing.T) {
	env := newBillingTestEnv(t)
	ctx := env.ctx

	channelName, salesName := env.seedChannelWithSales("收款")
	tenantA := env.createInstitution("收款测试门诊A", "口腔", []string{"口腔科"}, channelName, salesName)
	tenantB := env.createInstitution("收款测试门诊B", "眼科", []string{"眼科"}, channelName, salesName)
	env.activateFormalUnpaid(tenantA.ID, "yingke") // 12600
	env.activateFormalUnpaid(tenantB.ID, "yingke") // 12600

	orderA := env.firstOrderID(tenantA.ID)
	orderB := env.firstOrderID(tenantB.ID)
	today := time.Now().Format("2006-01-02")

	// 一笔收款 20000 同时核销两个租户的订单：A 全收 12600、B 部分收 5000。
	result, err := env.svc.CreatePayment(ctx, billing.CreatePaymentRequest{
		PayerName:  "测试付款方",
		Amount:     "20000",
		ReceivedOn: today,
		Method:     "对公转账",
		Note:       "跨租户核销",
		Allocations: []billing.PaymentAllocationInput{
			{OrderID: orderA, Amount: "12600"},
			{OrderID: orderB, Amount: "5000"},
		},
	}, env.adminID)
	if err != nil {
		t.Fatalf("创建收款单失败: %v", err)
	}
	env.paymentIDs = append(env.paymentIDs, result.Payment.ID)
	if len(result.Allocations) != 2 {
		t.Fatalf("应有 2 条核销明细，实际 %d", len(result.Allocations))
	}
	if money(string(result.Payment.Amount)) != "20000.00" || money(string(result.Payment.Settled)) != "17600.00" {
		t.Fatalf("收款单金额/已核销不对: amount=%s settled=%s", result.Payment.Amount, result.Payment.Settled)
	}
	if money(string(result.UnallocatedBalance)) != "2400.00" {
		t.Fatalf("未核销余额应为 2400，实际 %s", result.UnallocatedBalance)
	}
	if result.Payment.State != "部分核销" || result.Payment.Status != billing.PaymentStatusPartial {
		t.Fatalf("收款单状态不对: state=%s status=%s", result.Payment.State, result.Payment.Status)
	}

	paidA, statusA := env.orderPaymentState(orderA)
	paidB, statusB := env.orderPaymentState(orderB)
	if paidA != "12600.00" || statusA != billing.OrderStatusSettled {
		t.Fatalf("A 订单应收满：paid=%s status=%s", paidA, statusA)
	}
	if paidB != "5000.00" || statusB != billing.OrderStatusPaid {
		t.Fatalf("B 订单应部分收：paid=%s status=%s", paidB, statusB)
	}
	// 两笔核销明细都落到各自的订单
	var allocationRows int
	if err := env.pool.QueryRow(ctx, `
SELECT count(*) FROM billing_payment_allocations WHERE payment_id = $1`, result.Payment.ID).Scan(&allocationRows); err != nil {
		t.Fatalf("统计核销明细失败: %v", err)
	}
	if allocationRows != 2 {
		t.Fatalf("应写 2 条核销明细，实际 %d", allocationRows)
	}

	// 重复核销幂等：同一笔收款、同样金额再提交一次，不重复加已收。
	repeat, err := env.svc.AllocatePayment(ctx, result.Payment.ID, billing.CreatePaymentRequest{
		Allocations: []billing.PaymentAllocationInput{
			{OrderID: orderA, Amount: "12600"},
			{OrderID: orderB, Amount: "5000"},
		},
	}, env.adminID)
	if err != nil {
		t.Fatalf("重复核销应幂等成功: %v", err)
	}
	if len(repeat.Allocations) != 2 {
		t.Fatalf("重复核销后核销明细不应增加，实际 %d", len(repeat.Allocations))
	}
	if paidB, _ := env.orderPaymentState(orderB); paidB != "5000.00" {
		t.Fatalf("重复核销不应重复加已收，实际 %s", paidB)
	}
	if money(string(repeat.Payment.Settled)) != "17600.00" {
		t.Fatalf("重复核销后已核销不应变化，实际 %s", repeat.Payment.Settled)
	}

	// 核销金额不能超过应收：B 还剩 7600，用一张新收款单核销 20000 会被拒绝。
	over, err := env.svc.CreatePayment(ctx, billing.CreatePaymentRequest{
		Amount:     "20000",
		ReceivedOn: today,
		Allocations: []billing.PaymentAllocationInput{
			{OrderID: orderB, Amount: "20000"},
		},
	}, env.adminID)
	if err == nil {
		env.paymentIDs = append(env.paymentIDs, over.Payment.ID)
		t.Fatalf("核销金额超过应收应被拒绝")
	}
	if !strings.Contains(err.Error(), "核销金额不能超过应收") {
		t.Fatalf("超过应收的错误提示不对: %v", err)
	}
	if paidB, _ := env.orderPaymentState(orderB); paidB != "5000.00" {
		t.Fatalf("失败核销不应改订单已收，实际 %s", paidB)
	}

	// 收款单列表：能按财务视角看到金额、已核销、应收合计与状态。
	list, err := env.svc.ListPayments(ctx, env.adminID)
	if err != nil {
		t.Fatalf("查询收款单失败: %v", err)
	}
	var found bool
	for _, item := range list.Items {
		if item.ID != result.Payment.ID {
			continue
		}
		found = true
		if money(string(item.Amount)) != "20000.00" || money(string(item.Settled)) != "17600.00" {
			t.Fatalf("收款单列表金额不对: %+v", item)
		}
		if money(string(item.ReceivableTotal)) != "25200.00" {
			t.Fatalf("应收合计应为 25200（12600+12600），实际 %s", item.ReceivableTotal)
		}
		if item.State != "部分核销" {
			t.Fatalf("收款单展示状态不对: %s", item.State)
		}
	}
	if !found {
		t.Fatalf("收款单列表没有找到本次创建的收款单")
	}
}

// TestBillingPaymentScope 验证非平台运营只能核销自己组织名下租户的订单。
func TestBillingPaymentScope(t *testing.T) {
	env := newBillingTestEnv(t)
	ctx := env.ctx

	channelName, salesName := env.seedChannelWithSales("越权")
	tenant := env.createInstitution("越权测试门诊", "口腔", []string{"口腔科"}, channelName, salesName)
	env.activateFormalUnpaid(tenant.ID, "yingke")
	orderID := env.firstOrderID(tenant.ID)

	// 另一个组织的运营账号（无平台权限）
	var otherOrgID, otherAdminID int64
	if err := env.pool.QueryRow(ctx, `
INSERT INTO ops_organizations (name, type, status) VALUES ($1, 'agency', 'active') RETURNING id`,
		"其他渠道-"+env.suffix).Scan(&otherOrgID); err != nil {
		t.Fatalf("创建其他组织失败: %v", err)
	}
	env.channelIDs = append(env.channelIDs, otherOrgID)
	if err := env.pool.QueryRow(ctx, `
INSERT INTO operations_admins (name, phone, username, password_hash, org_id, is_active, session_version, created_at, updated_at)
VALUES ($1, $2::varchar, $2::text, 'x', $3, 1, 1, NOW(), NOW())
RETURNING id`, "其他运营-"+env.suffix, env.nextPhone("17"), otherOrgID).Scan(&otherAdminID); err != nil {
		t.Fatalf("创建其他运营失败: %v", err)
	}
	env.salesIDs = append(env.salesIDs, otherAdminID)

	_, err := env.svc.CreatePayment(ctx, billing.CreatePaymentRequest{
		Amount:      "100",
		ReceivedOn:  time.Now().Format("2006-01-02"),
		Allocations: []billing.PaymentAllocationInput{{OrderID: orderID, Amount: "100"}},
	}, otherAdminID)
	if err == nil {
		t.Fatalf("越权核销应被拒绝")
	}
	if !strings.Contains(err.Error(), "permission") {
		t.Fatalf("越权错误提示不对: %v", err)
	}
}
