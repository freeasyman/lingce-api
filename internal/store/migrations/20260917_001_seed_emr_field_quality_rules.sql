-- 初始化栏位中心化电子病历质量标准。
--
-- 目标模型：
--   emr_fields.description   保存完整自然语言栏位标准；
--   emr_fields.quality_rules 保存该栏位的结构化质量规则数组。
--
-- 质量标准和规则内容只写入 tenant_id=0 的平台默认栏位，不覆盖租户自定义内容。
-- 新增 NOT NULL 列时，将已有 NULL 统一初始化为空规则数组；不会覆盖已有非空租户规则。
-- 数据来源：
--   apps/emr/data/emr-field-quality-standards.md
--   apps/emr/data/emr-field-quality-rules.seed.json
--
-- 署名：Codex
-- 时间：2026-09-17

DO $$
BEGIN
    IF to_regclass('public.emr_fields') IS NULL THEN
        RAISE EXCEPTION '缺少表 public.emr_fields，无法导入栏位质量标准';
    END IF;
END $$;

ALTER TABLE public.emr_fields
    ADD COLUMN IF NOT EXISTS quality_rules JSONB;

UPDATE public.emr_fields
SET quality_rules = '[]'::jsonb
WHERE quality_rules IS NULL;

ALTER TABLE public.emr_fields
    ALTER COLUMN quality_rules SET DEFAULT '[]'::jsonb,
    ALTER COLUMN quality_rules SET NOT NULL;

ALTER TABLE public.emr_fields
    DROP CONSTRAINT IF EXISTS emr_fields_quality_rules_array_check;

ALTER TABLE public.emr_fields
    ADD CONSTRAINT emr_fields_quality_rules_array_check
    CHECK (jsonb_typeof(quality_rules) = 'array');

COMMENT ON COLUMN public.emr_fields.quality_rules IS
    '栏位中心化质量规则数组；每条规则归属于当前栏位，多栏位规则通过 related_fields 指明关联栏位。';

CREATE TEMP TABLE _emr_field_quality_seed (
    code TEXT PRIMARY KEY,
    description TEXT NOT NULL,
    quality_rules JSONB NOT NULL
) ON COMMIT DROP;

INSERT INTO _emr_field_quality_seed
    (code, description, quality_rules)
VALUES
    ('chief_complaint', $emr_desc_chief_complaint$chief_complaint：主诉

适用范围：通用

质量要求：

- 必须记录患者本次就诊最主要的问题。
- 应包含主要症状或体征、发生部位和持续时间。
- 应简明，不能写成大段现病史。
- 不能只写疾病名称。
- 不能把模型推测的诊断写入主诉。
- 没有明确内容时留空并标记待补充。
- 字数不超过20字

病历生成要求：

- 从转写中提取患者本次最主要的症状、体征、部位和持续时间。
- 只保留明确事实，不把诊断推测写入主诉。
- 将主诉压缩为简洁的门诊表达。字数不超过20字。
- 缺少持续时间或其他关键信息时保留缺失，不得自行补充。

可执行检查类型：非空、长度、语义、栏位关系

本栏位相关的多栏位联合质量要求：

### `chief_complaint` + `present_illness`

要求：

- 主诉应能在现病史中找到依据。
- 现病史不能与主诉明显冲突。
- 主诉和现病史中的症状、部位、时间表达应保持基本一致。

判断方式：大模型语义判断，必要时人工复核$emr_desc_chief_complaint$, $emr_rules_chief_complaint$[
  {
    "code": "required",
    "name": "主诉不能为空",
    "enabled": true,
    "execution": "program",
    "executor": "chief_complaint_required",
    "parameters": {}
  },
  {
    "code": "max_length",
    "name": "主诉不超过20字",
    "enabled": true,
    "execution": "program",
    "executor": "chief_complaint_max_length",
    "parameters": {
      "max_length": 20,
      "count_unit": "中文字符"
    }
  },
  {
    "code": "symptom_sign",
    "name": "主诉表达主要症状或体征",
    "enabled": true,
    "execution": "llm",
    "executor": "chief_complaint_symptom_sign",
    "parameters": {},
    "llm_instruction": "判断主诉是否包含本次就诊最主要的明确症状或体征，不补充转写中没有出现的内容。"
  },
  {
    "code": "duration",
    "name": "主诉包含明确持续时间",
    "enabled": true,
    "execution": "llm",
    "executor": "chief_complaint_duration",
    "parameters": {},
    "llm_instruction": "只检查转写中是否明确出现起病或持续时间；没有明确时间时判为缺失，不猜测日期。"
  },
  {
    "code": "not_diagnosis_only",
    "name": "主诉不能只写疾病名称",
    "enabled": true,
    "execution": "llm",
    "executor": "chief_complaint_not_diagnosis_only",
    "parameters": {},
    "llm_instruction": "判断主诉是否只有疾病名称而没有症状、体征或就诊问题。"
  },
  {
    "code": "not_vague",
    "name": "主诉不能明显空泛",
    "enabled": true,
    "execution": "llm",
    "executor": "chief_complaint_not_vague",
    "parameters": {},
    "llm_instruction": "判断主诉是否能让医生理解患者本次就诊的具体问题，不因文字简短就武断判定为空泛。"
  },
  {
    "code": "present_illness_consistency",
    "name": "主诉与现病史一致",
    "enabled": true,
    "execution": "llm",
    "executor": "chief_complaint_present_illness_consistency",
    "related_fields": [
      "present_illness"
    ],
    "parameters": {},
    "llm_instruction": "比较主诉与现病史中的症状、部位和时间，不把表达差异误判为冲突。"
  }
]$emr_rules_chief_complaint$::jsonb),
    ('present_illness', $emr_desc_present_illness$present_illness：现病史

适用范围：通用

质量要求：

- 应记录起病时间、症状发展和伴随症状。
- 应保留患者明确否认的重要症状。
- 应记录既往处理和效果。
- 应区分患者陈述、医生判断和未明确信息。
- 没有询问的内容不能写成“无”。
- 不得补充转写中没有出现的病情。

病历生成要求：

- 按起病与演变、伴随症状、既往处理及效果组织内容。
- 保留明确的阳性和阴性信息。
- 未询问、未回答和无法确定的信息分别表达。
- 不把医生提问自动当成患者肯定回答。

可执行检查类型：非空、结构完整、语义、栏位关系

本栏位相关的多栏位联合质量要求：

### `present_illness` + `physical_exam`

要求：

- 体格检查不能由症状自动推导。
- 阳性体征应有明确检查依据。
- 患者自述症状与医生实际检查所见必须区分。

判断方式：大模型语义判断，必要时人工复核$emr_desc_present_illness$, $emr_rules_present_illness$[
  {
    "code": "onset",
    "name": "现病史记录起病情况",
    "enabled": true,
    "execution": "llm",
    "executor": "present_illness_onset",
    "parameters": {},
    "llm_instruction": "检查是否记录转写中明确出现的起病时间、起病方式或起病经过；没有明确内容不得补写。"
  },
  {
    "code": "evolution",
    "name": "现病史记录病情演变",
    "enabled": true,
    "execution": "llm",
    "executor": "present_illness_evolution",
    "parameters": {},
    "llm_instruction": "检查是否保留转写中明确出现的症状变化、持续或加重情况。"
  },
  {
    "code": "associated_symptoms",
    "name": "现病史记录重要伴随症状",
    "enabled": true,
    "execution": "llm",
    "executor": "present_illness_associated_symptoms",
    "parameters": {},
    "llm_instruction": "只根据转写中明确出现的重要伴随症状及明确否认内容判断，不自行决定哪些症状具有临床重要性。"
  },
  {
    "code": "prior_treatment",
    "name": "现病史记录既往处理",
    "enabled": true,
    "execution": "llm",
    "executor": "present_illness_prior_treatment",
    "parameters": {},
    "llm_instruction": "检查是否记录患者明确说出的既往处理、用药或其他处理；未询问不能写成无。"
  },
  {
    "code": "treatment_effect",
    "name": "现病史记录既往处理效果",
    "enabled": true,
    "execution": "llm",
    "executor": "present_illness_treatment_effect",
    "parameters": {},
    "llm_instruction": "只有转写明确出现治疗效果时才要求记录；没有询问或没有回答不得补成有效或无效。"
  },
  {
    "code": "general_status_diet",
    "name": "饮食情况已记录或明确不适用",
    "enabled": true,
    "execution": "llm",
    "executor": "present_illness_general_status_diet",
    "parameters": {},
    "llm_instruction": "区分已记录、明确不适用、未询问和已询问未回答；只有前两者满足要求。"
  },
  {
    "code": "general_status_sleep",
    "name": "睡眠情况已记录或明确不适用",
    "enabled": true,
    "execution": "llm",
    "executor": "present_illness_general_status_sleep",
    "parameters": {},
    "llm_instruction": "区分已记录、明确不适用、未询问和已询问未回答；只有前两者满足要求。"
  },
  {
    "code": "general_status_bowel",
    "name": "大便情况已记录或明确不适用",
    "enabled": true,
    "execution": "llm",
    "executor": "present_illness_general_status_bowel",
    "parameters": {},
    "llm_instruction": "区分已记录、明确不适用、未询问和已询问未回答；只有前两者满足要求。"
  },
  {
    "code": "general_status_urination",
    "name": "小便情况已记录或明确不适用",
    "enabled": true,
    "execution": "llm",
    "executor": "present_illness_general_status_urination",
    "parameters": {},
    "llm_instruction": "区分已记录、明确不适用、未询问和已询问未回答；只有前两者满足要求。"
  },
  {
    "code": "general_status_weight_change",
    "name": "近期体重变化已记录或明确不适用",
    "enabled": true,
    "execution": "llm",
    "executor": "present_illness_general_status_weight_change",
    "parameters": {},
    "llm_instruction": "区分已记录、明确不适用、未询问和已询问未回答；只有前两者满足要求。"
  },
  {
    "code": "unasked_not_none",
    "name": "未询问内容不能写成无",
    "enabled": true,
    "execution": "llm",
    "executor": "present_illness_unasked_not_none",
    "parameters": {},
    "llm_instruction": "核对现病史中的否认表达是否有转写依据，不能把医生提问、患者沉默或未回答改写成无。"
  },
  {
    "code": "physical_exam_consistency",
    "name": "现病史与体格检查事实一致",
    "enabled": true,
    "execution": "llm",
    "executor": "present_illness_physical_exam_consistency",
    "related_fields": [
      "physical_exam"
    ],
    "parameters": {},
    "llm_instruction": "区分患者自述症状和医生实际检查所见，不把症状自动当成体征。"
  }
]$emr_rules_present_illness$::jsonb),
    ('past_history', $emr_desc_past_history$past_history：既往史

适用范围：通用

质量要求：

- 应记录既往疾病、手术、住院和重要治疗史。
- 有明确否认时记录否认。
- 未询问时不能写成“无既往史”。
- 疾病名称和时间不能凭空补充。
- 不能把本次疾病混入既往史。

病历生成要求：

- 只提取患者明确陈述的既往疾病、手术、住院和治疗信息。
- 区分明确否认、未询问和信息待确认。
- 不根据当前诊断推测既往病史。

可执行检查类型：非空、语义、人工判断$emr_desc_past_history$, $emr_rules_past_history$[
  {
    "code": "recorded_or_explicitly_denied",
    "name": "既往史有记录或明确否认",
    "enabled": true,
    "execution": "llm",
    "executor": "past_history_recorded_or_explicitly_denied",
    "parameters": {},
    "llm_instruction": "把既往疾病、手术、住院和重要治疗史作为一个通用栏位判断；有明确记录或明确否认即可，不按慢病、手术等拆成额外报警。"
  },
  {
    "code": "not_current_illness",
    "name": "既往史不混入本次疾病",
    "enabled": true,
    "execution": "llm",
    "executor": "past_history_not_current_illness",
    "parameters": {},
    "llm_instruction": "判断既往史是否把本次就诊新出现的疾病或症状错误写成既往史。"
  }
]$emr_rules_past_history$::jsonb),
    ('personal_history', $emr_desc_personal_history$personal_history：个人史

适用范围：通用

质量要求：

- 根据实际询问记录吸烟、饮酒、职业、生活习惯等信息。
- 只记录对话中明确出现的内容。
- 未询问的项目保持未记录。
- 不得根据年龄、性别或疾病推测生活习惯。

病历生成要求：

- 只提取明确出现的职业、吸烟、饮酒、生活习惯和暴露信息。
- 未出现的信息留空，不生成默认结论。

可执行检查类型：非空、语义、人工判断$emr_desc_personal_history$, $emr_rules_personal_history$[
  {
    "code": "recorded_when_mentioned",
    "name": "个人史保留明确询问结果",
    "enabled": true,
    "execution": "llm",
    "executor": "personal_history_recorded_when_mentioned",
    "parameters": {},
    "llm_instruction": "只检查转写中明确出现的吸烟、饮酒、职业、生活习惯或暴露信息是否被保留。未询问的项目不要求补写。"
  },
  {
    "code": "no_inference",
    "name": "个人史不得自行推测",
    "enabled": true,
    "execution": "llm",
    "executor": "personal_history_no_inference",
    "parameters": {},
    "llm_instruction": "不得根据年龄、性别、职业印象或疾病推测吸烟、饮酒和生活习惯。"
  }
]$emr_rules_personal_history$::jsonb),
    ('family_history', $emr_desc_family_history$family_history：家族史

适用范围：通用

质量要求：

- 应记录明确提到的家族疾病和亲属关系。
- 有明确否认时记录否认。
- 未询问不能写成“无家族史”。
- 不得根据疾病类型推测遗传风险。

病历生成要求：

- 记录明确出现的疾病、亲属关系和家族史否认信息。
- 不从患者当前疾病推导遗传风险。

可执行检查类型：非空、语义、人工判断$emr_desc_family_history$, $emr_rules_family_history$[
  {
    "code": "recorded_or_explicitly_denied",
    "name": "家族史有记录或明确否认",
    "enabled": true,
    "execution": "llm",
    "executor": "family_history_recorded_or_explicitly_denied",
    "parameters": {},
    "llm_instruction": "记录转写中明确提到的家族疾病、亲属关系或明确否认；未询问不能写成无家族史。"
  },
  {
    "code": "no_inference",
    "name": "家族史不得自行推测",
    "enabled": true,
    "execution": "llm",
    "executor": "family_history_no_inference",
    "parameters": {},
    "llm_instruction": "不得根据当前疾病推导遗传风险或家族患病情况。"
  }
]$emr_rules_family_history$::jsonb),
    ('allergy_history', $emr_desc_allergy_history$allergy_history：过敏史

适用范围：通用

质量要求：

- 必须区分无过敏史、具体过敏原、药物过敏和其他过敏。
- 必须区分明确否认和未询问。
- 有过敏时应尽量记录过敏原和反应。
- 不能将“患者没有回答”写成“无过敏史”。
- 应与处方、用药和患者档案中的过敏信息保持一致。

病历生成要求：

- 只根据明确回答记录过敏原、过敏反应和否认信息。
- 使用“明确无过敏史”“有具体药物过敏史”“有其他过敏原”“尚未询问”“信息待确认”等状态。
- 不把医生提问、患者沉默或无关回答当成否认。

可执行检查类型：状态、非空、语义、栏位关系、人工判断

本栏位相关的多栏位联合质量要求：

### `allergy_history` + `prescription`

要求：

- 处方不能明显违反已确认的药物过敏信息。
- 未确认的过敏信息不能被当成明确无过敏。

判断方式：结构化判断，无法确定时人工复核$emr_desc_allergy_history$, $emr_rules_allergy_history$[
  {
    "code": "status_valid",
    "name": "过敏史状态合法",
    "enabled": true,
    "execution": "program",
    "executor": "allergy_history_status_valid",
    "parameters": {
      "allowed_statuses": [
        "无药物过敏",
        "具体药物过敏",
        "其他过敏原",
        "尚未询问",
        "待确认"
      ]
    }
  },
  {
    "code": "none_evidence",
    "name": "无过敏史有明确问诊依据",
    "enabled": true,
    "execution": "llm",
    "executor": "allergy_history_none_evidence",
    "parameters": {},
    "llm_instruction": "只有患者或监护人明确否认过敏时，才允许形成无过敏史；不能把医生提问、沉默或无关回答当作否认。"
  },
  {
    "code": "drug_detail",
    "name": "具体药物过敏原已记录",
    "enabled": true,
    "execution": "program",
    "executor": "allergy_history_drug_detail",
    "parameters": {
      "required_when_status": "具体药物过敏",
      "required_key": "drug_name"
    }
  },
  {
    "code": "other_detail",
    "name": "其他过敏原已记录",
    "enabled": true,
    "execution": "program",
    "executor": "allergy_history_other_detail",
    "parameters": {
      "required_when_status": "其他过敏原",
      "required_key": "other_allergen"
    }
  },
  {
    "code": "unconfirmed",
    "name": "尚未询问或待确认不能作为完成",
    "enabled": true,
    "execution": "program",
    "executor": "allergy_history_unconfirmed",
    "parameters": {
      "incomplete_statuses": [
        "尚未询问",
        "待确认"
      ]
    }
  },
  {
    "code": "prescription_consistency",
    "name": "过敏史与处方保持一致",
    "enabled": true,
    "execution": "llm",
    "executor": "allergy_history_prescription_consistency",
    "related_fields": [
      "prescription"
    ],
    "parameters": {},
    "llm_instruction": "只检查病历明确记录的过敏信息与处方是否存在事实上的明显冲突；无法确定时说明需要人工核对，不作临床用药安全结论。"
  }
]$emr_rules_allergy_history$::jsonb),
    ('physical_exam', $emr_desc_physical_exam$physical_exam：体格检查

适用范围：通用

质量要求：

- 应记录实际观察、测量或医生明确描述的体征。
- 结构化生命体征必须保留数值和单位。
- 阳性体征和重要阴性体征应有明确依据。
- 不能根据症状推测体征。
- 没有实施检查时不能虚构正常结果。

病历生成要求：

- 区分生命体征、一般状态、阳性体征、必要阴性体征和专科查体。
- 只记录实际测量、观察或明确描述的内容。
- 缺少检查依据时留空或标记未检查。

可执行检查类型：结构完整、数值格式、语义、栏位关系、人工判断$emr_desc_physical_exam$, $emr_rules_physical_exam$[
  {
    "code": "vital_signs_format",
    "name": "生命体征数值和单位格式有效",
    "enabled": true,
    "execution": "program",
    "executor": "physical_exam_vital_signs_format",
    "parameters": {
      "check_numeric_value": true,
      "check_unit": true
    }
  },
  {
    "code": "actual_exam_basis",
    "name": "体格检查有实际观察或测量依据",
    "enabled": true,
    "execution": "llm",
    "executor": "physical_exam_actual_exam_basis",
    "parameters": {},
    "llm_instruction": "只接受实际测量、观察或医生明确描述的体征；不能根据患者症状推测体征。"
  },
  {
    "code": "positive_findings_evidence",
    "name": "阳性体征有明确依据",
    "enabled": true,
    "execution": "llm",
    "executor": "physical_exam_positive_findings_evidence",
    "parameters": {},
    "llm_instruction": "判断阳性体征是否来自医生实际观察、测量或明确描述，而不是来自患者自述或模型推断。"
  },
  {
    "code": "no_unexamined_normal",
    "name": "未检查不能写成正常",
    "enabled": true,
    "execution": "llm",
    "executor": "physical_exam_no_unexamined_normal",
    "parameters": {},
    "llm_instruction": "检查正常或未见异常的表述是否有实际检查依据；未实施检查不能填写正常。"
  }
]$emr_rules_physical_exam$::jsonb),
    ('auxiliary_exam', $emr_desc_auxiliary_exam$auxiliary_exam：辅助检查

适用范围：通用

质量要求：

- 应区分检查项目、检查时间和检查结果。
- 已做检查但没有结果时，必须标记结果缺失。
- 未做检查不能写成正常。
- 不能把医生建议检查写成已经完成。
- 检查结果应与诊断和治疗方案保持事实一致。

病历生成要求：

- 分别记录检查项目、时间、结果和来源。
- 区分已完成、已安排、未完成和结果待补。
- 不把检查计划或医生建议转换成检查结果。

可执行检查类型：结构完整、状态、语义、栏位关系、人工判断$emr_desc_auxiliary_exam$, $emr_rules_auxiliary_exam$[
  {
    "code": "item_status_recorded",
    "name": "辅助检查项目状态已记录",
    "enabled": true,
    "execution": "llm",
    "executor": "auxiliary_exam_item_status_recorded",
    "parameters": {},
    "llm_instruction": "区分已完成、已安排、未完成和结果待补，不能只列检查名称而不说明状态。"
  },
  {
    "code": "result_for_completed",
    "name": "已完成检查有结果或明确结果缺失",
    "enabled": true,
    "execution": "program",
    "executor": "auxiliary_exam_result_for_completed",
    "parameters": {
      "completed_statuses": [
        "已完成",
        "已检查"
      ],
      "missing_result_status": "结果待补"
    }
  },
  {
    "code": "pending_explicit",
    "name": "未完成或待补结果有明确标记",
    "enabled": true,
    "execution": "program",
    "executor": "auxiliary_exam_pending_explicit",
    "parameters": {
      "pending_statuses": [
        "已安排",
        "未完成",
        "结果待补"
      ]
    }
  },
  {
    "code": "no_plan_as_result",
    "name": "检查计划不能写成检查结果",
    "enabled": true,
    "execution": "llm",
    "executor": "auxiliary_exam_no_plan_as_result",
    "parameters": {},
    "llm_instruction": "区分医生建议或安排的检查与已经完成并有结果的检查，不补充结果。"
  }
]$emr_rules_auxiliary_exam$::jsonb),
    ('diagnosis', $emr_desc_diagnosis$diagnosis：诊断

适用范围：通用

质量要求：

- 只能记录医生明确提出或确认的诊断。
- 应区分明确诊断、疑似诊断和待排诊断。
- 诊断应有主诉、现病史、查体或检查依据。
- 不能把患者自述直接当作正式诊断。
- 不确定表达不能被模型强化成确定诊断。

病历生成要求：

- 保留医生诊断原意和确定程度。
- 区分明确、可能、考虑、待排等表达。
- 不根据症状自行新增疾病诊断。
- 没有明确诊断时留空或标记待医生确认。

可执行检查类型：非空、语义、证据关联、栏位关系、人工判断

本栏位相关的多栏位联合质量要求：

### `diagnosis` + `auxiliary_exam`

要求：

- 诊断不得虚构检查依据。
- 检查结果与诊断不能明显冲突。
- 已安排检查不能被当作诊断依据。

判断方式：大模型语义判断，必要时人工复核

### `diagnosis` + `prescription` + `medical_advice`

要求：

- 诊断、处方和医嘱应逻辑一致。
- 药品、用法、疗程和医嘱不能互相矛盾。
- 不能把缺失的处方信息推导为确定用药方案。

判断方式：结构化判断 + 大模型语义判断，必要时人工复核

### `diagnosis` + `disposition`

要求：

- 处置应与诊断和病情记录一致。
- 必须区分计划处置和已经执行的处置。

判断方式：结构化判断 + 大模型语义判断$emr_desc_diagnosis$, $emr_rules_diagnosis$[
  {
    "code": "present",
    "name": "诊断栏位有明确内容或待确认标记",
    "enabled": true,
    "execution": "program",
    "executor": "diagnosis_present",
    "parameters": {
      "allow_pending_confirmation": true
    }
  },
  {
    "code": "standard_name",
    "name": "诊断名称和编码符合ICD-10目录",
    "enabled": true,
    "execution": "program",
    "executor": "diagnosis_icd10_match",
    "parameters": {
      "catalog": "ICD-10医保2.0版",
      "check_clinical_correctness": false
    }
  },
  {
    "code": "certainty_preserved",
    "name": "诊断保留明确程度",
    "enabled": true,
    "execution": "llm",
    "executor": "diagnosis_certainty_preserved",
    "parameters": {},
    "llm_instruction": "保留明确、可能、考虑、待排等确定程度，不把不确定表达强化为确定诊断。"
  },
  {
    "code": "not_patient_statement",
    "name": "患者自述不能直接作为正式诊断",
    "enabled": true,
    "execution": "llm",
    "executor": "diagnosis_not_patient_statement",
    "parameters": {},
    "llm_instruction": "判断诊断是否有医生明确提出或确认；患者自述的疾病名称不能直接变成正式诊断。"
  },
  {
    "code": "evidence_consistency",
    "name": "诊断与病历依据一致",
    "enabled": true,
    "execution": "llm",
    "executor": "diagnosis_evidence_consistency",
    "related_fields": [
      "chief_complaint",
      "present_illness",
      "physical_exam",
      "auxiliary_exam"
    ],
    "parameters": {},
    "llm_instruction": "只判断诊断是否能在相关栏位中找到明确事实依据；不判断诊断是否临床正确。"
  },
  {
    "code": "prescription_advice_consistency",
    "name": "诊断与处方、医嘱一致",
    "enabled": true,
    "execution": "llm",
    "executor": "diagnosis_prescription_advice_consistency",
    "related_fields": [
      "prescription",
      "medical_advice"
    ],
    "parameters": {},
    "llm_instruction": "检查诊断、处方和医嘱之间是否存在明显事实矛盾，不作临床治疗合理性判断。"
  },
  {
    "code": "disposition_consistency",
    "name": "诊断与处置一致",
    "enabled": true,
    "execution": "llm",
    "executor": "diagnosis_disposition_consistency",
    "related_fields": [
      "disposition"
    ],
    "parameters": {},
    "llm_instruction": "检查处置与病历中明确的诊断和病情是否存在明显事实矛盾，并区分计划处置与已执行处置。"
  }
]$emr_rules_diagnosis$::jsonb),
    ('prescription', $emr_desc_prescription$prescription：处方

适用范围：通用

质量要求：

- 应记录药品或治疗项目、剂量、频次、用法和疗程。
- 缺少具体信息时保留缺失，不得补齐。
- 不能将医生提到的药物示例误写成正式处方。
- 处方应与过敏史、诊断和医嘱保持一致。
- 外用、口服等用法必须有明确依据。

病历生成要求：

- 分别提取药品名称、规格、用法、用量、频次、疗程和数量。
- 区分正式开具、建议使用和举例说明。
- 任何缺失的处方信息保持缺失，不自行补全。

可执行检查类型：结构完整、格式、语义、栏位关系、人工判断$emr_desc_prescription$, $emr_rules_prescription$[
  {
    "code": "item_completeness",
    "name": "处方项目的已知信息完整",
    "enabled": true,
    "execution": "program",
    "executor": "prescription_item_completeness",
    "parameters": {
      "fields": [
        "drug_name",
        "specification",
        "usage",
        "dose",
        "frequency",
        "duration",
        "quantity"
      ]
    }
  },
  {
    "code": "status_distinction",
    "name": "正式处方与建议或举例明确区分",
    "enabled": true,
    "execution": "llm",
    "executor": "prescription_status_distinction",
    "parameters": {},
    "llm_instruction": "区分医生正式开具、建议使用和举例说明；不能把示例药物写成正式处方。"
  },
  {
    "code": "no_missing_detail_invention",
    "name": "缺失处方信息不得自行补齐",
    "enabled": true,
    "execution": "llm",
    "executor": "prescription_no_missing_detail_invention",
    "parameters": {},
    "llm_instruction": "药品名称、剂量、频次、用法或疗程缺失时保留缺失，不自行补全。"
  }
]$emr_rules_prescription$::jsonb),
    ('disposition', $emr_desc_disposition$disposition：处置

适用范围：通用

质量要求：

- 记录本次就诊实际采取的处置措施。
- 应区分已执行、已安排和仅建议。
- 不能把计划写成已经完成。
- 不得增加转诊、住院或检查安排。
- 应与诊断和医嘱保持一致。

病历生成要求：

- 区分检查申请、治疗操作、输液、注射及其他处置。
- 明确记录已执行、已安排或仅建议的状态。
- 不增加转写中没有出现的处置。

可执行检查类型：状态、结构完整、语义、栏位关系、人工判断$emr_desc_disposition$, $emr_rules_disposition$[
  {
    "code": "status_recorded",
    "name": "处置状态已区分",
    "enabled": true,
    "execution": "llm",
    "executor": "disposition_status_recorded",
    "parameters": {},
    "llm_instruction": "对每项处置区分已执行、已安排和仅建议。"
  },
  {
    "code": "no_plan_as_completed",
    "name": "处置计划不能写成已完成",
    "enabled": true,
    "execution": "llm",
    "executor": "disposition_no_plan_as_completed",
    "parameters": {},
    "llm_instruction": "检查已安排或仅建议的检查、治疗、输液、注射和其他处置是否被错误写成已经完成。"
  },
  {
    "code": "no_unstated_action",
    "name": "处置不得增加转写没有出现的动作",
    "enabled": true,
    "execution": "llm",
    "executor": "disposition_no_unstated_action",
    "parameters": {},
    "llm_instruction": "不得新增转诊、住院、检查或其他医疗处置安排。"
  }
]$emr_rules_disposition$::jsonb),
    ('medical_advice', $emr_desc_medical_advice$medical_advice：医嘱

适用范围：通用

质量要求：

- 记录医生明确交代的用药、观察、生活和注意事项。
- 必须保留明确的时间、频次和执行条件。
- 不得增加医生没有说过的医学建议。
- 不得把模型推断写成医嘱。
- 应与处方和诊断保持一致。

病历生成要求：

- 只提取医生明确交代的执行事项。
- 保留时间、频次、条件和异常处理要求。
- 将患者问题和医生回答区分开。

可执行检查类型：非空、语义、栏位关系、人工判断$emr_desc_medical_advice$, $emr_rules_medical_advice$[
  {
    "code": "explicit_only",
    "name": "医嘱只记录医生明确交代内容",
    "enabled": true,
    "execution": "llm",
    "executor": "medical_advice_explicit_only",
    "parameters": {},
    "llm_instruction": "只记录医生明确交代的用药、观察、生活和注意事项，不把模型推断写成医嘱。"
  },
  {
    "code": "conditions_preserved",
    "name": "医嘱保留时间、频次和执行条件",
    "enabled": true,
    "execution": "llm",
    "executor": "medical_advice_conditions_preserved",
    "parameters": {},
    "llm_instruction": "检查转写明确出现的时间、频次、执行条件和异常处理要求是否被保留。"
  }
]$emr_rules_medical_advice$::jsonb),
    ('followup', $emr_desc_followup$followup：复诊安排

适用范围：通用

质量要求：

- 只记录医生明确交代的复诊、复查或联系安排。
- 明确日期或相对时间应保留。
- 模糊时间不能擅自转换成具体日期。
- 没有明确安排时保持为空。
- 不得为了提高复诊率自动生成复诊计划。

病历生成要求：

- 保留医生明确说出的日期、相对时间和复诊条件。
- 区分明确安排、建议和患者自行计划。
- 无明确安排时不生成日期。

可执行检查类型：日期格式、时间语义、非空、人工判断$emr_desc_followup$, $emr_rules_followup$[
  {
    "code": "explicit_plan_only",
    "name": "复诊安排只记录明确计划",
    "enabled": true,
    "execution": "llm",
    "executor": "followup_explicit_plan_only",
    "parameters": {},
    "llm_instruction": "只记录医生明确交代的复诊、复查或联系安排；没有明确安排时保持为空。"
  },
  {
    "code": "date_format",
    "name": "复诊日期格式有效",
    "enabled": true,
    "execution": "program",
    "executor": "followup_date_format",
    "parameters": {
      "date_format": "YYYY-MM-DD"
    }
  },
  {
    "code": "ambiguous_time_not_converted",
    "name": "模糊时间不能转换成具体日期",
    "enabled": true,
    "execution": "llm",
    "executor": "followup_ambiguous_time_not_converted",
    "parameters": {},
    "llm_instruction": "不得把最近、过几天、月初、下个月等模糊表达转换为没有依据的具体日期。"
  }
]$emr_rules_followup$::jsonb),
    ('supplement', $emr_desc_supplement$supplement：补充说明

适用范围：通用

质量要求：

- 记录无法归入其他栏位但确有依据的补充事实。
- 不得重复大段内容。
- 不得用于隐藏诊断、处方或医嘱。
- 不得加入模型推测和未经确认的信息。

病历生成要求：

- 仅保留无法归入其他栏位且确有保存必要的事实。
- 优先将内容归入正确栏位，不用补充说明替代正式栏位。

可执行检查类型：非空、语义、人工判断

## 二、妇科栏位$emr_desc_supplement$, $emr_rules_supplement$[
  {
    "code": "proper_use",
    "name": "补充说明只保存无法归类的必要事实",
    "enabled": true,
    "execution": "llm",
    "executor": "supplement_proper_use",
    "parameters": {},
    "llm_instruction": "判断内容是否确有保存必要且无法归入其他栏位，不用补充说明隐藏诊断、处方或医嘱。"
  },
  {
    "code": "no_invention",
    "name": "补充说明不得加入推测",
    "enabled": true,
    "execution": "llm",
    "executor": "supplement_no_invention",
    "parameters": {},
    "llm_instruction": "补充说明只能来自转写中的明确事实，不得加入模型推测或未经确认的信息。"
  }
]$emr_rules_supplement$::jsonb),
    ('menstrual_history', $emr_desc_menstrual_history$menstrual_history：月经史

适用范围：妇科

质量要求：

- 应记录月经周期、经期、经量和规律性等明确内容。
- 未询问的项目不能写成正常。
- 异常情况应保留患者原始表述。
- 不能根据年龄或症状推测月经状态。

病历生成要求：

- 按周期、经期、经量、末次月经、痛经和其他情况组织。
- 未提及的项目留空或标记未询问。

可执行检查类型：结构完整、日期、数值格式、语义、人工判断$emr_desc_menstrual_history$, $emr_rules_menstrual_history$[
  {
    "code": "known_items_recorded",
    "name": "月经史保留明确项目",
    "enabled": true,
    "execution": "llm",
    "executor": "menstrual_history_known_items_recorded",
    "parameters": {},
    "llm_instruction": "保留转写中明确出现的周期、经期、经量、规律性、痛经等内容；未询问不能写成正常。"
  },
  {
    "code": "no_inference",
    "name": "月经史不得根据年龄或症状推测",
    "enabled": true,
    "execution": "llm",
    "executor": "menstrual_history_no_inference",
    "parameters": {},
    "llm_instruction": "不得根据年龄、性别或症状推测月经状态。"
  }
]$emr_rules_menstrual_history$::jsonb),
    ('last_menstrual_period', $emr_desc_last_menstrual_period$last_menstrual_period：末次月经

适用范围：妇科

质量要求：

- 只能填写明确日期或明确时间表达。
- 日期必须合法。
- 模糊表达不得转换成具体日期。
- 未提及时保持为空。
- 应与孕产史和妊娠相关内容保持一致。

病历生成要求：

- 只记录明确的末次月经日期。
- 对“上个月”“月初”等模糊表达不擅自转换日期。

可执行检查类型：日期格式、时间语义、栏位关系、人工判断$emr_desc_last_menstrual_period$, $emr_rules_last_menstrual_period$[
  {
    "code": "valid_date",
    "name": "末次月经日期合法",
    "enabled": true,
    "execution": "program",
    "executor": "last_menstrual_period_valid_date",
    "parameters": {
      "date_format": "YYYY-MM-DD",
      "allow_empty": true
    }
  },
  {
    "code": "no_ambiguous_conversion",
    "name": "模糊月经时间不能转换为具体日期",
    "enabled": true,
    "execution": "llm",
    "executor": "last_menstrual_period_no_ambiguous_conversion",
    "parameters": {},
    "llm_instruction": "对上个月、月初等模糊表达保持模糊，不擅自转换成具体日期。"
  }
]$emr_rules_last_menstrual_period$::jsonb),
    ('marital_reproductive_history', $emr_desc_marital_reproductive_history$marital_reproductive_history：婚育史

适用范围：妇科

质量要求：

- 记录婚姻和生育情况的明确陈述。
- 应区分婚姻史和生育史。
- 不得根据年龄或称谓推断。
- 未询问不能写成无婚育史。

病历生成要求：

- 分别记录婚姻状态、妊娠、生育和子女情况。
- 仅保留明确陈述。

可执行检查类型：结构完整、数值格式、语义、人工判断$emr_desc_marital_reproductive_history$, $emr_rules_marital_reproductive_history$[
  {
    "code": "known_items_recorded",
    "name": "婚育史保留明确陈述",
    "enabled": true,
    "execution": "llm",
    "executor": "marital_reproductive_history_known_items_recorded",
    "parameters": {},
    "llm_instruction": "分别记录明确出现的婚姻状态、妊娠、生育和子女信息，不把未询问写成无。"
  },
  {
    "code": "no_inference",
    "name": "婚育史不得自行推断",
    "enabled": true,
    "execution": "llm",
    "executor": "marital_reproductive_history_no_inference",
    "parameters": {},
    "llm_instruction": "不得根据年龄、称谓或其他信息推断婚育情况。"
  }
]$emr_rules_marital_reproductive_history$::jsonb),
    ('obstetric_history', $emr_desc_obstetric_history$obstetric_history：孕产史

适用范围：妇科

质量要求：

- 记录孕次、产次、流产、分娩及相关情况。
- 数量必须来自明确事实。
- 不确定或无法确认时标记待确认。
- 不得从“怀孕过”推导完整孕产次数。

病历生成要求：

- 分别记录孕次、产次、流产及分娩细节。
- 不自行推算孕产次数。

可执行检查类型：结构完整、数值格式、语义、栏位关系、人工判断$emr_desc_obstetric_history$, $emr_rules_obstetric_history$[
  {
    "code": "numbers_format",
    "name": "孕产史数量格式有效",
    "enabled": true,
    "execution": "program",
    "executor": "obstetric_history_numbers_format",
    "parameters": {
      "non_negative": true,
      "integer": true
    }
  },
  {
    "code": "explicit_facts_only",
    "name": "孕产史数量只来自明确事实",
    "enabled": true,
    "execution": "llm",
    "executor": "obstetric_history_explicit_facts_only",
    "parameters": {},
    "llm_instruction": "不得从怀孕过、生产过等不完整表述推算完整孕次、产次或流产次数。"
  }
]$emr_rules_obstetric_history$::jsonb),
    ('gynecology_specialty_exam', $emr_desc_gynecology_specialty_exam$gynecology_specialty_exam：妇科专科查体

适用范围：妇科

质量要求：

- 只记录实际完成的妇科检查。
- 应区分检查所见和医生判断。
- 未检查不能写成正常。
- 不得根据症状虚构妇科体征。
- 涉及隐私的内容必须有明确记录依据。

病历生成要求：

- 只提取医生明确描述的妇科检查所见。
- 区分未检查、已检查未见异常和发现异常。

可执行检查类型：非空、语义、结构关系、人工判断$emr_desc_gynecology_specialty_exam$, $emr_rules_gynecology_specialty_exam$[
  {
    "code": "completed_exam_only",
    "name": "妇科查体只记录实际完成内容",
    "enabled": true,
    "execution": "llm",
    "executor": "gynecology_specialty_exam_completed_exam_only",
    "parameters": {},
    "llm_instruction": "只记录医生明确描述的实际妇科检查所见，区分未检查、已检查未见异常和发现异常。"
  },
  {
    "code": "no_unexamined_normal",
    "name": "未检查不能写成正常",
    "enabled": true,
    "execution": "llm",
    "executor": "gynecology_specialty_exam_no_unexamined_normal",
    "parameters": {},
    "llm_instruction": "检查正常结果是否有实际检查依据，不根据症状补写妇科体征。"
  }
]$emr_rules_gynecology_specialty_exam$::jsonb),
    ('gynecology_auxiliary_exam', $emr_desc_gynecology_auxiliary_exam$gynecology_auxiliary_exam：妇科相关辅助检查

适用范围：妇科

质量要求：

- 记录检查项目、时间和结果。
- 区分已完成、已安排和未完成。
- 不能补充检查结果。
- 应与妇科诊断和治疗计划保持一致。

病历生成要求：

- 分别记录检查名称、时间、结果和来源。
- 不把检查计划当成检查结果。

可执行检查类型：结构完整、状态、日期、语义、栏位关系

## 三、中医栏位$emr_desc_gynecology_auxiliary_exam$, $emr_rules_gynecology_auxiliary_exam$[
  {
    "code": "status_and_result",
    "name": "妇科辅助检查区分状态和结果",
    "enabled": true,
    "execution": "llm",
    "executor": "gynecology_auxiliary_exam_status_and_result",
    "parameters": {},
    "llm_instruction": "区分已完成、已安排和未完成，只有明确检查结果才能写入结果栏位。"
  },
  {
    "code": "no_result_invention",
    "name": "妇科辅助检查不得补充结果",
    "enabled": true,
    "execution": "llm",
    "executor": "gynecology_auxiliary_exam_no_result_invention",
    "parameters": {},
    "llm_instruction": "不得根据妇科诊断或治疗计划推测检查结果。"
  }
]$emr_rules_gynecology_auxiliary_exam$::jsonb),
    ('tcm_diagnosis', $emr_desc_tcm_diagnosis$tcm_diagnosis：中医诊断

适用范围：中医

质量要求：

- 记录明确的中医病名或诊断。
- 不得把证候推测成正式诊断。
- 应与四诊、辨证和治法一致。
- 不明确时保留待确认状态。

病历生成要求：

- 保留医生明确提出的中医病名和诊断。
- 不从症状自行生成中医诊断。

可执行检查类型：非空、语义、栏位关系、人工判断

本栏位相关的多栏位联合质量要求：

### 中医联合要求

涉及栏位：

- `tcm_diagnosis`
- `tcm_four_examinations`
- `tcm_pattern_differentiation`
- `tcm_treatment_principle`

要求：

- 中医诊断、四诊、辨证和治法应保持一致。
- 不得以模型推理替代医生明确的辨证和治法。
- 四诊信息不足时，相关结论应标记无法确认。

判断方式：大模型语义判断，必要时人工复核$emr_desc_tcm_diagnosis$, $emr_rules_tcm_diagnosis$[
  {
    "code": "explicit_present",
    "name": "中医诊断有明确内容",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_diagnosis_explicit_present",
    "parameters": {},
    "llm_instruction": "只记录医生明确提出的中医病名或诊断，不把模型推测的证候写成诊断。"
  },
  {
    "code": "four_examinations_consistency",
    "name": "中医诊断与四诊、辨证、治法一致",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_diagnosis_four_examinations_consistency",
    "related_fields": [
      "tcm_four_examinations",
      "tcm_pattern_differentiation",
      "tcm_treatment_principle"
    ],
    "parameters": {},
    "llm_instruction": "只检查病历各栏位是否存在明显事实矛盾，不以模型推理替代医生辨证和治法。"
  }
]$emr_rules_tcm_diagnosis$::jsonb),
    ('tcm_four_examinations', $emr_desc_tcm_four_examinations$tcm_four_examinations：中医四诊

适用范围：中医

质量要求：

- 应能反映望、闻、问、切四类信息。
- 只记录实际询问或观察到的内容。
- 未涉及的诊法不能自动补全。
- 不得将普通症状强行转换为中医术语。

病历生成要求：

- 分别组织望诊、闻诊、问诊和切诊信息。
- 未涉及的诊法留空，不强行补全。

可执行检查类型：结构完整、语义、栏位关系、人工判断$emr_desc_tcm_four_examinations$, $emr_rules_tcm_four_examinations$[
  {
    "code": "source_items_only",
    "name": "四诊只记录实际问诊或观察内容",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_four_examinations_source_items_only",
    "parameters": {},
    "llm_instruction": "分别检查望、闻、问、切信息是否来自实际问诊或观察；未涉及的诊法留空，不强行补全。"
  },
  {
    "code": "no_term_invention",
    "name": "四诊不得强行转换为中医术语",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_four_examinations_no_term_invention",
    "parameters": {},
    "llm_instruction": "普通症状不能未经医生明确表达就被改写成中医术语或证候。"
  }
]$emr_rules_tcm_four_examinations$::jsonb),
    ('tcm_inspection', $emr_desc_tcm_inspection$tcm_inspection：望诊

适用范围：中医

质量要求：

- 记录面色、形态、舌象等实际观察内容。
- 没有观察依据不能填写。
- 不得把患者自述写成望诊结果。

病历生成要求：

- 只记录明确观察到的面色、形态和舌象。
- 区分观察内容和患者自述。

可执行检查类型：非空、语义、栏位关系、人工判断$emr_desc_tcm_inspection$, $emr_rules_tcm_inspection$[
  {
    "code": "observation_basis",
    "name": "望诊有实际观察依据",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_inspection_observation_basis",
    "parameters": {},
    "llm_instruction": "只记录实际观察到的面色、形态、舌象等内容，不能把患者自述写成望诊结果。"
  }
]$emr_rules_tcm_inspection$::jsonb),
    ('tcm_auscultation_olfaction', $emr_desc_tcm_auscultation_olfaction$tcm_auscultation_olfaction：闻诊

适用范围：中医

质量要求：

- 记录声音、气味等实际信息。
- 未观察不能写成正常。
- 不得根据疾病推测相关表现。

病历生成要求：

- 只提取明确的声音和气味观察结果。
- 未观察时留空。

可执行检查类型：非空、语义、人工判断$emr_desc_tcm_auscultation_olfaction$, $emr_rules_tcm_auscultation_olfaction$[
  {
    "code": "observation_basis",
    "name": "闻诊有实际观察依据",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_auscultation_olfaction_observation_basis",
    "parameters": {},
    "llm_instruction": "只记录明确观察到的声音和气味信息，未观察不能写成正常。"
  }
]$emr_rules_tcm_auscultation_olfaction$::jsonb),
    ('tcm_inquiry', $emr_desc_tcm_inquiry$tcm_inquiry：问诊

适用范围：中医

质量要求：

- 记录中医问诊中明确询问和回答的内容。
- 应保留重要症状、否定信息、寒热、汗出、饮食、睡眠、二便等信息。
- 未询问不能写成否认。
- 不得脱离原话推导证候。

病历生成要求：

- 只整理明确问答内容。
- 不将问诊事实直接改写为中医证候结论。

可执行检查类型：非空、结构完整、语义、栏位关系$emr_desc_tcm_inquiry$, $emr_rules_tcm_inquiry$[
  {
    "code": "question_answer_facts",
    "name": "问诊保留明确问答事实",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_inquiry_question_answer_facts",
    "parameters": {},
    "llm_instruction": "保留明确问答中的症状、否定信息、寒热、汗出、饮食、睡眠和二便等内容；未询问不能写成否认。"
  },
  {
    "code": "no_pattern_inference",
    "name": "问诊事实不能直接推导证候",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_inquiry_no_pattern_inference",
    "parameters": {},
    "llm_instruction": "问诊栏位只整理事实，不把模型推理的证候结论写入问诊内容。"
  }
]$emr_rules_tcm_inquiry$::jsonb),
    ('tcm_palpation', $emr_desc_tcm_palpation$tcm_palpation：切诊

适用范围：中医

质量要求：

- 记录脉象、按诊等实际检查结果。
- 未实施切诊不能填写正常。
- 不得由其他症状推测脉象。

病历生成要求：

- 只记录明确实施切诊后的结果。
- 未实施时留空或标记未检查。

可执行检查类型：非空、语义、人工判断$emr_desc_tcm_palpation$, $emr_rules_tcm_palpation$[
  {
    "code": "actual_exam_basis",
    "name": "切诊有实际检查依据",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_palpation_actual_exam_basis",
    "parameters": {},
    "llm_instruction": "只记录实际实施切诊后的脉象或按诊结果，未实施不能填写正常。"
  }
]$emr_rules_tcm_palpation$::jsonb),
    ('tcm_tongue', $emr_desc_tcm_tongue$tcm_tongue：舌象

适用范围：中医

质量要求：

- 记录舌质、舌苔等明确观察结果。
- 必须区分实际观察和模型推测。
- 未观察时留空。

病历生成要求：

- 按舌质、舌苔、湿润程度和其他明确内容组织。
- 不根据症状推测舌象。

可执行检查类型：结构完整、语义、人工判断$emr_desc_tcm_tongue$, $emr_rules_tcm_tongue$[
  {
    "code": "observation_basis",
    "name": "舌象有明确观察依据",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_tongue_observation_basis",
    "parameters": {},
    "llm_instruction": "只记录明确观察到的舌质、舌苔和其他舌象，不根据症状推测。"
  }
]$emr_rules_tcm_tongue$::jsonb),
    ('tcm_pulse', $emr_desc_tcm_pulse$tcm_pulse：脉象

适用范围：中医

质量要求：

- 记录明确的脉象描述。
- 不得根据症状推测脉象。
- 医生未明确判断时不能自动生成。

病历生成要求：

- 只提取医生明确描述的脉象。
- 未切脉或未描述时留空。

可执行检查类型：非空、语义、人工判断$emr_desc_tcm_pulse$, $emr_rules_tcm_pulse$[
  {
    "code": "explicit_observation",
    "name": "脉象有明确检查描述",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_pulse_explicit_observation",
    "parameters": {},
    "llm_instruction": "医生未明确描述脉象时留空，不根据症状或诊断自动生成脉象。"
  }
]$emr_rules_tcm_pulse$::jsonb),
    ('tcm_pattern_differentiation', $emr_desc_tcm_pattern_differentiation$tcm_pattern_differentiation：中医辨证

适用范围：中医

质量要求：

- 记录医生明确提出的证候和辨证依据。
- 应与四诊结果一致。
- 不得由模型自行完成辨证。
- 证候不明确时标记待确认。

病历生成要求：

- 保留医生明确提出的证候和依据。
- 不以模型推理替代医生辨证。

可执行检查类型：非空、语义、栏位关系、人工判断$emr_desc_tcm_pattern_differentiation$, $emr_rules_tcm_pattern_differentiation$[
  {
    "code": "explicit_pattern",
    "name": "辨证来自医生明确表达",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_pattern_differentiation_explicit_pattern",
    "parameters": {},
    "llm_instruction": "只保留医生明确提出的证候和辨证依据，不以模型推理替代医生辨证。"
  },
  {
    "code": "four_examinations_consistency",
    "name": "辨证与四诊和中医诊断一致",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_pattern_differentiation_four_examinations_consistency",
    "related_fields": [
      "tcm_four_examinations",
      "tcm_diagnosis"
    ],
    "parameters": {},
    "llm_instruction": "只判断栏位之间是否存在明显事实矛盾；四诊不足时标记无法确认，不自行补全。"
  }
]$emr_rules_tcm_pattern_differentiation$::jsonb),
    ('tcm_treatment_principle', $emr_desc_tcm_treatment_principle$tcm_treatment_principle：治法

适用范围：中医

质量要求：

- 记录医生明确提出的治疗原则。
- 应与中医诊断和辨证一致。
- 不得根据诊断自动生成治法。
- 未明确时保持为空。

病历生成要求：

- 只记录医生明确说出的治法。
- 不根据辨证自动补写治疗原则。

可执行检查类型：非空、语义、栏位关系、人工判断$emr_desc_tcm_treatment_principle$, $emr_rules_tcm_treatment_principle$[
  {
    "code": "explicit_treatment",
    "name": "治法来自医生明确表达",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_treatment_principle_explicit_treatment",
    "parameters": {},
    "llm_instruction": "只记录医生明确说出的治疗原则，不根据诊断或辨证自动补写治法。"
  },
  {
    "code": "diagnosis_pattern_consistency",
    "name": "治法与中医诊断、辨证一致",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_treatment_principle_diagnosis_pattern_consistency",
    "related_fields": [
      "tcm_diagnosis",
      "tcm_pattern_differentiation"
    ],
    "parameters": {},
    "llm_instruction": "只检查病历中的明确表达是否相互矛盾，不判断治法的临床正确性。"
  }
]$emr_rules_tcm_treatment_principle$::jsonb),
    ('tcm_herbal_prescription', $emr_desc_tcm_herbal_prescription$tcm_herbal_prescription：中药处方

适用范围：中医

质量要求：

- 记录药材、剂量、用法、频次和疗程。
- 药材和剂量必须有明确依据。
- 不得根据证候自动补方。
- 应与辨证、治法和过敏史保持一致。

病历生成要求：

- 分别记录药材、剂量、用法、频次和疗程。
- 不根据证候自动生成药材或剂量。

可执行检查类型：结构完整、格式、语义、栏位关系、人工判断

## 四、口腔栏位$emr_desc_tcm_herbal_prescription$, $emr_rules_tcm_herbal_prescription$[
  {
    "code": "known_items_complete",
    "name": "中药处方保留已知药材和用法信息",
    "enabled": true,
    "execution": "program",
    "executor": "tcm_herbal_prescription_known_items_complete",
    "parameters": {
      "fields": [
        "herb_name",
        "dose",
        "usage",
        "frequency",
        "duration"
      ]
    }
  },
  {
    "code": "no_formula_invention",
    "name": "中药处方不得根据证候补方",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_herbal_prescription_no_formula_invention",
    "parameters": {},
    "llm_instruction": "药材和剂量必须有明确依据，不根据证候自动生成药材、剂量或完整方剂。"
  },
  {
    "code": "pattern_treatment_consistency",
    "name": "中药处方与辨证、治法一致",
    "enabled": true,
    "execution": "llm",
    "executor": "tcm_herbal_prescription_pattern_treatment_consistency",
    "related_fields": [
      "tcm_pattern_differentiation",
      "tcm_treatment_principle",
      "allergy_history"
    ],
    "parameters": {},
    "llm_instruction": "只判断明确记录之间是否存在明显冲突，不作中药临床疗效或处方合理性判断。"
  }
]$emr_rules_tcm_herbal_prescription$::jsonb),
    ('oral_tooth_position', $emr_desc_oral_tooth_position$oral_tooth_position：牙位

适用范围：口腔

质量要求：

- 记录明确的牙位或治疗部位。
- 牙位表达应统一。
- 不明确时不能推测。
- 应与口腔检查、影像和治疗计划一致。

病历生成要求：

- 只记录明确出现的牙位、牙面或区域。
- 保留原始明确表达，不自行转换不确定牙位。

可执行检查类型：结构完整、格式、语义、栏位关系

本栏位相关的多栏位联合质量要求：

### 口腔联合要求

涉及栏位：

- `oral_tooth_position`
- `oral_specialty_exam`
- `oral_periodontal_or_dental_condition`
- `oral_imaging_findings`
- `oral_treatment_plan`

要求：

- 牙位、检查结果和治疗部位必须对应。
- 影像所见、牙体牙周情况和治疗计划不能互相矛盾。
- 不明确的牙位不能被模型自行推断。

判断方式：结构化判断 + 大模型语义判断，必要时人工复核$emr_desc_oral_tooth_position$, $emr_rules_oral_tooth_position$[
  {
    "code": "explicit_position",
    "name": "牙位或治疗部位明确",
    "enabled": true,
    "execution": "llm",
    "executor": "oral_tooth_position_explicit_position",
    "parameters": {},
    "llm_instruction": "只记录转写中明确出现的牙位、牙面或区域，不自行转换不确定牙位。"
  },
  {
    "code": "related_content_consistency",
    "name": "牙位与检查、影像和治疗计划对应",
    "enabled": true,
    "execution": "llm",
    "executor": "oral_tooth_position_related_content_consistency",
    "related_fields": [
      "oral_specialty_exam",
      "oral_periodontal_or_dental_condition",
      "oral_imaging_findings",
      "oral_treatment_plan"
    ],
    "parameters": {},
    "llm_instruction": "比较牙位与相关检查、影像和治疗计划中的明确部位，不把疼痛部位自动当成确诊牙位。"
  }
]$emr_rules_oral_tooth_position$::jsonb),
    ('oral_specialty_exam', $emr_desc_oral_specialty_exam$oral_specialty_exam：口腔专科检查

适用范围：口腔

质量要求：

- 记录实际检查到的口腔情况。
- 应区分检查所见和诊断。
- 未检查不能写正常。
- 不得根据主诉虚构牙体或牙周表现。

病历生成要求：

- 只记录实际检查所见。
- 区分检查结果和医生诊断。

可执行检查类型：非空、语义、栏位关系、人工判断$emr_desc_oral_specialty_exam$, $emr_rules_oral_specialty_exam$[
  {
    "code": "actual_findings_only",
    "name": "口腔专科检查只记录实际所见",
    "enabled": true,
    "execution": "llm",
    "executor": "oral_specialty_exam_actual_findings_only",
    "parameters": {},
    "llm_instruction": "只记录实际检查所见，区分检查结果和医生诊断，不根据主诉虚构牙体或牙周表现。"
  }
]$emr_rules_oral_specialty_exam$::jsonb),
    ('oral_periodontal_or_dental_condition', $emr_desc_oral_periodontal_or_dental_condition$oral_periodontal_or_dental_condition：牙周或牙体情况

适用范围：口腔

质量要求：

- 记录明确的牙周、牙体异常。
- 应与牙位对应。
- 不能把患者疼痛部位直接当作疾病诊断。
- 未明确检查结果时保持待确认。

病历生成要求：

- 记录明确的牙周或牙体检查结果。
- 不将疼痛部位直接改写成疾病诊断。

可执行检查类型：非空、语义、栏位关系、人工判断$emr_desc_oral_periodontal_or_dental_condition$, $emr_rules_oral_periodontal_or_dental_condition$[
  {
    "code": "explicit_condition",
    "name": "牙周或牙体异常有明确依据",
    "enabled": true,
    "execution": "llm",
    "executor": "oral_periodontal_or_dental_condition_explicit_condition",
    "parameters": {},
    "llm_instruction": "只记录明确的牙周或牙体检查结果，不能把患者疼痛部位直接改写成疾病诊断。"
  }
]$emr_rules_oral_periodontal_or_dental_condition$::jsonb),
    ('oral_imaging_findings', $emr_desc_oral_imaging_findings$oral_imaging_findings：影像所见

适用范围：口腔

质量要求：

- 记录检查项目和明确影像结果。
- 区分已检查、已安排和未检查。
- 不能补充影像结论。
- 应与牙位、诊断和治疗计划一致。

病历生成要求：

- 只记录明确影像检查结果。
- 区分已完成和已安排检查。

可执行检查类型：结构完整、状态、语义、栏位关系$emr_desc_oral_imaging_findings$, $emr_rules_oral_imaging_findings$[
  {
    "code": "status_and_result",
    "name": "影像检查区分状态和结果",
    "enabled": true,
    "execution": "llm",
    "executor": "oral_imaging_findings_status_and_result",
    "parameters": {},
    "llm_instruction": "区分已检查、已安排和未检查，只有明确影像结果才能写入影像所见。"
  },
  {
    "code": "no_result_invention",
    "name": "影像所见不得补充结论",
    "enabled": true,
    "execution": "llm",
    "executor": "oral_imaging_findings_no_result_invention",
    "parameters": {},
    "llm_instruction": "不得根据牙位、症状、诊断或治疗计划补写影像结果。"
  }
]$emr_rules_oral_imaging_findings$::jsonb),
    ('oral_treatment_plan', $emr_desc_oral_treatment_plan$oral_treatment_plan：口腔治疗计划

适用范围：口腔

质量要求：

- 记录医生明确提出的治疗方案。
- 应与牙位、检查和诊断对应。
- 区分计划治疗和已完成治疗。
- 不得自行增加治疗项目或疗程。

病历生成要求：

- 记录医生明确提出的治疗方案、分期和沟通结果。
- 区分治疗计划与已经完成的治疗。

可执行检查类型：非空、语义、栏位关系、人工判断

## 五、儿科栏位$emr_desc_oral_treatment_plan$, $emr_rules_oral_treatment_plan$[
  {
    "code": "explicit_plan",
    "name": "治疗计划来自医生明确表达",
    "enabled": true,
    "execution": "llm",
    "executor": "oral_treatment_plan_explicit_plan",
    "parameters": {},
    "llm_instruction": "只记录医生明确提出的治疗方案、分期和沟通结果，不自行增加治疗项目或疗程。"
  },
  {
    "code": "planned_vs_completed",
    "name": "治疗计划与已完成治疗明确区分",
    "enabled": true,
    "execution": "llm",
    "executor": "oral_treatment_plan_planned_vs_completed",
    "parameters": {},
    "llm_instruction": "区分计划治疗和已经完成的治疗，不能把计划写成已执行。"
  }
]$emr_rules_oral_treatment_plan$::jsonb),
    ('pediatric_birth_history', $emr_desc_pediatric_birth_history$pediatric_birth_history：出生史

适用范围：儿科

质量要求：

- 记录出生方式、孕周、出生体重和出生异常等明确内容。
- 未询问不能写正常。
- 不得根据儿童年龄推测出生情况。

病历生成要求：

- 按孕周、分娩方式、出生体重和围产期情况组织。
- 未询问项目留空。

可执行检查类型：结构完整、数值格式、语义、人工判断$emr_desc_pediatric_birth_history$, $emr_rules_pediatric_birth_history$[
  {
    "code": "known_items_recorded",
    "name": "出生史保留明确项目",
    "enabled": true,
    "execution": "llm",
    "executor": "pediatric_birth_history_known_items_recorded",
    "parameters": {},
    "llm_instruction": "保留明确出现的孕周、分娩方式、出生体重和围产期情况，未询问项目留空。"
  },
  {
    "code": "no_inference",
    "name": "出生史不得根据年龄推测",
    "enabled": true,
    "execution": "llm",
    "executor": "pediatric_birth_history_no_inference",
    "parameters": {},
    "llm_instruction": "不得根据儿童当前年龄或其他信息推测出生情况。"
  }
]$emr_rules_pediatric_birth_history$::jsonb),
    ('pediatric_feeding_history', $emr_desc_pediatric_feeding_history$pediatric_feeding_history：喂养史

适用范围：儿科

质量要求：

- 记录喂养方式、喂养量、辅食和喂养困难等明确内容。
- 不得根据年龄推测喂养情况。
- 未询问项目保持未记录。

病历生成要求：

- 按喂养方式、辅食、食欲和喂养困难组织。
- 不依据年龄自动推断喂养方式或喂养量。

可执行检查类型：结构完整、数值格式、语义、人工判断$emr_desc_pediatric_feeding_history$, $emr_rules_pediatric_feeding_history$[
  {
    "code": "known_items_recorded",
    "name": "喂养史保留明确项目",
    "enabled": true,
    "execution": "llm",
    "executor": "pediatric_feeding_history_known_items_recorded",
    "parameters": {},
    "llm_instruction": "保留明确出现的喂养方式、喂养量、辅食、食欲和喂养困难，未询问项目留空。"
  },
  {
    "code": "no_inference",
    "name": "喂养史不得根据年龄推测",
    "enabled": true,
    "execution": "llm",
    "executor": "pediatric_feeding_history_no_inference",
    "parameters": {},
    "llm_instruction": "不得根据年龄自动推断喂养方式或喂养量。"
  }
]$emr_rules_pediatric_feeding_history$::jsonb),
    ('pediatric_guardian_statement', $emr_desc_pediatric_guardian_statement$pediatric_guardian_statement：监护人陈述

适用范围：儿科

质量要求：

- 区分监护人陈述和医生观察。
- 保留监护人对症状、病程和用药的明确描述。
- 不得将监护人意见直接改写成医学结论。
- 不得补充监护人没有表达的信息。

病历生成要求：

- 明确标记信息来源为监护人陈述。
- 区分监护人观察、监护人判断和医生判断。

可执行检查类型：非空、语义、人工判断$emr_desc_pediatric_guardian_statement$, $emr_rules_pediatric_guardian_statement$[
  {
    "code": "source_distinction",
    "name": "监护人陈述与医生观察明确区分",
    "enabled": true,
    "execution": "llm",
    "executor": "pediatric_guardian_statement_source_distinction",
    "parameters": {},
    "llm_instruction": "明确标记信息来源为监护人陈述，区分监护人观察、监护人判断和医生判断。"
  },
  {
    "code": "no_inference",
    "name": "监护人陈述不得改写成医学结论",
    "enabled": true,
    "execution": "llm",
    "executor": "pediatric_guardian_statement_no_inference",
    "parameters": {},
    "llm_instruction": "不得将监护人意见或描述直接强化为医生诊断、检查结果或医学结论。"
  }
]$emr_rules_pediatric_guardian_statement$::jsonb),
    ('pediatric_weight', $emr_desc_pediatric_weight$pediatric_weight：儿童体重

适用范围：儿科

质量要求：

- 必须记录明确数值和单位。
- 数值应与测量时间对应。
- 不得根据年龄估算体重。
- 体重应与儿童用药依据保持一致。

病历生成要求：

- 只记录明确测量或明确提供的体重数值和单位。
- 不根据年龄、身高或疾病估算体重。

可执行检查类型：数值格式、单位、时间、栏位关系

本栏位相关的多栏位联合质量要求：

### 儿科联合要求

涉及栏位：

- `pediatric_weight`
- `pediatric_medication_basis`
- `prescription`

要求：

- 儿童用药依据应与体重和实际处方一致。
- 缺少体重或剂量依据时不能自行计算或补齐。
- 处方中的儿童用药信息应与病历中明确记录的依据一致。

判断方式：结构化判断 + 大模型语义判断，必要时人工复核

## 七、明确不属于本文件的内容

以下内容不属于栏位质量标准：

- 模板是否发布；
- 医生和机构权限；
- 病历状态流转；
- 操作人和修改原因；
- 审计留痕；
- 保存、提交、确认、归档的工作流约束；
- 某一次病历的具体检查结果。

这些内容由模板治理、权限、工作流、审计和质检结果模块分别负责。$emr_desc_pediatric_weight$, $emr_rules_pediatric_weight$[
  {
    "code": "value_and_unit",
    "name": "儿童体重有明确数值和单位",
    "enabled": true,
    "execution": "program",
    "executor": "pediatric_weight_value_and_unit",
    "parameters": {
      "unit": "kg",
      "allow_empty": true,
      "non_negative": true
    }
  },
  {
    "code": "no_age_estimation",
    "name": "儿童体重不得根据年龄估算",
    "enabled": true,
    "execution": "llm",
    "executor": "pediatric_weight_no_age_estimation",
    "parameters": {},
    "llm_instruction": "只接受明确测量或明确提供的体重，不根据年龄、身高或疾病估算。"
  },
  {
    "code": "medication_basis_consistency",
    "name": "儿童体重与用药依据一致",
    "enabled": true,
    "execution": "llm",
    "executor": "pediatric_weight_medication_basis_consistency",
    "related_fields": [
      "pediatric_medication_basis",
      "prescription"
    ],
    "parameters": {},
    "llm_instruction": "只检查病历中明确记录的体重、用药依据和处方是否互相矛盾，不计算剂量，不判断剂量是否临床合理。"
  }
]$emr_rules_pediatric_weight$::jsonb),
    ('pediatric_medication_basis', $emr_desc_pediatric_medication_basis$pediatric_medication_basis：儿童特殊用药依据

适用范围：儿科

质量要求：

- 记录药物、体重、年龄、剂量计算和调整依据。
- 缺少依据时必须明确标记缺失。
- 不得自行计算剂量。
- 不得把常规经验写成医生实际依据。
- 应与处方、体重和诊断保持一致。

病历生成要求：

- 只记录医生明确说明的用药依据。
- 不自行计算剂量或补充年龄、体重依据。
- 缺少依据时保留缺失并标记待医生确认。

可执行检查类型：结构完整、数值格式、语义、栏位关系、人工判断$emr_desc_pediatric_medication_basis$, $emr_rules_pediatric_medication_basis$[
  {
    "code": "explicit_basis_recorded",
    "name": "儿童用药依据保留明确内容",
    "enabled": true,
    "execution": "llm",
    "executor": "pediatric_medication_basis_explicit_basis_recorded",
    "parameters": {},
    "llm_instruction": "只记录医生明确说明的年龄、体重、剂量计算或调整依据；缺少依据时标记缺失，不自行计算。"
  },
  {
    "code": "prescription_consistency",
    "name": "儿童用药依据与处方一致",
    "enabled": true,
    "execution": "llm",
    "executor": "pediatric_medication_basis_prescription_consistency",
    "related_fields": [
      "prescription",
      "pediatric_weight"
    ],
    "parameters": {},
    "llm_instruction": "只核对明确记录之间是否有矛盾，不计算儿童剂量，不进行临床专业支持。"
  }
]$emr_rules_pediatric_medication_basis$::jsonb);

DO $$
DECLARE
    seed_field_count INTEGER;
    seed_rule_count INTEGER;
    existing_field_count INTEGER;
    updated_field_count INTEGER;
    persisted_rule_count INTEGER;
    missing_codes TEXT;
    invalid_rules TEXT;
    invalid_related_fields TEXT;
BEGIN
    SELECT COUNT(*)
    INTO seed_field_count
    FROM _emr_field_quality_seed;

    IF seed_field_count <> 41 THEN
        RAISE EXCEPTION 'EMR 栏位质量种子数量不正确：expected=41, actual=%', seed_field_count;
    END IF;

    SELECT COALESCE(SUM(jsonb_array_length(quality_rules)), 0)
    INTO seed_rule_count
    FROM _emr_field_quality_seed;

    IF seed_rule_count <> 108 THEN
        RAISE EXCEPTION 'EMR 栏位质量规则数量不正确：expected=108, actual=%', seed_rule_count;
    END IF;

    SELECT string_agg(s.code, ', ' ORDER BY s.code)
    INTO missing_codes
    FROM _emr_field_quality_seed s
    WHERE NOT EXISTS (
        SELECT 1
        FROM public.emr_fields f
        WHERE f.tenant_id = 0
          AND f.code = s.code
    );

    IF missing_codes IS NOT NULL THEN
        RAISE EXCEPTION 'EMR 平台默认栏位缺失，不能导入质量标准：%', missing_codes;
    END IF;

    SELECT COUNT(*)
    INTO existing_field_count
    FROM public.emr_fields f
    WHERE f.tenant_id = 0
      AND f.code IN (SELECT code FROM _emr_field_quality_seed);

    IF existing_field_count <> 41 THEN
        RAISE EXCEPTION 'EMR 平台默认栏位数量不正确：expected=41, actual=%', existing_field_count;
    END IF;

    WITH rule_items AS (
        SELECT s.code AS field_code, rule.value AS rule
        FROM _emr_field_quality_seed s
        CROSS JOIN LATERAL jsonb_array_elements(s.quality_rules) AS rule(value)
    )
    SELECT string_agg(field_code || '.' || COALESCE(rule->>'code', '<missing>'), ', ' ORDER BY field_code, rule->>'code')
    INTO invalid_rules
    FROM rule_items
    WHERE jsonb_typeof(rule) <> 'object'
       OR BTRIM(COALESCE(rule->>'code', '')) = ''
       OR BTRIM(COALESCE(rule->>'name', '')) = ''
       OR BTRIM(COALESCE(rule->>'execution', '')) = ''
       OR BTRIM(COALESCE(rule->>'executor', '')) = ''
       OR (rule ? 'related_fields' AND jsonb_typeof(rule->'related_fields') <> 'array');

    IF invalid_rules IS NOT NULL THEN
        RAISE EXCEPTION 'EMR 栏位质量规则结构不完整：%', invalid_rules;
    END IF;

    WITH rule_items AS (
        SELECT s.code AS field_code, rule.value AS rule
        FROM _emr_field_quality_seed s
        CROSS JOIN LATERAL jsonb_array_elements(s.quality_rules) AS rule(value)
    ), related AS (
        SELECT
            field_code,
            rule->>'code' AS rule_code,
            jsonb_array_elements_text(rule->'related_fields') AS related_field
        FROM rule_items
        WHERE rule ? 'related_fields'
    )
    SELECT string_agg(field_code || '.' || rule_code || ' -> ' || related_field, ', ' ORDER BY field_code, rule_code, related_field)
    INTO invalid_related_fields
    FROM related r
    WHERE NOT EXISTS (
        SELECT 1
        FROM _emr_field_quality_seed s
        WHERE s.code = r.related_field
    );

    IF invalid_related_fields IS NOT NULL THEN
        RAISE EXCEPTION 'EMR 栏位质量规则 related_fields 引用了不存在的栏位：%', invalid_related_fields;
    END IF;

    UPDATE public.emr_fields f
    SET description = s.description,
        quality_rules = s.quality_rules,
        status = 'enabled',
        updated_at = NOW(),
        updated_by = 0
    FROM _emr_field_quality_seed s
    WHERE f.tenant_id = 0
      AND f.code = s.code;

    SELECT COUNT(*)
    INTO updated_field_count
    FROM public.emr_fields f
    JOIN _emr_field_quality_seed s ON s.code = f.code
    WHERE f.tenant_id = 0
      AND f.description = s.description
      AND f.quality_rules = s.quality_rules
      AND jsonb_typeof(f.quality_rules) = 'array'
      AND BTRIM(f.description) <> ''
      AND f.description LIKE '%质量要求：%'
      AND f.description LIKE '%病历生成要求：%';

    IF updated_field_count <> 41 THEN
        RAISE EXCEPTION 'EMR 栏位质量标准写入校验失败：expected=41, actual=%', updated_field_count;
    END IF;

    SELECT COALESCE(SUM(jsonb_array_length(f.quality_rules)), 0)
    INTO persisted_rule_count
    FROM public.emr_fields f
    JOIN _emr_field_quality_seed s ON s.code = f.code
    WHERE f.tenant_id = 0;

    IF persisted_rule_count <> 108 THEN
        RAISE EXCEPTION 'EMR 栏位质量规则持久化数量不正确：expected=108, actual=%', persisted_rule_count;
    END IF;
END $$;
