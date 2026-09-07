package emrrecord

import (
	"context"
	"testing"
)

func TestCreateRequiresEncounter(t *testing.T) {
	service := &Service{}
	if _, err := service.Create(context.Background(), 1, 2, CreateRequest{TemplateVersionID: "template", VisitType: "初诊"}); err == nil {
		t.Fatal("expected encounter_id validation error")
	}
}

func TestDiffTopLevelCapturesContentChanges(t *testing.T) {
	before := map[string]any{"chief_complaint": "咳嗽", "past_history": "无"}
	after := map[string]any{"chief_complaint": "咳嗽5天", "present_illness": "3天"}

	changes := diffTopLevel(before, after)
	if len(changes) != 3 {
		t.Fatalf("changes = %#v, want one change", changes)
	}
	first, ok := changes[0].(map[string]any)
	if !ok || first["section_code"] != "chief_complaint" || first["change_type"] != "修改" || first["before"] != "咳嗽" || first["after"] != "咳嗽5天" {
		t.Fatalf("first change = %#v, want structured modification", first)
	}
	second, ok := changes[1].(map[string]any)
	if !ok || second["section_code"] != "past_history" || second["change_type"] != "删除" || second["before"] != "无" || second["after"] != nil {
		t.Fatalf("second change = %#v, want structured deletion", second)
	}
	third, ok := changes[2].(map[string]any)
	if !ok || third["section_code"] != "present_illness" || third["change_type"] != "新增" || third["before"] != nil || third["after"] != "3天" {
		t.Fatalf("third change = %#v, want structured addition", third)
	}
}
