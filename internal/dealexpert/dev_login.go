package dealexpert

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// RegisterDevLoginRoute 仅在开发环境注册：一键登录为租户 4 的咨询师账号，
// 返回与真实登录相同结构的 JWT，供前端本地验收。生产环境不注册该路由。
func (h *Handler) RegisterDevLoginRoute(mux *http.ServeMux, jwtSecret string) {
	if os.Getenv("LINGCE_ENV") == "production" {
		return
	}
	mux.HandleFunc("POST /api/v1/deal-expert/dev-session", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var (
			id             int64
			name           string
			phone          string
			role           string
			tenantID       int64
			sessionVersion int
			roleName       string
		)
		err := h.pool.QueryRow(ctx, `
			SELECT e.id, COALESCE(NULLIF(e.full_name,''), e.name, ''), COALESCE(e.phone,''), COALESCE(e.role,''),
			       e.tenant_id, COALESCE(e.session_version,1)
			FROM employees e
			WHERE e.tenant_id = 4 AND e.is_active = 1 AND e.deleted_at IS NULL
			ORDER BY CASE WHEN e.role = 'admin' THEN 0 WHEN e.role = 'operating_manager' THEN 1 ELSE 2 END, e.id
			LIMIT 1
		`).Scan(&id, &name, &phone, &role, &tenantID, &sessionVersion)
		if err != nil {
			httputil.WriteError(w, http.StatusServiceUnavailable, "DEV_LOGIN_CANDIDATE_MISSING", "no employee for dev login", nil)
			return
		}
		switch role {
		case "admin":
			roleName = "院长"
		case "operating_manager":
			roleName = "运营负责人"
		case "consultant":
			roleName = "咨询师"
		default:
			roleName = role
		}
		tid := tenantID
		token, _, err := auth.GenerateToken(jwtSecret, id, auth.UserTypeEmployee, &tid, sessionVersion, 24)
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "DEV_LOGIN_TOKEN_FAILED", err.Error(), nil)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": token,
			"token_type":   "bearer",
			"user": map[string]any{
				"id": id, "name": name, "phone": phone, "role": role,
				"role_name": roleName, "tenant_id": tenantID, "tenant_name": "北京茗视光眼科",
			},
		})
	})
}
