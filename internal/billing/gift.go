package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// 赠送挂包的审批阈值：≤1 个增量包运营直接挂，>1 个必须老板点头（订单记 approved_by）。
const (
	maxGiftPacksWithoutApproval = 1
	// maxGiftPacks 防止手误输入过大的包数：包数只会是从价目表规格整除推导出来的整数，
	// 超过上限基本可以判定为录错，直接 400 比写入一个天文数字额度更安全。
	maxGiftPacks = 999
)

var (
	// ErrGiftApprovalRequired 表示赠送超过 1 个增量包但没有记录 approved_by。
	// handler 映射成 400 GIFT_APPROVAL_REQUIRED。
	ErrGiftApprovalRequired = errors.New("billing: gift order requires approval")
)

// GiftPreviewRequest 是 POST /api/v1/ops/orders/gift/preview 的请求体。
//
// tenant_id 只用于校验调用方意图（运营在哪个租户上预览），不参与算价；
// packs 是"增量包个数"，系统按价目表的 increments × packs 算出要发的额度。
type GiftPreviewRequest struct {
	TenantID    int64  `json:"tenant_id"`
	ProductCode string `json:"product_code"`
	Edition     string `json:"edition"`
	Packs       int    `json:"packs"`
}

// GiftPreviewItem 是预览里一个计量单位的总额度（单包规格 × 包数）。
type GiftPreviewItem struct {
	Unit   string  `json:"unit"`
	Quotas Decimal `json:"quotas"`
}

// GiftPreviewResponse 是赠送预览的响应体：
// 说"送 2 个包"，系统直接给出"相当于 5,000 元"以及每个单位总共要下发多少额度。
type GiftPreviewResponse struct {
	ProductCode      string            `json:"product_code"`
	ProductName      string            `json:"product_name"`
	Edition          string            `json:"edition"`
	Packs            int               `json:"packs"`
	PackPrice        Decimal           `json:"pack_price"`
	EquivalentAmount Decimal           `json:"equivalent_amount"`
	Items            []GiftPreviewItem `json:"items"`
	NeedsApproval    bool              `json:"needs_approval"`
}

// giftPreviewInput 是校验并归一化后的预览入参。
type giftPreviewInput struct {
	TenantID    int64
	ProductCode string
	Edition     string
	Packs       int
}

// PreviewGift 计算赠送挂包的额度与等价金额（E1）。
//
// 规则：pack_price / increments 取 product_catalog 里该产品+档位的最新 price_version；
// 等价金额 = 包数 × increment_price；needs_approval = 包数 > 1。
func (s *Service) PreviewGift(ctx context.Context, req GiftPreviewRequest) (*GiftPreviewResponse, error) {
	input, err := req.normalize()
	if err != nil {
		return nil, err
	}

	// 价目表按 product+edition 过滤后仍可能是多个版本，统一取最新版本（与录单一致）。
	catalogItems, err := s.repo.ListCatalog(ctx, CatalogFilter{
		ProductCode: input.ProductCode,
		Edition:     input.Edition,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve gift catalog: %w", err)
	}
	catalogItem, found := findCatalogItem(catalogItems, input.ProductCode, input.Edition)
	if !found {
		return nil, invalidf("product %s edition %s not found in price list", input.ProductCode, input.Edition)
	}

	increments, err := parseGiftIncrements(catalogItem)
	if err != nil {
		return nil, err
	}
	items, err := giftQuotasForPacks(increments, input.Packs)
	if err != nil {
		return nil, err
	}
	packCents, err := parseAmountCents(catalogItem.IncrementPrice)
	if err != nil {
		return nil, invalidf("price list increment_price for %s edition %s %s", catalogItem.ProductCode, catalogItem.Edition, err)
	}
	equivalentCents, err := giftEquivalentCents(catalogItem, input.Packs)
	if err != nil {
		return nil, err
	}

	return &GiftPreviewResponse{
		ProductCode:      catalogItem.ProductCode,
		ProductName:      catalogItem.ProductName,
		Edition:          catalogItem.Edition,
		Packs:            input.Packs,
		PackPrice:        Decimal(centsToDecimal(packCents)),
		EquivalentAmount: Decimal(centsToDecimal(equivalentCents)),
		Items:            items,
		NeedsApproval:    input.Packs > maxGiftPacksWithoutApproval,
	}, nil
}

// normalize 校验赠送预览入参并归一化枚举。
func (r GiftPreviewRequest) normalize() (giftPreviewInput, error) {
	if r.TenantID < 0 {
		return giftPreviewInput{}, invalidf("tenant_id must be a positive integer")
	}
	productCode := strings.TrimSpace(r.ProductCode)
	if productCode == "" {
		return giftPreviewInput{}, invalidf("product_code is required")
	}
	if len(productCode) > 32 {
		return giftPreviewInput{}, invalidf("product_code must be at most 32 characters")
	}
	edition := normalizeEnum(r.Edition)
	if edition == "" {
		edition = EditionStandard
	}
	if !IsValidEdition(edition) {
		return giftPreviewInput{}, invalidf("edition must be one of %s", strings.Join(Editions, "/"))
	}
	if r.Packs <= 0 {
		return giftPreviewInput{}, invalidf("packs must be a positive integer")
	}
	if r.Packs > maxGiftPacks {
		return giftPreviewInput{}, invalidf("packs must not exceed %d", maxGiftPacks)
	}
	return giftPreviewInput{
		TenantID:    r.TenantID,
		ProductCode: productCode,
		Edition:     edition,
		Packs:       r.Packs,
	}, nil
}

// findCatalogItem 在价目表快照里按产品+档位找最新版本的一行。
func findCatalogItem(items []CatalogItem, productCode, edition string) (CatalogItem, bool) {
	wantKey := catalogKey(productCode, edition)
	for _, item := range latestCatalogVersions(items) {
		if catalogKey(item.ProductCode, item.Edition) == wantKey {
			return item, true
		}
	}
	return CatalogItem{}, false
}

// parseGiftIncrements 解析价目表的增量包规格（单位 → 每包数量），
// 并要求每个单位的数量为正数：否则无法用额度反推包数。
func parseGiftIncrements(catalog CatalogItem) (map[string]Decimal, error) {
	raw := make(map[string]Decimal)
	if len(catalog.Increments) > 0 {
		if err := json.Unmarshal(catalog.Increments, &raw); err != nil {
			return nil, invalidf("price list increments for %s edition %s is not a valid object",
				catalog.ProductCode, catalog.Edition)
		}
	}
	if len(raw) == 0 {
		return nil, invalidf("price list has no increment packs for %s edition %s",
			catalog.ProductCode, catalog.Edition)
	}

	increments := make(map[string]Decimal, len(raw))
	for rawUnit, value := range raw {
		unit := normalizeEnum(rawUnit)
		if !IsValidUnit(unit) {
			return nil, invalidf("price list increments contains unsupported unit %q, allowed: %s", rawUnit, strings.Join(Units, "/"))
		}
		if _, exists := increments[unit]; exists {
			return nil, invalidf("price list increments contains duplicate unit %q", unit)
		}
		cents, err := parseEntitlementCents("increments."+unit, value)
		if err != nil {
			return nil, invalidf("price list increments.%s %s", unit, err)
		}
		if cents <= 0 {
			return nil, invalidf("price list increments.%s must be greater than 0", unit)
		}
		increments[unit] = value
	}
	return increments, nil
}

// giftUnitDisplayOrder 决定赠送预览 items 的展示顺序：按运营口头习惯的"份/轮/小时/张/题次/人次"排列，
// 与设计文档 2.3 的例子（record、round、hour）一致；未列出的单位按 Units 声明顺序补在后面。
var giftUnitDisplayOrder = []string{"record", "round", "hour", "shot", "prompt", "person"}

// giftQuotasForPacks 按"单包规格 × 包数"算出每个单位要下发的总量。
//
// 输出顺序固定（见 giftUnitDisplayOrder），保证同一份价目表每次预览返回的 items
// 顺序稳定，前端可以直接渲染。
func giftQuotasForPacks(increments map[string]Decimal, packs int) ([]GiftPreviewItem, error) {
	if packs <= 0 {
		return nil, invalidf("packs must be a positive integer")
	}
	units := make([]string, 0, len(increments))
	for unit := range increments {
		units = append(units, unit)
	}
	sort.SliceStable(units, func(left, right int) bool {
		return giftUnitRank(units[left]) < giftUnitRank(units[right])
	})

	items := make([]GiftPreviewItem, 0, len(units))
	for _, unit := range units {
		cents, err := parseEntitlementCents("increments."+unit, increments[unit])
		if err != nil {
			return nil, invalidf("price list increments.%s %s", unit, err)
		}
		if cents > maxQuotaAmountCents/int64(packs) {
			return nil, invalidf("gift quota for unit %s would exceed the maximum quota amount", unit)
		}
		items = append(items, GiftPreviewItem{
			Unit:   unit,
			Quotas: centsToQuantity(cents * int64(packs)),
		})
	}
	return items, nil
}

// giftPacks 用 items 里各单位额度反推包数（E1 录单强校验）。
//
// 对每个单位计算 quotas[unit] / increments[unit]：
//   - 必须能整除且商为正整数；
//   - 各单位的商必须一致（同一个产品档位不可能出现 record 2 包、hour 3 包）。
//
// 反推失败说明运营填的额度不是价目表增量包的整数倍，返回错误由上层转 400。
func giftPacks(quotas Quantities, increments map[string]Decimal) (int, error) {
	values := make(map[string]Decimal)
	if len(quotas) > 0 {
		if err := json.Unmarshal(quotas, &values); err != nil {
			return 0, errors.New("quotas is not a valid object")
		}
	}
	if len(values) == 0 {
		return 0, errors.New("quotas is required")
	}

	units := make([]string, 0, len(values))
	for unit := range values {
		units = append(units, unit)
	}
	sort.SliceStable(units, func(left, right int) bool {
		return giftUnitRank(units[left]) < giftUnitRank(units[right])
	})

	packs := 0
	for _, rawUnit := range units {
		unit := normalizeEnum(rawUnit)
		if !IsValidUnit(unit) {
			return 0, fmt.Errorf("quotas contains unsupported unit %q", rawUnit)
		}
		increment, ok := increments[unit]
		if !ok {
			return 0, fmt.Errorf("quotas.%s is not part of any increment pack", unit)
		}
		incrementCents, err := parseEntitlementCents("increments."+unit, increment)
		if err != nil {
			return 0, fmt.Errorf("price list increments.%s %s", unit, err)
		}
		quotaCents, err := parseEntitlementCents("quotas."+unit, values[rawUnit])
		if err != nil {
			return 0, fmt.Errorf("quotas.%s %s", unit, err)
		}
		if quotaCents <= 0 {
			return 0, fmt.Errorf("quotas.%s must be greater than 0", unit)
		}
		if quotaCents%incrementCents != 0 {
			return 0, fmt.Errorf("quotas.%s = %s is not a whole multiple of the pack size %s",
				unit, values[rawUnit].String(), increment.String())
		}
		// 先在上限内比较商，再转 int，避免超大额度在 32 位 int 上溢出。
		if quotaCents/incrementCents > maxGiftPacks {
			return 0, fmt.Errorf("quotas.%s implies more than %d packs, which exceeds the limit", unit, maxGiftPacks)
		}
		unitPacks := int(quotaCents / incrementCents)
		if unitPacks <= 0 {
			return 0, fmt.Errorf("quotas.%s must be at least one increment pack", unit)
		}
		if packs == 0 {
			packs = unitPacks
			continue
		}
		if packs != unitPacks {
			return 0, fmt.Errorf("quotas do not agree on the increment pack count (%s implies %d packs)", unit, unitPacks)
		}
	}
	return packs, nil
}

// giftEquivalentCents 计算"相当于多少元" = 包数 × 价目表 increment_price（用整数分运算）。
func giftEquivalentCents(catalog CatalogItem, packs int) (int64, error) {
	if packs <= 0 {
		return 0, invalidf("packs must be a positive integer")
	}
	packCents, err := parseAmountCents(catalog.IncrementPrice)
	if err != nil {
		return 0, invalidf("price list increment_price for %s edition %s %s", catalog.ProductCode, catalog.Edition, err)
	}
	if packCents > maxOrderAmountCents/int64(packs) {
		return 0, invalidf("equivalent amount for %d packs exceeds the maximum order amount", packs)
	}
	return packCents * int64(packs), nil
}

// unitOrderIndex 返回单位在 Units 里的声明顺序；未知单位排到最后。
func unitOrderIndex(unit string) int {
	for index, candidate := range Units {
		if candidate == unit {
			return index
		}
	}
	return len(Units)
}

// giftUnitRank 返回赠送预览里单位的展示优先级（见 giftUnitDisplayOrder）。
func giftUnitRank(unit string) int {
	for index, candidate := range giftUnitDisplayOrder {
		if candidate == unit {
			return index
		}
	}
	return len(giftUnitDisplayOrder) + unitOrderIndex(unit)
}

// centsToQuantity 把整数分转成"数量"文本：整数不补两位小数（500 而不是 500.00），
// 小数去掉尾随 0（40.50 → 40.5），与价目表 increments 里额度数量的写法一致。
func centsToQuantity(cents int64) Decimal {
	text := string(centsToDecimal(cents))
	if !strings.Contains(text, ".") {
		return Decimal(text)
	}
	text = strings.TrimSuffix(strings.TrimRight(text, "0"), ".")
	return Decimal(text)
}
