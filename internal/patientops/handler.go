package patientops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret, internalToken string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/patient-operations/screenshots/unlinked/count", Handler: h.CountUnlinkedSources, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/patient-operations/screenshots/unlinked", Handler: h.ListUnlinkedSources, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/patient-operations/screenshots/unlinked/{id}/detail", Handler: h.GetUnlinkedSource, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/screenshots/analyze", Handler: h.AnalyzeAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/patient-operations/screenshots/{id}/status", Handler: h.StatusAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/screenshots/{id}/confirm", Handler: h.ConfirmAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/screenshots/{id}/prospect", Handler: h.CreateProspectAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/screenshots/{id}/actions/generate", Handler: h.GenerateScreenshotActionAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/patient-operations/customers/{id}/sources", Handler: h.ListCustomerSources, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/patient-operations/customers/{id}/records", Handler: h.ListOperationRecords, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/patient-operations/customers/{id}/tasks", Handler: h.ListCustomerTasks, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/patient-operations/customers/task-summary", Handler: h.ListCustomerTaskSummary, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/patient-operations/tasks", Handler: h.ListTasksAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/tasks/{id}/complete", Handler: h.CompleteTaskAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/tasks/{id}/status", Handler: h.UpdateTaskStatusAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/customers/{id}/actions", Handler: h.SaveActionAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/patient-operations/customers/{id}/actions/generate", Handler: h.GenerateActionAuthenticated, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/internal/patient-operations/screenshots/analyze", Handler: h.Analyze, AuthMode: "internal"},
		{Method: "POST", Path: "/api/v1/internal/patient-operations/recordings/analyze", Handler: h.AnalyzeRecording, AuthMode: "internal"},
	}, router.RouteDeps{JWTSecret: jwtSecret, InternalToken: strings.TrimSpace(internalToken)})
}

func (h *Handler) CountUnlinkedSources(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	count, err := h.service.CountUnlinkedSources(r.Context(), *claims.TenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]int64{"count": count})
}

func (h *Handler) AnalyzeRecording(w http.ResponseWriter, r *http.Request) {
	var req RecordingAnalyzeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 2*1024*1024)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if err := validateRecordingAnalyzeRequest(req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	result, err := h.service.AcceptRecordingAnalysis(r.Context(), req)
	if err != nil {
		if errors.Is(err, ErrRecordingAnalysisInvalidReference) {
			httputil.WriteError(w, http.StatusUnprocessableEntity, "INVALID_REFERENCE", err.Error(), nil)
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

func validateRecordingAnalyzeRequest(req RecordingAnalyzeRequest) error {
	if req.TenantID <= 0 || req.RecordingID <= 0 || req.EncounterID <= 0 || req.EmployeeID <= 0 || req.CustomerID <= 0 {
		return fmt.Errorf("tenant_id, recording_id, encounter_id, employee_id and customer_id are required")
	}
	if strings.TrimSpace(req.CustomerName) == "" || strings.TrimSpace(req.RecordedAt) == "" || strings.TrimSpace(req.CleanedTranscript) == "" {
		return fmt.Errorf("customer_name, recorded_at and cleaned_transcript are required")
	}
	return nil
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
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		httputil.WriteBadRequest(w, "read request body: "+err.Error())
		return
	}
	body = bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}))
	if len(body) == 0 {
		httputil.WriteBadRequest(w, "request body is empty")
		return
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		httputil.WriteBadRequest(w, "invalid request body: "+err.Error())
		return
	}
	if nested, ok := envelope["data"]; ok {
		body = nested
	} else if nested, ok := envelope["payload"]; ok {
		body = nested
	}
	var req GenerateActionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		httputil.WriteBadRequest(w, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	if req.Action == "" {
		httputil.WriteBadRequest(w, "action is required")
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

func (h *Handler) CompleteTaskAuthenticated(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid task id")
		return
	}
	var req CompletePatientTaskRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil || strings.TrimSpace(req.Result) == "" {
		httputil.WriteBadRequest(w, "result is required")
		return
	}
	if err := h.service.CompletePatientTask(r.Context(), *claims.TenantID, claims.UserID, id, req); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"status": "已完成"})
}

func (h *Handler) UpdateTaskStatusAuthenticated(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid task id")
		return
	}
	var req UpdatePatientTaskRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if err := h.service.UpdatePatientTaskStatus(r.Context(), *claims.TenantID, claims.UserID, id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"status": req.Status, "task_id": id})
}

func (h *Handler) ListTasksAuthenticated(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	page, pageSize := 1, 50
	filter := strings.TrimSpace(r.URL.Query().Get("filter"))
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && v > 0 && v <= 100 {
		pageSize = v
	}
	items, total, err := h.service.ListPatientTasks(r.Context(), *claims.TenantID, claims.UserID, page, pageSize, claims.UserType == auth.UserTypeAdmin, filter)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, total, page, pageSize)
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

func (h *Handler) ListCustomerTasks(w http.ResponseWriter, r *http.Request) {
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
	items, err := h.service.ListCustomerTasks(r.Context(), *claims.TenantID, customerID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) ListCustomerTaskSummary(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	items, err := h.service.ListCustomerTaskSummary(r.Context(), *claims.TenantID, claims.UserID, claims.UserType == auth.UserTypeAdmin)
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
		if errors.Is(err, ErrScreenshotCustomerConflict) {
			httputil.WriteError(w, http.StatusConflict, "SCREENSHOT_CUSTOMER_CONFLICT", "这张截图已经关联了其他客户，请重新选择截图。", nil)
			return
		}
		slog.Error("patient screenshot analyze failed", "tenant_id", req.TenantID, "employee_id", req.EmployeeID, "error", err)
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}
