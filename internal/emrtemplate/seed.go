package emrtemplate

type builtinTemplateSeed struct {
	Code           string
	Name           string
	ShortName      string
	DepartmentCode string
	DepartmentName string
	Status         string
	Description    string
	SchemaJSON     map[string]any
	Bindings       []BindingItemUpsert
}

func commonCoreFields() []map[string]any {
	return []map[string]any{
		{"key": "chief_complaint", "label": "主诉", "required": true, "group": "history", "description": "症状、部位、持续时间。"},
		{"key": "present_illness", "label": "现病史", "required": true, "group": "history", "description": "围绕主诉展开的本次发病过程。"},
		{"key": "past_history", "label": "既往史", "required": false, "group": "history", "description": "慢病、手术、传染病、既往重要疾病。"},
		{"key": "personal_history", "label": "个人史", "required": false, "group": "history", "description": "吸烟饮酒、职业暴露、生活习惯等。"},
		{"key": "family_history", "label": "家族史", "required": false, "group": "history", "description": "遗传病、肿瘤、慢病等家族病史。"},
		{"key": "allergy_history", "label": "过敏史", "required": true, "group": "history", "description": "药物、食物及其他过敏史。"},
		{"key": "physical_exam", "label": "体格检查", "required": true, "group": "exam", "description": "生命体征、阳性体征、必要阴性体征、专科查体。"},
		{"key": "auxiliary_exam", "label": "辅助检查", "required": false, "group": "exam", "description": "检查时间、项目、结果和结论。"},
		{"key": "diagnosis", "label": "诊断", "required": true, "group": "diagnosis", "description": "规范诊断名称，可含西医/中医诊断。"},
		{"key": "prescription", "label": "处方", "required": false, "group": "orders", "description": "药品处方。"},
		{"key": "treatment", "label": "处置", "required": false, "group": "orders", "description": "治疗、检查申请、输液、操作项目。"},
		{"key": "medical_advice", "label": "医嘱", "required": true, "group": "orders", "description": "执行指导、禁忌和注意事项。"},
		{"key": "follow_up", "label": "复诊安排", "required": false, "group": "followup", "description": "复诊时间、触发条件和随访安排。"},
		{"key": "draft_note", "label": "补充说明", "required": false, "group": "followup", "description": "无法归入正式字段的特殊说明。"},
	}
}

func commonStructuredGroups() []map[string]any {
	return []map[string]any{
		{
			"key":   "present_illness",
			"label": "现病史补全项",
			"scope": "all",
			"items": []map[string]any{
				{"key": "onset_and_evolution", "label": "起病与演变", "required": true, "description": "起病时间、演变、缓解/加重过程。"},
				{"key": "associated_symptoms", "label": "伴随症状", "required": true, "description": "阳性与必要阴性伴随症状。"},
				{"key": "diet", "label": "饮食", "required": false, "description": "食欲、饮水、摄入变化。"},
				{"key": "sleep", "label": "睡眠", "required": false, "description": "睡眠质量、夜醒和近期变化。"},
				{"key": "bowel_movement", "label": "大便", "required": false, "description": "频次、性状和异常。"},
				{"key": "urination", "label": "小便", "required": false, "description": "频次、颜色、异常。"},
				{"key": "recent_weight_change", "label": "近期体重变化", "required": false, "description": "增减幅度和时间范围。"},
				{"key": "prior_treatment_response", "label": "既往治疗及效果", "required": false, "description": "本次就诊前处理及反应。"},
			},
		},
		{
			"key":   "physical_exam",
			"label": "体格检查子项",
			"scope": "all",
			"items": []map[string]any{
				{"key": "temperature", "label": "体温", "required": true, "description": "可记录为具体数值或未测原因。"},
				{"key": "blood_pressure", "label": "血压", "required": true, "description": "收缩压/舒张压。"},
				{"key": "pulse", "label": "脉搏", "required": true, "description": "脉率和节律。"},
				{"key": "respiratory_rate", "label": "呼吸", "required": true, "description": "呼吸频率和异常表现。"},
				{"key": "height", "label": "身高", "required": false, "description": "儿科、慢病、营养评估重点。"},
				{"key": "weight", "label": "体重", "required": false, "description": "儿科、慢病、用药剂量重点。"},
				{"key": "bmi", "label": "BMI", "required": false, "description": "由身高体重换算。"},
				{"key": "general_exam", "label": "一般状态", "required": true, "description": "神志、体位、病容、面色。"},
				{"key": "positive_findings", "label": "阳性体征", "required": true, "description": "与诊断直接相关的阳性体征。"},
				{"key": "necessary_negative_findings", "label": "必要阴性体征", "required": true, "description": "支持鉴别诊断的重要阴性体征。"},
				{"key": "specialty_exam", "label": "专科查体", "required": false, "description": "按科室模板追加。"},
			},
		},
	}
}

func defaultStages() BindingStageConfig {
	return BindingStageConfig{"realtime": "enabled", "save_check": "notice", "submit_check": "required", "archive_check": "scored"}
}

func commonQualityBindings() []BindingItemUpsert {
	return []BindingItemUpsert{
		{RuleCode: "emr.patient.basic_info", Enabled: true, RuleScope: "common", SortOrder: 40, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.time.consistency", Enabled: true, RuleScope: "common", SortOrder: 45, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.chief_complaint.norm", Enabled: true, RuleScope: "common", SortOrder: 50, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.present_illness.complete", Enabled: true, RuleScope: "common", SortOrder: 60, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.past_history.complete", Enabled: true, RuleScope: "common", SortOrder: 61, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.personal_history.complete", Enabled: true, RuleScope: "common", SortOrder: 62, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.family_history.complete", Enabled: true, RuleScope: "common", SortOrder: 63, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.physical_exam.specific", Enabled: true, RuleScope: "common", SortOrder: 80, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.auxiliary_exam.closed_loop", Enabled: true, RuleScope: "common", SortOrder: 90, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.diagnosis.plan", Enabled: true, RuleScope: "common", SortOrder: 100, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.treatment.consistency", Enabled: true, RuleScope: "common", SortOrder: 110, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.allergy.required", Enabled: true, RuleScope: "common", SortOrder: 120, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.allergy.prescription_risk", Enabled: true, RuleScope: "common", SortOrder: 121, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.followup.complete", Enabled: true, RuleScope: "common", SortOrder: 190, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.signature.required", Enabled: true, RuleScope: "common", SortOrder: 200, StageConfigJSON: defaultStages()},
		{RuleCode: "emr.audit.trail", Enabled: true, RuleScope: "common", SortOrder: 210, StageConfigJSON: defaultStages()},
	}
}

func withBindings(items ...[]BindingItemUpsert) []BindingItemUpsert {
	out := make([]BindingItemUpsert, 0)
	for _, group := range items {
		out = append(out, group...)
	}
	return out
}

func builtinTemplates() []builtinTemplateSeed {
	return []builtinTemplateSeed{
		{
			Code:           "general",
			Name:           "标准全科门诊病历",
			ShortName:      "全科门诊",
			DepartmentCode: "gp",
			DepartmentName: "全科医疗科",
			Status:         "enabled",
			Description:    "适用于普通门诊、全科初诊和复诊。",
			SchemaJSON:     map[string]any{"core_fields": commonCoreFields(), "structured_groups": commonStructuredGroups(), "extension_groups": []map[string]any{}},
			Bindings: withBindings(
				[]BindingItemUpsert{
					{RuleCode: "common.visible_fields", Enabled: true, RuleScope: "common", SortOrder: 10, StageConfigJSON: defaultStages()},
					{RuleCode: "common.orders_split", Enabled: true, RuleScope: "common", SortOrder: 20, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.present_illness.general_status", Enabled: true, RuleScope: "common", SortOrder: 70, StageConfigJSON: defaultStages()},
				},
				commonQualityBindings(),
			),
		},
		{
			Code:           "gynecology",
			Name:           "妇科门诊病历",
			ShortName:      "妇科门诊",
			DepartmentCode: "gyne",
			DepartmentName: "妇科",
			Status:         "enabled",
			Description:    "强化月经史、婚育史、孕产史和妇科专科查体。",
			SchemaJSON: map[string]any{
				"core_fields":       commonCoreFields(),
				"structured_groups": commonStructuredGroups(),
				"extension_groups": []map[string]any{
					{"key": "gynecology_history", "label": "妇科扩展字段", "items": []map[string]any{
						{"key": "menstrual_history", "label": "月经史", "required": true, "description": "周期、经期、经量、痛经。"},
						{"key": "last_menstrual_period", "label": "末次月经", "required": true, "description": "LMP 日期或无法提供原因。"},
						{"key": "marriage_childbearing", "label": "婚育史", "required": true, "description": "婚育情况、避孕情况。"},
						{"key": "pregnancy_history", "label": "孕产史", "required": true, "description": "孕次、产次、流产及分娩情况。"},
						{"key": "gyne_exam", "label": "妇科专科查体", "required": false, "description": "外阴、阴道、宫颈、附件等。"},
					}},
				},
			},
			Bindings: withBindings(
				[]BindingItemUpsert{
					{RuleCode: "common.visible_fields", Enabled: true, RuleScope: "common", SortOrder: 10, StageConfigJSON: defaultStages()},
					{RuleCode: "gynecology.history", Enabled: true, RuleScope: "specialty", SortOrder: 20, StageConfigJSON: defaultStages()},
					{RuleCode: "gynecology.exam", Enabled: true, RuleScope: "specialty", SortOrder: 30, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.gynecology.menstrual_history", Enabled: true, RuleScope: "specialty", SortOrder: 130, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.gynecology.marriage_childbearing", Enabled: true, RuleScope: "specialty", SortOrder: 131, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.gynecology.exam_required", Enabled: true, RuleScope: "specialty", SortOrder: 132, StageConfigJSON: defaultStages()},
				},
				commonQualityBindings(),
			),
		},
		{
			Code:           "tcm",
			Name:           "中医门诊病历",
			ShortName:      "中医门诊",
			DepartmentCode: "tcm",
			DepartmentName: "中医科",
			Status:         "enabled",
			Description:    "强化四诊、辨证、治法和方药。",
			SchemaJSON: map[string]any{
				"core_fields":       commonCoreFields(),
				"structured_groups": commonStructuredGroups(),
				"extension_groups": []map[string]any{
					{"key": "tcm_extensions", "label": "中医扩展字段", "items": []map[string]any{
						{"key": "tcm_diagnosis", "label": "中医诊断", "required": true, "description": "中医病名或证名。"},
						{"key": "inspection", "label": "望诊", "required": true, "description": "神色、形态、舌质舌苔等。"},
						{"key": "listening_smelling", "label": "闻诊", "required": false, "description": "声音、气味等。"},
						{"key": "inquiry", "label": "问诊", "required": true, "description": "寒热、汗、饮食、二便等。"},
						{"key": "palpation", "label": "切诊", "required": true, "description": "脉象及按诊。"},
						{"key": "tongue", "label": "舌象", "required": true, "description": "舌质、舌苔。"},
						{"key": "pulse_tcm", "label": "脉象", "required": true, "description": "浮沉迟数、虚实滑涩。"},
						{"key": "syndrome", "label": "中医辨证", "required": true, "description": "证候、病机、辨证依据。"},
						{"key": "treatment_method", "label": "治法", "required": true, "description": "治则治法。"},
						{"key": "formula", "label": "方药", "required": false, "description": "方名、药味、剂量、煎服法。"},
					}},
				},
			},
			Bindings: withBindings(
				[]BindingItemUpsert{
					{RuleCode: "common.visible_fields", Enabled: true, RuleScope: "common", SortOrder: 10, StageConfigJSON: defaultStages()},
					{RuleCode: "tcm.dual_diagnosis", Enabled: true, RuleScope: "specialty", SortOrder: 20, StageConfigJSON: defaultStages()},
					{RuleCode: "tcm.formula", Enabled: true, RuleScope: "specialty", SortOrder: 30, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.present_illness.general_status", Enabled: true, RuleScope: "common", SortOrder: 70, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.tcm.elements", Enabled: true, RuleScope: "specialty", SortOrder: 140, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.tcm.evidence_chain", Enabled: true, RuleScope: "specialty", SortOrder: 141, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.tcm.western_diagnosis_prompt", Enabled: true, RuleScope: "specialty", SortOrder: 142, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.tcm.formula_consistency", Enabled: true, RuleScope: "specialty", SortOrder: 143, StageConfigJSON: defaultStages()},
				},
				commonQualityBindings(),
			),
		},
		{
			Code:           "dental",
			Name:           "口腔门诊病历",
			ShortName:      "口腔门诊",
			DepartmentCode: "dental",
			DepartmentName: "口腔科",
			Status:         "enabled",
			Description:    "强化牙位、口腔专科检查和治疗计划。",
			SchemaJSON: map[string]any{
				"core_fields":       commonCoreFields(),
				"structured_groups": commonStructuredGroups(),
				"extension_groups": []map[string]any{
					{"key": "dental_extensions", "label": "口腔扩展字段", "items": []map[string]any{
						{"key": "tooth_position", "label": "牙位", "required": true, "description": "问题牙位或治疗牙位。"},
						{"key": "periodontal_status", "label": "牙周情况", "required": false, "description": "松动度、牙周袋、牙龈情况。"},
						{"key": "oral_special_exam", "label": "口腔专科检查", "required": false, "description": "龋坏、缺损、咬合、修复情况。"},
						{"key": "imaging_findings", "label": "影像所见", "required": false, "description": "牙片、CBCT 等。"},
						{"key": "treatment_plan", "label": "口腔治疗计划", "required": true, "description": "分期计划、知情告知、复诊安排。"},
					}},
				},
			},
			Bindings: withBindings(
				[]BindingItemUpsert{
					{RuleCode: "common.visible_fields", Enabled: true, RuleScope: "common", SortOrder: 10, StageConfigJSON: defaultStages()},
					{RuleCode: "dental.tooth_position", Enabled: true, RuleScope: "specialty", SortOrder: 20, StageConfigJSON: defaultStages()},
					{RuleCode: "dental.exam", Enabled: true, RuleScope: "specialty", SortOrder: 30, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.dental.tooth_plan", Enabled: true, RuleScope: "specialty", SortOrder: 150, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.dental.informed_consent", Enabled: true, RuleScope: "specialty", SortOrder: 151, StageConfigJSON: defaultStages()},
				},
				commonQualityBindings(),
			),
		},
		{
			Code:           "pediatrics",
			Name:           "儿科门诊病历",
			ShortName:      "儿科门诊",
			DepartmentCode: "pediatrics",
			DepartmentName: "儿科",
			Status:         "enabled",
			Description:    "强化监护人信息、喂养史、生长发育和儿科用药依据。",
			SchemaJSON: map[string]any{
				"core_fields":       commonCoreFields(),
				"structured_groups": commonStructuredGroups(),
				"extension_groups": []map[string]any{
					{"key": "pediatrics_extensions", "label": "儿科扩展字段", "items": []map[string]any{
						{"key": "guardian", "label": "监护人信息", "required": true, "description": "陪诊监护人与患儿关系。"},
						{"key": "feeding", "label": "喂养史", "required": false, "description": "母乳、奶粉、辅食和近期进食。"},
						{"key": "vaccination", "label": "疫苗接种史", "required": false, "description": "按年龄关注疫苗接种和近期接种。"},
						{"key": "growth", "label": "生长发育", "required": false, "description": "身高、体重、发育里程碑。"},
					}},
				},
			},
			Bindings: withBindings(
				[]BindingItemUpsert{
					{RuleCode: "common.visible_fields", Enabled: true, RuleScope: "common", SortOrder: 10, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.present_illness.general_status", Enabled: true, RuleScope: "common", SortOrder: 70, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.pediatrics.guardian_info", Enabled: true, RuleScope: "specialty", SortOrder: 160, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.pediatrics.fever_peak", Enabled: true, RuleScope: "specialty", SortOrder: 161, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.pediatrics.weight_dose", Enabled: true, RuleScope: "specialty", SortOrder: 162, StageConfigJSON: defaultStages()},
					{RuleCode: "emr.pediatrics.feeding_growth", Enabled: true, RuleScope: "specialty", SortOrder: 163, StageConfigJSON: defaultStages()},
				},
				commonQualityBindings(),
			),
		},
	}
}
