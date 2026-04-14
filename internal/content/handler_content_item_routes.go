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

	// Legacy proxy routes (Phase 2 compatibility)
	mux.Handle("GET /api/v1/content/contents", withContentItemDeprecation(authMw(http.HandlerFunc(h.ListContents))))
	mux.Handle("GET /api/v1/content/contents/", withContentItemDeprecation(authMw(http.HandlerFunc(h.ListContents))))
	mux.Handle("GET /api/v1/content/contents/{content_id}", withContentItemDeprecation(authMw(http.HandlerFunc(h.GetContent))))
	mux.Handle("POST /api/v1/content/contents/generate", withContentItemDeprecation(authMw(http.HandlerFunc(h.GenerateContent))))
	mux.Handle("POST /api/v1/content/contents", withContentItemDeprecation(authMw(http.HandlerFunc(h.CreateContent))))
	mux.Handle("PUT /api/v1/content/contents/{content_id}", withContentItemDeprecation(authMw(http.HandlerFunc(h.UpdateContent))))
	mux.Handle("DELETE /api/v1/content/contents/{content_id}", withContentItemDeprecation(authMw(http.HandlerFunc(h.DeleteContent))))
	mux.Handle("POST /api/v1/content/contents/{content_id}/generate-images", withContentItemDeprecation(authMw(http.HandlerFunc(h.GenerateImages))))
	mux.Handle("POST /api/v1/content/contents/{content_id}/generate-single-image", withContentItemDeprecation(authMw(http.HandlerFunc(h.GenerateSingleImage))))
	mux.Handle("POST /api/v1/content/contents/{content_id}/save-composed-images", withContentItemDeprecation(authMw(http.HandlerFunc(h.SaveComposedImages))))
	mux.Handle("POST /api/v1/content/contents/{content_id}/publish", withContentItemDeprecation(authMw(http.HandlerFunc(h.PublishContent))))
	mux.Handle("POST /api/v1/content/contents/{content_id}/unpublish", withContentItemDeprecation(authMw(http.HandlerFunc(h.UnpublishContent))))

	mux.Handle("POST /api/v1/content/geo/analyze", withContentItemDeprecation(authMw(http.HandlerFunc(h.AnalyzeGEO))))
	mux.Handle("POST /api/v1/content/geo/analyze-by-id/{content_id}", withContentItemDeprecation(authMw(http.HandlerFunc(h.AnalyzeGEOByID))))
	mux.Handle("POST /api/v1/content/geo/optimize", withContentItemDeprecation(authMw(http.HandlerFunc(h.OptimizeGEO))))
	mux.Handle("GET /api/v1/content/geo/prompt-injection", withContentItemDeprecation(authMw(http.HandlerFunc(h.GetGEOPromptInjection))))
}

func withContentItemDeprecation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Sunset", "Tue, 30 Jun 2026 23:59:59 GMT")
		w.Header().Set("Link", `</api/v1/content-items>; rel="successor-version"`)
		next.ServeHTTP(w, r)
	})
}
