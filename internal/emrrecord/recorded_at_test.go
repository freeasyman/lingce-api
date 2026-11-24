package emrrecord

import (
	"testing"
	"time"
)

func TestParseRecordedAtKeepsBeijingWallClock(t *testing.T) {
	cases := []string{
		"2026-11-19T22:53:40Z",
		"2026-11-19T22:53:40+08:00",
		"2026-11-19 22:53:40",
	}
	for _, value := range cases {
		got := parseRecordedAt(value)
		if got.IsZero() {
			t.Fatalf("%s: 解析失败", value)
		}
		_, offset := got.Zone()
		if offset != 8*3600 {
			t.Fatalf("%s: 时区偏移=%d，期望 28800", value, offset)
		}
		if got.Format("2006-01-02 15:04:05") != "2026-11-19 22:53:40" {
			t.Fatalf("%s: 挂钟值=%s，期望 2026-11-19 22:53:40", value, got.Format("2006-01-02 15:04:05"))
		}
	}
}

func TestParseRecordedAtInvalidReturnsZero(t *testing.T) {
	for _, value := range []string{"", "  ", "not-a-time"} {
		if got := parseRecordedAt(value); !got.IsZero() {
			t.Fatalf("%q: 期望零值，实际 %v", value, got)
		}
	}
}

func TestBeijingWallClockKeepsComponents(t *testing.T) {
	source := time.Date(2026, 11, 19, 14, 53, 40, 0, time.UTC)
	got := beijingWallClock(source)
	if got.Format("2006-01-02 15:04:05") != "2026-11-19 14:53:40" {
		t.Fatalf("挂钟值变了：%s", got.Format("2006-01-02 15:04:05"))
	}
	if _, offset := got.Zone(); offset != 8*3600 {
		t.Fatalf("时区偏移=%d", offset)
	}
}
