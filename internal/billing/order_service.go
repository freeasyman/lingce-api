package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CreateOrder 录入一笔订单（C1）。
//
// 金额在 service 层用"分"整数加总：total_amount = Σ price，payable_amount = total_amount；
// order_type=gift 时两者强制为 0（行金额仍保留，用于展示"相当于 X 元"）。
// price 缺省的 item 从 product_catalog 带出：increment 订单用增量包价，其余用年价。
func (s *Service) CreateOrder(ctx context.Context, req CreateOrderRequest) (Order, error) {
	draft, err := req.normalize()
	if err != nil {
		return Order{}, err
	}

	catalogItems, err := s.repo.ListCatalog(ctx, CatalogFilter{PriceVersion: draft.PriceListVersion})
	if err != nil {
		return Order{}, fmt.Errorf("resolve order pricing: %w", err)
	}
	catalogIndex := make(map[string]CatalogItem, len(catalogItems))
	for _, item := range latestCatalogVersions(catalogItems) {
		catalogIndex[catalogKey(item.ProductCode, item.Edition)] = item
	}

	priceVersion := draft.PriceListVersion
	totalCents := int64(0)
	for index := range draft.Items {
		item := &draft.Items[index]
		catalogItem, found := catalogIndex[catalogKey(item.ProductCode, item.Edition)]
		if found && priceVersion == "" && comparePriceVersion(catalogItem.PriceVersion, priceVersion) > 0 {
			priceVersion = catalogItem.PriceVersion
		}
		if item.Price == "" {
			if !found {
				return Order{}, invalidItemsf("items[%d]: product %s edition %s not found in price list %q",
					index, item.ProductCode, item.Edition, draft.PriceListVersion)
			}
			catalogPrice := catalogItem.AnnualPrice
			if draft.OrderType == OrderTypeIncrement {
				catalogPrice = catalogItem.IncrementPrice
			}
			cents, err := parseAmountCents(catalogPrice)
			if err != nil {
				return Order{}, invalidItemsf("items[%d].price from price list %s", index, err)
			}
			item.Price = centsToDecimal(cents)
		}
		cents, err := parseAmountCents(item.Price)
		if err != nil {
			return Order{}, invalidItemsf("items[%d].price %s", index, err)
		}
		if cents > maxOrderAmountCents-totalCents {
			return Order{}, invalidItemsf("total_amount must not exceed %g", maxCatalogAmount)
		}
		totalCents += cents
	}
	if draft.OrderType == OrderTypeGift {
		totalCents = 0
	}

	input := CreateOrderInput{
		TenantID:         draft.TenantID,
		OrderType:        draft.OrderType,
		Items:            draft.Items,
		TotalAmount:      centsToDecimal(totalCents),
		PayableAmount:    centsToDecimal(totalCents),
		Source:           draft.Source,
		SalesmanID:       draft.SalesmanID,
		ChannelID:        draft.ChannelID,
		Reason:           draft.Reason,
		ApprovedBy:       draft.ApprovedBy,
		PriceListVersion: priceVersion,
	}
	return s.insertOrderWithRetry(ctx, input)
}

// insertOrderWithRetry 生成 order_no 并落库；唯一索引冲突（同一秒内随机数撞车）换号重试一次。
func (s *Service) insertOrderWithRetry(ctx context.Context, input CreateOrderInput) (Order, error) {
	input.OrderNo = s.nextOrderNo()
	order, err := s.repo.InsertOrder(ctx, input)
	if errors.Is(err, ErrOrderNoExists) {
		input.OrderNo = s.nextOrderNo()
		order, err = s.repo.InsertOrder(ctx, input)
	}
	if err != nil {
		return Order{}, mapOrderError(err, "insert order")
	}
	return order, nil
}

// nextOrderNo 生成 BL + yyyyMMddHHmmss + 3 位随机数的订单号。
func (s *Service) nextOrderNo() string {
	suffix := 0
	if s.orderNoRand != nil {
		suffix = s.orderNoRand(1000)
	}
	if suffix < 0 {
		suffix = -suffix
	}
	return "BL" + s.clock().Format("20060102150405") + fmt.Sprintf("%03d", suffix%1000)
}

// ListOrders 分页查询订单，按 created_at DESC；筛选值先归一化再校验。
func (s *Service) ListOrders(ctx context.Context, query OrderQuery) (*OrderPage, error) {
	if query.TenantID < 0 {
		return nil, invalidf("tenant_id must be a positive integer")
	}
	query.Status = normalizeEnum(query.Status)
	if query.Status != "" && !containsString(OrderStatuses, query.Status) {
		return nil, invalidf("status must be one of %s", strings.Join(OrderStatuses, "/"))
	}
	query.OrderType = normalizeEnum(query.OrderType)
	if query.OrderType != "" && !containsString(OrderTypes, query.OrderType) {
		return nil, invalidf("order_type must be one of %s", strings.Join(OrderTypes, "/"))
	}
	query = NormalizeOrderQuery(query)

	items, total, err := s.repo.ListOrders(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	if items == nil {
		items = []Order{}
	}
	return &OrderPage{
		Items:    items,
		Total:    total,
		Page:     query.Page,
		PageSize: query.PageSize,
	}, nil
}

// GetOrder 查询订单详情（含 items）。
func (s *Service) GetOrder(ctx context.Context, orderID int64) (Order, error) {
	if orderID <= 0 {
		return Order{}, invalidf("order id is required")
	}
	order, err := s.repo.GetOrder(ctx, orderID)
	if err != nil {
		return Order{}, mapOrderError(err, "get order")
	}
	return order, nil
}

// SettleOrder 做回款确认（C2）：只允许 created，且 paid_amount 必须等于 payable_amount（全款）。
func (s *Service) SettleOrder(ctx context.Context, orderID int64, req SettleOrderRequest, adminID int64) (Order, error) {
	if orderID <= 0 {
		return Order{}, invalidf("order id is required")
	}
	if adminID <= 0 {
		return Order{}, invalidf("confirmed_by is required")
	}
	if req.PaidAmount == nil {
		return Order{}, invalidf("paid_amount is required")
	}
	paidCents, err := parseAmountCents(*req.PaidAmount)
	if err != nil {
		return Order{}, invalidf("paid_amount %s", err)
	}

	order, err := s.repo.GetOrder(ctx, orderID)
	if err != nil {
		return Order{}, mapOrderError(err, "get order")
	}
	if order.Status != OrderStatusCreated {
		return Order{}, ErrOrderStatusInvalid
	}
	payableCents, err := parseAmountCents(order.PayableAmount)
	if err != nil {
		return Order{}, fmt.Errorf("parse order payable amount: %w", err)
	}
	if paidCents != payableCents {
		return Order{}, ErrOrderAmountMismatch
	}

	paidAt := s.clock()
	if req.PaidAt != nil {
		paidAt = req.PaidAt.UTC()
	}
	settled, err := s.repo.SettleOrder(ctx, SettleOrderInput{
		OrderID:     orderID,
		PaidAmount:  centsToDecimal(paidCents),
		PaidAt:      paidAt,
		ConfirmedBy: adminID,
	})
	if err != nil {
		return Order{}, mapOrderError(err, "settle order")
	}
	return settled, nil
}

// ActivateOrder 开通订单（C3）：先抢订单行，再建服务期、发额度、写流水，全在一个事务里。
//
// 幂等的关键：service 只做前置校验，真正的"只能开通一次"由 store 事务里的
// UPDATE ... WHERE activated_at IS NULL AND status IN (...) 保证；影响 0 行即返回冲突，
// 绝不重复发额度。开通成功后返回该租户最新额度（复用 ListEntitlements）。
func (s *Service) ActivateOrder(ctx context.Context, orderID, adminID int64) (*ActivationResult, error) {
	if orderID <= 0 {
		return nil, invalidf("order id is required")
	}
	if adminID <= 0 {
		return nil, invalidf("activated_by is required")
	}

	order, err := s.repo.GetOrder(ctx, orderID)
	if err != nil {
		return nil, mapOrderError(err, "get order")
	}
	if order.ActivatedAt != nil {
		return nil, ErrOrderAlreadyActivated
	}
	allowedStatuses := allowedActivationStatuses(order.OrderType)
	if !containsString(allowedStatuses, order.Status) {
		return nil, ErrOrderStatusInvalid
	}
	grants, err := order.GrantLines()
	if err != nil {
		return nil, err
	}
	source, reason := activationLedgerMeta(order)

	activated, period, err := s.repo.ActivateOrder(ctx, ActivateOrderInput{
		OrderID:         order.ID,
		ActorID:         adminID,
		Operator:        s.resolveOperator(ctx, adminID),
		AllowedStatuses: allowedStatuses,
		Grants:          grants,
		LedgerSource:    source,
		Reason:          reason,
	})
	if err != nil {
		return nil, mapOrderError(err, "activate order")
	}

	entitlements, err := s.ListEntitlements(ctx, activated.TenantID, "", false)
	if err != nil {
		return nil, err
	}
	return &ActivationResult{
		Order:         activated,
		ServicePeriod: &period,
		Entitlements:  entitlements,
	}, nil
}

// RefundOrder 做开通前全额退款（C4）：只有 settled 且未开通的订单能退；
// 已开通返回 ORDER_ALREADY_ACTIVATED，其余状态返回 ORDER_STATUS_INVALID。
func (s *Service) RefundOrder(ctx context.Context, orderID, adminID int64) (Order, error) {
	if orderID <= 0 {
		return Order{}, invalidf("order id is required")
	}
	if adminID <= 0 {
		return Order{}, invalidf("operator is required")
	}

	order, err := s.repo.GetOrder(ctx, orderID)
	if err != nil {
		return Order{}, mapOrderError(err, "get order")
	}
	if order.ActivatedAt != nil {
		return Order{}, ErrOrderAlreadyActivated
	}
	if order.Status != OrderStatusSettled {
		return Order{}, ErrOrderStatusInvalid
	}

	refunded, err := s.repo.RefundOrder(ctx, RefundOrderInput{OrderID: orderID})
	if err != nil {
		return Order{}, mapOrderError(err, "refund order")
	}
	return refunded, nil
}

// activationLedgerMeta 返回开通时要写进流水的原因与 source。
func activationLedgerMeta(order Order) (source, reason string) {
	switch order.OrderType {
	case OrderTypeGift:
		reason = "赠送"
		if order.Reason != "" {
			reason += "：" + order.Reason
		}
		return LedgerSourceGift, reason + "（订单 " + order.OrderNo + "）"
	case OrderTypeTrial:
		reason = "试用开通"
		if order.Reason != "" {
			reason += "：" + order.Reason
		}
		return LedgerSourceTrial, reason + "（订单 " + order.OrderNo + "）"
	default:
		return LedgerSourcePurchase, "回款确认按单下发（订单 " + order.OrderNo + "）"
	}
}

// resolveOperator 取操作人展示名；查不到时退回 admin#<id>，不阻塞开通。
func (s *Service) resolveOperator(ctx context.Context, adminID int64) string {
	name, err := s.repo.GetAdminDisplayName(ctx, adminID)
	if err == nil {
		if name = strings.TrimSpace(name); name != "" {
			runes := []rune(name)
			if len(runes) > 64 {
				name = string(runes[:64])
			}
			return name
		}
	}
	return fmt.Sprintf("admin#%d", adminID)
}

// clock 返回可替换的当前时间（单测固定时间，保证 order_no 可断言）。
func (s *Service) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// catalogKey 是价目表按产品 + 档位建索引的键。
func catalogKey(productCode, edition string) string {
	return normalizeEnum(productCode) + "\x00" + normalizeEnum(edition)
}

// mapOrderError 把 store 错误还原成 service 层的 sentinel（handler 据此映射错误码），
// 未知错误包一层上下文交给 500。
func mapOrderError(err error, action string) error {
	for _, known := range []error{
		ErrOrderNotFound,
		ErrOrderNoExists,
		ErrOrderAmountMismatch,
		ErrOrderAlreadyActivated,
		ErrOrderStatusInvalid,
	} {
		if errors.Is(err, known) {
			return known
		}
	}
	var items *OrderItemsError
	if errors.As(err, &items) {
		return err
	}
	return fmt.Errorf("%s: %w", action, err)
}
