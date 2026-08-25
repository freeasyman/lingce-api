package emrrule

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

func (e *Engine) Evaluate(record *RecordInput, rules []RuleDefinition, stage string) []RuleHit {
	results := make([]RuleHit, 0)
	for _, rule := range rules {
		if hit, ok := evaluateHardRule(record, rule, stage); ok {
			results = append(results, hit)
		}
	}
	return results
}

func evaluateHardRule(record *RecordInput, rule RuleDefinition, stage string) (RuleHit, bool) {
	switch rule.Code {
	case "emr.patient.basic_info":
		return missingFieldsHit(record, rule, stage, []fieldCheck{
			{"patient.name", "患者姓名", record.PatientName},
			{"patient.gender", "性别", record.PatientGender},
			{"patient.age_text", "年龄", record.PatientAgeText},
			{"patient.phone", "联系电话", record.PatientPhone},
		})
	case "emr.chief_complaint.norm":
		chief := firstNonEmpty(record.ChiefComplaint, stringAt(record.DocumentJSON, "sections", "chief_complaint"))
		if strings.TrimSpace(chief) == "" {
			return buildHit(record, rule, stage, "important", "sections.chief_complaint", "主诉", "主诉不能为空", "请补充主诉，写明主要症状或体征及持续时间。", "required", "", "非空"), true
		}
		if utf8.RuneCountInString(chief) > 20 {
			return buildHit(record, rule, stage, "important", "sections.chief_complaint", "主诉", "主诉超过20字", "请将主诉控制在20个字以内。", "max_length", chief, "不超过20字"), true
		}
	case "emr.allergy.required":
		allergy := firstNonEmpty(
			stringAt(record.PatientSnapshot, "allergy_history"),
			stringAt(record.DocumentJSON, "patient", "allergy_history"),
			stringAt(record.DocumentJSON, "sections", "allergy_history"),
		)
		if isBlankOrUnconfirmed(allergy) {
			severity := severityForStage(stage, "important", "blocking")
			return buildHit(record, rule, stage, severity, "patient.allergy_history", "过敏史", "过敏史未确认", "请确认过敏史；如无过敏也需记录已询问。", "confirmed_required", allergy, "无/具体过敏史/已确认"), true
		}
	case "emr.diagnosis.plan":
		diagnosis := firstNonEmpty(record.DiagnosisSummary, stringAt(record.DocumentJSON, "sections", "diagnosis"))
		if strings.TrimSpace(diagnosis) == "" {
			return buildHit(record, rule, stage, "important", "sections.diagnosis", "诊断", "诊断为空", "请完善诊断；待查时请写明下一步计划。", "required", "", "非空"), true
		}
	case "emr.past_history.complete":
		return sectionRequiredHit(record, rule, stage, "sections.past_history", "既往史", "既往史未记录", "请补充既往史；如无特殊情况，也需记录已询问或确认无。", "important")
	case "emr.personal_history.complete":
		return sectionRequiredHit(record, rule, stage, "sections.personal_history", "个人史", "个人史未记录", "请补充个人史，或确认本次不适用。", "notice")
	case "emr.family_history.complete":
		return sectionRequiredHit(record, rule, stage, "sections.family_history", "家族史", "家族史未记录", "请补充家族史，或确认无相关家族病史。", "notice")
	case "emr.present_illness.general_status":
		return fieldGroupRequiredHit(record, rule, stage, []fieldCheck{
			{"structured.diet", "饮食", firstNonEmpty(stringAt(record.DocumentJSON, "structured", "diet"), stringAt(record.DocumentJSON, "sections", "diet"))},
			{"structured.sleep", "睡眠", firstNonEmpty(stringAt(record.DocumentJSON, "structured", "sleep"), stringAt(record.DocumentJSON, "sections", "sleep"))},
			{"structured.bowel_movement", "大便", firstNonEmpty(stringAt(record.DocumentJSON, "structured", "bowel_movement"), stringAt(record.DocumentJSON, "sections", "bowel_movement"))},
			{"structured.urination", "小便", firstNonEmpty(stringAt(record.DocumentJSON, "structured", "urination"), stringAt(record.DocumentJSON, "sections", "urination"))},
		}, "structured.general_status", "一般情况", "饮食睡眠二便信息未完整记录", "请补问饮食、睡眠、二便，或确认本次不适用。", "notice")
	case "emr.auxiliary_exam.closed_loop":
		orders := firstNonEmpty(stringAt(record.DocumentJSON, "structured", "exam_orders"), stringAt(record.DocumentJSON, "sections", "exam_orders"))
		results := firstNonEmpty(stringAt(record.DocumentJSON, "structured", "exam_results"), stringAt(record.DocumentJSON, "sections", "auxiliary_exam"))
		pendingNote := firstNonEmpty(stringAt(record.DocumentJSON, "structured", "exam_pending_note"), stringAt(record.DocumentJSON, "sections", "exam_pending_note"))
		if strings.TrimSpace(orders) != "" && strings.TrimSpace(results) == "" && strings.TrimSpace(pendingNote) == "" {
			return buildHitWithExecutor(record, rule, stage, "structured", "important", "structured.exam_orders", "辅助检查", "辅助检查结果未闭环", "请补充检查结果，或标明结果待回。", "exam_closed_loop", orders, "结果入文或待回说明"), true
		}
	case "emr.treatment.consistency":
		prescription := firstNonEmpty(stringAt(record.DocumentJSON, "sections", "prescription"), stringAt(record.DocumentJSON, "structured", "prescription"))
		treatment := firstNonEmpty(stringAt(record.DocumentJSON, "sections", "treatment"), stringAt(record.DocumentJSON, "sections", "medical_advice"), stringAt(record.DocumentJSON, "structured", "treatment"))
		if strings.TrimSpace(prescription) != "" && strings.TrimSpace(treatment) == "" {
			return buildHitWithExecutor(record, rule, stage, "structured", "important", "sections.treatment", "处理意见", "有处方但病历缺少处理意见", "请核对处方、处置和病历医嘱是否一致。", "relationship_required", prescription, "处方应有对应处理意见"), true
		}
	case "emr.allergy.prescription_risk":
		allergy := firstNonEmpty(stringAt(record.PatientSnapshot, "allergy_history"), stringAt(record.DocumentJSON, "patient", "allergy_history"), stringAt(record.DocumentJSON, "sections", "allergy_history"))
		prescription := firstNonEmpty(stringAt(record.DocumentJSON, "sections", "prescription"), stringAt(record.DocumentJSON, "structured", "prescription"))
		if allergyPrescriptionConflict(allergy, prescription) {
			return buildHitWithExecutor(record, rule, stage, "structured", "blocking", "sections.prescription", "处方", "过敏史与处方存在明确风险", "请先核对过敏史与处方风险，再继续开药。", "allergy_prescription_conflict", prescription, "处方不得包含明确过敏风险药物"), true
		}
	case "emr.tcm.western_diagnosis_prompt":
		if !isTCM(record) {
			return RuleHit{}, false
		}
		diagnosis := firstNonEmpty(record.DiagnosisSummary, stringAt(record.DocumentJSON, "sections", "diagnosis"))
		if strings.TrimSpace(diagnosis) == "" {
			return buildHit(record, rule, stage, "notice", "sections.diagnosis", "西医诊断", "中医病历未写西医诊断", "如能明确，请补充西医诊断；如暂不能明确，可确认后继续。", "optional_prompt", "", "可明确时补充"), true
		}
	case "emr.followup.complete":
		if !needsFollowUp(record) {
			return RuleHit{}, false
		}
		followUp := firstNonEmpty(
			stringAt(record.DocumentJSON, "sections", "follow_up"),
			stringAt(record.DocumentJSON, "structured", "follow_up"),
			stringAt(record.DocumentJSON, "sections", "medical_advice"),
		)
		if strings.TrimSpace(followUp) == "" {
			return buildHit(record, rule, stage, "notice", "sections.follow_up", "复诊安排", "需要复诊或观察但缺少复诊安排", "请补充复诊时间、触发条件或随访要求。", "required_when_followup_needed", "", "有复诊安排或随访要求"), true
		}
	case "emr.signature.required":
		if stage != "pre_archive" {
			return RuleHit{}, false
		}
		signature := firstNonEmpty(
			stringAt(record.DocumentJSON, "structured", "physician_signature"),
			stringAt(record.DocumentJSON, "sections", "physician_signature"),
		)
		if strings.TrimSpace(signature) == "" {
			return buildHit(record, rule, stage, "blocking", "structured.physician_signature", "医师签名", "归档前缺少医师签名", "归档前请完成医师确认，必要时补上级签名。", "required", "", "已签名"), true
		}
	case "emr.time.consistency":
		if record.EncounteredAt != nil && record.ArchivedAt != nil && record.ArchivedAt.Before(*record.EncounteredAt) {
			return buildHit(record, rule, stage, "blocking", "record.archived_at", "归档时间", "归档时间早于接诊时间", "请检查病历时间链是否存在逆序。", "time_order", record.ArchivedAt.String(), "归档时间不得早于接诊时间"), true
		}
	case "emr.gynecology.menstrual_history":
		if !isGynecology(record) {
			return RuleHit{}, false
		}
		return fieldGroupRequiredHit(record, rule, stage, []fieldCheck{
			{"extensions.menstrual_history", "月经史", firstNonEmpty(stringAt(record.DocumentJSON, "extensions", "menstrual_history"), stringAt(record.DocumentJSON, "structured", "menstrual_history"), stringAt(record.DocumentJSON, "sections", "menstrual_history"))},
			{"extensions.last_menstrual_period", "末次月经", firstNonEmpty(stringAt(record.DocumentJSON, "extensions", "last_menstrual_period"), stringAt(record.DocumentJSON, "structured", "last_menstrual_period"))},
		}, "extensions.gynecology", "月经史", "月经史或末次月经未完整记录", "请补充月经史和末次月经。", "important")
	case "emr.gynecology.marriage_childbearing":
		if !isGynecology(record) {
			return RuleHit{}, false
		}
		return fieldGroupRequiredHit(record, rule, stage, []fieldCheck{
			{"extensions.marriage_childbearing", "婚育史", firstNonEmpty(stringAt(record.DocumentJSON, "extensions", "marriage_childbearing"), stringAt(record.DocumentJSON, "sections", "marriage_childbearing"))},
			{"extensions.pregnancy_history", "孕产史", firstNonEmpty(stringAt(record.DocumentJSON, "extensions", "pregnancy_history"), stringAt(record.DocumentJSON, "sections", "pregnancy_history"))},
		}, "extensions.gynecology", "婚育史/孕产史", "婚育史或孕产史未完整记录", "请补充婚育史和孕产史。", "important")
	case "emr.gynecology.exam_required":
		if !isGynecology(record) {
			return RuleHit{}, false
		}
		exam := firstNonEmpty(stringAt(record.DocumentJSON, "sections", "gyne_exam"), stringAt(record.DocumentJSON, "extensions", "gyne_exam"))
		reason := firstNonEmpty(stringAt(record.DocumentJSON, "extensions", "gyne_exam_not_done_reason"), stringAt(record.DocumentJSON, "sections", "gyne_exam_not_done_reason"))
		if strings.TrimSpace(exam) == "" && strings.TrimSpace(reason) == "" {
			return buildHit(record, rule, stage, "important", "sections.gyne_exam", "妇科查体", "妇科查体或未查原因未记录", "请记录妇科查体，或说明未查原因。", "required_or_reason", "", "查体或未查原因"), true
		}
	case "emr.dental.tooth_plan":
		if !isDental(record) {
			return RuleHit{}, false
		}
		tooth := firstNonEmpty(stringAt(record.DocumentJSON, "structured", "tooth_position"), stringAt(record.DocumentJSON, "extensions", "tooth_position"), stringAt(record.DocumentJSON, "sections", "tooth_position"))
		plan := firstNonEmpty(stringAt(record.DocumentJSON, "sections", "treatment_plan"), stringAt(record.DocumentJSON, "structured", "treatment_plan"))
		if strings.TrimSpace(plan) != "" && strings.TrimSpace(tooth) == "" {
			return buildHitWithExecutor(record, rule, stage, "structured", "important", "structured.tooth_position", "牙位", "治疗计划缺少对应牙位", "请确认牙位与治疗计划、影像部位一致。", "relationship_required", plan, "治疗计划应有关联牙位"), true
		}
	case "emr.dental.informed_consent":
		if !isDental(record) {
			return RuleHit{}, false
		}
		plan := firstNonEmpty(stringAt(record.DocumentJSON, "sections", "treatment_plan"), stringAt(record.DocumentJSON, "structured", "treatment_plan"))
		consent := firstNonEmpty(stringAt(record.DocumentJSON, "structured", "informed_consent"), stringAt(record.DocumentJSON, "sections", "informed_consent"))
		if hasDentalOperation(plan) && strings.TrimSpace(consent) == "" {
			return buildHitWithExecutor(record, rule, stage, "structured", "important", "structured.informed_consent", "知情告知", "口腔操作缺少知情告知记录", "请补充知情告知和同意记录。", "required_when_operation", plan, "操作计划应有知情告知"), true
		}
	case "emr.pediatrics.guardian_info":
		if !isPediatrics(record) {
			return RuleHit{}, false
		}
		guardian := firstNonEmpty(
			stringAt(record.DocumentJSON, "structured", "guardian"),
			stringAt(record.DocumentJSON, "patient", "guardian"),
			stringAt(record.DocumentJSON, "extensions", "guardian"),
		)
		if strings.TrimSpace(guardian) == "" {
			return buildHit(record, rule, stage, "important", "patient.guardian", "监护人", "儿科病历缺少监护人信息", "请补充监护人身份和与患儿关系。", "required", "", "非空"), true
		}
	case "emr.pediatrics.fever_peak":
		if !isPediatrics(record) || !hasFever(record) {
			return RuleHit{}, false
		}
		return fieldGroupRequiredHit(record, rule, stage, []fieldCheck{
			{"structured.temperature", "最高体温", firstNonEmpty(stringAt(record.DocumentJSON, "structured", "temperature"), stringAt(record.DocumentJSON, "extensions", "temperature"))},
			{"structured.fever_duration", "发热持续时间", firstNonEmpty(stringAt(record.DocumentJSON, "structured", "fever_duration"), stringAt(record.DocumentJSON, "extensions", "fever_duration"))},
			{"structured.mental_status", "精神反应", firstNonEmpty(stringAt(record.DocumentJSON, "structured", "mental_status"), stringAt(record.DocumentJSON, "extensions", "mental_status"))},
		}, "extensions.pediatrics_fever", "发热病史", "儿童发热要素未完整记录", "请补充最高体温、持续时间和精神反应。", "important")
	case "emr.pediatrics.weight_dose":
		if !isPediatrics(record) {
			return RuleHit{}, false
		}
		prescription := firstNonEmpty(stringAt(record.DocumentJSON, "sections", "prescription"), stringAt(record.DocumentJSON, "structured", "prescription"))
		weight := firstNonEmpty(stringAt(record.DocumentJSON, "patient", "weight"), stringAt(record.DocumentJSON, "structured", "weight"), stringAt(record.PatientSnapshot, "weight"))
		if strings.TrimSpace(prescription) != "" && strings.TrimSpace(weight) == "" {
			return buildHit(record, rule, stage, "important", "patient.weight", "体重", "儿科处方缺少体重或剂量依据", "请补充体重和剂量依据。", "required_when_prescription", "", "有儿科处方时体重非空"), true
		}
	case "emr.pediatrics.feeding_growth":
		if !isPediatrics(record) {
			return RuleHit{}, false
		}
		return fieldGroupRequiredHit(record, rule, stage, []fieldCheck{
			{"extensions.feeding", "喂养史", firstNonEmpty(stringAt(record.DocumentJSON, "extensions", "feeding"), stringAt(record.DocumentJSON, "sections", "feeding"))},
			{"extensions.growth", "生长发育", firstNonEmpty(stringAt(record.DocumentJSON, "extensions", "growth"), stringAt(record.DocumentJSON, "sections", "growth"))},
			{"extensions.vaccination", "疫苗接种", firstNonEmpty(stringAt(record.DocumentJSON, "extensions", "vaccination"), stringAt(record.DocumentJSON, "sections", "vaccination"))},
		}, "extensions.pediatrics_growth", "喂养与生长发育", "喂养史、生长发育或疫苗接种未完整记录", "请补充喂养史、生长发育和疫苗接种情况。", "notice")
	}
	return RuleHit{}, false
}

type fieldCheck struct{ Path, Label, Value string }

func sectionRequiredHit(record *RecordInput, rule RuleDefinition, stage, path, label, summary, message, severity string) (RuleHit, bool) {
	value := valueByDottedPath(record, path)
	if strings.TrimSpace(value) == "" {
		return buildHit(record, rule, stage, severity, path, label, summary, message, "required", "", "非空"), true
	}
	return RuleHit{}, false
}

func fieldGroupRequiredHit(record *RecordInput, rule RuleDefinition, stage string, fields []fieldCheck, targetPath, label, summary, message, severity string) (RuleHit, bool) {
	missing := make([]string, 0)
	for _, field := range fields {
		if strings.TrimSpace(field.Value) == "" {
			missing = append(missing, field.Label)
		}
	}
	if len(missing) == 0 {
		return RuleHit{}, false
	}
	return buildHit(record, rule, stage, severity, targetPath, label, summary+"："+strings.Join(missing, "、"), message, "required_fields", strings.Join(missing, "、"), "必填字段完整"), true
}

func valueByDottedPath(record *RecordInput, path string) string {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return ""
	}
	switch parts[0] {
	case "sections", "structured", "extensions", "patient":
		return stringAt(record.DocumentJSON, parts...)
	case "record":
		if len(parts) > 1 && parts[1] == "chief_complaint" {
			return record.ChiefComplaint
		}
	}
	return ""
}

func missingFieldsHit(record *RecordInput, rule RuleDefinition, stage string, fields []fieldCheck) (RuleHit, bool) {
	missing := make([]string, 0)
	for _, field := range fields {
		if strings.TrimSpace(field.Value) == "" {
			missing = append(missing, field.Label)
		}
	}
	if len(missing) == 0 {
		return RuleHit{}, false
	}
	return buildHit(record, rule, stage, "important", "patient", "患者基本信息", "患者基本信息不完整："+strings.Join(missing, "、"), "请补全患者基本信息后再提交。", "required_fields", strings.Join(missing, "、"), "必填字段完整"), true
}

func buildHit(record *RecordInput, rule RuleDefinition, stage, severity, targetPath, fieldLabel, summary, doctorMessage, evidenceType, actual, expected string) RuleHit {
	return buildHitWithExecutor(record, rule, stage, "hard", severity, targetPath, fieldLabel, summary, doctorMessage, evidenceType, actual, expected)
}

func buildHitWithExecutor(record *RecordInput, rule RuleDefinition, stage, executor, severity, targetPath, fieldLabel, summary, doctorMessage, evidenceType, actual, expected string) RuleHit {
	if strings.TrimSpace(doctorMessage) == "" {
		doctorMessage = rule.SuggestedScript
	}
	return RuleHit{
		RecordID: record.ID, RuleID: rule.ID, RuleCode: rule.Code, RuleName: rule.Name,
		Stage: stage, ExecutorType: executor, TargetType: "field", TargetPath: targetPath,
		HitStatus: "hit", Severity: severity, DoctorMessage: doctorMessage, QCMessage: rule.Description,
		Summary: summary, CurrentState: "open", IsActive: true,
		ActionPolicy: actionPolicy(stage, severity),
		Evidence:     []RuleEvidence{{EvidenceType: "field", FieldPath: targetPath, FieldLabel: fieldLabel, TextExcerpt: actual, StructuredValue: JSONMap{"check": evidenceType}, ExpectedValue: JSONMap{"value": expected}, ActualValue: JSONMap{"value": actual}}},
	}
}

func actionPolicy(stage, severity string) JSONMap {
	return JSONMap{
		"block_submit":  severity == "blocking" && stage == "pre_submit",
		"block_archive": severity == "blocking" && stage == "pre_archive",
		"allow_confirm": severity != "blocking",
		"count_in_qc":   true,
	}
}

func severityForStage(stage, defaultSeverity, archiveSeverity string) string {
	if stage == "pre_archive" {
		return archiveSeverity
	}
	return defaultSeverity
}

func isBlankOrUnconfirmed(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return true
	}
	for _, marker := range []string{"未询问", "待确认", "不详", "未知"} {
		if strings.Contains(v, marker) {
			return true
		}
	}
	return false
}

func isPediatrics(record *RecordInput) bool {
	text := strings.ToLower(record.TemplateCode + " " + record.TemplateName)
	return strings.Contains(text, "pediatrics") || strings.Contains(text, "儿科")
}

func isGynecology(record *RecordInput) bool {
	text := strings.ToLower(record.TemplateCode + " " + record.TemplateName)
	return strings.Contains(text, "gynecology") || strings.Contains(text, "妇科")
}

func isTCM(record *RecordInput) bool {
	text := strings.ToLower(record.TemplateCode + " " + record.TemplateName)
	return strings.Contains(text, "tcm") || strings.Contains(text, "中医")
}

func isDental(record *RecordInput) bool {
	text := strings.ToLower(record.TemplateCode + " " + record.TemplateName)
	return strings.Contains(text, "dental") || strings.Contains(text, "口腔")
}

func allergyPrescriptionConflict(allergy, prescription string) bool {
	allergy = strings.ToLower(strings.TrimSpace(allergy))
	prescription = strings.ToLower(strings.TrimSpace(prescription))
	if allergy == "" || prescription == "" || isBlankOrUnconfirmed(allergy) {
		return false
	}
	checks := map[string][]string{
		"青霉素": {"青霉素", "阿莫西林", "氨苄西林", "哌拉西林"},
		"头孢":  {"头孢", "cef"},
		"磺胺":  {"磺胺"},
	}
	for allergen, drugs := range checks {
		if strings.Contains(allergy, allergen) {
			for _, drug := range drugs {
				if strings.Contains(prescription, strings.ToLower(drug)) {
					return true
				}
			}
		}
	}
	return false
}

func hasDentalOperation(plan string) bool {
	for _, keyword := range []string{"拔牙", "种植", "正畸", "修复", "根管", "手术"} {
		if strings.Contains(plan, keyword) {
			return true
		}
	}
	return false
}

func hasFever(record *RecordInput) bool {
	text := record.ChiefComplaint + " " + stringAt(record.DocumentJSON, "sections", "chief_complaint") + " " + stringAt(record.DocumentJSON, "sections", "present_illness")
	return strings.Contains(text, "发热") || strings.Contains(text, "发烧") || strings.Contains(strings.ToLower(text), "fever")
}

func needsFollowUp(record *RecordInput) bool {
	text := strings.Join([]string{
		record.ChiefComplaint,
		record.DiagnosisSummary,
		stringAt(record.DocumentJSON, "sections", "chief_complaint"),
		stringAt(record.DocumentJSON, "sections", "present_illness"),
		stringAt(record.DocumentJSON, "sections", "diagnosis"),
		stringAt(record.DocumentJSON, "sections", "auxiliary_exam"),
		stringAt(record.DocumentJSON, "structured", "exam_pending_note"),
	}, " ")
	for _, keyword := range []string{"复诊", "随访", "观察", "待查", "待回", "复查", "慢病", "高血压", "糖尿病"} {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringAt(data JSONMap, path ...string) string {
	var current any = data
	for _, key := range path {
		switch m := current.(type) {
		case JSONMap:
			current = m[key]
		case map[string]any:
			current = m[key]
		default:
			return ""
		}
	}
	switch v := current.(type) {
	case string:
		return strings.TrimSpace(v)
	case nil:
		return ""
	default:
		return strings.TrimSpace(strings.Trim(strings.ReplaceAll(strings.ReplaceAll(toString(v), "\n", " "), "\t", " "), " "))
	}
}

func toString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(fmtAny(v)), "map[", ""), "]", "")), "{"), "}"), "\n", " "), "\t", " "))
	}
}

func fmtAny(value any) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(fmt.Sprintf("%v", value)), "\n", " "), "\t", " "))
}
