package billing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// 扣量涉及的枚举。
//
// source 映射到 entitlement_ledger.source 的 CHECK 约束取值；正式额度统一记 purchase，
// 与阶段 C 开通发额度时的 source 保持一致。status 映射到 product_entitlements.status。
const (
	ledgerSourceRollover = "rollover"
	ledgerSourceTrial    = "trial"
	ledgerSourceOfficial = "purchase"

	entitlementStatusActive  = "active"
	entitlementStatusPaused  = "paused"
	entitlementStatusExpired = "expired"
	entitlementStatusCleared = "cleared"

	// deductOperator 是自动扣量的操作人；人工操作才写运营姓名。
	deductOperator = "系统"
	// deductDefaultReason 是调用方没给 reason 时的兜底原因，保证流水可读。
	deductDefaultReason = "交付扣量"
	// deductOverdraftSuffix 标注"这笔扣减让余额变成负数"，对账时一眼能看出超扣。
	deductOverdraftSuffix = "（超出额度）"

	maxDeductItems        = 50
	maxDeductReasonLength = 500
	// idempotency_key 落库列是 VARCHAR(128)。
	maxDeductKeyLength = 128
	// maxQuotaAmountCents 是 NUMERIC(14,2) 能承载的最大额度（999,999,999,999.99）对应的分值。
	maxQuotaAmountCents int64 = 99999999999999
)

var (
	// ErrAlreadyDeducted 表示该幂等键已经扣过量。上层据此判定为幂等成功（不重复扣）。
	ErrAlreadyDeducted = errors.New("billing: idempotency key already deducted")
	// ErrInsufficientEntitlement 表示额度不足以完成本次预检（只读）或扣量。
	ErrInsufficientEntitlement = errors.New("billing: insufficient entitlement")
)

// InsufficientEntitlementError 带上"哪个单位、现在多少、需要多少、差多少"，
// 让交付入口能把缺口透传给前端；errors.Is(err, ErrInsufficientEntitlement) 依然成立。
type InsufficientEntitlementError struct {
	Unit      string
	Current   float64
	Required  float64
	Shortfall float64
}

func (e *InsufficientEntitlementError) Error() string {
	return fmt.Sprintf("billing: insufficient entitlement for unit %s: current %.2f, required %.2f, shortfall %.2f",
		e.Unit, e.Current, e.Required, e.Shortfall)
}

// Unwrap 让 errors.Is(err, ErrInsufficientEntitlement) 成立。
func (e *InsufficientEntitlementError) Unwrap() error { return ErrInsufficientEntitlement }

func newInsufficientError(unit string, currentCents, requiredCents int64) *InsufficientEntitlementError {
	shortfall := requiredCents - currentCents
	if shortfall < 0 {
		shortfall = 0
	}
	return &InsufficientEntitlementError{
		Unit:      unit,
		Current:   centsToFloat(currentCents),
		Required:  centsToFloat(requiredCents),
		Shortfall: centsToFloat(shortfall),
	}
}

// DeductItem 是一次扣量里的一个计量单位。
type DeductItem struct {
	Unit   string  `json:"unit"`
	Amount float64 `json:"amount"`
}

// DeductInput 是 POST /api/v1/internal/billing/deduct 的请求体，也是 service 的入参。
//
// IdempotencyKey 必填，格式 <模块>:<业务对象>:<交付类型>，例如
// emr:record:123:draft / deal:recording:123:analysis；同一 key 重复调用只扣一次。
type DeductInput struct {
	TenantID       int64        `json:"tenant_id"`
	ProductCode    string       `json:"product_code"`
	Items          []DeductItem `json:"items"`
	IdempotencyKey string       `json:"idempotency_key"`
	Reason         string       `json:"reason"`
}

// DeductLine 是本次扣量在某个单位上的三段来源明细，三段之和 = Amount。
type DeductLine struct {
	Unit     string  `json:"unit"`
	Amount   float64 `json:"amount"`
	Rollover float64 `json:"rollover"`
	Trial    float64 `json:"trial"`
	Official float64 `json:"official"`
}

// DeductResult 是扣量接口的响应体：逐单位明细 + 产品最新额度 + 是否已暂停交付。
type DeductResult struct {
	Lines    []DeductLine  `json:"lines"`
	Balances []Entitlement `json:"balances"`
	Paused   bool          `json:"paused"`
	// Replayed 为 true 表示这次请求命中了幂等键：没有重复扣量，
	// 返回的是「这笔已经扣过」的结果，调用方可以当成功处理。
	Replayed bool `json:"replayed,omitempty"`
}

// CheckQuotaResponse 是 POST /api/v1/internal/billing/check-quota 成功时的响应体。
type CheckQuotaResponse struct {
	OK bool `json:"ok"`
}

// DeductLedgerEntry 是一条待写入的 usage 流水。
//
// 幂等键只挂在本次扣量的第一条 usage 流水上，其余为 NULL：迁移里的部分唯一索引
// entitlement_ledger_idempotency_uq (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL
// 允许 NULL 重复，因此既能防重，又不需要额外的占位行。
type DeductLedgerEntry struct {
	TenantID       int64
	ProductCode    string
	Unit           string
	Amount         Decimal // 负数
	Source         string
	Reason         string
	IdempotencyKey *string
	Operator       string
}

// DeductChange 是一次扣量对某一行额度的改动。
//
// 行由 (tenant_id, product_code, unit, edition, quota_type) 唯一确定；
// balance 不在这里直接写，store 会用 SQL 重算 rollover_bal + granted - used。
type DeductChange struct {
	Unit        string
	Edition     string
	QuotaType   string
	RolloverBal Decimal
	Used        Decimal
	Status      string
}

// DeductTx 是扣量事务内的数据访问契约。
//
// 所有方法都在同一个 pgx.Tx 上执行，service 层负责编排顺序：
// 锁行 → 幂等闸门 → 写流水 → 更新额度 → 重算并读回 → 提交。
// 用接口隔离后，service 的跨来源拆分算法可以用内存实现单测，不依赖真实数据库。
type DeductTx interface {
	LockEntitlements(ctx context.Context, tenantID int64, productCode string, units []string) ([]Entitlement, error)
	IdempotencyKeyExists(ctx context.Context, tenantID int64, idempotencyKey string) (bool, error)
	InsertUsageLedger(ctx context.Context, entry DeductLedgerEntry) error
	ApplyEntitlementChanges(ctx context.Context, tenantID int64, productCode string, changes []DeductChange) error
	RecomputeAndList(ctx context.Context, tenantID int64, productCode string, units []string) ([]Entitlement, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// deductItemCents 是校验后的入参：用整数分做运算，避免 float64 误差。
type deductItemCents struct {
	unit  string
	cents int64
}

// deductRequest 是校验并归一化后的扣量入参。
type deductRequest struct {
	tenantID       int64
	productCode    string
	idempotencyKey string
	reason         string
	items          []deductItemCents
}

// units 返回本次扣量涉及的计量单位（已去重，保持入参顺序）。
func (r deductRequest) units() []string {
	units := make([]string, 0, len(r.items))
	for _, item := range r.items {
		units = append(units, item.unit)
	}
	return units
}

// Deduct 执行一次扣量（D1）。
//
// 扣量顺序按设计文档：结转（rollover_bal）→ 试用（quota_type='trial' 的 balance）
// → 正式（quota_type='official' 的 balance）；每个 unit 独立计算。
//
// 余额允许被扣成负数：最后一笔交付可能略微超出，此时对应单位的所有额度行会被置为
// paused，结果里 Paused=true，上层据此拒绝后续交付。但当一个单位已经没有任何可用额度
// （可用额度 <= 0，通常因为上一次已扣成 0 或负数）时，扣量直接返回
// ErrInsufficientEntitlement，不再继续超扣。
// replayDeduct 处理幂等命中：不重复扣量，把当前账本状态回放成结果返回。
//
// 只读实现，不写任何流水。返回的 lines 是这笔扣量在账本里的原始金额
// （三段来源无法从流水反推，统一记在 official 上并置 Replayed=true 提示调用方），
// balances 是当前真实余额，调用方拿到的余额一定是准的。
func (s *Service) replayDeduct(ctx context.Context, request deductRequest) (*DeductResult, error) {
	result, err := s.repo.LoadReplay(ctx, request.tenantID, request.productCode,
		request.idempotencyKey, request.units())
	if err != nil {
		// 读不到就退回原来的语义，让调用方知道重复了。
		return nil, ErrAlreadyDeducted
	}
	result.Replayed = true
	return result, nil
}

func (s *Service) Deduct(ctx context.Context, input DeductInput) (*DeductResult, error) {
	request, err := normalizeDeductInput(input)
	if err != nil {
		return nil, err
	}

	tx, err := s.repo.BeginDeduct(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin deduct transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	units := request.units()
	rows, err := tx.LockEntitlements(ctx, request.tenantID, request.productCode, units)
	if err != nil {
		return nil, fmt.Errorf("lock entitlements: %w", err)
	}

	// 幂等闸门的前置查询：放在行锁之后、可用额度校验之前，保证"重试同一笔已扣完的交付"
	// 得到 ALREADY_DEDUCTED 而不是 INSUFFICIENT_ENTITLEMENT。真正防并发重扣的仍是
	// 下面 InsertUsageLedger 的唯一索引（23505）。
	already, err := tx.IdempotencyKeyExists(ctx, request.tenantID, request.idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("query idempotency key: %w", err)
	}
	if already {
		// 幂等命中：不重复扣量。重试的调用方最需要的是「成功 + 结果」，
		// 所以这里把当前账本状态回放成结果返回（Replayed=true），
		// 而不是抛错让上层误以为扣费失败。
		return s.replayDeduct(ctx, request)
	}

	plan, err := planDeduction(request, rows)
	if err != nil {
		return nil, err
	}

	for index := range plan.ledger {
		entry := plan.ledger[index]
		if index == 0 {
			key := request.idempotencyKey
			entry.IdempotencyKey = &key
		}
		if err := tx.InsertUsageLedger(ctx, entry); err != nil {
			if errors.Is(err, ErrAlreadyDeducted) {
				return nil, ErrAlreadyDeducted
			}
			return nil, fmt.Errorf("insert usage ledger: %w", err)
		}
	}

	if err := tx.ApplyEntitlementChanges(ctx, request.tenantID, request.productCode, plan.changes); err != nil {
		return nil, fmt.Errorf("apply entitlement changes: %w", err)
	}

	// 提交前读回该产品所有 unit 的额度：balance 已由 SQL 按
	// rollover_bal + granted - used 重算，避免多次扣量后余额漂移。
	balances, err := tx.RecomputeAndList(ctx, request.tenantID, request.productCode, units)
	if err != nil {
		return nil, fmt.Errorf("recompute entitlements: %w", err)
	}
	paused, err := unitsPaused(units, balances)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit deduct transaction: %w", err)
	}

	result := &DeductResult{
		Lines:    make([]DeductLine, 0, len(plan.lines)),
		Balances: balances,
		Paused:   paused,
	}
	if result.Balances == nil {
		result.Balances = []Entitlement{}
	}
	for _, line := range plan.lines {
		result.Lines = append(result.Lines, DeductLine{
			Unit:     line.unit,
			Amount:   centsToFloat(line.amount),
			Rollover: centsToFloat(line.rollover),
			Trial:    centsToFloat(line.trial),
			Official: centsToFloat(line.official),
		})
	}
	return result, nil
}

// CheckQuota 是只读额度预检（供六产品交付前闸门使用）。
//
// 每个 unit 的可用额度 = 该 unit 所有可消费额度行的 balance 正数部分之和。
// 由于表结构约定 balance = rollover_bal + granted - used，正式行的 balance 已经包含
// 结转余额，这里不能再加一次 rollover_bal，否则预检会高估额度。
// 任一 unit 不足即返回 *InsufficientEntitlementError（带 current/required/shortfall）。
func (s *Service) CheckQuota(ctx context.Context, tenantID int64, productCode string, units []DeductItem) error {
	productCode, items, err := normalizeDemand(tenantID, productCode, units)
	if err != nil {
		return err
	}
	rows, err := s.repo.ListEntitlements(ctx, tenantID, productCode, true)
	if err != nil {
		return fmt.Errorf("check quota: %w", err)
	}
	// 复用扣量侧的"按来源拆分可扣上限"逻辑，保证预检口径与扣量完全一致。
	grouped := make(map[string][]*plannedRow, len(items))
	for index := range rows {
		row, err := newPlannedRow(rows[index])
		if err != nil {
			return fmt.Errorf("check quota: unit %s: %w", rows[index].Unit, err)
		}
		grouped[row.unit] = append(grouped[row.unit], row)
	}
	for _, item := range items {
		if available := unitAvailable(grouped[item.unit]); available < item.cents {
			return newInsufficientError(item.unit, available, item.cents)
		}
	}
	return nil
}

// normalizeDeductInput 校验扣量入参并归一化枚举、幂等键、原因与金额。
func normalizeDeductInput(input DeductInput) (deductRequest, error) {
	productCode, items, err := normalizeDemand(input.TenantID, input.ProductCode, input.Items)
	if err != nil {
		return deductRequest{}, err
	}
	idempotencyKey, err := normalizeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return deductRequest{}, err
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = deductDefaultReason
	}
	if len([]rune(reason)) > maxDeductReasonLength {
		return deductRequest{}, invalidf("reason must be at most %d characters", maxDeductReasonLength)
	}
	return deductRequest{
		tenantID:       input.TenantID,
		productCode:    productCode,
		idempotencyKey: idempotencyKey,
		reason:         reason,
		items:          items,
	}, nil
}

// normalizeDemand 校验 tenant/product/items（扣量与预检共用）。
func normalizeDemand(tenantID int64, productCode string, rawItems []DeductItem) (string, []deductItemCents, error) {
	if tenantID <= 0 {
		return "", nil, invalidf("tenant_id is required")
	}
	productCode = strings.TrimSpace(productCode)
	if productCode == "" {
		return "", nil, invalidf("product_code is required")
	}
	if len(productCode) > 32 {
		return "", nil, invalidf("product_code must be at most 32 characters")
	}
	if len(rawItems) == 0 {
		return "", nil, invalidf("items is required")
	}
	if len(rawItems) > maxDeductItems {
		return "", nil, invalidf("items must contain at most %d units", maxDeductItems)
	}

	items := make([]deductItemCents, 0, len(rawItems))
	seen := make(map[string]struct{}, len(rawItems))
	for index, raw := range rawItems {
		unit := strings.ToLower(strings.TrimSpace(raw.Unit))
		if !IsValidUnit(unit) {
			return "", nil, invalidf("items[%d].unit must be one of %s", index, strings.Join(Units, "/"))
		}
		if _, exists := seen[unit]; exists {
			return "", nil, invalidf("items[%d].unit %q is duplicated", index, unit)
		}
		seen[unit] = struct{}{}

		cents, err := deductAmountCents(index, raw.Amount)
		if err != nil {
			return "", nil, err
		}
		items = append(items, deductItemCents{unit: unit, cents: cents})
	}
	return productCode, items, nil
}

// normalizeIdempotencyKey 校验幂等键：必填、<=128 字节、无空白，
// 且符合 <模块>:<业务对象>:<交付类型>（业务对象通常自带 id，所以至少 3 段）。
func normalizeIdempotencyKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", invalidf("idempotency_key is required")
	}
	if len(key) > maxDeductKeyLength {
		return "", invalidf("idempotency_key must be at most %d characters", maxDeductKeyLength)
	}
	if strings.ContainsAny(key, " \t\r\n") {
		return "", invalidf("idempotency_key must not contain whitespace")
	}
	segments := strings.Split(key, ":")
	if len(segments) < 3 {
		return "", invalidf("idempotency_key must look like <module>:<business_object>:<delivery_type>, e.g. emr:record:123:draft")
	}
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			return "", invalidf("idempotency_key must not contain empty segments")
		}
	}
	return key, nil
}

// deductAmountCents 把入参金额转成整数分。
//
// 入参是 JSON number（float64），最多按两位小数舍入（NUMERIC(14,2) 的精度），
// 避免 0.1+0.2 这类浮点误差累积到余额与流水里。
func deductAmountCents(index int, amount float64) (int64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, invalidf("items[%d].amount must be a decimal number", index)
	}
	if amount <= 0 {
		return 0, invalidf("items[%d].amount must be greater than 0", index)
	}
	if amount > maxQuotaAmount {
		return 0, invalidf("items[%d].amount must not exceed %g", index, maxQuotaAmount)
	}
	cents := int64(math.Round(amount * 100))
	if cents <= 0 || cents > maxQuotaAmountCents {
		return 0, invalidf("items[%d].amount must be greater than 0", index)
	}
	return cents, nil
}

// plannedRow 是扣量过程中某一行额度的内存视图（金额统一转成分）。
type plannedRow struct {
	unit      string
	edition   string
	quotaType string
	status    string
	usable    bool

	grantedCents  int64
	usedCents     int64
	balanceCents  int64
	rolloverCents int64

	newUsedCents     int64
	newRolloverCents int64

	// 按来源拆出来的可扣上限；三者之和 = 该行可扣减到 0 的额度。
	rolloverCap int64
	trialCap    int64
	officialCap int64
}

// deductLineCents 是内存中的扣量明细（分）。
type deductLineCents struct {
	unit     string
	amount   int64
	rollover int64
	trial    int64
	official int64
}

// deductionPlan 是事务里要落库的全部动作：明细、额度改动、usage 流水。
type deductionPlan struct {
	lines   []deductLineCents
	changes []DeductChange
	ledger  []DeductLedgerEntry
}

// planDeduction 是扣量算法的纯函数部分（不碰数据库），可以完整单测。
//
// 对每个 unit 独立按 结转 → 试用 → 正式 扣减：
//
//	remaining := 需求量
//	① 遍历 official 行，扣 min(remaining, rollover_bal)       → 流水 source=rollover
//	② 遍历 trial    行，扣 min(remaining, balance)            → 流水 source=trial
//	③ 遍历 official 行，扣 min(remaining, balance-rollover_bal) → 流水 source=purchase
//	④ 仍有余量（说明本单会超扣）：记到最后一行 official（没有 official 就记 trial）
//	  的 used 上让余额变负，流水再记一条 source=对应来源，reason 标注"超出额度"
//
// 只要该 unit 还有可用额度（可用额度 > 0），就允许扣成负数；可用额度为 0 时
// 返回 ErrInsufficientEntitlement，避免在已经暂停的入口上无限超扣。
func planDeduction(request deductRequest, rows []Entitlement) (*deductionPlan, error) {
	grouped := make(map[string][]*plannedRow, len(request.items))
	for index := range rows {
		row, err := newPlannedRow(rows[index])
		if err != nil {
			return nil, fmt.Errorf("plan deduction: unit %s/%s: %w", rows[index].Unit, rows[index].QuotaType, err)
		}
		grouped[row.unit] = append(grouped[row.unit], row)
	}
	// 同一 unit 内的行顺序固定为 (quota_type, edition)，保证多档位/多行时扣减顺序确定。
	for unit := range grouped {
		unitRows := grouped[unit]
		sort.SliceStable(unitRows, func(left, right int) bool {
			if unitRows[left].quotaType != unitRows[right].quotaType {
				return unitRows[left].quotaType < unitRows[right].quotaType
			}
			return unitRows[left].edition < unitRows[right].edition
		})
	}

	plan := &deductionPlan{
		lines:   make([]deductLineCents, 0, len(request.items)),
		changes: make([]DeductChange, 0, len(rows)),
		ledger:  make([]DeductLedgerEntry, 0, len(request.items)*3),
	}

	for _, item := range request.items {
		unitRows := grouped[item.unit]
		totalAvailable := unitAvailable(unitRows)
		if totalAvailable <= 0 {
			return nil, newInsufficientError(item.unit, totalAvailable, item.cents)
		}

		line := deductLineCents{unit: item.unit, amount: item.cents}
		remaining := item.cents

		// ① 结转：只扣 official 行上的 rollover_bal。
		for _, row := range unitRows {
			if remaining == 0 {
				break
			}
			if !row.usable || row.rolloverCap == 0 {
				continue
			}
			take := min(remaining, row.rolloverCap)
			row.newRolloverCents -= take
			line.rollover += take
			remaining -= take
			plan.ledger = append(plan.ledger, newUsageLedgerEntry(request, item.unit, take, ledgerSourceRollover, false))
		}

		// ② 试用：扣 trial 行的 balance。
		for _, row := range unitRows {
			if remaining == 0 {
				break
			}
			if !row.usable || row.trialCap == 0 {
				continue
			}
			take := min(remaining, row.trialCap)
			row.newUsedCents += take
			line.trial += take
			remaining -= take
			plan.ledger = append(plan.ledger, newUsageLedgerEntry(request, item.unit, take, ledgerSourceTrial, false))
		}

		// ③ 正式：扣 official 行扣掉结转后的剩余额度（= granted - used）。
		for _, row := range unitRows {
			if remaining == 0 {
				break
			}
			if !row.usable || row.officialCap == 0 {
				continue
			}
			take := min(remaining, row.officialCap)
			row.newUsedCents += take
			line.official += take
			remaining -= take
			plan.ledger = append(plan.ledger, newUsageLedgerEntry(request, item.unit, take, ledgerSourceOfficial, false))
		}

		// ④ 超扣：余额允许为负，记到最后一行可用 official（没有则 trial）上。
		if remaining > 0 {
			row, source := overdraftTarget(unitRows)
			if row == nil {
				// 理论上不会发生：totalAvailable > 0 时一定存在可用行。
				return nil, newInsufficientError(item.unit, totalAvailable, item.cents)
			}
			row.newUsedCents += remaining
			if source == ledgerSourceTrial {
				line.trial += remaining
			} else {
				line.official += remaining
			}
			plan.ledger = append(plan.ledger, newUsageLedgerEntry(request, item.unit, remaining, source, true))
			remaining = 0
		}

		if line.rollover+line.trial+line.official != line.amount {
			return nil, fmt.Errorf("plan deduction: unit %s split %d+%d+%d != %d",
				line.unit, line.rollover, line.trial, line.official, line.amount)
		}
		plan.lines = append(plan.lines, line)

		// 扣完后的单位总余额（用 granted + rollover - used 推导，与 SQL 重算口径一致）；
		// <= 0 表示该单位额度已用完，把它的所有可用行置为 paused。paused 行不参与
		// 扣量，也不会在这里被自动恢复——额度补发由开通/续费把行重新置为 active。
		paused := unitRemainingBalance(unitRows) <= 0
		for _, row := range unitRows {
			if !row.usable {
				continue
			}
			status := row.status
			if paused {
				status = entitlementStatusPaused
			}
			if !row.touched() && status == row.status {
				continue
			}
			plan.changes = append(plan.changes, DeductChange{
				Unit:        row.unit,
				Edition:     row.edition,
				QuotaType:   row.quotaType,
				RolloverBal: Decimal(centsToDecimal(row.newRolloverCents)),
				Used:        Decimal(centsToDecimal(row.newUsedCents)),
				Status:      status,
			})
		}
	}
	return plan, nil
}

// newUsageLedgerEntry 构造一条 usage 流水（金额为负）。
func newUsageLedgerEntry(request deductRequest, unit string, amountCents int64, source string, overdraft bool) DeductLedgerEntry {
	reason := request.reason
	if overdraft {
		reason += deductOverdraftSuffix
	}
	return DeductLedgerEntry{
		TenantID:    request.tenantID,
		ProductCode: request.productCode,
		Unit:        unit,
		Amount:      Decimal(centsToDecimal(-amountCents)),
		Source:      source,
		Reason:      reason,
		Operator:    deductOperator,
	}
}

// newPlannedRow 解析一行额度并算出按来源拆分的可扣上限。
func newPlannedRow(entitlement Entitlement) (*plannedRow, error) {
	granted, err := parseEntitlementCents("granted", entitlement.Granted)
	if err != nil {
		return nil, err
	}
	used, err := parseEntitlementCents("used", entitlement.Used)
	if err != nil {
		return nil, err
	}
	balance, err := parseEntitlementCents("balance", entitlement.Balance)
	if err != nil {
		return nil, err
	}
	rollover, err := parseEntitlementCents("rollover_bal", entitlement.RolloverBal)
	if err != nil {
		return nil, err
	}

	row := &plannedRow{
		unit:             entitlement.Unit,
		edition:          entitlement.Edition,
		quotaType:        entitlement.QuotaType,
		status:           entitlement.Status,
		usable:           entitlementUsable(entitlement.Status),
		grantedCents:     granted,
		usedCents:        used,
		balanceCents:     balance,
		rolloverCents:    rollover,
		newUsedCents:     used,
		newRolloverCents: rollover,
	}
	if !row.usable {
		return row, nil
	}

	// 可扣额度以 balance 的正数部分为准：balance 已经包含 rollover，若 granted-used 为负，
	// 结转余额会被已超用的部分抵消，不能按 rollover_bal 原值再扣一次。
	available := max(balance, 0)
	if entitlement.QuotaType == QuotaTypeTrial {
		row.trialCap = available
		return row, nil
	}
	row.rolloverCap = min(max(rollover, 0), available)
	row.officialCap = available - row.rolloverCap
	return row, nil
}

// touched 表示这一行的 rollover_bal / used 是否发生了变化（不含状态）。
func (r *plannedRow) touched() bool {
	return r.newRolloverCents != r.rolloverCents || r.newUsedCents != r.usedCents
}

// unitAvailable 返回一个 unit 当前可扣减到 0 的额度（分）。
func unitAvailable(rows []*plannedRow) int64 {
	total := int64(0)
	for _, row := range rows {
		if !row.usable {
			continue
		}
		total += row.rolloverCap + row.trialCap + row.officialCap
	}
	return total
}

// unitRemainingBalance 返回扣量后一个 unit 的总余额（分），用于判断是否暂停。
func unitRemainingBalance(rows []*plannedRow) int64 {
	total := int64(0)
	for _, row := range rows {
		if !row.usable {
			continue
		}
		total += row.grantedCents + row.newRolloverCents - row.newUsedCents
	}
	return total
}

// overdraftTarget 选出承担超扣的那一行：优先最后一行可用 official，其次最后一行可用 trial。
func overdraftTarget(rows []*plannedRow) (*plannedRow, string) {
	var trial *plannedRow
	for _, row := range rows {
		if row.usable && row.quotaType == QuotaTypeTrial {
			trial = row
		}
	}
	for index := len(rows) - 1; index >= 0; index-- {
		row := rows[index]
		if row.usable && row.quotaType != QuotaTypeTrial {
			return row, ledgerSourceOfficial
		}
	}
	if trial != nil {
		return trial, ledgerSourceTrial
	}
	return nil, ""
}

// entitlementUsable 判断一行额度是否可以继续消费：
// active（含历史空值）可以；paused/expired/cleared 都不参与扣量与预检。
func entitlementUsable(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", entitlementStatusActive:
		return true
	case entitlementStatusPaused, entitlementStatusExpired, entitlementStatusCleared:
		return false
	default:
		return false
	}
}

// unitsPaused 读回结果后判断本次扣量的单位里是否有 unit 已经用完额度。
// 用实际落库的 balance（已由 SQL 重算）判断，避免内存推导值与库里不一致。
func unitsPaused(units []string, balances []Entitlement) (bool, error) {
	for _, unit := range units {
		total := int64(0)
		found := false
		for index := range balances {
			balance := balances[index]
			if balance.Unit != unit || !entitlementUsable(balance.Status) {
				continue
			}
			found = true
			cents, err := parseEntitlementCents("balance", balance.Balance)
			if err != nil {
				return false, fmt.Errorf("read back balance for unit %s: %w", unit, err)
			}
			total += cents
		}
		if !found || total <= 0 {
			return true, nil
		}
	}
	return false, nil
}

// parseEntitlementCents 把库里的 NUMERIC 文本解析成整数分，允许负数（余额可为负）。
func parseEntitlementCents(field string, value Decimal) (int64, error) {
	text := strings.TrimSpace(string(value))
	if text == "" {
		return 0, nil
	}
	sign := int64(1)
	switch {
	case strings.HasPrefix(text, "-"):
		sign = -1
		text = text[1:]
	case strings.HasPrefix(text, "+"):
		text = text[1:]
	}
	whole, fraction, _ := strings.Cut(text, ".")
	if whole == "" && fraction == "" {
		return 0, fmt.Errorf("%s is not a decimal number", field)
	}
	if whole == "" {
		whole = "0"
	}
	if err := checkDigits(whole); err != nil {
		return 0, fmt.Errorf("%s is not a decimal number", field)
	}
	if err := checkDigits(fraction); err != nil {
		return 0, fmt.Errorf("%s is not a decimal number", field)
	}
	if len(fraction) > 2 {
		return 0, fmt.Errorf("%s must have at most 2 decimal places", field)
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	wholeValue, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s is out of range", field)
	}
	fractionValue, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s is not a decimal number", field)
	}
	if wholeValue > (math.MaxInt64-fractionValue)/100 {
		return 0, fmt.Errorf("%s is out of range", field)
	}
	return sign * (wholeValue*100 + fractionValue), nil
}

// centsToFloat 把整数分转成接口输出的 float64（只用于 JSON，不参与运算）。
func centsToFloat(cents int64) float64 {
	return float64(cents) / 100
}
