package complianceguard

import (
	"encoding/json"
	"testing"
)

// TestCommunicationRuleSetConsultantContract 锁定咨询师场景与规则集的绑定关系。
//
// 咨询师录音必须使用 communication.consultant.consultation 规则集和 consultant
// 角色查询规则；任何一侧缺失都会让 loadCommunicationRules 返回空规则集，导致咨询师
// 录音被静默跳过。
func TestCommunicationRuleSetConsultantContract(t *testing.T) {
	const consultantScene = "communication.consultant.consultation"

	if got := communicationRuleRole(consultantScene); got != "consultant" {
		t.Fatalf("communicationRuleRole(%q) = %q, want consultant", consultantScene, got)
	}
	if got := communicationRuleRole("communication.doctor.outpatient"); got != "doctor" {
		t.Fatalf("doctor scene role = %q, want doctor", got)
	}
	if got := communicationRuleRole("unknown.scene"); got != "" {
		t.Fatalf("unknown scene role = %q, want empty", got)
	}
	if got := communicationRuleSetName(consultantScene); got != "咨询师沟通合规" {
		t.Fatalf("communicationRuleSetName(%q) = %q", consultantScene, got)
	}
}

// TestConsultantRoutingCoversScopeAndRole 验证业务范围和角色任一为 consultant
// 时都能进入咨询师规则集，同时 therapist/frontdesk 不会被误路由为咨询师。
func TestConsultantRoutingCoversScopeAndRole(t *testing.T) {
	tests := []struct {
		name          string
		businessScope string
		roleCode      string
		want          string
	}{
		{"scope only", "consultant", "", "communication.consultant.consultation"},
		{"role only", "", "consultant", "communication.consultant.consultation"},
		{"both", "consultant", "consultant", "communication.consultant.consultation"},
		{"scope variant", "consultant_conversion", "", "communication.consultant.consultation"},
		{"role variant", "", "consultant_conversion", "communication.consultant.consultation"},
		{"therapist falls back to doctor set", "therapist", "therapist", "communication.doctor.outpatient"},
		{"frontdesk falls back to doctor set", "frontdesk", "frontdesk", "communication.doctor.outpatient"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultCommunicationRuleSetCode(tt.businessScope, tt.roleCode); got != tt.want {
				t.Fatalf("defaultCommunicationRuleSetCode(%q,%q) = %q, want %q",
					tt.businessScope, tt.roleCode, got, tt.want)
			}
		})
	}
}

// TestCommunicationRoleAllowsConsultant 明确 consultant 不应被角色校验拦截。
func TestCommunicationRoleAllowsConsultant(t *testing.T) {
	for _, pair := range [][2]string{
		{"consultant", "consultant"},
		{"", "consultant"},
		{"consultant", ""},
	} {
		if err := validateCommunicationRole(pair[0], pair[1]); err != nil {
			t.Fatalf("validateCommunicationRole(%q,%q) = %v, want nil", pair[0], pair[1], err)
		}
	}
}

// TestConsultantFindingPassesRiskCandidateContract 验证咨询师专属规则
// 在 risk_candidate_v1 契约下可以通过解析，确保能生成核查事项。
func TestConsultantFindingPassesRiskCandidateContract(t *testing.T) {
	const transcript = "咨询师：这个项目我们是全国唯一能做的，别家都不行。"
	finding := map[string]any{
		"theme_key":               "consultant_exclusivity",
		"rule_code":               "communication.misleading.exclusivity",
		"source_rule_codes":       []any{"communication.misleading.exclusivity"},
		"question":                "咨询师是否把项目表述为全国唯一？",
		"risk_hypothesis":         "可能构成绝对化、排他性宣传。",
		"primary_risk_type":       "patient_decision",
		"related_risk_types":      []any{"business_reputation"},
		"check_basis":             []any{"规则：绝对化疗效、设备或方案排他宣传"},
		"suggested_actions":       []any{"核实宣传与设备授权依据"},
		"priority":                "important",
		"is_normal_communication": false,
		"review_recommended":      true,
		"evidence": []any{map[string]any{
			"quote":         "这个项目我们是全国唯一能做的，别家都不行",
			"speaker":       "咨询师",
			"segment_index": nil,
			"start_second":  nil,
			"end_second":    nil,
		}},
	}
	raw, err := json.Marshal(map[string]any{"findings": []any{finding}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseFindingsForRuntime(string(raw), transcript, []string{"communication.misleading.exclusivity"}, "risk_candidate_v1")
	if err != nil {
		t.Fatalf("consultant finding rejected: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("findings = %d, want 1", len(got))
	}
}
