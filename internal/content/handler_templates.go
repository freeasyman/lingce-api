package content

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ListTemplates handles listing prompt templates
func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req TemplateListRequest

	// Admin can view all tenants, employees can only view their own tenant
	if claims.UserType != auth.UserTypeAdmin {
		req.TenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tenantID, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		req.TenantID = &tenantID
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	templates, total, err := h.service.ListPromptTemplates(r.Context(), req, false)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, templates, int64(total), req.Page, req.PageSize)
}

// CreateTemplate handles creating a prompt template
func (h *Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
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
	template, err := h.service.CreatePromptTemplate(r.Context(), tenantID, claims.UserID, req, false)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, template)
}

// GetTemplate handles getting template by ID
func (h *Handler) GetTemplate(w http.ResponseWriter, r *http.Request) {
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

	template, err := h.service.GetPromptTemplateByID(r.Context(), id, false)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, template)
}

// UpdateTemplate handles updating a template
func (h *Handler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
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

	template, err := h.service.UpdatePromptTemplate(r.Context(), id, req, false)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, template)
}

// DeleteTemplate handles deleting a template
func (h *Handler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
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

	if err := h.service.DeletePromptTemplate(r.Context(), id, false); err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Template deleted successfully"})
}

// PreviewTemplate handles previewing template rendering
func (h *Handler) PreviewTemplate(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&req)
	templateText, _ := req["template"].(string)
	variables := map[string]string{}
	if rawVars, ok := req["variables"].(map[string]interface{}); ok {
		for k, v := range rawVars {
			variables[k] = strings.TrimSpace(toString(v))
		}
	}
	preview := templateText
	for k, v := range variables {
		preview = strings.ReplaceAll(preview, "{{"+k+"}}", v)
		preview = strings.ReplaceAll(preview, "{{ "+k+" }}", v)
	}
	httputil.WriteSuccess(w, map[string]string{"preview": preview})
}

// CloneTemplate handles cloning a template
func (h *Handler) CloneTemplate(w http.ResponseWriter, r *http.Request) {
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

	cloned, err := h.service.ClonePromptTemplate(r.Context(), id, claims.UserID, false)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, cloned)
}

// CreateTemplateVersion handles creating a template version
func (h *Handler) CreateTemplateVersion(w http.ResponseWriter, r *http.Request) {
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

	var req CreateVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	version, err := h.service.CreatePromptTemplateVersion(r.Context(), id, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, version)
}

// ListTemplateVersions handles listing template versions
func (h *Handler) ListTemplateVersions(w http.ResponseWriter, r *http.Request) {
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

	versions, err := h.service.ListPromptTemplateVersions(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, versions)
}

// PublishTemplate handles publishing a template version
func (h *Handler) PublishTemplate(w http.ResponseWriter, r *http.Request) {
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

	if err := h.service.PublishPromptTemplate(r.Context(), id); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Template published successfully"})
}

// RollbackTemplate handles rolling back to a template version
func (h *Handler) RollbackTemplate(w http.ResponseWriter, r *http.Request) {
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

	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid version")
		return
	}

	if err := h.service.RollbackPromptTemplate(r.Context(), id, version); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Template rolled back successfully"})
}

// TestTemplate handles testing a template
func (h *Handler) TestTemplate(w http.ResponseWriter, r *http.Request) {
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

	result, err := h.service.TestPromptTemplate(r.Context(), id, false)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"result": result})
}

// GetTemplateStats handles getting template usage statistics
func (h *Handler) GetTemplateStats(w http.ResponseWriter, r *http.Request) {
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

	stats, err := h.service.PromptTemplateStats(r.Context(), id, false)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, stats)
}

func toString(v interface{}) string {
	switch tv := v.(type) {
	case string:
		return tv
	default:
		return ""
	}
}
