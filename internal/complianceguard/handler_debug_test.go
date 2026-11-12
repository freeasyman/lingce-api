package complianceguard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsProductionEnv(t *testing.T) {
	cases := map[string]bool{
		"production":  true,
		"prod":        true, // configs/prod.toml 的写法
		" PROD ":      true,
		"prd":         true,
		"dev":         false,
		"development": false,
		"test":        false,
		"staging":     false,
		"":            false,
	}
	for env, want := range cases {
		if got := isProductionEnv(env); got != want {
			t.Fatalf("isProductionEnv(%q) = %v, want %v", env, got, want)
		}
	}
}

func registerComplianceTestRoutes(env string) *http.ServeMux {
	module := NewModule(nil, nil)
	module.SetAppEnv(env)
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, "compliance-test-secret", "compliance-test-internal-token")
	return mux
}

// 生产环境（app.env=production/prod）不注册调试分析接口：路由不存在，返回 404。
func TestDebugAnalyzeTextRouteDisabledInProduction(t *testing.T) {
	for _, env := range []string{"production", "prod", " PROD "} {
		t.Run(env, func(t *testing.T) {
			mux := registerComplianceTestRoutes(env)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/debug/analyze-text", strings.NewReader(`{}`))
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (route must not be registered); body=%s",
					recorder.Code, recorder.Body.String())
			}
		})
	}
}

// 生产环境只关闭调试路由，其余合规接口照常注册（未带 token 返回 401 而不是 404）。
func TestOtherComplianceRoutesRemainRegisteredInProduction(t *testing.T) {
	mux := registerComplianceTestRoutes("production")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/findings", nil)
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, request)

	if recorder.Code == http.StatusNotFound {
		t.Fatalf("findings route must stay registered in production; body=%s", recorder.Body.String())
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for missing token", recorder.Code)
	}
}

// 开发/测试环境保留调试接口：路由已注册（无 token 返回 401，而不是 404）。
func TestDebugAnalyzeTextRouteAvailableOutsideProduction(t *testing.T) {
	for _, env := range []string{"dev", "development", "test", ""} {
		t.Run(env, func(t *testing.T) {
			mux := registerComplianceTestRoutes(env)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/debug/analyze-text", strings.NewReader(`{}`))
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code == http.StatusNotFound {
				t.Fatalf("debug route must be registered when app.env=%q", env)
			}
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 for missing token", recorder.Code)
			}
		})
	}
}

// 双保险：即使生产环境有别的路径直接调用 Handler，也返回 403 并说明仅开发环境可用。
func TestAnalyzeTextHandlerRejectsInProduction(t *testing.T) {
	handler := NewHandler(nil)
	handler.SetAppEnv("prod")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/debug/analyze-text", strings.NewReader(`{}`))
	recorder := httptest.NewRecorder()

	handler.AnalyzeText(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "仅开发环境可用") {
		t.Fatalf("body should explain dev-only: %s", recorder.Body.String())
	}
}
