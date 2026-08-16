package wecom

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type LicenseHandler struct {
	service *LicenseService
}

type LicenseActivationRequest struct {
	CorpID     string `json:"corp_id"`
	UserID     string `json:"user_id"`
	ActiveCode string `json:"active_code,omitempty"`
}

func NewLicenseHandler(service *LicenseService) *LicenseHandler {
	return &LicenseHandler{service: service}
}

func (h *LicenseHandler) RegisterRoutes(mux *http.ServeMux, internalToken string) {
	router.Register(mux, []router.Route{
		{Method: "POST", Path: "/api/v1/wecom/license/activate", Handler: h.Activate, AuthMode: "internal"},
		{Method: "POST", Path: "/api/v1/wecom/license/status", Handler: h.Status, AuthMode: "internal"},
	}, router.RouteDeps{InternalToken: strings.TrimSpace(internalToken)})
}

func (h *LicenseHandler) Activate(w http.ResponseWriter, r *http.Request) {
	var req LicenseActivationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	result, err := h.service.Activate(r.Context(), req.CorpID, req.UserID, req.ActiveCode)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

func (h *LicenseHandler) Status(w http.ResponseWriter, r *http.Request) {
	var req LicenseActivationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	result, err := h.service.Status(r.Context(), req.CorpID, req.UserID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}
