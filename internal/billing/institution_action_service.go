package billing

// 机构动作：正式开通 / 试用开通 / 加量 / 赠送 / 续期 / 加产品 / 延长试用 / 收款。
//
// 每个动作都在一个事务里完成「锁租户 → 建订单 → 发额度写流水 → 写服务期 →（可选）收款核销」，
// 并把服务期同步到 tenant_subscriptions 与 tenants 有效期，保证存量逻辑（登录校验、到期任务）
// 与新表 subscription_periods 口径一致。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// catalogProduct 是价目表里一个产品的标准档快照（开通/加量/续期算价与发放用）。
type catalogProduct struct {
	Code       string
	Name       string
	Annual     Decimal
	IncPrice   Decimal
	Quotas     []quotaPair
	Increments []quotaPair
}

// loadCatalogProducts 取每个产品最新价目版本的标准档。
func (s *InstitutionService) loadCatalogProducts(ctx context.Context) (map[string]catalogProduct, string, error) {
	items, err := s.service.repo.ListCatalog(ctx, CatalogFilter{})
	if err != nil {
		return nil, "", fmt.Errorf("load product catalog: %w", err)
	}
	products := make(map[string]catalogProduct, 8)
	priceVersion := ""
	for _, item := range latestCatalogVersions(items) {
		if normalizeEnum(item.Edition) != EditionStandard {
			continue
		}
		quotas, err := parseQuotaMap(item.Quotas)
		if err != nil {
			return nil, "", fmt.Errorf("catalog %s quotas: %w", item.ProductCode, err)
		}
		increments, err := parseQuotaMap(item.Increments)
		if err != nil {
			return nil, "", fmt.Errorf("catalog %s increments: %w", item.ProductCode, err)
		}
		products[normalizeEnum(item.ProductCode)] = catalogProduct{
			Code:       normalizeEnum(item.ProductCode),
			Name:       item.ProductName,
			Annual:     item.AnnualPrice,
			IncPrice:   item.IncrementPrice,
			Quotas:     quotas,
			Increments: increments,
		}
		if comparePriceVersion(item.PriceVersion, priceVersion) > 0 {
			priceVersion = item.PriceVersion
		}
	}
	return products, priceVersion, nil
}

// resolveProducts 按代码取产品；缺失返回 400（不是 404：请求里的产品代码不合法）。
func resolveCatalogProducts(index map[string]catalogProduct, codes []string) ([]catalogProduct, error) {
	products := make([]catalogProduct, 0, len(codes))
	for _, code := range codes {
		product, ok := index[normalizeEnum(code)]
		if !ok {
			return nil, invalidf("产品 %s 不在价目表中", code)
		}
		products = append(products, product)
	}
	return products, nil
}

// catalogOrderItems 把产品快照转成订单行（标准档、正式额度、年价）。
func catalogOrderItems(products []catalogProduct) ([]OrderItem, error) {
	items := make([]OrderItem, 0, len(products))
	for _, product := range products {
		quotas, err := encodeQuotaPairs(product.Quotas)
		if err != nil {
			return nil, err
		}
		items = append(items, OrderItem{
			ProductCode: product.Code,
			Edition:     EditionStandard,
			QuotaType:   QuotaTypeOfficial,
			Quotas:      quotas,
			Price:       product.Annual,
		})
	}
	return items, nil
}

// encodeQuotaPairs 把 单位→数量 转成订单 items 所需的 JSONB 原文。
func encodeQuotaPairs(pairs []quotaPair) (Quantities, error) {
	values := make(map[string]json.Number, len(pairs))
	for _, pair := range pairs {
		unit := normalizeEnum(pair.Unit)
		if unit == "" {
			continue
		}
		values[unit] = json.Number(pair.Amount.String())
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode quotas: %w", err)
	}
	return Quantities(encoded), nil
}

// sumAnnualCents 汇总产品年价（分）。
func sumAnnualCents(products []catalogProduct) (int64, error) {
	total := int64(0)
	for _, product := range products {
		cents, err := parseAmountCents(product.Annual)
		if err != nil {
			return 0, fmt.Errorf("product %s annual_price: %w", product.Code, err)
		}
		total += cents
	}
	return total, nil
}

// sameStringSet 判断两个字符串集合是否相等（忽略顺序与大小写）。
func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	normalized := make(map[string]int, len(left))
	for _, item := range left {
		normalized[normalizeEnum(item)]++
	}
	for _, item := range right {
		normalized[normalizeEnum(item)]--
	}
	for _, count := range normalized {
		if count != 0 {
			return false
		}
	}
	return true
}

// orderAttribution 按当前归属决定订单来源与渠道/销售：
// 平台组织算直营，其它组织算渠道。
func (s *InstitutionService) orderAttribution(ctx context.Context, tenantID int64) (string, *int64, *int64) {
	ownership, err := s.store.GetCurrentOwnershipRow(ctx, tenantID)
	if err != nil || ownership == nil {
		return OrderSourceDirect, nil, nil
	}
	source := OrderSourceChannel
	if ownership.OrgID != nil {
		if channel, err := s.store.GetChannelRow(ctx, *ownership.OrgID); err == nil && normalizeEnum(channel.Type) == opsOrgTypePlatform {
			source = OrderSourceDirect
		}
	}
	return source, ownership.OrgID, ownership.SalesAdminID
}

// insertOrderWithRetryTx 写订单，order_no 撞号时换号重试（同一事务内）。
func (s *InstitutionService) insertOrderWithRetryTx(ctx context.Context, tx pgx.Tx, input institutionOrderInput) (Order, error) {
	for attempt := 0; attempt < 5; attempt++ {
		input.OrderNo = s.service.nextOrderNo()
		order, err := s.store.InsertInstitutionOrderTx(ctx, tx, input)
		if errors.Is(err, ErrOrderNoExists) {
			continue
		}
		if err != nil {
			return Order{}, err
		}
		return order, nil
	}
	return Order{}, fmt.Errorf("generate unique order number: %w", ErrOrderNoExists)
}

// requireOpenInstitution 校验机构已开通（有 active 服务期）。
func requireOpenInstitution(base institutionBaseRow) error {
	if base.PeriodID == nil || base.PeriodStatus != PeriodStatusActive {
		return ErrInstitutionNotOpened
	}
	return nil
}

// requireFormalInstitution 校验机构处于正式模式（试用机构不能加量/赠送/续期）。
func requireFormalInstitution(base institutionBaseRow) error {
	if normalizeEnum(base.AccountMode) == AccountModeTrial {
		return &ConflictError{Sentinel: ErrInstitutionNotOpened, Message: "试用机构请先转正式后再操作"}
	}
	return nil
}

// statusAfterPayment 按已收金额推导订单状态：收满 settled，部分收 paid，未收 created。
func statusAfterPayment(paid, payable Decimal) string {
	paidCents, err := parseAmountCents(paid)
	if err != nil {
		paidCents = 0
	}
	payableCents, err := parseAmountCents(payable)
	if err != nil {
		payableCents = 0
	}
	switch {
	case paidCents <= 0:
		return OrderStatusCreated
	case paidCents >= payableCents:
		return OrderStatusSettled
	default:
		return OrderStatusPaid
	}
}

// ---------------------------------------------------------------------------
// 正式开通
// ---------------------------------------------------------------------------

// ActivateFormal 正式开通：写服务期 → 按产品发额度 → 记应收（订单）→ 可选记收款并核销。
func (s *InstitutionService) ActivateFormal(ctx context.Context, tenantID int64, req FormalActivationRequest, adminID int64) (*Institution, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	req, err = req.normalize()
	if err != nil {
		return nil, err
	}
	endDate, err := parseDateOnly(req.EndDate)
	if err != nil {
		return nil, invalidf("到期日必须是 YYYY-MM-DD")
	}
	today := s.today()
	if !endDate.After(today) {
		return nil, invalidf("到期日需晚于今天")
	}

	index, priceVersion, err := s.loadCatalogProducts(ctx)
	if err != nil {
		return nil, err
	}
	products, err := resolveCatalogProducts(index, req.ProductCodes)
	if err != nil {
		return nil, err
	}

	var bundle *BundleDefinition
	if req.BundleKey != "" {
		item, err := s.store.GetBundle(ctx, req.BundleKey)
		if err != nil {
			if errors.Is(err, ErrPeerNotFound) {
				return nil, invalidf("套餐不存在")
			}
			return nil, err
		}
		if !sameStringSet(item.Items, req.ProductCodes) {
			return nil, &ValidationError{Message: "套餐与勾选的产品不一致"}
		}
		bundle = &item
	}

	totalCents, err := sumAnnualCents(products)
	if err != nil {
		return nil, err
	}
	orderType := OrderTypeSingle
	if bundle != nil {
		bundleCents, err := parseAmountCents(bundle.Price)
		if err != nil {
			return nil, fmt.Errorf("bundle price: %w", err)
		}
		totalCents = bundleCents
		orderType = OrderTypeBundle
	}

	paidCents := int64(0)
	if req.Payment.Mode == "paid" {
		paidCents, err = parseAmountCents(req.Payment.Amount)
		if err != nil {
			return nil, invalidf("实收金额 %s", err)
		}
		if paidCents > totalCents {
			return nil, invalidf("实收金额不能大于应收金额")
		}
	}
	paidAt := today
	if req.Payment.Date != "" {
		parsed, err := parseDateOnly(req.Payment.Date)
		if err != nil {
			return nil, invalidf("到账日必须是 YYYY-MM-DD")
		}
		paidAt = parsed
	}

	items, err := catalogOrderItems(products)
	if err != nil {
		return nil, err
	}
	total := centsToDecimal(totalCents)
	if err := distributeItemPrices(items, total); err != nil {
		return nil, err
	}
	source, channelID, salesmanID := s.orderAttribution(ctx, tenantID)
	operator := s.service.resolveOperator(ctx, adminID)

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin formal activation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockTenantRow(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	order, err := s.insertOrderWithRetryTx(ctx, tx, institutionOrderInput{
		TenantID:         tenantID,
		OrderType:        orderType,
		Items:            items,
		TotalAmount:      total,
		PayableAmount:    total,
		Source:           source,
		SalesmanID:       salesmanID,
		ChannelID:        channelID,
		PriceListVersion: priceVersion,
		Status:           OrderStatusCreated,
		Reason:           s.activationReason(req),
		PaidAmount:       "0",
		ActivatedBy:      &adminID,
	})
	if err != nil {
		return nil, err
	}

	reason := s.activationReason(req) + "（订单 " + order.OrderNo + "）"
	for _, product := range products {
		grants := s.activationGrants(product, req.FromTrial && req.GiftNewcomer)
		for _, grant := range grants {
			if err := s.store.WriteEntitlementTx(ctx, tx, entitlementWriteSet, tenantID,
				product.Code, grant.Unit, EditionStandard, QuotaTypeOfficial, grant.Amount, formatDateOnly(endDate)); err != nil {
				return nil, err
			}
			if err := s.store.InsertGrantLedgerTx(ctx, tx, tenantID, product.Code, grant.Unit,
				grant.Amount, LedgerSourcePurchase, reason, &order.ID, operator); err != nil {
				return nil, err
			}
		}
	}

	if err := s.store.CloseActivePeriodTx(ctx, tx, tenantID, PeriodStatusTerminated); err != nil {
		return nil, err
	}
	if _, err := s.store.InsertActivePeriodTx(ctx, tx, tenantID, today, endDate, PeriodSourceActivation); err != nil {
		return nil, err
	}
	if err := s.syncServicePeriodTx(ctx, tx, tenantID, AccountModeFormal, today, endDate); err != nil {
		return nil, err
	}

	if paidCents > 0 {
		if _, err := s.createPaymentAndAllocateTx(ctx, tx, paymentAllocationInput{
			TenantID:    tenantID,
			Payer:       firstNonEmpty(base.Legal, base.Name),
			Amount:      centsToDecimal(paidCents),
			ReceivedOn:  paidAt,
			Method:      req.Payment.Method,
			Note:        "开通收款（订单 " + order.OrderNo + "）",
			ActorID:     adminID,
			OrderID:     order.ID,
			OrderAmount: centsToDecimal(paidCents),
		}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit formal activation transaction: %w", err)
	}
	return s.GetInstitution(ctx, tenantID)
}

// activationGrants 返回正式开通要发放的逐单位额度：
// 标准档 +（试用转正式且勾选新客包时）每产品 1 个增量包。
// 同一单位在标准档与增量包里都出现时先相加再写库，避免 set 语义把前面的额度覆盖掉。
func (s *InstitutionService) activationGrants(product catalogProduct, giftNewcomer bool) []quotaPair {
	amounts := make(map[string]Decimal, len(product.Quotas)+len(product.Increments))
	order := make([]string, 0, len(product.Quotas)+len(product.Increments))
	appendAmount := func(unit string, amount Decimal) {
		unit = normalizeEnum(unit)
		if unit == "" {
			return
		}
		if _, exists := amounts[unit]; !exists {
			order = append(order, unit)
			amounts[unit] = amount
			return
		}
		amounts[unit] = sumDecimal(amounts[unit], amount)
	}
	for _, quota := range product.Quotas {
		appendAmount(quota.Unit, quota.Amount)
	}
	if giftNewcomer {
		for _, inc := range product.Increments {
			appendAmount(inc.Unit, inc.Amount)
		}
	}
	pairs := make([]quotaPair, 0, len(order))
	for _, unit := range order {
		pairs = append(pairs, quotaPair{Unit: unit, Amount: amounts[unit]})
	}
	return pairs
}

func (s *InstitutionService) activationReason(req FormalActivationRequest) string {
	if req.FromTrial {
		return "试用转正式"
	}
	return "开通"
}

// ---------------------------------------------------------------------------
// 试用开通
// ---------------------------------------------------------------------------

// ActivateTrial 试用开通：额度 = 标准档 × 试用天数 ÷ 365（每单位最少 1），收款恒为 0。
func (s *InstitutionService) ActivateTrial(ctx context.Context, tenantID int64, req TrialActivationRequest, adminID int64) (*Institution, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	// 试用只用于「已建档未开通」的机构。已开通（含试用中/正式/已过期）的机构
	// 再走试用会把正式服务期直接关成 terminated 并切到 trial，属于不可逆的降级；
	// 试用续期走 extend-trial，正式开通/重新开通走 activate-formal。
	if base.PeriodID != nil {
		return nil, &ConflictError{
			Sentinel: ErrPeerConflict,
			Message:  "该机构已开通，不能重复开通试用；试用续期请用「延长试用」",
		}
	}
	req, err = req.normalize()
	if err != nil {
		return nil, err
	}
	index, priceVersion, err := s.loadCatalogProducts(ctx)
	if err != nil {
		return nil, err
	}
	products, err := resolveCatalogProducts(index, req.ProductCodes)
	if err != nil {
		return nil, err
	}

	today := s.today()
	days := int64(req.Days)
	endDate := addDaysDate(today, req.Days)
	quotasByProduct := make(map[string][]quotaPair, len(products))
	for _, product := range products {
		quotas := make([]quotaPair, 0, len(product.Quotas))
		for _, quota := range product.Quotas {
			// 试用额度按天折算后取整（最少 1），保证存储/页面/PDF 三处一致。
			amount, err := scaleQuotaWhole(quota.Amount, days, 365)
			if err != nil {
				return nil, fmt.Errorf("trial quota %s/%s: %w", product.Code, quota.Unit, err)
			}
			quotas = append(quotas, quotaPair{Unit: quota.Unit, Amount: amount})
		}
		quotasByProduct[product.Code] = quotas
	}

	items := make([]OrderItem, 0, len(products))
	for _, product := range products {
		quotas, err := encodeQuotaPairs(quotasByProduct[product.Code])
		if err != nil {
			return nil, err
		}
		items = append(items, OrderItem{
			ProductCode: product.Code,
			Edition:     EditionTrial,
			QuotaType:   QuotaTypeTrial,
			Quotas:      quotas,
			Price:       "0",
		})
	}
	source, channelID, salesmanID := s.orderAttribution(ctx, tenantID)
	operator := s.service.resolveOperator(ctx, adminID)
	reason := fmt.Sprintf("试用开通 %d 天", req.Days)

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin trial activation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockTenantRow(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	order, err := s.insertOrderWithRetryTx(ctx, tx, institutionOrderInput{
		TenantID:         tenantID,
		OrderType:        OrderTypeTrial,
		Items:            items,
		TotalAmount:      "0",
		PayableAmount:    "0",
		Source:           source,
		SalesmanID:       salesmanID,
		ChannelID:        channelID,
		PriceListVersion: priceVersion,
		Status:           OrderStatusCreated,
		Reason:           reason,
		PaidAmount:       "0",
		ActivatedBy:      &adminID,
	})
	if err != nil {
		return nil, err
	}

	for _, product := range products {
		for _, quota := range quotasByProduct[product.Code] {
			if err := s.store.WriteEntitlementTx(ctx, tx, entitlementWriteSet, tenantID,
				product.Code, quota.Unit, EditionTrial, QuotaTypeTrial, quota.Amount, formatDateOnly(endDate)); err != nil {
				return nil, err
			}
			if err := s.store.InsertGrantLedgerTx(ctx, tx, tenantID, product.Code, quota.Unit,
				quota.Amount, LedgerSourceTrial, reason+"（订单 "+order.OrderNo+"）", &order.ID, operator); err != nil {
				return nil, err
			}
		}
	}

	if err := s.store.CloseActivePeriodTx(ctx, tx, tenantID, PeriodStatusTerminated); err != nil {
		return nil, err
	}
	if _, err := s.store.InsertActivePeriodTx(ctx, tx, tenantID, today, endDate, PeriodSourceTrial); err != nil {
		return nil, err
	}
	if err := s.syncServicePeriodTx(ctx, tx, tenantID, AccountModeTrial, today, endDate); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit trial activation transaction: %w", err)
	}
	return s.GetInstitution(ctx, tenantID)
}

// ---------------------------------------------------------------------------
// 加量 / 赠送
// ---------------------------------------------------------------------------

// AddIncrement 加量：给已有产品追加增量包（按包计价，生成应收）。
func (s *InstitutionService) AddIncrement(ctx context.Context, tenantID int64, req IncrementRequest, adminID int64) (*Institution, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	req, err = req.normalize()
	if err != nil {
		return nil, err
	}
	if err := requireOpenInstitution(base); err != nil {
		return nil, err
	}
	if err := requireFormalInstitution(base); err != nil {
		return nil, err
	}
	index, priceVersion, err := s.loadCatalogProducts(ctx)
	if err != nil {
		return nil, err
	}
	product, ok := index[req.Code]
	if !ok {
		return nil, invalidf("产品 %s 不在价目表中", req.Code)
	}
	if len(product.Increments) == 0 {
		return nil, invalidf("该产品没有可售增量包")
	}
	unitPriceCents, err := parseAmountCents(product.IncPrice)
	if err != nil {
		return nil, fmt.Errorf("product %s increment_price: %w", product.Code, err)
	}
	totalCents := unitPriceCents * int64(req.Packs)

	grants := make([]quotaPair, 0, len(product.Increments))
	for _, inc := range product.Increments {
		amount, err := multiplyQuotaAmount(inc.Amount, int64(req.Packs))
		if err != nil {
			return nil, err
		}
		grants = append(grants, quotaPair{Unit: inc.Unit, Amount: amount})
	}

	return s.applyIncrement(ctx, tenantID, incrementActionInput{
		Product:      product,
		Grants:       grants,
		OrderType:    OrderTypeIncrement,
		Total:        centsToDecimal(totalCents),
		Reason:       fmt.Sprintf("加量：%s × %d 包", product.Name, req.Packs),
		LedgerSource: LedgerSourcePurchase,
		PriceVersion: priceVersion,
		AdminID:      adminID,
	})
}

// GiftIncrement 赠送：挂增量包、金额记 0、必须填原因；超过 1 包需要审批人。
func (s *InstitutionService) GiftIncrement(ctx context.Context, tenantID int64, req GiftIncrementRequest, adminID int64) (*Institution, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	req, err = req.normalize()
	if err != nil {
		return nil, err
	}
	if err := requireOpenInstitution(base); err != nil {
		return nil, err
	}
	if err := requireFormalInstitution(base); err != nil {
		return nil, err
	}
	index, priceVersion, err := s.loadCatalogProducts(ctx)
	if err != nil {
		return nil, err
	}
	product, ok := index[req.Code]
	if !ok {
		return nil, invalidf("产品 %s 不在价目表中", req.Code)
	}
	if len(product.Increments) == 0 {
		return nil, invalidf("该产品没有增量包规格")
	}

	grants := make([]quotaPair, 0, len(product.Increments))
	for _, inc := range product.Increments {
		amount, err := multiplyQuotaAmount(inc.Amount, int64(req.Packs))
		if err != nil {
			return nil, err
		}
		grants = append(grants, quotaPair{Unit: inc.Unit, Amount: amount})
	}

	reason := req.Reason
	approvedBy := (*int64)(nil)
	if resolved, found, lookupErr := s.store.GetSalesByNameAndOrg(ctx, 0, req.Approver); lookupErr == nil && found {
		approvedBy = &resolved.ID
	}
	if req.Approver != "" {
		reason = reason + " · 审批人 " + req.Approver
	}

	return s.applyIncrement(ctx, tenantID, incrementActionInput{
		Product:      product,
		Grants:       grants,
		OrderType:    OrderTypeGift,
		Total:        "0",
		Reason:       "赠送：" + reason,
		LedgerSource: LedgerSourceGift,
		PriceVersion: priceVersion,
		AdminID:      adminID,
		ApprovedBy:   approvedBy,
	})
}

// incrementActionInput 是加量与赠送共用的落库入参。
type incrementActionInput struct {
	Product      catalogProduct
	Grants       []quotaPair
	OrderType    string
	Total        Decimal
	Reason       string
	LedgerSource string
	PriceVersion string
	AdminID      int64
	ApprovedBy   *int64
}

// applyIncrement 执行加量/赠送的共同事务：校验产品已开通 → 建订单 → 累加额度。
func (s *InstitutionService) applyIncrement(ctx context.Context, tenantID int64, input incrementActionInput) (*Institution, error) {
	today := s.today()
	source, channelID, salesmanID := s.orderAttribution(ctx, tenantID)
	operator := s.service.resolveOperator(ctx, input.AdminID)

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin increment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockTenantRow(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	rows, err := s.store.LoadEntitlementRowsForUpdateTx(ctx, tx, tenantID)
	if err != nil {
		return nil, err
	}
	productRows := make([]Entitlement, 0, 4)
	for _, row := range rows {
		if normalizeEnum(row.ProductCode) == input.Product.Code && normalizeEnum(row.QuotaType) == QuotaTypeOfficial {
			productRows = append(productRows, row)
		}
	}
	if len(productRows) == 0 {
		return nil, ErrProductNotActive
	}
	periodEnd := productRows[0].PeriodEnd
	if periodEnd == "" {
		periodEnd = formatDateOnly(today)
	}

	items := []OrderItem{{
		ProductCode: input.Product.Code,
		Edition:     EditionStandard,
		QuotaType:   QuotaTypeOfficial,
		Quotas:      mustEncodeQuotas(input.Grants),
		Price:       input.Total,
	}}
	order, err := s.insertOrderWithRetryTx(ctx, tx, institutionOrderInput{
		TenantID:         tenantID,
		OrderType:        input.OrderType,
		Items:            items,
		TotalAmount:      input.Total,
		PayableAmount:    input.Total,
		Source:           source,
		SalesmanID:       salesmanID,
		ChannelID:        channelID,
		PriceListVersion: input.PriceVersion,
		Status:           OrderStatusCreated,
		Reason:           input.Reason,
		ApprovedBy:       input.ApprovedBy,
		PaidAmount:       "0",
		ActivatedBy:      &input.AdminID,
	})
	if err != nil {
		return nil, err
	}

	reason := input.Reason + "（订单 " + order.OrderNo + "）"
	for _, grant := range input.Grants {
		if err := s.store.WriteEntitlementTx(ctx, tx, entitlementWriteAdd, tenantID,
			input.Product.Code, grant.Unit, EditionStandard, QuotaTypeOfficial, grant.Amount, periodEnd); err != nil {
			return nil, err
		}
		if err := s.store.InsertGrantLedgerTx(ctx, tx, tenantID, input.Product.Code, grant.Unit,
			grant.Amount, input.LedgerSource, reason, &order.ID, operator); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit increment transaction: %w", err)
	}
	return s.GetInstitution(ctx, tenantID)
}

// mustEncodeQuotas 编码额度对象；失败时退化为空对象（调用前的数据都来自库里，正常不会失败）。
func mustEncodeQuotas(pairs []quotaPair) Quantities {
	encoded, err := encodeQuotaPairs(pairs)
	if err != nil {
		return Quantities("{}")
	}
	return encoded
}

// ---------------------------------------------------------------------------
// 续期
// ---------------------------------------------------------------------------

// RenewInstitution 续期：延长服务期 + 发新一年额度 + 上期余额结转一次。
//   - 整年续：新到期日 = 原到期日 + 12 个月 - 1 天，金额 = 年价合计；
//   - 按天续：新到期日 = 指定日期，金额 = 年价 × 新增天数 ÷ 365（不设下限）；
//   - 已过期：从今天重新起算（原到期日早于今天时，整年续 = 今天 + 12 个月 - 1 天）。
func (s *InstitutionService) RenewInstitution(ctx context.Context, tenantID int64, req RenewRequest, adminID int64) (*Institution, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	req, err = req.normalize()
	if err != nil {
		return nil, err
	}
	if base.PeriodID == nil {
		return nil, ErrInstitutionNotOpened
	}
	if err := requireFormalInstitution(base); err != nil {
		return nil, err
	}

	index, priceVersion, err := s.loadCatalogProducts(ctx)
	if err != nil {
		return nil, err
	}

	today := s.today()
	currentEnd := today
	if base.ExpiredOn != nil {
		if parsed, err := parseDateOnly(*base.ExpiredOn); err == nil {
			currentEnd = parsed
		}
	}
	renewFrom := currentEnd
	if renewFrom.Before(today) {
		renewFrom = today
	}

	newEnd := addServiceYear(renewFrom)
	addedDays := int(daysUntil(newEnd, renewFrom))
	if req.Mode == "custom" {
		parsed, err := parseDateOnly(req.EndDate)
		if err != nil {
			return nil, invalidf("新到期日必须是 YYYY-MM-DD")
		}
		if !parsed.After(currentEnd) || !parsed.After(today) {
			return nil, invalidf("新到期日需晚于当前到期日")
		}
		newEnd = parsed
		addedDays = int(daysUntil(newEnd, renewFrom))
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin renewal transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockTenantRow(ctx, tx, tenantID); err != nil {
		return nil, err
	}

	rows, err := s.store.LoadEntitlementRowsForUpdateTx(ctx, tx, tenantID)
	if err != nil {
		return nil, err
	}
	productCodes := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
	for _, row := range rows {
		code := normalizeEnum(row.ProductCode)
		if normalizeEnum(row.QuotaType) != QuotaTypeOfficial {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		productCodes = append(productCodes, code)
	}
	sort.Strings(productCodes)
	if len(productCodes) == 0 {
		return nil, &ConflictError{Sentinel: ErrInstitutionNotOpened, Message: "机构没有正式产品，不能续期"}
	}
	products := make([]catalogProduct, 0, len(productCodes))
	for _, code := range productCodes {
		if product, ok := index[code]; ok {
			products = append(products, product)
		}
	}
	if len(products) == 0 {
		return nil, &ConflictError{Sentinel: ErrInstitutionNotOpened, Message: "机构产品不在价目表中，无法续期"}
	}
	annualCents, err := sumAnnualCents(products)
	if err != nil {
		return nil, err
	}
	amountCents := annualCents
	if req.Mode == "custom" {
		amountCents = int64(math.Round(float64(annualCents) * float64(addedDays) / 365))
		if amountCents < 1 {
			amountCents = 1
		}
	}
	total := centsToDecimal(amountCents)

	items, err := catalogOrderItems(products)
	if err != nil {
		return nil, err
	}
	if err := distributeItemPrices(items, total); err != nil {
		return nil, err
	}
	source, channelID, salesmanID := s.orderAttribution(ctx, tenantID)
	operator := s.service.resolveOperator(ctx, adminID)
	reason := fmt.Sprintf("续期至 %s", formatDateOnly(newEnd))
	order, err := s.insertOrderWithRetryTx(ctx, tx, institutionOrderInput{
		TenantID:         tenantID,
		OrderType:        OrderTypeSingle,
		Items:            items,
		TotalAmount:      total,
		PayableAmount:    total,
		Source:           source,
		SalesmanID:       salesmanID,
		ChannelID:        channelID,
		PriceListVersion: priceVersion,
		Status:           OrderStatusCreated,
		Reason:           reason,
		PaidAmount:       "0",
		ActivatedBy:      &adminID,
	})
	if err != nil {
		return nil, err
	}
	ledgerReason := reason + "（订单 " + order.OrderNo + "）"

	if err := s.applyRenewalEntitlementsTx(ctx, tx, tenantID, rows, index, newEnd, ledgerReason, operator); err != nil {
		return nil, err
	}

	// 服务期：优先顺延当前 active；没有（已到期/被手工终止）则新建一条。
	periodStart := renewFrom
	if base.StartedOn != nil && !currentEnd.Before(today) {
		if parsed, err := parseDateOnly(*base.StartedOn); err == nil {
			periodStart = parsed
		}
	}
	extended, err := s.store.ExtendActivePeriodTx(ctx, tx, tenantID, newEnd)
	if err != nil {
		return nil, err
	}
	if !extended {
		if _, err := s.store.InsertActivePeriodTx(ctx, tx, tenantID, periodStart, newEnd, PeriodSourceRenewal); err != nil {
			return nil, err
		}
	}
	if err := s.syncServicePeriodTx(ctx, tx, tenantID, AccountModeFormal, periodStart, newEnd); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit renewal transaction: %w", err)
	}
	return s.GetInstitution(ctx, tenantID)
}

// applyRenewalEntitlementsTx 续期结转：
//   - 正式额度：leftover = 上期剩余（balance），newRollover = leftover（只结转一次），
//     newGranted = 标准档 + leftover，used 归零；
//   - 试用额度：直接清零（写 usage/expired 流水），不参与结转。
func (s *InstitutionService) applyRenewalEntitlementsTx(ctx context.Context, tx pgx.Tx, tenantID int64,
	rows []Entitlement, index map[string]catalogProduct, newEnd time.Time, reason, operator string) error {
	periodEnd := formatDateOnly(newEnd)
	for _, row := range rows {
		if normalizeEnum(row.QuotaType) == QuotaTypeTrial {
			balanceCents, err := parseEntitlementCents("balance", row.Balance)
			if err != nil {
				return fmt.Errorf("renew trial row %s/%s: %w", row.ProductCode, row.Unit, err)
			}
			if balanceCents > 0 {
				if err := s.store.InsertUsageLedgerTx(ctx, tx, tenantID, row.ProductCode, row.Unit,
					Decimal(centsToDecimal(-balanceCents)), ledgerSourceExpired, reasonTrialExpired, operator); err != nil {
					return err
				}
			}
			if err := s.store.UpdateEntitlementRowTx(ctx, tx, tenantID, row,
				"0", "0", "0", "0", periodEnd, entitlementStatusCleared); err != nil {
				return err
			}
			continue
		}

		balanceCents, err := parseEntitlementCents("balance", row.Balance)
		if err != nil {
			return fmt.Errorf("renew row %s/%s: %w", row.ProductCode, row.Unit, err)
		}
		rolloverCents, err := parseEntitlementCents("rollover_bal", row.RolloverBal)
		if err != nil {
			return fmt.Errorf("renew row %s/%s: %w", row.ProductCode, row.Unit, err)
		}
		// 只结转「本期自己没用完的」：上期结转来的部分清零（与既有续费口径一致）。
		leftoverCents := balanceCents - rolloverCents
		if leftoverCents < 0 {
			leftoverCents = 0
		}
		standardCents := int64(0)
		if product, ok := index[normalizeEnum(row.ProductCode)]; ok {
			for _, quota := range product.Quotas {
				if normalizeEnum(quota.Unit) == normalizeEnum(row.Unit) {
					standardCents, err = parseEntitlementCents("standard quota", quota.Amount)
					if err != nil {
						return err
					}
					break
				}
			}
		}
		newGranted := standardCents + leftoverCents
		status := entitlementStatusActive
		switch {
		case newGranted > 0:
			status = entitlementStatusActive
		case entitlementRenewableStatus(row.Status):
			status = entitlementStatusPaused
		default:
			status = row.Status
		}

		if leftoverCents > 0 {
			if err := s.store.InsertGrantLedgerTx(ctx, tx, tenantID, row.ProductCode, row.Unit,
				Decimal(centsToDecimal(leftoverCents)), ledgerSourceRollover, reasonRollover, nil, operator); err != nil {
				return err
			}
		}
		if standardCents > 0 {
			if err := s.store.InsertGrantLedgerTx(ctx, tx, tenantID, row.ProductCode, row.Unit,
				Decimal(centsToDecimal(standardCents)), LedgerSourcePurchase, reason, nil, operator); err != nil {
				return err
			}
		}
		if err := s.store.UpdateEntitlementRowTx(ctx, tx, tenantID, row,
			Decimal(centsToDecimal(newGranted)), "0", Decimal(centsToDecimal(leftoverCents)),
			Decimal(centsToDecimal(newGranted)), periodEnd, status); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 加产品
// ---------------------------------------------------------------------------

// AddProduct 老客户中途加一个新产品：按剩余天数折算金额与额度（不足 30 天按 30 天）。
func (s *InstitutionService) AddProduct(ctx context.Context, tenantID int64, req AddProductRequest, adminID int64) (*Institution, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	req, err = req.normalize()
	if err != nil {
		return nil, err
	}
	if err := requireOpenInstitution(base); err != nil {
		return nil, err
	}
	if err := requireFormalInstitution(base); err != nil {
		return nil, err
	}
	index, priceVersion, err := s.loadCatalogProducts(ctx)
	if err != nil {
		return nil, err
	}
	product, ok := index[req.Code]
	if !ok {
		return nil, invalidf("产品 %s 不在价目表中", req.Code)
	}

	today := s.today()
	endDate := today
	if base.ExpiredOn != nil {
		if parsed, err := parseDateOnly(*base.ExpiredOn); err == nil {
			endDate = parsed
		}
	}
	days := daysUntil(endDate, today)
	if days < 30 {
		days = 30
	}
	annualCents, err := parseAmountCents(product.Annual)
	if err != nil {
		return nil, fmt.Errorf("product %s annual_price: %w", product.Code, err)
	}
	amountCents := int64(math.Round(float64(annualCents) * float64(days) / 365))
	if amountCents < 1 {
		amountCents = 1
	}
	total := centsToDecimal(amountCents)

	grants := make([]quotaPair, 0, len(product.Quotas))
	for _, quota := range product.Quotas {
		amount, err := scaleQuotaAmount(quota.Amount, int64(days), 365)
		if err != nil {
			return nil, err
		}
		grants = append(grants, quotaPair{Unit: quota.Unit, Amount: amount})
	}
	source, channelID, salesmanID := s.orderAttribution(ctx, tenantID)
	operator := s.service.resolveOperator(ctx, adminID)
	reason := fmt.Sprintf("加产品：%s（%d 天折算）", product.Name, days)

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin add product transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockTenantRow(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	rows, err := s.store.LoadEntitlementRowsForUpdateTx(ctx, tx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if normalizeEnum(row.ProductCode) == product.Code && normalizeEnum(row.QuotaType) == QuotaTypeOfficial {
			return nil, ErrProductAlreadyActive
		}
	}

	items := []OrderItem{{
		ProductCode: product.Code,
		Edition:     EditionStandard,
		QuotaType:   QuotaTypeOfficial,
		Quotas:      mustEncodeQuotas(grants),
		Price:       total,
	}}
	order, err := s.insertOrderWithRetryTx(ctx, tx, institutionOrderInput{
		TenantID:         tenantID,
		OrderType:        OrderTypeSingle,
		Items:            items,
		TotalAmount:      total,
		PayableAmount:    total,
		Source:           source,
		SalesmanID:       salesmanID,
		ChannelID:        channelID,
		PriceListVersion: priceVersion,
		Status:           OrderStatusCreated,
		Reason:           reason,
		PaidAmount:       "0",
		ActivatedBy:      &adminID,
	})
	if err != nil {
		return nil, err
	}
	ledgerReason := reason + "（订单 " + order.OrderNo + "）"
	for _, grant := range grants {
		if err := s.store.WriteEntitlementTx(ctx, tx, entitlementWriteAdd, tenantID,
			product.Code, grant.Unit, EditionStandard, QuotaTypeOfficial, grant.Amount, formatDateOnly(endDate)); err != nil {
			return nil, err
		}
		if err := s.store.InsertGrantLedgerTx(ctx, tx, tenantID, product.Code, grant.Unit,
			grant.Amount, LedgerSourcePurchase, ledgerReason, &order.ID, operator); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit add product transaction: %w", err)
	}
	return s.GetInstitution(ctx, tenantID)
}

// ---------------------------------------------------------------------------
// 延长试用
// ---------------------------------------------------------------------------

// ExtendTrial 延长试用：到期日后延、额度按同比例增加（不生成订单、不涉及收款）。
func (s *InstitutionService) ExtendTrial(ctx context.Context, tenantID int64, req ExtendTrialRequest, adminID int64) (*Institution, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	req, err = req.normalize()
	if err != nil {
		return nil, err
	}
	if err := requireOpenInstitution(base); err != nil {
		return nil, err
	}
	if normalizeEnum(base.AccountMode) != AccountModeTrial {
		return nil, &ConflictError{Sentinel: ErrInstitutionNotOpened, Message: "只有试用机构可以延长试用"}
	}
	index, _, err := s.loadCatalogProducts(ctx)
	if err != nil {
		return nil, err
	}
	today := s.today()
	currentEnd := today
	if base.ExpiredOn != nil {
		if parsed, err := parseDateOnly(*base.ExpiredOn); err == nil {
			currentEnd = parsed
		}
	}
	newEnd := addDaysDate(currentEnd, req.Days)
	periodStart := today
	if base.StartedOn != nil {
		if parsed, err := parseDateOnly(*base.StartedOn); err == nil {
			periodStart = parsed
		}
	}
	// 延长试用按「从试用开始到新到期日」的总天数重算整额，再补差额：
	// 不用新增天数单独折算后叠加，避免两次取整累积误差。
	totalDays := int64(newEnd.Sub(periodStart).Hours() / 24)
	if totalDays < 0 {
		totalDays = 0
	}
	operator := s.service.resolveOperator(ctx, adminID)

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin extend trial transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockTenantRow(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	rows, err := s.store.LoadEntitlementRowsForUpdateTx(ctx, tx, tenantID)
	if err != nil {
		return nil, err
	}
	reason := fmt.Sprintf("延长试用 %d 天", req.Days)
	for _, row := range rows {
		if normalizeEnum(row.QuotaType) != QuotaTypeTrial {
			continue
		}
		product, ok := index[normalizeEnum(row.ProductCode)]
		if !ok {
			continue
		}
		var target Decimal
		for _, quota := range product.Quotas {
			if normalizeEnum(quota.Unit) != normalizeEnum(row.Unit) {
				continue
			}
			amount, err := scaleQuotaWhole(quota.Amount, totalDays, 365)
			if err != nil {
				return nil, err
			}
			target = amount
			break
		}
		if target == "" {
			continue
		}
		// 目标额 - 已发额 = 本次补发额（按整数分算，保证结果为整数）。
		targetCents, err := parseAmountCents(target)
		if err != nil {
			return nil, err
		}
		grantedCents, err := parseAmountCents(row.Granted)
		if err != nil {
			return nil, err
		}
		if targetCents <= grantedCents {
			continue
		}
		added := centsToDecimal(targetCents - grantedCents)
		if err := s.store.WriteEntitlementTx(ctx, tx, entitlementWriteAdd, tenantID,
			row.ProductCode, row.Unit, row.Edition, QuotaTypeTrial, added, formatDateOnly(newEnd)); err != nil {
			return nil, err
		}
		if err := s.store.InsertGrantLedgerTx(ctx, tx, tenantID, row.ProductCode, row.Unit,
			added, LedgerSourceTrial, reason, nil, operator); err != nil {
			return nil, err
		}
	}
	extended, err := s.store.ExtendActivePeriodTx(ctx, tx, tenantID, newEnd)
	if err != nil {
		return nil, err
	}
	if !extended {
		return nil, ErrInstitutionNotOpened
	}
	if err := s.syncServicePeriodTx(ctx, tx, tenantID, AccountModeTrial, periodStart, newEnd); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit extend trial transaction: %w", err)
	}
	return s.GetInstitution(ctx, tenantID)
}
