// Package billing 实现计费缴费 MVP 的查询能力：价目表、租户额度、额度流水。
//
// 本包只做查询，唯一的写操作是"新增价目版本"（不改历史版本，不动存量额度与流水）。
// 设计依据：docs/计费缴费设计-MVP.md 第 1 章、docs/计费缴费MVP-开发计划.md 阶段 B。
package billing

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 产品档位、额度类型与流水类型枚举，与迁移 20261110_002 的 CHECK 约束保持一致。
const (
	EditionStandard = "standard"
	EditionLight    = "light"
	EditionTrial    = "trial"

	QuotaTypeOfficial = "official"
	QuotaTypeTrial    = "trial"

	LedgerTypeGrant = "grant"
	LedgerTypeUsage = "usage"
)

// Editions 是可写入价目表的档位集合。
var Editions = []string{EditionStandard, EditionLight, EditionTrial}

// Units 是额度与流水支持的计量单位集合。
var Units = []string{"hour", "record", "round", "shot", "prompt", "person"}

// 流水分页默认值与上限（与接口约定一致：默认 50，上限 200）。
const (
	DefaultLedgerPage     = 1
	DefaultLedgerPageSize = 50
	MaxLedgerPageSize     = 200
)

// NUMERIC(12,2) 与 NUMERIC(14,2) 能承载的最大值，用于入参范围校验，避免依赖数据库报错。
const (
	maxCatalogAmount = 9999999999.99
	maxQuotaAmount   = 999999999999.99
)

var (
	// ErrCatalogVersionExists 表示同 (product_code, edition, price_version) 的价目版本已存在。
	ErrCatalogVersionExists = errors.New("billing: catalog price version already exists")
)

// ValidationError 表示调用方入参不合法，handler 统一映射成 400。
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalidf(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Decimal 用字符串承载 NUMERIC，避免 float64 往返丢精度。
//
// JSON 编码时输出数字字面量（例如 12600.00），前端可以直接当数字用；
// JSON 解码时同时接受数字与数字字符串，方便运营后台和脚本两种来源。
type Decimal string

// decimalLiteralPattern 覆盖 JSON 数字与 PostgreSQL numeric 文本两种写法。
var decimalLiteralPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// String 返回十进制文本；空值按 0 处理（NUMERIC 列都有默认值 0）。
func (d Decimal) String() string {
	value := strings.TrimSpace(string(d))
	if value == "" {
		return "0"
	}
	return value
}

// MarshalJSON 输出数字字面量；仅当值不是合法十进制文本时退化为字符串，保证 JSON 始终合法。
func (d Decimal) MarshalJSON() ([]byte, error) {
	value := d.String()
	if !decimalLiteralPattern.MatchString(value) {
		return json.Marshal(value)
	}
	return []byte(value), nil
}

// UnmarshalJSON 允许数字或数字字符串入参。
func (d *Decimal) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*d = ""
		return nil
	}
	if strings.HasPrefix(raw, `"`) {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		*d = Decimal(strings.TrimSpace(text))
		return nil
	}
	if !decimalLiteralPattern.MatchString(raw) {
		return fmt.Errorf("invalid decimal literal %s", raw)
	}
	*d = Decimal(raw)
	return nil
}

// Quantities 承载 product_catalog.quotas / increments 的 JSONB 原文（单位 → 数量）。
// 原样输出可以保留库里写入的数字字面量，不做 float64 往返。
type Quantities json.RawMessage

// MarshalJSON 保证空值输出 {} 而不是 null。
func (q Quantities) MarshalJSON() ([]byte, error) {
	if len(q) == 0 {
		return []byte("{}"), nil
	}
	return []byte(q), nil
}

// UnmarshalJSON 复制一份 JSONB 原文（避免与解码缓冲区共用内存），null 归一为 {}。
func (q *Quantities) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*q = Quantities("{}")
		return nil
	}
	*q = Quantities(append([]byte(nil), data...))
	return nil
}

// String 返回 JSONB 原文，便于日志与断言。
func (q Quantities) String() string {
	if len(q) == 0 {
		return "{}"
	}
	return string(q)
}

// CatalogItem 是价目表的一行：产品 + 档位 + 价目版本。
type CatalogItem struct {
	ProductCode    string     `json:"product_code"`
	ProductName    string     `json:"product_name"`
	Edition        string     `json:"edition"`
	PriceVersion   string     `json:"price_version"`
	AnnualPrice    Decimal    `json:"annual_price"`
	Quotas         Quantities `json:"quotas"`
	IncrementPrice Decimal    `json:"increment_price"`
	Increments     Quantities `json:"increments"`
	IsActive       bool       `json:"is_active"`
}

// CatalogFilter 是价目表查询条件。三个字段都为空时，每个 product+edition 只返回最新 price_version。
type CatalogFilter struct {
	ProductCode  string
	Edition      string
	PriceVersion string
}

// CatalogListResponse 是 GET /api/v1/ops/billing/catalog 的响应体。
type CatalogListResponse struct {
	Items []CatalogItem `json:"items"`
}

// CatalogVersionInput 是新增价目版本的存储入参（service 层已完成校验与归一化）。
type CatalogVersionInput struct {
	ProductCode    string
	ProductName    string
	Edition        string
	PriceVersion   string
	AnnualPrice    Decimal
	Quotas         Quantities
	IncrementPrice Decimal
	Increments     Quantities
}

// QuantityInputs 是"单位 → 数量"的入参映射，值可以是数字或数字字符串。
type QuantityInputs map[string]Decimal

// CreateCatalogRequest 是 POST /api/v1/ops/billing/catalog 的请求体。
//
// product_name 可选：不传时沿用同产品已有版本的名字（没有历史版本时退回 product_code），
// 避免价目表新版本出现空产品名。
type CreateCatalogRequest struct {
	ProductCode    string         `json:"product_code"`
	ProductName    string         `json:"product_name"`
	Edition        string         `json:"edition"`
	PriceVersion   string         `json:"price_version"`
	AnnualPrice    Decimal        `json:"annual_price"`
	Quotas         QuantityInputs `json:"quotas"`
	IncrementPrice Decimal        `json:"increment_price"`
	Increments     QuantityInputs `json:"increments"`
}

// Normalize 校验并归一化新增价目版本的入参。
func (r CreateCatalogRequest) Normalize() (CatalogVersionInput, error) {
	productCode := strings.TrimSpace(r.ProductCode)
	if productCode == "" {
		return CatalogVersionInput{}, invalidf("product_code is required")
	}
	if len(productCode) > 32 {
		return CatalogVersionInput{}, invalidf("product_code must be at most 32 characters")
	}
	productName := strings.TrimSpace(r.ProductName)
	if len(productName) > 64 {
		return CatalogVersionInput{}, invalidf("product_name must be at most 64 characters")
	}
	edition := strings.ToLower(strings.TrimSpace(r.Edition))
	if !IsValidEdition(edition) {
		return CatalogVersionInput{}, invalidf("edition must be one of %s", strings.Join(Editions, "/"))
	}
	priceVersion := strings.TrimSpace(r.PriceVersion)
	if priceVersion == "" {
		return CatalogVersionInput{}, invalidf("price_version is required")
	}
	if len(priceVersion) > 32 {
		return CatalogVersionInput{}, invalidf("price_version must be at most 32 characters")
	}
	annualPrice, err := normalizeAmount("annual_price", r.AnnualPrice, maxCatalogAmount)
	if err != nil {
		return CatalogVersionInput{}, err
	}
	incrementPrice, err := normalizeAmount("increment_price", r.IncrementPrice, maxCatalogAmount)
	if err != nil {
		return CatalogVersionInput{}, err
	}
	quotas, err := normalizeQuantities("quotas", r.Quotas)
	if err != nil {
		return CatalogVersionInput{}, err
	}
	increments, err := normalizeQuantities("increments", r.Increments)
	if err != nil {
		return CatalogVersionInput{}, err
	}
	return CatalogVersionInput{
		ProductCode:    productCode,
		ProductName:    productName,
		Edition:        edition,
		PriceVersion:   priceVersion,
		AnnualPrice:    annualPrice,
		Quotas:         quotas,
		IncrementPrice: incrementPrice,
		Increments:     increments,
	}, nil
}

// normalizeAmount 校验金额/数量的取值范围，空值按 0 处理。
func normalizeAmount(field string, value Decimal, max float64) (Decimal, error) {
	text := strings.TrimSpace(string(value))
	if text == "" {
		text = "0"
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return "", invalidf("%s must be a decimal number", field)
	}
	if number < 0 {
		return "", invalidf("%s must not be negative", field)
	}
	if number > max {
		return "", invalidf("%s must not exceed %g", field, max)
	}
	return Decimal(text), nil
}

// normalizeQuantities 校验单位 key 与数量 value，并编码成 JSONB 参数。
func normalizeQuantities(field string, values QuantityInputs) (Quantities, error) {
	normalized := make(map[string]json.Number, len(values))
	for rawUnit, rawValue := range values {
		unit := strings.ToLower(strings.TrimSpace(rawUnit))
		if !IsValidUnit(unit) {
			return nil, invalidf("%s contains unsupported unit %q, allowed: %s", field, rawUnit, strings.Join(Units, "/"))
		}
		if _, exists := normalized[unit]; exists {
			return nil, invalidf("%s contains duplicate unit %q", field, unit)
		}
		amount, err := normalizeAmount(field+"."+unit, rawValue, maxQuotaAmount)
		if err != nil {
			return nil, err
		}
		normalized[unit] = json.Number(amount.String())
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", field, err)
	}
	return Quantities(encoded), nil
}

// Entitlement 是租户某个产品某个单位的额度余额。
type Entitlement struct {
	ProductCode string  `json:"product_code"`
	ProductName string  `json:"product_name"`
	Unit        string  `json:"unit"`
	Edition     string  `json:"edition"`
	QuotaType   string  `json:"quota_type"`
	Granted     Decimal `json:"granted"`
	Used        Decimal `json:"used"`
	RolloverBal Decimal `json:"rollover_bal"`
	Balance     Decimal `json:"balance"`
	PeriodEnd   string  `json:"period_end"`
	Status      string  `json:"status"`
}

// ServicePeriod 是租户的服务期（来自 tenant_subscriptions，无订阅时为 null）。
type ServicePeriod struct {
	StartedOn *string `json:"started_on"`
	ExpiredOn *string `json:"expired_on"`
}

// EntitlementListResponse 是 GET /api/v1/tenants/{id}/entitlements 的响应体。
type EntitlementListResponse struct {
	Items         []Entitlement  `json:"items"`
	ServicePeriod *ServicePeriod `json:"service_period"`
}

// LedgerEntry 是一条额度流水（发放或扣减）。
type LedgerEntry struct {
	ID            int64     `json:"id"`
	ProductCode   string    `json:"product_code"`
	Unit          string    `json:"unit"`
	Type          string    `json:"type"`
	Amount        Decimal   `json:"amount"`
	Source        string    `json:"source"`
	Reason        string    `json:"reason"`
	LinkedOrderID *int64    `json:"linked_order_id"`
	Operator      string    `json:"operator"`
	CreatedAt     time.Time `json:"created_at"`
}

// LedgerQuery 是额度流水的查询条件与分页参数。
type LedgerQuery struct {
	TenantID int64
	Product  string
	Unit     string
	From     *time.Time
	To       *time.Time
	Page     int
	PageSize int
}

// LedgerPage 是 GET /api/v1/tenants/{id}/entitlement-ledger 的响应体。
type LedgerPage struct {
	Items    []LedgerEntry `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

// NormalizeLedgerQuery 收敛分页边界：page < 1 归 1，page_size < 1 归 50，超过 200 截断到 200。
func NormalizeLedgerQuery(query LedgerQuery) LedgerQuery {
	if query.Page < DefaultLedgerPage {
		query.Page = DefaultLedgerPage
	}
	if query.PageSize < 1 {
		query.PageSize = DefaultLedgerPageSize
	}
	if query.PageSize > MaxLedgerPageSize {
		query.PageSize = MaxLedgerPageSize
	}
	return query
}

// Offset 返回 SQL OFFSET，并对极端 page 做溢出保护。
func (q LedgerQuery) Offset() int64 {
	page := int64(q.Page)
	if page < DefaultLedgerPage {
		page = DefaultLedgerPage
	}
	size := int64(q.PageSize)
	if size < 1 {
		size = DefaultLedgerPageSize
	}
	if size > MaxLedgerPageSize {
		size = MaxLedgerPageSize
	}
	if page-1 > math.MaxInt64/size {
		return math.MaxInt64
	}
	return (page - 1) * size
}

// IsValidEdition 判断档位是否在 standard/light/trial 之内。
func IsValidEdition(edition string) bool {
	return containsString(Editions, edition)
}

// IsValidUnit 判断计量单位是否在迁移的 CHECK 约束之内。
func IsValidUnit(unit string) bool {
	return containsString(Units, unit)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
