package his

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// 价格身份与折扣大类。身份只影响「大类折率矩阵」，不按单项配置。
const (
	priceIdentityStandard  = "standard"
	priceIdentityMember    = "member"
	priceIdentityStaff     = "staff"
	priceIdentityFamily    = "family"
	priceIdentityAgreement = "agreement"
)

// 折扣大类（与 his_discount_rules.category 一致）。
const (
	discountCategoryDrug      = "药品"
	discountCategoryMaterial  = "材料"
	discountCategoryRegister  = "挂号"
	discountCategoryConsult   = "诊查"
	discountCategoryExam      = "检查"
	discountCategoryLab       = "检验"
	discountCategoryTreatment = "治疗"
	discountCategoryNursing   = "护理"
	discountCategoryOther     = "其他"
)

// 议价模式：percent 为整单减免百分比（10 = 让 10%），amount 为整单减免金额。
const (
	negotiationModePercent = "percent"
	negotiationModeAmount  = "amount"
)

var (
	// ErrDrugDiscountLocked 表示药品折率被锁定为 1.0。
	ErrDrugDiscountLocked = errors.New("drug discount is locked at 1.0")
	// ErrInvalidDiscountRate 表示折率不在 0<rate<=1。
	ErrInvalidDiscountRate = errors.New("invalid discount rate")
	// ErrInvalidPriceIdentity 表示价格身份不在允许集合内。
	ErrInvalidPriceIdentity = errors.New("invalid price identity")
	// ErrNegotiationForbidden 表示当前角色/上限不允许该议价。
	ErrNegotiationForbidden = errors.New("negotiation forbidden")
	// ErrNegotiationReasonRequired 表示议价必须填写原因。
	ErrNegotiationReasonRequired = errors.New("negotiation reason required")
	// ErrNegotiationExceedsPayable 表示议价金额超过可折行应收。
	ErrNegotiationExceedsPayable = errors.New("negotiation exceeds payable")
)

// DiscountIdentity 身份元数据（前端下拉与矩阵行）。
type DiscountIdentity struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// DiscountCategory 大类元数据；Locked 表示折率固定 1.0（目前仅药品）。
type DiscountCategory struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Locked bool   `json:"locked"`
}

// DiscountRuleMatrix 身份 × 大类矩阵。
type DiscountRuleMatrix struct {
	Identities                []DiscountIdentity            `json:"identities"`
	Categories                []DiscountCategory            `json:"categories"`
	Rules                     map[string]map[string]float64 `json:"rules"`
	CashierMaxNegotiationRate float64                       `json:"cashier_max_negotiation_rate"`
}

func discountIdentities() []DiscountIdentity {
	return []DiscountIdentity{
		{Key: priceIdentityStandard, Label: "普通"},
		{Key: priceIdentityMember, Label: "会员"},
		{Key: priceIdentityStaff, Label: "员工"},
		{Key: priceIdentityFamily, Label: "家属"},
		{Key: priceIdentityAgreement, Label: "协议"},
	}
}

func discountCategories() []DiscountCategory {
	return []DiscountCategory{
		{Key: discountCategoryDrug, Label: "药品", Locked: true},
		{Key: discountCategoryMaterial, Label: "材料"},
		{Key: discountCategoryRegister, Label: "挂号"},
		{Key: discountCategoryConsult, Label: "诊查"},
		{Key: discountCategoryExam, Label: "检查"},
		{Key: discountCategoryLab, Label: "检验"},
		{Key: discountCategoryTreatment, Label: "治疗"},
		{Key: discountCategoryNursing, Label: "护理"},
		{Key: discountCategoryOther, Label: "其他"},
	}
}

func priceIdentityKeys() map[string]struct{} {
	keys := make(map[string]struct{}, len(discountIdentities()))
	for _, identity := range discountIdentities() {
		keys[identity.Key] = struct{}{}
	}
	return keys
}

func isValidPriceIdentity(identity string) bool {
	_, ok := priceIdentityKeys()[identity]
	return ok
}

func isDiscountCategory(category string) bool {
	for _, item := range discountCategories() {
		if item.Key == category {
			return true
		}
	}
	return false
}

func isLockedDiscountCategory(category string) bool {
	return category == discountCategoryDrug
}

// normalizeChargeItemCategory 把收费项目字典的 category 归一到 9 个大类。
// 诊费/诊查归「诊查」，病理/输氧归「其他」，未知分类归「其他」。
func normalizeChargeItemCategory(category string) string {
	switch strings.TrimSpace(category) {
	case "材料", "耗材":
		return discountCategoryMaterial
	case "挂号", "挂号费":
		return discountCategoryRegister
	case "诊查", "诊费", "诊查费":
		return discountCategoryConsult
	case "检查", "影像":
		return discountCategoryExam
	case "检验", "化验":
		return discountCategoryLab
	case "治疗", "治疗理疗", "理疗":
		return discountCategoryTreatment
	case "护理":
		return discountCategoryNursing
	default:
		return discountCategoryOther
	}
}

// discountCategoryForLine 按行类型与收费项目 category 判定折扣大类。
func discountCategoryForLine(itemType, chargeCategory string) string {
	switch itemType {
	case orderItemRegistrationFee:
		return discountCategoryRegister
	case orderItemDrug:
		return discountCategoryDrug
	default:
		return normalizeChargeItemCategory(chargeCategory)
	}
}

// isNegotiableCategory 议价只作用于可折行：药品/材料不参与。
func isNegotiableCategory(category string) bool {
	return category != discountCategoryDrug && category != discountCategoryMaterial
}

func defaultDiscountRates() map[string]float64 {
	rates := make(map[string]float64, len(discountCategories()))
	for _, category := range discountCategories() {
		rates[category.Key] = 1
	}
	return rates
}

func defaultDiscountRules() map[string]map[string]float64 {
	rules := make(map[string]map[string]float64, len(discountIdentities()))
	for _, identity := range discountIdentities() {
		rules[identity.Key] = defaultDiscountRates()
	}
	return rules
}

// normalizeDiscountRates 校验并补全单身份折率：未知大类拒绝，药品 <1 拒绝，缺省补 1.0。
func normalizeDiscountRates(input map[string]float64) (map[string]float64, error) {
	for category, rate := range input {
		if !isDiscountCategory(category) {
			return nil, ErrInvalidDiscountRate
		}
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate <= 0 || rate > 1 {
			return nil, ErrInvalidDiscountRate
		}
		if isLockedDiscountCategory(category) && rate < 1 {
			return nil, ErrDrugDiscountLocked
		}
	}
	rates := defaultDiscountRates()
	for category, rate := range input {
		rates[category] = rate
	}
	return rates, nil
}

// rateFor 读取某身份某大类折率，缺省 1.0。
func rateFor(rates map[string]float64, category string) float64 {
	if rate, ok := rates[category]; ok && rate > 0 && rate <= 1 {
		return rate
	}
	return 1
}

// roundMoney 与 stats.go 保持一致（2 位小数、四舍五入）。
func discountMoney(value float64) float64 {
	return math.Round(value*100) / 100
}

// applyIdentityPricingToRows 对未定价的新行应用身份折率：
// 原价 = 单价×数量；身份折扣 = 原价×(1−折率)；行应收 = 原价−身份折扣。
func applyIdentityPricingToRows(rows []orderItemRow, rates map[string]float64) {
	for index := range rows {
		row := &rows[index]
		if row.OriginalTotal <= 0 {
			row.OriginalTotal = discountMoney(row.UnitPrice * row.Quantity)
		}
		category := row.Category
		if category == "" {
			category = discountCategoryForLine(row.ItemType, "")
		}
		row.Category = category
		rate := rateFor(rates, category)
		row.IdentityRate = rate
		row.DiscountAmount = discountMoney(row.OriginalTotal * (1 - rate))
		row.NegotiationAmount = 0
		row.LineTotal = discountMoney(row.OriginalTotal - row.DiscountAmount)
	}
}

// allocateNegotiation 按权重分摊议价金额（分单位计算，余数给权重最大的行）。
func allocateNegotiation(total float64, weights []float64) []float64 {
	allocations := make([]float64, len(weights))
	sumWeight := 0.0
	for _, weight := range weights {
		if weight > 0 {
			sumWeight += weight
		}
	}
	if total <= 0 || sumWeight <= 0 {
		return allocations
	}
	totalCents := int64(math.Round(total * 100))
	if totalCents <= 0 {
		return allocations
	}
	allocated := int64(0)
	largest := -1
	largestWeight := -1.0
	for index, weight := range weights {
		if weight <= 0 {
			continue
		}
		cents := int64(math.Floor(float64(totalCents) * weight / sumWeight))
		allocations[index] = float64(cents) / 100
		allocated += cents
		if weight > largestWeight {
			largestWeight = weight
			largest = index
		}
	}
	if remainder := totalCents - allocated; remainder > 0 && largest >= 0 {
		allocations[largest] += float64(remainder) / 100
	}
	return allocations
}

// normalizeNegotiation 统一旧版 discount_total 与新版 negotiation 入参。
// 旧版整单优惠等价于 amount 模式议价，原因为「整单优惠」；新版必须填写原因。
func normalizeNegotiation(input *NegotiationInput, legacyDiscount float64, operatorID int64) (orderNegotiation, error) {
	if input != nil {
		mode := strings.TrimSpace(input.Mode)
		reason := strings.TrimSpace(input.Reason)
		if mode == "" {
			if input.Value <= 0 {
				return orderNegotiation{By: &operatorID}, nil
			}
			mode = negotiationModeAmount
		}
		if mode != negotiationModePercent && mode != negotiationModeAmount {
			return orderNegotiation{}, ErrInvalidRequest
		}
		if math.IsNaN(input.Value) || math.IsInf(input.Value, 0) || input.Value < 0 {
			return orderNegotiation{}, ErrInvalidRequest
		}
		if mode == negotiationModePercent && input.Value > 100 {
			return orderNegotiation{}, ErrInvalidRequest
		}
		if input.Value > 0 && reason == "" {
			return orderNegotiation{}, ErrNegotiationReasonRequired
		}
		return orderNegotiation{Mode: mode, Value: input.Value, Reason: reason, By: &operatorID}, nil
	}
	if legacyDiscount < 0 {
		return orderNegotiation{}, ErrInvalidRequest
	}
	if legacyDiscount == 0 {
		return orderNegotiation{By: &operatorID}, nil
	}
	return orderNegotiation{
		Mode: negotiationModeAmount, Value: legacyDiscount, Reason: "整单优惠", By: &operatorID,
	}, nil
}

// applyNegotiation 把议价分摊到可折行：基数取身份折后应收，与身份价不叠加（只减不增）。
func applyNegotiation(rows []orderItemRow, mode string, value float64) (float64, error) {
	for index := range rows {
		rows[index].NegotiationAmount = 0
	}
	indices := make([]int, 0, len(rows))
	weights := make([]float64, 0, len(rows))
	totalBase := 0.0
	for index := range rows {
		row := &rows[index]
		base := discountMoney(row.OriginalTotal - row.DiscountAmount)
		if base < 0 {
			base = 0
		}
		row.LineTotal = base
		if !isNegotiableCategory(row.Category) {
			continue
		}
		if base <= 0 {
			continue
		}
		indices = append(indices, index)
		weights = append(weights, base)
		totalBase += base
	}
	if mode == "" || value <= 0 || totalBase <= 0 {
		return 0, nil
	}

	var total float64
	switch mode {
	case negotiationModePercent:
		if value > 100 {
			return 0, ErrInvalidRequest
		}
		total = discountMoney(totalBase * value / 100)
	case negotiationModeAmount:
		total = discountMoney(value)
	default:
		return 0, ErrInvalidRequest
	}
	if total < 0 {
		return 0, ErrInvalidRequest
	}
	if total > totalBase+0.000001 {
		return 0, ErrNegotiationExceedsPayable
	}
	if total <= 0 {
		return 0, nil
	}
	allocations := allocateNegotiation(total, weights)
	applied := 0.0
	for position, index := range indices {
		allocation := allocations[position]
		base := rows[index].LineTotal
		if allocation > base {
			allocation = base
		}
		rows[index].NegotiationAmount = allocation
		rows[index].LineTotal = discountMoney(base - allocation)
		applied += allocation
	}
	return discountMoney(applied), nil
}

// orderRowsOriginalTotal 订单原价合计。
func orderRowsOriginalTotal(rows []orderItemRow) float64 {
	total := 0.0
	for _, row := range rows {
		total += row.OriginalTotal
	}
	return discountMoney(total)
}

// orderRowsIdentityDiscount 订单身份折扣合计。
func orderRowsIdentityDiscount(rows []orderItemRow) float64 {
	total := 0.0
	for _, row := range rows {
		total += row.DiscountAmount
	}
	return discountMoney(total)
}

// orderRowsNegotiation 订单议价合计。
func orderRowsNegotiation(rows []orderItemRow) float64 {
	total := 0.0
	for _, row := range rows {
		total += row.NegotiationAmount
	}
	return discountMoney(total)
}

// orderRowsPayable 订单应收合计（行应收之和）。
func orderRowsPayable(rows []orderItemRow) float64 {
	total := 0.0
	for _, row := range rows {
		total += row.LineTotal
	}
	return discountMoney(total)
}

// ---------------------------------------------------------------------------
// 存储层：折扣矩阵 / 价格身份

// DiscountMatrix 读取租户「身份 × 大类」矩阵，缺省补 1.0。
// 协议（agreement）未单独配置时默认取会员价，可单独编辑覆盖。
func (s *Store) DiscountMatrix(ctx context.Context, tenantID int64) (map[string]map[string]float64, error) {
	rules := defaultDiscountRules()
	explicit := map[string]map[string]bool{}
	rows, err := s.pool.Query(ctx, `
		SELECT identity, category, rate FROM his_discount_rules WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var identity, category string
		var rate float64
		if err := rows.Scan(&identity, &category, &rate); err != nil {
			return nil, err
		}
		if !isValidPriceIdentity(identity) || !isDiscountCategory(category) {
			continue
		}
		rules[identity][category] = rate
		if explicit[identity] == nil {
			explicit[identity] = map[string]bool{}
		}
		explicit[identity][category] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, category := range discountCategories() {
		if explicit[priceIdentityAgreement] == nil || !explicit[priceIdentityAgreement][category.Key] {
			rules[priceIdentityAgreement][category.Key] = rules[priceIdentityMember][category.Key]
		}
	}
	return rules, nil
}

// ReplaceDiscountRules 覆盖写某身份的全部大类折率。
func (s *Store) ReplaceDiscountRules(ctx context.Context, tenantID int64, identity string, input map[string]float64) error {
	if !isValidPriceIdentity(identity) {
		return ErrInvalidPriceIdentity
	}
	rates, err := normalizeDiscountRates(input)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for category, rate := range rates {
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_discount_rules (tenant_id, identity, category, rate, updated_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (tenant_id, identity, category)
			DO UPDATE SET rate = EXCLUDED.rate, updated_at = NOW()`,
			tenantID, identity, category, rate); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// CashierMaxNegotiationRate 收费员议价上限（默认 0.10）。
func (s *Store) CashierMaxNegotiationRate(ctx context.Context, tenantID int64) (float64, error) {
	var rate float64
	err := s.pool.QueryRow(ctx, `
		SELECT cashier_max_negotiation_rate FROM his_discount_settings WHERE tenant_id = $1`,
		tenantID).Scan(&rate)
	if err == pgx.ErrNoRows {
		return 0.10, nil
	}
	if err != nil {
		return 0, err
	}
	return rate, nil
}

// UpdateCashierMaxNegotiationRate 设置收费员议价上限。
func (s *Store) UpdateCashierMaxNegotiationRate(ctx context.Context, tenantID int64, rate float64) error {
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate > 1 {
		return ErrInvalidDiscountRate
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO his_discount_settings (tenant_id, cashier_max_negotiation_rate, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (tenant_id)
		DO UPDATE SET cashier_max_negotiation_rate = EXCLUDED.cashier_max_negotiation_rate, updated_at = NOW()`,
		tenantID, rate)
	return err
}

// SetCustomerPriceIdentity 设置患者价格身份，并记录变更留痕（操作人/原身份/新身份）。
func (s *Store) SetCustomerPriceIdentity(ctx context.Context, tenantID, customerID, operatorID int64, identity string) error {
	if !isValidPriceIdentity(identity) {
		return ErrInvalidPriceIdentity
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var oldIdentity string
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(price_identity, ''), 'standard') FROM customers
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL FOR UPDATE`,
		tenantID, customerID).Scan(&oldIdentity)
	if err == pgx.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if oldIdentity == identity {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE customers SET price_identity = $3, updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`, tenantID, customerID, identity); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO his_customer_identity_logs (tenant_id, customer_id, old_identity, new_identity, operator_id)
		VALUES ($1, $2, $3, $4, $5)`, tenantID, customerID, oldIdentity, identity, operatorID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CustomerIdentityLog 价格身份变更留痕。
type CustomerIdentityLog struct {
	ID           int64     `json:"id"`
	CustomerID   int64     `json:"customer_id"`
	OldIdentity  string    `json:"old_identity"`
	NewIdentity  string    `json:"new_identity"`
	OperatorID   *int64    `json:"operator_id"`
	OperatorName string    `json:"operator_name"`
	CreatedAt    time.Time `json:"created_at"`
}

// ListCustomerIdentityLogs 读取患者身份变更记录（最近 N 条）。
func (s *Store) ListCustomerIdentityLogs(ctx context.Context, tenantID, customerID int64, limit int) ([]CustomerIdentityLog, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `
		SELECT l.id, l.customer_id, l.old_identity, l.new_identity,
		       l.operator_id, COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), ''), l.created_at
		FROM his_customer_identity_logs l
		LEFT JOIN employees e ON e.tenant_id = l.tenant_id AND e.id = l.operator_id
		WHERE l.tenant_id = $1 AND l.customer_id = $2
		ORDER BY l.id DESC LIMIT $3`, tenantID, customerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]CustomerIdentityLog, 0)
	for rows.Next() {
		var item CustomerIdentityLog
		if err := rows.Scan(&item.ID, &item.CustomerID, &item.OldIdentity, &item.NewIdentity,
			&item.OperatorID, &item.OperatorName, &item.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, item)
	}
	return logs, rows.Err()
}

// CustomerPriceIdentity 读取患者价格身份（缺省 standard）。
func (s *Store) CustomerPriceIdentity(ctx context.Context, tenantID, customerID int64) (string, error) {
	var identity string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(price_identity, ''), 'standard') FROM customers
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, customerID).Scan(&identity)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return identity, nil
}

// discountContextTx 读取患者身份与租户折率矩阵（事务内）。
func (s *Store) discountContextTx(ctx context.Context, q querier, tenantID, customerID int64) (string, map[string]float64, error) {
	var identity string
	err := q.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(price_identity, ''), 'standard') FROM customers
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, customerID).Scan(&identity)
	if err == pgx.ErrNoRows {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, err
	}
	if !isValidPriceIdentity(identity) {
		identity = priceIdentityStandard
	}
	rules := defaultDiscountRates()
	seen := map[string]bool{}
	rows, err := q.Query(ctx, `
		SELECT category, rate FROM his_discount_rules
		WHERE tenant_id = $1 AND identity = $2`, tenantID, identity)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var category string
		var rate float64
		if err := rows.Scan(&category, &rate); err != nil {
			return "", nil, err
		}
		if isDiscountCategory(category) && rate > 0 && rate <= 1 {
			rules[category] = rate
			seen[category] = true
		}
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	// 协议默认取会员价：仅补齐未单独配置的类目。
	if identity == priceIdentityAgreement {
		memberRows, err := q.Query(ctx, `
			SELECT category, rate FROM his_discount_rules
			WHERE tenant_id = $1 AND identity = $2`, tenantID, priceIdentityMember)
		if err != nil {
			return "", nil, err
		}
		defer memberRows.Close()
		for memberRows.Next() {
			var category string
			var rate float64
			if err := memberRows.Scan(&category, &rate); err != nil {
				return "", nil, err
			}
			if !seen[category] && isDiscountCategory(category) && rate > 0 && rate <= 1 {
				rules[category] = rate
			}
		}
		if err := memberRows.Err(); err != nil {
			return "", nil, err
		}
	}
	// 药品在代码层强制 1.0，避免历史/外部写入 <1 的脏数据。
	rules[discountCategoryDrug] = 1
	return identity, rules, nil
}

// checkNegotiationPermissionTx 议价权限：管理员/店长（chief）不限；其他角色受租户上限约束。
// 租户尚未配置角色时放行（与既有首用兼容策略一致）。
func (s *Store) checkNegotiationPermissionTx(ctx context.Context, q querier, tenantID, employeeID int64, itemsTotal, negotiationTotal float64) error {
	var configured bool
	if err := q.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM his_staff_roles WHERE tenant_id = $1)`, tenantID).Scan(&configured); err != nil {
		return err
	}
	if !configured {
		return nil
	}
	var privileged int
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM his_staff_roles
		WHERE tenant_id = $1 AND employee_id = $2 AND role IN ('admin', 'chief')`,
		tenantID, employeeID).Scan(&privileged); err != nil {
		return err
	}
	if privileged > 0 {
		return nil
	}
	maxRate := 0.10
	var stored float64
	err := q.QueryRow(ctx, `
		SELECT cashier_max_negotiation_rate FROM his_discount_settings WHERE tenant_id = $1`,
		tenantID).Scan(&stored)
	if err == nil {
		maxRate = stored
	} else if err != pgx.ErrNoRows {
		return err
	}
	if itemsTotal <= 0 {
		return ErrNegotiationForbidden
	}
	if negotiationTotal/itemsTotal > maxRate+0.000001 {
		return ErrNegotiationForbidden
	}
	return nil
}
