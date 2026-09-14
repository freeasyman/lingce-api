package emrcheck

import (
	"testing"
	"time"
)

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
	if result.Conclusion != "无法完成" || result.CheckStatus != "未完成" {
		t.Fatalf("duration result = %#v", result)
	}
	if result.ActualMode != "程序判断" || result.ExecutionStatus != "executor_not_implemented" || result.HandlingResult != "系统配置缺失" {
		t.Fatalf("duration handling result = %#v", result)
	}
	if result.IncompleteReason != "程序执行器未实现" {
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

func TestEvaluateBasicInfoUsesPatientContext(t *testing.T) {
	data := checkContext{
		SnapshotID: "snapshot-1",
		PatientContext: map[string]any{
			"name":   "张三",
			"gender": "男",
			"age":    30,
		},
		EncounterContext: map[string]any{
			"medical_department_code": "internal_medicine",
			"doctor_name":             "李医生",
		},
		RecordContext: map[string]any{"started_at": "2026-09-14T10:00:00+08:00"},
	}

	for _, code := range []string{
		"emr.patient.basic_info.name",
		"emr.patient.basic_info.gender",
		"emr.patient.basic_info.birth_or_age",
		"emr.patient.basic_info.department",
		"emr.patient.basic_info.attending_doctor",
		"emr.patient.basic_info.encounter_time",
	} {
		result := evaluateRequirement(binding{Code: code, ExecutionMode: "程序判断"}, data, "手动保存")
		if result.Conclusion != "符合" {
			t.Fatalf("%s result = %#v", code, result)
		}
	}
}

func TestEvaluateBasicInfoPendingConfirmation(t *testing.T) {
	result := evaluateRequirement(binding{
		Code: "emr.patient.basic_info.pending_confirmation", ExecutionMode: "程序判断",
	}, checkContext{
		SnapshotID:     "snapshot-1",
		PatientContext: map[string]any{"pending_confirmation": true},
	}, "手动保存")
	if result.Conclusion != "不符合" || result.CheckStatus != "已完成" {
		t.Fatalf("pending confirmation result = %#v", result)
	}
}

func TestEvaluateTimeConsistency(t *testing.T) {
	created := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	started := created.Add(time.Hour)
	ended := started.Add(30 * time.Minute)
	saved := ended.Add(time.Minute)
	data := checkContext{
		SnapshotID:  "snapshot-1",
		Status:      "草稿",
		CreatedAt:   &created,
		StartedAt:   &started,
		EndedAt:     &ended,
		LastSavedAt: &saved,
		UpdatedAt:   &saved,
		RecordContext: map[string]any{
			"abnormal_span_seconds": float64(7200),
		},
	}

	for _, code := range []string{
		"emr.time.consistency.fields_present",
		"emr.time.consistency.order",
		"emr.time.consistency.last_modified",
		"emr.time.consistency.abnormal_span",
	} {
		result := evaluateRequirement(binding{Code: code, ExecutionMode: "程序判断"}, data, "手动保存")
		if result.Conclusion != "符合" {
			t.Fatalf("%s result = %#v", code, result)
		}
	}

	data.RecordContext = map[string]any{}
	result := evaluateRequirement(binding{
		Code: "emr.time.consistency.abnormal_span", ExecutionMode: "程序判断",
	}, data, "手动保存")
	if result.Conclusion != "无法判断" || result.CheckStatus != "未完成" {
		t.Fatalf("missing threshold result = %#v", result)
	}
}

func TestEvaluateTimeConsistencyRejectsInvalidOrder(t *testing.T) {
	created := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	started := created.Add(-time.Minute)
	result := evaluateRequirement(binding{
		Code: "emr.time.consistency.order", ExecutionMode: "程序判断",
	}, checkContext{
		SnapshotID: "snapshot-1", CreatedAt: &created, StartedAt: &started,
	}, "手动保存")
	if result.Conclusion != "不符合" {
		t.Fatalf("invalid order result = %#v", result)
	}
}

func TestEvaluateRecordCompleteness(t *testing.T) {
	t.Run("vital signs", func(t *testing.T) {
		item := binding{Code: "emr.physical_exam.vital_signs", ExecutionMode: "程序判断"}
		result := evaluateRequirement(item, checkContext{
			SnapshotID: "snapshot-1",
			Content: map[string]any{
				"physical_exam": map[string]any{
					"vitals": map[string]any{"temperature_c": 36.6},
				},
			},
		}, "手动保存")
		if result.Conclusion != "符合" {
			t.Fatalf("vital signs result = %#v", result)
		}

		result = evaluateRequirement(item, checkContext{
			SnapshotID: "snapshot-1",
			Content: map[string]any{
				"physical_exam": map[string]any{
					"vitals":              map[string]any{},
					"not_measured_reason": "患者拒绝测量",
				},
			},
		}, "手动保存")
		if result.Conclusion != "符合" {
			t.Fatalf("not measured reason result = %#v", result)
		}

		result = evaluateRequirement(item, checkContext{
			SnapshotID: "snapshot-1",
			Content:    map[string]any{"physical_exam": map[string]any{"vitals": map[string]any{}}},
		}, "手动保存")
		if result.Conclusion != "不符合" {
			t.Fatalf("missing vital signs result = %#v", result)
		}
	})

	t.Run("auxiliary exam", func(t *testing.T) {
		item := binding{Code: "emr.auxiliary_exam.result_recorded", ExecutionMode: "程序判断"}
		data := checkContext{
			SnapshotID: "snapshot-1",
			Content:    map[string]any{"auxiliary_exam": "血常规：白细胞正常"},
			EncounterContext: map[string]any{
				"auxiliary_exams": []any{
					map[string]any{"item_name": "血常规", "result_status": "completed", "result_text": "白细胞正常"},
				},
			},
		}
		result := evaluateRequirement(item, data, "手动保存")
		if result.Conclusion != "符合" {
			t.Fatalf("auxiliary result = %#v", result)
		}

		data.Content = map[string]any{"auxiliary_exam": ""}
		result = evaluateRequirement(item, data, "手动保存")
		if result.Conclusion != "不符合" {
			t.Fatalf("missing auxiliary result = %#v", result)
		}

		data.EncounterContext = map[string]any{}
		result = evaluateRequirement(item, data, "手动保存")
		if result.Conclusion != "符合" {
			t.Fatalf("without external exam object result = %#v", result)
		}
	})

	t.Run("pending diagnosis requires plan", func(t *testing.T) {
		item := binding{Code: "emr.diagnosis.followup_plan", ExecutionMode: "程序判断"}
		data := checkContext{
			SnapshotID: "snapshot-1",
			Content: map[string]any{
				"diagnosis": map[string]any{"status": "待查", "name": "肺部阴影"},
			},
		}
		result := evaluateRequirement(item, data, "手动保存")
		if result.Conclusion != "不符合" {
			t.Fatalf("missing diagnosis plan = %#v", result)
		}

		data.Content["medical_advice"] = "完善胸部CT检查后复诊"
		result = evaluateRequirement(item, data, "手动保存")
		if result.Conclusion != "符合" {
			t.Fatalf("diagnosis plan = %#v", result)
		}
	})

	t.Run("treatment records", func(t *testing.T) {
		item := binding{Code: "emr.treatment.consistency.recorded", ExecutionMode: "程序判断"}
		data := checkContext{
			SnapshotID: "snapshot-1",
			Content:    map[string]any{"prescription": "阿莫西林，口服"},
			EncounterContext: map[string]any{
				"prescriptions": []any{
					map[string]any{"kind": "prescription", "drug_name": "阿莫西林"},
				},
			},
		}
		result := evaluateRequirement(item, data, "手动保存")
		if result.Conclusion != "符合" {
			t.Fatalf("treatment record = %#v", result)
		}

		data.Content = map[string]any{}
		result = evaluateRequirement(item, data, "手动保存")
		if result.Conclusion != "不符合" {
			t.Fatalf("missing treatment record = %#v", result)
		}
	})

	t.Run("supervisor signature", func(t *testing.T) {
		item := binding{Code: "emr.signature.supervisor", ExecutionMode: "程序判断"}
		data := checkContext{
			SnapshotID:    "snapshot-1",
			RecordContext: map[string]any{"supervisor_required": true},
		}
		result := evaluateRequirement(item, data, "归档")
		if result.Conclusion != "不符合" {
			t.Fatalf("missing supervisor signature = %#v", result)
		}

		data.RecordContext["supervisor_id"] = int64(42)
		result = evaluateRequirement(item, data, "归档")
		if result.Conclusion != "符合" {
			t.Fatalf("supervisor signature = %#v", result)
		}
	})
}
