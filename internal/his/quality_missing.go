package his

import (
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/emrquality"
)

// patientFieldLabels 质控患者快照字段 → 中文名（与 emrquality.patientFieldName 对齐，
// 出生日期按 HIS 业务口径命名为「出生日期」）。
var patientFieldLabels = map[string]string{
	"name":           "姓名",
	"gender":         "性别",
	"birth_date":     "出生日期",
	"age":            "年龄",
	"phone":          "手机号",
	"ethnicity":      "民族",
	"marital_status": "婚姻状况",
	"occupation":     "职业",
	"work_unit":      "工作单位",
	"address":        "住址",
	"allergy":        "过敏史",
	"drug_allergy":   "药物过敏史",
	"id_number":      "身份证号",
}

// contentFieldLabels 病历内容字段 → 中文名（用于完成接诊缺项定位）。
var contentFieldLabels = map[string]string{
	"chief_complaint":           "主诉",
	"present_illness":           "现病史",
	"past_history":              "既往史",
	"personal_history":          "个人史",
	"family_history":            "家族史",
	"allergy_history":           "过敏史",
	"physical_exam":             "体格检查",
	"auxiliary_exam":            "辅助检查",
	"diagnosis":                 "诊断",
	"tcm_diagnosis":             "中医诊断",
	"prescription":              "处方",
	"treatment":                 "治疗意见",
	"disposition":               "处置",
	"medical_advice":            "医嘱",
	"followup":                  "复诊安排",
	"supplement":                "补充说明",
	"menstrual_history":         "月经史",
	"gynecology_specialty_exam": "妇科专科查体",
}

// fieldLabel 返回字段中文名；未知字段返回空串，由调用方回退到证据原文。
func fieldLabel(field string) string {
	field = strings.TrimSpace(field)
	field = strings.TrimPrefix(field, "patient.")
	field = strings.TrimPrefix(field, "content.")
	if label, ok := patientFieldLabels[field]; ok {
		return label
	}
	return contentFieldLabels[field]
}

// missingFieldsFromReport 从质检报告中提取阻断缺项：优先使用 defect_keys，
// 其次使用证据路径；去重并保留报告顺序。
func missingFieldsFromReport(report *emrquality.Report) []MissingField {
	if report == nil {
		return []MissingField{}
	}
	missing := make([]MissingField, 0)
	seen := make(map[string]bool)
	for _, result := range report.Results {
		if result.Result != "problem" || !result.Blocking {
			continue
		}
		keys := make([]string, 0, len(result.DefectKeys)+len(result.Evidence))
		keys = append(keys, result.DefectKeys...)
		for _, evidence := range result.Evidence {
			path := strings.TrimSpace(evidence.Path)
			if path != "" {
				keys = append(keys, path)
			}
		}
		if len(keys) == 0 {
			// 无法定位到具体字段时，仍把阻断规则展示给医生，避免只剩通用文案。
			label := strings.TrimSpace(result.Reason)
			if label == "" {
				label = result.Message
			}
			if label == "" {
				label = result.RuleCode
			}
			key := "rule:" + result.RuleCode
			if seen[key] {
				continue
			}
			seen[key] = true
			missing = append(missing, MissingField{Field: "rule:" + result.RuleCode, Label: label, RuleCode: result.RuleCode, Message: result.Message})
			continue
		}
		for _, raw := range keys {
			key := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(raw), "patient."), "content.")
			if key == "" || seen[key] {
				continue
			}
			label := fieldLabel(key)
			if label == "" {
				label = strings.TrimSpace(result.Reason)
			}
			if label == "" {
				label = result.RuleCode
			}
			seen[key] = true
			missing = append(missing, MissingField{Field: key, Label: label, RuleCode: result.RuleCode, Message: result.Message})
		}
	}
	return missing
}

// qualityBlockedMessage 生成「病历质量检查未通过：缺少出生日期」类文案。
func qualityBlockedMessage(missing []MissingField) string {
	labels := make([]string, 0, len(missing))
	for _, item := range missing {
		if label := strings.TrimSpace(item.Label); label != "" {
			labels = append(labels, label)
		}
	}
	if len(labels) == 0 {
		return "病历质量检查未通过"
	}
	return fmt.Sprintf("病历质量检查未通过：缺少%s", strings.Join(labels, "、"))
}
