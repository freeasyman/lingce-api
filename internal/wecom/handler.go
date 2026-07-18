package wecom

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	service       *Service
	internalToken string
}

func NewHandler(service *Service, internalToken string) *Handler {
	return &Handler{service: service, internalToken: strings.TrimSpace(internalToken)}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string, pool *pgxpool.Pool) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/wecom/callback", Handler: h.VerifyURL},
		{Method: "POST", Path: "/api/v1/wecom/callback", Handler: h.Callback},
		{Method: "POST", Path: "/api/v1/wecom/oauth/login", Handler: h.OAuthLogin},
		{Method: "POST", Path: "/api/v1/wecom/internal/send", Handler: h.InternalSend},
		{
			Method:           "POST",
			Path:             "/api/v1/wecom/bind",
			Handler:          h.Bind,
			Auth:             true,
			AllowedUserTypes: []string{"employee", "mobile"},
		},
		{Method: "GET", Path: "/api/v1/ops/wecom/apps", Handler: h.ListTenantApps, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/ops/wecom/apps/{id}", Handler: h.GetTenantApp, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/ops/wecom/apps", Handler: h.UpsertTenantApp, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "PUT", Path: "/api/v1/ops/wecom/apps/{id}", Handler: h.UpsertTenantApp, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "DELETE", Path: "/api/v1/ops/wecom/apps/{id}", Handler: h.DeleteTenantApp, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/ops/wecom/apps/{id}/test-token", Handler: h.TestTenantAppToken, Auth: true, AllowedUserTypes: []string{"admin"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret, Pool: pool})
}

func (h *Handler) isAdmin(r *http.Request) bool {
	claims := middleware.GetUserClaims(r.Context())
	return claims != nil && claims.UserType == "admin"
}

func (h *Handler) VerifyURL(w http.ResponseWriter, r *http.Request) {
	plain, err := h.service.VerifyURL(
		r.Context(),
		r.URL.Query().Get("msg_signature"),
		r.URL.Query().Get("timestamp"),
		r.URL.Query().Get("nonce"),
		r.URL.Query().Get("echostr"),
	)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(plain))
}

func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid body"))
		return
	}
	if err := h.service.HandleCallback(
		r.Context(),
		r.URL.Query().Get("msg_signature"),
		r.URL.Query().Get("timestamp"),
		r.URL.Query().Get("nonce"),
		body,
	); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("success"))
}

func (h *Handler) OAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req OAuthLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		httputil.WriteBadRequest(w, "code is required")
		return
	}
	resp, err := h.service.LoginWithOAuth(r.Context(), req.Code, req.CorpID)
	if err != nil {
		httputil.WriteError(w, http.StatusUnauthorized, "WECOM_LOGIN_FAILED", err.Error(), nil)
		return
	}
	if resp.Status == "needs_bind" {
		httputil.WriteError(w, http.StatusConflict, "WECOM_BIND_REQUIRED", "wecom user is not bound to an employee", resp)
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) Bind(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	var req BindRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if strings.TrimSpace(req.CorpID) == "" || strings.TrimSpace(req.WeComUserID) == "" {
		httputil.WriteBadRequest(w, "corp_id and wecom_user_id are required")
		return
	}
	if err := h.service.BindEmployee(r.Context(), req.CorpID, req.WeComUserID, claims.UserID); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"message": "binding created"})
}

func (h *Handler) InternalSend(w http.ResponseWriter, r *http.Request) {
	if h.internalToken != "" && strings.TrimSpace(r.Header.Get("X-Internal-Token")) != h.internalToken {
		httputil.WriteUnauthorized(w, "invalid internal token")
		return
	}
	var req InternalSendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	resp, err := h.service.SendInternalMessage(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) ListTenantApps(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	var tenantID *int64
	if raw := strings.TrimSpace(r.URL.Query().Get("tenant_id")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			httputil.WriteBadRequest(w, "tenant_id must be an integer")
			return
		}
		if parsed > 0 {
			tenantID = &parsed
		}
	}
	items, err := h.service.ListTenantApps(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) GetTenantApp(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid app ID")
		return
	}
	item, err := h.service.GetTenantApp(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) UpsertTenantApp(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	var req TenantWeComAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.UpsertTenantApp(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) DeleteTenantApp(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid app ID")
		return
	}
	if err := h.service.DeleteTenantApp(r.Context(), id); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "tenant wecom app deleted"})
}

func (h *Handler) TestTenantAppToken(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid app ID")
		return
	}
	item, err := h.service.TestTenantAppToken(r.Context(), id)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}
