package patientops

import (
	"fmt"
	"strings"
)

// normalizeScreenshotResultMap 容忍模型把“数组字符串”写成对象（例如
// {"role":"家长","quote":"..."}）；把它压成可展示字符串后再做严格 schema 校验。
// 这不是放宽业务规则，而是避免因表达形式差异丢掉整次分析。
func normalizeScreenshotResultMap(raw map[string]any) {
	for _, key := range []string{"patient_utterances", "relationship_clues", "employee_commitments", "suggested_actions", "product_interest", "evidence"} {
		value, exists := raw[key]
		if !exists {
			continue
		}
		list, ok := value.([]any)
		if !ok {
			continue
		}
		normalized := make([]any, 0, len(list))
		seen := make(map[string]bool, len(list))
		for _, item := range list {
			text := stringifyScreenshotItem(item)
			if text == "" {
				continue
			}
			if key == "patient_utterances" && screenshotUIText(text) {
				continue
			}
			if key == "evidence" && strings.HasPrefix(text, "[患者]") && screenshotUIText(text) {
				text = strings.Replace(text, "[患者]", "[系统]", 1)
			}
			if seen[text] {
				continue
			}
			seen[text] = true
			normalized = append(normalized, text)
		}
		raw[key] = normalized
	}
	for _, key := range []string{"concern", "decision_maker", "patient_question"} {
		if value, ok := raw[key]; ok {
			if _, isString := value.(string); !isString {
				raw[key] = stringifyScreenshotItem(value)
			}
		}
	}
}

func screenshotUIText(text string) bool {
	for _, marker := range []string{"门店地址", "服务内容", "立即咨询", "价格表", "企业微信联系卡", "点击了"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func stringifyScreenshotItem(item any) string {
	switch value := item.(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]any:
		parts := make([]string, 0, 3)
		for _, key := range []string{"role", "speaker", "who"} {
			if text := strings.TrimSpace(fmt.Sprint(value[key])); text != "" && text != "<nil>" {
				parts = append(parts, "["+text+"]")
				break
			}
		}
		for _, key := range []string{"quote", "text", "content", "summary", "action", "name", "reason", "value"} {
			if text := strings.TrimSpace(fmt.Sprint(value[key])); text != "" && text != "<nil>" {
				parts = append(parts, text)
				break
			}
		}
		return strings.Join(parts, " ")
	case []any:
		parts := make([]string, 0, len(value))
		for _, child := range value {
			if text := stringifyScreenshotItem(child); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "；")
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}
