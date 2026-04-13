package content

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ListContentTemplates handles listing content prompt templates
func (h *Handler) ListContentTemplates(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req TemplateListRequest

	scope, err := h.resolveTenantScope(claims, r)
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if len(scope.TenantIDs) == 0 {
		httputil.WritePaginated(w, []TemplateResponse{}, 0, 1, 20)
		return
	}
	if scope.TenantID != nil {
		req.TenantID = scope.TenantID
	} else {
		req.TenantIDs = scope.TenantIDs
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	templates, total, err := h.service.ListPromptTemplates(r.Context(), req, true)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, templates, int64(total), req.Page, req.PageSize)
}

// CreateContentTemplate handles creating a content prompt template
func (h *Handler) CreateContentTemplate(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	var tenantID *int64
	if claims.UserType != auth.UserTypeAdmin {
		tenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tid, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		tenantID = &tid
	}
	template, err := h.service.CreatePromptTemplate(r.Context(), tenantID, claims.UserID, req, true)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, template)
}

// GetContentTemplate handles getting content template by ID
func (h *Handler) GetContentTemplate(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("template_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid template ID")
		return
	}

	template, err := h.service.GetPromptTemplateByID(r.Context(), id, true)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, template)
}

// UpdateContentTemplate handles updating a content template
func (h *Handler) UpdateContentTemplate(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("template_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid template ID")
		return
	}

	var req UpdateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	template, err := h.service.UpdatePromptTemplate(r.Context(), id, req, true)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, template)
}

// DeleteContentTemplate handles deleting a content template
func (h *Handler) DeleteContentTemplate(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("template_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid template ID")
		return
	}

	if err := h.service.DeletePromptTemplate(r.Context(), id, true); err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Content template deleted successfully"})
}

// CloneContentTemplate handles cloning a content template
func (h *Handler) CloneContentTemplate(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("template_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid template ID")
		return
	}

	cloned, err := h.service.ClonePromptTemplate(r.Context(), id, claims.UserID, true)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, cloned)
}

// TestContentTemplate handles testing a content template
func (h *Handler) TestContentTemplate(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("template_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid template ID")
		return
	}

	result, err := h.service.TestPromptTemplate(r.Context(), id, true)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"result": result})
}

// GetContentTemplateStats handles getting content template usage statistics
func (h *Handler) GetContentTemplateStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("template_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid template ID")
		return
	}

	stats, err := h.service.PromptTemplateStats(r.Context(), id, true)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, stats)
}

// InitializeDefaultTemplates handles initializing default content templates
func (h *Handler) InitializeDefaultTemplates(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var tenantID *int64
	if claims.UserType != auth.UserTypeAdmin {
		tenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tid, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		tenantID = &tid
	}
	created, err := h.service.InitializeDefaultContentTemplates(r.Context(), tenantID, claims.UserID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"created": created,
		"message": "Default templates initialized successfully",
	})
}
