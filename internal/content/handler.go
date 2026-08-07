package content

import (
	"encoding/json"
	"net/http"
	"strconv"

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

// RegisterRoutes registers content module routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	deps := router.RouteDeps{JWTSecret: jwtSecret}
	h.registerTopicRoutes(mux, deps)
	h.registerContentItemRoutes(mux, deps)
	h.registerContentSeedRoutes(mux, deps)
	h.registerLLMPromptRoutes(mux, deps)
	h.registerContentPromptRoutes(mux, deps)
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
		httputil.WritePaginated(w, []*TopicResponse{}, 0, 1, 20)
		return
	}
	if scope.TenantID != nil {
		req.TenantID = scope.TenantID
	} else {
		req.TenantIDs = scope.TenantIDs
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

	id, err := parseTopicID(r)
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
	if err := tenancy.RequireSameTenant(claims, topic.TenantID); err != nil {
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

	tenantID, err := tenancy.RequireTenantID(claims, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
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

	id, err := parseTopicID(r)
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

	if err := tenancy.RequireSameTenant(claims, existingTopic.TenantID); err != nil {
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

	id, err := parseTopicID(r)
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

	if err := tenancy.RequireSameTenant(claims, existingTopic.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	if err := h.service.DeleteTopic(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Topic deleted successfully"})
}

func parseTopicID(r *http.Request) (int64, error) {
	idStr := r.PathValue("topic_id")
	if idStr == "" {
		idStr = r.PathValue("id")
	}
	return strconv.ParseInt(idStr, 10, 64)
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
	tenantID, err := tenancy.RequireTenantID(claims, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	topics, err := h.service.GenerateTopics(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, topics)
}
