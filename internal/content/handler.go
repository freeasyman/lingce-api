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

	// Topic management endpoints
	mux.Handle("GET /api/v1/content/hot-topics", authMw(http.HandlerFunc(h.GetHotTopics)))
	mux.Handle("POST /api/v1/content/hot-topics/refresh", authMw(http.HandlerFunc(h.RefreshHotTopics)))
	mux.Handle("GET /api/v1/content/topics", authMw(http.HandlerFunc(h.ListTopics)))
	mux.Handle("GET /api/v1/content/topics/{topic_id}", authMw(http.HandlerFunc(h.GetTopic)))
	mux.Handle("POST /api/v1/content/topics/generate", authMw(http.HandlerFunc(h.GenerateTopics)))
	mux.Handle("POST /api/v1/content/topics", authMw(http.HandlerFunc(h.CreateTopic)))
	mux.Handle("PUT /api/v1/content/topics/{topic_id}", authMw(http.HandlerFunc(h.UpdateTopic)))
	mux.Handle("PUT /api/v1/content/topics/{topic_id}/select", authMw(http.HandlerFunc(h.SelectTopic)))
	mux.Handle("DELETE /api/v1/content/topics/{topic_id}", authMw(http.HandlerFunc(h.DeleteTopic)))

	// "我有想法" endpoints
	mux.Handle("POST /api/v1/content/idea-topics/start", authMw(http.HandlerFunc(h.IdeaTopicStart)))
	mux.Handle("POST /api/v1/content/idea-topics/parse-files", authMw(http.HandlerFunc(h.ParseFiles)))
	mux.Handle("POST /api/v1/content/idea-topics/generate", authMw(http.HandlerFunc(h.IdeaGenerateTopics)))
	mux.Handle("POST /api/v1/content/idea-topics/save-topics", authMw(http.HandlerFunc(h.SaveIdeaTopics)))

	// Content management endpoints
	mux.Handle("GET /api/v1/content/contents", authMw(http.HandlerFunc(h.ListContents)))
	mux.Handle("GET /api/v1/content/contents/{content_id}", authMw(http.HandlerFunc(h.GetContent)))
	mux.Handle("POST /api/v1/content/contents/generate", authMw(http.HandlerFunc(h.GenerateContent)))
	mux.Handle("POST /api/v1/content/contents", authMw(http.HandlerFunc(h.CreateContent)))
	mux.Handle("PUT /api/v1/content/contents/{content_id}", authMw(http.HandlerFunc(h.UpdateContent)))
	mux.Handle("DELETE /api/v1/content/contents/{content_id}", authMw(http.HandlerFunc(h.DeleteContent)))
	mux.Handle("POST /api/v1/content/contents/{content_id}/generate-images", authMw(http.HandlerFunc(h.GenerateImages)))
	mux.Handle("POST /api/v1/content/contents/{content_id}/generate-single-image", authMw(http.HandlerFunc(h.GenerateSingleImage)))
	mux.Handle("POST /api/v1/content/contents/{content_id}/save-composed-images", authMw(http.HandlerFunc(h.SaveComposedImages)))
	mux.Handle("POST /api/v1/content/contents/{content_id}/publish", authMw(http.HandlerFunc(h.PublishContent)))
	mux.Handle("POST /api/v1/content/contents/{content_id}/unpublish", authMw(http.HandlerFunc(h.UnpublishContent)))

	// Conversation insights endpoints
	mux.Handle("GET /api/v1/content/conversation-insights/stats", authMw(http.HandlerFunc(h.GetInsightsStats)))
	mux.Handle("GET /api/v1/content/conversation-insights/frequent-questions", authMw(http.HandlerFunc(h.GetFrequentQuestions)))
	mux.Handle("POST /api/v1/content/conversation-insights/mine-topics", authMw(http.HandlerFunc(h.MineTopics)))
	mux.Handle("POST /api/v1/content/conversation-insights/save-topics", authMw(http.HandlerFunc(h.SaveMinedTopics)))

	// Content seeds endpoints
	mux.Handle("GET /api/v1/content-seeds", authMw(http.HandlerFunc(h.ListSeeds)))
	mux.Handle("GET /api/v1/content-seeds/stats", authMw(http.HandlerFunc(h.GetSeedStats)))
	mux.Handle("GET /api/v1/content-seeds/clusters", authMw(http.HandlerFunc(h.GetClusters)))
	mux.Handle("GET /api/v1/content-seeds/my-inspirations", authMw(http.HandlerFunc(h.GetMyInspirations)))
	mux.Handle("POST /api/v1/content-seeds/{seed_id}/generate-draft", authMw(http.HandlerFunc(h.GenerateDraftFromSeed)))
	mux.Handle("POST /api/v1/content-seeds/{seed_id}/dismiss", authMw(http.HandlerFunc(h.DismissSeed)))
	mux.Handle("GET /api/v1/content-seeds/{seed_id}", authMw(http.HandlerFunc(h.GetSeed)))
	mux.Handle("PATCH /api/v1/content-seeds/{seed_id}/status", authMw(http.HandlerFunc(h.UpdateSeedStatus)))
	mux.Handle("GET /api/v1/content-seeds/honor-list", authMw(http.HandlerFunc(h.GetHonorList)))
	mux.Handle("GET /api/v1/content-seeds/my-stats", authMw(http.HandlerFunc(h.GetMyStats)))
	mux.Handle("GET /api/v1/content-seeds/my-adopted", authMw(http.HandlerFunc(h.GetMyAdopted)))

	// Prompt templates endpoints
	mux.Handle("GET /api/v1/prompt-templates", authMw(http.HandlerFunc(h.ListTemplates)))
	mux.Handle("POST /api/v1/prompt-templates", authMw(http.HandlerFunc(h.CreateTemplate)))
	mux.Handle("GET /api/v1/prompt-templates/{template_id}", authMw(http.HandlerFunc(h.GetTemplate)))
	mux.Handle("PUT /api/v1/prompt-templates/{template_id}", authMw(http.HandlerFunc(h.UpdateTemplate)))
	mux.Handle("DELETE /api/v1/prompt-templates/{template_id}", authMw(http.HandlerFunc(h.DeleteTemplate)))
	mux.Handle("POST /api/v1/prompt-templates/preview", authMw(http.HandlerFunc(h.PreviewTemplate)))
	mux.Handle("POST /api/v1/prompt-templates/{template_id}/clone", authMw(http.HandlerFunc(h.CloneTemplate)))
	mux.Handle("POST /api/v1/prompt-templates/{template_id}/versions", authMw(http.HandlerFunc(h.CreateTemplateVersion)))
	mux.Handle("GET /api/v1/prompt-templates/{template_id}/versions", authMw(http.HandlerFunc(h.ListTemplateVersions)))
	mux.Handle("POST /api/v1/prompt-templates/{template_id}/publish", authMw(http.HandlerFunc(h.PublishTemplate)))
	mux.Handle("POST /api/v1/prompt-templates/{template_id}/rollback/{version}", authMw(http.HandlerFunc(h.RollbackTemplate)))
	mux.Handle("POST /api/v1/prompt-templates/{template_id}/test", authMw(http.HandlerFunc(h.TestTemplate)))
	mux.Handle("GET /api/v1/prompt-templates/{template_id}/stats", authMw(http.HandlerFunc(h.GetTemplateStats)))

	// Content prompt templates endpoints
	mux.Handle("GET /api/v1/content-prompt-templates", authMw(http.HandlerFunc(h.ListContentTemplates)))
	mux.Handle("POST /api/v1/content-prompt-templates", authMw(http.HandlerFunc(h.CreateContentTemplate)))
	mux.Handle("GET /api/v1/content-prompt-templates/{template_id}", authMw(http.HandlerFunc(h.GetContentTemplate)))
	mux.Handle("PUT /api/v1/content-prompt-templates/{template_id}", authMw(http.HandlerFunc(h.UpdateContentTemplate)))
	mux.Handle("DELETE /api/v1/content-prompt-templates/{template_id}", authMw(http.HandlerFunc(h.DeleteContentTemplate)))
	mux.Handle("POST /api/v1/content-prompt-templates/{template_id}/clone", authMw(http.HandlerFunc(h.CloneContentTemplate)))
	mux.Handle("POST /api/v1/content-prompt-templates/{template_id}/test", authMw(http.HandlerFunc(h.TestContentTemplate)))
	mux.Handle("GET /api/v1/content-prompt-templates/{template_id}/stats", authMw(http.HandlerFunc(h.GetContentTemplateStats)))
	mux.Handle("POST /api/v1/content-prompt-templates/initialize-defaults", authMw(http.HandlerFunc(h.InitializeDefaultTemplates)))

	// Publish tasks endpoints
	mux.Handle("GET /api/v1/content/publish-tasks/dashboard", authMw(http.HandlerFunc(h.GetPublishDashboard)))
	mux.Handle("GET /api/v1/content/publish-tasks", authMw(http.HandlerFunc(h.ListPublishTasks)))
	mux.Handle("GET /api/v1/content/publish-tasks/{task_id}", authMw(http.HandlerFunc(h.GetPublishTask)))
	mux.Handle("POST /api/v1/content/publish-tasks", authMw(http.HandlerFunc(h.CreatePublishTask)))
	mux.Handle("POST /api/v1/content/publish-tasks/batch", authMw(http.HandlerFunc(h.BatchCreatePublishTasks)))
	mux.Handle("PUT /api/v1/content/publish-tasks/{task_id}/status", authMw(http.HandlerFunc(h.UpdatePublishTaskStatus)))
	mux.Handle("PUT /api/v1/content/publish-tasks/{task_id}/cancel", authMw(http.HandlerFunc(h.CancelPublishTask)))
	mux.Handle("PUT /api/v1/content/publish-tasks/{task_id}/retry", authMw(http.HandlerFunc(h.RetryPublishTask)))
	mux.Handle("DELETE /api/v1/content/publish-tasks/{task_id}", authMw(http.HandlerFunc(h.DeletePublishTask)))

	// GEO optimization endpoints
	mux.Handle("POST /api/v1/content/geo/analyze", authMw(http.HandlerFunc(h.AnalyzeGEO)))
	mux.Handle("POST /api/v1/content/geo/analyze-by-id/{content_id}", authMw(http.HandlerFunc(h.AnalyzeGEOByID)))
	mux.Handle("POST /api/v1/content/geo/optimize", authMw(http.HandlerFunc(h.OptimizeGEO)))
	mux.Handle("GET /api/v1/content/geo/prompt-injection", authMw(http.HandlerFunc(h.GetGEOPromptInjection)))
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
