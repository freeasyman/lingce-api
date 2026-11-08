package recording

import "testing"

func TestIsUnsafeFollowUpTask(t *testing.T) {
	cases := []struct {
		name   string
		task   map[string]interface{}
		status string
		unsafe bool
	}{
		{
			name: "术后追缴费",
			task: map[string]interface{}{
				"title": "确认今日手术及缴费", "timing": "今天下午手术后",
				"script": "孩子下午的手术顺利结束了，费用都结清了吧？", "description": "确认手术及尾款",
			},
			status: "付款待治疗", unsafe: true,
		},
		{
			name: "术前正常缴费任务",
			task: map[string]interface{}{
				"title": "引导前台核对费用", "timing": "手术前",
				"script": "咱们先去前台把费用和术前手续办一下，下午就能安排。", "description": "术前缴费",
			},
			status: "付款待治疗", unsafe: false,
		},
		{
			name: "术后复查不追费",
			task: map[string]interface{}{
				"title": "术后复查提醒", "timing": "术后一周",
				"script": "记得按时复查，有干涩随时说。", "description": "术后复查",
			},
			status: "付款待治疗", unsafe: false,
		},
		{
			name: "还没成交就写术后复查",
			task: map[string]interface{}{
				"title": "补发术后复查提醒", "timing": "手术后次日早晨",
				"script": "孩子昨天手术辛苦了，今天记得来复查。", "description": "术后复查",
			},
			status: "意向明确未成交", unsafe: true,
		},
		{
			name: "刚预约就写手术结束",
			task: map[string]interface{}{
				"title": "术后回访", "timing": "术后",
				"script": "今天手术顺利结束了，感觉怎么样？", "description": "术后回访",
			},
			status: "已预约", unsafe: true,
		},
	}
	for _, tc := range cases {
		if got := isUnsafeFollowUpTask(tc.task, tc.status); got != tc.unsafe {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.unsafe)
		}
	}
}
