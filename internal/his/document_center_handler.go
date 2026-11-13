package his

import (
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ListDocumentTemplates 文书模板列表（按租户）。
func (h *Handler) ListDocumentTemplates(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListDocumentTemplates(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list document templates")
		return
	}
	httputil.WriteSuccess(w, items)
}

// GetDocumentPrefill 文书自动带出字段（患者/诊断/医生/日期/处方/治疗）。
func (h *Handler) GetDocumentPrefill(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	encounterID, _ := strconv.ParseInt(r.URL.Query().Get("encounter_id"), 10, 64)
	prefill, err := h.service.DocumentPrefill(r.Context(), tenantID, encounterID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, prefill)
}

// CreateEncounterDocument 生成就诊文书（模板 + extra_fields）。
func (h *Handler) CreateEncounterDocument(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	var req CreateEncounterDocumentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	doc, err := h.service.GenerateEncounterDocument(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, doc)
}

// DeleteEncounterDocument 删除未签名文书。
func (h *Handler) DeleteEncounterDocument(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteEncounterDocument(r.Context(), tenantID, id); err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"id": id, "deleted": true})
}
