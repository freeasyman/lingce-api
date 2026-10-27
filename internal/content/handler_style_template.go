package content

import (
	"encoding/json"
	"net/http"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// LearnStyleTemplate 学风格模板。
func (h *Handler) LearnStyleTemplate(w http.ResponseWriter, r *http.Request) {
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
	var req LearnStyleTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	result, err := h.service.LearnStyleTemplate(r.Context(), tenantID, req)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// ExtractArticleLayout 从公众号链接提取排版模板。
func (h *Handler) ExtractArticleLayout(w http.ResponseWriter, r *http.Request) {
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
	var req ExtractArticleLayoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	result, err := h.service.ExtractArticleLayout(r.Context(), tenantID, req)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// VerifyImageText OCR 错字校验。
func (h *Handler) VerifyImageText(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		ImageURL     string `json:"image_url"`
		ExpectedText string `json:"expected_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	result, err := h.service.VerifyImageText(r.Context(), tenantID, req.ImageURL, req.ExpectedText)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

func (h *Handler) registerStyleTemplateRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	routes := []router.Route{
		{Method: "POST", Path: "/api/v1/content-style-templates/actions/learn", Handler: h.LearnStyleTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-style-templates/actions/extract-layout", Handler: h.ExtractArticleLayout, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/{id}/actions/verify-text", Handler: h.VerifyImageText, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, deps)
}
