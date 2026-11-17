package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 会话校验依赖的数据库不可用时，必须返回可重试的 503，而不是 401。
// 401 会让前端清空登录态并把用户踢回登录页（真实故障：开发库连接数耗尽时
// 整页并发请求随机 401，用户只是打开机构档案就被登出）。
func TestAuthReturnsServiceUnavailableWhenSessionCheckFails(t *testing.T) {
	// 指向一个没有监听的端口，让会话校验的数据库查询必然失败。
	pool, err := pgxpool.New(context.Background(), "postgres://user:pass@127.0.0.1:1/lingce?sslmode=disable")
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()

	SetAuthValidationPool(pool)
	defer SetAuthValidationPool(nil)

	token, _, err := auth.GenerateToken("test-secret", 1, auth.UserTypeAdmin, nil, 1, 1)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	request.Header.Set("Authorization", "Bearer "+token)

	Auth("test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (body: %s)", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
	if body := recorder.Body.String(); !contains(body, "SESSION_CHECK_UNAVAILABLE") {
		t.Fatalf("body does not carry retryable code: %s", body)
	}
}

func TestAuthStillRejectsMissingToken(t *testing.T) {
	SetAuthValidationPool(nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)

	Auth("test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
