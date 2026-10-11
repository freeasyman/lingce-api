package complianceguard

// Opt-in real Gateway replay. Outputs live outside the repository. Persistence
// uses connection-local temporary copies; no hospital work items are created.
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type phase5Sample struct {
	Name     string                            `json:"sample"`
	Request  CommunicationCheckRequest         `json:"input"`
	Expected int                               `json:"expected"`
	Raw      *llmgateway.TextInferenceResponse `json:"response,omitempty"`
	Findings []any                             `json:"findings,omitempty"`
	Error    string                            `json:"error,omitempty"`
}

func TestPhase5RealReplay(t *testing.T) {
	dir := os.Getenv("COMPLIANCE_PHASE5_DIR")
	if dir == "" {
		t.Skip("set COMPLIANCE_PHASE5_DIR outside the repository to run real Gateway replay")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs("../..")
	if dir == root || strings.HasPrefix(dir, root+string(os.PathSeparator)) {
		t.Fatal("real artifacts must live outside the repository")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(root, "configs/dev.toml"))
	if err != nil {
		t.Fatal(err)
	}
	pc, err := pgxpool.ParseConfig(cfg.Database.DSN())
	if err != nil {
		t.Fatal("invalid database config")
	}
	pc.MaxConns, pc.MinConns = 1, 0
	pc.ConnConfig.ConnectTimeout = 15 * time.Second
	pc.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		for _, table := range []string{"compliance_check_runs", "compliance_findings", "compliance_check_items", "compliance_check_item_sources", "compliance_check_item_process_records", "recording_analysis_prompts", "recording_analysis_tenant_configs"} {
			if _, err := conn.Exec(ctx, "CREATE TEMP TABLE "+table+" (LIKE public."+table+" INCLUDING ALL)"); err != nil {
				return err
			}
		}
		_, err := conn.Exec(ctx, "SET search_path TO pg_temp,public")
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// Use one connection so every operation sees the same temporary tables.
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	svc := NewService(pool, llmgateway.NewClient(cfg.External.LLMGatewayURL, cfg.External.LLMGatewayAPIKey))
	var candidate struct {
		System string          `json:"system_prompt"`
		User   string          `json:"user_prompt_template"`
		Schema json.RawMessage `json:"output_schema"`
	}
	b, err := os.ReadFile(filepath.Join(root, "docs/compliance/候选回放/医生风险线索提示词-v3.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &candidate); err != nil {
		t.Fatal(err)
	}
	contract, err := detectCommunicationOutputContract(candidate.Schema)
	if err != nil || contract != "risk_candidate_v1" {
		t.Fatalf("candidate schema: %s %v", contract, err)
	}
	// Actual selection queries run against temporary configuration copies too.
	if _, err := pool.Exec(ctx, `INSERT INTO recording_analysis_prompts SELECT * FROM public.recording_analysis_prompts WHERE code=$1`, compliancePromptCode); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO recording_analysis_tenant_configs SELECT * FROM public.recording_analysis_tenant_configs WHERE prompt_code=$1`, compliancePromptCode); err != nil {
		t.Fatal(err)
	}
	before, err := svc.loadCommunicationPrompt(ctx, 5, "communication.consultant.consultation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO recording_analysis_prompts(code,name,category,system_prompt,user_prompt_template,output_schema,version,is_active) VALUES($1,'隔离医生候选','analysis',$2,$3,$4::json,'v4-safety-20261011',true)`, complianceDoctorPromptCode, candidate.System, candidate.User, string(candidate.Schema)); err != nil {
		t.Fatal(err)
	}
	doctor, err := svc.loadCommunicationPrompt(ctx, 5, "communication.doctor.outpatient")
	if err != nil || doctor.Code != complianceDoctorPromptCode || doctor.OutputContract != contract {
		t.Fatalf("doctor selection: %+v %v", doctor, err)
	}
	consultant, err := svc.loadCommunicationPrompt(ctx, 5, "communication.consultant.consultation")
	if err != nil || consultant.Code != before.Code || consultant.SystemPrompt != before.SystemPrompt || string(consultant.OutputSchema) != string(before.OutputSchema) {
		t.Fatalf("consultant configuration changed: %+v %v", consultant, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE recording_analysis_prompts SET is_active=false WHERE code=$1`, complianceDoctorPromptCode); err != nil {
		t.Fatal(err)
	}
	doctor, err = svc.loadCommunicationPrompt(ctx, 5, "communication.doctor.outpatient")
	if err != nil || doctor.Code != compliancePromptCode {
		t.Fatalf("doctor rollback selection: %+v %v", doctor, err)
	}
	model, err := svc.loadModelConfig(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	_, rules, version, err := svc.loadCommunicationRules(ctx, 5, "communication.doctor.outpatient")
	if err != nil {
		t.Fatal(err)
	}
	rulesJSON, _ := json.Marshal(rules)
	// Fixed illustrative inputs contain no model answers or real recording data;
	// actual responses are always freshly requested below.
	var original [][]json.RawMessage
	b, err = os.ReadFile("testdata/phase2_samples.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &original); err != nil {
		t.Fatal(err)
	}
	samples := make([]phase5Sample, 0, 40)
	for _, row := range original {
		var name, text string
		var expected int
		if len(row) != 3 {
			t.Fatal("invalid regression sample")
		}
		for j, target := range []any{&name, &expected, &text} {
			if err := json.Unmarshal(row[j], target); err != nil {
				t.Fatal(err)
			}
		}
		samples = append(samples, phase5Sample{Name: name, Expected: expected, Request: CommunicationCheckRequest{TenantID: 5, CleanedTranscript: text, RoleCode: "doctor", BusinessScope: "doctor", EmployeeName: "测试医生"}})
	}
	for _, v := range []struct {
		name, text string
		expected   int
	}{
		{"safety-promise", "医生：我保证用这个药后你绝对不会产生血栓，不会有任何并发症。", 1},
		{"safety-prevention", "医生：我们的目标是尽量预防血栓，这不能保证不会发生，仍需监测。", 0},
		{"safety-patient-wish", "患者：至少保证不产生血栓就行。医生：不能这样保证，用药只是降低风险。", 0},
		{"safety-goal", "医生：治疗目标是预防血栓，我们希望尽量避免，但风险不能归零。", 0},
		{"safety-quoted", "医生：有人说保证不产生血栓，这是不对的，不能作这种承诺。", 0},
		{"safety-conditional", "医生：遵医嘱治疗有助于减少血栓风险，仍可能发生，要定期复查。", 0},
	} {
		samples = append(samples, phase5Sample{Name: v.name, Expected: v.expected, Request: CommunicationCheckRequest{TenantID: 5, CleanedTranscript: v.text, RoleCode: "doctor", BusinessScope: "doctor", EmployeeName: "测试医生"}})
	}
	for _, v := range []struct {
		id       int64
		expected int
	}{{1706, 1}, {1793, 0}, {1818, 0}, {1796, 0}} {
		var req CommunicationCheckRequest
		var raw []byte
		err := pool.QueryRow(ctx, `SELECT r.tenant_id,r.id,COALESCE(r.encounter_id,0),r.employee_id,COALESCE(r.customer_id,0),COALESCE(e.name,''),r.recorded_at::text,r.cleaned_transcription FROM public.recordings r JOIN public.employees e ON e.id=r.employee_id AND e.tenant_id=r.tenant_id WHERE r.id=$1 AND r.deleted_at IS NULL`, v.id).Scan(&req.TenantID, &req.RecordingID, &req.EncounterID, &req.EmployeeID, &req.CustomerID, &req.EmployeeName, &req.RecordedAt, &raw)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		switch x := doc.(type) {
		case map[string]any:
			req.CleanedTranscript, _ = x["text"].(string)
		case []any:
			var lines []string
			for _, segment := range x {
				if m, ok := segment.(map[string]any); ok {
					if text, ok := m["text"].(string); ok {
						lines = append(lines, text)
					}
				}
			}
			req.CleanedTranscript = strings.Join(lines, "\n")
		default:
			t.Fatal("unsupported transcript")
		}
		req.RoleCode, req.BusinessScope = "doctor", "doctor"
		samples = append(samples, phase5Sample{Name: fmt.Sprintf("recording-%d", v.id), Request: req, Expected: v.expected})
	}
	runtimes := make([]*communicationRuntime, len(samples))
	for i := range samples {
		req := samples[i].Request
		vars := map[string]string{"tenant_id": fmt.Sprint(req.TenantID), "source_type": "recording", "source_id": fmt.Sprint(req.RecordingID), "source_version": req.RecordedAt, "scene_code": "communication.doctor.outpatient", "role_code": "doctor", "employee_name": req.EmployeeName, "department": "", "recorded_at": req.RecordedAt, "rules_json": string(rulesJSON), "transcript": req.CleanedTranscript}
		runtimes[i] = &communicationRuntime{Request: req, RuleSetCode: "communication.doctor.outpatient", RuleSetVersion: version, PromptCode: complianceDoctorPromptCode, PromptVersion: "v4-safety-20261011", OutputContract: contract, RulesJSON: string(rulesJSON), SystemPrompt: renderCommunicationTemplate(candidate.System, vars), UserPrompt: renderCommunicationTemplate(candidate.User, vars)}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	for i := range samples {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			request := buildCommunicationLLMRequest(samples[i].Request.TenantID, fmt.Sprint(samples[i].Request.RecordingID), "communication.doctor.outpatient", "communication.doctor.outpatient", version, "医生沟通", model, runtimes[i])
			request.CallerModule = "compliance.phase5.isolated-replay"
			out, _, raw, err := svc.llm.TextInferenceWithRaw(ctx, request)
			if err != nil {
				samples[i].Error = err.Error()
				return
			}
			samples[i].Raw = out
			runtimes[i].GatewayResponseJSON = string(raw)
			samples[i].Findings, err = parseFindingsForRuntime(out.Content, samples[i].Request.CleanedTranscript, ruleCodes(rules), contract)
			if err != nil {
				samples[i].Error = err.Error()
			} else if len(samples[i].Findings) != samples[i].Expected {
				samples[i].Error = fmt.Sprintf("expected %d, actual %d", samples[i].Expected, len(samples[i].Findings))
			}
		}(i)
	}
	wg.Wait()
	failed := false
	for _, sample := range samples {
		b, _ := json.MarshalIndent(sample, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, sample.Name+".json"), b, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s expected=%d actual=%d error=%s", sample.Name, sample.Expected, len(sample.Findings), sample.Error)
		failed = failed || sample.Error != ""
	}
	// Save metadata without secrets even on failures; no formatted/coerced output.
	b, _ = json.MarshalIndent(map[string]any{"provider": model.Provider, "model_code": model.ModelCode, "model_config_id": model.ID, "rules": ruleCodes(rules), "prompt_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(candidate.System+candidate.User))), "results": samples}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if failed {
		t.Fatal("real replay quality/contract failed; inspect external summary")
	}
	for i := range samples {
		if samples[i].Name == "recording-1706" {
			verifyPhase5IsolatedLifecycle(t, ctx, svc, model, runtimes[i], samples[i].Findings)
		}
	}
}

func verifyPhase5IsolatedLifecycle(t *testing.T, ctx context.Context, svc *Service, model *complianceModelConfig, runtime *communicationRuntime, findings []any) {
	t.Helper()
	req := runtime.Request
	fingerprint := communicationInputFingerprint(req)
	key := buildCommunicationExecutionKey(req, runtime, model.ID, fingerprint)
	resp, claimed, err := svc.claimCommunicationRun(ctx, req, runtime, model, fingerprint, key)
	if err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	resp.Findings = findings
	if err := svc.completeCommunicationRun(ctx, resp, runtime, fingerprint, key); err != nil {
		t.Fatal(err)
	}
	duplicate, claimed, err := svc.claimCommunicationRun(ctx, req, runtime, model, fingerprint, key)
	if err != nil || claimed || duplicate.RunID != resp.RunID {
		t.Fatalf("duplicate: %v %v", claimed, err)
	}
	items, err := svc.ListCheckItems(ctx, req.TenantID, "pending")
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	id := items[0].ID
	if _, err := svc.GetCheckItem(ctx, req.TenantID+1000, id); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("tenant detail isolation: %v", err)
	}
	if _, err := svc.ProcessCheckItem(ctx, req.TenantID+1000, id, req.EmployeeID, CheckItemProcessRequest{Conclusion: "insufficient_evidence", Reason: "隔离验证", Actions: []string{"回听"}, IdempotencyKey: "cross-tenant"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("tenant process isolation: %v", err)
	}
	overview, err := svc.GetOverview(ctx, req.TenantID)
	if err != nil || overview.PendingCount != 1 {
		t.Fatalf("overview: %+v %v", overview, err)
	}
	// An intentional rerun creates another execution without increasing items.
	rerun := req
	rerun.TriggerReason = "manual_rerun"
	rerun.RerunKey = "phase5-intentional-rerun"
	rerunKey := buildCommunicationExecutionKey(rerun, runtime, model.ID, fingerprint)
	second, claimed, err := svc.claimCommunicationRun(ctx, rerun, runtime, model, fingerprint, rerunKey)
	if err != nil || !claimed || second.RunID == resp.RunID {
		t.Fatal("rerun identity failed", err)
	}
	second.Findings = findings
	if err := svc.completeCommunicationRun(ctx, second, runtime, fingerprint, rerunKey); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := svc.claimCommunicationRun(ctx, rerun, runtime, model, fingerprint, rerunKey); err != nil || claimed {
		t.Fatal("rerun retry was not idempotent", err)
	}
	items, err = svc.ListCheckItems(ctx, req.TenantID, "")
	if err != nil || len(items) != 1 || items[0].ID != id {
		t.Fatal("rerun increased items", err)
	}
	var count int
	if err := svc.pool.QueryRow(ctx, `SELECT count(*) FROM compliance_check_item_sources WHERE item_id=$1`, id).Scan(&count); err != nil || count != 2 {
		t.Fatalf("source traceability %d %v", count, err)
	}
	for n := 0; n < 2; n++ {
		item, err := svc.StartCheckItem(ctx, req.TenantID, id, req.EmployeeID)
		if err != nil || item.Status != "in_progress" {
			t.Fatal("start", err)
		}
	}
	process := CheckItemProcessRequest{Conclusion: "insufficient_evidence", Reason: "隔离自动验证，不构成医院人工处理结论", Actions: []string{"人工回听并核对风险边界"}, IdempotencyKey: "phase5-process"}
	for n := 0; n < 2; n++ {
		item, err := svc.ProcessCheckItem(ctx, req.TenantID, id, req.EmployeeID, process)
		if err != nil || item.Status != "processed" || len(item.ProcessRecords) != 1 {
			t.Fatalf("process: %+v %v", item, err)
		}
	}
	overview, err = svc.GetOverview(ctx, req.TenantID)
	if err != nil || overview.PendingCount != 0 || overview.ReviewedCount != 1 || overview.ConfirmedCount != 0 {
		t.Fatalf("final overview: %+v %v", overview, err)
	}
	t.Log("isolated actual business persistence, source links, duplicate delivery, intentional rerun, tenant isolation, start/process retry and overview passed; temporary tables are discarded on disconnect")
}
