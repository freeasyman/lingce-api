package delegated

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string, pool *pgxpool.Pool, internalToken string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/wecom/delegated-app/callback", Handler: h.VerifyCallbackURL},
		{Method: "POST", Path: "/api/v1/wecom/delegated-app/callback", Handler: h.Callback},
		{Method: "GET", Path: "/api/v1/wecom/partner-template/callback", Handler: h.VerifyCallbackURL},
		{Method: "POST", Path: "/api/v1/wecom/partner-template/callback", Handler: h.Callback},
		{Method: "GET", Path: "/api/v1/wecom/delegated-app/enterprise-callback", Handler: h.VerifyEnterpriseCallbackURL},
		{Method: "POST", Path: "/api/v1/wecom/delegated-app/enterprise-callback", Handler: h.EnterpriseCallback},
		{Method: "GET", Path: "/api/v1/wecom/delegated-app/launch", Handler: h.Launch},
		{Method: "POST", Path: "/api/v1/wecom/delegated-app/oauth/login", Handler: h.OAuthLogin},
		{Method: "POST", Path: "/api/v1/wecom/delegated-app/bind", Handler: h.Bind, Auth: true, AllowedUserTypes: []string{"employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/wecom/internal/send", Handler: h.InternalSend, AuthMode: "internal"},
		{Method: "GET", Path: "/api/v1/ops/wecom/overview", Handler: h.GetOverview, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/ops/wecom/corp-installs", Handler: h.ListCorpInstalls, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/ops/wecom/corp-installs/{id}", Handler: h.GetCorpInstallDetail, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/ops/wecom/corp-installs/bind", Handler: h.BindCorpInstallTenant, Auth: true, AllowedUserTypes: []string{"admin"}},
	}, router.RouteDeps{JWTSecret: jwtSecret, Pool: pool, InternalToken: strings.TrimSpace(internalToken)})
}

func (h *Handler) VerifyCallbackURL(w http.ResponseWriter, r *http.Request) {
	echostr := rawQueryValue(r.URL.RawQuery, "echostr")
	plain, err := h.service.VerifyCallbackURL(
		r.URL.Query().Get("msg_signature"),
		r.URL.Query().Get("timestamp"),
		r.URL.Query().Get("nonce"),
		echostr,
	)
	if err != nil {
		slog.Warn("delegated app template callback verify failed",
			"path", r.URL.Path,
			"has_signature", strings.TrimSpace(r.URL.Query().Get("msg_signature")) != "",
			"has_timestamp", strings.TrimSpace(r.URL.Query().Get("timestamp")) != "",
			"has_nonce", strings.TrimSpace(r.URL.Query().Get("nonce")) != "",
			"echostr_len", len(echostr),
			"error", err,
		)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(plain))
}

func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		slog.Warn("delegated app template callback read body failed",
			"path", r.URL.Path,
			"error", err,
		)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid body"))
		return
	}
	if err := h.service.HandleCallback(
		r.Context(),
		r.URL.Query().Get("msg_signature"),
		r.URL.Query().Get("timestamp"),
		r.URL.Query().Get("nonce"),
		payload,
	); err != nil {
		slog.Warn("delegated app template callback failed",
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"body_len", len(payload),
			"body_preview", previewPayload(payload),
			"error", err,
		)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("success"))
}

func (h *Handler) VerifyEnterpriseCallbackURL(w http.ResponseWriter, r *http.Request) {
	echostr := rawQueryValue(r.URL.RawQuery, "echostr")
	plain, err := h.service.VerifyEnterpriseCallbackURL(
		r.URL.Query().Get("msg_signature"),
		r.URL.Query().Get("timestamp"),
		r.URL.Query().Get("nonce"),
		echostr,
	)
	if err != nil {
		slog.Warn("delegated app enterprise callback verify failed",
			"path", r.URL.Path,
			"has_signature", strings.TrimSpace(r.URL.Query().Get("msg_signature")) != "",
			"has_timestamp", strings.TrimSpace(r.URL.Query().Get("timestamp")) != "",
			"has_nonce", strings.TrimSpace(r.URL.Query().Get("nonce")) != "",
			"echostr_len", len(echostr),
			"error", err,
		)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(plain))
}

func (h *Handler) EnterpriseCallback(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		slog.Warn("delegated app enterprise callback read body failed",
			"path", r.URL.Path,
			"error", err,
		)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid body"))
		return
	}
	if err := h.service.HandleEnterpriseCallback(
		r.Context(),
		r.URL.Query().Get("msg_signature"),
		r.URL.Query().Get("timestamp"),
		r.URL.Query().Get("nonce"),
		payload,
	); err != nil {
		slog.Warn("delegated app enterprise callback failed",
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"body_len", len(payload),
			"body_preview", previewPayload(payload),
			"error", err,
		)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("success"))
}

func (h *Handler) Launch(w http.ResponseWriter, r *http.Request) {
	corpID := strings.TrimSpace(r.URL.Query().Get("corp_id"))
	if corpID == "" {
		corpID = strings.TrimSpace(r.URL.Query().Get("corpid"))
	}
	targetURL, err := h.service.ResolveLoginEntryURL(r.Context(), corpID)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	http.Redirect(w, r, targetURL, http.StatusFound)
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
		slog.Warn("delegated app oauth login failed",
			"path", r.URL.Path,
			"corp_id", strings.TrimSpace(req.CorpID),
			"error", err,
		)
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

func (h *Handler) GetOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.service.GetOverview(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, overview)
}

func (h *Handler) ListCorpInstalls(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	tenantID, err := tenancy.ResolveOptionalTenantID(claims, r.URL.Query().Get("tenant_id"), true)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	items, err := h.service.ListCorpInstalls(r.Context(), tenantID, r.URL.Query().Get("corp_id"))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) GetCorpInstallDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid corp install id")
		return
	}
	items, err := h.service.ListCorpInstalls(r.Context(), nil, "")
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	var target *CorpInstallResponse
	for _, item := range items {
		if item != nil && item.ID == strconv.FormatInt(id, 10) {
			target = item
			break
		}
	}
	if target == nil {
		httputil.WriteNotFound(w, "corp install not found")
		return
	}
	detail, err := h.service.GetCorpInstallDetail(r.Context(), target.ProviderApp, target.CorpID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, detail)
}

func (h *Handler) BindCorpInstallTenant(w http.ResponseWriter, r *http.Request) {
	var req CorpInstallBindRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.BindCorpInstallTenant(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func rawQueryValue(rawQuery, key string) string {
	if strings.TrimSpace(rawQuery) == "" || strings.TrimSpace(key) == "" {
		return ""
	}
	prefix := key + "="
	for _, part := range strings.Split(rawQuery, "&") {
		if !strings.HasPrefix(part, prefix) {
			continue
		}
		value := strings.TrimPrefix(part, prefix)
		decoded, err := url.QueryUnescape(strings.ReplaceAll(value, "+", "%2B"))
		if err != nil {
			return value
		}
		return decoded
	}
	return ""
}

func previewPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	const maxPreview = 512
	if len(payload) <= maxPreview {
		return string(payload)
	}
	return string(payload[:maxPreview]) + "...(truncated)"
}
