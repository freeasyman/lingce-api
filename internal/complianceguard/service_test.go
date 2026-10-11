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

// TestValidateCommunicationCheckRequestAllowsUnboundCustomer 验证患者尚未绑定时，
// 实时电脑麦克风录音仍可进入沟通合规检查。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-10-06
func TestValidateCommunicationCheckRequestAllowsUnboundCustomer(t *testing.T) {
	req := CommunicationCheckRequest{
		TenantID:          1,
		RecordingID:       1834,
		EncounterID:       123,
		EmployeeID:        117,
		EmployeeName:      "测试医生",
		BusinessScope:     "doctor",
		RoleCode:          "doctor",
		RecordedAt:        "2026-10-06T10:00:00+08:00",
		CleanedTranscript: "患者您好，这是实时录音测试文本。",
	}
	if err := validateCommunicationCheckRequest(req); err != nil {
		t.Fatalf("validateCommunicationCheckRequest() error = %v, want nil", err)
	}
}

// TestValidateCommunicationCheckRequestStillRequiresCoreFields 验证放宽患者信息后，
// 录音、接诊、员工、角色和最终转写仍然必须有效。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-10-06
func TestValidateCommunicationCheckRequestStillRequiresCoreFields(t *testing.T) {
	base := CommunicationCheckRequest{
		TenantID:          1,
		RecordingID:       1834,
		EncounterID:       123,
		EmployeeID:        117,
		EmployeeName:      "测试医生",
		BusinessScope:     "doctor",
		RoleCode:          "doctor",
		RecordedAt:        "2026-10-06T10:00:00+08:00",
		CleanedTranscript: "患者您好，这是实时录音测试文本。",
	}
	tests := []struct {
		name   string
		mutate func(*CommunicationCheckRequest)
	}{
		{"missing recording", func(req *CommunicationCheckRequest) { req.RecordingID = 0 }},
		{"missing encounter", func(req *CommunicationCheckRequest) { req.EncounterID = 0 }},
		{"missing employee", func(req *CommunicationCheckRequest) { req.EmployeeID = 0 }},
		{"missing role", func(req *CommunicationCheckRequest) { req.RoleCode = "" }},
		{"missing transcript", func(req *CommunicationCheckRequest) { req.CleanedTranscript = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			tt.mutate(&req)
			if err := validateCommunicationCheckRequest(req); err == nil {
				t.Fatal("validateCommunicationCheckRequest() error = nil, want validation error")
			}
		})
	}
}

func TestCommunicationRerunIdentity(t *testing.T) {
	base := CommunicationCheckRequest{TenantID: 5, RecordingID: 1706, EncounterID: 36, EmployeeID: 104, EmployeeName: "测试医生", BusinessScope: "doctor", RoleCode: "doctor", RecordedAt: "2026-05-28", CleanedTranscript: "医生：保证不会产生血栓。"}
	runtime := &communicationRuntime{RuleSetCode: "communication.doctor.outpatient", RuleSetVersion: "1", PromptCode: "compliance_check_v1", PromptVersion: "candidate"}
	fingerprint := communicationInputFingerprint(base)
	key := buildCommunicationExecutionKey(base, runtime, 29, fingerprint)
	delivery := base
	delivery.TriggerReason = "delivery"
	if got := buildCommunicationExecutionKey(delivery, runtime, 29, communicationInputFingerprint(delivery)); got != key {
		t.Fatal("explicit delivery changed the existing execution identity")
	}
	rerun := base
	rerun.TriggerReason, rerun.RerunKey = "manual_rerun", "retry-stable-key-1"
	if err := validateCommunicationCheckRequest(rerun); err != nil {
		t.Fatal(err)
	}
	first := buildCommunicationExecutionKey(rerun, runtime, 29, communicationInputFingerprint(rerun))
	if first == key || communicationInputFingerprint(rerun) != fingerprint {
		t.Fatal("rerun must change execution identity, not source identity")
	}
	if first != buildCommunicationExecutionKey(rerun, runtime, 29, fingerprint) {
		t.Fatal("rerun retries must be stable")
	}
	rerun.RerunKey = "retry-stable-key-2"
	if first == buildCommunicationExecutionKey(rerun, runtime, 29, fingerprint) {
		t.Fatal("a new intentional rerun must have a new identity")
	}
	for _, pair := range [][2]string{{"manual_rerun", ""}, {"delivery", "key"}, {"", "key"}, {"debug", "key"}} {
		req := base
		req.TriggerReason, req.RerunKey = pair[0], pair[1]
		if validateCommunicationCheckRequest(req) == nil {
			t.Fatalf("accepted invalid trigger/key %v", pair)
		}
	}
}

func TestDoctorPromptSelectionKeepsSharedConsultantPrompt(t *testing.T) {
	doctor := communicationPromptCodes("communication.doctor.outpatient")
	if len(doctor) != 2 || doctor[0] != complianceDoctorPromptCode || doctor[1] != compliancePromptCode {
		t.Fatalf("doctor prompt order: %v", doctor)
	}
	consultant := communicationPromptCodes("communication.consultant.consultation")
	if len(consultant) != 1 || consultant[0] != compliancePromptCode {
		t.Fatalf("consultant prompt order: %v", consultant)
	}
}
