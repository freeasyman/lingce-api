package dealexpert

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/xuri/excelize/v2"
)

// RegisterUserRoutes 注册赢客专家前端使用的业务接口。
// 权限：登录员工；租户与数据范围由 resolveDealScope 统一收敛。
// 员工 App 登录后拿到的是 mobile token，因此员工可用业务接口放行
// admin/employee/mobile；审批规则、权限管理、推送补发等管理接口仍只允许
// admin/employee，不对 mobile 开放。
func (h *Handler) RegisterUserRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/deal-expert/encounters", Handler: h.ListEncounters, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/follow-ups", Handler: h.ListSalesTasks, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/deal-expert/follow-ups", Handler: h.CreateSalesTask, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/follow-ups/{id}", Handler: h.GetSalesTask, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/deal-expert/follow-ups/{id}/actions/complete", Handler: h.CompleteSalesTask, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/deal-expert/follow-ups/{id}/actions/confirm", Handler: h.ConfirmSalesTaskDraft, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/deal-expert/recordings/{id}/action-drafts", Handler: h.CreateActionDraft, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/encounters/{id}/follow-ups", Handler: h.ListEncounterSalesTasks, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/encounters/{id}/outcomes", Handler: h.ListEncounterOutcomes, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/deal-expert/outcomes", Handler: h.CreateOutcome, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/deal-expert/outcomes/claim", Handler: h.ClaimOutcome, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/outcomes", Handler: h.ListOutcomes, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/outcomes/export.xlsx", Handler: h.ExportOutcomes, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "PATCH", Path: "/api/v1/deal-expert/outcomes/{id}", Handler: h.UpdateOutcome, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/deal-expert/outcomes/{id}/void", Handler: h.VoidOutcome, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/claims/unreported", Handler: h.ListUnreportedClaims, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/recordings/{id}/recording-record.docx", Handler: h.ExportRecordingRecord, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "PUT", Path: "/api/v1/deal-expert/recordings/{id}/customer-name", Handler: h.RenameRecordingCustomer, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/recordings/{id}/analysis", Handler: h.GetConsultationAnalysis, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/recordings/{id}/outcome", Handler: h.GetRecordingOutcome, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/deal-expert/recordings/{id}/analyze", Handler: h.AnalyzeRecording, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/analyses", Handler: h.ListConsultationAnalyses, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/recordings/{id}/analysis-actions", Handler: h.ListAnalysisActions, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/deal-expert/recordings/{id}/analysis-actions", Handler: h.CreateAnalysisAction, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/overview", Handler: h.Overview, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/my-actions", Handler: h.ListMyActions, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/diagnosis", Handler: h.GetDiagnosis, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/forecast-calibration", Handler: h.GetForecastCalibration, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/email-deliveries", Handler: h.ListEmailDeliveries, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/operation/email-deliveries", Handler: h.ListEmailDeliveriesForOperation, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/deal-expert/approval-rules", Handler: h.GetApprovalRules, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/deal-expert/approval-rules", Handler: h.SaveApprovalRules, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret, Pool: h.pool})
}

type tenantError struct {
	status  int
	message string
}

func writeTenantError(w http.ResponseWriter, err *tenantError) {
	httputil.WriteError(w, err.status, "SCOPE", err.message, nil)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSalesTaskNotFound), errors.Is(err, ErrOutcomeNotFound):
		httputil.WriteNotFound(w, err.Error())
	case errors.Is(err, ErrSalesTaskNotActive):
		httputil.WriteError(w, http.StatusConflict, "CONFLICT", err.Error(), nil)
	case errors.Is(err, ErrInvalidResult), errors.Is(err, ErrInvalidOutcomeType), errors.Is(err, ErrAmountRequired),
		errors.Is(err, ErrInvalidOutcomeStatus), errors.Is(err, ErrInvalidSalesTaskState), errors.Is(err, ErrInvalidDecisionFilter):
		httputil.WriteBadRequest(w, err.Error())
	default:
		httputil.WriteInternalError(w, err.Error())
	}
}

func (h *Handler) ListSalesTasks(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 20
	}
	items, total, err := h.service.ListSalesTasks(r.Context(), scope.TenantID, strings.TrimSpace(r.URL.Query().Get("status")), page, pageSize, scope.EmployeeIDs)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateSalesTask(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	var req CreateSalesTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if req.EncounterID > 0 {
		if accessErr := h.checkEncounterAccess(r.Context(), scope, req.EncounterID); accessErr != nil {
			writeTenantError(w, accessErr)
			return
		}
	} else if accessErr := h.checkRecordingAccess(r.Context(), scope, req.RecordingID); accessErr != nil {
		// encounter_id=0 的录音没有可解析的接待，按关联录音归属校验。
		writeTenantError(w, accessErr)
		return
	}
	item, inserted, err := h.service.CreateSalesTask(r.Context(), scope.TenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"follow_up": item, "inserted": inserted})
}

func (h *Handler) GetSalesTask(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid follow up id")
		return
	}
	item, err := h.service.GetSalesTask(r.Context(), scope.TenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if item.EncounterID > 0 {
		if accessErr := h.checkEncounterAccess(r.Context(), scope, item.EncounterID); accessErr != nil {
			writeTenantError(w, accessErr)
			return
		}
	} else if accessErr := h.checkSalesTaskAccess(r.Context(), scope, item.RecordingID, item.OwnerEmployeeID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	events, err := h.service.ListSalesTaskEvents(r.Context(), scope.TenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"follow_up": item, "events": events})
}

func (h *Handler) CompleteSalesTask(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid follow up id")
		return
	}
	current, err := h.service.GetSalesTask(r.Context(), scope.TenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if current.EncounterID > 0 {
		if accessErr := h.checkEncounterAccess(r.Context(), scope, current.EncounterID); accessErr != nil {
			writeTenantError(w, accessErr)
			return
		}
	} else if accessErr := h.checkSalesTaskAccess(r.Context(), scope, current.RecordingID, current.OwnerEmployeeID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	var req CompleteSalesTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	var actorID *int64
	if claims != nil && claims.UserID > 0 {
		value := claims.UserID
		actorID = &value
	}
	item, events, outcome, err := h.service.CompleteSalesTask(r.Context(), scope.TenantID, id, req, actorID, "")
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"follow_up": item, "events": events, "outcome": outcome})
}

func (h *Handler) ListEncounterSalesTasks(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	encounterID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || encounterID <= 0 {
		httputil.WriteBadRequest(w, "invalid encounter id")
		return
	}
	if accessErr := h.checkEncounterAccess(r.Context(), scope, encounterID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	items, err := h.service.ListSalesTasksByEncounter(r.Context(), scope.TenantID, encounterID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) ListEncounterOutcomes(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	encounterID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || encounterID <= 0 {
		httputil.WriteBadRequest(w, "invalid encounter id")
		return
	}
	if accessErr := h.checkEncounterAccess(r.Context(), scope, encounterID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	items, err := h.service.ListOutcomesByEncounter(r.Context(), scope.TenantID, encounterID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) CreateOutcome(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	var req CreateOutcomeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if req.EncounterID > 0 {
		if accessErr := h.checkEncounterAccess(r.Context(), scope, req.EncounterID); accessErr != nil {
			writeTenantError(w, accessErr)
			return
		}
	} else if accessErr := h.checkRecordingAccess(r.Context(), scope, req.RecordingID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	actorID := int64(0)
	if claims != nil {
		actorID = claims.UserID
	}
	item, err := h.service.CreateOutcome(r.Context(), scope.TenantID, actorID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"outcome": item})
}

func parseDateParam(value string, endOfDay bool) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, fmt.Errorf("invalid date %q, expected YYYY-MM-DD", value)
	}
	if endOfDay {
		parsed = parsed.Add(24 * time.Hour)
	}
	return &parsed, nil
}

func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	dateFrom, err := parseDateParam(r.URL.Query().Get("date_from"), false)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	dateTo, err := parseDateParam(r.URL.Query().Get("date_to"), true)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	overview, err := h.service.Overview(r.Context(), scope.TenantID, dateFrom, dateTo, scope.EmployeeIDs)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, overview)
}

// firstNonEmpty 返回第一个非空去空格值：用于 q/keyword 这类同义参数兼容。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (h *Handler) ListEncounters(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	dateFrom, err := parseDateParam(r.URL.Query().Get("date_from"), false)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	dateTo, err := parseDateParam(r.URL.Query().Get("date_to"), true)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var customerID *int64
	if raw := strings.TrimSpace(r.URL.Query().Get("customer_id")); raw != "" {
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value <= 0 {
			httputil.WriteBadRequest(w, "customer_id must be a positive integer")
			return
		}
		customerID = &value
	}
	var customerLinked *bool
	if raw := strings.TrimSpace(r.URL.Query().Get("customer_linked")); raw != "" {
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			httputil.WriteBadRequest(w, "invalid customer_linked, expected true or false")
			return
		}
		customerLinked = &value
	}
	items, total, err := h.service.ListEncounters(r.Context(), scope.TenantID, EncounterListParams{
		Page:           page,
		PageSize:       pageSize,
		Status:         strings.TrimSpace(r.URL.Query().Get("status")),
		Keyword:        firstNonEmpty(r.URL.Query().Get("q"), r.URL.Query().Get("keyword")),
		CustomerID:     customerID,
		DateFrom:       dateFrom,
		DateTo:         dateTo,
		CustomerLinked: customerLinked,
		SalesTaskState: strings.TrimSpace(r.URL.Query().Get("follow_up_state")),
		Decision:       strings.TrimSpace(r.URL.Query().Get("decision")),
	}, scope.EmployeeIDs)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// AnalyzeRecording 前端入口：同步生成（或重新生成）接待记录分析并返回结果。
func (h *Handler) AnalyzeRecording(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	recordingID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || recordingID <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	if accessErr := h.checkRecordingAccess(r.Context(), scope, recordingID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	if h.consultation == nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "ANALYSIS_UNAVAILABLE", "consultation analysis generator is not configured", nil)
		return
	}
	analysis, err := h.consultation.Generate(r.Context(), scope.TenantID, recordingID)
	if err != nil {
		if writeDealGateError(w, err) {
			return
		}
		httputil.WriteError(w, http.StatusInternalServerError, "ANALYSIS_GENERATE_FAILED", err.Error(), nil)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"status": "analyzed", "analysis": analysis})
}

// GetConsultationAnalysis 前端读取接待记录分析；无结果时 analysis 为空。
func (h *Handler) GetConsultationAnalysis(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	recordingID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || recordingID <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	if accessErr := h.checkRecordingAccess(r.Context(), scope, recordingID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	if h.consultation == nil {
		httputil.WriteSuccess(w, map[string]any{"status": "unavailable"})
		return
	}
	item, err := h.consultation.Get(r.Context(), scope.TenantID, recordingID)
	if err != nil {
		httputil.WriteSuccess(w, map[string]any{"status": "none"})
		return
	}
	httputil.WriteSuccess(w, item)
}

// CreateActionDraft 从卡片创建人工确认草案（不满足自动条件时的可执行候选）。
func (h *Handler) CreateActionDraft(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	recordingID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || recordingID <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	if accessErr := h.checkRecordingAccess(r.Context(), scope, recordingID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	var req struct {
		Objective string `json:"objective"`
		Action    string `json:"action"`
		Note      string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Action) == "" {
		httputil.WriteBadRequest(w, "action is required")
		return
	}
	rec, err := h.service.RecordingBrief(r.Context(), scope.TenantID, recordingID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	var ownerID int64
	if claims != nil {
		ownerID = claims.UserID
	}
	customerID := rec.CustomerID
	var customerIDPtr *int64
	if customerID > 0 {
		customerIDPtr = &customerID
	}
	var ownerIDPtr *int64
	if ownerID > 0 {
		ownerIDPtr = &ownerID
	}
	draft, err := h.service.CreateDraftSalesTask(r.Context(), scope.TenantID, CreateSalesTaskRequest{
		EncounterID:     rec.EncounterID,
		RecordingID:     recordingID,
		CustomerID:      customerIDPtr,
		CustomerName:    rec.CustomerName,
		Objective:       strings.TrimSpace(req.Objective),
		Action:          strings.TrimSpace(req.Action),
		OwnerEmployeeID: ownerIDPtr,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, draft)
}

// ConfirmSalesTaskDraft 人工确认后把草案转正为待处理任务。
func (h *Handler) ConfirmSalesTaskDraft(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid follow up id")
		return
	}
	current, err := h.service.GetSalesTask(r.Context(), scope.TenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if current.EncounterID > 0 {
		if accessErr := h.checkEncounterAccess(r.Context(), scope, current.EncounterID); accessErr != nil {
			writeTenantError(w, accessErr)
			return
		}
	} else if accessErr := h.checkSalesTaskAccess(r.Context(), scope, current.RecordingID, current.OwnerEmployeeID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	confirmed, err := h.service.ConfirmDraft(r.Context(), scope.TenantID, id, strings.TrimSpace(req.Note))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, confirmed)
}

// ListConsultationAnalyses 返回本租户已分析的接待记录（按接待时间倒序），供接待列表与看板使用。
func (h *Handler) ListConsultationAnalyses(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT a.recording_id, a.status, a.analysis, COALESCE(r.encounter_id,0), COALESCE(r.customer_id,0),
		       COALESCE(cu.name,''), COALESCE(NULLIF(e.full_name,''), e.name,''), COALESCE(r.recorded_at, r.created_at),
		       COALESCE(ov.type,''), ov.amount, ov.occurred_at, ov.voided_at, COALESCE(ov.void_reason,'')
		FROM consultation_analyses a
		JOIN recordings r ON r.id = a.recording_id
		LEFT JOIN customers cu ON cu.id = r.customer_id
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN LATERAL (
			SELECT o.type, o.amount, o.occurred_at, o.voided_at, o.void_reason
			FROM deal_outcome_events o
			WHERE o.tenant_id = a.tenant_id
			  AND ((r.encounter_id > 0 AND o.encounter_id = r.encounter_id) OR o.recording_id = a.recording_id)
			ORDER BY o.occurred_at DESC, o.id DESC
			LIMIT 1
		) ov ON TRUE
		WHERE a.tenant_id = $1 AND a.status = 'analyzed' AND r.deleted_at IS NULL
		  AND ($2::bigint[] IS NULL OR r.employee_id = ANY($2))
		ORDER BY r.recorded_at DESC
		LIMIT 500
	`, scope.TenantID, scope.EmployeeIDs)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "LIST_ANALYSES_FAILED", err.Error(), nil)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, 64)
	for rows.Next() {
		var recordingID, encID, customerID int64
		var status, customer, consultant, outcomeType, outcomeVoidReason string
		var raw []byte
		var recordedAt time.Time
		var outcomeAt, outcomeVoidedAt any
		var outcomeAmount *float64
		if err := rows.Scan(&recordingID, &status, &raw, &encID, &customerID, &customer, &consultant, &recordedAt, &outcomeType, &outcomeAmount, &outcomeAt, &outcomeVoidedAt, &outcomeVoidReason); err != nil {
			continue
		}
		var parsed map[string]any
		_ = json.Unmarshal(raw, &parsed)
		slim := map[string]any{}
		for _, key := range []string{"headline", "deal_status", "scene_type", "stuck_point_headline", "real_reason", "killer_evidence", "deal_confidence", "blocker_label", "customer_context_tags", "sales_tasks", "amount_hints", "pattern", "intent_project"} {
			if v, ok := parsed[key]; ok {
				slim[key] = v
			}
		}
		items = append(items, map[string]any{
			"recording_id":        recordingID,
			"encounter_id":        encID,
			"customer_id":         customerID,
			"status":              status,
			"customer":            customer,
			"consultant":          consultant,
			"recorded_at":         formatDealDBLocalTime(recordedAt),
			"outcome_type":        outcomeType,
			"outcome_amount":      outcomeAmount,
			"outcome_at":          outcomeAt,
			"outcome_voided":      outcomeVoidedAt != nil,
			"outcome_void_reason": outcomeVoidReason,
			"analysis":            slim,
		})
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": len(items)})
}

// formatDealDBLocalTime 把数据库 timestamp without timezone 按北京时间输出。
// 这些列保存的是北京时间墙上时间，直接让 Go 按 UTC 序列化会被前端再加 8 小时。
func formatDealDBLocalTime(t time.Time) string {
	cst := time.FixedZone("CST", 8*3600)
	wall := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), cst)
	return wall.Format(time.RFC3339)
}

func (h *Handler) outcomeFilterFromRequest(r *http.Request, scope *dealScope) (OutcomeFilter, error) {
	from, err := parseDateParam(r.URL.Query().Get("date_from"), false)
	if err != nil {
		return OutcomeFilter{}, err
	}
	to, err := parseDateParam(r.URL.Query().Get("date_to"), true)
	if err != nil {
		return OutcomeFilter{}, err
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	f := OutcomeFilter{
		DateFrom: from, DateTo: to, Type: strings.TrimSpace(r.URL.Query().Get("type")),
		IncludeVoided: r.URL.Query().Get("include_voided") == "1",
		Page:          page, PageSize: pageSize,
	}
	if !scope.allowsAllEmployees() {
		f.EmployeeIDs = scope.EmployeeIDs
		// 限定范围内仍允许按具体员工再收窄，但不能借参数扩大范围。
		if raw := r.URL.Query().Get("employee_id"); raw != "" {
			if id, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil && id > 0 && scope.allowsEmployee(id) {
				f.EmployeeIDs = []int64{id}
			}
		}
	} else if raw := r.URL.Query().Get("employee_id"); raw != "" {
		if id, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil && id > 0 {
			f.EmployeeIDs = []int64{id}
		}
	}
	return f, nil
}

func (h *Handler) ListOutcomes(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	f, err := h.outcomeFilterFromRequest(r, scope)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	items, total, err := h.service.ListOutcomes(r.Context(), scope.TenantID, f)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "page": f.Page, "page_size": f.PageSize})
}

func (h *Handler) ListUnreportedClaims(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	items, err := h.service.ListUnreportedClaims(r.Context(), scope.TenantID, scope.EmployeeIDs)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": len(items)})
}

func (h *Handler) requireManager(w http.ResponseWriter, scope *dealScope) bool {
	if !scope.Manager {
		httputil.WriteError(w, http.StatusForbidden, "MANAGER_ONLY", "只有负责人可以更正或驳回业绩", nil)
		return false
	}
	return true
}

func (h *Handler) UpdateOutcome(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	if !h.requireManager(w, scope) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid outcome id")
		return
	}
	if accessErr := h.checkOutcomeAccess(r.Context(), scope, id, "更正"); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	var patch OutcomePatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	var occurredAt *time.Time
	if patch.OccurredAt != nil && strings.TrimSpace(*patch.OccurredAt) != "" {
		parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(*patch.OccurredAt))
		if parseErr != nil {
			httputil.WriteBadRequest(w, "invalid occurred_at")
			return
		}
		occurredAt = &parsed
	}
	actorID := int64(0)
	if claims := middleware.GetUserClaims(r.Context()); claims != nil {
		actorID = claims.UserID
	}
	item, err := h.service.UpdateOutcome(r.Context(), scope.TenantID, id, actorID, patch, occurredAt)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"outcome": item})
}

func (h *Handler) VoidOutcome(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	if !h.requireManager(w, scope) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid outcome id")
		return
	}
	if accessErr := h.checkOutcomeAccess(r.Context(), scope, id, "驳回"); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	actorID := int64(0)
	if claims := middleware.GetUserClaims(r.Context()); claims != nil {
		actorID = claims.UserID
	}
	item, err := h.service.VoidOutcome(r.Context(), scope.TenantID, id, actorID, body.Reason)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"outcome": item})
}

func (h *Handler) ExportOutcomes(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	f, err := h.outcomeFilterFromRequest(r, scope)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	f.Page = 1
	f.PageSize = 1000
	items, _, err := h.service.ListOutcomes(r.Context(), scope.TenantID, f)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	file := excelize.NewFile()
	sheet := "业绩台账"
	_ = file.SetSheetName("Sheet1", sheet)
	headers := []string{"客户", "咨询师", "产品", "类型", "金额", "发生日期", "备注", "状态", "驳回/更正原因", "提交时间"}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = file.SetCellValue(sheet, cell, header)
	}
	style, _ := file.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	_ = file.SetCellStyle(sheet, "A1", "J1", style)
	var total float64
	for idx, item := range items {
		row := idx + 2
		status := "已入账"
		reason := item.UpdateReason
		if item.VoidedAt != nil {
			status = "已驳回"
			reason = item.VoidReason
		}
		amount := any("")
		if item.Amount != nil {
			amount = *item.Amount
			if item.Type == "付款" || item.Type == "定金" {
				total += *item.Amount
			}
		}
		values := []any{item.CustomerName, item.ConsultantName, item.ProductName, item.Type, amount,
			item.OccurredAt.Format("2006-01-02 15:04"), item.Note, status, reason, item.CreatedAt.Format("2006-01-02 15:04")}
		for col, value := range values {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			_ = file.SetCellValue(sheet, cell, value)
		}
	}
	totalRow := len(items) + 2
	cell, _ := excelize.CoordinatesToCellName(4, totalRow)
	_ = file.SetCellValue(sheet, cell, "合计（付款+定金）")
	cell, _ = excelize.CoordinatesToCellName(5, totalRow)
	_ = file.SetCellValue(sheet, cell, total)
	_ = file.SetColWidth(sheet, "A", "C", 18)
	_ = file.SetColWidth(sheet, "D", "J", 14)
	buf, err := file.WriteToBuffer()
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "EXPORT_FAILED", err.Error(), nil)
		return
	}
	filename := "业绩台账-" + time.Now().Format("20060102") + ".xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.QueryEscape(filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// ClaimOutcome 咨询师认业绩：默认入账，不等负责人确认；负责人后续可更正或驳回。
func (h *Handler) ClaimOutcome(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	var body struct {
		RecordingID int64    `json:"recording_id"`
		Amount      *float64 `json:"amount"`
		OccurredAt  string   `json:"occurred_at"`
		Note        string   `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RecordingID <= 0 {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if body.Amount == nil || *body.Amount <= 0 {
		httputil.WriteBadRequest(w, "请填写实际收款金额")
		return
	}
	if accessErr := h.checkRecordingAccess(r.Context(), scope, body.RecordingID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	var encounterID int64
	_ = h.pool.QueryRow(r.Context(), `SELECT COALESCE(encounter_id,0) FROM recordings WHERE id = $1 AND tenant_id = $2`, body.RecordingID, scope.TenantID).Scan(&encounterID)
	actorID := int64(0)
	if claims := middleware.GetUserClaims(r.Context()); claims != nil {
		actorID = claims.UserID
	}

	var existingID int64
	var existingAmount *float64
	var existingNote string
	err := h.pool.QueryRow(r.Context(), `
		SELECT id, amount, COALESCE(note,'') FROM deal_outcome_events
		WHERE tenant_id = $1 AND voided_at IS NULL AND COALESCE(amount,0) > 0
		  AND (recording_id = $2 OR ($3 > 0 AND encounter_id = $3))
		ORDER BY occurred_at DESC, id DESC LIMIT 1`,
		scope.TenantID, body.RecordingID, encounterID).Scan(&existingID, &existingAmount, &existingNote)
	if err == nil {
		httputil.WriteError(w, http.StatusConflict, "ALREADY_CLAIMED", "这条接待已经报过业绩，金额有误请联系负责人更正", nil)
		return
	}

	var occurredAt *string
	if strings.TrimSpace(body.OccurredAt) != "" {
		parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(body.OccurredAt))
		if parseErr != nil {
			httputil.WriteBadRequest(w, "invalid occurred_at")
			return
		}
		value := parsed.Format(time.RFC3339)
		occurredAt = &value
	}
	req := CreateOutcomeRequest{
		EncounterID: encounterID, RecordingID: body.RecordingID, Type: "付款",
		Amount: body.Amount, OccurredAt: occurredAt, Source: "consultant_claim", Note: body.Note,
	}
	item, err := h.service.CreateOutcome(r.Context(), scope.TenantID, actorID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"outcome": item})
}

// GetRecordingOutcome 返回一条接待最近一笔业绩记录（含已驳回），供接待详情展示。
func (h *Handler) GetRecordingOutcome(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	if accessErr := h.checkRecordingAccess(r.Context(), scope, id); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	var encounterID int64
	_ = h.pool.QueryRow(r.Context(), `SELECT COALESCE(encounter_id,0) FROM recordings WHERE id = $1 AND tenant_id = $2`, id, scope.TenantID).Scan(&encounterID)
	row := h.pool.QueryRow(r.Context(), `
		SELECT `+outcomeColumns+` FROM deal_outcome_events
		WHERE tenant_id = $1 AND (recording_id = $2 OR ($3 > 0 AND encounter_id = $3))
		ORDER BY occurred_at DESC, id DESC LIMIT 1`, scope.TenantID, id, encounterID)
	item, scanErr := scanOutcome(row)
	if scanErr != nil {
		httputil.WriteSuccess(w, map[string]any{"outcome": nil})
		return
	}
	httputil.WriteSuccess(w, map[string]any{"outcome": item})
}

// writeDealGateError 把交付闸门拒绝（到期/暂停/额度不足）写成统一的客户口径；
// 不是闸门错误时返回 false，由调用方走原有错误处理。
func writeDealGateError(w http.ResponseWriter, err error) bool {
	gate, ok := billing.GateRejection(err)
	if !ok {
		return false
	}
	var details map[string]any
	if gate.Insufficient != nil {
		details = map[string]any{
			"unit":      gate.Insufficient.Unit,
			"current":   gate.Insufficient.Current,
			"required":  gate.Insufficient.Required,
			"shortfall": gate.Insufficient.Shortfall,
		}
	}
	httputil.WriteError(w, gate.Status, gate.Code, gate.Message, details)
	return true
}
