package emrdiagnosis

import (
	"fmt"
	"strings"
)

// Validate enforces the single write-time diagnosis contract. Drafts may omit
// a diagnosis, but a diagnosis that is present must always be its structured
// object; the two diagnoses are never interchangeable.
func Validate(templateCode string, content map[string]any, complete bool) error {
	if content == nil {
		return fmt.Errorf("病历内容不能为空")
	}
	for _, key := range []string{"diagnosis_code", "icd10_code"} {
		if _, ok := content[key]; ok {
			return fmt.Errorf("诊断旧字段 %s 不允许使用", key)
		}
	}
	western, err := object(content, "diagnosis", false)
	if err != nil {
		return err
	}
	tcm, err := object(content, "tcm_diagnosis", true)
	if err != nil {
		return err
	}
	if strings.TrimSpace(templateCode) == "tcm" {
		// 中医病历以中医诊断为主；西医诊断仅作鼓励项，在质检中提醒，不阻断完成。
		if complete && name(tcm) == "" {
			return fmt.Errorf("请填写中医诊断")
		}
	} else {
		if _, exists := content["tcm_diagnosis"]; exists {
			return fmt.Errorf("非中医病历不能填写中医诊断")
		}
		if complete && name(western) == "" {
			return fmt.Errorf("请填写西医诊断")
		}
	}
	return nil
}

func object(content map[string]any, key string, tcm bool) (map[string]any, error) {
	v, exists := content[key]
	if !exists {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s 必须是结构化对象", key)
	}
	allowed := map[string]bool{"diagnosis_name": true, "basis": true}
	if tcm {
		allowed["pattern_name"] = true
	} else {
		allowed["icd10_code"] = true
	}
	for k, value := range m {
		if !allowed[k] {
			return nil, fmt.Errorf("%s 不允许字段 %s", key, k)
		}
		if _, ok := value.(string); !ok {
			return nil, fmt.Errorf("%s.%s 必须是文本", key, k)
		}
	}
	return m, nil
}

func ValidateField(key string, value any) error {
	_, err := object(map[string]any{key: value}, key, key == "tcm_diagnosis")
	return err
}

func Name(value any) string {
	m, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return name(m)
}
func name(m map[string]any) string {
	if m == nil {
		return ""
	}
	value, _ := m["diagnosis_name"].(string)
	return strings.TrimSpace(value)
}
