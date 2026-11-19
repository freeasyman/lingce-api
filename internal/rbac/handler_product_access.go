package rbac

import (
	"net/http"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/contract"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ProductAccess 判断当前登录者所属机构（仅门户入口）是否开通了指定产品。
// 唯一判断来源：contract.ProductActive（当前覆盖合同 + 生效额度行）。
// 各产品 App 的入口守卫统一调用本接口，产品码由调用方传入，不允许各自查表判断。
func (h *Handler) ProductAccess(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	claims := middleware.GetUserClaims(r.Context())
	open := false
	if code != "" && claims != nil && claims.TenantID != nil && *claims.TenantID > 0 &&
		claims.Entry == auth.EntryPortal &&
		(claims.UserType == auth.UserTypeEmployee || claims.UserType == auth.UserTypeMobile) {
		if ok, err := contract.ProductActive(r.Context(), h.service.store.pool, *claims.TenantID, code, time.Now()); err == nil {
			open = ok
		}
		// 查询出错按未开通处理（fail-closed），前端守卫展示未开通页。
	}
	httputil.WriteSuccess(w, map[string]any{"code": code, "open": open})
}
