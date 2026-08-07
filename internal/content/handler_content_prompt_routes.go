package content

import (
	"net/http"

	"github.com/freeasyman/lingce-api/internal/router"
)

func (h *Handler) registerContentPromptRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/content-items/prompts", Handler: h.ListContentTemplates, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/prompts", Handler: h.CreateContentTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-items/prompts/{id}", Handler: h.GetContentTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/content-items/prompts/{id}", Handler: h.UpdateContentTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/content-items/prompts/{id}", Handler: h.DeleteContentTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/prompts/{id}/actions/clone", Handler: h.CloneContentTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/prompts/{id}/actions/test", Handler: h.TestContentTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-items/prompts/{id}/stats", Handler: h.GetContentTemplateStats, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/prompts/actions/initialize-defaults", Handler: h.InitializeDefaultTemplates, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, deps)
}
