package emrdiagnosis

import (
	"fmt"
	"strings"
)

// Display 从 EMR 病历 content_json 中提取用于 HIS 单据/打印的诊断文本。
//
// 诊断唯一来源是病历记录本身：
//  1. content_json.diagnosis（西医诊断，结构化对象）
//  2. content_json.diagnoses（兼容旧版数组/对象/文本）
//  3. content_json.tcm_diagnosis（中医诊断，仅在前两者缺失时回退）
//
// 输出优先使用「诊断名称（ICD-10：编码）」，保证病案里 ICD 改动会同步反映到
// 处方笺、检验/检查申请单与文书，而不是继续读取 HIS 表里的旧诊断文本。
func Display(content map[string]any) string {
	if content == nil {
		return ""
	}
	for _, key := range []string{"diagnosis", "diagnoses", "tcm_diagnosis"} {
		if value, ok := content[key]; ok {
			if text := displayValue(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func displayValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := displayValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "、")
	case []string:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "、")
	case map[string]any:
		name := firstString(typed, "diagnosis_name", "name", "diagnosis", "text")
		code := firstString(typed, "icd10_code", "icd_code", "code")
		if name != "" && code != "" {
			return fmt.Sprintf("%s（ICD-10：%s）", name, code)
		}
		if name != "" {
			return name
		}
		if code != "" {
			return "ICD-10：" + code
		}
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := object[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}
