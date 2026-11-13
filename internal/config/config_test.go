package config

import "testing"

func TestDealAnalysisVersionDefaultsToV200(t *testing.T) {
	cases := map[string]string{
		"":          "v200",
		"  ":        "v200",
		"v200":      "v200",
		"v210-rc11": "v210-rc11",
	}
	for configured, want := range cases {
		got := DealConfig{AnalysisVersion: configured}.DealAnalysisVersion()
		if got != want {
			t.Fatalf("DealAnalysisVersion(%q) = %q, want %q", configured, got, want)
		}
	}
}
