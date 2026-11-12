package his

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExamRequestNoFormat(t *testing.T) {
	at := time.Date(2026, 11, 13, 9, 5, 0, 0, time.Local)
	cases := []struct {
		seq  int64
		want string
	}{
		{1, "E202611130001"},
		{42, "E202611130042"},
		{9999, "E202611139999"},
	}
	for _, tc := range cases {
		if got := examRequestNoFormat(tc.seq, at); got != tc.want {
			t.Fatalf("seq %d = %q, want %q", tc.seq, got, tc.want)
		}
	}
}

func TestNormalizeExamStatusFilter(t *testing.T) {
	for _, status := range []string{"", "  ", "pending", "in_progress", "reported", "cancelled"} {
		if _, err := normalizeExamStatusFilter(status); err != nil {
			t.Fatalf("status %q should be valid: %v", status, err)
		}
	}
	got, err := normalizeExamStatusFilter("in_progress")
	if err != nil || len(got) != 1 || got[0] != "in_progress" {
		t.Fatalf("in_progress filter = %v, %v", got, err)
	}
	if all, err := normalizeExamStatusFilter(""); err != nil || all != nil {
		t.Fatalf("empty filter = %v, %v; want nil,nil (全部)", all, err)
	}
	for _, status := range []string{"sampled", "executed", "IN_PROGRESS", "in-progress"} {
		if _, err := normalizeExamStatusFilter(status); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("status %q err = %v, want ErrInvalidRequest", status, err)
		}
	}
}

func TestNormalizeExamDate(t *testing.T) {
	for _, input := range []string{"", "  ", "2026-11-13"} {
		if _, err := normalizeExamDate(input); err != nil {
			t.Fatalf("date %q should be valid: %v", input, err)
		}
	}
	for _, input := range []string{"2026-13-01", "2026/11/13", "20261113", "today"} {
		if _, err := normalizeExamDate(input); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("date %q err = %v, want ErrInvalidRequest", input, err)
		}
	}
}

func TestNormalizeExamKeyword(t *testing.T) {
	got, err := normalizeExamKeyword("  胸部 DR  ")
	if err != nil || got != "胸部 DR" {
		t.Fatalf("trim keyword = %q, %v", got, err)
	}
	if _, err := normalizeExamKeyword(strings.Repeat("查", 64)); err != nil {
		t.Fatalf("64 runes should pass: %v", err)
	}
	if _, err := normalizeExamKeyword(strings.Repeat("查", 65)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("65 runes err = %v, want ErrInvalidRequest", err)
	}
}

func TestNormalizeCreateExamRequest(t *testing.T) {
	valid := CreateExamRequestRequest{
		EncounterID: 90776,
		ItemName:    "  胸部 DR ",
		BodyPart:    " 胸部 ",
		Purpose:     "  咳嗽查因 ",
	}
	got, err := normalizeCreateExamRequest(valid)
	if err != nil {
		t.Fatalf("valid request: %v", err)
	}
	if got.ItemName != "胸部 DR" || got.BodyPart != "胸部" || got.Purpose != "咳嗽查因" {
		t.Fatalf("trim result = %+v", got)
	}

	invalid := []CreateExamRequestRequest{
		{ItemName: "胸部 DR"},                                                    // 缺 encounter_id
		{EncounterID: 1, ItemName: "   "},                                      // 项目名空白
		{EncounterID: 1, ItemName: strings.Repeat("项", 65)},                    // 项目名超长
		{EncounterID: 1, ItemName: "胸部 DR", BodyPart: strings.Repeat("部", 65)}, // 部位超长
		{EncounterID: 1, ItemName: "胸部 DR", Purpose: strings.Repeat("目", 201)}, // 目的超长
	}
	for _, req := range invalid {
		if _, err := normalizeCreateExamRequest(req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid request %+v err = %v, want ErrInvalidRequest", req, err)
		}
	}
}

func TestNormalizeExamImageRefs(t *testing.T) {
	got, err := normalizeExamImageRefs([]string{" https://pacs.local/a.dcm ", "", "  ", "https://pacs.local/b.dcm"})
	if err != nil {
		t.Fatalf("valid refs: %v", err)
	}
	if len(got) != 2 || got[0] != "https://pacs.local/a.dcm" || got[1] != "https://pacs.local/b.dcm" {
		t.Fatalf("normalized refs = %+v", got)
	}

	if empty, err := normalizeExamImageRefs(nil); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("nil refs = %v, %v; want empty non-nil", empty, err)
	}

	exact := make([]string, maxExamImageRefs)
	for i := range exact {
		exact[i] = "https://pacs.local/" + strings.Repeat("a", 100)
	}
	if _, err := normalizeExamImageRefs(exact); err != nil {
		t.Fatalf("%d refs should pass: %v", len(exact), err)
	}
	tooMany := append(exact, "https://pacs.local/extra")
	if _, err := normalizeExamImageRefs(tooMany); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("%d refs err = %v, want ErrInvalidRequest", len(tooMany), err)
	}

	if _, err := normalizeExamImageRefs([]string{strings.Repeat("u", 500)}); err != nil {
		t.Fatalf("500-char URL should pass: %v", err)
	}
	if _, err := normalizeExamImageRefs([]string{strings.Repeat("u", 501)}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("501-char URL err = %v, want ErrInvalidRequest", err)
	}
}

func TestNormalizeReportExamRequest(t *testing.T) {
	valid := ReportExamRequestRequest{
		Findings:   " 双肺纹理增多 ",
		Impression: " 支气管炎可能 ",
		Suggestion: " 结合临床，必要时复查 ",
		ImageRefs:  []string{" https://pacs.local/ct/1.dcm ", ""},
	}
	got, err := normalizeReportExamRequest(valid)
	if err != nil {
		t.Fatalf("valid report: %v", err)
	}
	if got.Findings != "双肺纹理增多" || got.Impression != "支气管炎可能" || got.Suggestion != "结合临床，必要时复查" {
		t.Fatalf("trim report = %+v", got)
	}
	if len(got.ImageRefs) != 1 || got.ImageRefs[0] != "https://pacs.local/ct/1.dcm" {
		t.Fatalf("image refs = %+v", got.ImageRefs)
	}

	if _, err := normalizeReportExamRequest(ReportExamRequestRequest{
		Findings: "未见异常", Impression: "   ",
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty impression err = %v, want ErrInvalidRequest", err)
	}

	tooLong := []ReportExamRequestRequest{
		{Impression: "支气管炎", Findings: strings.Repeat("见", maxExamFindingsRunes+1)},
		{Impression: strings.Repeat("印", maxExamImpressionRunes+1)},
		{Impression: "支气管炎", Suggestion: strings.Repeat("建", maxExamSuggestionRunes+1)},
		{Impression: "支气管炎", ImageRefs: []string{strings.Repeat("u", maxExamImageURLRunes+1)}},
	}
	for _, req := range tooLong {
		if _, err := normalizeReportExamRequest(req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("overlong report %+v err = %v, want ErrInvalidRequest", req, err)
		}
	}

	// 边界值应通过：findings 4000 / impression 2000 / suggestion 2000。
	if _, err := normalizeReportExamRequest(ReportExamRequestRequest{
		Findings:   strings.Repeat("见", maxExamFindingsRunes),
		Impression: strings.Repeat("印", maxExamImpressionRunes),
		Suggestion: strings.Repeat("建", maxExamSuggestionRunes),
	}); err != nil {
		t.Fatalf("boundary-length report should pass: %v", err)
	}
}

func TestExamTransitionAllowed(t *testing.T) {
	allowed := []struct{ status, action string }{
		{examStatusPending, examActionStart},
		{examStatusInProgress, examActionReport},
		{examStatusPending, examActionCancel},
	}
	for _, tc := range allowed {
		if err := examTransitionAllowed(tc.status, tc.action); err != nil {
			t.Fatalf("%s + %s should be allowed: %v", tc.status, tc.action, err)
		}
	}

	blocked := []struct{ status, action string }{
		{examStatusInProgress, examActionStart},  // 不能重复开始
		{examStatusReported, examActionStart},    // 已出报告
		{examStatusCancelled, examActionStart},   // 已取消
		{examStatusPending, examActionReport},    // 未开始不能录报告
		{examStatusReported, examActionReport},   // 不能重复出报告
		{examStatusCancelled, examActionReport},  // 已取消
		{examStatusInProgress, examActionCancel}, // 检查中不能取消
		{examStatusReported, examActionCancel},   // 已出报告不能取消
		{examStatusCancelled, examActionCancel},  // 已取消
	}
	for _, tc := range blocked {
		if err := examTransitionAllowed(tc.status, tc.action); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("%s + %s err = %v, want ErrInvalidState", tc.status, tc.action, err)
		}
	}

	if err := examTransitionAllowed(examStatusPending, "verify"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown action err = %v, want ErrInvalidRequest", err)
	}
}

func TestNextExamStatus(t *testing.T) {
	cases := []struct{ action, want string }{
		{examActionStart, examStatusInProgress},
		{examActionReport, examStatusReported},
		{examActionCancel, examStatusCancelled},
	}
	for _, tc := range cases {
		got, err := nextExamStatus(tc.action)
		if err != nil || got != tc.want {
			t.Fatalf("nextExamStatus(%q) = %q, %v; want %q", tc.action, got, err, tc.want)
		}
	}
	if _, err := nextExamStatus("verify"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown action err = %v, want ErrInvalidRequest", err)
	}
}
