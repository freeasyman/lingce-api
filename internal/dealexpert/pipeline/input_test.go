package pipeline

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func material() InputMaterial {
	auth := true
	enc := int64(9001)
	t := time.Date(2026, 10, 31, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	return InputMaterial{Recording: RecordingInput{RecordingID: 1001, TenantID: 101, EncounterID: &enc, EmployeeID: 501, ConsultantName: "顾问"}, Context: ContextInput{AnalysisAt: t, Timezone: "Asia/Shanghai", IdentityConfirmed: true, ContactAuthorized: &auth, Coverage: "complete", CatalogComplete: true, AllowMissedOpportunity: true}, Transcript: []TranscriptInput{{ID: "s1", Text: "顾问介绍方案", Speaker: "咨询师", StartMS: ptrInt(1), TimingBasis: "asr", RoleBasis: "cleaned_transcription"}, {ID: "s2", Text: "我再和家人商量", Speaker: "客户", StartMS: ptrInt(2), TimingBasis: "aligned", RoleBasis: "cleaned_transcription"}}, Facts: []BusinessFactInput{{ID: "f1", Type: "定金", Text: "已核实定金", SourceRecordID: 701, OccurredAt: &t}}, Products: []ProductInput{{ID: 301, Name: "方案甲", Version: "1", RequiresClinicalConfirmation: true}}}
}
func ptrInt(v int) *int { return &v }
func TestAssembleInputDeterministicAndExplicit(t *testing.T) {
	m := material()
	a, err := AssembleInput(m)
	if err != nil {
		t.Fatal(err)
	}
	b, err := AssembleInput(m)
	if err != nil {
		t.Fatal(err)
	}
	if a.InputSnapshotID != b.InputSnapshotID || string(a.Input) != string(b.Input) || string(a.PreprocessingAudit) != string(b.PreprocessingAudit) {
		t.Fatal("input not deterministic")
	}
	var env map[string]any
	if err := json.Unmarshal(a.Input, &env); err != nil {
		t.Fatal(err)
	}
	ctx := env["context"].(map[string]any)
	if ctx["contact_authorized"] != true || ctx["identity_confirmed"] != true {
		t.Fatal("authorization/identity lost")
	}
	if strings.Contains(string(a.Input), "ev_") {
		t.Fatal("server evidence leaked into input")
	}
}
func TestAssembleInputDoesNotInferAuthorization(t *testing.T) {
	m := material()
	m.Context.ContactAuthorized = nil
	m.Context.IdentityConfirmed = true
	a, err := AssembleInput(m)
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	_ = json.Unmarshal(a.Input, &env)
	if env["context"].(map[string]any)["contact_authorized"] != nil {
		t.Fatal("inferred authorization")
	}
}
func TestAssembleInputPreservesUnknownAndTruncation(t *testing.T) {
	m := material()
	m.Context.Coverage = "partial"
	m.Context.Truncated = true
	m.Transcript = append(m.Transcript, TranscriptInput{ID: "s3", Text: "角色无法确认的原文", Speaker: "未知", TimingBasis: "unknown", RoleBasis: "unknown"})
	m.DroppedSourceIDs = []string{"$[3]"}
	a, err := AssembleInput(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(a.Input), "[转写过长，已截断]") || !strings.Contains(string(a.Input), "角色无法确认的原文") {
		t.Fatal("unknown dialogue lost or control marker sent as dialogue")
	}
	if !strings.Contains(string(a.PreprocessingAudit), "truncated") {
		t.Fatal("audit omitted truncation")
	}
}
func TestAssembleInputRejectsInferenceAndDuplicates(t *testing.T) {
	for _, mutate := range []func(*InputMaterial){func(m *InputMaterial) { m.Context.Coverage = "bad" }, func(m *InputMaterial) { m.Context.Truncated = true; m.Context.Coverage = "complete" }, func(m *InputMaterial) { m.Transcript[0].Speaker = "客户"; m.Transcript[0].RoleBasis = "unknown" }, func(m *InputMaterial) { m.Transcript = append(m.Transcript, m.Transcript[0]) }, func(m *InputMaterial) { m.Facts[0].ID = "s3" }, func(m *InputMaterial) { m.Products = append(m.Products, m.Products[0]) }} {
		m := material()
		mutate(&m)
		if _, err := AssembleInput(m); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
}
