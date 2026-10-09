package emrrecord

import (
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrtemplate"
)

func TestParseAndValidateEMRResult(t *testing.T) {
	template := &emrtemplate.PublishedVersionDetail{
		Sections: []*emrtemplate.Section{
			{Code: "chief_complaint"},
			{Code: "diagnosis", InputType: "结构化内容", Structure: map[string]any{"items": []any{"diagnosis_name", "icd10_code", "basis"}}},
		},
	}

	t.Run("accepts complete fenced JSON", func(t *testing.T) {
		content, unresolved, err := parseAndValidateEMRResult("```json\n{\n  \"working_content\": {\"chief_complaint\": \"咳嗽\", \"diagnosis\": {\"diagnosis_name\": \"待明确\", \"icd10_code\": \"\", \"basis\": \"\"}},\n  \"unresolved_items\": [{\"field_code\": \"diagnosis\", \"reason\": \"诊断需要医生核对\"}]\n}\n```", template)
		if err != nil {
			t.Fatalf("parse result: %v", err)
		}
		if content["chief_complaint"] != "咳嗽" {
			t.Fatalf("unexpected chief complaint: %#v", content["chief_complaint"])
		}
		if len(unresolved) != 1 {
			t.Fatalf("unexpected unresolved item count: %d", len(unresolved))
		}
	})

	t.Run("rejects legacy diagnosis aliases", func(t *testing.T) {
		_, _, err := parseAndValidateEMRResult(`{"working_content":{"chief_complaint":"咳嗽","diagnosis":{"text":"咳嗽"}},"unresolved_items":[]}`, template)
		if err == nil || !strings.Contains(err.Error(), "不允许字段") {
			t.Fatalf("expected legacy diagnosis field rejection, got %v", err)
		}
	})

	t.Run("rejects missing template field", func(t *testing.T) {
		_, _, err := parseAndValidateEMRResult(`{"working_content":{"chief_complaint":"咳嗽"},"unresolved_items":[]}`, template)
		if err == nil || !strings.Contains(err.Error(), "diagnosis") {
			t.Fatalf("expected missing diagnosis error, got %v", err)
		}
	})

	t.Run("rejects unknown template field", func(t *testing.T) {
		_, _, err := parseAndValidateEMRResult(`{"working_content":{"chief_complaint":"咳嗽","diagnosis":{"diagnosis_name":"","icd10_code":"","basis":""},"unknown":"不允许"},"unresolved_items":[]}`, template)
		if err == nil || !strings.Contains(err.Error(), "模板之外") {
			t.Fatalf("expected unknown field error, got %v", err)
		}
	})

	t.Run("rejects string unresolved items", func(t *testing.T) {
		_, _, err := parseAndValidateEMRResult(`{"working_content":{"chief_complaint":"咳嗽","diagnosis":{"diagnosis_name":"","icd10_code":"","basis":""}},"unresolved_items":["诊断需要核对"]}`, template)
		if err == nil || !strings.Contains(err.Error(), "对象数组") {
			t.Fatalf("expected unresolved item shape error, got %v", err)
		}
	})

	t.Run("rejects unresolved item without reason", func(t *testing.T) {
		_, _, err := parseAndValidateEMRResult(`{"working_content":{"chief_complaint":"咳嗽","diagnosis":{"diagnosis_name":"","icd10_code":"","basis":""}},"unresolved_items":[{"field_code":"diagnosis"}]}`, template)
		if err == nil || !strings.Contains(err.Error(), "缺少 reason") {
			t.Fatalf("expected unresolved item reason error, got %v", err)
		}
	})

	t.Run("preserves structured physical exam object", func(t *testing.T) {
		template := &emrtemplate.PublishedVersionDetail{Sections: []*emrtemplate.Section{
			{Code: "physical_exam", InputType: "结构化内容", Structure: map[string]any{"vitals": []any{"temperature"}, "metrics": []any{"height"}}},
		}}
		content, _, err := parseAndValidateEMRResult(`{"working_content":{"physical_exam":{"temperature":"45","height":"160","text":"脉象提示气虚"}},"unresolved_items":[]}`, template)
		if err != nil {
			t.Fatalf("parse structured result: %v", err)
		}
		value, ok := content["physical_exam"].(map[string]any)
		if !ok || value["temperature"] != "45" || value["height"] != "160" || value["text"] != "脉象提示气虚" {
			t.Fatalf("unexpected structured physical exam: %#v", content["physical_exam"])
		}
	})

	t.Run("rejects unknown structured subfield", func(t *testing.T) {
		template := &emrtemplate.PublishedVersionDetail{Sections: []*emrtemplate.Section{
			{Code: "physical_exam", InputType: "结构化内容", Structure: map[string]any{"vitals": []any{"temperature"}}},
		}}
		_, _, err := parseAndValidateEMRResult(`{"working_content":{"physical_exam":{"temperature":"36.5","unknown":"x"}},"unresolved_items":[]}`, template)
		if err == nil || !strings.Contains(err.Error(), "未定义子字段") {
			t.Fatalf("expected unknown structured field error, got %v", err)
		}
	})

	t.Run("flattens grouped structured physical exam object", func(t *testing.T) {
		template := &emrtemplate.PublishedVersionDetail{Sections: []*emrtemplate.Section{
			{Code: "physical_exam", InputType: "结构化内容", Structure: map[string]any{
				"vitals":     []any{"temperature", "blood_pressure"},
				"metrics":    []any{"height", "weight", "bmi"},
				"text_items": []any{"general_exam", "positive_findings"},
			}},
		}}
		content, _, err := parseAndValidateEMRResult(`{"working_content":{"physical_exam":{"vitals":{"temperature":"36.5","blood_pressure":"120/80"},"metrics":{"height":"170"},"text_items":{"general_exam":"精神可"}}},"unresolved_items":[]}`, template)
		if err != nil {
			t.Fatalf("parse grouped structured result: %v", err)
		}
		value, ok := content["physical_exam"].(map[string]any)
		if !ok || value["temperature"] != "36.5" || value["blood_pressure"] != "120/80" || value["height"] != "170" || value["general_exam"] != "精神可" {
			t.Fatalf("unexpected flattened physical exam: %#v", content["physical_exam"])
		}
	})
}

func TestRenderPromptUsesGenerationRequirementsOnly(t *testing.T) {
	specialty := "中医"
	template := &emrtemplate.PublishedVersionDetail{
		Version: &emrtemplate.TemplateVersion{
			TemplateCode:    "tcm",
			Name:            "中医门诊病历",
			VersionNo:       "1.0",
			DocumentType:    "门诊病历",
			VisitType:       "通用",
			SpecialtyModule: &specialty,
		},
		Sections: []*emrtemplate.Section{
			{
				ID:                "field-id-should-not-appear",
				TemplateVersionID: "template-id-should-not-appear",
				Code:              "tcm_treatment_principle",
				Name:              "治法",
				InputType:         "长文本",
				DisplayOrder:      400,
				Description:       "tcm_treatment_principle：治法\n\n质量要求：\n\n- 记录医生明确提出的治疗原则。\n- 不得根据诊断自动生成治法。\n\n病历生成要求：\n\n- 只记录医生明确说出的治法。\n- 不根据辨证自动补写治疗原则。\n\n可执行检查类型：非空、语义、栏位关系\n\n本栏位相关的多栏位联合质量要求：\n\n### 中医联合要求\n\n判断方式：大模型语义判断",
			},
		},
	}
	prompt := renderPrompt("{{template}}\n---\n{{template_fields}}\n---\n{{patient_context}}\n---\n{{encounter_context}}\n---\n{{transcript}}", EmrWorkerRequest{
		TenantID:          5,
		RecordingID:       1708,
		EncounterID:       37,
		EmployeeID:        104,
		DoctorName:        "胡欣",
		CustomerID:        8831,
		CustomerName:      "患者",
		RecordedAt:        time.Date(2026, 5, 28, 13, 34, 30, 0, time.UTC).Format(time.RFC3339),
		CleanedTranscript: "医生：热就是胃热。",
	}, map[string]any{"medical_department_code": "tcm"}, template)

	mustContain := []string{
		"模板编码：tcm",
		"【tcm_treatment_principle】治法",
		"病历生成要求：",
		"只记录医生明确说出的治法",
		"医生：热就是胃热。",
	}
	for _, expected := range mustContain {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q\nprompt:\n%s", expected, prompt)
		}
	}

	structuredTemplate := &emrtemplate.PublishedVersionDetail{
		Version: template.Version,
		Sections: []*emrtemplate.Section{{
			Code: "physical_exam", Name: "体格检查", InputType: "结构化内容",
			Structure: map[string]any{"vitals": []any{"temperature", "blood_pressure"}, "metrics": []any{"height", "weight"}},
		}},
	}
	structuredPrompt := renderPrompt("{{template_fields}}", EmrWorkerRequest{}, map[string]any{}, structuredTemplate)
	if !strings.Contains(structuredPrompt, "结构化字段定义") || !strings.Contains(structuredPrompt, "temperature") {
		t.Fatalf("expected structured field definition in prompt: %s", structuredPrompt)
	}
	mustNotContain := []string{
		"field-id-should-not-appear",
		"template-id-should-not-appear",
		"DisplayOrder",
		"display_order",
		"质量要求",
		"可执行检查类型",
		"多栏位联合质量要求",
		"判断方式",
		"不得根据诊断自动生成治法",
	}
	for _, forbidden := range mustNotContain {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("expected prompt not to contain %q\nprompt:\n%s", forbidden, prompt)
		}
	}
}

func TestExtractGenerationRequirements(t *testing.T) {
	description := "主诉\n\n质量要求：\n- 不进入生成\n\n病历生成要求：\n\n- 第一条\n- 第二条\n\n可执行检查类型：非空\n\n本栏位相关的多栏位联合质量要求：\n不进入生成"
	got := extractGenerationRequirements(description)
	if got != "- 第一条\n- 第二条" {
		t.Fatalf("unexpected requirements: %q", got)
	}
	if extractGenerationRequirements("没有生成要求") != "" {
		t.Fatalf("expected empty requirement")
	}
}
