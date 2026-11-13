package his

import (
	"strings"
	"testing"
)

func testDocumentSource() *documentSource {
	return &documentSource{
		EncounterID:      100,
		CustomerID:       200,
		Customer:         CustomerSummary{Name: "张三", Gender: "female", Age: 36, Phone: "13800000000", Allergy: "无"},
		DoctorName:       "李医生",
		TenantName:       "灵策门诊",
		Diagnosis:        "宫颈炎性疾病",
		Advice:           "注意休息，一周后复诊。",
		PrescriptionRows: []string{"甲硝唑片 · 0.2g*21片 · 0.4g bid 3 天 · 1 盒"},
		TreatmentNames:   []string{"红光治疗"},
		TreatmentRows:    []string{"红光治疗：计划 2 次，已完成 0 次"},
	}
}

func TestRenderDocumentHTMLEscapesEditedFields(t *testing.T) {
	tpl := DocumentTemplate{
		Code: "referral",
		Name: "转诊单",
		BodyTemplate: `<p>姓名：{{patient_name}}</p><p>转诊原因：{{referral_reason}}</p>
<p>{{tenant_name}}</p>`,
		Fields: []DocumentTemplateField{{Key: "referral_reason", Label: "转诊原因", Type: "textarea", Required: true}},
	}
	values, err := buildDocumentValues(tpl, testDocumentSource(), map[string]string{
		"referral_reason": `<script>alert(1)</script>`,
	})
	if err != nil {
		t.Fatalf("build values: %v", err)
	}
	html := renderDocumentHTML(tpl, testDocumentSource(), values)
	if strings.Contains(html, "<script>") {
		t.Fatalf("expected edited value escaped, got %s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("expected escaped script tag, got %s", html)
	}
	if !strings.Contains(html, "张三") || !strings.Contains(html, "转诊单") || !strings.Contains(html, "灵策门诊") {
		t.Fatalf("expected patient/title/tenant in output, got %s", html)
	}
	if strings.Contains(html, "{{") {
		t.Fatalf("unresolved placeholder in output: %s", html)
	}
}

func TestBuildDocumentValuesSickLeaveDates(t *testing.T) {
	tpl := DocumentTemplate{
		Code:         "sick_leave",
		Name:         "病假条",
		BodyTemplate: `<p>休息 {{sick_leave_days}} 天（{{start_date}} 至 {{end_date}}）</p>`,
		Fields: []DocumentTemplateField{
			{Key: "sick_leave_days", Label: "休息天数", Type: "number", Required: true, Default: "3"},
			{Key: "start_date", Label: "开始日期", Type: "date", Required: true, Default: "{{date}}"},
			{Key: "end_date", Label: "结束日期", Type: "date", Required: true, Default: "{{end_date}}"},
		},
	}
	values, err := buildDocumentValues(tpl, testDocumentSource(), map[string]string{
		"sick_leave_days": "5",
		"start_date":      "2026-11-14",
	})
	if err != nil {
		t.Fatalf("build values: %v", err)
	}
	if got := values["end_date"]; got != "2026-11-18" {
		t.Fatalf("end date = %s, want 2026-11-18", got)
	}
	if got := values["sick_leave_days"]; got != "5" {
		t.Fatalf("days = %s, want 5", got)
	}
}

func TestBuildDocumentValuesDefaultsAndValidation(t *testing.T) {
	tpl := DocumentTemplate{
		Code:         "diagnosis_certificate",
		Name:         "诊断证明书",
		BodyTemplate: `<p>诊断：{{diagnosis}}</p><p>建议：{{advice}}</p>`,
		Fields: []DocumentTemplateField{
			{Key: "advice", Label: "建议", Type: "textarea", Required: true, Default: "{{advice}}"},
		},
	}
	values, err := buildDocumentValues(tpl, testDocumentSource(), nil)
	if err != nil {
		t.Fatalf("build values: %v", err)
	}
	if values["diagnosis"] != "宫颈炎性疾病" {
		t.Fatalf("diagnosis = %s", values["diagnosis"])
	}
	if values["advice"] != "注意休息，一周后复诊。" {
		t.Fatalf("advice = %s", values["advice"])
	}
	if values["gender"] != "女" {
		t.Fatalf("gender = %s, want 女", values["gender"])
	}

	required := DocumentTemplate{
		Code: "gyn_consent",
		Name: "妇科专项知情同意书",
		Fields: []DocumentTemplateField{
			{Key: "treatment_purpose", Label: "治疗目的", Type: "textarea", Required: true},
		},
	}
	if _, err := buildDocumentValues(required, testDocumentSource(), nil); err == nil {
		t.Fatal("expected required field validation error")
	}
}

func TestBuildDocumentValuesRejectsUnknownField(t *testing.T) {
	tpl := DocumentTemplate{Code: "referral", Name: "转诊单"}
	if _, err := buildDocumentValues(tpl, testDocumentSource(), map[string]string{"unknown": "x"}); err != ErrInvalidRequest {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestDocumentPlaceholderNoLeak(t *testing.T) {
	tpl := DocumentTemplate{
		Code:         "infusion_notice",
		Name:         "静脉输液知情同意书",
		BodyTemplate: `<p>姓名：{{patient_name}}</p><p>{{missing_key}}</p>`,
	}
	values, err := buildDocumentValues(tpl, testDocumentSource(), nil)
	if err != nil {
		t.Fatalf("build values: %v", err)
	}
	html := renderDocumentHTML(tpl, testDocumentSource(), values)
	if strings.Contains(html, "{{") {
		t.Fatalf("unresolved placeholder: %s", html)
	}
	if !strings.Contains(html, "无") {
		t.Fatalf("missing placeholder should render 无: %s", html)
	}
}
