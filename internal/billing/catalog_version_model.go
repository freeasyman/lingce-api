package billing

// 改价（新增价目版本）的请求模型。
//
// 改价是敏感操作：approver 与 reason 必填；新版本从 effectiveFrom 当天生效，
// 历史订单仍按录单时写入的金额/价目版本结算，不受新价目影响。

import (
	"strings"
	"time"
)

// CreateCatalogVersionV2Request 是 POST /ops/billing/catalog/versions 的请求体。
//
// 前端只改「年价 + 某个单位的增量包规格/价」，其余单位的额度与增量规格沿用当前生效版本。
type CreateCatalogVersionV2Request struct {
	ProductCode    string  `json:"productCode"`
	Unit           string  `json:"unit"`
	AnnualPrice    Decimal `json:"annualPrice"`
	IncrementPrice Decimal `json:"incrementPrice"`
	IncrementSize  Decimal `json:"incrementSize"`
	EffectiveFrom  string  `json:"effectiveFrom"`
	Approver       string  `json:"approver"`
	Reason         string  `json:"reason"`

	SnakeProductCode    string  `json:"product_code"`
	SnakeUnit           string  `json:"unit_code"`
	SnakeAnnualPrice    Decimal `json:"annual_price"`
	SnakeIncrementPrice Decimal `json:"increment_price"`
	SnakeIncrementSize  Decimal `json:"increment_size"`
	SnakeEffectiveFrom  string  `json:"effective_from"`
}

// normalizedCatalogVersion 是校验后的改价入参。
type normalizedCatalogVersion struct {
	ProductCode    string
	Unit           string
	AnnualPrice    Decimal
	IncrementPrice Decimal
	IncrementSize  Decimal
	EffectiveFrom  time.Time
	Approver       string
	Reason         string
}

// normalize 合并别名并校验金额、生效日、审批人与原因。
func (r CreateCatalogVersionV2Request) normalize() (normalizedCatalogVersion, error) {
	productCode := firstNonEmpty(r.ProductCode, r.SnakeProductCode)
	unit := firstNonEmpty(r.Unit, r.SnakeUnit)
	annualPrice := r.AnnualPrice
	if strings.TrimSpace(string(annualPrice)) == "" {
		annualPrice = r.SnakeAnnualPrice
	}
	incrementPrice := r.IncrementPrice
	if strings.TrimSpace(string(incrementPrice)) == "" {
		incrementPrice = r.SnakeIncrementPrice
	}
	incrementSize := r.IncrementSize
	if strings.TrimSpace(string(incrementSize)) == "" {
		incrementSize = r.SnakeIncrementSize
	}
	effectiveFrom := firstNonEmpty(r.EffectiveFrom, r.SnakeEffectiveFrom)

	productCode = normalizeEnum(productCode)
	unit = normalizeEnum(unit)
	approver := strings.TrimSpace(r.Approver)
	reason := strings.TrimSpace(r.Reason)

	if productCode == "" {
		return normalizedCatalogVersion{}, invalidf("请选择产品")
	}
	if len(productCode) > 32 {
		return normalizedCatalogVersion{}, invalidf("产品代码过长")
	}
	if unit == "" {
		return normalizedCatalogVersion{}, invalidf("请选择单位")
	}
	if !IsValidUnit(unit) {
		return normalizedCatalogVersion{}, invalidf("单位不合法")
	}
	annualCents, err := parseAmountCents(annualPrice)
	if err != nil {
		return normalizedCatalogVersion{}, invalidf("年价 %s", err)
	}
	if annualCents <= 0 {
		return normalizedCatalogVersion{}, invalidf("年价要大于 0")
	}
	incrementCents, err := parseAmountCents(incrementPrice)
	if err != nil {
		return normalizedCatalogVersion{}, invalidf("增量价 %s", err)
	}
	sizeCents, err := parseAmountCents(incrementSize)
	if err != nil {
		return normalizedCatalogVersion{}, invalidf("增量包规格 %s", err)
	}
	if sizeCents <= 0 {
		return normalizedCatalogVersion{}, invalidf("增量包规格要大于 0")
	}
	if effectiveFrom == "" {
		return normalizedCatalogVersion{}, invalidf("请选择生效日期")
	}
	effective, err := parseDateOnly(effectiveFrom)
	if err != nil {
		return normalizedCatalogVersion{}, invalidf("生效日期必须是 YYYY-MM-DD")
	}
	if approver == "" {
		return normalizedCatalogVersion{}, invalidf("改价必须填写审批人")
	}
	if len([]rune(approver)) > 64 {
		return normalizedCatalogVersion{}, invalidf("审批人过长")
	}
	if reason == "" {
		return normalizedCatalogVersion{}, invalidf("改价必须填写原因")
	}
	if len([]rune(reason)) > 500 {
		return normalizedCatalogVersion{}, invalidf("改价原因过长")
	}
	return normalizedCatalogVersion{
		ProductCode:    productCode,
		Unit:           unit,
		AnnualPrice:    centsToDecimal(annualCents),
		IncrementPrice: centsToDecimal(incrementCents),
		IncrementSize:  centsToDecimal(sizeCents),
		EffectiveFrom:  effective,
		Approver:       approver,
		Reason:         reason,
	}, nil
}
