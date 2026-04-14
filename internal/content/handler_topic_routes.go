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
}
