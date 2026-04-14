package content

import "net/http"

func (h *Handler) registerTopicRoutes(mux *http.ServeMux, authMw func(http.Handler) http.Handler) {
	// Resource-oriented content-topic routes
	mux.Handle("GET /api/v1/content-topics", authMw(http.HandlerFunc(h.ListTopics)))
	mux.Handle("POST /api/v1/content-topics", authMw(http.HandlerFunc(h.CreateTopic)))
	mux.Handle("GET /api/v1/content-topics/{id}", authMw(http.HandlerFunc(h.GetTopic)))
	mux.Handle("PUT /api/v1/content-topics/{id}", authMw(http.HandlerFunc(h.UpdateTopic)))
	mux.Handle("DELETE /api/v1/content-topics/{id}", authMw(http.HandlerFunc(h.DeleteTopic)))

	mux.Handle("POST /api/v1/content-topics/actions/generate", authMw(http.HandlerFunc(h.GenerateTopics)))
	mux.Handle("POST /api/v1/content-topics/{id}/actions/select", authMw(http.HandlerFunc(h.SelectTopic)))

	mux.Handle("GET /api/v1/content-topics/insights/stats", authMw(http.HandlerFunc(h.GetInsightsStats)))
	mux.Handle("GET /api/v1/content-topics/insights/frequent-questions", authMw(http.HandlerFunc(h.GetFrequentQuestions)))
	mux.Handle("POST /api/v1/content-topics/insights/actions/mine-topics", authMw(http.HandlerFunc(h.MineTopics)))

	// Legacy proxy routes (Phase 2 compatibility)
	mux.Handle("GET /api/v1/content/topics", withTopicDeprecation(authMw(http.HandlerFunc(h.ListTopics))))
	mux.Handle("GET /api/v1/content/topics/", withTopicDeprecation(authMw(http.HandlerFunc(h.ListTopics))))
	mux.Handle("GET /api/v1/content/topics/{topic_id}", withTopicDeprecation(authMw(http.HandlerFunc(h.GetTopic))))
	mux.Handle("POST /api/v1/content/topics/generate", withTopicDeprecation(authMw(http.HandlerFunc(h.GenerateTopics))))
	mux.Handle("POST /api/v1/content/topics", withTopicDeprecation(authMw(http.HandlerFunc(h.CreateTopic))))
	mux.Handle("PUT /api/v1/content/topics/{topic_id}", withTopicDeprecation(authMw(http.HandlerFunc(h.UpdateTopic))))
	mux.Handle("PUT /api/v1/content/topics/{topic_id}/select", withTopicDeprecation(authMw(http.HandlerFunc(h.SelectTopic))))
	mux.Handle("DELETE /api/v1/content/topics/{topic_id}", withTopicDeprecation(authMw(http.HandlerFunc(h.DeleteTopic))))

	mux.Handle("GET /api/v1/content/conversation-insights/stats", withTopicDeprecation(authMw(http.HandlerFunc(h.GetInsightsStats))))
	mux.Handle("GET /api/v1/content/conversation-insights/frequent-questions", withTopicDeprecation(authMw(http.HandlerFunc(h.GetFrequentQuestions))))
	mux.Handle("POST /api/v1/content/conversation-insights/mine-topics", withTopicDeprecation(authMw(http.HandlerFunc(h.MineTopics))))
	mux.Handle("POST /api/v1/content/conversation-insights/save-topics", withTopicDeprecation(authMw(http.HandlerFunc(h.SaveMinedTopics))))
}

func withTopicDeprecation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Sunset", "Tue, 30 Jun 2026 23:59:59 GMT")
		w.Header().Set("Link", `</api/v1/content-topics>; rel="successor-version"`)
		next.ServeHTTP(w, r)
	})
}
