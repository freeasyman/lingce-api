package content

import (
	"net/http/httptest"
	"testing"
)

func TestCampaignLandingURL(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest("GET", "https://console.khgl.xyz/api/v1/operation/free-checkup/campaigns", nil)

	t.Run("环境变量优先", func(t *testing.T) {
		t.Setenv("FREE_CHECKUP_BASE_URL", "https://h5.example.com/")
		if got := h.campaignLandingURL(req, "abc"); got != "https://h5.example.com/?t=abc" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("缺失时回退正式域名而不是请求 Host", func(t *testing.T) {
		t.Setenv("FREE_CHECKUP_BASE_URL", "")
		got := h.campaignLandingURL(req, "abc")
		if got != "https://lc.khgl.xyz/?t=abc" {
			t.Fatalf("got %q", got)
		}
		if got == "https://console.khgl.xyz/?t=abc" {
			t.Fatalf("不允许回退到请求 Host")
		}
	})
}
