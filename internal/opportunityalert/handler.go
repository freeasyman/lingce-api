package opportunityalert

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterConfigRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)
	mux.Handle("GET /api/v1/opportunity-alert-cc-rules", authMw(http.HandlerFunc(h.ListCCRules)))
	mux.Handle("POST /api/v1/opportunity-alert-cc-rules", authMw(http.HandlerFunc(h.CreateCCRule)))
	mux.Handle("DELETE /api/v1/opportunity-alert-cc-rules/{id}", authMw(http.HandlerFunc(h.DeleteCCRule)))
	mux.Handle("GET /api/v1/opportunity-alert-deliveries/recent", authMw(http.HandlerFunc(h.ListRecentDeliveries)))
	mux.Handle("GET /api/v1/ops/opportunity-alerts", authMw(http.HandlerFunc(h.ListAdminAlerts)))
	mux.Handle("GET /api/v1/ops/opportunity-alerts/{id}", authMw(http.HandlerFunc(h.GetAdminAlert)))
	mux.Handle("GET /api/v1/ops/opportunity-alerts/{id}/recipients", authMw(http.HandlerFunc(h.ListAdminAlertRecipients)))
	mux.Handle("GET /api/v1/ops/opportunity-alerts/{id}/deliveries", authMw(http.HandlerFunc(h.ListAdminAlertDeliveries)))
	mux.Handle("GET /api/v1/ops/opportunity-alerts/{id}/logs", authMw(http.HandlerFunc(h.ListAdminAlertLogs)))
	mux.Handle("POST /api/v1/ops/opportunity-alerts/{id}/resend", authMw(http.HandlerFunc(h.ResendAlert)))
	mux.Handle("POST /api/v1/ops/opportunity-alerts/{id}/recipients/{employee_id}/resend", authMw(http.HandlerFunc(h.ResendAlertRecipient)))
}

func (h *Handler) ListRecentDeliveries(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	tenantID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("tenant_id")), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var scopedTenantID *int64
	if tenantID > 0 {
		scopedTenantID = &tenantID
	}
	items, err := h.service.ListRecentDeliveriesForAdmin(r.Context(), claims, RecentDeliveriesRequest{
		TenantID: scopedTenantID,
		Limit:    limit,
	})
	if err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) RegisterMobileRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)
	mux.Handle("GET /api/v1/mobile/opportunity-alerts", authMw(http.HandlerFunc(h.ListMobileAlerts)))
	mux.Handle("GET /api/v1/mobile/opportunity-alerts/{id}", authMw(http.HandlerFunc(h.GetMobileAlert)))
	mux.Handle("POST /api/v1/mobile/opportunity-alerts/{id}/actions/view", authMw(http.HandlerFunc(h.MarkViewed)))
	mux.Handle("POST /api/v1/mobile/opportunity-alerts/{id}/actions/handle", authMw(http.HandlerFunc(h.MarkHandled)))
	mux.Handle("POST /api/v1/mobile/opportunity-alerts/{id}/actions/ignore", authMw(http.HandlerFunc(h.MarkIgnored)))
}

func (h *Handler) RegisterInternalRoutes(mux *http.ServeMux, internalToken string) {
	router.Register(mux, []router.Route{
		{
			Method:   "POST",
			Path:     "/api/v1/internal/opportunity-alerts/from-recording",
			Handler:  h.CreateFromRecordingInternal,
			AuthMode: "internal",
		},
	}, router.RouteDeps{InternalToken: strings.TrimSpace(internalToken)})
}

func (h *Handler) CreateFromRecordingInternal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RecordingID   int64  `json:"recording_id"`
		TriggerSource string `json:"trigger_source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if req.RecordingID <= 0 {
		httputil.WriteBadRequest(w, "recording_id is required")
		return
	}
	triggerSource := strings.TrimSpace(req.TriggerSource)
	if triggerSource == "" {
		triggerSource = "internal"
	}
	if err := h.service.CreateFromRecording(r.Context(), req.RecordingID, triggerSource); err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"recording_id": req.RecordingID, "status": "processed"})
}

func (h *Handler) ListCCRules(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	tenantID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("tenant_id")), 10, 64)
	var employeeID *int64
	if raw := strings.TrimSpace(r.URL.Query().Get("employee_id")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			httputil.WriteBadRequest(w, "invalid employee_id")
			return
		}
		employeeID = &parsed
	}
	items, err := h.service.ListCCRules(r.Context(), claims, tenantID, employeeID)
	if err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) CreateCCRule(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	var req CreateCCRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.CreateCCRule(r.Context(), claims, req)
	if err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) DeleteCCRule(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid cc rule id")
		return
	}
	tenantID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("tenant_id")), 10, 64)
	if err := h.service.DeleteCCRule(r.Context(), claims, tenantID, id); err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "cc rule deleted"})
}

func (h *Handler) ResendAlert(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "admin access required")
		return
	}
	alertID, ok := parseAlertID(w, r)
	if !ok {
		return
	}
	var req ResendAlertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if err := h.service.ResendWeCom(r.Context(), alertID, req.EmployeeIDs); err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"alert_id": alertID, "status": "resent"})
}

func (h *Handler) ListAdminAlerts(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "admin access required")
		return
	}
	var tenantID *int64
	if raw := strings.TrimSpace(r.URL.Query().Get("tenant_id")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			httputil.WriteBadRequest(w, "invalid tenant_id")
			return
		}
		tenantID = &parsed
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	items, total, err := h.service.ListForAdmin(r.Context(), claims, AdminListRequest{
		TenantID: tenantID,
		Status:   normalizeStatus(r.URL.Query().Get("status")),
		Keyword:  strings.TrimSpace(r.URL.Query().Get("keyword")),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		httpError(w, err)
		return
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	httputil.WritePaginated(w, items, int64(total), page, pageSize)
}

func (h *Handler) GetAdminAlert(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	alertID, ok := parseAlertID(w, r)
	if !ok {
		return
	}
	item, err := h.service.GetForAdmin(r.Context(), claims, alertID)
	if err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) ListAdminAlertRecipients(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	alertID, ok := parseAlertID(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListRecipientsForAdmin(r.Context(), claims, alertID)
	if err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) ListAdminAlertDeliveries(w http.ResponseWriter, r *http.Request) {
	h.ListAdminAlertRecipients(w, r)
}

func (h *Handler) ListAdminAlertLogs(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	alertID, ok := parseAlertID(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListDeliveryLogsForAdmin(r.Context(), claims, alertID)
	if err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) ResendAlertRecipient(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "admin access required")
		return
	}
	alertID, ok := parseAlertID(w, r)
	if !ok {
		return
	}
	employeeID, err := strconv.ParseInt(r.PathValue("employee_id"), 10, 64)
	if err != nil || employeeID <= 0 {
		httputil.WriteBadRequest(w, "invalid employee_id")
		return
	}
	if err := h.service.ResendWeCom(r.Context(), alertID, []int64{employeeID}); err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"alert_id": alertID, "employee_id": employeeID, "status": "resent"})
}

func (h *Handler) ListMobileAlerts(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	items, total, err := h.service.ListForEmployee(r.Context(), claims, ListRequest{
		Status:   normalizeStatus(r.URL.Query().Get("status")),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		httpError(w, err)
		return
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	httputil.WritePaginated(w, items, int64(total), page, pageSize)
}

func (h *Handler) GetMobileAlert(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	alertID, ok := parseAlertID(w, r)
	if !ok {
		return
	}
	item, err := h.service.GetForEmployee(r.Context(), claims, alertID)
	if err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) MarkViewed(w http.ResponseWriter, r *http.Request) {
	h.handleAction(w, r, h.service.MarkViewed)
}

func (h *Handler) MarkHandled(w http.ResponseWriter, r *http.Request) {
	h.handleAction(w, r, h.service.MarkHandled)
}

func (h *Handler) MarkIgnored(w http.ResponseWriter, r *http.Request) {
	h.handleAction(w, r, h.service.MarkIgnored)
}

func (h *Handler) handleAction(w http.ResponseWriter, r *http.Request, action func(context.Context, *auth.Claims, int64) (*AlertResponse, error)) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	alertID, ok := parseAlertID(w, r)
	if !ok {
		return
	}
	var req ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := action(r.Context(), claims, alertID)
	if err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

func parseAlertID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	alertID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || alertID <= 0 {
		httputil.WriteBadRequest(w, "invalid opportunity alert id")
		return 0, false
	}
	return alertID, true
}

func requireEmployeeClaims(w http.ResponseWriter, r *http.Request) (*auth.Claims, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return nil, false
	}
	switch claims.UserType {
	case auth.UserTypeEmployee, auth.UserTypeMobile:
		return claims, true
	default:
		httputil.WriteForbidden(w, "forbidden")
		return nil, false
	}
}

func httpError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "not found") {
		httputil.WriteNotFound(w, msg)
		return
	}
	if strings.Contains(msg, "forbidden") || strings.Contains(msg, "only alert owner") {
		httputil.WriteForbidden(w, msg)
		return
	}
	httputil.WriteBadRequest(w, msg)
}
