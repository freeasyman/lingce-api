package patientops

import (
	"testing"
	"time"
)

func TestParsePatientDueAtSupportsLocalDateTime(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	for _, value := range []string{"2026-10-20T10:30", "2026-10-20 10:30", "2026-10-20"} {
		parsed, err := parsePatientDueAt(value, now)
		if err != nil {
			t.Fatalf("parse %q: %v", value, err)
		}
		if parsed.Year() != 2026 || parsed.Month() != 10 || parsed.Day() != 20 {
			t.Fatalf("parse %q: unexpected date %v", value, parsed)
		}
	}
}
