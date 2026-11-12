package his

import (
	"errors"
	"testing"
	"time"
)

func workbenchNow() time.Time {
	return time.Date(2026, 11, 12, 18, 30, 0, 0, time.Local)
}

func TestResolveWorkbenchDateDefaultToday(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		date, err := resolveWorkbenchDate(raw, workbenchNow())
		if err != nil {
			t.Fatalf("resolveWorkbenchDate(%q) error: %v", raw, err)
		}
		if got, want := date.Format(workbenchDateLayout), "2026-11-12"; got != want {
			t.Fatalf("date = %s, want %s", got, want)
		}
		if date.Hour() != 0 || date.Minute() != 0 {
			t.Fatalf("default date should be midnight, got %v", date)
		}
	}
}

func TestResolveWorkbenchDateExplicit(t *testing.T) {
	date, err := resolveWorkbenchDate(" 2026-01-05 ", workbenchNow())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := date.Format(workbenchDateLayout), "2026-01-05"; got != want {
		t.Fatalf("date = %s, want %s", got, want)
	}
}

func TestResolveWorkbenchDateInvalid(t *testing.T) {
	for _, raw := range []string{"2026/11/12", "2026-13-01", "2026-02-30", "today", "2026-11-12T00:00:00"} {
		if _, err := resolveWorkbenchDate(raw, workbenchNow()); !errors.Is(err, ErrInvalidWorkbenchDate) {
			t.Fatalf("resolveWorkbenchDate(%q) error = %v, want ErrInvalidWorkbenchDate", raw, err)
		}
	}
}

func TestWorkbenchTodoLabel(t *testing.T) {
	cases := map[string]string{
		workbenchTodoTypeWait: "待接诊",
		workbenchTodoTypeFee:  "待收费",
		workbenchTodoTypeDisp: "待发药",
		workbenchTodoTypeExec: "待执行",
		workbenchTodoTypeLab:  "待检验",
		workbenchTodoTypeExam: "待检查",
		"custom":              "custom",
	}
	for kind, want := range cases {
		if got := workbenchTodoLabel(kind); got != want {
			t.Fatalf("workbenchTodoLabel(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestNormalizeWorkbenchTodos(t *testing.T) {
	if got := normalizeWorkbenchTodos(nil, workbenchTodoLimit); got == nil || len(got) != 0 {
		t.Fatalf("nil todos should normalize to empty non-nil slice, got %#v", got)
	}

	items := make([]WorkbenchTodo, 0, 12)
	for i := 0; i < 12; i++ {
		items = append(items, WorkbenchTodo{Type: workbenchTodoTypeLab, RefID: int64(i + 1)})
	}
	got := normalizeWorkbenchTodos(items, workbenchTodoLimit)
	if len(got) != workbenchTodoLimit {
		t.Fatalf("len = %d, want %d", len(got), workbenchTodoLimit)
	}
	if got[0].RefID != 1 || got[len(got)-1].RefID != int64(workbenchTodoLimit) {
		t.Fatalf("todos should keep input order, got first=%d last=%d", got[0].RefID, got[len(got)-1].RefID)
	}
	for _, item := range got {
		if item.TypeLabel != "待检验" {
			t.Fatalf("type label not filled: %#v", item)
		}
	}

	zero := normalizeWorkbenchTodos(items, 0)
	if zero == nil || len(zero) != 0 {
		t.Fatalf("limit 0 should return empty non-nil slice, got %#v", zero)
	}
}

func TestMergeWorkbenchDoctors(t *testing.T) {
	if got := mergeWorkbenchDoctors(nil, nil, workbenchDoctorLimit); got == nil || len(got) != 0 {
		t.Fatalf("nil input should merge to empty non-nil slice, got %#v", got)
	}

	visits := []workbenchDoctorCount{
		{DoctorID: 2, DoctorName: "周明", VisitCount: 2},
		{DoctorID: 1, DoctorName: "赵敏", VisitCount: 3},
		{DoctorID: 3, DoctorName: "王丽", VisitCount: 3},
	}
	records := []workbenchDoctorCount{
		{DoctorID: 1, DoctorName: "赵敏", RecordCount: 2},
		{DoctorID: 3, DoctorName: "王丽", RecordCount: 1},
		{DoctorID: 4, DoctorName: "林晓", RecordCount: 2},
	}
	got := mergeWorkbenchDoctors(visits, records, workbenchDoctorLimit)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4: %#v", len(got), got)
	}
	// 接诊量倒序；同为 3 时按完成病历数倒序。
	if got[0].DoctorID != 1 || got[0].VisitCount != 3 || got[0].CompletedRecords != 2 {
		t.Fatalf("first row = %#v, want doctor 1", got[0])
	}
	if got[1].DoctorID != 3 || got[1].CompletedRecords != 1 {
		t.Fatalf("second row = %#v, want doctor 3", got[1])
	}
	if got[2].DoctorID != 2 || got[2].CompletedRecords != 0 {
		t.Fatalf("third row = %#v, want doctor 2", got[2])
	}
	if got[3].DoctorID != 4 || got[3].VisitCount != 0 || got[3].CompletedRecords != 2 {
		t.Fatalf("record-only doctor not merged: %#v", got[3])
	}

	limited := mergeWorkbenchDoctors(visits, records, 2)
	if len(limited) != 2 {
		t.Fatalf("limited len = %d, want 2", len(limited))
	}
	if limited[0].DoctorID != 1 || limited[1].DoctorID != 3 {
		t.Fatalf("limited rows = %#v", limited)
	}
}
