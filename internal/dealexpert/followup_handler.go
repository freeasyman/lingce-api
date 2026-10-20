package dealexpert

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// RegisterUserRoutes 注册赢客专家前端使用的业务接口。
// 权限：登录员工；租户与数据范围由 resolveDealScope 统一收敛。
func (h *Handler) RegisterUserRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/deal-expert/encounters", Handler: h.ListEncounters, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/deal-expert/follow-ups", Handler: h.ListFollowUps, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/deal-expert/follow-ups", Handler: h.CreateFollowUp, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/deal-expert/follow-ups/{id}", Handler: h.GetFollowUp, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/deal-expert/follow-ups/{id}/actions/complete", Handler: h.CompleteFollowUp, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/deal-expert/encounters/{id}/follow-ups", Handler: h.ListEncounterFollowUps, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/deal-expert/encounters/{id}/outcomes", Handler: h.ListEncounterOutcomes, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/deal-expert/outcomes", Handler: h.CreateOutcome, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/deal-expert/recordings/{id}/card", Handler: h.GetRecordingCard, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/deal-expert/overview", Handler: h.Overview, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
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
	case errors.Is(err, ErrCardNotFound):
		httputil.WriteNotFound(w, err.Error())
	case errors.Is(err, ErrFollowUpNotFound):
		httputil.WriteNotFound(w, err.Error())
	case errors.Is(err, ErrFollowUpNotActive):
		httputil.WriteError(w, http.StatusConflict, "CONFLICT", err.Error(), nil)
	case errors.Is(err, ErrInvalidResult), errors.Is(err, ErrInvalidOutcomeType), errors.Is(err, ErrAmountRequired),
		errors.Is(err, ErrInvalidOutcomeStatus), errors.Is(err, ErrInvalidFollowUpState), errors.Is(err, ErrInvalidDecisionFilter):
		httputil.WriteBadRequest(w, err.Error())
	default:
		httputil.WriteInternalError(w, err.Error())
	}
}

func (h *Handler) ListFollowUps(w http.ResponseWriter, r *http.Request) {
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
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	items, total, err := h.service.ListFollowUps(r.Context(), scope.TenantID, strings.TrimSpace(r.URL.Query().Get("status")), page, pageSize, scope.EmployeeFilter)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateFollowUp(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	var req CreateFollowUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if accessErr := h.checkEncounterAccess(r.Context(), scope, req.EncounterID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	item, inserted, err := h.service.CreateFollowUp(r.Context(), scope.TenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"follow_up": item, "inserted": inserted})
}

func (h *Handler) GetFollowUp(w http.ResponseWriter, r *http.Request) {
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
	item, err := h.service.GetFollowUp(r.Context(), scope.TenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if accessErr := h.checkEncounterAccess(r.Context(), scope, item.EncounterID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	events, err := h.service.ListFollowUpEvents(r.Context(), scope.TenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"follow_up": item, "events": events})
}

func (h *Handler) CompleteFollowUp(w http.ResponseWriter, r *http.Request) {
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
	current, err := h.service.GetFollowUp(r.Context(), scope.TenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if accessErr := h.checkEncounterAccess(r.Context(), scope, current.EncounterID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	var req CompleteFollowUpRequest
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
	item, events, outcome, err := h.service.CompleteFollowUp(r.Context(), scope.TenantID, id, req, actorID, "")
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"follow_up": item, "events": events, "outcome": outcome})
}

func (h *Handler) ListEncounterFollowUps(w http.ResponseWriter, r *http.Request) {
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
	items, err := h.service.ListFollowUpsByEncounter(r.Context(), scope.TenantID, encounterID)
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
	if accessErr := h.checkEncounterAccess(r.Context(), scope, req.EncounterID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	item, err := h.service.CreateOutcome(r.Context(), scope.TenantID, req)
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
	overview, err := h.service.Overview(r.Context(), scope.TenantID, dateFrom, dateTo, scope.EmployeeFilter)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, overview)
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
		Keyword:        strings.TrimSpace(r.URL.Query().Get("keyword")),
		DateFrom:       dateFrom,
		DateTo:         dateTo,
		CustomerLinked: customerLinked,
		FollowUpState:  strings.TrimSpace(r.URL.Query().Get("follow_up_state")),
		Decision:       strings.TrimSpace(r.URL.Query().Get("decision")),
	}, scope.EmployeeFilter)
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

func (h *Handler) GetRecordingCard(w http.ResponseWriter, r *http.Request) {
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
	card, err := h.service.LatestCard(r.Context(), scope.TenantID, recordingID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, card)
}
