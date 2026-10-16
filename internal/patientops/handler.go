package patientops

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret, internalToken string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/patient-operations/screenshots/unlinked", Handler: h.ListUnlinkedSources, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/patient-operations/screenshots/unlinked/{id}/detail", Handler: h.GetUnlinkedSource, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/screenshots/analyze", Handler: h.AnalyzeAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/patient-operations/screenshots/{id}/status", Handler: h.StatusAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/screenshots/{id}/confirm", Handler: h.ConfirmAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/screenshots/{id}/prospect", Handler: h.CreateProspectAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/screenshots/{id}/actions/generate", Handler: h.GenerateScreenshotActionAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/patient-operations/customers/{id}/sources", Handler: h.ListCustomerSources, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/patient-operations/customers/{id}/records", Handler: h.ListOperationRecords, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/customers/{id}/actions", Handler: h.SaveActionAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/customers/{id}/actions/generate", Handler: h.GenerateActionAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/internal/patient-operations/screenshots/analyze", Handler: h.Analyze, AuthMode: "internal"},
	}, router.RouteDeps{JWTSecret: jwtSecret, InternalToken: strings.TrimSpace(internalToken)})
}

func (h *Handler) GenerateActionAuthenticated(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	customerID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || customerID <= 0 {
		httputil.WriteBadRequest(w, "invalid customer id")
		return
	}
	var req GenerateActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	result, err := h.service.GenerateAction(r.Context(), *claims.TenantID, claims.UserID, customerID, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

func (h *Handler) GenerateScreenshotActionAuthenticated(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	sourceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || sourceID <= 0 {
		httputil.WriteBadRequest(w, "invalid source id")
		return
	}
	var req GenerateActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	req.SourceID = FlexibleInt64(sourceID)
	result, err := h.service.GenerateAction(r.Context(), *claims.TenantID, claims.UserID, 0, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

func (h *Handler) GetUnlinkedSource(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	sourceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || sourceID <= 0 {
		httputil.WriteBadRequest(w, "invalid source id")
		return
	}
	item, err := h.service.GetUnlinkedSource(r.Context(), *claims.TenantID, sourceID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) ListUnlinkedSources(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	page, pageSize := 1, 20
	for key, target := range map[string]*int{"page": &page, "page_size": &pageSize} {
		if raw := r.URL.Query().Get(key); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || (key == "page_size" && value > 100) {
				httputil.WriteBadRequest(w, "invalid "+key)
				return
			}
			*target = value
		}
	}
	items, err := h.service.ListUnlinkedSources(r.Context(), *claims.TenantID, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) ListOperationRecords(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	customerID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || customerID <= 0 {
		httputil.WriteBadRequest(w, "invalid customer id")
		return
	}
	items, err := h.service.ListOperationRecords(r.Context(), *claims.TenantID, customerID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) StatusAuthenticated(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	sourceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || sourceID <= 0 {
		httputil.WriteBadRequest(w, "invalid source id")
		return
	}
	result, err := h.service.GetScreenshotStatus(r.Context(), *claims.TenantID, sourceID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

func (h *Handler) SaveActionAuthenticated(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	customerID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || customerID <= 0 {
		httputil.WriteBadRequest(w, "invalid customer id")
		return
	}
	var req ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	result, err := h.service.SaveAction(r.Context(), *claims.TenantID, claims.UserID, customerID, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

func (h *Handler) CreateProspectAuthenticated(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	sourceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || sourceID <= 0 {
		httputil.WriteBadRequest(w, "invalid source id")
		return
	}
	var req CreateProspectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	customerID, err := h.service.CreateProspect(r.Context(), *claims.TenantID, claims.UserID, sourceID, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"customer_id": customerID, "source_id": sourceID, "match_status": "confirmed"})
}

func (h *Handler) ListCustomerSources(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	customerID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || customerID <= 0 {
		httputil.WriteBadRequest(w, "invalid customer id")
		return
	}
	items, err := h.service.ListCustomerSources(r.Context(), *claims.TenantID, customerID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) ConfirmAuthenticated(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	var req struct {
		CustomerID int64 `json:"customer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID <= 0 {
		httputil.WriteBadRequest(w, "customer_id is required")
		return
	}
	sourceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || sourceID <= 0 {
		httputil.WriteBadRequest(w, "invalid source id")
		return
	}
	if err := h.service.ConfirmScreenshot(r.Context(), *claims.TenantID, sourceID, req.CustomerID); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"source_id": sourceID, "customer_id": req.CustomerID, "match_status": "confirmed"})
}

func (h *Handler) AnalyzeAuthenticated(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
		return
	}
	var req ScreenshotAnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	req.TenantID = *claims.TenantID
	req.EmployeeID = claims.UserID
	h.analyze(w, r, req)
}
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	var req ScreenshotAnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	h.analyze(w, r, req)
}

func (h *Handler) analyze(w http.ResponseWriter, r *http.Request, req ScreenshotAnalyzeRequest) {
	resp, err := h.service.AnalyzeScreenshot(r.Context(), req)
	if err != nil {
		slog.Error("patient screenshot analyze failed", "tenant_id", req.TenantID, "employee_id", req.EmployeeID, "error", err)
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}
