package billing

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/pkg/auth"
)

// ---------- fakeRepository 的阶段 E 实现 ----------

// fakeSubscription 模拟 tenant_subscriptions 的一行（一个租户最新一条订阅）。
type fakeSubscription struct {
	Status string
	Period ServicePeriod
}

// fakeLedgerWrite 记录 fake store 写出的流水（带租户，便于多租户断言）。
type fakeLedgerWrite struct {
	TenantID    int64
	OrderID     int64
	ProductCode string
	Unit        string
	Type        string
	Amount      Decimal
	Source      string
	Reason      string
}

// RenewOrder 模拟续费事务：抢订单行 → 校验服务期 → 用生产侧纯函数 planRenewal 算结转 →
// 覆盖额度行/写流水/延长服务期。写库语义与 renew_store.go 保持一致，便于 service 层断言。
func (f *fakeRepository) RenewOrder(_ context.Context, input RenewOrderInput) (Order, ServicePeriod, error) {
	f.renewCalls++
	f.renewInput = input
	if f.renewErr != nil {
		return Order{}, ServicePeriod{}, f.renewErr
	}
	order, ok := f.orders[input.OrderID]
	if !ok {
		return Order{}, ServicePeriod{}, ErrOrderNotFound
	}
	if order.ActivatedAt != nil {
		return Order{}, ServicePeriod{}, ErrOrderAlreadyActivated
	}
	if !containsString(input.AllowedStatuses, order.Status) {
		return Order{}, ServicePeriod{}, ErrOrderStatusInvalid
	}
	subscription, ok := f.subscriptions[order.TenantID]
	if !ok || subscription.Status == "" || subscription.Period.ExpiredOn == nil {
		return Order{}, ServicePeriod{}, ErrNoSubscription
	}

	plan, err := planRenewal(
		subscription.Period,
		dateOnly(input.Today),
		f.entitlementsByTenant[order.TenantID],
		input.Grants,
		input.LedgerSource,
		input.Reason,
	)
	if err != nil {
		return Order{}, ServicePeriod{}, err
	}

	f.subscriptions[order.TenantID] = fakeSubscription{Status: "active", Period: plan.Period}
	f.entitlementsByTenant[order.TenantID] = entitlementsFromRenewalPlan(plan)
	for _, entry := range plan.Ledger {
		f.renewLedger = append(f.renewLedger, fakeLedgerWrite{
			TenantID:    order.TenantID,
			OrderID:     input.OrderID,
			ProductCode: entry.ProductCode,
			Unit:        entry.Unit,
			Type:        entry.Type,
			Amount:      entry.Amount,
			Source:      entry.Source,
			Reason:      entry.Reason,
		})
	}

	activatedAt := time.Date(2026, 11, 10, 10, 30, 0, 0, time.UTC)
	actor := input.ActorID
	order.ActivatedAt = &activatedAt
	order.ActivatedBy = &actor
	f.orders[input.OrderID] = order
	return order, plan.Period, nil
}

// ListExpiringTenantIDs 用与 SQL 相同的条件（status='active' 且 expired_on < today）筛选租户。
func (f *fakeRepository) ListExpiringTenantIDs(_ context.Context, today string) ([]int64, error) {
	f.expireListCalls++
	if f.expireErr != nil {
		return nil, f.expireErr
	}
	cutoff, err := time.Parse("2006-01-02", today)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(f.subscriptions))
	for tenantID, subscription := range f.subscriptions {
		if subscription.Status != "active" {
			continue
		}
		expiredOn, err := parseServiceDate("expired_on", subscription.Period.ExpiredOn)
		if err != nil {
			continue
		}
		if expiredOn.Before(cutoff) {
			ids = append(ids, tenantID)
		}
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	return ids, nil
}

// ExpireTenant 模拟到期清理事务：订阅置 expired、全部额度行清零、逐行写 usage/expired。
func (f *fakeRepository) ExpireTenant(_ context.Context, tenantID int64, today string) (int, error) {
	f.expireCalls = append(f.expireCalls, tenantID)
	if f.expireErr != nil {
		return 0, f.expireErr
	}
	subscription, ok := f.subscriptions[tenantID]
	if !ok || subscription.Status != "active" {
		return 0, nil
	}
	cutoff, err := time.Parse("2006-01-02", today)
	if err != nil {
		return 0, err
	}
	expiredOn, err := parseServiceDate("expired_on", subscription.Period.ExpiredOn)
	if err != nil || !expiredOn.Before(cutoff) {
		return 0, nil
	}

	rows := f.entitlementsByTenant[tenantID]
	entries, err := planExpiryLedger(rows)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		f.expireLedger = append(f.expireLedger, fakeLedgerWrite{
			TenantID:    tenantID,
			ProductCode: entry.ProductCode,
			Unit:        entry.Unit,
			Type:        LedgerTypeUsage,
			Amount:      entry.Amount,
			Source:      ledgerSourceExpired,
			Reason:      reasonSubscriptionExpired,
		})
	}
	for index := range rows {
		rows[index].Granted = Decimal("0.00")
		rows[index].Used = Decimal("0.00")
		rows[index].RolloverBal = Decimal("0.00")
		rows[index].Balance = Decimal("0.00")
		rows[index].Status = entitlementStatusCleared
	}
	f.entitlementsByTenant[tenantID] = rows
	f.subscriptions[tenantID] = fakeSubscription{Status: "expired", Period: subscription.Period}
	return len(rows), nil
}

// entitlementsFromRenewalPlan 把续费计划里的新行状态还原成额度行（模拟整行覆盖写入）。
func entitlementsFromRenewalPlan(plan RenewalPlan) []Entitlement {
	rows := make([]Entitlement, 0, len(plan.Updates))
	for _, update := range plan.Updates {
		rows = append(rows, Entitlement{
			ProductCode: update.ProductCode,
			Unit:        update.Unit,
			Edition:     update.Edition,
			QuotaType:   update.QuotaType,
			Granted:     update.Granted,
			Used:        update.Used,
			RolloverBal: update.RolloverBal,
			Balance:     update.Balance,
			PeriodEnd:   update.PeriodEnd,
			Status:      update.Status,
		})
	}
	return rows
}

// ---------- 测试辅助 ----------

const renewTestProduct = "tingxie"

// serviceTestPeriod 构造服务期的日期文本指针。
func serviceTestPeriod(startedOn, expiredOn string) ServicePeriod {
	return ServicePeriod{StartedOn: stringPointer(startedOn), ExpiredOn: stringPointer(expiredOn)}
}

// renewalRow 构造一行额度；金额参数用库里的 NUMERIC 文本。
func renewalRow(productCode, unit, quotaType, granted, used, rolloverBal, balance, status string) Entitlement {
	return Entitlement{
		ProductCode: productCode,
		Unit:        unit,
		Edition:     EditionStandard,
		QuotaType:   quotaType,
		Granted:     Decimal(granted),
		Used:        Decimal(used),
		RolloverBal: Decimal(rolloverBal),
		Balance:     Decimal(balance),
		PeriodEnd:   "2027-11-09",
		Status:      status,
	}
}

// renewalGrant 构造订单要下发的一行额度。
func renewalGrant(productCode, unit, amount string) GrantLine {
	return GrantLine{
		ProductCode: productCode,
		Unit:        unit,
		Edition:     EditionStandard,
		QuotaType:   QuotaTypeOfficial,
		Amount:      Decimal(amount),
	}
}

// renewalOrderFixture 构造一笔已回款、待开通的续费订单（听写病历 record 2500）。
func renewalOrderFixture(tenantID int64) Order {
	return Order{
		ID:            1,
		OrderNo:       "BL20261110103045123",
		TenantID:      tenantID,
		OrderType:     OrderTypeSingle,
		Status:        OrderStatusSettled,
		Items:         []OrderItem{{ProductCode: renewTestProduct, Edition: EditionStandard, QuotaType: QuotaTypeOfficial, Quotas: Quantities(`{"record":2500}`), Price: Decimal("21600.00")}},
		TotalAmount:   Decimal("21600.00"),
		PayableAmount: Decimal("21600.00"),
	}
}

// newRenewTestService 构造固定"今天=2026-11-10"的续费测试服务与 fake repo。
func newRenewTestService(tenantID int64, subscription fakeSubscription, rows ...Entitlement) (*Service, *fakeRepository) {
	repo := &fakeRepository{
		orders:               map[int64]Order{1: renewalOrderFixture(tenantID)},
		subscriptions:        map[int64]fakeSubscription{tenantID: subscription},
		entitlementsByTenant: map[int64][]Entitlement{tenantID: rows},
		adminName:            "运营小张",
	}
	service := NewService(repo)
	service.now = func() time.Time {
		return time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC)
	}
	service.orderNoRand = func(int) int { return 123 }
	return service, repo
}

func findRenewalUpdate(t *testing.T, plan RenewalPlan, productCode, unit, quotaType string) RenewalUpdate {
	t.Helper()
	for _, update := range plan.Updates {
		if normalizeEnum(update.ProductCode) == productCode && update.Unit == unit && update.QuotaType == quotaType {
			return update
		}
	}
	t.Fatalf("renewal update %s/%s/%s not found in %+v", productCode, unit, quotaType, plan.Updates)
	return RenewalUpdate{}
}

func findRenewalLedger(t *testing.T, entries []RenewalLedgerEntry, unit, ledgerType, source string) RenewalLedgerEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.Unit == unit && entry.Type == ledgerType && entry.Source == source {
			return entry
		}
	}
	t.Fatalf("renewal ledger %s/%s/%s not found in %+v", unit, ledgerType, source, entries)
	return RenewalLedgerEntry{}
}

func findFakeLedger(t *testing.T, entries []fakeLedgerWrite, tenantID int64, unit, ledgerType, source string) fakeLedgerWrite {
	t.Helper()
	for _, entry := range entries {
		if entry.TenantID == tenantID && entry.Unit == unit && entry.Type == ledgerType && entry.Source == source {
			return entry
		}
	}
	t.Fatalf("ledger %d/%s/%s/%s not found in %+v", tenantID, unit, ledgerType, source, entries)
	return fakeLedgerWrite{}
}

// ---------- E2 续费结转公式 ----------

func TestPlanRenewalCarriesOnlyThisPeriodUnused(t *testing.T) {
	today := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	// old_balance=200、old_rollover=50：本期自己没用完 = 200-50 = 150。
	rows := []Entitlement{renewalRow(renewTestProduct, "record", QuotaTypeOfficial, "2500", "2350", "50", "200", entitlementStatusActive)}
	plan, err := planRenewal(
		serviceTestPeriod("2026-11-10", "2027-11-09"),
		today,
		rows,
		[]GrantLine{renewalGrant(renewTestProduct, "record", "2500")},
		LedgerSourcePurchase,
		"回款确认按单下发",
	)
	if err != nil {
		t.Fatalf("planRenewal returned error: %v", err)
	}
	if !plan.RolloverAllowed {
		t.Fatal("service period not expired, rollover must be allowed")
	}
	update := findRenewalUpdate(t, plan, renewTestProduct, "record", QuotaTypeOfficial)
	if update.RolloverBal != Decimal("150.00") {
		t.Fatalf("new rollover_bal = %s, want 150.00 (old rollover must not be carried again)", update.RolloverBal)
	}
	if update.Balance != Decimal("2650.00") || update.Granted != Decimal("2500.00") || update.Used != Decimal("0.00") {
		t.Fatalf("unexpected renewed row: %+v", update)
	}
	if update.PeriodEnd != "2028-11-08" {
		t.Fatalf("period_end = %q, want 2028-11-08", update.PeriodEnd)
	}
	if update.Status != entitlementStatusActive {
		t.Fatalf("status = %q, want active", update.Status)
	}
	if plan.Period.StartedOn == nil || *plan.Period.StartedOn != "2026-11-10" {
		t.Fatalf("started_on must stay unchanged, got %v", plan.Period.StartedOn)
	}
	if plan.Period.ExpiredOn == nil || *plan.Period.ExpiredOn != "2028-11-08" {
		t.Fatalf("expired_on = %v, want 2028-11-08", plan.Period.ExpiredOn)
	}

	// 被丢弃的老结转 50 元写 usage/expired；新结转 150 写 grant/rollover；新授予 2500 写 grant/purchase。
	expired := findRenewalLedger(t, plan.Ledger, "record", LedgerTypeUsage, ledgerSourceExpired)
	if expired.Amount != Decimal("-50.00") || expired.Reason != reasonRolloverExpired {
		t.Fatalf("unexpected expired ledger: %+v", expired)
	}
	rollover := findRenewalLedger(t, plan.Ledger, "record", LedgerTypeGrant, ledgerSourceRollover)
	if rollover.Amount != Decimal("150.00") || rollover.Reason != reasonRollover {
		t.Fatalf("unexpected rollover ledger: %+v", rollover)
	}
	granted := findRenewalLedger(t, plan.Ledger, "record", LedgerTypeGrant, LedgerSourcePurchase)
	if granted.Amount != Decimal("2500.00") {
		t.Fatalf("unexpected purchase ledger: %+v", granted)
	}
}

func TestPlanRenewalClearsPreviousRolloverOnSecondRenewal(t *testing.T) {
	today := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	// 第二个周期：old_balance=300、old_rollover=150（上一期结转来的）→ 新结转只有 150，
	// 老的 150 被清掉，不会再滚第三期。
	rows := []Entitlement{renewalRow(renewTestProduct, "record", QuotaTypeOfficial, "2500", "2350", "150", "300", entitlementStatusActive)}
	plan, err := planRenewal(
		serviceTestPeriod("2026-11-10", "2027-11-09"),
		today,
		rows,
		[]GrantLine{renewalGrant(renewTestProduct, "record", "2500")},
		LedgerSourcePurchase,
		"续费下发",
	)
	if err != nil {
		t.Fatalf("planRenewal returned error: %v", err)
	}
	update := findRenewalUpdate(t, plan, renewTestProduct, "record", QuotaTypeOfficial)
	if update.RolloverBal != Decimal("150.00") {
		t.Fatalf("second renewal rollover = %s, want 150.00", update.RolloverBal)
	}
	if update.Balance != Decimal("2650.00") {
		t.Fatalf("balance = %s, want 150.00 carried + 2500.00 granted", update.Balance)
	}
	expired := findRenewalLedger(t, plan.Ledger, "record", LedgerTypeUsage, ledgerSourceExpired)
	if expired.Amount != Decimal("-150.00") {
		t.Fatalf("dropped rollover ledger = %s, want -150.00", expired.Amount)
	}
}

func TestPlanRenewalSkipsRolloverAfterThirtyDayWindow(t *testing.T) {
	today := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	rows := []Entitlement{renewalRow(renewTestProduct, "record", QuotaTypeOfficial, "2500", "2350", "50", "200", entitlementStatusActive)}

	// 脱期 31 天：视为新购，余额全部清零（不是 150，也不是 200）。
	plan, err := planRenewal(
		serviceTestPeriod("2025-11-10", "2026-10-10"),
		today,
		rows,
		[]GrantLine{renewalGrant(renewTestProduct, "record", "2500")},
		LedgerSourcePurchase,
		"续费下发",
	)
	if err != nil {
		t.Fatalf("planRenewal returned error: %v", err)
	}
	if plan.RolloverAllowed {
		t.Fatal("expired for 31 days must not roll over")
	}
	update := findRenewalUpdate(t, plan, renewTestProduct, "record", QuotaTypeOfficial)
	if update.RolloverBal != Decimal("0.00") || update.Balance != Decimal("2500.00") {
		t.Fatalf("late renewal must start from 0 rollover, got %+v", update)
	}
	if plan.Period.StartedOn == nil || *plan.Period.StartedOn != "2026-11-10" {
		t.Fatalf("late renewal must restart from today, got %v", plan.Period.StartedOn)
	}
	if plan.Period.ExpiredOn == nil || *plan.Period.ExpiredOn != "2027-11-09" {
		t.Fatalf("late renewal expired_on = %v, want 2027-11-09", plan.Period.ExpiredOn)
	}
	expired := findRenewalLedger(t, plan.Ledger, "record", LedgerTypeUsage, ledgerSourceExpired)
	if expired.Amount != Decimal("-200.00") || expired.Reason != reasonRenewLateExpired {
		t.Fatalf("late renewal must clear the whole old balance, got %+v", expired)
	}
}

func TestPlanRenewalAllowsRolloverOnExactlyThirtyDays(t *testing.T) {
	today := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	rows := []Entitlement{renewalRow(renewTestProduct, "record", QuotaTypeOfficial, "2500", "2350", "50", "200", entitlementStatusActive)}

	// 刚好 30 天（expired_on = today - 30）：仍在窗口内，允许结转。
	plan, err := planRenewal(
		serviceTestPeriod("2025-11-10", "2026-10-11"),
		today,
		rows,
		[]GrantLine{renewalGrant(renewTestProduct, "record", "2500")},
		LedgerSourcePurchase,
		"续费下发",
	)
	if err != nil {
		t.Fatalf("planRenewal returned error: %v", err)
	}
	if !plan.RolloverAllowed {
		t.Fatal("exactly 30 days overdue must still roll over")
	}
	update := findRenewalUpdate(t, plan, renewTestProduct, "record", QuotaTypeOfficial)
	if update.RolloverBal != Decimal("150.00") {
		t.Fatalf("rollover = %s, want 150.00", update.RolloverBal)
	}
}

func TestPlanRenewalClearsTrialQuotaWithoutRollover(t *testing.T) {
	today := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	rows := []Entitlement{
		renewalRow(renewTestProduct, "record", QuotaTypeTrial, "50", "20", "0", "30", entitlementStatusActive),
		renewalRow(renewTestProduct, "hour", QuotaTypeOfficial, "400", "400", "0", "0", entitlementStatusActive),
	}
	plan, err := planRenewal(
		serviceTestPeriod("2026-11-10", "2027-11-09"),
		today,
		rows,
		[]GrantLine{renewalGrant(renewTestProduct, "hour", "400")},
		LedgerSourcePurchase,
		"续费下发",
	)
	if err != nil {
		t.Fatalf("planRenewal returned error: %v", err)
	}
	trial := findRenewalUpdate(t, plan, renewTestProduct, "record", QuotaTypeTrial)
	if trial.Granted != Decimal("0.00") || trial.Used != Decimal("0.00") ||
		trial.RolloverBal != Decimal("0.00") || trial.Balance != Decimal("0.00") ||
		trial.Status != entitlementStatusCleared {
		t.Fatalf("trial row must be cleared, got %+v", trial)
	}
	expired := findRenewalLedger(t, plan.Ledger, "record", LedgerTypeUsage, ledgerSourceExpired)
	if expired.Amount != Decimal("-30.00") || expired.Reason != reasonTrialExpired {
		t.Fatalf("unexpected trial clearing ledger: %+v", expired)
	}
	// trial 的余额不清到 official 行上。
	official := findRenewalUpdate(t, plan, renewTestProduct, "hour", QuotaTypeOfficial)
	if official.Balance != Decimal("400.00") || official.RolloverBal != Decimal("0.00") {
		t.Fatalf("official row should only get the new grant, got %+v", official)
	}
}

func TestPlanRenewalRestartsExpiredPeriodFromToday(t *testing.T) {
	today := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	plan, err := planRenewal(
		serviceTestPeriod("2025-11-10", "2026-11-01"),
		today,
		nil,
		nil,
		LedgerSourcePurchase,
		"续费下发",
	)
	if err != nil {
		t.Fatalf("planRenewal returned error: %v", err)
	}
	if plan.Period.StartedOn == nil || *plan.Period.StartedOn != "2026-11-10" ||
		plan.Period.ExpiredOn == nil || *plan.Period.ExpiredOn != "2027-11-09" {
		t.Fatalf("unexpected restarted period: %+v", plan.Period)
	}
}

func TestAddServiceYearClampsLeapDay(t *testing.T) {
	// 与 PostgreSQL date + interval '12 months' 一致：2/29 夹到 2/28，再减 1 天。
	leap := time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC)
	if got := addServiceYear(leap); !got.Equal(time.Date(2029, 2, 27, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("addServiceYear(2028-02-29) = %s, want 2029-02-27", got.Format("2006-01-02"))
	}
	regular := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	if got := addServiceYear(regular); !got.Equal(time.Date(2027, 11, 8, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("addServiceYear(2026-11-09) = %s, want 2027-11-08", got.Format("2006-01-02"))
	}
}

// ---------- E2 续费接口 ----------

func TestActivateOrderRenewExtendsPeriodAndCarriesBalance(t *testing.T) {
	service, repo := newRenewTestService(9,
		fakeSubscription{Status: "active", Period: serviceTestPeriod("2026-11-10", "2027-11-09")},
		renewalRow(renewTestProduct, "record", QuotaTypeOfficial, "2500", "2350", "50", "200", entitlementStatusActive),
	)

	result, err := service.ActivateOrderWithMode(context.Background(), 1, 7, ActivateModeRenew)
	if err != nil {
		t.Fatalf("renew activation returned error: %v", err)
	}
	if repo.renewCalls != 1 || repo.activateCalls != 0 {
		t.Fatalf("renew must use RenewOrder only, renew=%d activate=%d", repo.renewCalls, repo.activateCalls)
	}
	if repo.renewInput.Today.Format("2006-01-02") != "2026-11-10" {
		t.Fatalf("today = %s, want 2026-11-10", repo.renewInput.Today.Format("2006-01-02"))
	}
	if repo.renewInput.LedgerSource != LedgerSourcePurchase || repo.renewInput.Operator != "运营小张" {
		t.Fatalf("unexpected renewal metadata: %+v", repo.renewInput)
	}
	if result.Order.ActivatedAt == nil {
		t.Fatal("order must be marked activated")
	}
	if result.ServicePeriod == nil || result.ServicePeriod.ExpiredOn == nil || *result.ServicePeriod.ExpiredOn != "2028-11-08" {
		t.Fatalf("unexpected service period: %+v", result.ServicePeriod)
	}
	if len(result.Entitlements) != 1 {
		t.Fatalf("expected 1 entitlement row, got %+v", result.Entitlements)
	}
	row := result.Entitlements[0]
	if row.RolloverBal != Decimal("150.00") || row.Balance != Decimal("2650.00") ||
		row.Granted != Decimal("2500.00") || row.Used != Decimal("0.00") ||
		row.PeriodEnd != "2028-11-08" || row.Status != entitlementStatusActive {
		t.Fatalf("unexpected renewed entitlement: %+v", row)
	}
	assertBalanceInvariant(t, row)

	expired := findFakeLedger(t, repo.renewLedger, 9, "record", LedgerTypeUsage, ledgerSourceExpired)
	if expired.Amount != Decimal("-50.00") || expired.OrderID != 1 {
		t.Fatalf("unexpected expired ledger: %+v", expired)
	}
	rollover := findFakeLedger(t, repo.renewLedger, 9, "record", LedgerTypeGrant, ledgerSourceRollover)
	if rollover.Amount != Decimal("150.00") || rollover.Reason != reasonRollover {
		t.Fatalf("unexpected rollover ledger: %+v", rollover)
	}
	granted := findFakeLedger(t, repo.renewLedger, 9, "record", LedgerTypeGrant, LedgerSourcePurchase)
	if granted.Amount != Decimal("2500.00") {
		t.Fatalf("unexpected purchase ledger: %+v", granted)
	}
}

func TestActivateOrderRenewRequiresSubscription(t *testing.T) {
	repo := &fakeRepository{
		orders:               map[int64]Order{1: renewalOrderFixture(9)},
		subscriptions:        map[int64]fakeSubscription{},
		entitlementsByTenant: map[int64][]Entitlement{},
		adminName:            "运营小张",
	}
	service := NewService(repo)
	service.now = func() time.Time { return time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC) }

	_, err := service.ActivateOrderWithMode(context.Background(), 1, 7, ActivateModeRenew)
	if !errors.Is(err, ErrNoSubscription) {
		t.Fatalf("expected ErrNoSubscription, got %v", err)
	}
	order := repo.orders[1]
	if order.ActivatedAt != nil {
		t.Fatal("failed renewal must not activate the order")
	}
}

func TestActivateOrderGrantModeKeepsOldBehaviour(t *testing.T) {
	service, repo := newRenewTestService(9,
		fakeSubscription{Status: "active", Period: serviceTestPeriod("2026-11-10", "2027-11-09")},
		renewalRow(renewTestProduct, "record", QuotaTypeOfficial, "2500", "100", "0", "2400", entitlementStatusActive),
	)

	result, err := service.ActivateOrder(context.Background(), 1, 7)
	if err != nil {
		t.Fatalf("grant activation returned error: %v", err)
	}
	if repo.activateCalls != 1 || repo.renewCalls != 0 {
		t.Fatalf("grant mode must not renew, activate=%d renew=%d", repo.activateCalls, repo.renewCalls)
	}
	if len(repo.renewLedger) != 0 {
		t.Fatalf("grant mode must not write rollover ledger: %+v", repo.renewLedger)
	}
	// mode=grant 不延期：fake 里订阅保持原样，额度仍是累加语义（由 ActivateOrder 负责）。
	if subscription := repo.subscriptions[9]; subscription.Period.ExpiredOn == nil || *subscription.Period.ExpiredOn != "2027-11-09" {
		t.Fatalf("grant mode must not extend the service period: %+v", subscription.Period)
	}
	if result.ServicePeriod == nil || result.ServicePeriod.ExpiredOn == nil || *result.ServicePeriod.ExpiredOn != "2027-11-09" {
		t.Fatalf("unexpected service period: %+v", result.ServicePeriod)
	}
}

func TestActivateOrderRejectsUnknownMode(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	_, err := service.ActivateOrderWithMode(context.Background(), 1, 7, "postpone")
	assertValidationError(t, err)
	if repo.activateCalls != 0 || repo.renewCalls != 0 {
		t.Fatalf("invalid mode must not reach store: %+v", repo)
	}
}

// ---------- E2 handler ----------

func TestActivateRenewHandlerMapsNoSubscription(t *testing.T) {
	repo := &fakeRepository{
		orders:               map[int64]Order{1: renewalOrderFixture(9)},
		subscriptions:        map[int64]fakeSubscription{},
		entitlementsByTenant: map[int64][]Entitlement{},
	}
	mux := newOrderTestMux(repo)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	// query 传 mode。
	response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/1/activate?mode=renew", adminToken, "")
	if response.Code != http.StatusConflict {
		t.Fatalf("renew without subscription status = %d, body=%s", response.Code, response.Body.String())
	}
	if code := decodeErrorCode(t, response); code != "NO_SUBSCRIPTION" {
		t.Fatalf("code = %q, want NO_SUBSCRIPTION", code)
	}

	// body 传 mode 也必须生效。
	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/1/activate", adminToken, `{"mode":"renew"}`)
	if response.Code != http.StatusConflict || decodeErrorCode(t, response) != "NO_SUBSCRIPTION" {
		t.Fatalf("body mode=renew should be 409 NO_SUBSCRIPTION, got %d body=%s", response.Code, response.Body.String())
	}

	// 未知模式 400，且不触发任何开通。
	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/1/activate", adminToken, `{"mode":"later"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid mode status = %d, body=%s", response.Code, response.Body.String())
	}
	if repo.renewCalls != 2 || repo.activateCalls != 0 {
		t.Fatalf("only the two valid-mode calls may reach store: renew=%d activate=%d", repo.renewCalls, repo.activateCalls)
	}
}

func TestActivateOrderRenewAddsRowsForNewUnits(t *testing.T) {
	service, repo := newRenewTestService(9,
		fakeSubscription{Status: "active", Period: serviceTestPeriod("2026-11-10", "2027-11-09")},
		// 旧周期只剩 hour 且已用完；续费订单只买 record（新单位）。
		renewalRow(renewTestProduct, "hour", QuotaTypeOfficial, "400", "400", "0", "0", entitlementStatusActive),
	)

	result, err := service.ActivateOrderWithMode(context.Background(), 1, 7, ActivateModeRenew)
	if err != nil {
		t.Fatalf("renew activation returned error: %v", err)
	}
	record := findEntitlement(t, result.Entitlements, "record", QuotaTypeOfficial)
	if record.Granted != Decimal("2500.00") || record.RolloverBal != Decimal("0.00") ||
		record.Balance != Decimal("2500.00") || record.Status != entitlementStatusActive {
		t.Fatalf("new unit row must be inserted with the order grant, got %+v", record)
	}
	if record.PeriodEnd != "2028-11-08" {
		t.Fatalf("new row period_end = %q, want 2028-11-08", record.PeriodEnd)
	}
	hour := findEntitlement(t, result.Entitlements, "hour", QuotaTypeOfficial)
	if hour.Balance != Decimal("0.00") || hour.Status != entitlementStatusPaused {
		t.Fatalf("un-renewed exhausted unit must be paused, got %+v", hour)
	}
	if repo.renewCalls != 1 {
		t.Fatalf("renew calls = %d, want 1", repo.renewCalls)
	}
}

func TestPlanRenewalMarksRowsNewToTheTenant(t *testing.T) {
	today := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	plan, err := planRenewal(
		serviceTestPeriod("2026-11-10", "2027-11-09"),
		today,
		nil, // 租户还没有任何额度行
		[]GrantLine{renewalGrant(renewTestProduct, "record", "2500")},
		LedgerSourcePurchase,
		"续费下发",
	)
	if err != nil {
		t.Fatalf("planRenewal returned error: %v", err)
	}
	if len(plan.Updates) != 1 {
		t.Fatalf("expected 1 new row, got %+v", plan.Updates)
	}
	update := plan.Updates[0]
	if update.Existing {
		t.Fatal("row without a previous entitlement must be marked as new")
	}
	if update.Balance != Decimal("2500.00") || update.RolloverBal != Decimal("0.00") ||
		update.Granted != Decimal("2500.00") || update.Status != entitlementStatusActive {
		t.Fatalf("unexpected new row: %+v", update)
	}
}

func TestPlanRenewalToleratesMissingStartedOn(t *testing.T) {
	today := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)

	// 未过期且 started_on 为 NULL：保持 NULL，只顺延 expired_on。
	plan, err := planRenewal(
		ServicePeriod{StartedOn: nil, ExpiredOn: stringPointer("2027-11-09")},
		today,
		nil,
		nil,
		LedgerSourcePurchase,
		"续费下发",
	)
	if err != nil {
		t.Fatalf("planRenewal returned error: %v", err)
	}
	if plan.Period.StartedOn != nil {
		t.Fatalf("missing started_on should stay empty, got %v", *plan.Period.StartedOn)
	}
	if plan.Period.ExpiredOn == nil || *plan.Period.ExpiredOn != "2028-11-08" {
		t.Fatalf("expired_on = %v, want 2028-11-08", plan.Period.ExpiredOn)
	}

	// 已过期且 started_on 为 NULL：从今天重新起算。
	plan, err = planRenewal(
		ServicePeriod{StartedOn: nil, ExpiredOn: stringPointer("2026-11-01")},
		today,
		nil,
		nil,
		LedgerSourcePurchase,
		"续费下发",
	)
	if err != nil {
		t.Fatalf("planRenewal returned error: %v", err)
	}
	if plan.Period.StartedOn == nil || *plan.Period.StartedOn != "2026-11-10" ||
		plan.Period.ExpiredOn == nil || *plan.Period.ExpiredOn != "2027-11-09" {
		t.Fatalf("unexpected restarted period: %+v", plan.Period)
	}
}
