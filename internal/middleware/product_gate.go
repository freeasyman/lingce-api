package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/contract"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// 新产品各产品 API 的路径前缀 → 产品码。
// 产品权限（合同维度）只在这里定义一次；各产品不得再自行判断。
var productPathPrefixes = []struct{ Prefix, Code string }{
	{"/api/v1/emr/", "tingxie"},
	{"/api/v1/followup/", "suifang"},
	{"/api/v1/content", "huoke"}, // content / content-items / content-topics / content-ai …
	{"/api/v1/deal-expert/", "yingke"},
	{"/api/v1/patient-operations/", "kehu"},
	{"/api/v1/compliance/", "hegui"},
}

func productCodeForPath(path string) string {
	for _, item := range productPathPrefixes {
		if strings.HasPrefix(path, item.Prefix) {
			return item.Code
		}
	}
	return ""
}

// ProductGate 是新产品产品 API 的统一入口闸门：
//   - 仅对「门户入口」的 employee/mobile token 生效；
//   - 机构端/移动端（老产品）token、admin token 一律放行（老产品不动）；
//   - 通过后校验 contract.ProductActive：当前覆盖合同必须开通了该产品，否则 403。
func ProductGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := productCodeForPath(r.URL.Path)
		if code == "" {
			next.ServeHTTP(w, r)
			return
		}
		claims := GetUserClaims(r.Context())
		if claims == nil || (claims.UserType != auth.UserTypeEmployee && claims.UserType != auth.UserTypeMobile) {
			next.ServeHTTP(w, r)
			return
		}
		if claims.Entry != auth.EntryPortal {
			// 老产品入口 / 旧 token：不拦截，行为与改造前一致。
			next.ServeHTTP(w, r)
			return
		}
		if claims.TenantID == nil || *claims.TenantID <= 0 {
			httputil.WriteForbidden(w, "缺少租户信息")
			return
		}
		ok, err := contract.ProductActive(r.Context(), authValidationPool, *claims.TenantID, code, time.Now())
		if err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		if !ok {
			httputil.WriteError(w, http.StatusForbidden, "PRODUCT_NOT_OPENED", "该产品未开通，请联系顾问开通试用或正式", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
