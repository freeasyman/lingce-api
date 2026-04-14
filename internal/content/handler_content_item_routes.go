package content

import "net/http"

func (h *Handler) registerContentItemRoutes(mux *http.ServeMux, authMw func(http.Handler) http.Handler) {
	// Resource-oriented content-item routes
	mux.Handle("GET /api/v1/content-items", authMw(http.HandlerFunc(h.ListContents)))
	mux.Handle("POST /api/v1/content-items", authMw(http.HandlerFunc(h.CreateContent)))
	mux.Handle("GET /api/v1/content-items/{id}", authMw(http.HandlerFunc(h.GetContent)))
	mux.Handle("PUT /api/v1/content-items/{id}", authMw(http.HandlerFunc(h.UpdateContent)))
	mux.Handle("DELETE /api/v1/content-items/{id}", authMw(http.HandlerFunc(h.DeleteContent)))

	mux.Handle("POST /api/v1/content-items/actions/generate", authMw(http.HandlerFunc(h.GenerateContent)))
	mux.Handle("POST /api/v1/content-items/{id}/actions/publish", authMw(http.HandlerFunc(h.PublishContent)))
	mux.Handle("POST /api/v1/content-items/{id}/actions/unpublish", authMw(http.HandlerFunc(h.UnpublishContent)))
	mux.Handle("POST /api/v1/content-items/{id}/actions/generate-images", authMw(http.HandlerFunc(h.GenerateImages)))
	mux.Handle("POST /api/v1/content-items/{id}/actions/generate-single-image", authMw(http.HandlerFunc(h.GenerateSingleImage)))
	mux.Handle("POST /api/v1/content-items/{id}/actions/save-composed-images", authMw(http.HandlerFunc(h.SaveComposedImages)))

	// GEO sub-resources
	mux.Handle("POST /api/v1/content-items/{id}/geo/analyze", authMw(http.HandlerFunc(h.AnalyzeGEOByID)))
	mux.Handle("POST /api/v1/content-items/geo/analyze", authMw(http.HandlerFunc(h.AnalyzeGEO)))
	mux.Handle("POST /api/v1/content-items/geo/optimize", authMw(http.HandlerFunc(h.OptimizeGEO)))
	mux.Handle("GET /api/v1/content-items/geo/prompt-injection", authMw(http.HandlerFunc(h.GetGEOPromptInjection)))
}
