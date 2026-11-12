package billing

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

// Service 封装计费业务规则：价目与额度查询，以及阶段 C 的订单/回款/开通/退款。
type Service struct {
	repo Repository

	// now/orderNoRand 是时间与订单号随机后缀的注入点，单测可替换以保证可断言。
	now         func() time.Time
	orderNoRand func(n int) int
}

// NewService 用给定的数据访问实现构造服务层（单元测试传入 fake Repository）。
func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now, orderNoRand: rand.IntN}
}

// ListCatalog 查询价目表。
//
// 未指定 price_version 时，每个 product+edition 只返回最新版本；选择逻辑放在 Go 里，
// 便于单测覆盖"新增版本后默认查最新"的规则，也不依赖数据库窗口函数。
func (s *Service) ListCatalog(ctx context.Context, filter CatalogFilter) ([]CatalogItem, error) {
	filter.ProductCode = strings.TrimSpace(filter.ProductCode)
	filter.Edition = strings.ToLower(strings.TrimSpace(filter.Edition))
	filter.PriceVersion = strings.TrimSpace(filter.PriceVersion)
	if filter.Edition != "" && !IsValidEdition(filter.Edition) {
		return nil, invalidf("edition must be one of %s", strings.Join(Editions, "/"))
	}
	if len(filter.ProductCode) > 32 {
		return nil, invalidf("product_code must be at most 32 characters")
	}
	if len(filter.PriceVersion) > 32 {
		return nil, invalidf("price_version must be at most 32 characters")
	}

	items, err := s.repo.ListCatalog(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list catalog: %w", err)
	}
	// 未指定版本时返回「当前生效」的最新版本：未来生效的版本先不参与价目与算价，
	// 保证改价从 effectiveFrom 当天起才影响新订单，历史订单金额不受影响。
	var latest []CatalogItem
	if filter.PriceVersion != "" {
		latest = latestCatalogVersions(items)
	} else {
		latest = effectiveCatalogVersions(items, dateOnly(s.now()))
	}
	if latest == nil {
		latest = []CatalogItem{}
	}
	return latest, nil
}

// CreateCatalogVersion 新增一个价目版本。价目表只允许加新版本，不提供修改旧版本的入口。
func (s *Service) CreateCatalogVersion(ctx context.Context, req CreateCatalogRequest) (CatalogItem, error) {
	input, err := req.Normalize()
	if err != nil {
		return CatalogItem{}, err
	}
	item, err := s.repo.InsertCatalogVersion(ctx, input)
	if err != nil {
		if errors.Is(err, ErrCatalogVersionExists) {
			return CatalogItem{}, ErrCatalogVersionExists
		}
		return CatalogItem{}, fmt.Errorf("insert catalog version: %w", err)
	}
	return item, nil
}

// ListEntitlements 查询租户额度。
//
// 该方法是 D 阶段"扣量前查余额"的同进程入口，签名保持稳定：
// tenantID 必填，product 为空表示所有产品，includeZero 为 false 时过滤掉余额为 0 的单位。
func (s *Service) ListEntitlements(ctx context.Context, tenantID int64, product string, includeZero bool) ([]Entitlement, error) {
	if tenantID <= 0 {
		return nil, invalidf("tenant_id is required")
	}
	product = strings.TrimSpace(product)
	if len(product) > 32 {
		return nil, invalidf("product must be at most 32 characters")
	}
	items, err := s.repo.ListEntitlements(ctx, tenantID, product, includeZero)
	if err != nil {
		return nil, fmt.Errorf("list entitlements: %w", err)
	}
	if items == nil {
		items = []Entitlement{}
	}
	return items, nil
}

// GetServicePeriod 返回租户服务期；没有订阅记录时返回 (nil, nil)，接口输出 service_period: null。
func (s *Service) GetServicePeriod(ctx context.Context, tenantID int64) (*ServicePeriod, error) {
	if tenantID <= 0 {
		return nil, invalidf("tenant_id is required")
	}
	period, err := s.repo.GetServicePeriod(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get service period: %w", err)
	}
	return period, nil
}

// ListLedger 分页查询额度流水。page/page_size 越界时按约定收敛，不做报错。
func (s *Service) ListLedger(ctx context.Context, query LedgerQuery) (*LedgerPage, error) {
	query, err := normalizeLedgerFilters(query)
	if err != nil {
		return nil, err
	}

	items, total, err := s.repo.ListLedger(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list ledger: %w", err)
	}
	if items == nil {
		items = []LedgerEntry{}
	}
	return &LedgerPage{
		Items:    items,
		Total:    total,
		Page:     query.Page,
		PageSize: query.PageSize,
	}, nil
}

// normalizeLedgerFilters 校验并归一化流水过滤条件（分页查询与 CSV 导出共用）。
//
// tenant_id 必填；product/unit 归一化后再校验枚举；from 不能晚于 to；
// 分页参数统一按 NormalizeLedgerQuery 收敛（导出不使用分页，收敛结果无副作用）。
func normalizeLedgerFilters(query LedgerQuery) (LedgerQuery, error) {
	if query.TenantID <= 0 {
		return LedgerQuery{}, invalidf("tenant_id is required")
	}
	query.Product = strings.TrimSpace(query.Product)
	if len(query.Product) > 32 {
		return LedgerQuery{}, invalidf("product must be at most 32 characters")
	}
	query.Unit = strings.ToLower(strings.TrimSpace(query.Unit))
	if query.Unit != "" && !IsValidUnit(query.Unit) {
		return LedgerQuery{}, invalidf("unit must be one of %s", strings.Join(Units, "/"))
	}
	if query.From != nil && query.To != nil && query.From.After(*query.To) {
		return LedgerQuery{}, invalidf("from must not be after to")
	}
	return NormalizeLedgerQuery(query), nil
}

// latestCatalogVersions 在每个 product+edition 内只保留 price_version 最大的一行。
// 不依赖 SQL 排序：先出现的同键行可能被后续更大的版本替换，输出顺序沿用首次出现顺序。
func latestCatalogVersions(items []CatalogItem) []CatalogItem {
	if len(items) == 0 {
		return nil
	}
	selected := make(map[string]CatalogItem, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		key := item.ProductCode + "\x00" + item.Edition
		current, exists := selected[key]
		if !exists {
			order = append(order, key)
			selected[key] = item
			continue
		}
		if comparePriceVersion(item.PriceVersion, current.PriceVersion) > 0 {
			selected[key] = item
		}
	}
	result := make([]CatalogItem, 0, len(selected))
	for _, key := range order {
		result = append(result, selected[key])
	}
	return result
}

// effectiveCatalogVersions 在每个 product+edition 内只保留「截至 today 已生效」的最新版本。
// effective_from 为空的历史版本视为长期有效；未来生效的版本跳过。
func effectiveCatalogVersions(items []CatalogItem, today time.Time) []CatalogItem {
	if len(items) == 0 {
		return nil
	}
	selected := make(map[string]CatalogItem, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		if effectiveFrom, err := time.Parse("2006-01-02", strings.TrimSpace(item.EffectiveFrom)); err == nil && effectiveFrom.After(today) {
			continue
		}
		key := item.ProductCode + "\x00" + item.Edition
		current, exists := selected[key]
		if !exists {
			order = append(order, key)
			selected[key] = item
			continue
		}
		if comparePriceVersion(item.PriceVersion, current.PriceVersion) > 0 {
			selected[key] = item
		}
	}
	result := make([]CatalogItem, 0, len(selected))
	for _, key := range order {
		result = append(result, selected[key])
	}
	return result
}

// comparePriceVersion 比较价目版本号：纯数字按数值比（2027 > 2026，10 > 9），否则按文本比。
func comparePriceVersion(left, right string) int {
	leftNumber, leftErr := strconv.ParseInt(left, 10, 64)
	rightNumber, rightErr := strconv.ParseInt(right, 10, 64)
	if leftErr == nil && rightErr == nil {
		switch {
		case leftNumber < rightNumber:
			return -1
		case leftNumber > rightNumber:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(left, right)
}
