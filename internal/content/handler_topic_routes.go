package content

import (
	"net/http"

	"github.com/freeasyman/lingce-api/internal/router"
)

func (h *Handler) registerTopicRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/content-topics", Handler: h.ListTopics, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-topics", Handler: h.CreateTopic, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-topics/{id}", Handler: h.GetTopic, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/content-topics/{id}", Handler: h.UpdateTopic, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/content-topics/{id}", Handler: h.DeleteTopic, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-topics/actions/generate", Handler: h.GenerateTopics, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-topics/{id}/actions/select", Handler: h.SelectTopic, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-topics/insights/stats", Handler: h.GetInsightsStats, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-topics/insights/frequent-questions", Handler: h.GetFrequentQuestions, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-topics/insights/actions/mine-topics", Handler: h.MineTopics, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, deps)
}
