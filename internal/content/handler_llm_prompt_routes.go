package content

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

func (h *Handler) registerLLMPromptRoutes(mux *http.ServeMux, authMw func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/llm/prompts", authMw(http.HandlerFunc(h.ListTemplates)))
	mux.Handle("POST /api/v1/llm/prompts", authMw(http.HandlerFunc(h.CreateTemplate)))
	mux.Handle("GET /api/v1/llm/prompts/{template_id}", authMw(http.HandlerFunc(h.GetTemplate)))
	mux.Handle("PUT /api/v1/llm/prompts/{template_id}", authMw(http.HandlerFunc(h.UpdateTemplate)))
	mux.Handle("DELETE /api/v1/llm/prompts/{template_id}", authMw(http.HandlerFunc(h.DeleteTemplate)))
	mux.Handle("GET /api/v1/llm/prompts/{template_id}/versions", authMw(http.HandlerFunc(h.ListTemplateVersions)))
	mux.Handle("POST /api/v1/llm/prompts/{template_id}/actions/publish", authMw(http.HandlerFunc(h.PublishTemplate)))
	mux.Handle("POST /api/v1/llm/prompts/{template_id}/actions/rollback", authMw(http.HandlerFunc(h.RollbackLLMPromptAction)))
}

type rollbackLLMPromptRequest struct {
	Version int `json:"version"`
}

// RollbackLLMPromptAction handles /llm/prompts rollback action.
func (h *Handler) RollbackLLMPromptAction(w http.ResponseWriter, r *http.Request) {
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

	var req rollbackLLMPromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Version <= 0 {
		httputil.WriteBadRequest(w, "version is required")
		return
	}

	if err := h.service.RollbackPromptTemplate(r.Context(), id, req.Version); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Template rolled back successfully"})
}
