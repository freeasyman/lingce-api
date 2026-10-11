-- 完整门诊病历质量规则集。
-- 规则定义仍只有 emr_quality_rule_definitions 一个来源；模板只提供输入字段。
-- 程序规则负责确定性检查，llm 规则负责需要上下文的语义和处方关系检查。

INSERT INTO public.emr_quality_rule_set_versions(version,status,scoring_ready,note)
VALUES ('2026-10-04-v2','draft',false,'完整规则执行、语义检查与统一评分；旧批次保留原规则版本。');

INSERT INTO public.emr_quality_rule_definitions
  (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,
   input_spec,evaluation_spec,action_policy,message_templates,status)
SELECT rule_code,'2026-10-04-v2',section_code,name,source_text,product_text,standard_points,executor_kind,
       input_spec,evaluation_spec,action_policy,message_templates,'published'
FROM public.emr_quality_rule_definitions WHERE version='2026-10-04';

WITH rules(rule_code, section_code, name, source_text, product_text, standard_points, executor_kind, evaluation_spec, complete_blocking, archive_blocking, blocking_fields) AS (
  VALUES
    ('GEN-002','general','一项医疗信息填写不正确','一项医疗信息填写不正确','',1,'llm','{}',false,false,'[]'),
    ('GEN-003','general','一项医疗信息填写不完整','一项医疗信息填写不完整','',1,'llm','{}',false,false,'[]'),
    ('GEN-004','general','急诊病历就诊日期未写到分钟','急诊病历就诊日期未写到分钟','',3,'program','{"mode":"emergency_minute"}',false,false,'[]'),
    ('GEN-005','general','药物过敏史必须明确','', '药物过敏史必须明确；没有过敏史也要明确填写。',0,'program','{"uncertain":["待确认","尚未询问","不详","未询问"]}',true,true,'[]'),
    ('GEN-006','general','药物过敏史记录不具体','', '药物过敏史记录不具体',0,'llm','{}',false,false,'[]'),
    ('CC-002','chief_complaint','描述不确切、不完整或缺少某一部分','描述不确切、不完整或缺少某一部分','',2,'llm','{"checks":["症状","部位","时间"]}',false,false,'[]'),
    ('CC-003','chief_complaint','以诊断代替主诉','以诊断代替主诉','',2,'llm','{}',false,false,'[]'),
    ('CC-004','chief_complaint','有新症状体征而缺主诉的复诊、取药病历','有新症状体征而缺主诉的复诊、取药病历','',3,'llm','{}',false,false,'[]'),
    ('HIS-002','history','初诊现病史与主诉不相符','初诊现病史与主诉不相符','',4,'llm','{}',false,false,'[]'),
    ('HIS-003','history','初诊现病史描述混乱、重点不突出','初诊现病史描述混乱、重点不突出','',4,'llm','{}',false,false,'[]'),
    ('HIS-004','history','初诊缺少主要鉴别诊断资料','初诊缺少主要鉴别诊断资料','',3,'llm','{}',false,false,'[]'),
    ('HIS-005','history','复诊未记录治疗后病情变化或治疗效果','复诊未记录治疗后病情变化或治疗效果','',5,'llm','{}',false,false,'[]'),
    ('HIS-006','history','未确诊复诊病历未记录新出现的症状体征或补充鉴别诊断资料','未确诊复诊病历未记录新出现的症状体征或补充鉴别诊断资料','',3,'llm','{}',false,false,'[]'),
    ('HIS-007','history','缺少与本次疾病相关的既往史及其他病史记录','缺少与本次疾病相关的既往史及其他病史记录','',5,'llm','{}',false,false,'[]'),
    ('HIS-008','history','与本次疾病相关的既往史及其他病史有重要不足','与本次疾病相关的既往史及其他病史有重要不足','',3,'llm','{}',false,false,'[]'),
    ('HIS-010','history','病史中的过敏史记录不完整或不具体','病史中的过敏史记录不完整或不具体','',3,'llm','{}',false,false,'[]'),
    ('EXAM-002','physical_exam','记录不准确、不全面','记录不准确、不全面','',5,'llm','{}',false,false,'[]'),
    ('EXAM-003','physical_exam','阳性体征或主要阴性体征未描述','阳性体征或主要阴性体征未描述','',5,'llm','{}',false,false,'[]'),
    ('EXAM-004','physical_exam','阳性体征或主要阴性体征描述不准确','阳性体征或主要阴性体征描述不准确','',3,'llm','{}',false,false,'[]'),
    ('EXAM-005','physical_exam','缺少专科检查记录','缺少专科检查记录','',10,'llm','{}',false,false,'[]'),
    ('EXAM-006','physical_exam','复诊缺少原有阳性体征变化或必要的体检记录','复诊缺少原有阳性体征变化或必要的体检记录','',20,'llm','{}',false,false,'[]'),
    ('TEST-001','auxiliary_exam','初诊或复诊必需的检查缺一项','初诊或复诊必需的检查缺一项','',2,'llm','{}',false,false,'[]'),
    ('TEST-002','auxiliary_exam','相关检查结果未纳入病历记录','相关检查结果未纳入病历记录','',3,'llm','{}',false,false,'[]'),
    ('TEST-003','auxiliary_exam','相关检查结果记录遗漏或不准确','相关检查结果记录遗漏或不准确','',2,'llm','{}',false,false,'[]'),
    ('DX-002','diagnosis','诊断内容没有临床意义','诊断内容没有临床意义','',5,'llm','{}',false,false,'[]'),
    ('DX-003','diagnosis','诊断名称和编码不符合ICD-10','诊断名称不全、不规范','产品要求按 ICD-10 编码',5,'program','{"mode":"icd10"}',false,false,'[]'),
    ('TRT-001','treatment','缺治疗意见','缺治疗意见','',20,'program','{}',true,true,'[]'),
    ('TRT-002','treatment','各项治疗措施不合理、不正确或不及时','各项治疗措施不合理、不正确或不及时','',3,'llm','{}',false,false,'[]'),
    ('TRT-003','treatment','处方书写未按规则要求','处方书写未按规则要求','',3,'llm','{}',false,false,'[]'),
    ('TRT-004','treatment','处方与病历中医嘱不相符','处方与病历中医嘱不相符','',5,'llm','{}',false,false,'[]'),
    ('TRT-005','treatment','未注明住院原因','未注明住院原因','',5,'llm','{}',false,false,'[]'),
    ('TRT-006','treatment','出具医学证明书与本次疾病不相符','出具医学证明书与本次疾病不相符','',5,'llm','{}',false,false,'[]'),
    ('TRT-007','treatment','缺少特殊检查、治疗或手术的操作记录','缺少特殊检查、治疗或手术的操作记录','',10,'llm','{}',false,false,'[]'),
    ('TRT-008','treatment','特殊检查、治疗或手术缺少患者签名','特殊检查、治疗或手术缺少患者签名','',5,'llm','{}',false,false,'[]'),
    ('TRT-009','treatment','缺少应当向患者交代的重要事项记录','缺少应当向患者交代的重要事项记录','',5,'llm','{}',false,false,'[]'),
    ('TRT-010','treatment','未按规定报告传染病疫情和肿瘤病例','未按规定报告传染病疫情和肿瘤病例','',4,'llm','{}',false,false,'[]')
), prepared AS (
  SELECT rule_code, section_code, name, source_text, product_text, standard_points, executor_kind,
         evaluation_spec::jsonb AS evaluation_spec, complete_blocking, archive_blocking, blocking_fields::jsonb AS blocking_fields
  FROM rules
)
INSERT INTO public.emr_quality_rule_definitions
  (rule_code, version, section_code, name, source_text, product_text, standard_points, executor_kind,
   input_spec, evaluation_spec, action_policy, message_templates, status)
SELECT rule_code, '2026-10-04-v2', section_code, name, source_text, product_text, standard_points, executor_kind,
       '{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,
       evaluation_spec,
       jsonb_build_object(
         'save', jsonb_build_object('enabled', true, 'blocking', false, 'blocking_fields', '[]'::jsonb),
         'complete', jsonb_build_object('enabled', true, 'blocking', complete_blocking, 'blocking_fields', blocking_fields),
         'archive', jsonb_build_object('enabled', true, 'blocking', archive_blocking, 'blocking_fields', blocking_fields)
       ),
       jsonb_build_object(
         'save', '发现' || name || '，请根据具体依据核对。',
         'complete', '发现' || name || '，请根据具体依据核对。',
         'archive', '发现' || name || '，请根据具体依据核对。'
       ),
       'published'
FROM prepared
ON CONFLICT (rule_code, version) DO UPDATE SET
  section_code=EXCLUDED.section_code, name=EXCLUDED.name, source_text=EXCLUDED.source_text,
  product_text=EXCLUDED.product_text, standard_points=EXCLUDED.standard_points,
  executor_kind=EXCLUDED.executor_kind, input_spec=EXCLUDED.input_spec,
  evaluation_spec=EXCLUDED.evaluation_spec, action_policy=EXCLUDED.action_policy,
  message_templates=EXCLUDED.message_templates, status='published', updated_at=NOW();

UPDATE public.emr_quality_rule_set_versions
SET status='published', scoring_ready=TRUE,
    note='统一门诊病历质量标准完整规则集；程序规则与大模型语义规则共用同一版本，评分由规则结果计算。',
    updated_at=NOW()
WHERE version='2026-10-04-v2';

UPDATE public.emr_quality_rule_set_versions SET status='retired' WHERE version='2026-10-04';

WITH specs(code,kind,instruction,per_item,score_group) AS (VALUES
 ('GEN-001','program','按患者快照逐项检查基本信息；科别、病历编号、日期读取病历上下文。缺项和不完整不重复计算。',true,''),
 ('GEN-002','program','只判断可证明的错误，例如出生年月晚于就诊日期。无法证明个人信息真实性时不猜测。',true,''),
 ('GEN-003','program','出生年月至少记录年和月；已填写年份但没有月份适用此项。缺项不能重复记作不完整。',true,''),
 ('GEN-004','program','仅急诊病历适用；started_at 或明确的就诊时间原值包含小时分钟即可，秒为0不代表缺分钟。',false,''),
 ('GEN-005','program','患者快照中的药物过敏史必须明确；明确否认或明确记录过敏均可，详细程度不用于阻断。',false,''),
 ('GEN-006','llm','只检查药物过敏史已记录但缺具体类型或过敏原。明确否认过敏不得提示不具体；无记录由GEN-005处理。',false,''),
 ('CC-001','program','没有主诉阻断；复诊缺主诉按原标准复诊缺项3分，不再同时扣5分。',false,''),
 ('CC-002','llm','分别检查主要症状、部位和明确的持续时间。头痛3天已经包含三者。没有内容由CC-001处理；未写时间只提示不阻断。',false,''),
 ('CC-003','llm','诊断名称代替症状才判定；诊断明确、无新症状的复诊取药允许病史同前。',false,''),
 ('CC-004','llm','只适用复诊取药；引用本次病史明确新症状或对照既往记录，主诉未反映该症状时提示。无历史且无新症状证据不推断。',false,''),
 ('CC-005','program','主诉超过20字符阻断，不新增原标准扣分。',false,''),
 ('HIS-001','program','现病史没有医疗内容阻断，不重复计算现病史语义缺陷。',false,''),
 ('HIS-002','llm','仅初诊，对比主诉和现病史具体症状、部位、时间的明确矛盾；需要两端原文。',false,''),
 ('HIS-003','llm','仅初诊，指出现病史具体混乱段落或重点缺失；不能自创独立病情演变扣分项。',false,''),
 ('HIS-004','llm','仅初诊，引用明确疾病或症状及缺失的鉴别诊断资料并说明医学依据；不能列举所有可能疾病要求逐项排除。不确定时人工核对。',false,''),
 ('HIS-005','llm','仅复诊，有同一患者可靠既往治疗记录才判断疗效或变化是否记录；无既往记录返回not_evaluable。',false,''),
 ('HIS-006','llm','仅未明确诊断的复诊，有既往记录及明确新情况证据才检查未记录的新症状或补充鉴别资料。',false,''),
 ('HIS-007','llm','依据本次病情指出真正相关的既往史、婚育史或家族史缺项；不能机械要求全部。过敏史由HIS-009单独处理。',false,'history_missing'),
 ('HIS-008','llm','相关病史已有内容但重要不足；具体缺项必须有依据。过敏史不足由HIS-010处理，避免重复。',true,''),
 ('HIS-009','program','病史过敏史范围包括药物、食物及其他过敏。写无过敏史可通过；未询问或待确认不通过。',false,'history_missing'),
 ('HIS-010','llm','已有过敏记录但不具体，指出具体缺少的过敏类型或过敏原。明确无过敏史不得扣分。',true,''),
 ('EXAM-001','hybrid','没有体检内容由程序阻断。已有内容，仅有具体证据证明缺少必要体检项目时判定，不机械要求所有生命体征。不可把科别直接当作所有检查必需的依据。',false,''),
 ('EXAM-002','llm','体检记录不准确需要体检与对照原文具体差异。不能判定真实体征是否存在。同一矛盾不重复记入EXAM-004。',false,''),
 ('EXAM-003','llm','引用病史诊断或检查中已经明确的阳性或必要阴性体征，但体检没记录才判定，必须列出具体体征。',false,''),
 ('EXAM-004','llm','阳性或阴性体征描述与对照数据的具体矛盾，必须提供两端原文，不能猜测临床真实性。',false,''),
 ('EXAM-005','llm','必须根据主诉病情列出具体专科检查和医学依据，不能仅因specialty_exam字段没有内容就判定缺项；其他体检字段已有同样项目视为已记录。',false,''),
 ('EXAM-006','llm','仅复诊且可靠既往记录存在，具体阳性体征或必要项目变化未记录时判定；数据不足不判断，不能猜测前次体征。',false,''),
 ('TEST-001','llm','依据本次病情和已明确检查建议指出具体缺少的必要检查；没有明确适用依据不强行要求影像或化验。',false,''),
 ('TEST-002','manual','必须有原始检查结果和就诊关联；系统没有此数据时返回not_evaluable，不推断未记录。',false,''),
 ('TEST-003','manual','必须对照原始结果与本次记录两端文本；数据源不存在时不能判定。',false,''),
 ('DX-001','hybrid','没有诊断由程序阻断；语义检查不能仅匹配待查两字。症状性或首先考虑的可能诊断有临床意义，并记录进一步检查、处理建议时允许。无意义占位或无有效临床判断及建议时阻断。',false,''),
 ('DX-002','llm','判断是否表达有意义临床判断，不要求模型证实疾病真实性。DX-001已经判定的同一缺陷不得重复。',false,''),
 ('DX-003','program','查询现行国家ICD-10目录；标准名称可准确匹配时可通过，输入编码时名称与编码必须匹配。自由文本不得猜测编码。',false,''),
 ('TRT-001','program','治疗意见由处方、处置、医嘱及治疗字段组成；任一已有实际内容不得判为缺治疗意见。',false,''),
 ('TRT-002','llm','逐一检查处方药物和处置与诊断病史的合理关联，包括过敏冲突；引用药物和诊断两端原文，不能单凭科别不同认定错误。医学推断须标记manual_review。不替医生决定停药或新增治疗。',true,''),
 ('TRT-003','hybrid','处方需有药物全称、用量和用法；结构化输入程序检查，文本由模型提取具体药物和缺项。未开药不扣分；一般治疗建议不是具体药物处方不得凭空找药名。',false,''),
 ('TRT-004','llm','处方与医嘱明确药物、剂量、用法、频次存在矛盾才判定，两端原文必需；医嘱未重复处方不等于冲突。',false,''),
 ('TRT-005','llm','明确住院建议且没有原因才提示；与建议相邻已说明住院理由不得判缺失；没有住院建议不适用。',false,''),
 ('TRT-006','manual','只有具备可关联的医学证明书内容才能两端核对，无数据源不能判定缺失。',false,''),
 ('TRT-007','manual','需要已实施的特殊项目及关联操作记录，无记录数据源时不能推断缺操作记录。',false,''),
 ('TRT-008','manual','患者签名需要可验证的知情同意记录，不能用医生登录代替患者签名；数据源不存在时not_evaluable。',false,''),
 ('TRT-009','llm','从已明确疾病处置或具体药物指出需交代的重要事项及依据；不能每例机械要求所有复诊、休息或注意事项。',false,''),
 ('TRT-010','manual','需要明确的报告条件和关联报告状态，单凭诊断文字不得推断未上报。',true,'')
)
UPDATE public.emr_quality_rule_definitions r
SET executor_kind=s.kind,
    evaluation_spec=r.evaluation_spec || jsonb_build_object('instruction',s.instruction,'per_item',s.per_item,'scoring_group',s.score_group),
    action_policy=jsonb_set(r.action_policy,'{save,enabled}',to_jsonb(s.kind IN ('program','hybrid')))
FROM specs s WHERE r.version='2026-10-04-v2' AND r.rule_code=s.code;

UPDATE public.emr_quality_rule_definitions SET
 action_policy=jsonb_set(jsonb_set(action_policy,'{complete,blocking}','true'),'{archive,blocking}','true')
WHERE version='2026-10-04-v2' AND rule_code='EXAM-006';

UPDATE public.emr_quality_rule_definitions SET message_templates=jsonb_build_object(
 'save','请明确记录药物过敏史；没有过敏史也请明确填写。',
 'complete','没有明确记录药物过敏史，不能完成病历。',
 'archive','没有明确记录药物过敏史，不能归档。') WHERE version='2026-10-04-v2' AND rule_code='GEN-005';

UPDATE public.emr_quality_rule_definitions SET message_templates=jsonb_build_object(
 'save','病历没有记录治疗意见，请补充。',
 'complete','没有治疗意见，不能完成病历。',
 'archive','没有治疗意见，不能归档。') WHERE version='2026-10-04-v2' AND rule_code='TRT-001';

DO $$ BEGIN
 IF (SELECT count(*) FROM public.emr_quality_rule_definitions WHERE version='2026-10-04-v2') <> 43 THEN
  RAISE EXCEPTION '完整规则集必须包含43条规则';
 END IF;
END $$;
