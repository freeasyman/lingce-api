package testcleanup

import "testing"

func TestClassifyCleanupPlan(t *testing.T) {
	p := classify(map[string]int64{
		"recordings":    3,
		"content_items": 2,
		"customers":     4,
		"badge_tickets": 1,
		"unknown_table": 7,
	})
	if p.Deletable["recordings"] != 3 || p.Deletable["content_items"] != 2 || p.Deletable["badge_tickets"] != 1 {
		t.Fatalf("deletable classification mismatch: %#v", p.Deletable)
	}
	if p.Protected["customers"] != 4 {
		t.Fatalf("protected classification mismatch: %#v", p.Protected)
	}
	if p.Review["unknown_table"] != 7 {
		t.Fatalf("review classification mismatch: %#v", p.Review)
	}
	if p.Notes["customers"] == "" || p.Notes["unknown_table"] == "" {
		t.Fatal("classification notes must explain each decision")
	}
}
