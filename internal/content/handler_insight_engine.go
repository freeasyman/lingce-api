package content

import (
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ExtractInsights 从录音转写提取 10 类洞察并落库。
func (h *Handler) ExtractInsights(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	recordingID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	insights, err := h.service.ExtractInsights(r.Context(), tenantID, recordingID)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, insights)
}

func (h *Handler) registerInsightEngineRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	routes := []router.Route{
		{Method: "POST", Path: "/api/v1/content-insights/actions/extract/{id}", Handler: h.ExtractInsights, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-insights/actions/extract-batch", Handler: h.ExtractInsightsBatch, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, deps)
}
