package followup

import (
	"fmt"
	"strings"
	"time"
)

// Relative dates are anchored to the encounter, never to a retry's wall clock.
func resolveContactTime(value, recordedAt string) (time.Time, error) {
	if absolute, err := parseContactTime(value); err == nil {
		return absolute, nil
	}
	offsets := map[string]int{"今天": 0, "明天": 1, "后天": 2, "三天后": 3, "一周后": 7, "7天后": 7, "两周后": 14, "14天后": 14}
	days, ok := offsets[strings.TrimSpace(value)]
	if !ok {
		if strings.TrimSpace(value) == "医院默认" || strings.TrimSpace(value) == "默认" {
			return hospitalDefaultContactTime(recordedAt)
		}
		return time.Time{}, fmt.Errorf("ambiguous contact time %q", value)
	}
	anchor, err := time.Parse(time.RFC3339, recordedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("relative date requires valid recorded_at: %w", err)
	}
	return time.Date(anchor.Year(), anchor.Month(), anchor.Day()+days, 10, 0, 0, 0, anchor.Location()), nil
}

func hospitalDefaultContactTime(recordedAt string) (time.Time, error) {
	anchor, err := time.Parse(time.RFC3339, recordedAt)
	if err != nil {
		return time.Time{}, err
	}
	// 医院默认规则：下一个工作日 10:00；周末顺延至周一。
	d := anchor.AddDate(0, 0, 1)
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, 1)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 10, 0, 0, 0, d.Location()), nil
}
