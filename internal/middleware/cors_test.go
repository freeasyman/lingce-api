package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORSAllowsIdempotencyPreflight(t *testing.T) {
	called := false
	handler := CORS(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/compliance/check-items/test/process", nil)
	req.Header.Set("Origin", "http://localhost:3005")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type,authorization,idempotency-key")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent || called {
		t.Fatalf("preflight status=%d, downstream called=%v", res.Code, called)
	}
	if !strings.Contains(strings.ToLower(res.Header().Get("Access-Control-Allow-Headers")), "idempotency-key") {
		t.Fatal("idempotency header missing from preflight response")
	}
}
