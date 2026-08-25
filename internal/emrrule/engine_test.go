package emrrule

import "testing"

func TestEngineEvaluateChiefComplaintRequiredAndLength(t *testing.T) {
	engine := NewEngine()
	rule := RuleDefinition{ID: "r1", Code: "emr.chief_complaint.norm", Name: "主诉规范性", SuggestedScript: "请补充主诉"}
	record := &RecordInput{ID: 1, DocumentJSON: JSONMap{"sections": map[string]any{"chief_complaint": ""}}}

	hits := engine.Evaluate(record, []RuleDefinition{rule}, "save")
	if len(hits) != 1 {
		t.Fatalf("expected one hit, got %d", len(hits))
	}
	if hits[0].Summary != "主诉不能为空" {
		t.Fatalf("unexpected summary: %s", hits[0].Summary)
	}

	record.ChiefComplaint = "发热伴咳嗽咽痛流涕全身酸痛乏力食欲下降三天"
	hits = engine.Evaluate(record, []RuleDefinition{rule}, "save")
	if len(hits) != 1 || hits[0].Summary != "主诉超过20字" {
		t.Fatalf("expected length hit, got %#v", hits)
	}
}

func TestEngineEvaluateAllergyArchiveBlocking(t *testing.T) {
	engine := NewEngine()
	rule := RuleDefinition{ID: "r1", Code: "emr.allergy.required", Name: "过敏史必须确认"}
	record := &RecordInput{ID: 1, PatientSnapshot: JSONMap{"allergy_history": "待确认"}, DocumentJSON: JSONMap{}}

	hits := engine.Evaluate(record, []RuleDefinition{rule}, "pre_archive")
	if len(hits) != 1 {
		t.Fatalf("expected one hit, got %d", len(hits))
	}
	if hits[0].Severity != "blocking" {
		t.Fatalf("expected blocking severity, got %s", hits[0].Severity)
	}
	if blockArchive, _ := hits[0].ActionPolicy["block_archive"].(bool); !blockArchive {
		t.Fatalf("expected block_archive action policy")
	}
}

func TestEngineSkipsPediatricsRulesForGeneralTemplate(t *testing.T) {
	engine := NewEngine()
	rule := RuleDefinition{ID: "r1", Code: "emr.pediatrics.guardian_info", Name: "监护人信息完整性"}
	record := &RecordInput{ID: 1, TemplateCode: "general", TemplateName: "标准全科门诊病历", DocumentJSON: JSONMap{}}

	hits := engine.Evaluate(record, []RuleDefinition{rule}, "save")
	if len(hits) != 0 {
		t.Fatalf("expected no hits, got %d", len(hits))
	}
}

func TestEngineEvaluateGynecologyRequiredFields(t *testing.T) {
	engine := NewEngine()
	rules := []RuleDefinition{
		{ID: "r1", Code: "emr.gynecology.menstrual_history", Name: "适龄女性月经史"},
		{ID: "r2", Code: "emr.gynecology.exam_required", Name: "妇科查体确认"},
	}
	record := &RecordInput{ID: 1, TemplateCode: "gynecology", TemplateName: "妇科门诊病历", DocumentJSON: JSONMap{"sections": map[string]any{}}}

	hits := engine.Evaluate(record, rules, "pre_submit")
	if len(hits) != 2 {
		t.Fatalf("expected two gynecology hits, got %d", len(hits))
	}
}

func TestEngineEvaluateTCMWesternDiagnosisPrompt(t *testing.T) {
	engine := NewEngine()
	rule := RuleDefinition{ID: "r1", Code: "emr.tcm.western_diagnosis_prompt", Name: "中医病历西医诊断提示"}
	record := &RecordInput{ID: 1, TemplateCode: "tcm", TemplateName: "中医门诊病历", DocumentJSON: JSONMap{"sections": map[string]any{"tcm_diagnosis": "风寒感冒"}}}

	hits := engine.Evaluate(record, []RuleDefinition{rule}, "save")
	if len(hits) != 1 || hits[0].Severity != "notice" {
		t.Fatalf("expected notice prompt, got %#v", hits)
	}
}

func TestEngineEvaluatePediatricsFeverPeak(t *testing.T) {
	engine := NewEngine()
	rule := RuleDefinition{ID: "r1", Code: "emr.pediatrics.fever_peak", Name: "儿童发热峰值记录"}
	record := &RecordInput{ID: 1, TemplateCode: "pediatrics", TemplateName: "儿科门诊病历", ChiefComplaint: "发热2天", DocumentJSON: JSONMap{"sections": map[string]any{"chief_complaint": "发热2天"}, "structured": map[string]any{"temperature": "39℃"}}}

	hits := engine.Evaluate(record, []RuleDefinition{rule}, "save")
	if len(hits) != 1 {
		t.Fatalf("expected fever detail hit, got %d", len(hits))
	}
	if hits[0].TargetPath != "extensions.pediatrics_fever" {
		t.Fatalf("unexpected target path: %s", hits[0].TargetPath)
	}
}

func TestEngineEvaluateAllergyPrescriptionConflict(t *testing.T) {
	engine := NewEngine()
	rule := RuleDefinition{ID: "r1", Code: "emr.allergy.prescription_risk", Name: "过敏史与处方风险一致性"}
	record := &RecordInput{
		ID:              1,
		PatientSnapshot: JSONMap{"allergy_history": "青霉素过敏"},
		DocumentJSON:    JSONMap{"sections": map[string]any{"prescription": "阿莫西林胶囊"}},
	}

	hits := engine.Evaluate(record, []RuleDefinition{rule}, "pre_submit")
	if len(hits) != 1 {
		t.Fatalf("expected allergy prescription conflict hit, got %d", len(hits))
	}
	if hits[0].ExecutorType != "structured" || hits[0].Severity != "blocking" {
		t.Fatalf("unexpected hit: %#v", hits[0])
	}
}

func TestEngineEvaluateAuxiliaryExamClosedLoop(t *testing.T) {
	engine := NewEngine()
	rule := RuleDefinition{ID: "r1", Code: "emr.auxiliary_exam.closed_loop", Name: "辅助检查结果闭环"}
	record := &RecordInput{
		ID:           1,
		DocumentJSON: JSONMap{"structured": map[string]any{"exam_orders": "血常规"}},
	}

	hits := engine.Evaluate(record, []RuleDefinition{rule}, "pre_archive")
	if len(hits) != 1 {
		t.Fatalf("expected exam closed-loop hit, got %d", len(hits))
	}
	if hits[0].Summary != "辅助检查结果未闭环" {
		t.Fatalf("unexpected summary: %s", hits[0].Summary)
	}
}

func TestEngineEvaluateDentalInformedConsent(t *testing.T) {
	engine := NewEngine()
	rule := RuleDefinition{ID: "r1", Code: "emr.dental.informed_consent", Name: "口腔操作知情告知"}
	record := &RecordInput{
		ID:           1,
		TemplateCode: "dental",
		TemplateName: "口腔门诊病历",
		DocumentJSON: JSONMap{"sections": map[string]any{"treatment_plan": "拔牙"}},
	}

	hits := engine.Evaluate(record, []RuleDefinition{rule}, "pre_archive")
	if len(hits) != 1 {
		t.Fatalf("expected informed consent hit, got %d", len(hits))
	}
	if hits[0].ExecutorType != "structured" {
		t.Fatalf("expected structured executor, got %s", hits[0].ExecutorType)
	}
}

func TestEngineEvaluateFollowUpComplete(t *testing.T) {
	engine := NewEngine()
	rule := RuleDefinition{ID: "r1", Code: "emr.followup.complete", Name: "复诊安排完整性"}
	record := &RecordInput{
		ID:               1,
		DiagnosisSummary: "高血压",
		DocumentJSON:     JSONMap{"sections": map[string]any{"diagnosis": "高血压"}},
	}

	hits := engine.Evaluate(record, []RuleDefinition{rule}, "pre_submit")
	if len(hits) != 1 {
		t.Fatalf("expected follow-up hit, got %d", len(hits))
	}
	if hits[0].TargetPath != "sections.follow_up" {
		t.Fatalf("unexpected target path: %s", hits[0].TargetPath)
	}

	record.DocumentJSON = JSONMap{"sections": map[string]any{"diagnosis": "高血压", "follow_up": "一周后复诊"}}
	hits = engine.Evaluate(record, []RuleDefinition{rule}, "pre_submit")
	if len(hits) != 0 {
		t.Fatalf("expected no follow-up hit, got %d", len(hits))
	}
}
