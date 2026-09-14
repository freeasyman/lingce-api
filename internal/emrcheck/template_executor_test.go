package emrcheck

import "testing"

func TestEvaluateTemplateStructureRuleVisibleField(t *testing.T) {
	fields := []TemplateField{{Code: "present_illness", Name: "现病史", Visible: true}}

	ok, evidence, _, _ := EvaluateTemplateStructureRule(
		"common.visible_fields.present_illness", fields,
	)
	if !ok || evidence["exists"] != true || evidence["is_visible"] != true {
		t.Fatalf("visible field result = %#v, %#v", ok, evidence)
	}

	ok, evidence, _, _ = EvaluateTemplateStructureRule(
		"common.visible_fields.diagnosis", fields,
	)
	if ok || evidence["exists"] != false {
		t.Fatalf("missing field result = %#v, %#v", ok, evidence)
	}
}

func TestEvaluateTemplateStructureRuleRejectsHiddenField(t *testing.T) {
	ok, evidence, _, _ := EvaluateTemplateStructureRule(
		"common.visible_fields.prescription",
		[]TemplateField{{Code: "prescription", Name: "处方", Visible: false}},
	)
	if ok || evidence["is_visible"] != false {
		t.Fatalf("hidden field result = %#v, %#v", ok, evidence)
	}
}

func TestEvaluateTemplateStructureRuleOrdersAreSeparate(t *testing.T) {
	fields := []TemplateField{
		{Code: "prescription", Name: "处方", Visible: true},
		{Code: "disposition", Name: "处置", Visible: true},
	}
	ok, _, _, _ := EvaluateTemplateStructureRule("common.orders_split.exam", fields)
	if !ok {
		t.Fatal("expected separate prescription and disposition fields to pass")
	}

	ok, _, _, _ = EvaluateTemplateStructureRule("common.orders_split.exam",
		[]TemplateField{{Code: "prescription", Name: "处方", Visible: true}})
	if ok {
		t.Fatal("expected missing disposition field to fail")
	}
}
