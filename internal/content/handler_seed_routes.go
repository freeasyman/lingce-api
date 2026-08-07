package content

import (
	"net/http"

	"github.com/freeasyman/lingce-api/internal/router"
)

func (h *Handler) registerContentSeedRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/content-seeds", Handler: h.ListSeeds, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-seeds/stats", Handler: h.GetSeedStats, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-seeds/clusters", Handler: h.GetClusters, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-seeds/{id}", Handler: h.GetSeed, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-seeds/{id}/actions/adopt", Handler: h.AdoptSeed, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-seeds/{id}/actions/dismiss", Handler: h.DismissSeed, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-seeds/honor-list", Handler: h.GetHonorList, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-seeds/my-inspirations", Handler: h.GetMyInspirations, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-seeds/my-stats", Handler: h.GetMyStats, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-seeds/my-adopted", Handler: h.GetMyAdopted, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, deps)
}
