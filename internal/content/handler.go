package content

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers content module routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)

	// Topic management endpoints (P0 priority only)
	mux.Handle("GET /api/v1/content/topics", authMw(http.HandlerFunc(h.ListTopics)))
	mux.Handle("GET /api/v1/content/topics/{topic_id}", authMw(http.HandlerFunc(h.GetTopic)))
	mux.Handle("POST /api/v1/content/topics", authMw(http.HandlerFunc(h.CreateTopic)))
	mux.Handle("PUT /api/v1/content/topics/{topic_id}", authMw(http.HandlerFunc(h.UpdateTopic)))
	mux.Handle("DELETE /api/v1/content/topics/{topic_id}", authMw(http.HandlerFunc(h.DeleteTopic)))
	mux.Handle("POST /api/v1/content/topics/generate", authMw(http.HandlerFunc(h.GenerateTopics)))

	// TODO: Implement remaining endpoints in future phases:
	// - Content management (11 endpoints)
	// - Publish tasks (9 endpoints)
	// - Content seeds (11 endpoints)
	// - Prompt templates (22 endpoints)
	// - GEO optimization (4 endpoints)
	// - Conversation insights (4 endpoints)
}

// Topic Handlers

// ListTopics handles listing topics
func (h *Handler) ListTopics(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req TopicListRequest

	// Admin can view all tenants, employees can only view their own tenant
	if claims.UserType != auth.UserTypeAdmin {
		req.TenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tenantID, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		req.TenantID = &tenantID
	}

	if status := r.URL.Query().Get("status"); status != "" {
		req.Status = &status
	}

	if category := r.URL.Query().Get("category"); category != "" {
		req.Category = &category
	}

	if source := r.URL.Query().Get("source"); source != "" {
		req.Source = &source
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

	topics, total, err := h.service.ListTopics(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, topics, int64(total), req.Page, req.PageSize)
}

// GetTopic handles getting topic by ID
func (h *Handler) GetTopic(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("topic_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid topic ID")
		return
	}

	topic, err := h.service.GetTopicByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	// Check tenant access
	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && topic.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	httputil.WriteSuccess(w, topic)
}

// CreateTopic handles creating a topic
func (h *Handler) CreateTopic(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateTopicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	// Get tenant ID
	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}

	topic, err := h.service.CreateTopic(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, topic)
}

// UpdateTopic handles updating a topic
func (h *Handler) UpdateTopic(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("topic_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid topic ID")
		return
	}

	// Check tenant access
	existingTopic, err := h.service.GetTopicByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingTopic.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req UpdateTopicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	topic, err := h.service.UpdateTopic(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, topic)
}

// DeleteTopic handles deleting a topic
func (h *Handler) DeleteTopic(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("topic_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid topic ID")
		return
	}

	// Check tenant access
	existingTopic, err := h.service.GetTopicByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingTopic.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	if err := h.service.DeleteTopic(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Topic deleted successfully"})
}

// GenerateTopics handles AI topic generation
func (h *Handler) GenerateTopics(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req GenerateTopicsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	// Get tenant ID
	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}

	topics, err := h.service.GenerateTopics(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, topics)
}
