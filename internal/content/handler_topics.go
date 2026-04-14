package content

import (
	"encoding/json"
	"net/http"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// GetHotTopics handles getting hot topics
func (h *Handler) GetHotTopics(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	resp, err := h.service.GetHotTopics(r.Context(), claims.TenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}

// RefreshHotTopics handles refreshing hot topics
func (h *Handler) RefreshHotTopics(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}

	count, err := h.service.RefreshHotTopics(r.Context(), tenantID, claims.UserID)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"message":         "Hot topics refreshed successfully",
		"generated_count": count,
	})
}

// SelectTopic handles selecting a topic
func (h *Handler) SelectTopic(w http.ResponseWriter, r *http.Request) {
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

	// Update topic status to "selected"
	status := "selected"
	req := UpdateTopicRequest{
		Status: &status,
	}

	topic, err := h.service.UpdateTopic(r.Context(), id, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, topic)
}

// IdeaTopicStart handles "我有想法" initialization
func (h *Handler) IdeaTopicStart(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req IdeaTopicStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}

	resp, err := h.service.StartIdeaTopicSession(r.Context(), claims.UserID, tenantID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}

// ParseFiles handles file parsing for idea topics
func (h *Handler) ParseFiles(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req ParseFilesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.SessionID == "" {
		httputil.WriteBadRequest(w, "session_id is required")
		return
	}

	resp, err := h.service.ParseIdeaFiles(r.Context(), claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}

// IdeaGenerateTopics handles generating topics from idea session
func (h *Handler) IdeaGenerateTopics(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req IdeaGenerateTopicsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.SessionID == "" {
		httputil.WriteBadRequest(w, "session_id is required")
		return
	}

	topics, err := h.service.GenerateIdeaTopics(r.Context(), claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, topics)
}

// SaveIdeaTopics handles saving idea topics
func (h *Handler) SaveIdeaTopics(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req SaveIdeaTopicsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.SessionID == "" {
		httputil.WriteBadRequest(w, "session_id is required")
		return
	}
	if len(req.TopicIDs) == 0 {
		httputil.WriteBadRequest(w, "topic_ids is required")
		return
	}

	count, err := h.service.SaveIdeaTopics(r.Context(), claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"message":     "Topics saved successfully",
		"saved_count": count,
	})
}
