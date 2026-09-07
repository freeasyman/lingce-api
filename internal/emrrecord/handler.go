package emrrecord

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/emrcheck"
	"github.com/freeasyman/lingce-api/internal/emrpermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service     *Service
	permissions *emrpermission.Service
}

func NewHandler(service *Service, permissions *emrpermission.Service) *Handler {
	return &Handler{service: service, permissions: permissions}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/emr/records", Handler: h.List, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/{id}/emr-records", Handler: h.ListByPatient, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records", Handler: h.Create, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}", Handler: h.Get, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}/versions", Handler: h.Snapshots, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/emr/records/{id}/working-draft", Handler: h.AutoSave, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/save", Handler: h.ManualSave, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/submit", Handler: h.Submit, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/confirm", Handler: h.Confirm, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/archive", Handler: h.Archive, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/revise", Handler: h.Revise, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/void", Handler: h.Void, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) tenant(r *http.Request) (int64, int64, error) {
	claims := middleware.GetUserClaims(r.Context())
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		return 0, 0, err
	}
	return tenantID, claims.UserID, nil
}

func (h *Handler) authorize(r *http.Request, ability string) (*emrpermission.Access, int64, error) {
	claims := middleware.GetUserClaims(r.Context())
	tenantID, _, err := h.tenant(r)
	if err != nil {
		return nil, 0, err
	}
	access, err := h.permissions.Authorize(r.Context(), claims, tenantID, ability)
	if err != nil {
		return nil, 0, err
	}
	return access, tenantID, nil
}

func (h *Handler) authorizeRecord(r *http.Request, ability string) (*emrpermission.Access, int64, error) {
	access, tenantID, err := h.authorize(r, ability)
	if err != nil {
		return nil, 0, err
	}
	allowed, err := h.permissions.CanAccessRecord(r.Context(), access, recordID(r))
	if err != nil {
		return nil, 0, err
	}
	if !allowed {
		return nil, 0, fmt.Errorf("emr record access denied")
	}
	return access, tenantID, nil
}

func decodeBody(r *http.Request, value any) error { return json.NewDecoder(r.Body).Decode(value) }
func recordID(r *http.Request) string             { return strings.TrimSpace(r.PathValue("id")) }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorize(r, "record.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	items, err := h.service.ListScoped(r.Context(), tenantID, r.URL.Query().Get("status"), access)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) ListByPatient(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorize(r, "record.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	patientID, err := parsePositiveID(recordID(r), "patient_id")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	pagination := httputil.ParsePagination(r)
	page, pageSize := pagination.Page, pagination.PageSize
	items, total, err := h.service.ListByPatientScoped(r.Context(), tenantID, patientID, page, pageSize, access)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, int64(total), page, pageSize)
}

func parsePositiveID(raw, name string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return id, nil
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorize(r, "record.create")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req CreateRequest
	if decodeBody(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	doctorID := access.UserID
	if req.DoctorID != nil && *req.DoctorID > 0 {
		doctorID = *req.DoctorID
	}
	if !access.CanRecord(doctorID, req.DepartmentID) {
		httputil.WriteForbidden(w, "emr record access denied")
		return
	}
	item, err := h.service.Create(r.Context(), tenantID, access.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	_, tenantID, err := h.authorizeRecord(r, "record.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	item, err := h.service.Get(r.Context(), tenantID, recordID(r))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if item == nil {
		httputil.WriteNotFound(w, "record not found")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) Snapshots(w http.ResponseWriter, r *http.Request) {
	_, tenantID, err := h.authorizeRecord(r, "record.history.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	items, err := h.service.Snapshots(r.Context(), tenantID, recordID(r))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}
func (h *Handler) AutoSave(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorizeRecord(r, "record.edit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req ContentRequest
	if decodeBody(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.AutoSave(r.Context(), tenantID, access.UserID, recordID(r), req.Content)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) ManualSave(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorizeRecord(r, "record.edit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req ActionRequest
	if decodeBody(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.ManualSave(r.Context(), tenantID, access.UserID, recordID(r), req.Content, req.Note)
	if err != nil {
		writeActionError(w, item, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "record.submit", h.service.Submit)
}

func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "record.archive", h.service.Confirm)
}

func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "record.archive", h.service.Archive)
}

func (h *Handler) action(w http.ResponseWriter, r *http.Request, ability string, action func(context.Context, int64, int64, string, ActionRequest) (*WriteOutcome, error)) {
	access, tenantID, err := h.authorizeRecord(r, ability)
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req ActionRequest
	if r.Body != nil && r.ContentLength != 0 {
		if decodeBody(r, &req) != nil {
			httputil.WriteBadRequest(w, "invalid request body")
			return
		}
	}
	item, err := action(r.Context(), tenantID, access.UserID, recordID(r), req)
	if err != nil {
		writeActionError(w, item, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

func writeActionError(w http.ResponseWriter, item *WriteOutcome, actionErr error) {
	if item == nil || item.CheckRun == nil {
		httputil.WriteBadRequest(w, actionErr.Error())
		return
	}
	details := map[string]any{
		"check_run_id": item.CheckRun.ID,
		"issues":       checkIssues(item.CheckRun),
	}
	if item.Record != nil {
		details["status"] = item.Record.Status
	}
	httputil.WriteError(w, http.StatusBadRequest, "EMR_CHECK_BLOCKED", actionErr.Error(), details)
}

func checkIssues(run *emrcheck.CheckRun) []map[string]any {
	issues := make([]map[string]any, 0)
	for _, result := range run.Results {
		if result.Conclusion == "符合" && result.HandlingResult == "允许继续" {
			continue
		}
		issues = append(issues, map[string]any{
			"code":            result.RequirementCode,
			"name":            result.RequirementName,
			"conclusion":      result.Conclusion,
			"check_status":    result.CheckStatus,
			"handling_result": result.HandlingResult,
			"explanation":     result.HitExplanation,
			"suggested":       result.SuggestedHandling,
			"evidence":        result.Evidence,
		})
	}
	return issues
}
func (h *Handler) Revise(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorizeRecord(r, "record.edit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req ActionRequest
	_ = decodeBody(r, &req)
	item, err := h.service.Revise(r.Context(), tenantID, access.UserID, recordID(r), req.Note)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) Void(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorizeRecord(r, "record.edit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req ActionRequest
	_ = decodeBody(r, &req)
	item, err := h.service.Void(r.Context(), tenantID, access.UserID, recordID(r), req.Note)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
