package wecom

import (
	"fmt"
	"net/url"
	"strconv"
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

func sanitizeState(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func encodePartnerInstallState(tenantID int64, rawState string) string {
	safeState := sanitizeState(rawState)
	if tenantID <= 0 {
		if safeState == "" {
			return ""
		}
		return safeState
	}
	if safeState == "" {
		return fmt.Sprintf("tenant_%d", tenantID)
	}
	return fmt.Sprintf("tenant_%d_%s", tenantID, safeState)
}

func decodePartnerInstallTenantID(state string) int64 {
	state = sanitizeState(state)
	if !strings.HasPrefix(state, "tenant_") {
		return 0
	}
	rest := strings.TrimPrefix(state, "tenant_")
	if rest == "" {
		return 0
	}
	parts := strings.SplitN(rest, "_", 2)
	tenantID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || tenantID <= 0 {
		return 0
	}
	return tenantID
}

func attachWeComPartnerEntryParams(targetURL string, suiteID, corpID, mode string) string {
	target := strings.TrimSpace(targetURL)
	if target == "" || strings.TrimSpace(suiteID) == "" {
		return target
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	if parsed.Path == "/login" || parsed.Path == "" {
		switch strings.TrimSpace(mode) {
		case ModePartnerTemplate:
			parsed.Path = "/login/partner-template"
		case ModePartnerStandard:
			parsed.Path = "/login/partner-standard"
		}
	}
	query := parsed.Query()
	if query.Get("suite_id") == "" && query.Get("suiteid") == "" {
		query.Set("suite_id", strings.TrimSpace(suiteID))
	}
	if strings.TrimSpace(corpID) != "" && query.Get("corp_id") == "" && query.Get("corpid") == "" {
		query.Set("corp_id", strings.TrimSpace(corpID))
	}
	if query.Get("mode") == "" && strings.TrimSpace(mode) != "" {
		query.Set("mode", strings.TrimSpace(mode))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
