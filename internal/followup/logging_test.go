package followup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

func TestOptionalFollowupLogs(t *testing.T) {
	s := &Service{}
	s.configureLogDirectory(" ")
	if s.debugLogPath != "" || s.generatedTasksPath != "" {
		t.Fatal("logs must be disabled without an explicit directory")
	}
	if err := s.writeLLMLog(GenerateRequest{}, "", "", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.writeGeneratedTasksLog(GenerateRequest{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	s.configureLogDirectory(t.TempDir())
	if err := s.writeLLMLog(GenerateRequest{}, "", "", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.debugLogPath); err != nil {
		t.Fatal(err)
	}
}

func TestSavedTasksSurviveLogFailure(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `
	ALTER TABLE employees ADD COLUMN is_active boolean DEFAULT true;
	ALTER TABLE customers ADD COLUMN phone text, ADD COLUMN age integer, ADD COLUMN gender text,
	 ADD COLUMN extra_data jsonb, ADD COLUMN name text;
	UPDATE customers SET name='匿名患者' WHERE id=30;
	CREATE TABLE employee_partnerships(id bigint, tenant_id bigint, primary_employee_id bigint,
	 partner_employee_id bigint, is_active boolean, is_primary boolean);
	CREATE TABLE recording_tasks (
	 id bigserial PRIMARY KEY, tenant_id bigint, recording_id bigint, customer_id bigint,
	 customer_name text, customer_phone text, title text, description text, script text, status text,
	 priority text, due_at timestamptz, source_type text, source_detail text, contact_reason text,
	 encounter_id bigint, purpose text, background text, evidence text, contact_method text, doctor_name text,
	 assigned_to integer, assigned_by text, editable_fields jsonb, generation_source text,
	 generation_prompt_version text, generation_model text, raw_generation_payload jsonb,
	 phone_script text, wechat_content text, task_category text, patient_age integer, patient_gender text,
	 anonymous_patient boolean, time_basis text
	);`)
	if err != nil {
		t.Fatal(err)
	}
	// A file used as the parent directory deterministically fails on every OS, including root.
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.generatedTasksPath = filepath.Join(blocked, "result.log")
	response := &llmgateway.TextInferenceResponse{Content: `{"tasks":[{"title":"了解用药困难","category":"understanding","time_basis":"hospital_default","contact_time":"2026-10-09T10:00:00+08:00","purpose":"了解困难","evidence":"患者说不知道怎么使用","phone_script":"您好，请问使用时还有什么困难？"}]}`}
	count, payload, err := s.saveGeneratedTasks(ctx, testRequest(), response, "test")
	if err != nil || count != 1 || !strings.Contains(string(payload), "hospital_default") {
		t.Fatalf("task result changed after log failure: count=%d payload=%s err=%v", count, payload, err)
	}
	var persisted int
	var anonymous bool
	var assignee *int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM recording_tasks WHERE tenant_id=1 AND recording_id=60`).Scan(&persisted); err != nil || persisted != 1 {
		t.Fatalf("task not committed: count=%d err=%v", persisted, err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT anonymous_patient,assigned_to FROM recording_tasks WHERE tenant_id=1 AND recording_id=60`).Scan(&anonymous, &assignee); err != nil || !anonymous || assignee != nil {
		t.Fatalf("incomplete patient routing changed: anonymous=%v assignee=%v err=%v", anonymous, assignee, err)
	}
}
