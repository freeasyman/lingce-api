package complianceguard

import (
	"encoding/json"
	"fmt"
	"strings"
)

var complianceRiskTypes = map[string]struct{}{
	"patient_decision": {}, "medical_safety": {}, "legal_evidence": {},
	"business_reputation": {}, "management_system": {},
}
var compliancePriorities = map[string]struct{}{"urgent": {}, "important": {}, "normal": {}}

// Validation requires the exact transcript and enabled rules used in this call.
// This candidate parser is deliberately isolated from the legacy runtime parser.
func decodeCommunicationFindings(raw, transcript string, allowedRules []string) ([]any, error) {
	var payload struct {
		Findings []any `json:"findings"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("invalid model JSON: %w", err)
	}
	if payload.Findings == nil {
		return nil, fmt.Errorf("findings must be an array")
	}
	allowed := map[string]bool{}
	for _, rule := range allowedRules {
		allowed[rule] = true
	}
	for i, value := range payload.Findings {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("finding[%d] must be object", i)
		}
		fail := func(field string) error { return fmt.Errorf("finding[%d].%s violates contract", i, field) }
		for _, field := range []string{"theme_key", "rule_code", "question", "risk_hypothesis", "primary_risk_type", "priority"} {
			s, ok := item[field].(string)
			if !ok || strings.TrimSpace(s) == "" {
				return nil, fail(field)
			}
		}
		if !allowed[item["rule_code"].(string)] {
			return nil, fail("rule_code")
		}
		for _, pair := range []struct {
			key  string
			want bool
		}{{"is_normal_communication", false}, {"review_recommended", true}} {
			b, ok := item[pair.key].(bool)
			if !ok || b != pair.want {
				return nil, fail(pair.key)
			}
		}
		if _, ok := complianceRiskTypes[item["primary_risk_type"].(string)]; !ok {
			return nil, fail("primary_risk_type")
		}
		if _, ok := compliancePriorities[item["priority"].(string)]; !ok {
			return nil, fail("priority")
		}
		for _, field := range []string{"check_basis", "suggested_actions", "source_rule_codes", "related_risk_types"} {
			values, ok := item[field].([]any)
			if !ok || (field != "related_risk_types" && len(values) == 0) {
				return nil, fail(field)
			}
			containsPrimary := false
			for _, v := range values {
				s, ok := v.(string)
				if !ok || strings.TrimSpace(s) == "" {
					return nil, fail(field)
				}
				if field == "related_risk_types" {
					if _, ok := complianceRiskTypes[s]; !ok {
						return nil, fail(field)
					}
				}
				if field == "source_rule_codes" {
					if !allowed[s] {
						return nil, fail(field)
					}
					if s == item["rule_code"] {
						containsPrimary = true
					}
				}
			}
			if field == "source_rule_codes" && !containsPrimary {
				return nil, fail(field)
			}
		}
		evidence, ok := item["evidence"].([]any)
		if !ok || len(evidence) == 0 {
			return nil, fail("evidence")
		}
		for _, value := range evidence {
			e, ok := value.(map[string]any)
			if !ok {
				return nil, fail("evidence element")
			}
			quote, ok := e["quote"].(string)
			if !ok || strings.TrimSpace(quote) == "" || !strings.Contains(transcript, quote) {
				return nil, fail("evidence.quote")
			}
			speaker, present := e["speaker"]
			if !present {
				return nil, fail("evidence.speaker")
			}
			if speaker != nil {
				s, ok := speaker.(string)
				if !ok || strings.TrimSpace(s) == "" {
					return nil, fail("evidence.speaker")
				}
			}
			for _, key := range []string{"segment_index", "start_second", "end_second"} {
				v, present := e[key]
				if !present {
					return nil, fail("evidence." + key)
				}
				if v == nil {
					continue
				}
				n, ok := v.(float64)
				if !ok || n < 0 || (key == "segment_index" && n != float64(int64(n))) {
					return nil, fail("evidence." + key)
				}
			}
			start, sok := e["start_second"].(float64)
			end, eok := e["end_second"].(float64)
			if sok != eok || (sok && end < start) {
				return nil, fail("evidence.time_range")
			}
		}
	}
	return payload.Findings, nil
}
