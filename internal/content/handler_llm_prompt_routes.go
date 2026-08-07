package content

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

func (h *Handler) registerLLMPromptRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/llm/prompts", Handler: h.ListTemplates, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/llm/prompts", Handler: h.CreateTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/llm/prompts/{template_id}", Handler: h.GetTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/llm/prompts/{template_id}", Handler: h.UpdateTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/llm/prompts/{template_id}", Handler: h.DeleteTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/llm/prompts/{template_id}/versions", Handler: h.ListTemplateVersions, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/llm/prompts/{template_id}/actions/publish", Handler: h.PublishTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/llm/prompts/{template_id}/actions/rollback", Handler: h.RollbackLLMPromptAction, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, deps)
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
