package billing

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ---------- E3 到期清零的流水与任务 ----------

func TestPlanExpiryLedgerWritesOneRowPerEntitlement(t *testing.T) {
	rows := []Entitlement{
		renewalRow(renewTestProduct, "record", QuotaTypeOfficial, "2500", "2379.5", "0", "120.5", entitlementStatusActive),
		renewalRow(renewTestProduct, "round", QuotaTypeOfficial, "5000", "5000", "0", "0", entitlementStatusActive),
		// 超扣成负数的行也要冲销：amount = -old_balance = +3.00。
		renewalRow(renewTestProduct, "hour", QuotaTypeOfficial, "400", "403", "0", "-3", entitlementStatusPaused),
	}
	entries, err := planExpiryLedger(rows)
	if err != nil {
		t.Fatalf("planExpiryLedger returned error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected one ledger row per entitlement, got %+v", entries)
	}
	amounts := make(map[string]Decimal, len(entries))
	for _, entry := range entries {
		amounts[entry.Unit] = entry.Amount
	}
	if amounts["record"] != Decimal("-120.50") {
		t.Fatalf("record amount = %s, want -120.50", amounts["record"])
	}
	if amounts["round"] != Decimal("0.00") {
		t.Fatalf("zero balance should still write a 0 amount row, got %s", amounts["round"])
	}
	if amounts["hour"] != Decimal("3.00") {
		t.Fatalf("negative balance should be reversed to +3.00, got %s", amounts["hour"])
	}
}

func TestExpireDueSubscriptionsClearsOnlyExpiredTenants(t *testing.T) {
	repo := &fakeRepository{
		subscriptions: map[int64]fakeSubscription{
			// 到期未续：expired_on 是昨天。
			1: {Status: "active", Period: serviceTestPeriod("2025-11-10", "2026-11-09")},
			// 刚续费的租户：到期日在未来。
			2: {Status: "active", Period: serviceTestPeriod("2026-11-10", "2027-11-09")},
			// 今天到期（expired_on = today）按 SQL 条件不算过期，留给最后一天。
			3: {Status: "active", Period: serviceTestPeriod("2025-11-10", "2026-11-10")},
		},
		entitlementsByTenant: map[int64][]Entitlement{
			1: {
				renewalRow(renewTestProduct, "record", QuotaTypeOfficial, "2500", "2380", "0", "120", entitlementStatusActive),
				renewalRow(renewTestProduct, "hour", QuotaTypeOfficial, "400", "400", "0", "0", entitlementStatusActive),
			},
			2: {renewalRow(renewTestProduct, "record", QuotaTypeOfficial, "2500", "2000", "0", "500", entitlementStatusActive)},
			3: {renewalRow(renewTestProduct, "record", QuotaTypeOfficial, "2500", "100", "0", "2400", entitlementStatusActive)},
		},
	}
	service := NewService(repo)
	service.now = func() time.Time { return time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC) }

	count, err := service.ExpireDueSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("ExpireDueSubscriptions returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("processed tenants = %d, want 1", count)
	}
	if len(repo.expireCalls) != 1 || repo.expireCalls[0] != 1 {
		t.Fatalf("expire calls = %v, want [1]", repo.expireCalls)
	}
	if repo.subscriptions[1].Status != "expired" {
		t.Fatalf("tenant 1 subscription status = %q, want expired", repo.subscriptions[1].Status)
	}
	for _, row := range repo.entitlementsByTenant[1] {
		if row.Granted != Decimal("0.00") || row.Used != Decimal("0.00") ||
			row.RolloverBal != Decimal("0.00") || row.Balance != Decimal("0.00") ||
			row.Status != entitlementStatusCleared {
			t.Fatalf("tenant 1 entitlement not cleared: %+v", row)
		}
	}
	if repo.subscriptions[2].Status != "active" || repo.subscriptions[3].Status != "active" {
		t.Fatalf("renewed / not-yet-expired tenants must stay active: %+v", repo.subscriptions)
	}
	if len(repo.entitlementsByTenant[2]) != 1 || repo.entitlementsByTenant[2][0].Balance != Decimal("500") {
		t.Fatalf("renewed tenant entitlements must be untouched: %+v", repo.entitlementsByTenant[2])
	}
	if repo.entitlementsByTenant[3][0].Balance != Decimal("2400") {
		t.Fatalf("tenant expiring today must not be cleared: %+v", repo.entitlementsByTenant[3])
	}
	if len(repo.expireLedger) != 2 {
		t.Fatalf("expected 2 clearing ledger rows for tenant 1, got %+v", repo.expireLedger)
	}
	record := findFakeLedger(t, repo.expireLedger, 1, "record", LedgerTypeUsage, ledgerSourceExpired)
	if record.Amount != Decimal("-120.00") {
		t.Fatalf("record ledger = %s, want -120.00 (=-old balance)", record.Amount)
	}
	if record.Reason != reasonSubscriptionExpired {
		t.Fatalf("reason = %q, want %q", record.Reason, reasonSubscriptionExpired)
	}
	hour := findFakeLedger(t, repo.expireLedger, 1, "hour", LedgerTypeUsage, ledgerSourceExpired)
	if hour.Amount != Decimal("0.00") {
		t.Fatalf("hour ledger = %s, want 0.00", hour.Amount)
	}
}

func TestExpireDueSubscriptionsSkipsRenewedTenant(t *testing.T) {
	service, repo := newRenewTestService(9,
		// 脱期 9 天：窗口内续费，服务期会被延长到明年。
		fakeSubscription{Status: "active", Period: serviceTestPeriod("2025-11-10", "2026-11-01")},
		renewalRow(renewTestProduct, "record", QuotaTypeOfficial, "2500", "2350", "50", "200", entitlementStatusActive),
	)

	result, err := service.ActivateOrderWithMode(context.Background(), 1, 7, ActivateModeRenew)
	if err != nil {
		t.Fatalf("renew activation returned error: %v", err)
	}
	if result.ServicePeriod == nil || result.ServicePeriod.ExpiredOn == nil || *result.ServicePeriod.ExpiredOn != "2027-11-09" {
		t.Fatalf("unexpected renewal period: %+v", result.ServicePeriod)
	}

	count, err := service.ExpireDueSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("ExpireDueSubscriptions returned error: %v", err)
	}
	if count != 0 {
		t.Fatalf("renewed tenant must not be expired, processed = %d", count)
	}
	if len(repo.expireCalls) != 0 {
		t.Fatalf("renewed tenant must not be cleared, calls = %v", repo.expireCalls)
	}
	if repo.subscriptions[9].Status != "active" {
		t.Fatalf("subscription status = %q, want active", repo.subscriptions[9].Status)
	}
	row := repo.entitlementsByTenant[9][0]
	if row.Balance != Decimal("2650.00") || row.Status != entitlementStatusActive {
		t.Fatalf("renewed entitlements must survive the expiry task: %+v", row)
	}
	for _, entry := range repo.renewLedger {
		if entry.Reason == reasonSubscriptionExpired {
			t.Fatalf("renewal must not produce expiry clearing ledger: %+v", entry)
		}
	}
}

func TestExpireDueSubscriptionsReturnsListError(t *testing.T) {
	service := NewService(&fakeRepository{expireErr: errors.New("db down")})
	service.now = func() time.Time { return time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC) }

	count, err := service.ExpireDueSubscriptions(context.Background())
	if err == nil {
		t.Fatal("expected an error when listing expiring tenants fails")
	}
	if count != 0 {
		t.Fatalf("processed = %d, want 0", count)
	}
}

func TestStartExpiryWorkerRunsOnceImmediately(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立刻取消：验证"启动先跑一次"后 ticker 循环退出。

	// StartExpiryWorker 是阻塞的；能返回说明已响应 ctx.Done。
	service.StartExpiryWorker(ctx, time.Hour)
	if repo.expireListCalls != 1 {
		t.Fatalf("startup run should scan expiring tenants exactly once, calls=%d", repo.expireListCalls)
	}
}
