package his

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// 采购单状态：草稿 → 已下单 → 已入库；草稿/已下单可取消。
const (
	purchaseStatusDraft     = "draft"
	purchaseStatusOrdered   = "ordered"
	purchaseStatusReceived  = "received"
	purchaseStatusCancelled = "cancelled"
)

var purchaseStatuses = map[string]struct{}{
	purchaseStatusDraft:     {},
	purchaseStatusOrdered:   {},
	purchaseStatusReceived:  {},
	purchaseStatusCancelled: {},
}

// purchaseOrderNo 采购单号：PO + 日期 + 4 位流水（超过 4 位自然扩展）。
func purchaseOrderNo(seq int64, now time.Time) string {
	return fmt.Sprintf("PO%s%04d", now.Format("20060102"), seq)
}

// purchaseLineAmount 明细金额，四舍五入到分。
func purchaseLineAmount(quantity, price float64) float64 {
	return math.Round(quantity*price*100) / 100
}

// purchaseOrderTotal 采购单合计（按明细行先舍入再求和）。
func purchaseOrderTotal(items []CreatePurchaseOrderItem) float64 {
	total := 0.0
	for _, item := range items {
		total += purchaseLineAmount(item.Quantity, item.PurchasePrice)
	}
	return math.Round(total*100) / 100
}

// normalizePurchaseOrderInput 校验并规整新建采购单入参。
func normalizePurchaseOrderInput(req CreatePurchaseOrderRequest) (CreatePurchaseOrderRequest, error) {
	req.Supplier = strings.TrimSpace(req.Supplier)
	if req.Supplier == "" || len(req.Items) == 0 {
		return CreatePurchaseOrderRequest{}, ErrInvalidRequest
	}
	for index := range req.Items {
		item := &req.Items[index]
		item.BatchNo = strings.TrimSpace(item.BatchNo)
		if item.DrugID <= 0 || item.Quantity <= 0 || item.PurchasePrice < 0 {
			return CreatePurchaseOrderRequest{}, ErrInvalidRequest
		}
		if item.ExpiryDate == nil {
			continue
		}
		value := strings.TrimSpace(*item.ExpiryDate)
		if value == "" {
			item.ExpiryDate = nil
			continue
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return CreatePurchaseOrderRequest{}, ErrInvalidRequest
		}
		item.ExpiryDate = &value
	}
	return req, nil
}

// purchaseReceiveStatusError 校验收货状态：已入库单独报错，其余非法状态按状态流转拒绝。
func purchaseReceiveStatusError(status string) error {
	switch status {
	case purchaseStatusDraft, purchaseStatusOrdered:
		return nil
	case purchaseStatusReceived:
		return ErrPurchaseAlreadyReceived
	default:
		return ErrInvalidState
	}
}

// purchaseCancelStatusError 仅草稿/已下单可取消。
func purchaseCancelStatusError(status string) error {
	switch status {
	case purchaseStatusDraft, purchaseStatusOrdered:
		return nil
	default:
		return ErrInvalidState
	}
}

// validatePurchaseStatusFilter 校验列表状态筛选值。
func validatePurchaseStatusFilter(status string) error {
	if status == "" {
		return nil
	}
	if _, ok := purchaseStatuses[status]; !ok {
		return ErrInvalidRequest
	}
	return nil
}

// ListPurchaseOrders 采购单列表（?status=&keyword=）。
func (s *Service) ListPurchaseOrders(ctx context.Context, tenantID int64, status, keyword string) ([]PurchaseOrder, error) {
	if err := validatePurchaseStatusFilter(status); err != nil {
		return nil, err
	}
	return s.store.ListPurchaseOrders(ctx, tenantID, status, strings.TrimSpace(keyword))
}

// CreatePurchaseOrder 新建采购单（draft），金额按明细实时计算。
func (s *Service) CreatePurchaseOrder(ctx context.Context, tenantID, createdBy int64, req CreatePurchaseOrderRequest) (PurchaseOrder, error) {
	normalized, err := normalizePurchaseOrderInput(req)
	if err != nil {
		return PurchaseOrder{}, err
	}
	return s.store.CreatePurchaseOrder(ctx, tenantID, createdBy, normalized)
}

// ReceivePurchaseOrder 采购收货：逐行写库存与流水，订单置为已入库；重复收货拒绝。
func (s *Service) ReceivePurchaseOrder(ctx context.Context, tenantID, operatorID, orderID int64) (PurchaseOrder, error) {
	return s.store.ReceivePurchaseOrder(ctx, tenantID, operatorID, orderID)
}

// CancelPurchaseOrder 取消采购单（仅 draft/ordered）。
func (s *Service) CancelPurchaseOrder(ctx context.Context, tenantID, orderID int64) (PurchaseOrder, error) {
	return s.store.CancelPurchaseOrder(ctx, tenantID, orderID)
}
