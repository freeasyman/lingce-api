package badge

import (
	"net/http"
	"testing"
)

func TestRegisterRoutes_LegacyBadgeControlRoutesRemainAvailable(t *testing.T) {
	mux := http.NewServeMux()
	NewHandler(nil).RegisterRoutes(mux, "test-secret")

	tests := []struct {
		method  string
		path    string
		pattern string
	}{
		{method: http.MethodPost, path: "/api/v1/badge-control/assign/tenant", pattern: "POST /api/v1/badge-control/assign/tenant"},
		{method: http.MethodPost, path: "/api/v1/badge-control/assign/employee", pattern: "POST /api/v1/badge-control/assign/employee"},
		{method: http.MethodPost, path: "/api/v1/badge-control/reclaim/employee", pattern: "POST /api/v1/badge-control/reclaim/employee"},
		{method: http.MethodPost, path: "/api/v1/badge-control/reclaim/tenant", pattern: "POST /api/v1/badge-control/reclaim/tenant"},
	}

	for _, tc := range tests {
		req, err := http.NewRequest(tc.method, tc.path, nil)
		if err != nil {
			t.Fatalf("new request %s %s: %v", tc.method, tc.path, err)
		}
		_, pattern := mux.Handler(req)
		if pattern != tc.pattern {
			t.Fatalf("route %s %s matched pattern %q, want %q", tc.method, tc.path, pattern, tc.pattern)
		}
	}
}
