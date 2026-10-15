package followup

import (
	"strings"
	"testing"
)

func TestGeneratedTaskQuality(t *testing.T) {
	valid := `{"tasks":[{"title":"了解用药困难","category":"understanding","time_basis":"doctor_explicit","contact_time":"2026-10-09T10:00:00+08:00","purpose":"了解困难","evidence":"患者说不知道怎么使用","phone_script":"您好，请问使用时还有什么困难？","wechat_content":"您好，想了解您使用时是否还有困难。"}]}`
	cases := []struct {
		name, input string
		invalid     bool
	}{
		{"channel_only", valid, false},
		{"empty", `{"tasks":[]}`, false},
		{"sales", strings.Replace(valid, "understanding", "sales", 1), true},
		{"missing_evidence", strings.Replace(valid, "患者说不知道怎么使用", "", 1), true},
		{"missing_purpose", strings.Replace(valid, `"purpose":"了解困难"`, `"purpose":""`, 1), true},
		{"missing_time_basis", strings.Replace(valid, `"time_basis":"doctor_explicit",`, "", 1), true},
		{"invalid_time_basis", strings.Replace(valid, "doctor_explicit", "invented", 1), true},
		{"long_phone", strings.Replace(valid, "您好，请问使用时还有什么困难？", strings.Repeat("字", 121), 1), true},
		{"invalid_date", strings.Replace(valid, "2026-10-09T10:00:00+08:00", "尽快", 1), true},
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

func TestParseContactTimeRelativeNodes(t *testing.T) {
	for _, value := range []string{"今天", "明天", "后天", "三天后", "一周后", "7天后", "两周后", "14天后"} {
		if _, err := resolveContactTime(value, "2026-10-06T15:00:00+08:00"); err != nil {
			t.Fatalf("%s: %v", value, err)
		}
	}
	if _, err := parseContactTime("尽快"); err == nil {
		t.Fatal("模糊时间必须拒绝")
	}
}

func TestRelativeDateAnchoredToEncounter(t *testing.T) {
	got, err := resolveContactTime("三天后", "2026-10-06T15:00:00+08:00")
	if err != nil || got.Format("2006-01-02T15:04:05Z07:00") != "2026-10-09T10:00:00+08:00" {
		t.Fatalf("unexpected date %v: %v", got, err)
	}
	if _, err := resolveContactTime("明天", "invalid"); err == nil {
		t.Fatal("missing encounter time accepted")
	}
}

func TestHospitalDefaultContactTime(t *testing.T) {
	got, err := hospitalDefaultContactTime("2026-10-09T15:00:00+08:00")
	if err != nil || got.Format("2006-01-02 15:04") != "2026-10-12 10:00" {
		t.Fatalf("unexpected default: %v %v", got, err)
	}
}

func TestSubjectSynonyms(t *testing.T) {
	if followupSubjectKey("复查提醒") != followupSubjectKey("复诊提醒") {
		t.Fatal("synonyms must match")
	}
	if followupSubjectKey("用药困难") == followupSubjectKey("复诊提醒") {
		t.Fatal("different subjects merged")
	}
	if followupSubjectKey("术后第3天复查") == followupSubjectKey("术后第7天复查") {
		t.Fatal("different nodes merged")
	}
	// 当前重复控制是规则级主题归一化，不宣称医学语义相似度识别。
	if followupSubjectKey("术后一周恢复情况") == followupSubjectKey("手术后第七天康复状况") {
		t.Fatal("semantic variants must remain outside the rule-level guarantee")
	}
	if followupSubjectKey("  复查!提醒 ") != followupSubjectKey("复诊提醒") {
		t.Fatal("punctuation and whitespace normalization failed")
	}
}
