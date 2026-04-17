package badge

import "testing"

func TestNormalizeHealthStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "healthy", in: "healthy", want: HealthStatusHealthy},
		{name: "normal legacy", in: "normal", want: HealthStatusHealthy},
		{name: "warning", in: "warning", want: HealthStatusWarning},
		{name: "error", in: "error", want: HealthStatusError},
		{name: "unknown", in: "unknown", want: HealthStatusUnknown},
		{name: "invalid", in: "bad", want: HealthStatusUnknown},
		{name: "empty", in: "", want: HealthStatusUnknown},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeHealthStatus(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeHealthStatus(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseHealthStatusFilter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{name: "healthy", in: "healthy", want: HealthStatusHealthy, wantOK: true},
		{name: "normal legacy", in: "normal", want: HealthStatusHealthy, wantOK: true},
		{name: "warning", in: "warning", want: HealthStatusWarning, wantOK: true},
		{name: "error", in: "error", want: HealthStatusError, wantOK: true},
		{name: "unknown", in: "unknown", want: HealthStatusUnknown, wantOK: true},
		{name: "invalid", in: "invalid", want: "", wantOK: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseHealthStatusFilter(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("parseHealthStatusFilter(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("parseHealthStatusFilter(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
