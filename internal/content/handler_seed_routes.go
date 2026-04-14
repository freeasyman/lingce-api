package content

import "net/http"

func (h *Handler) registerContentSeedRoutes(mux *http.ServeMux, authMw func(http.Handler) http.Handler) {
	// Resource-oriented routes
	mux.Handle("GET /api/v1/content-seeds", authMw(http.HandlerFunc(h.ListSeeds)))
	mux.Handle("GET /api/v1/content-seeds/stats", authMw(http.HandlerFunc(h.GetSeedStats)))
	mux.Handle("GET /api/v1/content-seeds/clusters", authMw(http.HandlerFunc(h.GetClusters)))
	mux.Handle("GET /api/v1/content-seeds/{id}", authMw(http.HandlerFunc(h.GetSeed)))
	mux.Handle("POST /api/v1/content-seeds/{id}/actions/adopt", authMw(http.HandlerFunc(h.AdoptSeed)))
	mux.Handle("POST /api/v1/content-seeds/{id}/actions/dismiss", authMw(http.HandlerFunc(h.DismissSeed)))
	mux.Handle("GET /api/v1/content-seeds/honor-list", authMw(http.HandlerFunc(h.GetHonorList)))

	// Keep existing seed views as extended sub-resources
	mux.Handle("GET /api/v1/content-seeds/my-inspirations", authMw(http.HandlerFunc(h.GetMyInspirations)))
	mux.Handle("GET /api/v1/content-seeds/my-stats", authMw(http.HandlerFunc(h.GetMyStats)))
	mux.Handle("GET /api/v1/content-seeds/my-adopted", authMw(http.HandlerFunc(h.GetMyAdopted)))
}
