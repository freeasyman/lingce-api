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
			text, ok := normalizeScreenshotArrayItem(item)
			if !ok {
				normalized = append(normalized, item)
				continue
			}
			if key == "patient_utterances" && isUIOnlyText(text) {
				continue
			}
			if key == "evidence" && isUIEvidence(text) {
				for _, prefix := range []string{"[患者]", "[用户]", "[家属]", "[家长]"} {
					if strings.HasPrefix(text, prefix) {
						text = strings.Replace(text, prefix, "[系统]", 1)
						break
					}
				}
			}
			if key == "relationship_clues" && onlyRoleText(text) {
				continue
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
				if normalized, ok := normalizeScreenshotArrayItem(value); ok {
					raw[key] = normalized
				}
			}
		}
	}
}

func onlyRoleText(text string) bool {
	trimmed := strings.TrimSpace(text)
	bareRoles := []string{"家长", "患者", "本人", "家属", "朋友圈作者", "咨询者", "员工", "无法判断", "客户"}
	for _, role := range bareRoles {
		if trimmed == role || trimmed == "["+role+"]" {
			return true
		}
	}
	return strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") && !strings.Contains(trimmed, "：") && !strings.Contains(trimmed, ":")
}

// isUIOnlyText 只识别“本身就是界面标签/单纯点击动作”的短文本。
// 带“打不开/失败/没反应/为什么”等后续内容的句子是患者在描述问题，必须保留。
func isUIOnlyText(text string) bool {
	trimmed := strings.TrimSpace(text)
	for _, prefix := range []string{"[患者]", "[用户]", "[家属]", "[家长]", "[系统]"} {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
	}
	labels := []string{"门店地址", "服务内容", "立即咨询", "价格表", "企业微信联系卡", "预约"}
	for _, label := range labels {
		if trimmed == label {
			return true
		}
	}
	if trimmed == "点击了" {
		return true
	}
	if strings.HasPrefix(trimmed, "点击了") {
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "点击了"))
		rest = strings.Trim(rest, "“”\"' ")
		for _, label := range labels {
			if rest == label {
				return true
			}
		}
	}
	return false
}

// isUIEvidence 只把“单纯点击动作”归为系统证据；不把“点击了价格表，打不开”改成系统。
func isUIEvidence(text string) bool {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "[患者]") && !strings.HasPrefix(trimmed, "[用户]") && !strings.HasPrefix(trimmed, "[家属]") && !strings.HasPrefix(trimmed, "[家长]") {
		return false
	}
	body := trimmed
	for _, prefix := range []string{"[患者]", "[用户]", "[家属]", "[家长]"} {
		body = strings.TrimSpace(strings.TrimPrefix(body, prefix))
	}
	return isUIOnlyText(body)
}

func normalizeScreenshotArrayItem(item any) (string, bool) {
	switch value := item.(type) {
	case string:
		return strings.TrimSpace(value), true
	case map[string]any:
		// 任何已知字段只要出现，就必须是字符串；混合类型直接交给 Schema 拒绝。
		for _, key := range []string{"role", "speaker", "who", "quote", "text", "content", "summary", "action", "name", "reason", "value"} {
			if raw, exists := value[key]; exists {
				if _, ok := raw.(string); !ok {
					return "", false
				}
			}
		}
		for _, key := range []string{"quote", "text", "content", "summary", "action", "name", "reason", "value"} {
			if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
				return stringifyScreenshotItem(value), true
			}
		}
		return "", false
	default:
		return "", false
	}
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
