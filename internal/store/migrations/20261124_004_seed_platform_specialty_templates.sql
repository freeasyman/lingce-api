-- 平台标准专科模板种子：皮肤科 / 外科 / 妇科初诊复诊 / 中医初诊复诊（tenant_id = 0）
-- 来源：dev 库中已提升为平台标准的 6 套模板（含专科字段与句式覆盖）。
-- 幂等：可重复执行；不引用租户 ID；无 DELETE。
DO $$
DECLARE
  v_template UUID;
  v_field UUID;
  seed RECORD;
BEGIN
  -- 1) 专科字段（全局 tenant 0）
  INSERT INTO public.emr_fields
    (tenant_id, code, name, specialty, content_category, input_type, structure_json, options_json, description, status, created_by, updated_by)
  VALUES
    (0, 'obstetric_history', '孕产史', '妇科', '病史', '结构化内容', '{}'::jsonb, '{}'::jsonb, 'obstetric_history：孕产史

适用范围：妇科

质量要求：

- 记录孕次、产次、流产、分娩及相关情况。
- 数量必须来自明确事实。
- 不确定或无法确认时标记待确认。
- 不得从“怀孕过”推导完整孕产次数。

病历生成要求：

- 分别记录孕次、产次、流产及分娩细节。
- 不自行推算孕产次数。

可执行检查类型：结构完整、数值格式、语义、栏位关系、人工判断', 'enabled', 0, 0),
    (0, 'tcm_diagnosis', '中医诊断', '中医', '医疗判断', '结构化内容', '{"items": ["diagnosis_name", "pattern_name", "basis"]}'::jsonb, '{}'::jsonb, 'tcm_diagnosis：中医诊断；填写中医病名、证候和辨证依据，不填写 ICD-10。', 'enabled', 0, 0),
    (0, 'gynecology_auxiliary_exam', '妇科相关辅助检查', '妇科', '诊疗依据', '结构化内容', '{}'::jsonb, '{}'::jsonb, 'gynecology_auxiliary_exam：妇科相关辅助检查

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

## 三、中医栏位', 'enabled', 0, 0),
    (0, 'dermatology_lesion_site', '皮损部位', '皮肤科', '诊疗依据', '长文本', '{}'::jsonb, '{}'::jsonb, '记录皮损发生的解剖部位、分布范围、是否对称及累及面积。', 'enabled', 0, 0),
    (0, 'dermatology_lesion_morphology', '皮损形态', '皮肤科', '诊疗依据', '多选', '{}'::jsonb, '{"options": ["红斑", "丘疹", "水疱", "脓疱", "斑块", "结节", "风团", "脱屑", "糜烂", "溃疡", "皲裂", "色素沉着"]}'::jsonb, '记录皮损的原发和继发形态（红斑、丘疹、水疱、脱屑、糜烂等）、大小、颜色、边界、数目及排列方式。', 'enabled', 0, 0),
    (0, 'dermatology_itch_severity', '瘙痒程度', '皮肤科', '病史', '单选', '{}'::jsonb, '{"options": ["无瘙痒", "轻度", "中度", "重度"]}'::jsonb, '按无瘙痒、轻度、中度、重度记录瘙痒程度及对睡眠和日常生活的影响。', 'enabled', 0, 0),
    (0, 'dermatology_trigger', '诱因', '皮肤科', '病史', '长文本', '{}'::jsonb, '{}'::jsonb, '记录可疑诱因，包括接触物、食物、药物、环境、季节、精神因素及既往类似发作情况。', 'enabled', 0, 0),
    (0, 'gynecology_specialty_exam', '妇科专科查体', '妇科', '诊疗依据', '长文本', '{}'::jsonb, '{}'::jsonb, 'gynecology_specialty_exam：妇科专科查体

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

可执行检查类型：非空、语义、结构关系、人工判断', 'enabled', 0, 0),
    (0, 'dermatology_specialty_exam', '皮肤科专科检查', '皮肤科', '诊疗依据', '长文本', '{}'::jsonb, '{}'::jsonb, '记录皮肤科专科查体：皮损部位、形态、大小、颜色、边界、数目、分布，以及黏膜、毛发、甲受累情况。', 'enabled', 0, 0),
    (0, 'surgery_injury_site', '受伤/发病部位', '外科', '诊疗依据', '长文本', '{}'::jsonb, '{}'::jsonb, '记录受伤或发病的解剖部位、侧别及范围。', 'enabled', 0, 0),
    (0, 'surgery_wound', '伤口情况', '外科', '诊疗依据', '长文本', '{}'::jsonb, '{}'::jsonb, '记录伤口大小、深度、边缘、污染程度、渗出、出血、异物及周围组织情况。', 'enabled', 0, 0),
    (0, 'surgery_tenderness_mobility', '压痛/活动度', '外科', '诊疗依据', '长文本', '{}'::jsonb, '{}'::jsonb, '记录局部压痛部位与程度、关节或肢体活动度、感觉及血运情况。', 'enabled', 0, 0),
    (0, 'surgery_treatment_method', '处理方式', '外科', '医疗措施', '多选', '{}'::jsonb, '{"options": ["清创", "包扎", "换药", "引流", "缝合", "固定", "其他"]}'::jsonb, '记录本次处理方式（清创、包扎、换药、引流、缝合、固定等）及操作要点。', 'enabled', 0, 0),
    (0, 'surgery_specialty_exam', '外科专科检查', '外科', '诊疗依据', '长文本', '{}'::jsonb, '{}'::jsonb, '记录外科专科查体：视诊、触诊、压痛、包块、活动度、伤口及周围组织情况。', 'enabled', 0, 0),
    (0, 'menstrual_history', '月经史', '妇科', '病史', '结构化内容', '{}'::jsonb, '{}'::jsonb, 'menstrual_history：月经史

适用范围：妇科

质量要求：

- 应记录月经周期、经期、经量和规律性等明确内容。
- 未询问的项目不能写成正常。
- 异常情况应保留患者原始表述。
- 不能根据年龄或症状推测月经状态。

病历生成要求：

- 按周期、经期、经量、末次月经、痛经和其他情况组织。
- 未提及的项目留空或标记未询问。

可执行检查类型：结构完整、日期、数值格式、语义、人工判断', 'enabled', 0, 0),
    (0, 'physical_exam', '体格检查', NULL, '诊疗依据', '结构化内容', '{"vitals": ["temperature", "blood_pressure", "pulse", "respiration", "blood_glucose"], "metrics": ["height", "weight", "bmi"], "text_items": ["general_exam", "positive_findings", "necessary_negative_findings", "specialty_exam", "supplement"]}'::jsonb, '{}'::jsonb, '记录生命体征、一般状态、阳性体征、必要阴性体征和专科查体。', 'enabled', 0, 0),
    (0, 'last_menstrual_period', '末次月经', '妇科', '病史', '日期', '{}'::jsonb, '{}'::jsonb, 'last_menstrual_period：末次月经

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

可执行检查类型：日期格式、时间语义、栏位关系、人工判断', 'enabled', 0, 0),
    (0, 'tcm_four_examinations', '中医四诊', '中医', '诊疗依据', '结构化内容', '{}'::jsonb, '{}'::jsonb, 'tcm_four_examinations：中医四诊

适用范围：中医

质量要求：

- 应能反映望、闻、问、切四类信息。
- 只记录实际询问或观察到的内容。
- 未涉及的诊法不能自动补全。
- 不得将普通症状强行转换为中医术语。

病历生成要求：

- 分别组织望诊、闻诊、问诊和切诊信息。
- 未涉及的诊法留空，不强行补全。

可执行检查类型：结构完整、语义、栏位关系、人工判断', 'enabled', 0, 0),
    (0, 'marital_reproductive_history', '婚育史', '妇科', '病史', '结构化内容', '{}'::jsonb, '{}'::jsonb, 'marital_reproductive_history：婚育史

适用范围：妇科

质量要求：

- 记录婚姻和生育情况的明确陈述。
- 应区分婚姻史和生育史。
- 不得根据年龄或称谓推断。
- 未询问不能写成无婚育史。

病历生成要求：

- 分别记录婚姻状态、妊娠、生育和子女情况。
- 仅保留明确陈述。

可执行检查类型：结构完整、数值格式、语义、人工判断', 'enabled', 0, 0),
    (0, 'tcm_pulse', '脉象', '中医', '诊疗依据', '长文本', '{}'::jsonb, '{}'::jsonb, 'tcm_pulse：脉象

适用范围：中医

质量要求：

- 记录明确的脉象描述。
- 不得根据症状推测脉象。
- 医生未明确判断时不能自动生成。

病历生成要求：

- 只提取医生明确描述的脉象。
- 未切脉或未描述时留空。

可执行检查类型：非空、语义、人工判断', 'enabled', 0, 0),
    (0, 'tcm_tongue', '舌象', '中医', '诊疗依据', '结构化内容', '{}'::jsonb, '{}'::jsonb, 'tcm_tongue：舌象

适用范围：中医

质量要求：

- 记录舌质、舌苔等明确观察结果。
- 必须区分实际观察和模型推测。
- 未观察时留空。

病历生成要求：

- 按舌质、舌苔、湿润程度和其他明确内容组织。
- 不根据症状推测舌象。

可执行检查类型：结构完整、语义、人工判断', 'enabled', 0, 0),
    (0, 'tcm_pattern_differentiation', '中医辨证', '中医', '医疗判断', '长文本', '{}'::jsonb, '{}'::jsonb, 'tcm_pattern_differentiation：中医辨证

适用范围：中医

质量要求：

- 记录医生明确提出的证候和辨证依据。
- 应与四诊结果一致。
- 不得由模型自行完成辨证。
- 证候不明确时标记待确认。

病历生成要求：

- 保留医生明确提出的证候和依据。
- 不以模型推理替代医生辨证。

可执行检查类型：非空、语义、栏位关系、人工判断', 'enabled', 0, 0),
    (0, 'tcm_treatment_principle', '治法', '中医', '医疗判断', '长文本', '{}'::jsonb, '{}'::jsonb, 'tcm_treatment_principle：治法

适用范围：中医

质量要求：

- 记录医生明确提出的治疗原则。
- 应与中医诊断和辨证一致。
- 不得根据诊断自动生成治法。
- 未明确时保持为空。

病历生成要求：

- 只记录医生明确说出的治法。
- 不根据辨证自动补写治疗原则。

可执行检查类型：非空、语义、栏位关系、人工判断', 'enabled', 0, 0),
    (0, 'supplement', '补充说明', NULL, '医疗措施', '长文本', '{}'::jsonb, '{}'::jsonb, 'supplement：补充说明

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

## 二、妇科栏位', 'enabled', 0, 0),
    (0, 'past_history', '既往史', NULL, '病史', '长文本', '{}'::jsonb, '{}'::jsonb, 'past_history：既往史

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

可执行检查类型：非空、语义、人工判断', 'enabled', 0, 0),
    (0, 'followup', '复诊安排', NULL, '医疗措施', '长文本', '{}'::jsonb, '{}'::jsonb, 'followup：复诊安排

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

可执行检查类型：日期格式、时间语义、非空、人工判断', 'enabled', 0, 0),
    (0, 'present_illness', '现病史', NULL, '病史', '长文本', '{}'::jsonb, '{}'::jsonb, 'present_illness：现病史

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

判断方式：大模型语义判断，必要时人工复核', 'enabled', 0, 0),
    (0, 'personal_history', '个人史', NULL, '病史', '长文本', '{}'::jsonb, '{}'::jsonb, 'personal_history：个人史

适用范围：通用

质量要求：

- 根据实际询问记录吸烟、饮酒、职业、生活习惯等信息。
- 只记录对话中明确出现的内容。
- 未询问的项目保持未记录。
- 不得根据年龄、性别或疾病推测生活习惯。

病历生成要求：

- 只提取明确出现的职业、吸烟、饮酒、生活习惯和暴露信息。
- 未出现的信息留空，不生成默认结论。

可执行检查类型：非空、语义、人工判断', 'enabled', 0, 0),
    (0, 'chief_complaint', '主诉', NULL, '病史', '长文本', '{}'::jsonb, '{}'::jsonb, 'chief_complaint：主诉

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

判断方式：大模型语义判断，必要时人工复核', 'enabled', 0, 0),
    (0, 'allergy_history', '过敏史', NULL, '病史', '长文本', '{}'::jsonb, '{}'::jsonb, 'allergy_history：过敏史

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

判断方式：结构化判断，无法确定时人工复核', 'enabled', 0, 0),
    (0, 'diagnosis', '西医诊断', NULL, '医疗判断', '结构化内容', '{"items": ["diagnosis_name", "icd10_code", "basis"]}'::jsonb, '{}'::jsonb, 'diagnosis：西医诊断；使用 ICD-10 编码。', 'enabled', 0, 0),
    (0, 'family_history', '家族史', NULL, '病史', '长文本', '{}'::jsonb, '{}'::jsonb, 'family_history：家族史

适用范围：通用

质量要求：

- 应记录明确提到的家族疾病和亲属关系。
- 有明确否认时记录否认。
- 未询问不能写成“无家族史”。
- 不得根据疾病类型推测遗传风险。

病历生成要求：

- 记录明确出现的疾病、亲属关系和家族史否认信息。
- 不从患者当前疾病推导遗传风险。

可执行检查类型：非空、语义、人工判断', 'enabled', 0, 0),
    (0, 'disposition', '处置', NULL, '医疗措施', '长文本', '{}'::jsonb, '{}'::jsonb, 'disposition：处置

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

可执行检查类型：状态、结构完整、语义、栏位关系、人工判断', 'enabled', 0, 0),
    (0, 'medical_advice', '医嘱', NULL, '医疗措施', '长文本', '{}'::jsonb, '{}'::jsonb, 'medical_advice：医嘱

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

可执行检查类型：非空、语义、栏位关系、人工判断', 'enabled', 0, 0),
    (0, 'auxiliary_exam', '辅助检查', NULL, '诊疗依据', '长文本', '{}'::jsonb, '{}'::jsonb, 'auxiliary_exam：辅助检查

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

可执行检查类型：结构完整、状态、语义、栏位关系、人工判断', 'enabled', 0, 0),
    (0, 'prescription', '处方', NULL, '医疗措施', '长文本', '{}'::jsonb, '{}'::jsonb, 'prescription：处方

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

可执行检查类型：结构完整、格式、语义、栏位关系、人工判断', 'enabled', 0, 0)
  ON CONFLICT (tenant_id, code) DO NOTHING;

  -- 2) 平台标准模板（6 套）
  INSERT INTO public.emr_templates
    (tenant_id, code, name, version_no, document_type, visit_type, specialty, status, is_current, print_title, description, created_by, published_by, updated_by, published_at)
  VALUES
    (0, 'dermatology', '皮肤科门诊病历', '1.0', '门诊病历', '初诊', '皮肤科', 'published', TRUE, '皮肤科门诊病历', '皮肤科门诊初诊病历模板，包含皮损部位、皮损形态、瘙痒程度、诱因等专科栏位。', 0, 0, 0, NOW()),
    (0, 'gynecology', '妇科初诊病历', '1.0', '门诊病历', '初诊', '妇科', 'published', TRUE, '妇科门诊初诊病历', '平台标准妇科初诊病历，包含专科栏位。', 0, 0, 0, NOW()),
    (0, 'gynecology', '妇科复诊病历', '1.0', '门诊病历', '复诊', '妇科', 'published', TRUE, '妇科门诊复诊病历', '平台标准妇科复诊病历，包含专科栏位。', 0, 0, 0, NOW()),
    (0, 'surgery', '外科门诊病历', '1.0', '门诊病历', '初诊', '外科', 'published', TRUE, '外科门诊病历', '外科门诊初诊病历模板，包含受伤/发病部位、伤口情况、压痛/活动度、处理方式等专科栏位。', 0, 0, 0, NOW()),
    (0, 'tcm', '中医初诊病历', '1.0', '门诊病历', '初诊', '中医', 'published', TRUE, '中医门诊初诊病历', '平台标准中医初诊病历，包含专科栏位。', 0, 0, 0, NOW()),
    (0, 'tcm', '中医复诊病历', '1.0', '门诊病历', '复诊', '中医', 'published', TRUE, '中医门诊复诊病历', '平台标准中医复诊病历，包含专科栏位。', 0, 0, 0, NOW())
  ON CONFLICT (tenant_id, code, version_no, visit_type) DO NOTHING;

  -- 3) 绑定栏目（含显示名/填写方式/固定句式覆盖）
  CREATE TEMP TABLE _opt_bindings (
    tpl_code TEXT NOT NULL, tpl_visit TEXT NOT NULL, field_code TEXT NOT NULL,
    display_order INTEGER NOT NULL, is_visible BOOLEAN NOT NULL DEFAULT TRUE,
    is_required BOOLEAN NOT NULL DEFAULT FALSE, description TEXT,
    name_override TEXT, input_type_override TEXT, structure_json JSONB
  ) ON COMMIT DROP;

  INSERT INTO _opt_bindings (tpl_code, tpl_visit, field_code, display_order, is_visible, is_required, description, name_override, input_type_override, structure_json) VALUES
    ('dermatology', '初诊', 'chief_complaint', 10, TRUE, FALSE, '主诉', NULL, NULL, NULL),
    ('dermatology', '初诊', 'present_illness', 20, TRUE, FALSE, '现病史', NULL, NULL, NULL),
    ('dermatology', '初诊', 'past_history', 30, TRUE, FALSE, '既往史', NULL, NULL, NULL),
    ('dermatology', '初诊', 'allergy_history', 40, TRUE, FALSE, '过敏史', NULL, NULL, NULL),
    ('dermatology', '初诊', 'physical_exam', 50, TRUE, FALSE, '体格检查', NULL, NULL, NULL),
    ('dermatology', '初诊', 'dermatology_specialty_exam', 60, TRUE, FALSE, '皮肤科专科检查', NULL, NULL, NULL),
    ('dermatology', '初诊', 'dermatology_lesion_site', 70, TRUE, FALSE, '皮损部位', NULL, NULL, NULL),
    ('dermatology', '初诊', 'dermatology_lesion_morphology', 80, TRUE, FALSE, '皮损形态', NULL, NULL, NULL),
    ('dermatology', '初诊', 'dermatology_itch_severity', 90, TRUE, FALSE, '瘙痒程度', NULL, NULL, NULL),
    ('dermatology', '初诊', 'dermatology_trigger', 100, TRUE, FALSE, '诱因', NULL, NULL, NULL),
    ('dermatology', '初诊', 'diagnosis', 110, TRUE, TRUE, '西医诊断', NULL, NULL, NULL),
    ('dermatology', '初诊', 'disposition', 120, TRUE, FALSE, '处置', NULL, NULL, NULL),
    ('dermatology', '初诊', 'medical_advice', 130, TRUE, FALSE, '医嘱', NULL, NULL, NULL),
    ('gynecology', '初诊', 'chief_complaint', 10, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'present_illness', 20, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'past_history', 30, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'personal_history', 40, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'family_history', 50, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'allergy_history', 60, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'physical_exam', 70, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'auxiliary_exam', 80, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'diagnosis', 90, TRUE, TRUE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'prescription', 100, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'disposition', 110, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'medical_advice', 120, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'followup', 130, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'supplement', 140, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'menstrual_history', 210, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'last_menstrual_period', 220, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'marital_reproductive_history', 230, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'obstetric_history', 240, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'gynecology_specialty_exam', 250, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '初诊', 'gynecology_auxiliary_exam', 260, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'chief_complaint', 10, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'present_illness', 20, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'past_history', 30, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'personal_history', 40, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'family_history', 50, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'allergy_history', 60, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'physical_exam', 70, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'auxiliary_exam', 80, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'diagnosis', 90, TRUE, TRUE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'prescription', 100, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'disposition', 110, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'medical_advice', 120, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'followup', 130, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'supplement', 140, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'menstrual_history', 210, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'last_menstrual_period', 220, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'marital_reproductive_history', 230, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'obstetric_history', 240, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'gynecology_specialty_exam', 250, TRUE, FALSE, '', NULL, NULL, NULL),
    ('gynecology', '复诊', 'gynecology_auxiliary_exam', 260, TRUE, FALSE, '', NULL, NULL, NULL),
    ('surgery', '初诊', 'chief_complaint', 10, TRUE, FALSE, '主诉', NULL, NULL, NULL),
    ('surgery', '初诊', 'present_illness', 20, TRUE, FALSE, '现病史', NULL, NULL, NULL),
    ('surgery', '初诊', 'past_history', 30, TRUE, FALSE, '既往史', NULL, NULL, NULL),
    ('surgery', '初诊', 'allergy_history', 40, TRUE, FALSE, '过敏史', NULL, NULL, NULL),
    ('surgery', '初诊', 'physical_exam', 50, TRUE, FALSE, '体格检查', NULL, NULL, NULL),
    ('surgery', '初诊', 'surgery_specialty_exam', 60, TRUE, FALSE, '外科专科检查', NULL, NULL, NULL),
    ('surgery', '初诊', 'surgery_injury_site', 70, TRUE, FALSE, '受伤/发病部位', NULL, NULL, NULL),
    ('surgery', '初诊', 'surgery_wound', 80, TRUE, FALSE, '伤口情况', NULL, NULL, NULL),
    ('surgery', '初诊', 'surgery_tenderness_mobility', 90, TRUE, FALSE, '压痛/活动度', NULL, NULL, NULL),
    ('surgery', '初诊', 'diagnosis', 100, TRUE, TRUE, '西医诊断', NULL, NULL, NULL),
    ('surgery', '初诊', 'surgery_treatment_method', 110, TRUE, FALSE, '处理方式', NULL, NULL, NULL),
    ('surgery', '初诊', 'disposition', 120, TRUE, FALSE, '处置', NULL, NULL, NULL),
    ('surgery', '初诊', 'medical_advice', 130, TRUE, FALSE, '医嘱', NULL, NULL, NULL),
    ('tcm', '初诊', 'chief_complaint', 10, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'present_illness', 20, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'past_history', 30, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'personal_history', 40, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'family_history', 50, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'allergy_history', 60, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'physical_exam', 70, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'auxiliary_exam', 80, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'diagnosis', 90, TRUE, TRUE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'tcm_diagnosis', 95, TRUE, TRUE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'prescription', 100, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'disposition', 110, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'medical_advice', 120, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'followup', 130, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'supplement', 140, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'tcm_four_examinations', 150, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'tcm_tongue', 160, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'tcm_pulse', 170, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'tcm_pattern_differentiation', 180, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '初诊', 'tcm_treatment_principle', 190, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'chief_complaint', 10, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'present_illness', 20, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'past_history', 30, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'personal_history', 40, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'family_history', 50, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'allergy_history', 60, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'physical_exam', 70, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'auxiliary_exam', 80, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'diagnosis', 90, TRUE, TRUE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'tcm_diagnosis', 95, TRUE, TRUE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'prescription', 100, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'disposition', 110, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'medical_advice', 120, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'followup', 130, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'supplement', 140, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'tcm_four_examinations', 150, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'tcm_tongue', 160, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'tcm_pulse', 170, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'tcm_pattern_differentiation', 180, TRUE, FALSE, '', NULL, NULL, NULL),
    ('tcm', '复诊', 'tcm_treatment_principle', 190, TRUE, FALSE, '', NULL, NULL, NULL);

  FOR seed IN SELECT * FROM _opt_bindings LOOP
    SELECT id INTO v_template FROM public.emr_templates WHERE COALESCE(tenant_id,0)=0 AND code=seed.tpl_code AND visit_type=seed.tpl_visit AND version_no='1.0';
    IF v_template IS NULL THEN RAISE EXCEPTION '目标模板不存在：%/%', seed.tpl_code, seed.tpl_visit; END IF;
    SELECT id INTO v_field FROM public.emr_fields WHERE tenant_id=0 AND code=seed.field_code;
    IF v_field IS NULL THEN RAISE EXCEPTION '字段不存在：%', seed.field_code; END IF;
    INSERT INTO public.emr_template_fields
      (tenant_id, template_id, field_id, display_order, is_visible, is_required, description, name_override, input_type_override, structure_json, created_by)
    VALUES (0, v_template, v_field, seed.display_order, seed.is_visible, seed.is_required, COALESCE(seed.description,''), seed.name_override, seed.input_type_override, seed.structure_json, 0)
    ON CONFLICT (template_id, field_id) DO UPDATE
      SET display_order=EXCLUDED.display_order, is_visible=EXCLUDED.is_visible, is_required=EXCLUDED.is_required,
          description=EXCLUDED.description, name_override=EXCLUDED.name_override,
          input_type_override=EXCLUDED.input_type_override, structure_json=EXCLUDED.structure_json, updated_at=NOW();
  END LOOP;
END $$;
