package his

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeTreatmentStatusFilter(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"pending", []string{"ordered", "charged"}},
		{"ordered", []string{"ordered", "charged"}},
		{"charged", []string{"ordered", "charged"}},
		{"executing", []string{"executing"}},
		{"completed", []string{"completed"}},
	}
	for _, tc := range cases {
		got, err := normalizeTreatmentStatusFilter(tc.input)
		if err != nil {
			t.Fatalf("status %q: %v", tc.input, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("status %q = %v, want %v", tc.input, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("status %q = %v, want %v", tc.input, got, tc.want)
			}
		}
	}
	if _, err := normalizeTreatmentStatusFilter("cancelled"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown status err = %v, want ErrInvalidRequest", err)
	}
}

func TestNormalizeTreatmentDate(t *testing.T) {
	for _, input := range []string{"", "  ", "2026-11-12"} {
		if _, err := normalizeTreatmentDate(input); err != nil {
			t.Fatalf("date %q should be valid: %v", input, err)
		}
	}
	for _, input := range []string{"2026-13-01", "2026/11/12", "20261112", "not-a-date"} {
		if _, err := normalizeTreatmentDate(input); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("date %q err = %v, want ErrInvalidRequest", input, err)
		}
	}
}

func TestNormalizeCreateTreatmentOrder(t *testing.T) {
	valid := CreateTreatmentOrderRequest{EncounterID: 9, ChargeItemID: 3, PlanTotalTimes: 3}
	got, err := normalizeCreateTreatmentOrder(valid)
	if err != nil || got.PlanTotalTimes != 3 {
		t.Fatalf("valid order: %+v %v", got, err)
	}

	byName := CreateTreatmentOrderRequest{EncounterID: 9, ChargeItemName: "  红光治疗  ", PlanTotalTimes: 1}
	got, err = normalizeCreateTreatmentOrder(byName)
	if err != nil || got.ChargeItemName != "红光治疗" {
		t.Fatalf("name order: %+v %v", got, err)
	}

	invalid := []CreateTreatmentOrderRequest{
		{ChargeItemID: 3, PlanTotalTimes: 1},
		{EncounterID: 9, PlanTotalTimes: 1},
		{EncounterID: 9, ChargeItemID: 3, PlanTotalTimes: 0},
		{EncounterID: 9, ChargeItemID: 3, PlanTotalTimes: 1000},
	}
	for _, req := range invalid {
		if _, err := normalizeCreateTreatmentOrder(req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid order %+v err = %v, want ErrInvalidRequest", req, err)
		}
	}
}

func TestNormalizeTreatmentNote(t *testing.T) {
	got, err := normalizeTreatmentNote("  第一次治疗，无不适  ")
	if err != nil || got != "第一次治疗，无不适" {
		t.Fatalf("trim note = %q, %v", got, err)
	}
	if _, err := normalizeTreatmentNote(strings.Repeat("治", 500)); err != nil {
		t.Fatalf("500 runes should pass: %v", err)
	}
	if _, err := normalizeTreatmentNote(strings.Repeat("治", 501)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("501 runes err = %v, want ErrInvalidRequest", err)
	}
}

func TestNextTreatmentProgress(t *testing.T) {
	next, status, err := nextTreatmentProgress(3, 0)
	if err != nil || next != 1 || status != "executing" {
		t.Fatalf("first execution: next=%d status=%s err=%v", next, status, err)
	}
	next, status, err = nextTreatmentProgress(3, 2)
	if err != nil || next != 3 || status != "completed" {
		t.Fatalf("final execution: next=%d status=%s err=%v", next, status, err)
	}
	if _, _, err := nextTreatmentProgress(3, 3); !errors.Is(err, ErrTreatmentCompleted) {
		t.Fatalf("over execution err = %v, want ErrTreatmentCompleted", err)
	}
	// 兼容既有调用方对 ErrInvalidState 的判断。
	if _, _, err := nextTreatmentProgress(3, 3); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("ErrTreatmentCompleted must wrap ErrInvalidState: %v", err)
	}
	if _, _, err := nextTreatmentProgress(0, 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero plan err = %v, want ErrInvalidRequest", err)
	}
}
