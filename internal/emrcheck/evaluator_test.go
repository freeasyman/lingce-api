package emrcheck

import "testing"

func TestEvaluateProgramChiefComplaint(t *testing.T) {
	base := binding{Code: "emr.chief_complaint.required", ExecutionMode: "程序判断"}

	result := evaluateRequirement(base, checkContext{SnapshotID: "snapshot-1", Content: map[string]any{}}, "手动保存")
	if result.Conclusion != "不符合" || result.CheckStatus != "已完成" || result.HandlingResult != "待医生处理" {
		t.Fatalf("empty chief complaint result = %#v", result)
	}

	result = evaluateRequirement(base, checkContext{SnapshotID: "snapshot-1", Content: map[string]any{"chief_complaint": "咳嗽"}}, "手动保存")
	if result.Conclusion != "符合" || result.CheckStatus != "已完成" {
		t.Fatalf("filled chief complaint result = %#v", result)
	}
}

func TestEvaluateProgramChiefComplaintMaxLength(t *testing.T) {
	item := binding{Code: "emr.chief_complaint.max_length", ExecutionMode: "程序判断", DeadlineAction: "归档前处理"}
	content := map[string]any{"chief_complaint": "一二三四五六七八九十一二三四五六七八九十一"}
	result := evaluateRequirement(item, checkContext{SnapshotID: "snapshot-1", Content: content}, "归档")
	if result.Conclusion != "不符合" || result.HandlingResult != "阻断" {
		t.Fatalf("long chief complaint result = %#v", result)
	}
	if result.Evidence["length"] != 21 {
		t.Fatalf("length evidence = %#v, want 21", result.Evidence["length"])
	}
}

func TestEvaluateProgramChiefComplaintDurationRemainsUnimplemented(t *testing.T) {
	item := binding{Code: "emr.chief_complaint.duration", ExecutionMode: "程序判断"}
	result := evaluateRequirement(item, checkContext{SnapshotID: "snapshot-1", Content: map[string]any{"chief_complaint": "咳嗽3天"}}, "归档")
	if result.Conclusion != "无法判断" || result.CheckStatus != "未完成" {
		t.Fatalf("duration result = %#v", result)
	}
	if result.IncompleteReason != "程序规则未实现" {
		t.Fatalf("duration incomplete reason = %q", result.IncompleteReason)
	}
}

func TestEvaluateProgramAllergyDetails(t *testing.T) {
	item := binding{Code: "emr.allergy.required.drug_detail", ExecutionMode: "程序判断", DeadlineAction: "归档前处理"}
	result := evaluateRequirement(item, checkContext{SnapshotID: "snapshot-1", Content: map[string]any{
		"allergy_history": map[string]any{"status": "具体药物过敏"},
	}}, "归档")
	if result.Conclusion != "不符合" || result.HandlingResult != "阻断" {
		t.Fatalf("missing allergy detail result = %#v", result)
	}

	result = evaluateRequirement(item, checkContext{SnapshotID: "snapshot-1", Content: map[string]any{
		"allergy_history": map[string]any{"status": "具体药物过敏", "drug_name": "青霉素"},
	}}, "归档")
	if result.Conclusion != "符合" {
		t.Fatalf("complete allergy detail result = %#v", result)
	}
}
