package dealexpert

import "testing"

func TestSanitizeCardTasks(t *testing.T) {
	card := map[string]any{
		"deal_status": "付款待治疗",
		"next_action": "确认今日手术及缴费",
		"follow_up_tasks": []any{
			map[string]any{"title": "确认今日手术及缴费", "timing": "今天下午手术后", "script": "手术顺利结束了，费用都结清了吧？", "description": "确认手术及尾款"},
			map[string]any{"title": "术后复查提醒", "timing": "术后一周", "script": "记得按时复查。", "description": "术后复查"},
		},
	}
	sanitizeCardTasks(card)
	tasks := card["follow_up_tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if card["next_action"] != "术后复查提醒" {
		t.Fatalf("unexpected next_action %v", card["next_action"])
	}
}
