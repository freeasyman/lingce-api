package his

import (
	"net/http"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ListPurchaseOrders 采购单列表（?status=&keyword=）。
func (h *Handler) ListPurchaseOrders(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	orders, err := h.service.ListPurchaseOrders(r.Context(), tenantID,
		r.URL.Query().Get("status"), r.URL.Query().Get("keyword"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, orders)
}

// CreatePurchaseOrder 新建采购单（草稿）。
func (h *Handler) CreatePurchaseOrder(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	var req CreatePurchaseOrderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	order, err := h.service.CreatePurchaseOrder(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, order)
}

// ReceivePurchaseOrder 采购收货入库（重复收货返回 409）。
func (h *Handler) ReceivePurchaseOrder(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	orderID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	order, err := h.service.ReceivePurchaseOrder(r.Context(), tenantID, claims.UserID, orderID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, order)
}

// CancelPurchaseOrder 取消采购单（仅 draft/ordered）。
func (h *Handler) CancelPurchaseOrder(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	orderID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	order, err := h.service.CancelPurchaseOrder(r.Context(), tenantID, orderID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, order)
}
