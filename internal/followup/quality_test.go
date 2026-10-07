package followup

import (
	"strings"
	"testing"
)

func TestGeneratedTaskQuality(t *testing.T) {
	valid := `{"tasks":[{"title":"了解用药困难","category":"understanding","contact_time":"2026-10-09T10:00:00+08:00","purpose":"了解困难","evidence":"患者说不知道怎么使用","phone_script":"您好，请问使用时还有什么困难？","wechat_content":"您好，想了解您使用时是否还有困难。"}]}`
	cases := []struct {
		name, input string
		invalid     bool
	}{
		{"channel_only", valid, false},
		{"empty", `{"tasks":[]}`, false},
		{"sales", strings.Replace(valid, "understanding", "sales", 1), true},
		{"missing_evidence", strings.Replace(valid, "患者说不知道怎么使用", "", 1), true},
		{"missing_purpose", strings.Replace(valid, `"purpose":"了解困难"`, `"purpose":""`, 1), true},
		{"long_phone", strings.Replace(valid, "您好，请问使用时还有什么困难？", strings.Repeat("字", 121), 1), true},
		{"invalid_date", strings.Replace(valid, "2026-10-09T10:00:00+08:00", "三天后", 1), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := parseGeneratedTasks(c.input)
			if (err != nil) != c.invalid {
				t.Fatalf("unexpected error: %v", err)
			}
			if err == nil && len(p.Tasks) > 0 && p.Tasks[0].Script == "" {
				t.Fatal("phone compatibility script is empty")
			}
		})
	}
}
