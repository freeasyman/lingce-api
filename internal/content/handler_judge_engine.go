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

// GenerateTopicsFromInsights 从洞察生成选题。
func (h *Handler) GenerateTopicsFromInsights(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req struct {
		InsightIDs []int64 `json:"insight_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	topics, err := h.service.GenerateTopicsFromInsights(r.Context(), tenantID, req.InsightIDs)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, topics)
}

// GenerateTopicsFromFrequentQuestion 从「高频问题」整体生成 1~3 个选题。
func (h *Handler) GenerateTopicsFromFrequentQuestion(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req struct {
		FrequentQuestionID int64 `json:"frequent_question_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.FrequentQuestionID <= 0 {
		httputil.WriteBadRequest(w, "frequent_question_id is required")
		return
	}
	topics, err := h.service.GenerateTopicsFromFrequentQuestion(r.Context(), tenantID, req.FrequentQuestionID)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, topics)
}

// GetTopic 选题详情（含判断信息）。
func (h *Handler) GetTopic(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid topic id")
		return
	}
	topic, err := h.service.store.getTopicV2(r.Context(), tenantID, id)
	if err != nil {
		writeContentError(w, err)
		return
	}
	if topic == nil {
		httputil.WriteNotFound(w, "topic not found")
		return
	}
	httputil.WriteSuccess(w, topic)
}

// EnrichTopic 补全选题判断信息。
func (h *Handler) EnrichTopic(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid topic id")
		return
	}
	result, err := h.service.EnrichTopic(r.Context(), tenantID, id)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// EnrichTopicsBatch 批量补全选题判断信息。
func (h *Handler) EnrichTopicsBatch(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	n, err := h.service.EnrichTopicsBatch(r.Context(), tenantID, limit)
	if err != nil {
		writeContentError(w, err)
		return
	}
	remaining, _ := h.service.store.countTopicsWithoutHelp(r.Context(), tenantID)
	httputil.WriteSuccess(w, map[string]int{"enriched": n, "remaining": remaining})
}

// CreateManualTopic 手动建"计划型"选题。
func (h *Handler) CreateManualTopic(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Form        string `json:"form"`
		Goal        string `json:"goal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.Title == "" {
		httputil.WriteBadRequest(w, "title is required")
		return
	}
	id, err := h.service.store.insertManualTopic(r.Context(), tenantID, req.Title, req.Description, req.Form, req.Goal)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]int64{"id": id})
}

func (h *Handler) registerJudgeEngineRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	routes := []router.Route{
		{Method: "POST", Path: "/api/v1/content-topics", Handler: h.CreateManualTopic, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-topics/actions/generate-from-insights", Handler: h.GenerateTopicsFromInsights, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-topics/actions/generate-from-frequent-question", Handler: h.GenerateTopicsFromFrequentQuestion, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-topics/{id}", Handler: h.GetTopic, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-topics/{id}/actions/enrich", Handler: h.EnrichTopic, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-topics/actions/enrich-batch", Handler: h.EnrichTopicsBatch, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, deps)
}
