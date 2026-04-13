package content

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ListContents handles listing contents
func (h *Handler) ListContents(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req ContentListRequest

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
		httputil.WritePaginated(w, []*ContentResponse{}, 0, 1, 20)
		return
	}
	if scope.TenantID != nil {
		req.TenantID = scope.TenantID
	} else {
		req.TenantIDs = scope.TenantIDs
	}

	if topicIDStr := r.URL.Query().Get("topic_id"); topicIDStr != "" {
		topicID, _ := strconv.ParseInt(topicIDStr, 10, 64)
		req.TopicID = &topicID
	}

	if status := r.URL.Query().Get("status"); status != "" {
		req.Status = &status
	}

	if category := r.URL.Query().Get("category"); category != "" {
		req.Category = &category
	}
	if createdByStr := r.URL.Query().Get("created_by"); createdByStr != "" {
		createdBy, _ := strconv.ParseInt(createdByStr, 10, 64)
		req.CreatedBy = &createdBy
	}
	if startDate := r.URL.Query().Get("start_date"); startDate != "" {
		req.StartDate = &startDate
	}
	if endDate := r.URL.Query().Get("end_date"); endDate != "" {
		req.EndDate = &endDate
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	contents, total, err := h.service.ListContents(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, contents, int64(total), req.Page, req.PageSize)
}

// GetContent handles getting content by ID
func (h *Handler) GetContent(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("content_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid content ID")
		return
	}

	content, err := h.service.GetContentByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && content.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	httputil.WriteSuccess(w, content)
}

// GenerateContent handles AI content generation
func (h *Handler) GenerateContent(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req GenerateContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}
	if claims.UserType == auth.UserTypeAdmin && tenantID == 0 {
		if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
			if parsed, err := strconv.ParseInt(tenantIDStr, 10, 64); err == nil {
				tenantID = parsed
			}
		}
	}
	if tenantID == 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
		return
	}

	content, err := h.service.GenerateContent(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, content)
}

// CreateContent handles creating content
func (h *Handler) CreateContent(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}
	if claims.UserType == auth.UserTypeAdmin && tenantID == 0 {
		if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
			if parsed, err := strconv.ParseInt(tenantIDStr, 10, 64); err == nil {
				tenantID = parsed
			}
		}
	}
	if tenantID == 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
		return
	}

	content, err := h.service.CreateContent(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, content)
}

// UpdateContent handles updating content
func (h *Handler) UpdateContent(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("content_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid content ID")
		return
	}

	var req UpdateContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	existing, err := h.service.GetContentByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existing.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	content, err := h.service.UpdateContent(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, content)
}

// DeleteContent handles deleting content
func (h *Handler) DeleteContent(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("content_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid content ID")
		return
	}

	existing, err := h.service.GetContentByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existing.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	if err := h.service.DeleteContent(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Content deleted successfully"})
}

// GenerateImages handles batch image generation
func (h *Handler) GenerateImages(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("content_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid content ID")
		return
	}

	var req GenerateImagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	existing, err := h.service.GetContentByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existing.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	response, err := h.service.GenerateImages(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, response)
}

// GenerateSingleImage handles single image generation
func (h *Handler) GenerateSingleImage(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("content_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid content ID")
		return
	}

	var req GenerateSingleImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	existing, err := h.service.GetContentByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existing.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	response, err := h.service.GenerateSingleImage(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, response)
}

// SaveComposedImages handles saving composed images
func (h *Handler) SaveComposedImages(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("content_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid content ID")
		return
	}

	var req SaveComposedImagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	existing, err := h.service.GetContentByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existing.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	content, err := h.service.SaveComposedImages(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, content)
}

// PublishContent handles publishing content
func (h *Handler) PublishContent(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("content_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid content ID")
		return
	}

	existing, err := h.service.GetContentByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existing.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	content, err := h.service.PublishContent(r.Context(), id)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, content)
}

// UnpublishContent handles unpublishing content
func (h *Handler) UnpublishContent(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("content_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid content ID")
		return
	}

	existing, err := h.service.GetContentByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existing.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	content, err := h.service.UnpublishContent(r.Context(), id)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, content)
}
