package badge

import "strings"

const (
	HealthStatusUnknown = "unknown"
	HealthStatusHealthy = "healthy"
	HealthStatusWarning = "warning"
	HealthStatusError   = "error"
)

func normalizeHealthStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case HealthStatusHealthy, "normal":
		return HealthStatusHealthy
	case HealthStatusWarning:
		return HealthStatusWarning
	case HealthStatusError:
		return HealthStatusError
	case HealthStatusUnknown:
		return HealthStatusUnknown
	default:
		return HealthStatusUnknown
	}
}

func parseHealthStatusFilter(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case HealthStatusUnknown:
		return HealthStatusUnknown, true
	case HealthStatusHealthy, "normal":
		return HealthStatusHealthy, true
	case HealthStatusWarning:
		return HealthStatusWarning, true
	case HealthStatusError:
		return HealthStatusError, true
	default:
		return "", false
	}
}
