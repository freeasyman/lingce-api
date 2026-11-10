package billing

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 订单类型、来源与状态枚举，与迁移 20261110_002 的 CHECK 约束保持一致。
const (
	OrderTypeSingle    = "single"
	OrderTypeBundle    = "bundle"
	OrderTypeIncrement = "increment"
	OrderTypeHardware  = "hardware"
	OrderTypeGift      = "gift"
	OrderTypeTrial     = "trial"

	OrderSourceDirect  = "direct"
	OrderSourceChannel = "channel"

	OrderStatusCreated   = "created"
	OrderStatusPaid      = "paid"
	OrderStatusSettled   = "settled"
	OrderStatusRefunded  = "refunded"
	OrderStatusCancelled = "cancelled"

	// 订单开通时写入 entitlement_ledger.source 的取值。
	LedgerSourcePurchase = "purchase"
	LedgerSourceGift     = "gift"
	LedgerSourceTrial    = "trial"
)

// OrderTypes 是录单支持的全部订单类型。
var OrderTypes = []string{
	OrderTypeSingle,
	OrderTypeBundle,
	OrderTypeIncrement,
	OrderTypeHardware,
	OrderTypeGift,
	OrderTypeTrial,
}

// OrderSources 是订单来源枚举。
var OrderSources = []string{OrderSourceDirect, OrderSourceChannel}

// OrderStatuses 是订单状态枚举（含 paid/cancelled，用于列表筛选校验）。
var OrderStatuses = []string{
	OrderStatusCreated,
	OrderStatusPaid,
	OrderStatusSettled,
	OrderStatusRefunded,
	OrderStatusCancelled,
}

// 订单列表分页默认值与上限（与流水分页保持同一约定：默认 50，上限 200）。
const (
	DefaultOrderPage     = 1
	DefaultOrderPageSize = 50
	MaxOrderPageSize     = 200
)

// maxOrderAmountCents 是 NUMERIC(12,2) 能承载的最大金额（9,999,999,999.99）对应的分值。
const maxOrderAmountCents int64 = 999999999999

var (
	// ErrOrderNotFound 表示订单不存在。
	ErrOrderNotFound = errors.New("billing: order not found")
	// ErrOrderNoExists 表示 order_no 唯一索引冲突，由 service 换号重试一次。
	ErrOrderNoExists = errors.New("billing: order number already exists")
	// ErrOrderAmountMismatch 表示回款金额与应付金额不一致（MVP 只支持全款）。
	ErrOrderAmountMismatch = errors.New("billing: paid amount does not match payable amount")
	// ErrOrderAlreadyActivated 表示订单已经开通过，不能重复开通或退款。
	ErrOrderAlreadyActivated = errors.New("billing: order already activated")
	// ErrOrderStatusInvalid 表示订单当前状态不允许该操作。
	ErrOrderStatusInvalid = errors.New("billing: order status does not allow this operation")
	// ErrStandardPlanMissing 表示 tenant_subscription_plans 缺少 code=standard 的套餐行。
	ErrStandardPlanMissing = errors.New("billing: standard subscription plan is missing")
)

// OrderItemsError 表示订单 items 不合法，handler 统一映射成 400 ORDER_ITEMS_INVALID。
type OrderItemsError struct{ Message string }

func (e *OrderItemsError) Error() string { return e.Message }

func invalidItemsf(format string, args ...any) error {
	return &OrderItemsError{Message: fmt.Sprintf(format, args...)}
}

// OrderItem 是订单里的一个产品行：产品 + 档位 + 额度类型 + 各单位额度 + 行金额。
//
// price 保留运营录入（或价目表带出）的等价金额：赠送单 total/payable 为 0，
// 但行金额仍用于展示"相当于 X 元"。
type OrderItem struct {
	ProductCode string     `json:"product_code"`
	Edition     string     `json:"edition"`
	QuotaType   string     `json:"quota_type"`
	Quotas      Quantities `json:"quotas"`
	Price       Decimal    `json:"price"`
}

// Order 是 billing_orders 的一行。items 以 JSONB 存储，读写都经由 OrderItem。
type Order struct {
	ID               int64       `json:"id"`
	OrderNo          string      `json:"order_no"`
	TenantID         int64       `json:"tenant_id"`
	OrderType        string      `json:"order_type"`
	Items            []OrderItem `json:"items"`
	TotalAmount      Decimal     `json:"total_amount"`
	PayableAmount    Decimal     `json:"payable_amount"`
	Source           string      `json:"source"`
	SalesmanID       *int64      `json:"salesman_id"`
	ChannelID        *int64      `json:"channel_id"`
	PriceListVersion string      `json:"price_list_version"`
	PaidAmount       Decimal     `json:"paid_amount"`
	PaidAt           *time.Time  `json:"paid_at"`
	ConfirmedBy      *int64      `json:"confirmed_by"`
	Status           string      `json:"status"`
	Reason           string      `json:"reason"`
	ApprovedBy       *int64      `json:"approved_by"`
	ActivatedAt      *time.Time  `json:"activated_at"`
	ActivatedBy      *int64      `json:"activated_by"`
	CreatedAt        time.Time   `json:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
}

// CreateOrderItem 是录单请求里的一行；price 为空表示由价目表自动带出。
type CreateOrderItem struct {
	ProductCode string         `json:"product_code"`
	Edition     string         `json:"edition"`
	QuotaType   string         `json:"quota_type"`
	Quotas      QuantityInputs `json:"quotas"`
	Price       *Decimal       `json:"price"`
}

// CreateOrderRequest 是 POST /api/v1/ops/orders 的请求体。
type CreateOrderRequest struct {
	TenantID         int64             `json:"tenant_id"`
	OrderType        string            `json:"order_type"`
	Items            []CreateOrderItem `json:"items"`
	Source           string            `json:"source"`
	SalesmanID       *int64            `json:"salesman_id"`
	ChannelID        *int64            `json:"channel_id"`
	Reason           string            `json:"reason"`
	ApprovedBy       *int64            `json:"approved_by"`
	PriceListVersion string            `json:"price_list_version"`
}

// CreateOrderInput 是 service 校验、算价后的落库入参（store 只负责写库）。
type CreateOrderInput struct {
	OrderNo          string
	TenantID         int64
	OrderType        string
	Items            []OrderItem
	TotalAmount      Decimal
	PayableAmount    Decimal
	Source           string
	SalesmanID       *int64
	ChannelID        *int64
	Reason           string
	ApprovedBy       *int64
	PriceListVersion string
}

// orderDraft 是校验后的录单草稿；Items[].Price 为空串表示需要从价目表带出。
type orderDraft struct {
	TenantID         int64
	OrderType        string
	Items            []OrderItem
	Source           string
	SalesmanID       *int64
	ChannelID        *int64
	Reason           string
	ApprovedBy       *int64
	PriceListVersion string
}

// normalize 校验录单入参并归一化枚举、额度与金额格式。
func (r CreateOrderRequest) normalize() (orderDraft, error) {
	if r.TenantID <= 0 {
		return orderDraft{}, invalidf("tenant_id is required")
	}
	orderType := normalizeEnum(r.OrderType)
	if !containsString(OrderTypes, orderType) {
		return orderDraft{}, invalidf("order_type must be one of %s", strings.Join(OrderTypes, "/"))
	}
	source := normalizeEnum(r.Source)
	if source == "" {
		source = OrderSourceDirect
	}
	if !containsString(OrderSources, source) {
		return orderDraft{}, invalidf("source must be one of %s", strings.Join(OrderSources, "/"))
	}
	priceVersion := strings.TrimSpace(r.PriceListVersion)
	if len(priceVersion) > 32 {
		return orderDraft{}, invalidf("price_list_version must be at most 32 characters")
	}
	reason := strings.TrimSpace(r.Reason)
	if orderType == OrderTypeGift && reason == "" {
		return orderDraft{}, invalidf("reason is required for gift orders")
	}
	if err := validateOptionalID("salesman_id", r.SalesmanID); err != nil {
		return orderDraft{}, err
	}
	if err := validateOptionalID("channel_id", r.ChannelID); err != nil {
		return orderDraft{}, err
	}
	if err := validateOptionalID("approved_by", r.ApprovedBy); err != nil {
		return orderDraft{}, err
	}
	if len(r.Items) == 0 {
		return orderDraft{}, invalidItemsf("items is required")
	}

	items := make([]OrderItem, 0, len(r.Items))
	for index, raw := range r.Items {
		item, err := normalizeOrderItem(orderType, index, raw)
		if err != nil {
			return orderDraft{}, err
		}
		items = append(items, item)
	}
	return orderDraft{
		TenantID:         r.TenantID,
		OrderType:        orderType,
		Items:            items,
		Source:           source,
		SalesmanID:       r.SalesmanID,
		ChannelID:        r.ChannelID,
		Reason:           reason,
		ApprovedBy:       r.ApprovedBy,
		PriceListVersion: priceVersion,
	}, nil
}

// normalizeOrderItem 校验一行订单：产品、档位、额度类型、各单位额度与金额。
func normalizeOrderItem(orderType string, index int, raw CreateOrderItem) (OrderItem, error) {
	productCode := strings.TrimSpace(raw.ProductCode)
	if productCode == "" {
		return OrderItem{}, invalidItemsf("items[%d].product_code is required", index)
	}
	if len(productCode) > 32 {
		return OrderItem{}, invalidItemsf("items[%d].product_code must be at most 32 characters", index)
	}
	edition := normalizeEnum(raw.Edition)
	if edition == "" {
		edition = EditionStandard
	}
	if !IsValidEdition(edition) {
		return OrderItem{}, invalidItemsf("items[%d].edition must be one of %s", index, strings.Join(Editions, "/"))
	}
	quotaType := normalizeEnum(raw.QuotaType)
	if quotaType == "" {
		quotaType = QuotaTypeOfficial
	}
	// 试用订单只能发试用额度，避免运营把试用单误录成正式额度（到期不清理）。
	if orderType == OrderTypeTrial {
		quotaType = QuotaTypeTrial
	}
	if quotaType != QuotaTypeOfficial && quotaType != QuotaTypeTrial {
		return OrderItem{}, invalidItemsf("items[%d].quota_type must be one of %s/%s", index, QuotaTypeOfficial, QuotaTypeTrial)
	}
	quotas, err := normalizeOrderQuotas(index, raw.Quotas)
	if err != nil {
		return OrderItem{}, err
	}
	price := Decimal("")
	if raw.Price != nil && strings.TrimSpace(string(*raw.Price)) != "" {
		cents, err := parseAmountCents(*raw.Price)
		if err != nil {
			return OrderItem{}, invalidItemsf("items[%d].price %s", index, err)
		}
		price = centsToDecimal(cents)
	}
	return OrderItem{
		ProductCode: productCode,
		Edition:     edition,
		QuotaType:   quotaType,
		Quotas:      quotas,
		Price:       price,
	}, nil
}

// normalizeOrderQuotas 校验单位与数量（必须 > 0），并编码成 JSONB 参数。
func normalizeOrderQuotas(index int, values QuantityInputs) (Quantities, error) {
	if len(values) == 0 {
		return nil, invalidItemsf("items[%d].quotas is required", index)
	}
	normalized := make(map[string]json.Number, len(values))
	for rawUnit, rawValue := range values {
		unit := normalizeEnum(rawUnit)
		if !IsValidUnit(unit) {
			return nil, invalidItemsf("items[%d].quotas contains unsupported unit %q, allowed: %s", index, rawUnit, strings.Join(Units, "/"))
		}
		if _, exists := normalized[unit]; exists {
			return nil, invalidItemsf("items[%d].quotas contains duplicate unit %q", index, unit)
		}
		text := strings.TrimSpace(string(rawValue))
		amount, err := strconv.ParseFloat(text, 64)
		if text == "" || err != nil || math.IsInf(amount, 0) || math.IsNaN(amount) {
			return nil, invalidItemsf("items[%d].quotas.%s must be a decimal number", index, unit)
		}
		if amount <= 0 {
			return nil, invalidItemsf("items[%d].quotas.%s must be greater than 0", index, unit)
		}
		if amount > maxQuotaAmount {
			return nil, invalidItemsf("items[%d].quotas.%s must not exceed %g", index, unit, maxQuotaAmount)
		}
		normalized[unit] = json.Number(strconv.FormatFloat(amount, 'f', -1, 64))
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode items[%d].quotas: %w", index, err)
	}
	return Quantities(encoded), nil
}

// validateOptionalID 校验可选的外键 id：不传可以，传了必须为正数。
func validateOptionalID(field string, value *int64) error {
	if value == nil {
		return nil
	}
	if *value <= 0 {
		return invalidf("%s must be a positive integer", field)
	}
	return nil
}

// normalizeEnum 把枚举值收敛成小写去空格形式。
func normalizeEnum(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// SettleOrderRequest 是 POST /api/v1/ops/orders/{id}/settle 的请求体。
// note 目前只做入参兼容：迁移只允许新增 activated_at/activated_by 两列，暂不落库。
type SettleOrderRequest struct {
	PaidAmount *Decimal   `json:"paid_amount"`
	PaidAt     *time.Time `json:"paid_at"`
	Note       string     `json:"note"`
}

// SettleOrderInput 是回款确认的落库入参。
type SettleOrderInput struct {
	OrderID     int64
	PaidAmount  Decimal
	PaidAt      time.Time
	ConfirmedBy int64
}

// RefundOrderInput 是退款的落库入参。
type RefundOrderInput struct {
	OrderID int64
}

// GrantLine 是一条待发放的额度：一个产品的一个单位。
type GrantLine struct {
	ProductCode string
	Unit        string
	Edition     string
	QuotaType   string
	Amount      Decimal
}

// ActivateOrderInput 是开通事务的落库入参。
//
// AllowedStatuses 由 service 按订单类型计算（gift/trial 允许 created，其余必须 settled），
// store 用它拼进"抢单"UPDATE 的 WHERE，保证并发/重复点击时只有一个事务能拿到这一行。
type ActivateOrderInput struct {
	OrderID         int64
	ActorID         int64
	Operator        string
	AllowedStatuses []string
	Grants          []GrantLine
	LedgerSource    string
	Reason          string
}

// ActivationResult 是 POST /api/v1/ops/orders/{id}/activate 的响应体：
// 订单最新状态 + 服务期 + 该租户最新额度（复用 Service.ListEntitlements）。
type ActivationResult struct {
	Order         Order          `json:"order"`
	ServicePeriod *ServicePeriod `json:"service_period"`
	Entitlements  []Entitlement  `json:"entitlements"`
}

// OrderQuery 是订单列表的查询条件与分页参数；TenantID 为 0 表示不限租户。
type OrderQuery struct {
	TenantID  int64
	Status    string
	OrderType string
	Page      int
	PageSize  int
}

// OrderPage 是 GET /api/v1/ops/orders 的响应体。
type OrderPage struct {
	Items    []Order `json:"items"`
	Total    int64   `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

// NormalizeOrderQuery 收敛分页边界：page < 1 归 1，page_size < 1 归默认值，超过上限截断。
func NormalizeOrderQuery(query OrderQuery) OrderQuery {
	if query.Page < DefaultOrderPage {
		query.Page = DefaultOrderPage
	}
	if query.PageSize < 1 {
		query.PageSize = DefaultOrderPageSize
	}
	if query.PageSize > MaxOrderPageSize {
		query.PageSize = MaxOrderPageSize
	}
	return query
}

// Offset 返回 SQL OFFSET，并对极端 page 做溢出保护。
func (q OrderQuery) Offset() int64 {
	page := int64(q.Page)
	if page < DefaultOrderPage {
		page = DefaultOrderPage
	}
	size := int64(q.PageSize)
	if size < 1 {
		size = DefaultOrderPageSize
	}
	if size > MaxOrderPageSize {
		size = MaxOrderPageSize
	}
	if page-1 > math.MaxInt64/size {
		return math.MaxInt64
	}
	return (page - 1) * size
}

// GrantLines 把订单 items 展开成"每个产品每个单位一条"的发放明细。
//
// 该方法只读订单快照，供 service 在事务前计算；数量与单位会在开通事务中再次校验，
// 因此这里对脏数据（例如手工改过 jsonb）返回 ORDER_ITEMS_INVALID 而不是静默跳过。
func (o Order) GrantLines() ([]GrantLine, error) {
	if len(o.Items) == 0 {
		return nil, invalidItemsf("order has no items to grant")
	}
	lines := make([]GrantLine, 0, len(o.Items)*2)
	for index, item := range o.Items {
		productCode := strings.TrimSpace(item.ProductCode)
		if productCode == "" {
			return nil, invalidItemsf("order items[%d].product_code is required", index)
		}
		edition := normalizeEnum(item.Edition)
		if edition == "" {
			edition = EditionStandard
		}
		if !IsValidEdition(edition) {
			return nil, invalidItemsf("order items[%d].edition is invalid", index)
		}
		quotaType := normalizeEnum(item.QuotaType)
		if quotaType == "" {
			quotaType = QuotaTypeOfficial
		}
		if quotaType != QuotaTypeOfficial && quotaType != QuotaTypeTrial {
			return nil, invalidItemsf("order items[%d].quota_type is invalid", index)
		}
		// 与录单归一化保持一致：试用订单只能发试用额度。
		if o.OrderType == OrderTypeTrial {
			quotaType = QuotaTypeTrial
		}

		quotas := make(map[string]Decimal)
		if len(item.Quotas) > 0 {
			if err := json.Unmarshal(item.Quotas, &quotas); err != nil {
				return nil, invalidItemsf("order items[%d].quotas is not a valid object", index)
			}
		}
		if len(quotas) == 0 {
			return nil, invalidItemsf("order items[%d].quotas is empty", index)
		}

		units := make([]string, 0, len(quotas))
		for rawUnit := range quotas {
			units = append(units, rawUnit)
		}
		sort.Strings(units)

		seen := make(map[string]struct{}, len(units))
		for _, rawUnit := range units {
			unit := normalizeEnum(rawUnit)
			if !IsValidUnit(unit) {
				return nil, invalidItemsf("order items[%d].quotas contains unsupported unit %q", index, rawUnit)
			}
			if _, exists := seen[unit]; exists {
				return nil, invalidItemsf("order items[%d].quotas contains duplicate unit %q", index, unit)
			}
			seen[unit] = struct{}{}

			text := strings.TrimSpace(string(quotas[rawUnit]))
			amount, err := strconv.ParseFloat(text, 64)
			if text == "" || err != nil || math.IsInf(amount, 0) || math.IsNaN(amount) {
				return nil, invalidItemsf("order items[%d].quotas.%s must be a decimal number", index, unit)
			}
			if amount <= 0 {
				return nil, invalidItemsf("order items[%d].quotas.%s must be greater than 0", index, unit)
			}
			if amount > maxQuotaAmount {
				return nil, invalidItemsf("order items[%d].quotas.%s must not exceed %g", index, unit, maxQuotaAmount)
			}
			lines = append(lines, GrantLine{
				ProductCode: productCode,
				Unit:        unit,
				Edition:     edition,
				QuotaType:   quotaType,
				Amount:      Decimal(strconv.FormatFloat(amount, 'f', -1, 64)),
			})
		}
	}
	sort.SliceStable(lines, func(left, right int) bool {
		if lines[left].ProductCode != lines[right].ProductCode {
			return lines[left].ProductCode < lines[right].ProductCode
		}
		if lines[left].Unit != lines[right].Unit {
			return lines[left].Unit < lines[right].Unit
		}
		if lines[left].Edition != lines[right].Edition {
			return lines[left].Edition < lines[right].Edition
		}
		return lines[left].QuotaType < lines[right].QuotaType
	})
	return lines, nil
}

// allowedActivationStatuses 返回某类订单允许开通的状态：
// 赠送/试用单可以从 created 直接开通，其余订单必须先回款确认（settled）。
func allowedActivationStatuses(orderType string) []string {
	if orderType == OrderTypeGift || orderType == OrderTypeTrial {
		return []string{OrderStatusCreated, OrderStatusSettled}
	}
	return []string{OrderStatusSettled}
}

// parseAmountCents 把 Decimal 金额解析成"分"，只接受最多两位小数的非负十进制数。
//
// 用整数分做加总与比较，避免 float64 在 0.1+0.2 这类金额上产生误差。
func parseAmountCents(value Decimal) (int64, error) {
	text := strings.TrimSpace(string(value))
	if text == "" {
		text = "0"
	}
	if strings.HasPrefix(text, "-") {
		return 0, errors.New("must not be negative")
	}
	if strings.ContainsAny(text, "eE+") {
		return 0, errors.New("must be a plain decimal number")
	}
	whole, fraction, _ := strings.Cut(text, ".")
	if whole == "" {
		return 0, errors.New("must be a decimal number")
	}
	if err := checkDigits(whole); err != nil {
		return 0, err
	}
	if err := checkDigits(fraction); err != nil {
		return 0, err
	}
	if len(fraction) > 2 {
		return 0, errors.New("must have at most 2 decimal places")
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	wholeValue, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, errors.New("must be a decimal number")
	}
	fractionValue, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, errors.New("must be a decimal number")
	}
	if wholeValue > (maxOrderAmountCents-fractionValue)/100 {
		return 0, fmt.Errorf("must not exceed %g", maxCatalogAmount)
	}
	return wholeValue*100 + fractionValue, nil
}

// checkDigits 只允许空串或纯数字（金额不接受指数、单位与符号）。
func checkDigits(text string) error {
	for _, char := range text {
		if char < '0' || char > '9' {
			return errors.New("must be a decimal number")
		}
	}
	return nil
}

// centsToDecimal 把"分"格式化成两位小数的 Decimal 文本。
func centsToDecimal(cents int64) Decimal {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return Decimal(fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100))
}
