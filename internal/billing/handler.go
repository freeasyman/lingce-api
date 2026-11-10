package billing

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// maxCatalogBodyBytes 限制价目表请求体大小：一条价目最多几 KB，1MB 足够且不会放大异常请求。
const maxCatalogBodyBytes = 1 << 20

// maxDeductBodyBytes 限制扣量请求体大小：一次交付最多几十个单位，1MB 足够。
const maxDeductBodyBytes = 1 << 20

// Handler 暴露计费查询接口与运营端订单接口（价目表新增版本、录单、回款、开通、退款是写入口）。
type Handler struct{ service *Service }

// NewHandler 构造 HTTP handler。
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// RegisterRoutes 注册计费路由：运营端只给 admin，机构端给 admin + employee。
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret, internalToken string) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/ops/billing/catalog", Handler: h.ListCatalog, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/ops/billing/catalog", Handler: h.CreateCatalogVersion, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/entitlements", Handler: h.ListEntitlements, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/entitlement-ledger", Handler: h.ListLedger, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	routes = append(routes, h.orderRoutes()...)
	routes = append(routes, h.internalRoutes()...)
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret, InternalToken: strings.TrimSpace(internalToken)})
}

// ListCatalog 查询价目表。
func (h *Handler) ListCatalog(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := CatalogFilter{
		ProductCode:  query.Get("product_code"),
		Edition:      query.Get("edition"),
		PriceVersion: query.Get("price_version"),
	}
	items, err := h.service.ListCatalog(r.Context(), filter)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	httputil.WriteSuccess(w, CatalogListResponse{Items: items})
}

// CreateCatalogVersion 新增一个价目版本（不改历史版本）。
func (h *Handler) CreateCatalogVersion(w http.ResponseWriter, r *http.Request) {
	var req CreateCatalogRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxCatalogBodyBytes)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.CreateCatalogVersion(r.Context(), req)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}

// ListEntitlements 查询租户额度余额与服务期。
func (h *Handler) ListEntitlements(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantAccess(w, r)
	if !ok {
		return
	}
	includeZero := false
	if raw := strings.TrimSpace(r.URL.Query().Get("include_zero")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			httputil.WriteBadRequest(w, "include_zero must be a boolean")
			return
		}
		includeZero = parsed
	}

	items, err := h.service.ListEntitlements(r.Context(), tenantID, r.URL.Query().Get("product"), includeZero)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	period, err := h.service.GetServicePeriod(r.Context(), tenantID)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	httputil.WriteSuccess(w, EntitlementListResponse{Items: items, ServicePeriod: period})
}

// ListLedger 分页查询租户额度流水。
func (h *Handler) ListLedger(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantAccess(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	ledgerQuery := LedgerQuery{
		TenantID: tenantID,
		Product:  query.Get("product"),
		Unit:     query.Get("unit"),
		Page:     parseQueryInt(query.Get("page")),
		PageSize: parseQueryInt(query.Get("page_size")),
	}
	from, err := parseTimeQuery("from", query.Get("from"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	to, err := parseTimeQuery("to", query.Get("to"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	ledgerQuery.From, ledgerQuery.To = from, to

	page, err := h.service.ListLedger(r.Context(), ledgerQuery)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	httputil.WriteSuccess(w, page)
}

// requireTenantAccess 校验调用者有权访问路径里的租户。
//
// 沿用仓库既有做法（见 internal/tenant/handler.go）：admin 可跨租户查询任意 tenant，
// employee 只能访问 JWT 绑定的本租户，否则 403，避免越权读别家余额与流水。
func requireTenantAccess(w http.ResponseWriter, r *http.Request) (int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return 0, false
	}
	tenantID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || tenantID <= 0 {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return 0, false
	}
	if err := tenancy.RequireSameTenant(claims, tenantID); err != nil {
		httputil.WriteForbidden(w, "No tenant access")
		return 0, false
	}
	return tenantID, true
}

// parseQueryInt 解析可选的整数 query 参数；缺省或非法时返回 0，由 service 层按默认值收敛。
func parseQueryInt(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}

// parseTimeQuery 解析 RFC3339 时间或 YYYY-MM-DD 日期（按 UTC 零点理解）。
func parseTimeQuery(field, raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return &parsed, nil
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		return &parsed, nil
	}
	return nil, fmt.Errorf("%s must be RFC3339 (e.g. 2026-11-01T00:00:00Z) or YYYY-MM-DD", field)
}

// writeBillingError 把 service/store 错误映射成仓库既有的错误响应格式。
func writeBillingError(w http.ResponseWriter, err error) {
	var (
		validation *ValidationError
		items      *OrderItemsError
	)
	switch {
	case errors.As(err, &validation):
		httputil.WriteBadRequest(w, validation.Message)
	case errors.As(err, &items):
		httputil.WriteError(w, http.StatusBadRequest, "ORDER_ITEMS_INVALID", items.Message, nil)
	case errors.Is(err, ErrOrderNotFound):
		httputil.WriteError(w, http.StatusNotFound, "ORDER_NOT_FOUND", "订单不存在", nil)
	case errors.Is(err, ErrOrderAmountMismatch):
		httputil.WriteError(w, http.StatusBadRequest, "ORDER_AMOUNT_MISMATCH", "回款金额必须等于应付金额（MVP 只支持全款确认）", nil)
	case errors.Is(err, ErrOrderAlreadyActivated):
		httputil.WriteError(w, http.StatusConflict, "ORDER_ALREADY_ACTIVATED", "订单已开通，不能重复开通", nil)
	case errors.Is(err, ErrOrderStatusInvalid):
		httputil.WriteError(w, http.StatusConflict, "ORDER_STATUS_INVALID", "订单当前状态不允许该操作", nil)
	case errors.Is(err, ErrGiftApprovalRequired):
		httputil.WriteError(w, http.StatusBadRequest, "GIFT_APPROVAL_REQUIRED", "赠送超过 1 个增量包，需要老板审批", nil)
	case errors.Is(err, ErrNoSubscription):
		httputil.WriteError(w, http.StatusConflict, "NO_SUBSCRIPTION", "租户尚未开通服务期，请先开通后再续费", nil)
	case errors.Is(err, ErrCatalogVersionExists):
		httputil.WriteError(w, http.StatusConflict, "CATALOG_VERSION_EXISTS", "该产品档位的价目版本已存在，请使用新的 price_version", nil)
	default:
		httputil.WriteInternalError(w, err.Error())
	}
}

// internalRoutes 是跨进程内部接口（D0）：worker 与 api 都能扣量/预检，
// 认证方式沿用仓库既有的 AuthMode=internal（X-Internal-Token），不校验 JWT。
func (h *Handler) internalRoutes() []router.Route {
	return []router.Route{
		{Method: "POST", Path: "/api/v1/internal/billing/deduct", Handler: h.InternalDeduct, AuthMode: "internal"},
		{Method: "POST", Path: "/api/v1/internal/billing/check-quota", Handler: h.InternalCheckQuota, AuthMode: "internal"},
	}
}

// InternalDeduct 执行一次幂等扣量：锁定额度行 → 写 usage 流水 → 更新余额 → 返回明细。
//
// 成功 200；幂等键已扣过 409 ALREADY_DEDUCTED（调用方视为成功，不再重试）；
// 额度不足 409 INSUFFICIENT_ENTITLEMENT（details 带 unit/current/required/shortfall）；
// 入参不合法 400 INVALID_REQUEST。
func (h *Handler) InternalDeduct(w http.ResponseWriter, r *http.Request) {
	var input DeductInput
	if err := json.NewDecoder(io.LimitReader(r.Body, maxDeductBodyBytes)).Decode(&input); err != nil {
		writeInternalBillingError(w, invalidf("invalid request body"))
		return
	}
	result, err := h.service.Deduct(r.Context(), input)
	if err != nil {
		writeInternalBillingError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// InternalCheckQuota 是只读额度预检：够返回 200 {"ok":true}，不够返回
// 409 INSUFFICIENT_ENTITLEMENT（details 同扣量接口），不写任何流水与余额。
func (h *Handler) InternalCheckQuota(w http.ResponseWriter, r *http.Request) {
	var input DeductInput
	if err := json.NewDecoder(io.LimitReader(r.Body, maxDeductBodyBytes)).Decode(&input); err != nil {
		writeInternalBillingError(w, invalidf("invalid request body"))
		return
	}
	if err := h.service.CheckQuota(r.Context(), input.TenantID, input.ProductCode, input.Items); err != nil {
		writeInternalBillingError(w, err)
		return
	}
	httputil.WriteSuccess(w, CheckQuotaResponse{OK: true})
}

// writeInternalBillingError 是内部扣量接口的错误映射：
// 扣量/预检的约定与运营端不同（INVALID_REQUEST / ALREADY_DEDUCTED / INSUFFICIENT_ENTITLEMENT），
// 其余错误继续走 writeBillingError 的通用映射。
func writeInternalBillingError(w http.ResponseWriter, err error) {
	var validation *ValidationError
	if errors.As(err, &validation) {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", validation.Message, nil)
		return
	}
	var insufficient *InsufficientEntitlementError
	if errors.As(err, &insufficient) {
		httputil.WriteError(w, http.StatusConflict, "INSUFFICIENT_ENTITLEMENT", "额度不足", map[string]any{
			"unit":      insufficient.Unit,
			"current":   insufficient.Current,
			"required":  insufficient.Required,
			"shortfall": insufficient.Shortfall,
		})
		return
	}
	if errors.Is(err, ErrAlreadyDeducted) {
		httputil.WriteError(w, http.StatusConflict, "ALREADY_DEDUCTED", "该幂等键已扣量，请勿重复提交", nil)
		return
	}
	writeBillingError(w, err)
}
