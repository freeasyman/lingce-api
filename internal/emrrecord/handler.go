package emrrecord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/emrrule"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/emr/records", Handler: h.ListRecords, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}", Handler: h.GetRecord, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/encounters/{id}", Handler: h.GetEncounter, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/import-recording-drafts", Handler: h.ImportRecordingDrafts, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/emr/records/{id}", Handler: h.SaveRecord, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/submit", Handler: h.SubmitRecord, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/archive", Handler: h.ArchiveRecord, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}/versions", Handler: h.ListVersions, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}/audit-events", Handler: h.ListAuditEvents, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) ListRecords(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "self"
	}
	page := parseIntDefault(r.URL.Query().Get("page"), 1)
	pageSize := parseIntDefault(r.URL.Query().Get("page_size"), 20)

	items, total, err := h.service.ListRecords(r.Context(), ListRecordsRequest{
		TenantID:   tenantID,
		EmployeeID: claims.UserID,
		Scope:      scope,
		Keyword:    r.URL.Query().Get("keyword"),
		Status:     r.URL.Query().Get("status"),
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, int64(total), page, pageSize)
}

func (h *Handler) ImportRecordingDrafts(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	var req ImportRecordingDraftsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	resp, err := h.service.ImportRecordingDrafts(r.Context(), tenantID, claims.UserID, req.RecordingIDs)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": resp})
}

func (h *Handler) GetRecord(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	recordID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || recordID <= 0 {
		httputil.WriteBadRequest(w, "invalid record id")
		return
	}
	resp, err := h.service.GetRecord(r.Context(), tenantID, recordID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if resp == nil {
		httputil.WriteNotFound(w, "record not found")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": resp})
}

func (h *Handler) SaveRecord(w http.ResponseWriter, r *http.Request) {
	h.writeRecordMutation(w, r, func(ctx context.Context, tenantID, recordID, actorID int64, actorType string, req SaveRecordRequest) (*RecordWriteResponse, error) {
		return h.service.SaveRecord(ctx, tenantID, recordID, actorID, actorType, req)
	})
}

func (h *Handler) GetEncounter(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	encounterID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || encounterID <= 0 {
		httputil.WriteBadRequest(w, "invalid encounter id")
		return
	}
	resp, err := h.service.GetEncounter(r.Context(), tenantID, encounterID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if resp == nil {
		httputil.WriteNotFound(w, "encounter not found")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": resp})
}

func (h *Handler) SubmitRecord(w http.ResponseWriter, r *http.Request) {
	h.writeRecordMutation(w, r, func(ctx context.Context, tenantID, recordID, actorID int64, actorType string, req SubmitRecordRequest) (*RecordWriteResponse, error) {
		return h.service.SubmitRecord(ctx, tenantID, recordID, actorID, actorType, req)
	})
}

func (h *Handler) ArchiveRecord(w http.ResponseWriter, r *http.Request) {
	h.writeRecordMutation(w, r, func(ctx context.Context, tenantID, recordID, actorID int64, actorType string, req ArchiveRecordRequest) (*RecordWriteResponse, error) {
		return h.service.ArchiveRecord(ctx, tenantID, recordID, actorID, actorType, req)
	})
}

func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	recordID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || recordID <= 0 {
		httputil.WriteBadRequest(w, "invalid record id")
		return
	}
	items, err := h.service.ListVersions(r.Context(), tenantID, recordID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) ListAuditEvents(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	recordID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || recordID <= 0 {
		httputil.WriteBadRequest(w, "invalid record id")
		return
	}
	items, err := h.service.ListAuditEvents(r.Context(), tenantID, recordID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) writeRecordMutation(w http.ResponseWriter, r *http.Request, fn any) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	recordID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || recordID <= 0 {
		httputil.WriteBadRequest(w, "invalid record id")
		return
	}
	switch handler := fn.(type) {
	case func(context.Context, int64, int64, int64, string, SaveRecordRequest) (*RecordWriteResponse, error):
		var req SaveRecordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.WriteBadRequest(w, "Invalid request body")
			return
		}
		resp, err := handler(r.Context(), tenantID, recordID, claims.UserID, string(claims.UserType), req)
		if err != nil {
			writeRecordMutationError(w, err)
			return
		}
		if resp == nil {
			httputil.WriteNotFound(w, "record not found")
			return
		}
		httputil.WriteSuccess(w, map[string]any{"data": resp})
	case func(context.Context, int64, int64, int64, string, SubmitRecordRequest) (*RecordWriteResponse, error):
		var req SubmitRecordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.WriteBadRequest(w, "Invalid request body")
			return
		}
		resp, err := handler(r.Context(), tenantID, recordID, claims.UserID, string(claims.UserType), req)
		if err != nil {
			writeRecordMutationError(w, err)
			return
		}
		if resp == nil {
			httputil.WriteNotFound(w, "record not found")
			return
		}
		httputil.WriteSuccess(w, map[string]any{"data": resp})
	case func(context.Context, int64, int64, int64, string, ArchiveRecordRequest) (*RecordWriteResponse, error):
		var req ArchiveRecordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.WriteBadRequest(w, "Invalid request body")
			return
		}
		resp, err := handler(r.Context(), tenantID, recordID, claims.UserID, string(claims.UserType), req)
		if err != nil {
			writeRecordMutationError(w, err)
			return
		}
		if resp == nil {
			httputil.WriteNotFound(w, "record not found")
			return
		}
		httputil.WriteSuccess(w, map[string]any{"data": resp})
	default:
		httputil.WriteInternalError(w, "unsupported record mutation")
	}
}

func writeRecordMutationError(w http.ResponseWriter, err error) {
	var blockErr *emrrule.BlockError
	if errors.As(err, &blockErr) {
		httputil.WriteError(w, http.StatusConflict, "EMR_RULE_BLOCKED", blockErr.Message, map[string]any{
			"summary": blockErr.Summary,
			"hits":    blockErr.Hits,
		})
		return
	}
	httputil.WriteInternalError(w, err.Error())
}

func parseIntDefault(value string, fallback int) int {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

var _ = fmt.Sprintf
