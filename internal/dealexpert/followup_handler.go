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
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// RegisterUserRoutes 注册赢客专家前端使用的业务接口。
// 权限：登录员工，租户由 token 与 tenant_id 参数共同约束。
func (h *Handler) RegisterUserRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/deal-expert/follow-ups", Handler: h.ListFollowUps, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/deal-expert/follow-ups", Handler: h.CreateFollowUp, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/deal-expert/follow-ups/{id}", Handler: h.GetFollowUp, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/deal-expert/follow-ups/{id}/actions/complete", Handler: h.CompleteFollowUp, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/deal-expert/encounters/{id}/follow-ups", Handler: h.ListEncounterFollowUps, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/deal-expert/encounters/{id}/outcomes", Handler: h.ListEncounterOutcomes, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/deal-expert/outcomes", Handler: h.CreateOutcome, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/deal-expert/overview", Handler: h.Overview, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret, Pool: h.pool})
}

type tenantError struct {
	status  int
	message string
}

func writeTenantError(w http.ResponseWriter, err *tenantError) {
	httputil.WriteError(w, err.status, "TENANT_SCOPE", err.message, nil)
}

func (h *Handler) tenantID(r *http.Request) (int64, *tenantError) {
	claims := middleware.GetUserClaims(r.Context())
	scope, err := tenancy.ResolveScope(r.Context(), h.pool, claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		if strings.Contains(err.Error(), "access") {
			return 0, &tenantError{status: http.StatusForbidden, message: err.Error()}
		}
		return 0, &tenantError{status: http.StatusBadRequest, message: err.Error()}
	}
	if scope == nil || scope.TenantID == nil {
		return 0, &tenantError{status: http.StatusBadRequest, message: "tenant_id is required"}
	}
	return *scope.TenantID, nil
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrFollowUpNotFound):
		httputil.WriteNotFound(w, err.Error())
	case errors.Is(err, ErrFollowUpNotActive):
		httputil.WriteError(w, http.StatusConflict, "CONFLICT", err.Error(), nil)
	case errors.Is(err, ErrInvalidResult), errors.Is(err, ErrInvalidOutcomeType), errors.Is(err, ErrAmountRequired):
		httputil.WriteBadRequest(w, err.Error())
	default:
		httputil.WriteInternalError(w, err.Error())
	}
}

func (h *Handler) ListFollowUps(w http.ResponseWriter, r *http.Request) {
	tenantID, httpErr := h.tenantID(r)
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
	items, total, err := h.service.ListFollowUps(r.Context(), tenantID, strings.TrimSpace(r.URL.Query().Get("status")), page, pageSize)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateFollowUp(w http.ResponseWriter, r *http.Request) {
	tenantID, httpErr := h.tenantID(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	var req CreateFollowUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, inserted, err := h.service.CreateFollowUp(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"follow_up": item, "inserted": inserted})
}

func (h *Handler) GetFollowUp(w http.ResponseWriter, r *http.Request) {
	tenantID, httpErr := h.tenantID(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid follow up id")
		return
	}
	item, err := h.service.GetFollowUp(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	events, err := h.service.ListFollowUpEvents(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"follow_up": item, "events": events})
}

func (h *Handler) CompleteFollowUp(w http.ResponseWriter, r *http.Request) {
	tenantID, httpErr := h.tenantID(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid follow up id")
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
	item, events, outcome, err := h.service.CompleteFollowUp(r.Context(), tenantID, id, req, actorID, "")
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"follow_up": item, "events": events, "outcome": outcome})
}

func (h *Handler) ListEncounterFollowUps(w http.ResponseWriter, r *http.Request) {
	tenantID, httpErr := h.tenantID(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	encounterID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || encounterID <= 0 {
		httputil.WriteBadRequest(w, "invalid encounter id")
		return
	}
	items, err := h.service.ListFollowUpsByEncounter(r.Context(), tenantID, encounterID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) ListEncounterOutcomes(w http.ResponseWriter, r *http.Request) {
	tenantID, httpErr := h.tenantID(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	encounterID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || encounterID <= 0 {
		httputil.WriteBadRequest(w, "invalid encounter id")
		return
	}
	items, err := h.service.ListOutcomesByEncounter(r.Context(), tenantID, encounterID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) CreateOutcome(w http.ResponseWriter, r *http.Request) {
	tenantID, httpErr := h.tenantID(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	var req CreateOutcomeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.CreateOutcome(r.Context(), tenantID, req)
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
	tenantID, httpErr := h.tenantID(r)
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
	overview, err := h.service.Overview(r.Context(), tenantID, dateFrom, dateTo)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, overview)
}
