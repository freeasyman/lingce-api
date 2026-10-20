package patientops

import (
	"strings"
	"testing"
)

func TestValidateJSONSchemaScreenshotContract(t *testing.T) {
	schema := `{"type":"object","required":["content_scene","patient_utterances"],"properties":{"content_scene":{"type":"string","enum":["聊天沟通","朋友圈动态","无法判断"]},"patient_utterances":{"type":"array","items":{"type":"string"}}}}`
	valid := map[string]any{"content_scene": "朋友圈动态", "patient_utterances": []any{}}
	if err := validateJSONSchema(schema, valid); err != nil {
		t.Fatalf("valid schema rejected: %v", err)
	}
	if err := validateJSONSchema(schema, map[string]any{"content_scene": "朋友圈动态"}); err == nil {
		t.Fatal("missing required field was accepted")
	}
	if err := validateJSONSchema(schema, map[string]any{"content_scene": "未知", "patient_utterances": []any{}}); err == nil {
		t.Fatal("invalid enum was accepted")
	}
	strict := `{"type":"object","additionalProperties":false,"required":["evidence"],"properties":{"evidence":{"type":"array","items":{"type":"string"}}}}`
	if err := validateJSONSchema(strict, map[string]any{"evidence": []any{"ok"}, "extra": "bad"}); err == nil {
		t.Fatal("additional property was accepted")
	}
	if err := validateJSONSchema(strict, map[string]any{"evidence": []any{123}}); err == nil {
		t.Fatal("numeric evidence was accepted")
	}
}

func TestNormalizeScreenshotResultMap(t *testing.T) {
	raw := map[string]any{
		"relationship_clues": []any{map[string]any{"role": "家长", "quote": "和孩子商量"}, "[家长]"},
		"patient_utterances": []any{"请把价格表发给我", "请把价格表发给我", "门店地址"},
		"evidence":           []any{"[患者] 请把价格表发给我", "[用户] 点击了门店地址", "[用户] 点击了门店地址"},
		"decision_maker":     map[string]any{"role": "本人", "quote": "我自己"},
	}
	normalizeScreenshotResultMap(raw)
	clues, _ := raw["relationship_clues"].([]any)
	if len(clues) != 1 || !strings.Contains(clues[0].(string), "家长") || !strings.Contains(clues[0].(string), "和孩子商量") {
		t.Fatalf("unexpected clues: %#v", clues)
	}
	if _, ok := raw["decision_maker"].(string); !ok {
		t.Fatalf("decision_maker was not normalized: %#v", raw["decision_maker"])
	}
	utterances, _ := raw["patient_utterances"].([]any)
	if len(utterances) != 1 || !strings.Contains(utterances[0].(string), "价格表") {
		t.Fatalf("real patient utterance was deleted: %#v", utterances)
	}
	evidence, _ := raw["evidence"].([]any)
	if len(evidence) != 2 || evidence[0].(string) != "[患者] 请把价格表发给我" || !strings.HasPrefix(evidence[1].(string), "[系统]") || len(clues) != 1 {
		t.Fatalf("evidence was not deduped/reclassified: %#v", evidence)
	}
}

func TestStripJSONCodeFenceForContinue(t *testing.T) {
	input := "```json\n{\"content\":\"下一步\",\"next_action\":\"联系\",\"next_contact_at\":\"明天\"}\n```"
	got := stripJSONCodeFence(input)
	if !strings.HasPrefix(got, "{") || !strings.Contains(got, "下一步") {
		t.Fatalf("unexpected cleaned JSON: %q", got)
	}
}

func TestNormalizeActionContactTime(t *testing.T) {
	if got := normalizeActionContactTime("下周三"); got != "" {
		t.Fatalf("free-form time should be cleared, got %q", got)
	}
	if got := normalizeActionContactTime("2026-10-21 10:00"); got != "2026-10-21 10:00" {
		t.Fatalf("absolute time changed: %q", got)
	}
	if got := normalizeActionContactTime("2026-10-21"); got != "2026-10-21" {
		t.Fatalf("absolute date changed: %q", got)
	}
}
