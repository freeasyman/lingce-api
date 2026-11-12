package his

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLabRequestNoFormat(t *testing.T) {
	at := time.Date(2026, 11, 12, 10, 30, 0, 0, time.Local)
	cases := []struct {
		seq  int64
		want string
	}{
		{1, "L202611120001"},
		{42, "L202611120042"},
		{9999, "L202611129999"},
	}
	for _, tc := range cases {
		if got := labRequestNoFormat(tc.seq, at); got != tc.want {
			t.Fatalf("seq %d = %q, want %q", tc.seq, got, tc.want)
		}
	}
}

func TestNormalizeLabStatusFilter(t *testing.T) {
	for _, status := range []string{"", "  ", "pending", "sampled", "reported", "cancelled"} {
		if _, err := normalizeLabStatusFilter(status); err != nil {
			t.Fatalf("status %q should be valid: %v", status, err)
		}
	}
	got, err := normalizeLabStatusFilter("pending")
	if err != nil || len(got) != 1 || got[0] != "pending" {
		t.Fatalf("pending filter = %v, %v", got, err)
	}
	if all, err := normalizeLabStatusFilter(""); err != nil || all != nil {
		t.Fatalf("empty filter = %v, %v; want nil,nil (全部)", all, err)
	}
	for _, status := range []string{"ordered", "executed", "sampled_at", "PENDING"} {
		if _, err := normalizeLabStatusFilter(status); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("status %q err = %v, want ErrInvalidRequest", status, err)
		}
	}
}

func TestNormalizeLabDate(t *testing.T) {
	for _, input := range []string{"", "  ", "2026-11-12"} {
		if _, err := normalizeLabDate(input); err != nil {
			t.Fatalf("date %q should be valid: %v", input, err)
		}
	}
	for _, input := range []string{"2026-13-01", "2026/11/12", "20261112", "today"} {
		if _, err := normalizeLabDate(input); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("date %q err = %v, want ErrInvalidRequest", input, err)
		}
	}
}

func TestNormalizeLabKeyword(t *testing.T) {
	got, err := normalizeLabKeyword("  张访涵  ")
	if err != nil || got != "张访涵" {
		t.Fatalf("trim keyword = %q, %v", got, err)
	}
	if _, err := normalizeLabKeyword(strings.Repeat("检", 64)); err != nil {
		t.Fatalf("64 runes should pass: %v", err)
	}
	if _, err := normalizeLabKeyword(strings.Repeat("检", 65)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("65 runes err = %v, want ErrInvalidRequest", err)
	}
}

func TestNormalizeCreateLabRequest(t *testing.T) {
	valid := CreateLabRequestRequest{
		EncounterID:  90776,
		ItemName:     "  血常规（五分类） ",
		SpecimenType: " 血 ",
		Purpose:      "  发热查因 ",
	}
	got, err := normalizeCreateLabRequest(valid)
	if err != nil {
		t.Fatalf("valid request: %v", err)
	}
	if got.ItemName != "血常规（五分类）" || got.SpecimenType != "血" || got.Purpose != "发热查因" {
		t.Fatalf("trim result = %+v", got)
	}

	invalid := []CreateLabRequestRequest{
		{ItemName: "血常规", SpecimenType: "血"},                                     // 缺 encounter_id
		{EncounterID: 1, ItemName: "   ", SpecimenType: "血"},                     // 项目名空白
		{EncounterID: 1, ItemName: strings.Repeat("项", 65)},                      // 项目名超长
		{EncounterID: 1, ItemName: "血常规", SpecimenType: strings.Repeat("血", 33)}, // 标本超长
		{EncounterID: 1, ItemName: "血常规", Purpose: strings.Repeat("目", 201)},     // 目的超长
	}
	for _, req := range invalid {
		if _, err := normalizeCreateLabRequest(req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid request %+v err = %v, want ErrInvalidRequest", req, err)
		}
	}
}

func TestNormalizeLabFlag(t *testing.T) {
	cases := []struct{ input, want string }{
		{"", ""},
		{" H ", "H"},
		{"l", "L"},
		{"N", ""},
		{"n", ""},
	}
	for _, tc := range cases {
		got, err := normalizeLabFlag(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("flag %q = %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	for _, input := range []string{"X", "HH", "偏高", "1"} {
		if _, err := normalizeLabFlag(input); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("flag %q err = %v, want ErrInvalidRequest", input, err)
		}
	}
}

func TestNormalizeLabResultItems(t *testing.T) {
	if _, err := normalizeLabResultItems(nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty items err = %v, want ErrInvalidRequest", err)
	}
	tooMany := make([]LabResultItemInput, maxLabResultRows+1)
	for i := range tooMany {
		tooMany[i] = LabResultItemInput{ItemName: "WBC"}
	}
	if _, err := normalizeLabResultItems(tooMany); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("%d items err = %v, want ErrInvalidRequest", len(tooMany), err)
	}

	got, err := normalizeLabResultItems([]LabResultItemInput{
		{ItemCode: " WBC ", ItemName: " 白细胞 ", Value: " 6.2 ", Unit: " 10^9/L ", ReferenceRange: " 3.5-9.5 ", Flag: " h "},
		{ItemCode: "RBC", ItemName: "红细胞", Value: "4.1", Unit: "10^12/L", ReferenceRange: "3.8-5.1"},
	})
	if err != nil {
		t.Fatalf("valid items: %v", err)
	}
	if len(got) != 2 || got[0].ItemCode != "WBC" || got[0].Value != "6.2" || got[0].Flag != "H" || got[1].Flag != "" {
		t.Fatalf("normalized items = %+v", got)
	}

	invalid := [][]LabResultItemInput{
		{{ItemName: ""}}, // 项目名必填
		{{ItemName: "WBC", ItemCode: strings.Repeat("c", 33)}},       // 编码超长
		{{ItemName: "WBC", Value: strings.Repeat("9", 65)}},          // 数值超长
		{{ItemName: "WBC", Unit: strings.Repeat("u", 33)}},           // 单位超长
		{{ItemName: "WBC", ReferenceRange: strings.Repeat("r", 65)}}, // 参考范围超长
		{{ItemName: "WBC", Flag: "偏高"}},                              // 标志非法
	}
	for _, items := range invalid {
		if _, err := normalizeLabResultItems(items); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid items %+v err = %v, want ErrInvalidRequest", items, err)
		}
	}
}

func TestLabTransitionAllowed(t *testing.T) {
	allowed := []struct{ status, action string }{
		{labStatusPending, labActionSample},
		{labStatusSampled, labActionReport},
		{labStatusPending, labActionCancel},
	}
	for _, tc := range allowed {
		if err := labTransitionAllowed(tc.status, tc.action); err != nil {
			t.Fatalf("%s + %s should be allowed: %v", tc.status, tc.action, err)
		}
	}

	blocked := []struct{ status, action string }{
		{labStatusSampled, labActionSample},   // 不能重复采样
		{labStatusReported, labActionSample},  // 已出报告
		{labStatusCancelled, labActionSample}, // 已取消
		{labStatusPending, labActionReport},   // 未采样不能录结果
		{labStatusReported, labActionReport},  // 不能重复出报告
		{labStatusCancelled, labActionReport}, // 已取消
		{labStatusSampled, labActionCancel},   // 已采样不能取消
		{labStatusReported, labActionCancel},  // 已出报告不能取消
		{labStatusCancelled, labActionCancel}, // 已取消
	}
	for _, tc := range blocked {
		if err := labTransitionAllowed(tc.status, tc.action); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("%s + %s err = %v, want ErrInvalidState", tc.status, tc.action, err)
		}
	}

	if err := labTransitionAllowed(labStatusPending, "verify"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown action err = %v, want ErrInvalidRequest", err)
	}
}

func TestNextLabStatus(t *testing.T) {
	cases := []struct{ action, want string }{
		{labActionSample, labStatusSampled},
		{labActionReport, labStatusReported},
		{labActionCancel, labStatusCancelled},
	}
	for _, tc := range cases {
		got, err := nextLabStatus(tc.action)
		if err != nil || got != tc.want {
			t.Fatalf("nextLabStatus(%q) = %q, %v; want %q", tc.action, got, err, tc.want)
		}
	}
	if _, err := nextLabStatus("verify"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown action err = %v, want ErrInvalidRequest", err)
	}
}
