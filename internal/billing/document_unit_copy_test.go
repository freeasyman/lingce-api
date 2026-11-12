package billing

// 通知函额度单位文案的回归测试：小时类单位必须写清「是什么录音」。
//
// 这份期望值与前端 packages/product-ui/src/quota-product-units.ts 的产品单位表
// 保持一致；前端对应用例在 quota-product-units.test.ts。改文案时两边一起改。

import (
	"strings"
	"testing"
)

func TestDocumentUnitLabelsSpeakRecordingType(t *testing.T) {
	want := map[string]map[string]string{
		"yingke":  {"hour": "小时接待录音"},
		"kehu":    {"hour": "小时接诊录音", "shot": "张截图", "record": "次对话", "person": "人次"},
		"suifang": {"hour": "小时接诊录音"},
		"tingxie": {"record": "份病历", "round": "轮质控", "hour": "小时接诊录音"},
		"hegui":   {"hour": "小时沟通录音"},
		"huoke":   {"hour": "小时沟通录音", "prompt": "道题", "record": "份脚本", "shot": "张图文"},
	}
	for code, units := range want {
		for unit, label := range units {
			if got := documentUnitLabel(code, unit); got != label {
				t.Fatalf("documentUnitLabel(%q, %q) = %q, want %q", code, unit, got, label)
			}
		}
	}

	// 所有小时类单位都要带录音类型，不能再出现「小时随访生成 / 小时录音判别 /
	// 小时录音分析」这种客户看不懂的旧文案。
	for code, units := range want {
		label, ok := units["hour"]
		if !ok {
			continue
		}
		if !strings.HasPrefix(label, "小时") || !strings.HasSuffix(label, "录音") {
			t.Fatalf("%s 的 hour 文案应以「小时」开头、以「录音」结尾，实际 %q", code, label)
		}
		if strings.Contains(label, "随访生成") || strings.Contains(label, "判别") || strings.Contains(label, "分析") {
			t.Fatalf("%s 的 hour 文案不应再带处理动作，实际 %q", code, label)
		}
	}
}

func TestDocumentUnitLabelFallbacks(t *testing.T) {
	if got := documentUnitLabel("unknown", "hour"); got != "小时" {
		t.Fatalf("未知产品 hour 兜底 = %q, want 小时", got)
	}
	if got := documentUnitLabel("yingke", "unknown"); got != "unknown" {
		t.Fatalf("未知单位应原样返回，实际 %q", got)
	}
}
