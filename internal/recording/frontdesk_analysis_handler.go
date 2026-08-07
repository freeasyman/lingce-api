package recording

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

func getFrontdeskTenantID(h *Handler, r *http.Request) (int64, int) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		return 0, http.StatusUnauthorized
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		return 0, http.StatusBadRequest
	}
	return tenantID, 0
}

// Helper function to respond with JSON
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	httputil.WriteJSON(w, status, data)
}

// Helper function to respond with error
func respondError(w http.ResponseWriter, status int, message string) {
	switch status {
	case http.StatusBadRequest:
		httputil.WriteBadRequest(w, message)
	case http.StatusUnauthorized:
		httputil.WriteUnauthorized(w, message)
	case http.StatusNotFound:
		httputil.WriteNotFound(w, message)
	case http.StatusInternalServerError:
		httputil.WriteInternalError(w, message)
	default:
		httputil.WriteError(w, status, "ERROR", message, nil)
	}
}

// RegisterFrontdeskAnalysisRoutes registers front-desk analysis routes
func (h *Handler) RegisterFrontdeskAnalysisRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/frontdesk/knowledge-bases", Handler: h.ListFrontdeskKnowledgeBases, Auth: true},
		{Method: "GET", Path: "/api/v1/frontdesk/knowledge-bases/{kb_type}", Handler: h.GetFrontdeskKnowledgeBase, Auth: true},
		{Method: "POST", Path: "/api/v1/frontdesk/knowledge-bases", Handler: h.CreateFrontdeskKnowledgeBase, Auth: true},
		{Method: "PUT", Path: "/api/v1/frontdesk/knowledge-bases/{kb_type}", Handler: h.UpdateFrontdeskKnowledgeBase, Auth: true},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

// ListFrontdeskShiftAnalyses lists shift analyses for a tenant
func (h *Handler) ListFrontdeskShiftAnalyses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	// Parse query parameters
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("page_size")
	employeeIDStr := r.URL.Query().Get("employee_id")
	dateStr := r.URL.Query().Get("date")

	page := 1
	if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
		page = p
	}

	pageSize := 20
	if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
		pageSize = ps
	}

	var employeeID *int64
	if employeeIDStr != "" {
		if id, err := strconv.ParseInt(employeeIDStr, 10, 64); err == nil {
			employeeID = &id
		}
	}

	analyses, total, err := h.service.ListShiftAnalyses(ctx, tenantID, page, pageSize, employeeID, dateStr)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"data":       analyses,
		"total":      total,
		"page":       page,
		"page_size":  pageSize,
		"total_page": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

// GetFrontdeskShiftAnalysis gets a shift analysis by ID
func (h *Handler) GetFrontdeskShiftAnalysis(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	idStr := r.PathValue("id")
	if idStr == "" {
		respondError(w, http.StatusBadRequest, "id required")
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid id")
		return
	}

	analysis, err := h.service.GetShiftAnalysis(ctx, tenantID, id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, analysis)
}

// CreateFrontdeskShiftAnalysis creates a new shift analysis
func (h *Handler) CreateFrontdeskShiftAnalysis(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	var req ShiftAnalysisCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	analysis, err := h.service.CreateShiftAnalysis(ctx, tenantID, &req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, analysis)
}

// ListFrontdeskDailyReports lists daily reports for a tenant
func (h *Handler) ListFrontdeskDailyReports(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("page_size")

	page := 1
	if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
		page = p
	}

	pageSize := 20
	if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
		pageSize = ps
	}

	reports, total, err := h.service.ListDailyReports(ctx, tenantID, page, pageSize)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"data":       reports,
		"total":      total,
		"page":       page,
		"page_size":  pageSize,
		"total_page": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

// GetFrontdeskDailyReport gets a daily report by date
func (h *Handler) GetFrontdeskDailyReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	dateStr := r.PathValue("date")
	if dateStr == "" {
		respondError(w, http.StatusBadRequest, "date required")
		return
	}

	report, err := h.service.GetFrontdeskDailyReport(ctx, tenantID, dateStr)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, report)
}

// ListFrontdeskKnowledgeBases lists knowledge bases for a tenant
func (h *Handler) ListFrontdeskKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	kbType := r.URL.Query().Get("kb_type")

	bases, err := h.service.ListKnowledgeBases(ctx, tenantID, kbType)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, bases)
}

// GetFrontdeskKnowledgeBase gets a knowledge base by type
func (h *Handler) GetFrontdeskKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	kbType := r.PathValue("kb_type")
	if kbType == "" {
		respondError(w, http.StatusBadRequest, "kb_type required")
		return
	}

	base, err := h.service.GetKnowledgeBase(ctx, tenantID, kbType)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, base)
}

// CreateFrontdeskKnowledgeBase creates a knowledge base
func (h *Handler) CreateFrontdeskKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	var req struct {
		KBType  string                 `json:"kb_type"`
		Content map[string]interface{} `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	base, err := h.service.CreateKnowledgeBase(ctx, tenantID, req.KBType, req.Content)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, base)
}

// UpdateFrontdeskKnowledgeBase updates a knowledge base
func (h *Handler) UpdateFrontdeskKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	kbType := r.PathValue("kb_type")
	if kbType == "" {
		respondError(w, http.StatusBadRequest, "kb_type required")
		return
	}

	var req struct {
		Content map[string]interface{} `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	base, err := h.service.UpdateKnowledgeBase(ctx, tenantID, kbType, req.Content)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, base)
}

// ListFrontdeskWeeklyReports lists weekly reports for a tenant
func (h *Handler) ListFrontdeskWeeklyReports(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("page_size")

	page := 1
	if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
		page = p
	}

	pageSize := 20
	if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
		pageSize = ps
	}

	reports, total, err := h.service.ListWeeklyReports(ctx, tenantID, page, pageSize)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"data":       reports,
		"total":      total,
		"page":       page,
		"page_size":  pageSize,
		"total_page": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

// GetFrontdeskWeeklyReport gets a weekly report by date
func (h *Handler) GetFrontdeskWeeklyReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	dateStr := r.PathValue("date")
	if dateStr == "" {
		respondError(w, http.StatusBadRequest, "date required")
		return
	}

	report, err := h.service.GetWeeklyReport(ctx, tenantID, dateStr)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, report)
}

// GenerateFrontdeskWeeklyReport manually generates a weekly report
func (h *Handler) GenerateFrontdeskWeeklyReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	var req struct {
		Date string `json:"date"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	report, err := h.service.GenerateWeeklyReport(ctx, tenantID, req.Date)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, report)
}

// PublishFrontdeskWeeklyReport publishes a weekly report
func (h *Handler) PublishFrontdeskWeeklyReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	var req struct {
		ReportDate     string `json:"report_date"`
		ManagerComment string `json:"manager_comment"`
		PublisherName  string `json:"publisher_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ReportDate == "" {
		respondError(w, http.StatusBadRequest, "report_date required")
		return
	}

	claims := middleware.GetUserClaims(ctx)
	publisher := ""
	if strings.TrimSpace(req.PublisherName) != "" {
		publisher = strings.TrimSpace(req.PublisherName)
	} else if claims != nil {
		publisher = strconv.FormatInt(claims.UserID, 10)
	}

	report, err := h.service.PublishWeeklyReport(ctx, tenantID, req.ReportDate, req.ManagerComment, publisher)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, report)
}

func (h *Handler) AddFrontdeskWeeklyReportEvidence(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, status := getFrontdeskTenantID(h, r)
	if status != 0 {
		respondError(w, status, "tenant_id required")
		return
	}

	var req struct {
		ReportDate  string `json:"report_date"`
		RecordingID int64  `json:"recording_id"`
		ShiftDate   string `json:"shift_date"`
		Summary     string `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RecordingID <= 0 {
		respondError(w, http.StatusBadRequest, "recording_id required")
		return
	}

	report, err := h.service.AddWeeklyReportEvidence(ctx, tenantID, req.ReportDate, req.RecordingID, req.ShiftDate, req.Summary)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, report)
}
