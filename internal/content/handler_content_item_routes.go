package content

import (
	"net/http"

	"github.com/freeasyman/lingce-api/internal/router"
)

func (h *Handler) registerContentItemRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/content-items", Handler: h.ListContents, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items", Handler: h.CreateContent, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-items/{id}", Handler: h.GetContent, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/content-items/{id}", Handler: h.UpdateContent, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/content-items/{id}", Handler: h.DeleteContent, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/actions/generate", Handler: h.GenerateContent, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/{id}/actions/publish", Handler: h.PublishContent, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/{id}/actions/unpublish", Handler: h.UnpublishContent, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/{id}/actions/generate-images", Handler: h.GenerateImages, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/{id}/actions/generate-single-image", Handler: h.GenerateSingleImage, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/{id}/actions/save-composed-images", Handler: h.SaveComposedImages, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/{id}/geo/analyze", Handler: h.AnalyzeGEOByID, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/geo/analyze", Handler: h.AnalyzeGEO, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/geo/optimize", Handler: h.OptimizeGEO, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-items/geo/prompt-injection", Handler: h.GetGEOPromptInjection, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, deps)
}
