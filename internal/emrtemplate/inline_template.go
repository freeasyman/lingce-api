package emrtemplate

import (
	"strconv"
	"strings"
)

// InlineTemplateSegments 把「固定文字 + 全角括号槽位」的句式拆开。
// segments 长度为槽位数+1（每个槽位前后的固定文字），defaults 为每个括号的默认值。
// 例：主诉 → "双眼视力差（　）余年，近2年每年增加（<）0.5D，要求手术"
//
//	segments = ["双眼视力差", "余年，近2年每年增加", "0.5D，要求手术"]
//	defaults = ["　", "<"]
func InlineTemplateSegments(template string) (segments []string, defaults []string) {
	segments = []string{""}
	rest := template
	for {
		open := strings.Index(rest, "（")
		if open < 0 {
			break
		}
		afterOpen := rest[open+len("（"):]
		closeRel := strings.Index(afterOpen, "）")
		if closeRel < 0 {
			break
		}
		segments[len(segments)-1] += rest[:open]
		defaults = append(defaults, afterOpen[:closeRel])
		segments = append(segments, "")
		rest = afterOpen[closeRel+len("）"):]
	}
	segments[len(segments)-1] += rest
	return segments, defaults
}

// AssembleInlineTemplate 用槽位值拼回完整句式；槽位为空时回退到默认值。
// 固定文字始终来自模板，模型无法改写括号外内容。
func AssembleInlineTemplate(segments, defaults, slots []string) string {
	var builder strings.Builder
	for index, segment := range segments {
		builder.WriteString(segment)
		if index >= len(defaults) {
			continue
		}
		value := ""
		if index < len(slots) {
			value = strings.TrimSpace(slots[index])
		}
		if value == "" {
			// 选项型默认值（如 有/无、正常/肥厚/…）是候选集，不是已填值；
			// 留空时应保持空白，而不是把整串选项写进病历。
			if inlineTemplateChoiceDefault(defaults[index]) {
				value = ""
			} else {
				value = defaults[index]
			}
		}
		builder.WriteString(value)
	}
	return builder.String()
}

// InlineTemplateSlotCount 返回句式中的括号槽位数量。
func InlineTemplateSlotCount(template string) int {
	_, defaults := InlineTemplateSegments(template)
	return len(defaults)
}

// InlineTemplateSlotHints 为每个括号槽位推断填写要求，供生成提示词使用。
// 只依赖句式本身：默认值形态（数值/选项/符号）和紧随其后的单位文字，
// 因此新增模板不需要额外写槽位元数据。
func InlineTemplateSlotHints(template string) []string {
	segments, defaults := InlineTemplateSegments(template)
	hints := make([]string, len(defaults))
	for index, defaultValue := range defaults {
		value := strings.TrimSpace(defaultValue)
		next := ""
		if index+1 < len(segments) {
			next = strings.TrimSpace(segments[index+1])
		}
		unit := inlineTemplateLeadingUnit(next)
		switch {
		case value == "" && unit != "":
			hints[index] = "数值（单位：" + unit + "）"
		case value == "":
			hints[index] = "文本"
		case inlineTemplateNumeric(value):
			hints[index] = "数值" + inlineTemplateUnitHint(unit)
		case value == "/":
			hints[index] = "填写有/无/不详或具体描述"
		case value == "<" || value == "≤" || value == ">" || value == "±":
			hints[index] = "填写符号或数值（如 <、≤、>）"
		case strings.Contains(value, "/"):
			hints[index] = "从默认值中选择一项（" + value + "）"
		default:
			hints[index] = "按默认值格式填写（" + value + "）"
		}
	}
	return hints
}

func inlineTemplateNumeric(value string) bool {
	_, err := strconv.ParseFloat(value, 64)
	return err == nil
}

func inlineTemplateLeadingUnit(text string) string {
	for _, unit := range []string{"余年", "个月", "年", "周", "天", "岁", "mm", "cm", "kg", "次", "%"} {
		if strings.HasPrefix(text, unit) {
			return unit
		}
	}
	return ""
}

func inlineTemplateUnitHint(unit string) string {
	if unit == "" {
		return ""
	}
	return "（单位：" + unit + "）"
}

// inlineTemplateChoiceDefault 判断括号默认值是否是「选项集」（用 / 分隔），
// 单独的 "/" 是占位符，不属于选项集。
func inlineTemplateChoiceDefault(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "/" && strings.Contains(trimmed, "/")
}
