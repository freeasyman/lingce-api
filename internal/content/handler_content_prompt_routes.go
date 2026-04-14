package content

import "net/http"

func (h *Handler) registerContentPromptRoutes(mux *http.ServeMux, authMw func(http.Handler) http.Handler) {
	// Resource-oriented content prompt template routes
	mux.Handle("GET /api/v1/content-items/prompts", authMw(http.HandlerFunc(h.ListContentTemplates)))
	mux.Handle("POST /api/v1/content-items/prompts", authMw(http.HandlerFunc(h.CreateContentTemplate)))
	mux.Handle("GET /api/v1/content-items/prompts/{id}", authMw(http.HandlerFunc(h.GetContentTemplate)))
	mux.Handle("PUT /api/v1/content-items/prompts/{id}", authMw(http.HandlerFunc(h.UpdateContentTemplate)))
	mux.Handle("DELETE /api/v1/content-items/prompts/{id}", authMw(http.HandlerFunc(h.DeleteContentTemplate)))
	mux.Handle("POST /api/v1/content-items/prompts/{id}/actions/clone", authMw(http.HandlerFunc(h.CloneContentTemplate)))
	mux.Handle("POST /api/v1/content-items/prompts/{id}/actions/test", authMw(http.HandlerFunc(h.TestContentTemplate)))
	mux.Handle("GET /api/v1/content-items/prompts/{id}/stats", authMw(http.HandlerFunc(h.GetContentTemplateStats)))
	mux.Handle("POST /api/v1/content-items/prompts/actions/initialize-defaults", authMw(http.HandlerFunc(h.InitializeDefaultTemplates)))

	// Legacy proxy routes
	mux.Handle("GET /api/v1/content-prompt-templates", withContentPromptDeprecation(authMw(http.HandlerFunc(h.ListContentTemplates))))
	mux.Handle("GET /api/v1/content-prompt-templates/", withContentPromptDeprecation(authMw(http.HandlerFunc(h.ListContentTemplates))))
	mux.Handle("POST /api/v1/content-prompt-templates", withContentPromptDeprecation(authMw(http.HandlerFunc(h.CreateContentTemplate))))
	mux.Handle("GET /api/v1/content-prompt-templates/{template_id}", withContentPromptDeprecation(authMw(http.HandlerFunc(h.GetContentTemplate))))
	mux.Handle("PUT /api/v1/content-prompt-templates/{template_id}", withContentPromptDeprecation(authMw(http.HandlerFunc(h.UpdateContentTemplate))))
	mux.Handle("DELETE /api/v1/content-prompt-templates/{template_id}", withContentPromptDeprecation(authMw(http.HandlerFunc(h.DeleteContentTemplate))))
	mux.Handle("POST /api/v1/content-prompt-templates/{template_id}/clone", withContentPromptDeprecation(authMw(http.HandlerFunc(h.CloneContentTemplate))))
	mux.Handle("POST /api/v1/content-prompt-templates/{template_id}/test", withContentPromptDeprecation(authMw(http.HandlerFunc(h.TestContentTemplate))))
	mux.Handle("GET /api/v1/content-prompt-templates/{template_id}/stats", withContentPromptDeprecation(authMw(http.HandlerFunc(h.GetContentTemplateStats))))
	mux.Handle("POST /api/v1/content-prompt-templates/initialize-defaults", withContentPromptDeprecation(authMw(http.HandlerFunc(h.InitializeDefaultTemplates))))
}

func withContentPromptDeprecation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Sunset", "Tue, 30 Jun 2026 23:59:59 GMT")
		w.Header().Set("Link", `</api/v1/content-items/prompts>; rel="successor-version"`)
		next.ServeHTTP(w, r)
	})
}
