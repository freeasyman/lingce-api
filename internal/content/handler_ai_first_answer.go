package content

import (
	"encoding/json"
	"net/http"

	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// AnalyzeAIAnswer 分析"患者问 AI"的回答。
func (h *Handler) AnalyzeAIAnswer(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	result, err := h.service.AnalyzeAIAnswer(r.Context(), tenantID, req.Question)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

func (h *Handler) registerAIRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	routes := []router.Route{
		{Method: "POST", Path: "/api/v1/content-ai/actions/analyze", Handler: h.AnalyzeAIAnswer, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-ai/actions/visibility", Handler: h.AnalyzeAIVisibility, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, deps)
}
