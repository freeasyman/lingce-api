package recording

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ===== Lingce Sales Handler =====

// RegisterSalesRoutes registers all lingce-sales API routes.
func (h *Handler) RegisterSalesRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "POST", Path: "/api/v1/lingce-sales/prospects", Handler: h.CreateSalesProspect, Auth: true},
		{Method: "GET", Path: "/api/v1/lingce-sales/prospects", Handler: h.ListSalesProspects, Auth: true},
		{Method: "GET", Path: "/api/v1/lingce-sales/prospects/{id}", Handler: h.GetSalesProspect, Auth: true},
		{Method: "PUT", Path: "/api/v1/lingce-sales/prospects/{id}", Handler: h.UpdateSalesProspect, Auth: true},
		{Method: "POST", Path: "/api/v1/lingce-sales/prospects/{id}/confirm-stage-change", Handler: h.ConfirmSalesStageChange, Auth: true},
		{Method: "GET", Path: "/api/v1/lingce-sales/prospects/{id}/stage-history", Handler: h.GetSalesStageHistory, Auth: true},
		{Method: "POST", Path: "/api/v1/lingce-sales/recordings/{id}/associate-prospect", Handler: h.AssociateSalesRecordingToProspect, Auth: true},
		{Method: "GET", Path: "/api/v1/lingce-sales/prospects/{id}/recordings", Handler: h.GetSalesProspectRecordings, Auth: true},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

// CreateSalesProspect handles POST /api/v1/lingce-sales/prospects
func (h *Handler) CreateSalesProspect(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "unauthorized")
		return
	}

	var req CreateLingceSalesProspectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}

	if req.InstitutionName == "" || req.ContactName == "" || req.ContactPhone == "" {
		httputil.WriteBadRequest(w, "institution_name, contact_name, contact_phone are required")
		return
	}

	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	prospect, err := h.service.CreateLingceSalesProspect(r.Context(), tenantID, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, ProspectToResponse(prospect))
}

// ListSalesProspects handles GET /api/v1/lingce-sales/prospects
func (h *Handler) ListSalesProspects(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "unauthorized")
		return
	}

	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	listReq := LingceSalesProspectListRequest{
		TenantID: tenantID,
		Page:     page,
		PageSize: pageSize,
	}

	if v := r.URL.Query().Get("decision_stage"); v != "" {
		listReq.DecisionStage = &v
	}
	if v := r.URL.Query().Get("deal_probability"); v != "" {
		listReq.DealProbability = &v
	}
	if v := r.URL.Query().Get("status"); v != "" {
		listReq.Status = &v
	}

	prospects, total, err := h.service.ListLingceSalesProspects(r.Context(), listReq)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	var items []LingceSalesProspectResponse
	for _, p := range prospects {
		items = append(items, ProspectToResponse(p))
	}

	httputil.WritePaginated(w, items, int64(total), page, pageSize)
}

// GetSalesProspect handles GET /api/v1/lingce-sales/prospects/{id}
func (h *Handler) GetSalesProspect(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "unauthorized")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid prospect id")
		return
	}

	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	prospect, err := h.service.GetLingceSalesProspect(r.Context(), tenantID, id)
	if err != nil {
		httputil.WriteNotFound(w, "prospect not found")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, ProspectToResponse(prospect))
}

// UpdateSalesProspect handles PUT /api/v1/lingce-sales/prospects/{id}
func (h *Handler) UpdateSalesProspect(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "unauthorized")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid prospect id")
		return
	}

	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	var req UpdateLingceSalesProspectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}

	if err := h.service.UpdateLingceSalesProspect(r.Context(), tenantID, id, req); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": "updated"})
}

// ConfirmSalesStageChange handles POST /api/v1/lingce-sales/prospects/{id}/confirm-stage-change
func (h *Handler) ConfirmSalesStageChange(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "unauthorized")
		return
	}

	prospectID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid prospect id")
		return
	}

	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	var req ConfirmStageChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}

	if req.ToStage == "" {
		httputil.WriteBadRequest(w, "to_stage is required")
		return
	}

	// Optional recording_id from query param
	var recordingID *int64
	if ridStr := r.URL.Query().Get("recording_id"); ridStr != "" {
		rid, err := strconv.ParseInt(ridStr, 10, 64)
		if err == nil {
			recordingID = &rid
		}
	}

	sc, err := h.service.ConfirmStageChange(r.Context(), tenantID, prospectID, recordingID, claims.UserID, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, StageChangeToResponse(sc))
}

// GetSalesStageHistory handles GET /api/v1/lingce-sales/prospects/{id}/stage-history
func (h *Handler) GetSalesStageHistory(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "unauthorized")
		return
	}

	prospectID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid prospect id")
		return
	}

	changes, err := h.service.GetProspectStageHistory(r.Context(), prospectID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	var items []LingceSalesStageChangeResponse
	for _, sc := range changes {
		items = append(items, StageChangeToResponse(sc))
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": items,
	})
}

// AssociateSalesRecordingToProspect handles POST /api/v1/lingce-sales/recordings/{id}/associate-prospect
func (h *Handler) AssociateSalesRecordingToProspect(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "unauthorized")
		return
	}

	recordingID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}

	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	var req AssociateRecordingToProspectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}

	if req.ProspectID == 0 || req.ConversationType == "" || req.ConversationPurpose == "" {
		httputil.WriteBadRequest(w, "prospect_id, conversation_type, conversation_purpose are required")
		return
	}

	id, err := h.service.AssociateRecordingToProspect(r.Context(), tenantID, recordingID, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"id":      id,
		"message": "associated",
	})
}

// GetSalesProspectRecordings handles GET /api/v1/lingce-sales/prospects/{id}/recordings
func (h *Handler) GetSalesProspectRecordings(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "unauthorized")
		return
	}

	prospectID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid prospect id")
		return
	}

	recs, err := h.service.GetProspectRecordings(r.Context(), prospectID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": recs,
	})
}
