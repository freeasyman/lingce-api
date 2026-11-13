package complianceguard

import (
	"strings"
	"testing"
	"time"
)

// TestDecisionActionConclusions 锁定页面三分法到既有结论枚举的映射。
func TestDecisionActionConclusions(t *testing.T) {
	cases := map[string]string{
		"close":   "not_confirmed",
		"letter":  "confirmed",
		"rectify": "confirmed",
	}
	for action, want := range cases {
		if got := decisionActionConclusions[action]; got != want {
			t.Fatalf("action %s maps to %q, want %q", action, got, want)
		}
	}
	if _, ok := decisionActionConclusions["confirm"]; ok {
		t.Fatal("unknown action must not map to a conclusion")
	}
}

// TestParseDecisionTime 覆盖前端会传来的三种时间写法。
func TestParseDecisionTime(t *testing.T) {
	date, err := parseDecisionTime("signed_at", "2026-12-20")
	if err != nil || date == nil {
		t.Fatalf("date-only parse failed: %v", err)
	}
	if date.Year() != 2026 || date.Month() != time.December || date.Day() != 20 {
		t.Fatalf("date-only parse drifted: %v", date)
	}
	if date.Hour() != 0 {
		t.Fatalf("date-only should keep local midnight, got %v", date)
	}

	rfc, err := parseDecisionTime("signed_at", "2026-12-20T09:30:00+08:00")
	if err != nil || rfc == nil || rfc.Hour() != 9 {
		t.Fatalf("rfc3339 parse failed: %v %v", rfc, err)
	}

	if empty, err := parseDecisionTime("signed_at", "  "); err != nil || empty != nil {
		t.Fatalf("empty value should be nil: %v %v", empty, err)
	}

	if _, err := parseDecisionTime("signed_at", "下周一"); err == nil {
		t.Fatal("invalid date should return an error")
	}
}

// TestParseDecisionDate 确认 review_date 只保留日期。
func TestParseDecisionDate(t *testing.T) {
	date, err := parseDecisionDate("review_date", "2026-12-20T18:00:00+08:00")
	if err != nil || date == nil {
		t.Fatalf("review date parse failed: %v", err)
	}
	if date.Format("2006-01-02") != "2026-12-20" || date.Hour() != 0 {
		t.Fatalf("review date should be date-only, got %v", date)
	}
}

// TestCheckItemSelectColumnsCoverDecisionFields 保证列表/详情查询包含新落库字段。
func TestCheckItemSelectColumnsCoverDecisionFields(t *testing.T) {
	for _, column := range []string{
		"decision_action", "decision_reason", "decision_by", "decision_at",
		"sign_method", "signed_at", "review_date", "review_method",
		"review_conclusion", "closed_at",
	} {
		if !strings.Contains(checkItemSelectColumns, column) {
			t.Fatalf("checkItemSelectColumns missing %s", column)
		}
		if !strings.Contains(checkItemSelectColumnsAliased, "i."+column) {
			t.Fatalf("checkItemSelectColumnsAliased missing i.%s", column)
		}
	}
}
