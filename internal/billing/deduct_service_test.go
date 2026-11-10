package billing

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"testing"
)

// ---------- fakeRepository / fakeDeductTx 的阶段 D 实现 ----------

// fakeDeductTx 是 DeductTx 的内存实现：在事务副本上改额度，Commit 时才写回 repo，
// 语义与真实 store 对齐（锁行 → 幂等闸门 → 更新 → 用 rollover+granted-used 重算）。
type fakeDeductTx struct {
	repo          *fakeRepository
	rows          []Entitlement
	pendingLedger []DeductLedgerEntry
	addedKeys     []string
	committed     bool
}

func (f *fakeRepository) BeginDeduct(_ context.Context) (DeductTx, error) {
	f.beginDeductCalls++
	if f.deductKeys == nil {
		f.deductKeys = make(map[string]struct{})
	}
	return &fakeDeductTx{repo: f, rows: cloneEntitlements(f.deductRows)}, nil
}

func (tx *fakeDeductTx) LockEntitlements(_ context.Context, tenantID int64, productCode string, units []string) ([]Entitlement, error) {
	if tx.repo.deductTenantID != 0 && tenantID != tx.repo.deductTenantID {
		return nil, nil
	}
	unitSet := make(map[string]struct{}, len(units))
	for _, unit := range units {
		unitSet[unit] = struct{}{}
	}
	items := make([]Entitlement, 0, len(units))
	for _, row := range tx.rows {
		if row.ProductCode != productCode {
			continue
		}
		if _, exists := unitSet[row.Unit]; !exists {
			continue
		}
		items = append(items, row)
	}
	sortEntitlements(items)
	return items, nil
}

func (tx *fakeDeductTx) IdempotencyKeyExists(_ context.Context, tenantID int64, idempotencyKey string) (bool, error) {
	if tx.repo.deductTenantID != 0 && tenantID != tx.repo.deductTenantID {
		return false, nil
	}
	if _, exists := tx.repo.deductKeys[idempotencyKey]; exists {
		return true, nil
	}
	for _, key := range tx.addedKeys {
		if key == idempotencyKey {
			return true, nil
		}
	}
	return false, nil
}

func (tx *fakeDeductTx) InsertUsageLedger(_ context.Context, entry DeductLedgerEntry) error {
	if entry.IdempotencyKey != nil {
		key := strings.TrimSpace(*entry.IdempotencyKey)
		if _, exists := tx.repo.deductKeys[key]; exists {
			return ErrAlreadyDeducted
		}
		for _, added := range tx.addedKeys {
			if added == key {
				return ErrAlreadyDeducted
			}
		}
		tx.addedKeys = append(tx.addedKeys, key)
	}
	tx.pendingLedger = append(tx.pendingLedger, entry)
	return nil
}

func (tx *fakeDeductTx) ApplyEntitlementChanges(_ context.Context, _ int64, productCode string, changes []DeductChange) error {
	tx.repo.applyCalls++
	for _, change := range changes {
		for index := range tx.rows {
			row := &tx.rows[index]
			if row.ProductCode != productCode || row.Unit != change.Unit ||
				row.Edition != change.Edition || row.QuotaType != change.QuotaType {
				continue
			}
			row.RolloverBal = change.RolloverBal
			row.Used = change.Used
			row.Status = change.Status
		}
	}
	return nil
}

func (tx *fakeDeductTx) RecomputeAndList(_ context.Context, _ int64, productCode string, _ []string) ([]Entitlement, error) {
	tx.repo.recomputeCalls++
	for index := range tx.rows {
		row := &tx.rows[index]
		if row.ProductCode != productCode {
			continue
		}
		granted, err := parseEntitlementCents("granted", row.Granted)
		if err != nil {
			return nil, err
		}
		used, err := parseEntitlementCents("used", row.Used)
		if err != nil {
			return nil, err
		}
		rollover, err := parseEntitlementCents("rollover_bal", row.RolloverBal)
		if err != nil {
			return nil, err
		}
		row.Balance = Decimal(centsToDecimal(granted + rollover - used))
	}
	items := make([]Entitlement, 0, len(tx.rows))
	for _, row := range tx.rows {
		if row.ProductCode == productCode {
			items = append(items, row)
		}
	}
	sortEntitlements(items)
	return items, nil
}

func (tx *fakeDeductTx) Commit(_ context.Context) error {
	if tx.committed {
		return nil
	}
	tx.committed = true
	tx.repo.commitCalls++
	tx.repo.deductRows = cloneEntitlements(tx.rows)
	if tx.repo.deductKeys == nil {
		tx.repo.deductKeys = make(map[string]struct{})
	}
	for _, key := range tx.addedKeys {
		tx.repo.deductKeys[key] = struct{}{}
	}
	tx.repo.usageLedger = append(tx.repo.usageLedger, tx.pendingLedger...)
	return nil
}

func (tx *fakeDeductTx) Rollback(_ context.Context) error {
	if tx.committed {
		return nil
	}
	tx.repo.rollbackCalls++
	return nil
}

func cloneEntitlements(rows []Entitlement) []Entitlement {
	return append([]Entitlement(nil), rows...)
}

// sortEntitlements 复刻 store 侧 ORDER BY unit, quota_type, edition。
func sortEntitlements(rows []Entitlement) {
	sort.SliceStable(rows, func(left, right int) bool {
		if rows[left].Unit != rows[right].Unit {
			return rows[left].Unit < rows[right].Unit
		}
		if rows[left].QuotaType != rows[right].QuotaType {
			return rows[left].QuotaType < rows[right].QuotaType
		}
		return rows[left].Edition < rows[right].Edition
	})
}

// ---------- 测试辅助 ----------

const deductProduct = "yingke"

func entitlementFixture(unit, quotaType, edition, granted, used, rolloverBal, balance, status string) Entitlement {
	return Entitlement{
		ProductCode: deductProduct,
		Unit:        unit,
		Edition:     edition,
		QuotaType:   quotaType,
		Granted:     Decimal(granted),
		Used:        Decimal(used),
		RolloverBal: Decimal(rolloverBal),
		Balance:     Decimal(balance),
		PeriodEnd:   "2027-11-09",
		Status:      status,
	}
}

func officialRow(unit, granted, used, rolloverBal, balance string) Entitlement {
	return entitlementFixture(unit, QuotaTypeOfficial, EditionStandard, granted, used, rolloverBal, balance, entitlementStatusActive)
}

func trialRow(unit, granted, used, balance string) Entitlement {
	return entitlementFixture(unit, QuotaTypeTrial, EditionTrial, granted, used, "0", balance, entitlementStatusActive)
}

func newDeductTestService(tenantID int64, rows ...Entitlement) (*Service, *fakeRepository) {
	repo := &fakeRepository{
		deductTenantID: tenantID,
		deductRows:     rows,
		deductKeys:     make(map[string]struct{}),
	}
	return NewService(repo), repo
}

func deductInput(tenantID int64, key string, items ...DeductItem) DeductInput {
	return DeductInput{
		TenantID:       tenantID,
		ProductCode:    deductProduct,
		Items:          items,
		IdempotencyKey: key,
		Reason:         "交付扣量：测试",
	}
}

func findEntitlement(t *testing.T, balances []Entitlement, unit, quotaType string) Entitlement {
	t.Helper()
	for _, balance := range balances {
		if balance.Unit == unit && balance.QuotaType == quotaType {
			return balance
		}
	}
	t.Fatalf("entitlement %s/%s not found in %+v", unit, quotaType, balances)
	return Entitlement{}
}

func assertBalanceInvariant(t *testing.T, balance Entitlement) {
	t.Helper()
	granted, err := parseEntitlementCents("granted", balance.Granted)
	if err != nil {
		t.Fatalf("parse granted: %v", err)
	}
	used, err := parseEntitlementCents("used", balance.Used)
	if err != nil {
		t.Fatalf("parse used: %v", err)
	}
	rollover, err := parseEntitlementCents("rollover_bal", balance.RolloverBal)
	if err != nil {
		t.Fatalf("parse rollover_bal: %v", err)
	}
	actual, err := parseEntitlementCents("balance", balance.Balance)
	if err != nil {
		t.Fatalf("parse balance: %v", err)
	}
	if actual != rollover+granted-used {
		t.Fatalf("balance invariant broken: balance=%s rollover=%s granted=%s used=%s",
			balance.Balance, balance.RolloverBal, balance.Granted, balance.Used)
	}
}

// ---------- D1 扣量 ----------

func TestDeductFromOfficialOnly(t *testing.T) {
	service, repo := newDeductTestService(9, officialRow("hour", "400", "0", "0", "400"))

	result, err := service.Deduct(context.Background(), deductInput(9, "deal:recording:123:analysis",
		DeductItem{Unit: " HOUR ", Amount: 12.5}))
	if err != nil {
		t.Fatalf("Deduct returned error: %v", err)
	}
	if len(result.Lines) != 1 {
		t.Fatalf("expected 1 line, got %+v", result.Lines)
	}
	line := result.Lines[0]
	if line.Unit != "hour" || line.Amount != 12.5 || line.Rollover != 0 || line.Trial != 0 || line.Official != 12.5 {
		t.Fatalf("unexpected line: %+v", line)
	}
	if result.Paused {
		t.Fatal("balance left 387.5, paused must be false")
	}
	if len(result.Balances) != 1 {
		t.Fatalf("expected 1 balance, got %+v", result.Balances)
	}
	balance := result.Balances[0]
	if balance.Balance != Decimal("387.50") || balance.Used != Decimal("12.50") || balance.Status != entitlementStatusActive {
		t.Fatalf("unexpected balance: %+v", balance)
	}
	assertBalanceInvariant(t, balance)

	if len(repo.usageLedger) != 1 {
		t.Fatalf("expected 1 usage ledger row, got %+v", repo.usageLedger)
	}
	entry := repo.usageLedger[0]
	if entry.Source != ledgerSourceOfficial || entry.Amount != Decimal("-12.50") {
		t.Fatalf("unexpected ledger entry: %+v", entry)
	}
	if entry.Reason != "交付扣量：测试" || entry.Operator != deductOperator {
		t.Fatalf("unexpected ledger metadata: %+v", entry)
	}
	if entry.IdempotencyKey == nil || *entry.IdempotencyKey != "deal:recording:123:analysis" {
		t.Fatalf("first usage ledger row must carry idempotency key, got %+v", entry.IdempotencyKey)
	}
	if repo.commitCalls != 1 || repo.applyCalls != 1 || repo.recomputeCalls != 1 {
		t.Fatalf("unexpected transaction calls: commit=%d apply=%d recompute=%d",
			repo.commitCalls, repo.applyCalls, repo.recomputeCalls)
	}
}

func TestDeductCrossesRolloverThenTrialThenOfficial(t *testing.T) {
	service, repo := newDeductTestService(9,
		officialRow("hour", "100", "0", "5", "105"),
		trialRow("hour", "3", "0", "3"),
	)

	result, err := service.Deduct(context.Background(), deductInput(9, "deal:recording:456:analysis",
		DeductItem{Unit: "hour", Amount: 70}))
	if err != nil {
		t.Fatalf("Deduct returned error: %v", err)
	}
	line := result.Lines[0]
	if line.Rollover != 5 || line.Trial != 3 || line.Official != 62 {
		t.Fatalf("cross-source split = rollover %.2f trial %.2f official %.2f, want 5/3/62",
			line.Rollover, line.Trial, line.Official)
	}
	if line.Rollover+line.Trial+line.Official != line.Amount {
		t.Fatalf("split does not add up to amount: %+v", line)
	}
	if result.Paused {
		t.Fatal("only 38 left on official, unit is not exhausted")
	}

	official := findEntitlement(t, result.Balances, "hour", QuotaTypeOfficial)
	if official.RolloverBal != Decimal("0.00") || official.Used != Decimal("62.00") || official.Balance != Decimal("38.00") {
		t.Fatalf("unexpected official row: %+v", official)
	}
	assertBalanceInvariant(t, official)
	trial := findEntitlement(t, result.Balances, "hour", QuotaTypeTrial)
	if trial.Used != Decimal("3.00") || trial.Balance != Decimal("0.00") {
		t.Fatalf("unexpected trial row: %+v", trial)
	}
	assertBalanceInvariant(t, trial)

	if len(repo.usageLedger) != 3 {
		t.Fatalf("expected 3 usage ledger rows, got %+v", repo.usageLedger)
	}
	wantSources := []string{ledgerSourceRollover, ledgerSourceTrial, ledgerSourceOfficial}
	wantAmounts := []Decimal{"-5.00", "-3.00", "-62.00"}
	for index, entry := range repo.usageLedger {
		if entry.Source != wantSources[index] || entry.Amount != wantAmounts[index] {
			t.Fatalf("ledger[%d] = source %s amount %s, want %s/%s",
				index, entry.Source, entry.Amount, wantSources[index], wantAmounts[index])
		}
		if index == 0 {
			if entry.IdempotencyKey == nil {
				t.Fatal("first usage ledger row must carry the idempotency key")
			}
		} else if entry.IdempotencyKey != nil {
			t.Fatalf("ledger[%d] must have NULL idempotency key, got %q", index, *entry.IdempotencyKey)
		}
	}
}

func TestDeductAllowsNegativeBalanceAndPauses(t *testing.T) {
	service, repo := newDeductTestService(9, officialRow("hour", "5", "0", "0", "5"))

	result, err := service.Deduct(context.Background(), deductInput(9, "deal:recording:789:analysis",
		DeductItem{Unit: "hour", Amount: 12.5}))
	if err != nil {
		t.Fatalf("Deduct returned error: %v", err)
	}
	if !result.Paused {
		t.Fatal("balance went to -7.5, paused must be true")
	}
	line := result.Lines[0]
	if line.Official != 12.5 || line.Amount != 12.5 {
		t.Fatalf("unexpected line: %+v", line)
	}
	balance := result.Balances[0]
	if balance.Balance != Decimal("-7.50") || balance.Used != Decimal("12.50") || balance.Status != entitlementStatusPaused {
		t.Fatalf("unexpected paused balance: %+v", balance)
	}
	assertBalanceInvariant(t, balance)

	if len(repo.usageLedger) != 2 {
		t.Fatalf("expected an in-quota row plus an overdraft row, got %+v", repo.usageLedger)
	}
	if repo.usageLedger[0].Amount != Decimal("-5.00") || strings.Contains(repo.usageLedger[0].Reason, "超出额度") {
		t.Fatalf("in-quota row should not be marked overdraft: %+v", repo.usageLedger[0])
	}
	overdraft := repo.usageLedger[1]
	if overdraft.Amount != Decimal("-7.50") || overdraft.Source != ledgerSourceOfficial || !strings.Contains(overdraft.Reason, "超出额度") {
		t.Fatalf("overdraft row not annotated: %+v", overdraft)
	}
}

func TestDeductMarksPausedOnlyForExhaustedUnit(t *testing.T) {
	service, _ := newDeductTestService(9,
		officialRow("hour", "5", "0", "0", "5"),
		officialRow("record", "100", "0", "0", "100"),
	)

	result, err := service.Deduct(context.Background(), deductInput(9, "emr:record:1:draft",
		DeductItem{Unit: "hour", Amount: 6},
		DeductItem{Unit: "record", Amount: 1},
	))
	if err != nil {
		t.Fatalf("Deduct returned error: %v", err)
	}
	if !result.Paused {
		t.Fatal("hour unit is exhausted, paused must be true")
	}
	hour := findEntitlement(t, result.Balances, "hour", QuotaTypeOfficial)
	if hour.Status != entitlementStatusPaused || hour.Balance != Decimal("-1.00") {
		t.Fatalf("hour row should be paused: %+v", hour)
	}
	record := findEntitlement(t, result.Balances, "record", QuotaTypeOfficial)
	if record.Status != entitlementStatusActive || record.Balance != Decimal("99.00") {
		t.Fatalf("record row should stay active: %+v", record)
	}
}

func TestDeductIsIdempotentByKey(t *testing.T) {
	service, repo := newDeductTestService(9, officialRow("hour", "400", "0", "0", "400"))
	input := deductInput(9, "deal:recording:123:analysis", DeductItem{Unit: "hour", Amount: 10})

	if _, err := service.Deduct(context.Background(), input); err != nil {
		t.Fatalf("first Deduct returned error: %v", err)
	}
	balanceAfterFirst := repo.deductRows[0].Balance
	usedAfterFirst := repo.deductRows[0].Used
	ledgerAfterFirst := len(repo.usageLedger)

	_, err := service.Deduct(context.Background(), input)
	if !errors.Is(err, ErrAlreadyDeducted) {
		t.Fatalf("second Deduct should return ErrAlreadyDeducted, got %v", err)
	}
	if repo.commitCalls != 1 {
		t.Fatalf("duplicate call must not commit, commits=%d", repo.commitCalls)
	}
	if len(repo.usageLedger) != ledgerAfterFirst {
		t.Fatalf("duplicate call wrote ledger rows: %d -> %d", ledgerAfterFirst, len(repo.usageLedger))
	}
	if repo.deductRows[0].Balance != balanceAfterFirst || repo.deductRows[0].Used != usedAfterFirst {
		t.Fatalf("duplicate call changed balance: %+v", repo.deductRows[0])
	}
}

// 重试"把余额一次扣完"的交付时，幂等优先于额度校验：必须返回 ALREADY_DEDUCTED，
// 而不是因为余额已经是 0 返回 INSUFFICIENT_ENTITLEMENT。
func TestDeductIdempotencyTakesPrecedenceOverExhaustedQuota(t *testing.T) {
	service, repo := newDeductTestService(9, officialRow("hour", "12.5", "0", "0", "12.5"))
	input := deductInput(9, "deal:recording:999:analysis", DeductItem{Unit: "hour", Amount: 12.5})

	first, err := service.Deduct(context.Background(), input)
	if err != nil {
		t.Fatalf("first Deduct returned error: %v", err)
	}
	if !first.Paused || first.Balances[0].Balance != Decimal("0.00") {
		t.Fatalf("first deduct should exhaust the quota: %+v", first)
	}

	_, err = service.Deduct(context.Background(), input)
	if !errors.Is(err, ErrAlreadyDeducted) {
		t.Fatalf("retry after exhaustion should be ALREADY_DEDUCTED, got %v", err)
	}
	if repo.commitCalls != 1 {
		t.Fatalf("retry must not commit, commits=%d", repo.commitCalls)
	}
	if repo.deductRows[0].Balance != Decimal("0.00") {
		t.Fatalf("retry changed balance: %+v", repo.deductRows[0])
	}
}

func TestDeductReturnsInsufficientWhenUnitHasNoUsableQuota(t *testing.T) {
	service, repo := newDeductTestService(9, officialRow("hour", "0", "0", "0", "0"))

	_, err := service.Deduct(context.Background(), deductInput(9, "deal:recording:1:analysis",
		DeductItem{Unit: "hour", Amount: 1}))
	if !errors.Is(err, ErrInsufficientEntitlement) {
		t.Fatalf("expected ErrInsufficientEntitlement, got %v", err)
	}
	var insufficient *InsufficientEntitlementError
	if !errors.As(err, &insufficient) {
		t.Fatalf("expected *InsufficientEntitlementError, got %T", err)
	}
	if insufficient.Unit != "hour" || insufficient.Current != 0 || insufficient.Required != 1 || insufficient.Shortfall != 1 {
		t.Fatalf("unexpected insufficient details: %+v", insufficient)
	}
	if repo.commitCalls != 0 || len(repo.usageLedger) != 0 {
		t.Fatalf("insufficient deduct must not write anything: commit=%d ledger=%d", repo.commitCalls, len(repo.usageLedger))
	}
	if repo.rollbackCalls != 1 {
		t.Fatalf("transaction should be rolled back exactly once, rollbacks=%d", repo.rollbackCalls)
	}
}

func TestDeductValidatesInputWithoutStartingTransaction(t *testing.T) {
	cases := []struct {
		name  string
		input DeductInput
	}{
		{"missing tenant", deductInput(0, "emr:record:1:draft", DeductItem{Unit: "hour", Amount: 1})},
		{"missing product", func() DeductInput {
			input := deductInput(9, "emr:record:1:draft", DeductItem{Unit: "hour", Amount: 1})
			input.ProductCode = " "
			return input
		}()},
		{"empty items", deductInput(9, "emr:record:1:draft")},
		{"unsupported unit", deductInput(9, "emr:record:1:draft", DeductItem{Unit: "minute", Amount: 1})},
		{"zero amount", deductInput(9, "emr:record:1:draft", DeductItem{Unit: "hour", Amount: 0})},
		{"negative amount", deductInput(9, "emr:record:1:draft", DeductItem{Unit: "hour", Amount: -1})},
		{"NaN amount", deductInput(9, "emr:record:1:draft", DeductItem{Unit: "hour", Amount: math.NaN()})},
		{"duplicate unit", deductInput(9, "emr:record:1:draft",
			DeductItem{Unit: "hour", Amount: 1}, DeductItem{Unit: " HOUR ", Amount: 2})},
		{"missing idempotency key", deductInput(9, "", DeductItem{Unit: "hour", Amount: 1})},
		{"malformed idempotency key", deductInput(9, "deal-recording-123", DeductItem{Unit: "hour", Amount: 1})},
		{"idempotency key with empty segment", deductInput(9, "deal::123:analysis", DeductItem{Unit: "hour", Amount: 1})},
		{"idempotency key too long", deductInput(9, strings.Repeat("x", maxDeductKeyLength+1), DeductItem{Unit: "hour", Amount: 1})},
		{"reason too long", func() DeductInput {
			input := deductInput(9, "emr:record:1:draft", DeductItem{Unit: "hour", Amount: 1})
			input.Reason = strings.Repeat("原", maxDeductReasonLength+1)
			return input
		}()},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, repo := newDeductTestService(9, officialRow("hour", "400", "0", "0", "400"))
			_, err := service.Deduct(context.Background(), test.input)
			assertValidationError(t, err)
			if repo.beginDeductCalls != 0 {
				t.Fatalf("invalid input must not start a transaction, calls=%d", repo.beginDeductCalls)
			}
		})
	}
}

func TestDeductRoundsAmountsToCents(t *testing.T) {
	service, repo := newDeductTestService(9, officialRow("hour", "400", "0", "0", "400"))

	result, err := service.Deduct(context.Background(), deductInput(9, "deal:recording:1:analysis",
		DeductItem{Unit: "hour", Amount: 0.1}, DeductItem{Unit: "record", Amount: 0.2}))
	if err != nil {
		// record 没有额度时应当报不足；这里只验证 hour 的 0.1 已按分精度入账。
		var insufficient *InsufficientEntitlementError
		if !errors.As(err, &insufficient) || insufficient.Unit != "record" {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	_ = result
	if len(repo.usageLedger) != 0 {
		t.Fatalf("整个事务应回滚，不应留下流水: %+v", repo.usageLedger)
	}

	service, repo = newDeductTestService(9, officialRow("hour", "400", "0", "0", "400"))
	result, err = service.Deduct(context.Background(), deductInput(9, "deal:recording:2:analysis",
		DeductItem{Unit: "hour", Amount: 0.1 + 0.2}))
	if err != nil {
		t.Fatalf("Deduct returned error: %v", err)
	}
	if result.Lines[0].Amount != 0.3 || result.Balances[0].Used != Decimal("0.30") {
		t.Fatalf("float amount not rounded to cents: line=%+v balance=%+v", result.Lines[0], result.Balances[0])
	}
}

// ---------- 额度预检 ----------

func TestCheckQuotaSufficientAndInsufficient(t *testing.T) {
	repo := &fakeRepository{entitlementItems: []Entitlement{
		officialRow("hour", "380", "0", "20", "400"),
	}}
	service := NewService(repo)

	if err := service.CheckQuota(context.Background(), 9, deductProduct,
		[]DeductItem{{Unit: "hour", Amount: 400}}); err != nil {
		t.Fatalf("CheckQuota should pass when balance covers demand: %v", err)
	}
	if repo.entitlementProduct != deductProduct || !repo.entitlementIncludeZero {
		t.Fatalf("CheckQuota must read all balance rows of the product: product=%q includeZero=%v",
			repo.entitlementProduct, repo.entitlementIncludeZero)
	}

	err := service.CheckQuota(context.Background(), 9, deductProduct,
		[]DeductItem{{Unit: "hour", Amount: 450}})
	if !errors.Is(err, ErrInsufficientEntitlement) {
		t.Fatalf("expected ErrInsufficientEntitlement, got %v", err)
	}
	var insufficient *InsufficientEntitlementError
	if !errors.As(err, &insufficient) {
		t.Fatalf("expected *InsufficientEntitlementError, got %T", err)
	}
	// 正式行的 balance 已包含结转余额，不能把 rollover 再加一次（否则会高估成 420）。
	if insufficient.Unit != "hour" || insufficient.Current != 400 || insufficient.Required != 450 || insufficient.Shortfall != 50 {
		t.Fatalf("unexpected insufficient details: %+v", insufficient)
	}
	if repo.usageLedger != nil || repo.beginDeductCalls != 0 {
		t.Fatalf("CheckQuota must be read-only: %+v", repo)
	}
}

func TestCheckQuotaIsPerUnitAndDefaultsToZero(t *testing.T) {
	repo := &fakeRepository{entitlementItems: []Entitlement{
		officialRow("hour", "400", "0", "0", "400"),
		trialRow("record", "10", "0", "10"),
	}}
	service := NewService(repo)

	// 两个单位都够。
	if err := service.CheckQuota(context.Background(), 9, deductProduct, []DeductItem{
		{Unit: "hour", Amount: 100}, {Unit: "record", Amount: 10},
	}); err != nil {
		t.Fatalf("CheckQuota returned error: %v", err)
	}

	// 第二个单位不够：报告的是 record，而不是笼统的产品级不足。
	err := service.CheckQuota(context.Background(), 9, deductProduct, []DeductItem{
		{Unit: "hour", Amount: 100}, {Unit: "record", Amount: 11},
	})
	var insufficient *InsufficientEntitlementError
	if !errors.As(err, &insufficient) || insufficient.Unit != "record" || insufficient.Shortfall != 1 {
		t.Fatalf("unexpected unit-level insufficiency: %+v (%v)", insufficient, err)
	}

	// 没有任何额度行的 unit 视为 0。
	err = service.CheckQuota(context.Background(), 9, deductProduct, []DeductItem{{Unit: "shot", Amount: 1}})
	if !errors.As(err, &insufficient) || insufficient.Unit != "shot" || insufficient.Current != 0 {
		t.Fatalf("missing unit should be reported as zero: %+v (%v)", insufficient, err)
	}
}

func TestCheckQuotaIgnoresUnusableRows(t *testing.T) {
	expired := trialRow("hour", "100", "0", "100")
	expired.Status = entitlementStatusExpired
	repo := &fakeRepository{entitlementItems: []Entitlement{
		officialRow("hour", "50", "0", "0", "50"),
		expired,
	}}
	service := NewService(repo)

	if err := service.CheckQuota(context.Background(), 9, deductProduct, []DeductItem{{Unit: "hour", Amount: 50}}); err != nil {
		t.Fatalf("CheckQuota returned error: %v", err)
	}
	err := service.CheckQuota(context.Background(), 9, deductProduct, []DeductItem{{Unit: "hour", Amount: 51}})
	var insufficient *InsufficientEntitlementError
	if !errors.As(err, &insufficient) || insufficient.Current != 50 {
		t.Fatalf("expired trial quota must not be counted: %+v (%v)", insufficient, err)
	}
}

func TestCheckQuotaValidatesInput(t *testing.T) {
	service := NewService(&fakeRepository{})
	valid := []DeductItem{{Unit: "hour", Amount: 1}}
	cases := []struct {
		name    string
		tenant  int64
		product string
		items   []DeductItem
	}{
		{"missing tenant", 0, deductProduct, valid},
		{"missing product", 9, " ", valid},
		{"empty items", 9, deductProduct, nil},
		{"invalid unit", 9, deductProduct, []DeductItem{{Unit: "minute", Amount: 1}}},
		{"zero amount", 9, deductProduct, []DeductItem{{Unit: "hour", Amount: 0}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assertValidationError(t, service.CheckQuota(context.Background(), test.tenant, test.product, test.items))
		})
	}
}
