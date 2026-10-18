package emrquality

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// evidenceList 兼容大模型返回的多形态 evidence。
//
// 正常形态是 [{"source":..,"path":..,"excerpt":..}]，但实测 qwen-max 偶尔会把
// 同一个数组再包一层 JSON 字符串返回，导致整个质检响应反序列化失败。这里把
// null / 数组 / JSON 字符串 / 单个对象 / 纯文本统一归一化为 []Evidence，避免
// 一条规则的格式抖动拖垮整单质检。
type evidenceList []Evidence

func (e *evidenceList) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*e = nil
		return nil
	}

	switch trimmed[0] {
	case '[':
		var items []Evidence
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return err
		}
		*e = items
		return nil
	case '{':
		var one Evidence
		if err := json.Unmarshal(trimmed, &one); err != nil {
			return err
		}
		*e = []Evidence{one}
		return nil
	case '"':
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			*e = nil
			return nil
		}
		// 字符串里仍是 JSON 数组或对象：继续解析。
		var items []Evidence
		if err := json.Unmarshal([]byte(text), &items); err == nil {
			*e = items
			return nil
		}
		var one Evidence
		if err := json.Unmarshal([]byte(text), &one); err == nil {
			*e = []Evidence{one}
			return nil
		}
		// 纯文本证据：整体作为摘要保留，供人工复核。
		*e = []Evidence{{Source: "llm", Excerpt: text}}
		return nil
	default:
		preview := string(trimmed)
		if len(preview) > 32 {
			preview = preview[:32]
		}
		return fmt.Errorf("unsupported evidence shape: %s", preview)
	}
}

// stringList 兼容大模型把字符串数组写成逗号分隔字符串的情况。
// 实测 qwen-max 会把 defect_keys 返回成 "原发性高血压, 头痛三天"。
type stringList []string

func (s *stringList) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*s = nil
		return nil
	}

	switch trimmed[0] {
	case '[':
		var items []string
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return err
		}
		*s = items
		return nil
	case '"':
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			*s = nil
			return nil
		}
		// 先按 JSON 数组解析（字符串里包数组）；失败再按逗号分隔。
		var items []string
		if err := json.Unmarshal([]byte(text), &items); err == nil {
			*s = items
			return nil
		}
		parts := strings.Split(text, ",")
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			if value := strings.TrimSpace(part); value != "" {
				values = append(values, value)
			}
		}
		*s = values
		return nil
	default:
		preview := string(trimmed)
		if len(preview) > 32 {
			preview = preview[:32]
		}
		return fmt.Errorf("unsupported string list shape: %s", preview)
	}
}
