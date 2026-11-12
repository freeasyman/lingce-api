package his

import (
	"errors"
	"testing"
	"time"
)

func testNow() time.Time {
	return time.Date(2026, 11, 12, 18, 30, 0, 0, time.Local)
}

func TestResolveStatsRangeDefault(t *testing.T) {
	from, to, err := resolveStatsRange("", "", testNow())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := from.Format(statsDateLayout), "2026-11-06"; got != want {
		t.Fatalf("from = %s, want %s", got, want)
	}
	if got, want := to.Format(statsDateLayout), "2026-11-12"; got != want {
		t.Fatalf("to = %s, want %s", got, want)
	}
	if days := statsRangeDays(from, to); days != statsDefaultRangeDays {
		t.Fatalf("days = %d, want %d", days, statsDefaultRangeDays)
	}
}

func TestResolveStatsRangeOneSideEmpty(t *testing.T) {
	from, to, err := resolveStatsRange("2026-11-01", "", testNow())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := from.Format(statsDateLayout), "2026-11-01"; got != want {
		t.Fatalf("from = %s, want %s", got, want)
	}
	if got, want := to.Format(statsDateLayout), "2026-11-12"; got != want {
		t.Fatalf("to = %s, want %s", got, want)
	}

	from, to, err = resolveStatsRange("", "2026-10-31", testNow())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := from.Format(statsDateLayout), "2026-10-25"; got != want {
		t.Fatalf("from = %s, want %s", got, want)
	}
	if got, want := to.Format(statsDateLayout), "2026-10-31"; got != want {
		t.Fatalf("to = %s, want %s", got, want)
	}
}

func TestResolveStatsRangeInvalid(t *testing.T) {
	cases := []struct {
		name     string
		dateFrom string
		dateTo   string
	}{
		{name: "reversed", dateFrom: "2026-11-12", dateTo: "2026-11-01"},
		{name: "bad format", dateFrom: "2026/11/01", dateTo: "2026-11-12"},
		{name: "bad date", dateFrom: "2026-02-30", dateTo: "2026-03-02"},
		{name: "too long", dateFrom: "2025-11-10", dateTo: "2026-11-12"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := resolveStatsRange(tc.dateFrom, tc.dateTo, testNow()); !errors.Is(err, ErrInvalidDateRange) {
				t.Fatalf("err = %v, want ErrInvalidDateRange", err)
			}
		})
	}
}

func TestResolveStatsRangeMaxAllowed(t *testing.T) {
	// 含首尾正好 366 天：2025-11-12 ~ 2026-11-12。
	from, to, err := resolveStatsRange("2025-11-12", "2026-11-12", testNow())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if days := statsRangeDays(from, to); days != statsMaxRangeDays {
		t.Fatalf("days = %d, want %d", days, statsMaxRangeDays)
	}
}

func TestFillRevenueByDay(t *testing.T) {
	from := time.Date(2026, 11, 6, 0, 0, 0, 0, time.Local)
	to := time.Date(2026, 11, 12, 0, 0, 0, 0, time.Local)
	rows := fillRevenueByDay(from, to, map[string]float64{
		"2026-11-06": 120.5,
		"2026-11-12": 80,
	})
	if len(rows) != 7 {
		t.Fatalf("len = %d, want 7", len(rows))
	}
	if rows[0].Date != "2026-11-06" || rows[0].Revenue != 120.5 {
		t.Fatalf("first row = %+v", rows[0])
	}
	if rows[1].Date != "2026-11-07" || rows[1].Revenue != 0 {
		t.Fatalf("zero fill row = %+v", rows[1])
	}
	if rows[6].Date != "2026-11-12" || rows[6].Revenue != 80 {
		t.Fatalf("last row = %+v", rows[6])
	}
}

func TestFillRevenueByDaySingleDay(t *testing.T) {
	day := time.Date(2026, 11, 12, 0, 0, 0, 0, time.Local)
	rows := fillRevenueByDay(day, day, nil)
	if len(rows) != 1 || rows[0].Date != "2026-11-12" || rows[0].Revenue != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestStatsAvgTicket(t *testing.T) {
	if got := statsAvgTicket(100, 3); got != 33.33 {
		t.Fatalf("avg = %v, want 33.33", got)
	}
	if got := statsAvgTicket(100, 0); got != 0 {
		t.Fatalf("avg with no charge = %v, want 0", got)
	}
	if got := statsAvgTicket(0, 5); got != 0 {
		t.Fatalf("avg with no revenue = %v, want 0", got)
	}
}

func TestNormalizeStatsPaymentMethods(t *testing.T) {
	rows := normalizeStatsPaymentMethods([]StatsPaymentMethod{
		{Method: "cash", Amount: 30, Count: 2},
		{Method: "wechat", Amount: 100.456, Count: 1},
		{Method: "voucher", Amount: 9, Count: 1},
	})
	if len(rows) != 5 {
		t.Fatalf("len = %d, want 5: %+v", len(rows), rows)
	}
	wantOrder := []string{"wechat", "alipay", "cash", "insurance", "voucher"}
	for i, want := range wantOrder {
		if rows[i].Method != want {
			t.Fatalf("row %d method = %s, want %s", i, rows[i].Method, want)
		}
	}
	if rows[0].Amount != 100.46 {
		t.Fatalf("wechat amount = %v, want rounded 100.46", rows[0].Amount)
	}
	if rows[1].Count != 0 || rows[1].Amount != 0 {
		t.Fatalf("alipay should be zero-filled: %+v", rows[1])
	}
}

func TestNormalizeStatsPaymentMethodsEmpty(t *testing.T) {
	rows := normalizeStatsPaymentMethods(nil)
	if len(rows) != 4 {
		t.Fatalf("len = %d, want 4", len(rows))
	}
	for i, want := range statsPaymentMethodOrder {
		if rows[i].Method != want {
			t.Fatalf("row %d = %s, want %s", i, rows[i].Method, want)
		}
	}
}

func TestNormalizeVisitStatuses(t *testing.T) {
	rows := normalizeVisitStatuses([]StatsVisitStatus{
		{Status: "visited", Count: 3},
		{Status: "no_show", Count: 1},
	})
	if len(rows) != len(statsVisitStatuses) {
		t.Fatalf("len = %d, want %d", len(rows), len(statsVisitStatuses))
	}
	for i, want := range statsVisitStatuses {
		if rows[i].Status != want {
			t.Fatalf("row %d = %s, want %s", i, rows[i].Status, want)
		}
	}
	if rows[1].Count != 3 || rows[4].Count != 1 || rows[0].Count != 0 {
		t.Fatalf("counts = %+v", rows)
	}
}
