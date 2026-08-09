package wecom

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PartnerHandler struct {
	service *PartnerService
}

func NewPartnerHandler(service *PartnerService) *PartnerHandler {
	return &PartnerHandler{service: service}
}

func (h *PartnerHandler) RegisterRoutes(mux *http.ServeMux, jwtSecret string, pool *pgxpool.Pool, internalToken string) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/wecom/partner/callback", Handler: h.VerifyURL},
		{Method: "POST", Path: "/api/v1/wecom/partner/callback", Handler: h.Callback},
		{Method: "GET", Path: "/api/v1/wecom/partner/install-url", Handler: h.InstallURL},
		{Method: "GET", Path: "/api/v1/wecom/partner/install", Handler: h.InstallRedirect},
		{Method: "GET", Path: "/api/v1/wecom/partner/install/callback", Handler: h.InstallCallback},
		{Method: "POST", Path: "/api/v1/wecom/partner/oauth/login", Handler: h.OAuthLogin},
		{
			Method:           "POST",
			Path:             "/api/v1/wecom/partner/bind",
			Handler:          h.Bind,
			Auth:             true,
			AllowedUserTypes: []string{"employee", "mobile"},
		},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret, Pool: pool, InternalToken: strings.TrimSpace(internalToken)})
}

func (h *PartnerHandler) VerifyURL(w http.ResponseWriter, r *http.Request) {
	plain, err := h.service.VerifyURL(
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

func (h *PartnerHandler) Callback(w http.ResponseWriter, r *http.Request) {
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

func (h *PartnerHandler) InstallURL(w http.ResponseWriter, r *http.Request) {
	authType := -1
	if value := strings.TrimSpace(r.URL.Query().Get("auth_type")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			httputil.WriteBadRequest(w, "auth_type must be an integer")
			return
		}
		authType = parsed
	}
	resp, err := h.service.BuildInstallURL(r.Context(), r.URL.Query().Get("state"), authType)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.WriteSuccess(w, resp)
}

func (h *PartnerHandler) InstallRedirect(w http.ResponseWriter, r *http.Request) {
	authType := -1
	if value := strings.TrimSpace(r.URL.Query().Get("auth_type")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			httputil.WriteBadRequest(w, "auth_type must be an integer")
			return
		}
		authType = parsed
	}
	resp, err := h.service.BuildInstallURL(r.Context(), r.URL.Query().Get("state"), authType)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	http.Redirect(w, r, resp.InstallURL, http.StatusFound)
}

func (h *PartnerHandler) InstallCallback(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.HandleInstallCallback(r.Context(), r.URL.Query().Get("auth_code"))
	if err != nil {
		writePartnerInstallHTML(w, http.StatusBadRequest, "授权失败", err.Error(), "请返回企业微信后台重试，或联系灵策技术支持。")
		return
	}
	corpName := strings.TrimSpace(result.CorpName)
	if corpName == "" {
		corpName = result.CorpID
	}
	writePartnerInstallHTML(w, http.StatusOK, "授权成功", fmt.Sprintf("企业 %s 已完成授权。", corpName), "现在可以回到企业微信继续配置应用可见范围，随后再测试员工免登录。")
}

func (h *PartnerHandler) OAuthLogin(w http.ResponseWriter, r *http.Request) {
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

func (h *PartnerHandler) Bind(w http.ResponseWriter, r *http.Request) {
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

func writePartnerInstallHTML(w http.ResponseWriter, status int, title, message, hint string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte("<!doctype html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>" + html.EscapeString(title) + "</title><style>body{margin:0;font-family:-apple-system,BlinkMacSystemFont,Segoe UI,PingFang SC,Hiragino Sans GB,Microsoft YaHei,sans-serif;background:#f5f7fb;color:#1f2937}main{max-width:520px;margin:0 auto;padding:48px 20px}section{background:#fff;border-radius:20px;padding:28px 24px;box-shadow:0 16px 40px rgba(15,23,42,.08)}h1{margin:0 0 12px;font-size:28px}p{margin:0 0 10px;line-height:1.7}small{display:block;color:#6b7280;line-height:1.6}</style></head><body><main><section><h1>" + html.EscapeString(title) + "</h1><p>" + html.EscapeString(message) + "</p><small>" + html.EscapeString(hint) + "</small></section></main></body></html>"))
}
