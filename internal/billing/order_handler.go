package billing

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// maxOrderBodyBytes 限制订单请求体大小：一单最多几十行 items，1MB 足够。
const maxOrderBodyBytes = 1 << 20

// orderRoutes 是阶段 C 的运营端订单路由：录单/列表/详情/回款确认/开通/退款，全部只允许 admin。
func (h *Handler) orderRoutes() []router.Route {
	return []router.Route{
		{Method: "POST", Path: "/api/v1/ops/orders", Handler: h.CreateOrder, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/ops/orders", Handler: h.ListOrders, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/ops/orders/{id}", Handler: h.GetOrder, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/ops/orders/{id}/settle", Handler: h.SettleOrder, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/ops/orders/{id}/activate", Handler: h.ActivateOrder, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/ops/orders/{id}/refund", Handler: h.RefundOrder, Auth: true, AllowedUserTypes: []string{"admin"}},
	}
}

// CreateOrder 录入订单（C1）。
func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxOrderBodyBytes)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	order, err := h.service.CreateOrder(r.Context(), req)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, order)
}

// ListOrders 分页查询订单（按 created_at DESC）。
func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	tenantID := int64(0)
	if raw := strings.TrimSpace(query.Get("tenant_id")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			httputil.WriteBadRequest(w, "tenant_id must be a positive integer")
			return
		}
		tenantID = parsed
	}
	page, err := h.service.ListOrders(r.Context(), OrderQuery{
		TenantID:  tenantID,
		Status:    query.Get("status"),
		OrderType: query.Get("order_type"),
		Page:      parseQueryInt(query.Get("page")),
		PageSize:  parseQueryInt(query.Get("page_size")),
	})
	if err != nil {
		writeBillingError(w, err)
		return
	}
	httputil.WriteSuccess(w, page)
}

// GetOrder 查询订单详情（含 items）。
func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	orderID, ok := requireOrderID(w, r)
	if !ok {
		return
	}
	order, err := h.service.GetOrder(r.Context(), orderID)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	httputil.WriteSuccess(w, order)
}

// SettleOrder 回款确认（C2）。confirmed_by 取 JWT 用户 id，不接受客户端指定。
func (h *Handler) SettleOrder(w http.ResponseWriter, r *http.Request) {
	orderID, ok := requireOrderID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req SettleOrderRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxOrderBodyBytes)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	order, err := h.service.SettleOrder(r.Context(), orderID, req, adminID)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	httputil.WriteSuccess(w, order)
}

// ActivateOrder 开通订单（C3）：建服务期 + 按档位发额度，返回最新额度。
func (h *Handler) ActivateOrder(w http.ResponseWriter, r *http.Request) {
	orderID, ok := requireOrderID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	result, err := h.service.ActivateOrder(r.Context(), orderID, adminID)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// RefundOrder 开通前全额退款（C4）。
//
// 已开通按接口约定返回 400 ORDER_ALREADY_ACTIVATED（开通接口本身是 409），
// 因此这里在通用错误映射前单独处理。
func (h *Handler) RefundOrder(w http.ResponseWriter, r *http.Request) {
	orderID, ok := requireOrderID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	order, err := h.service.RefundOrder(r.Context(), orderID, adminID)
	if err != nil {
		if errors.Is(err, ErrOrderAlreadyActivated) {
			httputil.WriteError(w, http.StatusBadRequest, "ORDER_ALREADY_ACTIVATED", "订单已开通，不能退款", nil)
			return
		}
		writeBillingError(w, err)
		return
	}
	httputil.WriteSuccess(w, order)
}

// requireOrderID 解析路径里的订单 id。
func requireOrderID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	orderID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || orderID <= 0 {
		httputil.WriteBadRequest(w, "Invalid order ID")
		return 0, false
	}
	return orderID, true
}

// requireAdminActor 取 JWT 里的操作人 id（confirmed_by / activated_by / operator）。
func requireAdminActor(w http.ResponseWriter, r *http.Request) (int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return 0, false
	}
	if claims.UserID <= 0 {
		httputil.WriteForbidden(w, "Invalid operator")
		return 0, false
	}
	return claims.UserID, true
}
