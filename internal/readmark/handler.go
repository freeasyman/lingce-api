package readmark

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handler 提供通用的「标记已读」接口。
type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

// RegisterRoutes 注册已读接口。
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	routes := []router.Route{
		{Method: "POST", Path: "/api/v1/read-marks", Handler: h.MarkRead, Auth: true, AllowedUserTypes: []string{"employee", "mobile"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

// MarkRead 标记一组资源为已读；只影响当前用户自己的角标。
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "tenant required")
		return
	}
	var req struct {
		ResourceType string  `json:"resource_type"`
		ResourceIDs  []int64 `json:"resource_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	resourceType := strings.TrimSpace(req.ResourceType)
	if !ValidType(resourceType) {
		httputil.WriteBadRequest(w, "invalid resource_type")
		return
	}
	if len(req.ResourceIDs) > 200 {
		httputil.WriteBadRequest(w, "too many resource_ids")
		return
	}
	// mobile 令牌的 UserID 可能不是员工编号，这里只接受员工令牌。
	if claims.UserType != auth.UserTypeEmployee {
		httputil.WriteForbidden(w, "employee token required")
		return
	}
	if err := MarkRead(r.Context(), h.pool, *claims.TenantID, claims.UserID, resourceType, req.ResourceIDs); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"marked": len(req.ResourceIDs)})
}
