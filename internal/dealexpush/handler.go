package dealexpush

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/dealexpertpermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
	access  *dealexpertpermission.Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service, access: dealexpertpermission.NewService(service.pool)}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		// 短链解析：公开访问，无鉴权。
		{Method: "GET", Path: "/s/{code}", Handler: h.ResolveRedirect},
		// 推送事件列表：管理员只读，便于验收。
		{Method: "GET", Path: "/api/v1/deal-expert/push-events", Handler: h.ListEvents, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

// ResolveRedirect 解析短链并 302 到 H5 接待详情；不存在 404，过期 410。
func (h *Handler) ResolveRedirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	item, err := h.service.ResolveShortLink(r.Context(), code)
	if err != nil {
		switch {
		case errors.Is(err, ErrShortLinkExpired):
			httputil.WriteError(w, http.StatusGone, "SHORT_LINK_EXPIRED", "短链已过期", nil)
		case errors.Is(err, ErrShortLinkNotFound):
			httputil.WriteNotFound(w, "短链不存在或已失效")
		default:
			httputil.WriteInternalError(w, err.Error())
		}
		return
	}
	base := strings.TrimRight(strings.TrimSpace(h.service.cfg.H5BaseURL), "/")
	if base == "" {
		base = "https://employee.khgl.xyz"
	}
	target := base + "/h5/encounters/" + strconv.FormatInt(item.RecordingID, 10) + "?code=" + item.Code
	http.Redirect(w, r, target, http.StatusFound)
}

// ListEvents 管理员只读推送事件列表（供验收核对干跑结果）。
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	access, err := h.access.AccessFor(r.Context(), claims, tenantID)
	if err != nil || !access.CanManagePermissions() {
		httputil.WriteForbidden(w, "deal expert permission management required")
		return
	}
	recordingID, _ := strconv.ParseInt(r.URL.Query().Get("recording_id"), 10, 64)
	items, err := h.service.ListEvents(r.Context(), tenantID, recordingID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": len(items)})
}
