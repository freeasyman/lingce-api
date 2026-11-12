package billing_test

// 第 3 批集成测试（真实 PostgreSQL）：
//   · 开票额度：累计不超过订单应收、已作废订单不能开票、作废发票释放额度；
//   · 发票号唯一、标记已开幂等、开票记录按机构过滤；
//   · 应收/收款/开票三张 CSV 导出的内容；
//   · 改价新版本从生效日生效、未来版本不提前生效、写 system_action_logs；
//   · 调额度幂等键不重复加减；机构变更记录能读到 system_action_logs。
//
// 与既有用例一致：未设置 BILLING_TEST_DATABASE_URL / TEST_DATABASE_URL 时 Skip；
// 每个用例自建租户并在结束后清理。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
)

// cleanupBatch3Logs 删除本用例租户的审计日志（logs 没有外键，不随租户删除）。
func cleanupBatch3Logs(t *testing.T, env *billingTestEnv) {
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

// createVoidOrder 直接插一张已作废订单（激活链路不会产生可作废状态，这里只验证开票闸门）。
func createVoidOrder(t *testing.T, env *billingTestEnv, tenantID int64) int64 {
	t.Helper()
	var orderID int64
	if err := env.pool.QueryRow(env.ctx, `
INSERT INTO billing_orders (order_no, tenant_id, order_type, items, total_amount, payable_amount, status, reason)
VALUES ($1, $2, 'single', '[]'::jsonb, 100, 100, 'void', '测试已作废订单')
RETURNING id`, fmt.Sprintf("B3VOID-%s-%d", env.suffix, time.Now().UnixNano()%100000), tenantID).Scan(&orderID); err != nil {
		t.Fatalf("插入已作废订单失败: %v", err)
	}
	return orderID
}

// invoiceRequest 生成一张关联订单的开票请求。
func invoiceRequest(tenantID, orderID int64, amount, invoiceNo string) billing.CreateInvoiceRequest {
	return billing.CreateInvoiceRequest{
		TenantID:    tenantID,
		OrderID:     orderID,
		Amount:      billing.Decimal(amount),
		InvoiceType: billing.InvoiceTypeSpecial,
		Title:       "第3批测试门诊部有限公司",
		TaxNo:       "91310000BATCH3TEST",
		InvoiceNo:   invoiceNo,
		Note:        "集成测试",
	}
}

func TestBatch3InvoiceRulesAndListing(t *testing.T) {
	env := newBillingTestEnv(t)
	cleanupBatch3Logs(t, env)

	channelName, salesName := env.seedChannelWithSales("开票")
	institution := env.createInstitution("开票测试门诊", "口腔", []string{"口腔科"}, channelName, salesName)
	env.activateFormalUnpaid(institution.ID, "yingke")
	orderID := env.firstOrderID(institution.ID)
	today := time.Now().Format("2006-01-02")

	// 1. 超过订单应收：拒绝。
	over, err := env.svc.CreateInvoice(env.ctx, invoiceRequest(institution.ID, orderID, "20000", ""), env.adminID)
	if err == nil {
		t.Fatalf("超过订单应收应被拒绝，实际创建了 %+v", over)
	}
	if !strings.Contains(err.Error(), "应收") || !strings.Contains(err.Error(), "剩余可开票") {
		t.Fatalf("超应收错误提示不对: %v", err)
	}

	// 2. 第一张 10000，第二张 5000 会超过 12600，改为 2600 正好开满。
	first, err := env.svc.CreateInvoice(env.ctx, invoiceRequest(institution.ID, orderID, "10000", ""), env.adminID)
	if err != nil {
		t.Fatalf("创建第一张发票失败: %v", err)
	}
	if first.Status != billing.InvoiceStatusApplied || first.InvoiceNo != "" {
		t.Fatalf("新建发票状态应为 applied 且无号: %+v", first)
	}
	if _, err := env.svc.CreateInvoice(env.ctx, invoiceRequest(institution.ID, orderID, "5000", ""), env.adminID); err == nil {
		t.Fatal("累计开票超过应收应被拒绝")
	}
	second, err := env.svc.CreateInvoice(env.ctx, invoiceRequest(institution.ID, orderID, "2600", ""), env.adminID)
	if err != nil {
		t.Fatalf("创建第二张发票失败: %v", err)
	}
	if _, err := env.svc.CreateInvoice(env.ctx, invoiceRequest(institution.ID, orderID, "1", ""), env.adminID); err == nil {
		t.Fatal("订单开满后继续开票应被拒绝")
	}

	// 3. 已作废订单不能开票。
	voidOrderID := createVoidOrder(t, env, institution.ID)
	if _, err := env.svc.CreateInvoice(env.ctx, invoiceRequest(institution.ID, voidOrderID, "100", ""), env.adminID); err == nil {
		t.Fatal("已作废订单不能开票")
	} else if !strings.Contains(err.Error(), "已作废") {
		t.Fatalf("作废订单错误提示不对: %v", err)
	}

	// 4. 标记已开：写号 + 日期；重复提交幂等；同号冲突；改号冲突。
	invoiceNo := "INV-B3-" + env.suffix + "-1"
	issued, err := env.svc.IssueInvoice(env.ctx, first.ID, billing.IssueInvoiceRequest{
		InvoiceNo: invoiceNo,
		IssuedOn:  today,
	}, env.adminID)
	if err != nil {
		t.Fatalf("标记已开失败: %v", err)
	}
	if issued.Status != billing.InvoiceStatusIssued || issued.InvoiceNo != invoiceNo || issued.IssuedOn != today {
		t.Fatalf("已开发票字段不对: %+v", issued)
	}
	repeat, err := env.svc.IssueInvoice(env.ctx, first.ID, billing.IssueInvoiceRequest{
		InvoiceNo: invoiceNo,
		IssuedOn:  today,
	}, env.adminID)
	if err != nil || repeat.Status != billing.InvoiceStatusIssued || repeat.InvoiceNo != invoiceNo {
		t.Fatalf("重复标记已开应幂等: %+v err=%v", repeat, err)
	}
	if _, err := env.svc.IssueInvoice(env.ctx, first.ID, billing.IssueInvoiceRequest{
		InvoiceNo: invoiceNo + "-X",
		IssuedOn:  today,
	}, env.adminID); err == nil {
		t.Fatal("已开发票改号应被拒绝")
	}

	// 5. 发票号唯一：第二张用同一个号标记已开 -> 409。
	if _, err := env.svc.IssueInvoice(env.ctx, second.ID, billing.IssueInvoiceRequest{
		InvoiceNo: invoiceNo,
		IssuedOn:  today,
	}, env.adminID); err == nil {
		t.Fatal("重复发票号应被拒绝")
	} else if !errors.Is(err, billing.ErrInvoiceNoExists) {
		t.Fatalf("重复发票号错误应为 ErrInvoiceNoExists，实际 %v", err)
	}

	// 6. 作废第二张后释放额度：可以再开 2600。
	voided, err := env.svc.VoidInvoice(env.ctx, second.ID, billing.VoidInvoiceRequest{Note: "抬头开错"}, env.adminID)
	if err != nil {
		t.Fatalf("作废发票失败: %v", err)
	}
	if voided.Status != billing.InvoiceStatusVoided {
		t.Fatalf("作废后状态不对: %+v", voided)
	}
	if again, err := env.svc.VoidInvoice(env.ctx, second.ID, billing.VoidInvoiceRequest{}, env.adminID); err != nil || again.Status != billing.InvoiceStatusVoided {
		t.Fatalf("重复作废应幂等: %+v err=%v", again, err)
	}
	replacement, err := env.svc.CreateInvoice(env.ctx, invoiceRequest(institution.ID, orderID, "2600", ""), env.adminID)
	if err != nil {
		t.Fatalf("作废释放额度后应能重开: %v", err)
	}
	// 新发票再占满额度，验证 voided 不计入累计。
	if _, err := env.svc.CreateInvoice(env.ctx, invoiceRequest(institution.ID, orderID, "1", ""), env.adminID); err == nil {
		t.Fatal("重开后订单再次开满，继续开票应被拒绝")
	}

	// 7. 手填金额（不关联订单）也可开票。
	manual, err := env.svc.CreateInvoice(env.ctx, billing.CreateInvoiceRequest{
		TenantID: institution.ID, Amount: "88.50", Title: "第3批测试门诊部有限公司",
	}, env.adminID)
	if err != nil {
		t.Fatalf("手填金额开票失败: %v", err)
	}
	if manual.OrderID != nil || manual.Amount.String() != "88.50" {
		t.Fatalf("手填金额发票字段不对: %+v", manual)
	}

	// 8. 列表按机构过滤，包含全部记录（含作废）。
	list, err := env.svc.ListInvoices(env.ctx, billing.InvoiceFilter{TenantID: institution.ID}, env.adminID)
	if err != nil {
		t.Fatalf("查询开票记录失败: %v", err)
	}
	if list.Total != 4 || len(list.Items) != 4 {
		t.Fatalf("应查到 4 张发票（first/second/replacement/manual），实际 total=%d items=%d", list.Total, len(list.Items))
	}
	issuedList, err := env.svc.ListInvoices(env.ctx, billing.InvoiceFilter{TenantID: institution.ID, Status: billing.InvoiceStatusIssued}, env.adminID)
	if err != nil || issuedList.Total != 1 {
		t.Fatalf("按已开票过滤失败: total=%d err=%v", issuedList.Total, err)
	}

	// 9. 可开票订单：当前订单已开满，不应再出现。
	orders, err := env.svc.ListInvoiceableOrders(env.ctx, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("查询可开票订单失败: %v", err)
	}
	for _, order := range orders {
		if order.ID == orderID {
			t.Fatalf("已开满订单不应出现在可开票列表: %+v", order)
		}
	}

	// 10. 变更记录读到开票创建/已开/作废三类日志。
	logs, err := env.svc.ListActionLogs(env.ctx, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("查询变更记录失败: %v", err)
	}
	seen := map[string]bool{}
	for _, log := range logs {
		seen[log.ActionCode] = true
	}
	for _, want := range []string{"billing.invoice.create", "billing.invoice.issue", "billing.invoice.void"} {
		if !seen[want] {
			t.Fatalf("变更记录缺少 %s：%+v", want, logs)
		}
	}

	_ = replacement
}

func TestBatch3FinanceExportContent(t *testing.T) {
	env := newBillingTestEnv(t)
	cleanupBatch3Logs(t, env)

	channelName, salesName := env.seedChannelWithSales("导出")
	institution := env.createInstitution("导出测试门诊", "眼科", []string{"眼科"}, channelName, salesName)
	env.activateFormalUnpaid(institution.ID, "yingke")
	orderID := env.firstOrderID(institution.ID)
	today := time.Now().Format("2006-01-02")

	invoice, err := env.svc.CreateInvoice(env.ctx, invoiceRequest(institution.ID, orderID, "12600", ""), env.adminID)
	if err != nil {
		t.Fatalf("创建导出发票失败: %v", err)
	}
	if _, err := env.svc.IssueInvoice(env.ctx, invoice.ID, billing.IssueInvoiceRequest{IssuedOn: today}, env.adminID); err != nil {
		t.Fatalf("标记导出发票已开失败: %v", err)
	}
	payment, err := env.svc.CreatePayment(env.ctx, billing.CreatePaymentRequest{
		PayerName:  "导出测试付款方",
		Amount:     "12600",
		ReceivedOn: today,
		Method:     "对公转账",
		VoucherNo:  "V-B3-" + env.suffix,
		Allocations: []billing.PaymentAllocationInput{
			{OrderID: orderID, Amount: "12600"},
		},
	}, env.adminID)
	if err != nil {
		t.Fatalf("创建导出收款单失败: %v", err)
	}
	env.paymentIDs = append(env.paymentIDs, payment.Payment.ID)

	yesterday := time.Now().AddDate(0, 0, -1)
	tomorrow := time.Now().AddDate(0, 0, 1)

	receivables, err := env.svc.ExportFinance(env.ctx, billing.FinanceExportReceivables, &yesterday, &tomorrow, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("导出应收失败: %v", err)
	}
	if len(receivables.Header) != 9 || !strings.HasSuffix(receivables.Filename, ".csv") {
		t.Fatalf("应收导出表头/文件名不对: header=%v filename=%s", receivables.Header, receivables.Filename)
	}
	if len(receivables.Rows) != 1 {
		t.Fatalf("应收应导出 1 行，实际 %d: %v", len(receivables.Rows), receivables.Rows)
	}
	if receivables.Rows[0][2] != "导出测试门诊" || receivables.Rows[0][7] != "已结清" {
		t.Fatalf("应收导出行内容不对: %v", receivables.Rows[0])
	}

	payments, err := env.svc.ExportFinance(env.ctx, billing.FinanceExportPayments, &yesterday, &tomorrow, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("导出收款失败: %v", err)
	}
	if len(payments.Rows) != 1 {
		t.Fatalf("收款应导出 1 行，实际 %d: %v", len(payments.Rows), payments.Rows)
	}
	if payments.Rows[0][0] != payment.Payment.No || payments.Rows[0][8] != "已核销" {
		t.Fatalf("收款导出行内容不对: %v", payments.Rows[0])
	}

	invoices, err := env.svc.ExportFinance(env.ctx, billing.FinanceExportInvoices, &yesterday, &tomorrow, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("导出发票失败: %v", err)
	}
	if len(invoices.Rows) != 1 {
		t.Fatalf("开票应导出 1 行，实际 %d: %v", len(invoices.Rows), invoices.Rows)
	}
	if invoices.Rows[0][0] == "" {
		t.Fatalf("开票导出发票号为空: %v", invoices.Rows[0])
	}
	if invoices.Rows[0][5] != billing.InvoiceTypeSpecial || invoices.Rows[0][8] != "已开票" {
		t.Fatalf("开票导出行内容不对: %v", invoices.Rows[0])
	}

	// 机构档案的收款记录会根据关联订单的发票推导开票状态（不再是写死的「未开票」）。
	institutionAfter, err := env.svc.GetInstitution(env.ctx, institution.ID)
	if err != nil {
		t.Fatalf("读取机构档案失败: %v", err)
	}
	if len(institutionAfter.PayLogs) != 1 || institutionAfter.PayLogs[0].Invoice != "已开票" {
		t.Fatalf("收款记录开票状态不对: %+v", institutionAfter.PayLogs)
	}

	// 未指定日期也能导出，且 tenant 过滤生效。
	all, err := env.svc.ExportFinance(env.ctx, billing.FinanceExportInvoices, nil, nil, institution.ID, env.adminID)
	if err != nil || len(all.Rows) != 1 {
		t.Fatalf("不限日期导出发票失败: rows=%d err=%v", len(all.Rows), err)
	}
	// 非法 kind 拒绝。
	if _, err := env.svc.ExportFinance(env.ctx, "unknown", nil, nil, 0, env.adminID); err == nil {
		t.Fatal("非法 kind 应被拒绝")
	}
}

func TestBatch3CatalogVersionEffectiveAndLog(t *testing.T) {
	env := newBillingTestEnv(t)
	ctx := env.ctx
	const productCode = "b3test"

	// 本用例独占一个测试产品，避免动 yingke 等真实价目。
	cleanup := func() {
		_, _ = env.pool.Exec(ctx, `DELETE FROM product_catalog WHERE product_code = $1`, productCode)
		_, _ = env.pool.Exec(ctx, `DELETE FROM system_action_logs WHERE object_id = $1`, productCode)
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := env.pool.Exec(ctx, `
INSERT INTO product_catalog (product_code, product_name, edition, price_version, annual_price, quotas, increment_price, increments)
VALUES ($1, '批次三测试产品', 'standard', '2026', 1000, '{"hour":10}'::jsonb, 100, '{"hour":5}'::jsonb)`, productCode); err != nil {
		t.Fatalf("插入测试产品失败: %v", err)
	}
	// 一张既有订单：改价后金额不变。
	channelName, salesName := env.seedChannelWithSales("改价")
	institution := env.createInstitution("改价测试门诊", "口腔", []string{"口腔科"}, channelName, salesName)
	env.activateFormalUnpaid(institution.ID, "yingke")
	orderID := env.firstOrderID(institution.ID)
	var orderPayableBefore string
	if err := env.pool.QueryRow(ctx, `SELECT payable_amount::text FROM billing_orders WHERE id = $1`, orderID).Scan(&orderPayableBefore); err != nil {
		t.Fatalf("读取订单应收失败: %v", err)
	}

	today := time.Now().Format("2006-01-02")
	item, err := env.svc.CreateCatalogVersionWithApproval(ctx, billing.CreateCatalogVersionV2Request{
		ProductCode:    productCode,
		Unit:           "hour",
		AnnualPrice:    "1200",
		IncrementPrice: "150",
		IncrementSize:  "7",
		EffectiveFrom:  today,
		Approver:       "王院长",
		Reason:         "年度成本调整",
	}, env.adminID)
	if err != nil {
		t.Fatalf("新增价目版本失败: %v", err)
	}
	if item.EffectiveFrom != today || item.AnnualPrice.String() != "1200.00" || item.Approver != "王院长" {
		t.Fatalf("新版本字段不对: %+v", item)
	}
	if !strings.Contains(item.Quotas.String(), `"hour"`) || !strings.Contains(item.Quotas.String(), "10") {
		t.Fatalf("新版本应沿用原额度，实际 %s", item.Quotas.String())
	}
	if !strings.Contains(item.Increments.String(), `"hour"`) || !strings.Contains(item.Increments.String(), "7") {
		t.Fatalf("新版本增量包规格不对: %s", item.Increments.String())
	}

	// 当前生效版本已切到新版本。
	service := billing.NewService(billing.NewStore(env.pool))
	effectiveItems, err := service.ListCatalog(ctx, billing.CatalogFilter{ProductCode: productCode})
	if err != nil {
		t.Fatalf("查询生效价目失败: %v", err)
	}
	if len(effectiveItems) != 1 || effectiveItems[0].AnnualPrice.String() != "1200.00" {
		t.Fatalf("新版本应立即生效: %+v", effectiveItems)
	}

	// 未来版本提前录入，不提前生效。
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if _, err := env.svc.CreateCatalogVersionWithApproval(ctx, billing.CreateCatalogVersionV2Request{
		ProductCode:    productCode,
		Unit:           "hour",
		AnnualPrice:    "1400",
		IncrementPrice: "170",
		IncrementSize:  "8",
		EffectiveFrom:  tomorrow,
		Approver:       "王院长",
		Reason:         "下月调价",
	}, env.adminID); err != nil {
		t.Fatalf("录入未来价目版本失败: %v", err)
	}
	effectiveItems, err = service.ListCatalog(ctx, billing.CatalogFilter{ProductCode: productCode})
	if err != nil {
		t.Fatalf("查询生效价目失败: %v", err)
	}
	if len(effectiveItems) != 1 || effectiveItems[0].AnnualPrice.String() != "1200.00" {
		t.Fatalf("未来版本不应提前生效: %+v", effectiveItems)
	}

	// 老订单金额不受影响。
	var orderPayableAfter string
	if err := env.pool.QueryRow(ctx, `SELECT payable_amount::text FROM billing_orders WHERE id = $1`, orderID).Scan(&orderPayableAfter); err != nil {
		t.Fatalf("读取改价后的订单应收失败: %v", err)
	}
	if orderPayableAfter != orderPayableBefore {
		t.Fatalf("改价不应回写历史订单：before=%s after=%s", orderPayableBefore, orderPayableAfter)
	}

	// 两次改价都写 system_action_logs（平台级 tenant_id 为空）。
	var logCount int
	if err := env.pool.QueryRow(ctx, `
SELECT count(*) FROM system_action_logs WHERE action_code = 'billing.catalog.version' AND object_id = $1`, productCode).Scan(&logCount); err != nil {
		t.Fatalf("读取改价日志失败: %v", err)
	}
	if logCount != 2 {
		t.Fatalf("改价应写 2 条 system_action_logs，实际 %d", logCount)
	}

	// 审批人/原因缺失在 service 层直接拒绝，不落库。
	if _, err := env.svc.CreateCatalogVersionWithApproval(ctx, billing.CreateCatalogVersionV2Request{
		ProductCode: productCode, Unit: "hour", AnnualPrice: "1500",
		IncrementPrice: "180", IncrementSize: "9", EffectiveFrom: today,
	}, env.adminID); err == nil {
		t.Fatal("缺少审批人/原因应被拒绝")
	}
}

func TestBatch3CorrectionIdempotencyAndChangeLog(t *testing.T) {
	env := newBillingTestEnv(t)
	cleanupBatch3Logs(t, env)
	institution := activateLifecycleInstitution(t, env)
	tenantID := institution.ID

	_, _, balanceBefore, _ := entitlementSnapshot(t, env, tenantID, "yingke", "hour")
	key := "b3-correction-" + env.suffix
	request := billing.EntitlementCorrectionRequest{
		ProductCode:    "yingke",
		Unit:           "hour",
		Amount:         "5",
		Reason:         "第3批幂等测试",
		IdempotencyKey: key,
	}
	if _, err := env.svc.CorrectEntitlement(env.ctx, tenantID, request, env.adminID); err != nil {
		t.Fatalf("第一次调额度失败: %v", err)
	}
	if _, err := env.svc.CorrectEntitlement(env.ctx, tenantID, request, env.adminID); err != nil {
		t.Fatalf("重复调额度（同幂等键）应成功: %v", err)
	}
	_, _, balanceAfter, _ := entitlementSnapshot(t, env, tenantID, "yingke", "hour")
	if balanceAfter != addMoney(balanceBefore, "5") {
		t.Fatalf("重复提交不应重复加额度：before=%s after=%s", balanceBefore, balanceAfter)
	}
	var ledgerCount int
	if err := env.pool.QueryRow(env.ctx, `
SELECT count(*) FROM entitlement_ledger WHERE tenant_id = $1 AND idempotency_key = $2`, tenantID, key).Scan(&ledgerCount); err != nil {
		t.Fatalf("读取幂等流水失败: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("同幂等键应只写 1 条流水，实际 %d", ledgerCount)
	}
	var logCount int
	if err := env.pool.QueryRow(env.ctx, `
SELECT count(*) FROM system_action_logs WHERE tenant_id = $1 AND action_code = 'institution.entitlement_correction'`, tenantID).Scan(&logCount); err != nil {
		t.Fatalf("读取调额度日志失败: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("重复提交不应重复写日志，实际 %d 条", logCount)
	}

	logs, err := env.svc.ListActionLogs(env.ctx, tenantID, env.adminID)
	if err != nil {
		t.Fatalf("查询变更记录失败: %v", err)
	}
	found := false
	for _, log := range logs {
		if log.ActionCode == "institution.entitlement_correction" {
			found = true
			if log.Detail != "第3批幂等测试" {
				t.Fatalf("变更记录详情应为调整原因，实际 %q", log.Detail)
			}
		}
	}
	if !found {
		t.Fatalf("变更记录缺少调额度记录: %+v", logs)
	}
}

// addMoney 把两个金额字符串相加（测试断言用，金额最多两位小数）。
func addMoney(left, right string) string {
	parse := func(value string) int64 {
		parts := strings.SplitN(value, ".", 2)
		whole := int64(0)
		for _, char := range parts[0] {
			whole = whole*10 + int64(char-'0')
		}
		fraction := int64(0)
		if len(parts) == 2 {
			text := parts[1]
			for len(text) < 2 {
				text += "0"
			}
			for _, char := range text[:2] {
				fraction = fraction*10 + int64(char-'0')
			}
		}
		return whole*100 + fraction
	}
	total := parse(left) + parse(right)
	return fmt.Sprintf("%d.%02d", total/100, total%100)
}
