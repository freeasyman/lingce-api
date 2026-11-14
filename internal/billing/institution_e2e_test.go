package billing_test

// 运营端计费「第 1 阶段」集成测试（真实 PostgreSQL）。
//
// 运行前置：本地测试库已跑完迁移（含 20261112_003~008）：
//
//	BILLING_TEST_DATABASE_URL='postgresql://yiliiang@127.0.0.1:5432/lingce_billing_test?sslmode=disable' \
//	  go test ./internal/billing/ -run 'TestInstitution|TestBillingPayment|TestBillingDictionary' -count=1 -v
//
// 与 e2e_smoke_test.go 的约定一致：未设置 BILLING_TEST_DATABASE_URL / TEST_DATABASE_URL 时直接 Skip。
// 用例自建独立运营组织/渠道/销售与租户（时间戳后缀），结束按租户逐表清理，不碰其它业务数据。

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/freeasyman/lingce-api/internal/tenant"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------- 测试环境 ----------------

type billingTestEnv struct {
	t        *testing.T
	ctx      context.Context
	pool     *pgxpool.Pool
	svc      *billing.InstitutionService
	adminID  int64
	orgID    int64
	suffix   string
	phoneSeq int64

	tenantIDs  []int64
	paymentIDs []int64
	salesIDs   []int64
	channelIDs []int64
}

func newBillingTestEnv(t *testing.T) *billingTestEnv {
	t.Helper()
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

	assertStageOneSchema(t, ctx, pool)

	store := billing.NewStore(pool)
	svc := billing.NewInstitutionService(billing.NewService(store), store, tenant.NewStore(pool))
	env := &billingTestEnv{
		t:      t,
		ctx:    ctx,
		pool:   pool,
		svc:    svc,
		suffix: strconv.FormatInt(time.Now().UnixNano()%1_000_000_000, 10),
	}
	t.Cleanup(env.cleanup)
	env.adminID, env.orgID = env.seedOpsContext()
	return env
}

// assertStageOneSchema 确认 5 张新表 + 档案列 + 字典种子已就绪。
func assertStageOneSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var tables int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM information_schema.tables
WHERE table_schema = 'public'
  AND table_name IN ('tenant_ownerships','subscription_periods','billing_payments',
                     'billing_payment_allocations','billing_industry_dict',
                     'billing_department_dict','tenant_departments','billing_bundles')`).Scan(&tables); err != nil {
		t.Fatalf("检查新增表失败: %v", err)
	}
	if tables != 8 {
		t.Fatalf("第 1 阶段新增表未全部建好（%d/8），请先跑 20261112_003~008 迁移", tables)
	}
	var industries, departments, bundles int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM billing_industry_dict`).Scan(&industries); err != nil {
		t.Fatalf("检查行业字典失败: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM billing_department_dict`).Scan(&departments); err != nil {
		t.Fatalf("检查科室字典失败: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM billing_bundles`).Scan(&bundles); err != nil {
		t.Fatalf("检查套餐失败: %v", err)
	}
	if industries < 17 || departments < 19 || bundles < 4 {
		t.Fatalf("字典/套餐种子不足：行业 %d（期望 ≥17）、科室 %d（期望 ≥19）、套餐 %d（期望 ≥4）",
			industries, departments, bundles)
	}
	// 管理员管理依赖 20261112_011 的 employees.is_primary。
	var hasPrimary bool
	if err := pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public' AND table_name = 'employees' AND column_name = 'is_primary'
)`).Scan(&hasPrimary); err != nil {
		t.Fatalf("检查 employees.is_primary 失败: %v", err)
	}
	if !hasPrimary {
		t.Fatalf("employees.is_primary 未建列，请先执行 20261112_011_institution_admin_primary 迁移")
	}
}

// seedOpsContext 建一个平台运营组织 + 平台运营账号（可看全部机构）。
func (e *billingTestEnv) seedOpsContext() (int64, int64) {
	e.t.Helper()
	ctx := e.ctx
	var orgID int64
	if err := e.pool.QueryRow(ctx, `
INSERT INTO ops_organizations (name, type, status)
VALUES ($1, 'platform', 'active')
RETURNING id`, "平台运营-"+e.suffix).Scan(&orgID); err != nil {
		e.t.Fatalf("创建运营组织失败: %v", err)
	}
	var adminID int64
	phone := "19" + fmt.Sprintf("%09d", time.Now().UnixNano()%1_000_000_000)
	if err := e.pool.QueryRow(ctx, `
INSERT INTO operations_admins (name, phone, username, password_hash, org_id, is_active, session_version, created_at, updated_at)
VALUES ($1, $2::varchar, $2::text, 'x', $3, 1, 1, NOW(), NOW())
RETURNING id`, "测试运营-"+e.suffix, phone, orgID).Scan(&adminID); err != nil {
		e.t.Fatalf("创建运营账号失败: %v", err)
	}
	return adminID, orgID
}

// nextPhone 生成一个全局唯一的 11 位手机号（employees.phone 有全局唯一索引）。
func (e *billingTestEnv) nextPhone(prefix string) string {
	e.phoneSeq++
	return prefix + fmt.Sprintf("%09d", (time.Now().UnixNano()/1000+e.phoneSeq)%1_000_000_000)
}

// seedSales 在指定渠道下建一个销售账号。
func (e *billingTestEnv) seedSales(orgID int64, name string) int64 {
	e.t.Helper()
	phone := e.nextPhone("18")
	var id int64
	if err := e.pool.QueryRow(e.ctx, `
INSERT INTO operations_admins (name, phone, username, password_hash, org_id, is_active, session_version, created_at, updated_at)
VALUES ($1, $2::varchar, $2::text, 'x', $3, 1, 1, NOW(), NOW())
RETURNING id`, name, phone, orgID).Scan(&id); err != nil {
		e.t.Fatalf("创建销售失败: %v", err)
	}
	e.salesIDs = append(e.salesIDs, id)
	return id
}

// createInstitution 走真实建档链路，并登记清理。
func (e *billingTestEnv) createInstitution(name, industry string, departments []string, channel, sales string) *billing.Institution {
	e.t.Helper()
	item, err := e.svc.CreateInstitution(e.ctx, billing.CreateInstitutionRequest{
		Name:        name,
		Legal:       name + "有限公司",
		Industry:    industry,
		Region:      billing.InstitutionRegion{Province: "310000", City: "310100", District: "310104"},
		Contact:     "联系人",
		Title:       "信息科",
		Phone:       e.nextPhone("13"),
		Email:       "test@example.com",
		Channel:     channel,
		Sales:       sales,
	}, e.adminID)
	if err != nil {
		e.t.Fatalf("建档失败: %v", err)
	}
	e.tenantIDs = append(e.tenantIDs, item.ID)
	return item
}

// cleanup 清理本用例创建的全部数据（只删自己建的，按外键依赖顺序）。
func (e *billingTestEnv) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec := func(statement string, args ...any) {
		if _, err := e.pool.Exec(ctx, statement, args...); err != nil {
			e.t.Logf("清理测试数据失败（%s）: %v", statement, err)
		}
	}
	if len(e.paymentIDs) > 0 {
		exec(`DELETE FROM billing_payments WHERE id = ANY($1)`, e.paymentIDs)
	}
	for _, tenantID := range e.tenantIDs {
		exec(`DELETE FROM billing_payment_allocations WHERE order_id IN (SELECT id FROM billing_orders WHERE tenant_id = $1)`, tenantID)
		exec(`DELETE FROM entitlement_ledger WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM product_entitlements WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM tenant_documents WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM billing_invoices WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM billing_orders WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM subscription_periods WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM tenant_departments WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM tenant_ownerships WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM tenant_subscriptions WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM emr_permission_assignments WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM institution_employee_roles WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM institution_role_menus WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM institution_department_roles WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM employees WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM departments WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM institution_roles WHERE tenant_id = $1`, tenantID)
		exec(`DELETE FROM tenants WHERE id = $1`, tenantID)
	}
	// 收款单没有 tenant_id：按「本用例运营账号录入 + 已无核销明细」清理（核销明细随租户/订单已删）。
	if e.adminID > 0 {
		exec(`DELETE FROM billing_payments
		      WHERE created_by = $1
		        AND NOT EXISTS (SELECT 1 FROM billing_payment_allocations a WHERE a.payment_id = billing_payments.id)`, e.adminID)
	}
	for _, salesID := range e.salesIDs {
		exec(`DELETE FROM tenant_ownerships WHERE sales_admin_id = $1`, salesID)
		exec(`DELETE FROM operations_admins WHERE id = $1`, salesID)
	}
	for _, channelID := range e.channelIDs {
		exec(`DELETE FROM tenant_ownerships WHERE org_id = $1`, channelID)
		exec(`DELETE FROM ops_organizations WHERE id = $1`, channelID)
	}
	if e.adminID > 0 {
		exec(`DELETE FROM operations_admins WHERE id = $1`, e.adminID)
	}
	if e.orgID > 0 {
		exec(`DELETE FROM ops_organizations WHERE id = $1`, e.orgID)
	}
}

// ---------------- 机构建档 + 开通 + 加量/赠送/加产品/续期/收款 ----------------

func TestInstitutionLifecycle(t *testing.T) {
	env := newBillingTestEnv(t)
	ctx := env.ctx

	channelName := "测试渠道-" + env.suffix
	salesName := "测试销售-" + env.suffix
	var channelID int64
	if err := env.pool.QueryRow(ctx, `
INSERT INTO ops_organizations (name, type, status) VALUES ($1, 'agency', 'active') RETURNING id`, channelName).Scan(&channelID); err != nil {
		t.Fatalf("创建渠道失败: %v", err)
	}
	env.channelIDs = append(env.channelIDs, channelID)
	env.seedSales(channelID, salesName)

	// 1. 建档
	institution := env.createInstitution("测试口腔门诊", "口腔", []string{"口腔科", "咨询客服"}, channelName, salesName)
	if institution.Open {
		t.Fatalf("新建机构不应是已开通: %+v", institution)
	}
	if institution.Mode != "formal" {
		t.Fatalf("新建机构 mode 应为 formal，实际 %s", institution.Mode)
	}
	if !strings.HasPrefix(institution.Code, "LC-") {
		t.Fatalf("机构代码格式不对: %s", institution.Code)
	}
	if institution.Legal != "测试口腔门诊有限公司" {
		t.Fatalf("机构全称未落库: %q", institution.Legal)
	}
	if strings.Join(institution.Departments, ",") != "口腔科,咨询客服" {
		t.Fatalf("涉及科室不对: %v", institution.Departments)
	}
	if institution.Channel != channelName || institution.Sales != salesName {
		t.Fatalf("归属不对: channel=%q sales=%q", institution.Channel, institution.Sales)
	}
	if len(institution.Admins) != 1 || institution.Admins[0].Role != "主管理员" {
		t.Fatalf("应建 1 个主管理员，实际 %+v", institution.Admins)
	}

	// 档案列与归属落库
	var legal string
	var orgID, salesID int64
	if err := env.pool.QueryRow(ctx, `SELECT COALESCE(legal_name,'') FROM tenants WHERE id = $1`, institution.ID).Scan(&legal); err != nil {
		t.Fatalf("读取 legal_name 失败: %v", err)
	}
	if legal != "测试口腔门诊有限公司" {
		t.Fatalf("legal_name 未落库: %q", legal)
	}
	if err := env.pool.QueryRow(ctx, `
SELECT COALESCE(org_id,0), COALESCE(sales_admin_id,0)
FROM tenant_ownerships WHERE tenant_id = $1 AND effective_to IS NULL`, institution.ID).Scan(&orgID, &salesID); err != nil {
		t.Fatalf("读取归属失败: %v", err)
	}
	if orgID != channelID || salesID == 0 {
		t.Fatalf("归属落库不对: org=%d sales=%d", orgID, salesID)
	}

	// 2. 手机号撞号 -> 409（conflict）
	dupPayload := billing.CreateInstitutionRequest{
		Name: "撞号机构", Legal: "撞号机构有限公司", Industry: "口腔",
		Region:  billing.InstitutionRegion{Province: "310000", City: "310100", District: "310104"},
		Contact: "联系人", Phone: institution.Phone, Channel: channelName, Sales: salesName,
	}
	if _, err := env.svc.CreateInstitution(ctx, dupPayload, env.adminID); err == nil {
		t.Fatalf("重复手机号应返回冲突")
	} else if !strings.Contains(err.Error(), "该手机号已被") {
		t.Fatalf("重复手机号错误提示不对: %v", err)
	}

	// 3. 正式开通：两个产品 + 部分收款
	today := time.Now().Format("2006-01-02")
	todayDate, _ := time.Parse("2006-01-02", today)
	endDate := todayDate.AddDate(0, 0, 365).Format("2006-01-02")
	activated, err := env.svc.ActivateFormal(ctx, institution.ID, billing.FormalActivationRequest{
		ProductCodes: []string{"yingke", "kehu"},
		EndDate:      endDate,
		Payment:      billing.ActivationPayment{Mode: "paid", Amount: "5000", Date: today, Method: "对公转账"},
	}, env.adminID)
	if err != nil {
		t.Fatalf("正式开通失败: %v", err)
	}
	if !activated.Open || activated.Mode != "formal" {
		t.Fatalf("开通后状态不对: open=%v mode=%s", activated.Open, activated.Mode)
	}
	if activated.Start != today || activated.End != endDate {
		t.Fatalf("服务期不对: start=%s end=%s（期望 %s ~ %s）", activated.Start, activated.End, today, endDate)
	}
	if _, ok := activated.Products["yingke"]["hour"]; !ok {
		t.Fatalf("开通后应有 yingke.hour 额度: %+v", activated.Products)
	}
	if got := string(activated.Products["yingke"]["hour"].Granted); got != "400.00" {
		t.Fatalf("yingke.hour 应发 400，实际 %s", got)
	}
	if len(activated.Receivables) != 1 {
		t.Fatalf("应有 1 张应收，实际 %+v", activated.Receivables)
	}
	if money(string(activated.Receivables[0].Amount)) != "23400.00" {
		t.Fatalf("应收应为 23400（12600+10800），实际 %s", activated.Receivables[0].Amount)
	}
	if money(string(activated.Receivables[0].Paid)) != "5000.00" {
		t.Fatalf("已收应为 5000，实际 %s", activated.Receivables[0].Paid)
	}
	if len(activated.PayLogs) != 1 || money(string(activated.PayLogs[0].Amount)) != "5000.00" {
		t.Fatalf("收款记录不对: %+v", activated.PayLogs)
	}

	// 订单/服务期/旧订阅表同步
	var orderStatus, orderPaid string
	if err := env.pool.QueryRow(ctx, `
SELECT status, paid_amount::text FROM billing_orders WHERE tenant_id = $1 ORDER BY id LIMIT 1`, institution.ID).
		Scan(&orderStatus, &orderPaid); err != nil {
		t.Fatalf("读取订单失败: %v", err)
	}
	if orderStatus != billing.OrderStatusPaid || money(orderPaid) != "5000.00" {
		t.Fatalf("订单状态/已收不对: status=%s paid=%s", orderStatus, orderPaid)
	}
	var periodStatus, periodEnd, subscriptionEnd, validTo string
	if err := env.pool.QueryRow(ctx, `
SELECT status, to_char(expired_on,'YYYY-MM-DD') FROM subscription_periods
WHERE tenant_id = $1 AND status = 'active'`, institution.ID).Scan(&periodStatus, &periodEnd); err != nil {
		t.Fatalf("读取服务期失败: %v", err)
	}
	if periodStatus != "active" || periodEnd != endDate {
		t.Fatalf("服务期不对: status=%s end=%s", periodStatus, periodEnd)
	}
	if err := env.pool.QueryRow(ctx, `
SELECT to_char(expired_on,'YYYY-MM-DD') FROM tenant_subscriptions WHERE tenant_id = $1`, institution.ID).Scan(&subscriptionEnd); err != nil {
		t.Fatalf("读取 tenant_subscriptions 失败: %v", err)
	}
	if subscriptionEnd != endDate {
		t.Fatalf("tenant_subscriptions 未同步: %s", subscriptionEnd)
	}
	if err := env.pool.QueryRow(ctx, `SELECT to_char(valid_to,'YYYY-MM-DD') FROM tenants WHERE id = $1`, institution.ID).Scan(&validTo); err != nil {
		t.Fatalf("读取 tenants.valid_to 失败: %v", err)
	}
	if validTo != endDate {
		t.Fatalf("tenants.valid_to 未同步: %s", validTo)
	}

	// 4. 加量：yingke × 2 包（+160 小时），生成应收 3000
	afterIncrement, err := env.svc.AddIncrement(ctx, institution.ID, billing.IncrementRequest{Code: "yingke", Packs: 2}, env.adminID)
	if err != nil {
		t.Fatalf("加量失败: %v", err)
	}
	if got := string(afterIncrement.Products["yingke"]["hour"].Granted); got != "560.00" {
		t.Fatalf("加量后 yingke.hour 应为 560，实际 %s", got)
	}
	if len(afterIncrement.Receivables) != 2 {
		t.Fatalf("加量后应有 2 张应收，实际 %d", len(afterIncrement.Receivables))
	}
	if money(string(afterIncrement.Receivables[1].Amount)) != "3000.00" {
		t.Fatalf("加量应收应为 3000（1500×2），实际 %s", afterIncrement.Receivables[1].Amount)
	}

	// 5. 赠送：yingke × 1 包（+80 小时），金额 0、不进应收
	afterGift, err := env.svc.GiftIncrement(ctx, institution.ID, billing.GiftIncrementRequest{
		Code: "yingke", Packs: 1, Reason: "客户补偿",
	}, env.adminID)
	if err != nil {
		t.Fatalf("赠送失败: %v", err)
	}
	if got := string(afterGift.Products["yingke"]["hour"].Granted); got != "640.00" {
		t.Fatalf("赠送后 yingke.hour 应为 640，实际 %s", got)
	}
	if len(afterGift.Receivables) != 2 {
		t.Fatalf("赠送不应生成应收，实际 %d 张", len(afterGift.Receivables))
	}

	// 赠送 >1 包没审批人 -> 400
	if _, err := env.svc.GiftIncrement(ctx, institution.ID, billing.GiftIncrementRequest{
		Code: "yingke", Packs: 2, Reason: "补偿",
	}, env.adminID); err == nil {
		t.Fatalf("赠送超过 1 包且无审批人应被拒绝")
	}

	// 6. 加产品：suifang 按剩余天数折算（yingke/kehu 已开通，不能重复加）
	afterAddProduct, err := env.svc.AddProduct(ctx, institution.ID, billing.AddProductRequest{Code: "suifang"}, env.adminID)
	if err != nil {
		t.Fatalf("加产品失败: %v", err)
	}
	if _, ok := afterAddProduct.Products["suifang"]; !ok {
		t.Fatalf("加产品后应有 suifang 额度")
	}
	if len(afterAddProduct.Receivables) != 3 {
		t.Fatalf("加产品后应有 3 张应收，实际 %d", len(afterAddProduct.Receivables))
	}
	// 重复加同一产品 -> 409
	if _, err := env.svc.AddProduct(ctx, institution.ID, billing.AddProductRequest{Code: "suifang"}, env.adminID); err == nil {
		t.Fatalf("重复加同一产品应返回冲突")
	}

	// 7. 续期整年：到期日顺延、发新一年额度、上期余额结转
	beforeRenew := afterAddProduct
	afterRenew, err := env.svc.RenewInstitution(ctx, institution.ID, billing.RenewRequest{Mode: "year"}, env.adminID)
	if err != nil {
		t.Fatalf("续期失败: %v", err)
	}
	oldEnd, _ := time.Parse("2006-01-02", beforeRenew.End)
	newEnd, _ := time.Parse("2006-01-02", afterRenew.End)
	if !newEnd.After(oldEnd) {
		t.Fatalf("续期后到期日应往后：%s -> %s", beforeRenew.End, afterRenew.End)
	}
	if got := string(afterRenew.Products["yingke"]["hour"].Granted); got != "1040.00" {
		t.Fatalf("续期后 yingke.hour 应为 1040（400 标准 + 640 结转），实际 %s", got)
	}
	if len(afterRenew.Receivables) != 4 {
		t.Fatalf("续期后应有 4 张应收，实际 %d", len(afterRenew.Receivables))
	}

	// 8. 收款：自动核销到最早的未收订单
	paidBefore := money(string(afterRenew.Receivables[0].Paid))
	afterPayment, err := env.svc.ReceiveInstitutionPayment(ctx, institution.ID, billing.ReceivePaymentRequest{
		Amount: "1000", Date: today, Method: "对公转账", Memo: "补款",
	}, env.adminID)
	if err != nil {
		t.Fatalf("收款失败: %v", err)
	}
	if len(afterPayment.PayLogs) != 2 {
		t.Fatalf("收款后应有 2 条收款记录，实际 %d", len(afterPayment.PayLogs))
	}
	if money(string(afterPayment.Receivables[0].Paid)) == paidBefore {
		t.Fatalf("收款后最早应收的已收金额应增加（原 %s）", paidBefore)
	}
	if afterPayment.Receivables[0].Amount != afterPayment.Receivables[0].Paid {
		// 这一单仍未收满是可以的，只要金额增加了；此处只做提示性断言。
		t.Logf("最早应收仍未收满：应收 %s 已收 %s", afterPayment.Receivables[0].Amount, afterPayment.Receivables[0].Paid)
	}
}

// ---------------- 归属 / 服务期 CRUD ----------------

func TestInstitutionOwnershipAndPeriods(t *testing.T) {
	env := newBillingTestEnv(t)
	ctx := env.ctx

	channelName := "归属渠道-" + env.suffix
	salesName := "归属销售-" + env.suffix
	var channelID int64
	if err := env.pool.QueryRow(ctx, `
INSERT INTO ops_organizations (name, type, status) VALUES ($1, 'agency', 'active') RETURNING id`, channelName).Scan(&channelID); err != nil {
		t.Fatalf("创建渠道失败: %v", err)
	}
	env.channelIDs = append(env.channelIDs, channelID)
	salesID := env.seedSales(channelID, salesName)
	otherSales := env.seedSales(channelID, "接手销售-"+env.suffix)

	institution := env.createInstitution("归属测试门诊", "口腔", []string{"口腔科"}, channelName, salesName)

	// 列表：1 条当前归属
	items, err := env.svc.ListOwnerships(ctx, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("查询归属失败: %v", err)
	}
	if len(items) != 1 || items[0].EffectiveTo != "" || items[0].SalesAdminID != salesID {
		t.Fatalf("当前归属不对: %+v", items)
	}

	// 换归属：旧行关闭、新行成为当前
	if _, err := env.svc.CreateOwnership(ctx, institution.ID, billing.OwnershipPayload{
		OrgID: channelID, SalesAdminID: otherSales, Source: "manual",
	}, env.adminID); err != nil {
		t.Fatalf("新增归属失败: %v", err)
	}
	items, err = env.svc.ListOwnerships(ctx, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("查询归属失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("换归属后应有 2 条历史，实际 %d", len(items))
	}
	current := 0
	for _, item := range items {
		if item.EffectiveTo == "" {
			current++
			if item.SalesAdminID != otherSales {
				t.Fatalf("当前归属销售不对: %+v", item)
			}
		}
	}
	if current != 1 {
		t.Fatalf("应只有 1 条当前归属，实际 %d", current)
	}

	// 修改归属
	updated, err := env.svc.UpdateOwnership(ctx, institution.ID, items[0].ID, billing.OwnershipPayload{
		OrgID: channelID, SalesAdminID: otherSales, Source: "migration",
	}, env.adminID)
	if err != nil {
		t.Fatalf("修改归属失败: %v", err)
	}
	if updated.Source != "migration" {
		t.Fatalf("归属来源未更新: %+v", updated)
	}

	// 关闭归属
	closed, err := env.svc.CloseOwnership(ctx, institution.ID, updated.ID, billing.OwnershipClosePayload{}, env.adminID)
	if err != nil {
		t.Fatalf("关闭归属失败: %v", err)
	}
	if closed.EffectiveTo == "" {
		t.Fatalf("关闭后 effectiveTo 不应为空: %+v", closed)
	}

	// 关错能改：手工补录服务期（active 会关闭旧的 active）并同步租户有效期
	start := time.Now().Format("2006-01-02")
	end := time.Now().AddDate(1, 0, -1).Format("2006-01-02")
	period, err := env.svc.CreateSubscriptionPeriod(ctx, institution.ID, billing.SubscriptionPeriodPayload{
		StartedOn: start, ExpiredOn: end, Status: "active", Source: "manual",
	}, env.adminID)
	if err != nil {
		t.Fatalf("新增服务期失败: %v", err)
	}
	if period.Status != "active" || period.ExpiredOn != end {
		t.Fatalf("服务期落库不对: %+v", period)
	}
	var validTo string
	if err := env.pool.QueryRow(ctx, `SELECT to_char(valid_to,'YYYY-MM-DD') FROM tenants WHERE id = $1`, institution.ID).Scan(&validTo); err != nil {
		t.Fatalf("读取租户有效期失败: %v", err)
	}
	if validTo != end {
		t.Fatalf("手工服务期未同步 tenants.valid_to: %s", validTo)
	}

	// 调整服务期到期日
	newEnd := time.Now().AddDate(2, 0, -1).Format("2006-01-02")
	updatedPeriod, err := env.svc.UpdateSubscriptionPeriod(ctx, institution.ID, period.ID, billing.SubscriptionPeriodPayload{
		ExpiredOn: newEnd,
	}, env.adminID)
	if err != nil {
		t.Fatalf("调整服务期失败: %v", err)
	}
	if updatedPeriod.ExpiredOn != newEnd {
		t.Fatalf("服务期未更新: %+v", updatedPeriod)
	}

	// 列表：服务期历史可查
	periods, err := env.svc.ListSubscriptionPeriods(ctx, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("查询服务期失败: %v", err)
	}
	if len(periods) == 0 || periods[0].ID != period.ID {
		t.Fatalf("服务期列表不对: %+v", periods)
	}

	// 起止日倒置 -> 400
	if _, err := env.svc.CreateSubscriptionPeriod(ctx, institution.ID, billing.SubscriptionPeriodPayload{
		StartedOn: newEnd, ExpiredOn: start, Status: "active",
	}, env.adminID); err == nil {
		t.Fatalf("到期日早于开始日应被拒绝")
	}
}

// ---------------- 试用开通 + 延长试用 ----------------

func TestInstitutionTrialFlow(t *testing.T) {
	env := newBillingTestEnv(t)
	ctx := env.ctx

	channelName := "试用渠道-" + env.suffix
	salesName := "试用销售-" + env.suffix
	var channelID int64
	if err := env.pool.QueryRow(ctx, `
INSERT INTO ops_organizations (name, type, status) VALUES ($1, 'agency', 'active') RETURNING id`, channelName).Scan(&channelID); err != nil {
		t.Fatalf("创建渠道失败: %v", err)
	}
	env.channelIDs = append(env.channelIDs, channelID)
	env.seedSales(channelID, salesName)
	institution := env.createInstitution("试用测试眼科", "眼科", []string{"眼科"}, channelName, salesName)

	trial, err := env.svc.ActivateTrial(ctx, institution.ID, billing.TrialActivationRequest{
		ProductCodes: []string{"tingxie"}, Days: 14,
	}, env.adminID)
	if err != nil {
		t.Fatalf("试用开通失败: %v", err)
	}
	if trial.Mode != "trial" || !trial.Open {
		t.Fatalf("试用状态不对: mode=%s open=%v", trial.Mode, trial.Open)
	}
	// 14 天试用按含首尾计算：今天算第 1 天，到期日 = 今天 + 13
	expectedEnd := time.Now().AddDate(0, 0, 13).Format("2006-01-02")
	if trial.End != expectedEnd {
		t.Fatalf("试用到期日应为 %s，实际 %s", expectedEnd, trial.End)
	}
	// tingxie record 标准 2500 × 14 / 365 = 95.89 → 发放时取整为 96（最少 1）
	if got := string(trial.Products["tingxie"]["record"].Granted); got != "96.00" {
		t.Fatalf("试用额度应为 96，实际 %s", got)
	}
	var orderCount int
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM billing_orders WHERE tenant_id = $1`, institution.ID).Scan(&orderCount); err != nil {
		t.Fatalf("读取试用订单失败: %v", err)
	}
	if orderCount != 1 {
		t.Fatalf("试用应生成 1 张订单，实际 %d", orderCount)
	}

	// 延长试用 7 天：到期日后延、额度按比例增加
	extended, err := env.svc.ExtendTrial(ctx, institution.ID, billing.ExtendTrialRequest{Days: 7}, env.adminID)
	if err != nil {
		t.Fatalf("延长试用失败: %v", err)
	}
	// 14 天 + 延 7 天 = 21 天（含首尾），到期日 = 今天 + 20
	expectedExtendedEnd := time.Now().AddDate(0, 0, 20).Format("2006-01-02")
	if extended.End != expectedExtendedEnd {
		t.Fatalf("延长后到期日应为 %s，实际 %s", expectedExtendedEnd, extended.End)
	}
	// 延长后按总天数重算：2500 × 21 / 365 = 143.8 → 144，不是 96 + round(2500×7/365)=48 的简单叠加
	if got := string(extended.Products["tingxie"]["record"].Granted); got != "144.00" {
		t.Fatalf("延长试用后额度应为 144，实际 %s", got)
	}

	// 试用机构不能加量
	if _, err := env.svc.AddIncrement(ctx, institution.ID, billing.IncrementRequest{Code: "tingxie", Packs: 1}, env.adminID); err == nil {
		t.Fatalf("试用机构加量应被拒绝")
	}
}

// TestInstitutionBundleActivationAndPricing 验证套餐价：按套餐开通时应收是套餐价，
// 不是单品相加；同时验证 /pricing 返回四个套餐与 singlePrice。
func TestInstitutionBundleActivationAndPricing(t *testing.T) {
	env := newBillingTestEnv(t)
	ctx := env.ctx

	channelName, salesName := env.seedChannelWithSales("套餐")
	institution := env.createInstitution("套餐测试门诊", "口腔", []string{"口腔科"}, channelName, salesName)

	pricing, err := env.svc.GetPricing(ctx)
	if err != nil {
		t.Fatalf("读取价目失败: %v", err)
	}
	if len(pricing.Products) < 6 {
		t.Fatalf("价目应有 6 个产品，实际 %d", len(pricing.Products))
	}
	if len(pricing.Bundles) < 4 {
		t.Fatalf("价目应有 4 个套餐，实际 %d", len(pricing.Bundles))
	}
	var deal *billing.BundleDefinition
	for index := range pricing.Bundles {
		if pricing.Bundles[index].Key == "deal" {
			deal = &pricing.Bundles[index]
		}
	}
	if deal == nil {
		t.Fatalf("缺少到院成交套餐")
	}
	if money(string(deal.Price)) != "17800.00" || money(string(deal.SinglePrice)) != "23400.00" {
		t.Fatalf("套餐价/单价合计不对: price=%s single=%s", deal.Price, deal.SinglePrice)
	}

	endDate := time.Now().AddDate(1, 0, -1).Format("2006-01-02")
	activated, err := env.svc.ActivateFormal(ctx, institution.ID, billing.FormalActivationRequest{
		ProductCodes: []string{"yingke", "kehu"},
		BundleKey:    "deal",
		EndDate:      endDate,
		Payment:      billing.ActivationPayment{Mode: "unpaid"},
	}, env.adminID)
	if err != nil {
		t.Fatalf("按套餐开通失败: %v", err)
	}
	if len(activated.Receivables) != 1 {
		t.Fatalf("套餐开通应有 1 张应收，实际 %d", len(activated.Receivables))
	}
	if money(string(activated.Receivables[0].Amount)) != "17800.00" {
		t.Fatalf("套餐开通应收应为 17800，实际 %s", activated.Receivables[0].Amount)
	}
	var orderType, total string
	if err := env.pool.QueryRow(ctx, `
SELECT order_type, total_amount::text FROM billing_orders WHERE tenant_id = $1 ORDER BY id LIMIT 1`, institution.ID).
		Scan(&orderType, &total); err != nil {
		t.Fatalf("读取套餐订单失败: %v", err)
	}
	if orderType != billing.OrderTypeBundle || money(total) != "17800.00" {
		t.Fatalf("套餐订单不对: type=%s total=%s", orderType, total)
	}

	// 套餐与勾选产品不一致 -> 400
	other, err := env.svc.ActivateFormal(ctx, institution.ID, billing.FormalActivationRequest{
		ProductCodes: []string{"yingke", "kehu", "suifang"},
		BundleKey:    "deal",
		EndDate:      endDate,
	}, env.adminID)
	if err == nil {
		t.Fatalf("套餐与产品不一致应被拒绝: %+v", other)
	}
}

// TestInstitutionRenewRolloverOnce 验证续期「只结转一次」：
// 第一次续期把本期剩余全部结转；第二次续期只结转本期新增没用的部分，历史结转不再滚。
func TestInstitutionRenewRolloverOnce(t *testing.T) {
	env := newBillingTestEnv(t)
	ctx := env.ctx

	channelName, salesName := env.seedChannelWithSales("续期")
	institution := env.createInstitution("续期测试门诊", "口腔", []string{"口腔科"}, channelName, salesName)
	env.activateFormalUnpaid(institution.ID, "yingke") // 400
	if _, err := env.svc.AddIncrement(ctx, institution.ID, billing.IncrementRequest{Code: "yingke", Packs: 1}, env.adminID); err != nil {
		t.Fatalf("加量失败: %v", err)
	} // 400 + 80 = 480

	first, err := env.svc.RenewInstitution(ctx, institution.ID, billing.RenewRequest{Mode: "year"}, env.adminID)
	if err != nil {
		t.Fatalf("第一次续期失败: %v", err)
	}
	if got := string(first.Products["yingke"]["hour"].Granted); got != "880.00" {
		t.Fatalf("第一次续期后应为 880（400 标准 + 480 结转），实际 %s", got)
	}

	second, err := env.svc.RenewInstitution(ctx, institution.ID, billing.RenewRequest{Mode: "custom", EndDate: time.Now().AddDate(3, 0, -1).Format("2006-01-02")}, env.adminID)
	if err != nil {
		t.Fatalf("第二次续期失败: %v", err)
	}
	// 第二次续期时 balance=880、rollover=480 -> 只结转 400（本期自己没用完的），
	// 新授予 = 400 标准 + 400 = 800；历史结转的 480 不再滚。
	if got := string(second.Products["yingke"]["hour"].Granted); got != "800.00" {
		t.Fatalf("第二次续期后应为 800（400 + 400，只结转一次），实际 %s", got)
	}
	if len(second.Receivables) != 4 {
		t.Fatalf("两次续期后应有 4 张应收（开通 + 加量 + 两次续期），实际 %d", len(second.Receivables))
	}
}

// TestInstitutionDataScope 验证「运营只能看自己有权限的租户」：
// 渠道账号只能看到自己组织名下的机构，平台账号可看全部；越权读详情/建档 403。
func TestInstitutionDataScope(t *testing.T) {
	env := newBillingTestEnv(t)
	ctx := env.ctx

	channelName, salesName := env.seedChannelWithSales("范围")
	institution := env.createInstitution("范围测试门诊", "口腔", []string{"口腔科"}, channelName, salesName)

	// 另一个渠道组织 + 运营账号
	otherChannelName := "他组织渠道-" + env.suffix
	var otherOrgID int64
	if err := env.pool.QueryRow(ctx, `
INSERT INTO ops_organizations (name, type, status) VALUES ($1, 'agency', 'active') RETURNING id`, otherChannelName).Scan(&otherOrgID); err != nil {
		t.Fatalf("创建其他组织失败: %v", err)
	}
	env.channelIDs = append(env.channelIDs, otherOrgID)
	env.seedSales(otherOrgID, "他组织销售-"+env.suffix)
	var otherAdminID int64
	if err := env.pool.QueryRow(ctx, `
INSERT INTO operations_admins (name, phone, username, password_hash, org_id, is_active, session_version, created_at, updated_at)
VALUES ($1, $2::varchar, $2::text, 'x', $3, 1, 1, NOW(), NOW())
RETURNING id`, "他组织运营-"+env.suffix, env.nextPhone("19"), otherOrgID).Scan(&otherAdminID); err != nil {
		t.Fatalf("创建其他运营失败: %v", err)
	}
	env.salesIDs = append(env.salesIDs, otherAdminID)

	// 平台账号看得到
	platformList, err := env.svc.ListInstitutions(ctx, billing.InstitutionQuery{}, env.adminID)
	if err != nil {
		t.Fatalf("平台账号查询机构失败: %v", err)
	}
	found := false
	for _, item := range platformList.Items {
		if item.ID == institution.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("平台账号应能看到机构 %d", institution.ID)
	}

	// 他组织账号看不到
	otherList, err := env.svc.ListInstitutions(ctx, billing.InstitutionQuery{}, otherAdminID)
	if err != nil {
		t.Fatalf("他组织账号查询机构失败: %v", err)
	}
	if otherList.Total != 0 {
		t.Fatalf("他组织账号不应看到别家机构，实际 %d 条", otherList.Total)
	}

	// 越权读详情 403
	if _, err := env.svc.GetInstitutionForAdmin(ctx, institution.ID, otherAdminID); err == nil {
		t.Fatalf("越权读机构详情应被拒绝")
	}
	// 越权给别家渠道建档 403
	if _, err := env.svc.CreateInstitution(ctx, billing.CreateInstitutionRequest{
		Name: "越权建档", Legal: "越权建档有限公司", Industry: "口腔",
		Region:  billing.InstitutionRegion{Province: "310000", City: "310100", District: "310104"},
		Contact: "联系人", Phone: env.nextPhone("15"), Channel: channelName, Sales: salesName,
	}, otherAdminID); err == nil {
		t.Fatalf("越权给别家渠道建档应被拒绝")
	}
	// 渠道账号不能改全局配置
	if _, err := env.svc.SaveChannel(ctx, billing.ChannelPayload{
		Name: "越权渠道-" + env.suffix, Type: "渠道商", Active: boolPtr(true),
	}, otherAdminID); err == nil {
		t.Fatalf("渠道账号修改渠道配置应被拒绝")
	}

	// 用自己的渠道建档后，他组织列表能看到 1 条
	own := env.createInstitution("他组织门诊", "眼科", []string{"眼科"}, otherChannelName, "他组织销售-"+env.suffix)
	otherList, err = env.svc.ListInstitutions(ctx, billing.InstitutionQuery{}, otherAdminID)
	if err != nil {
		t.Fatalf("他组织账号再次查询失败: %v", err)
	}
	if otherList.Total != 1 || otherList.Items[0].ID != own.ID {
		t.Fatalf("他组织账号应只看到自己名下 1 条，实际 %+v", otherList.Items)
	}
}
