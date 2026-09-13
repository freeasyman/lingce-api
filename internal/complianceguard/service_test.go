package complianceguard

import "testing"

// TestCommunicationRoleRouting 验证医生和咨询师分别选择各自规则集，
// 且 sales 不会被错误地当作咨询师。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-13
func TestCommunicationRoleRouting(t *testing.T) {
	tests := []struct {
		name          string
		businessScope string
		roleCode      string
		wantRuleSet   string
		wantError     bool
	}{
		{
			name:          "doctor",
			businessScope: "doctor",
			roleCode:      "doctor",
			wantRuleSet:   "communication.doctor.outpatient",
		},
		{
			name:          "consultant",
			businessScope: "consultant",
			roleCode:      "consultant",
			wantRuleSet:   "communication.consultant.consultation",
		},
		{
			name:          "consultant role fallback",
			businessScope: "",
			roleCode:      "consultant",
			wantRuleSet:   "communication.consultant.consultation",
		},
		{
			name:          "sales is explicitly unsupported",
			businessScope: "sales",
			roleCode:      "sales",
			wantError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCommunicationRole(tt.businessScope, tt.roleCode)
			if tt.wantError {
				if err == nil {
					t.Fatal("validateCommunicationRole() error = nil, want unsupported role error")
				}
				if _, ok := err.(*UnsupportedRoleError); !ok {
					t.Fatalf("error type = %T, want *UnsupportedRoleError", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateCommunicationRole() error = %v", err)
			}
			if got := defaultCommunicationRuleSetCode(tt.businessScope, tt.roleCode); got != tt.wantRuleSet {
				t.Fatalf("defaultCommunicationRuleSetCode() = %q, want %q", got, tt.wantRuleSet)
			}
		})
	}
}

// TestCommunicationEmployeeNameCompatibility 验证新字段优先、旧字段回退，
// 保证历史 Worker 请求仍然可以进入合规分析。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-13
func TestCommunicationEmployeeNameCompatibility(t *testing.T) {
	if got := communicationEmployeeName(CommunicationCheckRequest{
		EmployeeName: "新员工姓名",
		DoctorName:   "旧字段姓名",
	}); got != "新员工姓名" {
		t.Fatalf("communicationEmployeeName() = %q, want new field", got)
	}
	if got := communicationEmployeeName(CommunicationCheckRequest{
		DoctorName: "旧字段姓名",
	}); got != "旧字段姓名" {
		t.Fatalf("communicationEmployeeName() = %q, want legacy fallback", got)
	}
}
