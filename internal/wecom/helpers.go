package wecom

import (
	"strings"
	"time"
)

func maskString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return strings.Repeat("*", len(value))
	}
	return value[:4] + strings.Repeat("*", len(value)-8) + value[len(value)-4:]
}

func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}
