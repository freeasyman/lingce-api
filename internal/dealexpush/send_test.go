package dealexpush

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/freeasyman/lingce-api/internal/config"
)

// fakeSender 是单测用的模板短信发送器：记录调用并返回预设结果。
type fakeSender struct {
	calls   int
	bizID   string
	sendErr error
}

func (f *fakeSender) SendTemplate(phone, signName, templateCode string, params map[string]string) (string, error) {
	f.calls++
	if f.sendErr != nil {
		return "", f.sendErr
	}
	return f.bizID, nil
}

func enabledDryRunConfig(enabled, dryRun bool, whitelist []string) config.DealPushConfig {
	return config.DealPushConfig{Enabled: enabled, DryRun: &dryRun, Whitelist: whitelist}
}

func TestWhitelisted(t *testing.T) {
	cases := []struct {
		name      string
		whitelist []string
		phone     string
		want      bool
	}{
		{"in whitelist", []string{"18610815948"}, "18610815948", true},
		{"not in whitelist", []string{"18610815948"}, "13800000000", false},
		{"empty whitelist", nil, "18610815948", false},
		{"empty phone", []string{"18610815948"}, "", false},
		{"trimmed match", []string{"18610815948"}, " 18610815948 ", true},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := whitelisted(item.whitelist, item.phone); got != item.want {
				t.Fatalf("whitelisted(%v, %q) = %v, want %v", item.whitelist, item.phone, got, item.want)
			}
		})
	}
}

func TestDecideSend(t *testing.T) {
	cases := []struct {
		name   string
		cfg    config.DealPushConfig
		status string
		phone  string
		want   bool
	}{
		{"whitelist in and enabled", enabledDryRunConfig(true, false, []string{"18610815948"}), "dry_run", "18610815948", true},
		{"whitelist out no send", enabledDryRunConfig(true, false, []string{"18610815948"}), "dry_run", "13800000000", false},
		{"dry_run true no send", enabledDryRunConfig(true, true, []string{"18610815948"}), "dry_run", "18610815948", false},
		{"enabled false no send", enabledDryRunConfig(false, false, []string{"18610815948"}), "dry_run", "18610815948", false},
		{"already sent no resend", enabledDryRunConfig(true, false, []string{"18610815948"}), "sent", "18610815948", false},
		{"failed can retry", enabledDryRunConfig(true, false, []string{"18610815948"}), "failed", "18610815948", true},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := decideSend(item.cfg, item.status, item.phone); got != item.want {
				t.Fatalf("decideSend() = %v, want %v", got, item.want)
			}
		})
	}
}

func TestSendOnceWithFakeProvider(t *testing.T) {
	t.Run("provider success returns sent and biz id", func(t *testing.T) {
		fake := &fakeSender{bizID: "biz-123"}
		service := &Service{cfg: enabledDryRunConfig(true, false, []string{"18610815948"}), sender: fake, log: testLogger()}
		status, bizID, err := service.sendOnce("18610815948", "天津灵策科技", "SMS_512628042", map[string]string{"name": "杜舒然", "code": "ABC", "link": "https://x/s/ABC"})
		if err != nil {
			t.Fatalf("sendOnce err: %v", err)
		}
		if status != "sent" || bizID != "biz-123" {
			t.Fatalf("sendOnce = (%s, %s), want (sent, biz-123)", status, bizID)
		}
		if fake.calls != 1 {
			t.Fatalf("provider calls = %d, want 1", fake.calls)
		}
	})

	t.Run("provider failure returns failed", func(t *testing.T) {
		fake := &fakeSender{sendErr: errors.New("SMS API error: isv.BUSINESS_LIMIT_CONTROL")}
		service := &Service{cfg: enabledDryRunConfig(true, false, []string{"18610815948"}), sender: fake, log: testLogger()}
		status, _, err := service.sendOnce("18610815948", "天津灵策科技", "SMS_512628042", map[string]string{})
		if err == nil {
			t.Fatal("expected error")
		}
		if status != "failed" {
			t.Fatalf("status = %s, want failed", status)
		}
		if fake.calls != 1 {
			t.Fatalf("provider calls = %d, want 1", fake.calls)
		}
	})

	t.Run("not whitelisted is rejected before provider", func(t *testing.T) {
		fake := &fakeSender{bizID: "biz-x"}
		service := &Service{cfg: enabledDryRunConfig(true, false, []string{"18610815948"}), sender: fake, log: testLogger()}
		status, _, err := service.sendOnce("13800000000", "天津灵策科技", "SMS_512628042", map[string]string{})
		if err == nil {
			t.Fatal("expected not-in-whitelist error")
		}
		if status != "failed" {
			t.Fatalf("status = %s, want failed", status)
		}
		if fake.calls != 0 {
			t.Fatalf("provider must not be called, calls = %d", fake.calls)
		}
	})

	t.Run("sender nil is rejected", func(t *testing.T) {
		service := &Service{cfg: enabledDryRunConfig(true, false, []string{"18610815948"}), log: testLogger()}
		status, _, err := service.sendOnce("18610815948", "天津灵策科技", "SMS_512628042", map[string]string{})
		if err == nil {
			t.Fatal("expected sender unavailable error")
		}
		if status != "failed" {
			t.Fatalf("status = %s, want failed", status)
		}
	})
}

func TestTemplateParams(t *testing.T) {
	params := templateParams([]byte(`{"name":"杜舒然","code":"ABC","link":"https://khgl.xyz/s/ABC","template":"SMS_512628042"}`))
	if params["name"] != "杜舒然" || params["code"] != "ABC" {
		t.Fatalf("unexpected params: %v", params)
	}
	if _, ok := params["template"]; ok {
		t.Fatal("template must not be forwarded as a param")
	}
	if _, ok := params["link"]; ok {
		t.Fatal("link must not be forwarded as a param")
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
