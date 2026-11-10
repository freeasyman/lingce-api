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

// Handler 暴露计费查询接口（价目表新增版本是唯一写入口）。
type Handler struct{ service *Service }

// NewHandler 构造 HTTP handler。
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// RegisterRoutes 注册计费路由：运营端只给 admin，机构端给 admin + employee。
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret, internalToken string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/ops/billing/catalog", Handler: h.ListCatalog, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/ops/billing/catalog", Handler: h.CreateCatalogVersion, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/entitlements", Handler: h.ListEntitlements, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/entitlement-ledger", Handler: h.ListLedger, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret, InternalToken: strings.TrimSpace(internalToken)})
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
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		httputil.WriteBadRequest(w, validation.Message)
	case errors.Is(err, ErrCatalogVersionExists):
		httputil.WriteError(w, http.StatusConflict, "CATALOG_VERSION_EXISTS", "该产品档位的价目版本已存在，请使用新的 price_version", nil)
	default:
		httputil.WriteInternalError(w, err.Error())
	}
}
