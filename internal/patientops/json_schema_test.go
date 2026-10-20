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
}

func TestNormalizeScreenshotResultMap(t *testing.T) {
	raw := map[string]any{
		"relationship_clues": []any{map[string]any{"role": "家长", "quote": "和孩子商量"}},
		"evidence":           []any{"患者：我想考虑一下"},
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
}

func TestStripJSONCodeFenceForContinue(t *testing.T) {
	input := "```json\n{\"content\":\"下一步\",\"next_action\":\"联系\",\"next_contact_at\":\"明天\"}\n```"
	got := stripJSONCodeFence(input)
	if !strings.HasPrefix(got, "{") || !strings.Contains(got, "下一步") {
		t.Fatalf("unexpected cleaned JSON: %q", got)
	}
}
