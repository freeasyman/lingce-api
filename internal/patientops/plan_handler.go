package patientops

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

func planTenantAndEmployee(r *http.Request) (int64, int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		return 0, 0, false
	}
	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}
	if claims.UserType == auth.UserTypeAdmin {
		if raw := r.URL.Query().Get("tenant_id"); raw != "" {
			if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
				tenantID = parsed
			}
		}
	}
	if tenantID <= 0 || claims.UserID <= 0 {
		return 0, 0, false
	}
	return tenantID, claims.UserID, true
}

func planPathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid plan id")
	}
	return id, nil
}

func (h *Handler) ListPlans(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := planTenantAndEmployee(r)
	if !ok {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	items, total, err := h.service.ListPlans(r.Context(), tenantID, page, pageSize, r.URL.Query().Get("status"))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if items == nil {
		items = []Plan{}
	}
	httputil.WritePaginated(w, items, total, page, pageSize)
}

func (h *Handler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	tenantID, employeeID, ok := planTenantAndEmployee(r)
	if !ok {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	var req CreatePlanRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 256*1024)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	detail, err := h.service.CreatePlan(r.Context(), tenantID, employeeID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, detail)
}

func (h *Handler) GetPlan(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := planTenantAndEmployee(r)
	if !ok {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	planID, err := planPathID(r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	detail, err := h.service.GetPlan(r.Context(), tenantID, planID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, detail)
}

func (h *Handler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := planTenantAndEmployee(r)
	if !ok {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	planID, err := planPathID(r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req UpdatePlanRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 256*1024)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	detail, err := h.service.UpdatePlan(r.Context(), tenantID, planID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, detail)
}

func (h *Handler) PreviewPlan(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := planTenantAndEmployee(r)
	if !ok {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	planID, err := planPathID(r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	preview, err := h.service.PreviewPlan(r.Context(), tenantID, planID)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, preview)
}

func (h *Handler) PublishPlan(w http.ResponseWriter, r *http.Request) {
	tenantID, employeeID, ok := planTenantAndEmployee(r)
	if !ok {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	planID, err := planPathID(r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req PublishPlanRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(io.LimitReader(r.Body, 256*1024)).Decode(&req); err != nil {
			httputil.WriteBadRequest(w, "invalid request body")
			return
		}
	}
	detail, err := h.service.PublishPlan(r.Context(), tenantID, employeeID, planID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, detail)
}

func (h *Handler) AddPlanMembers(w http.ResponseWriter, r *http.Request) {
	tenantID, employeeID, ok := planTenantAndEmployee(r)
	if !ok {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	planID, err := planPathID(r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req AddPlanMembersRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 256*1024)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	detail, err := h.service.AddPlanMembers(r.Context(), tenantID, employeeID, planID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, detail)
}

func (h *Handler) ClosePlan(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := planTenantAndEmployee(r)
	if !ok {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	planID, err := planPathID(r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if err := h.service.ClosePlan(r.Context(), tenantID, planID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]bool{"closed": true})
}

func (h *Handler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	tenantID, employeeID, ok := planTenantAndEmployee(r)
	if !ok {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	planID, err := planPathID(r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if err := h.service.SoftDeletePlan(r.Context(), tenantID, planID, employeeID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]bool{"deleted": true})
}

func (h *Handler) SuggestProductKeywords(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := planTenantAndEmployee(r)
	if !ok {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	var req ProductKeywordSuggestRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	keywords, err := h.service.SuggestProductKeywords(r.Context(), tenantID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"keywords": keywords})
}
