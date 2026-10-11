-- 强化诊断与主诉/病史/检查、治疗与诊断/病史之间的关系检查。
-- 关系问题必须给出两端病历证据，不能只返回笼统结论。

UPDATE public.emr_quality_rule_definitions
SET name='诊断与主诉、病史或检查结果不一致或没有临床意义',
    product_text='诊断应与主诉、现病史和辅助检查结果相符；发现明显不一致时提示具体两端内容。',
    evaluation_spec=evaluation_spec || jsonb_build_object(
      'instruction','必须同时比较主诉、现病史、辅助检查和诊断。若主要症状、身体部位或检查结论与诊断明显不一致，返回problem；至少引用诊断和一个对照字段的原文，并明确说明哪两端不一致。若资料不足，返回not_evaluable。',
      'required_evidence_sources',jsonb_build_array('record.content.diagnosis','record.content.chief_complaint','record.content.present_illness','record.content.auxiliary_exam')
    ),
    message_templates=jsonb_build_object(
      'save','诊断与主诉、病史或检查结果可能不一致，请核对具体依据。',
      'complete','诊断与主诉、病史或检查结果不一致，请核对具体依据。',
      'archive','诊断与主诉、病史或检查结果不一致，请核对具体依据。'
    ),
    updated_at=NOW()
WHERE version='2026-10-04-v2' AND rule_code='DX-002';

UPDATE public.emr_quality_rule_definitions
SET name='治疗与诊断或病史不一致',
    product_text='同时检查处方、处置和医嘱与诊断、现病史及检查结果的关系。',
    evaluation_spec=evaluation_spec || jsonb_build_object(
      'instruction','必须同时检查处方、处置、医嘱与诊断、现病史和必要检查。没有处方时仍检查处置和医嘱。若治疗目标、身体部位或疾病与诊断明显不一致，返回problem；引用治疗内容和诊断或病史两端原文。资料不足返回not_evaluable。',
      'required_evidence_sources',jsonb_build_array('record.content.prescription','record.content.disposition','record.content.medical_advice','record.content.diagnosis','record.content.present_illness')
    ),
    message_templates=jsonb_build_object(
      'save','治疗内容与诊断或病史可能不一致，请核对具体依据。',
      'complete','治疗内容与诊断或病史不一致，请核对具体依据。',
      'archive','治疗内容与诊断或病史不一致，请核对具体依据。'
    ),
    updated_at=NOW()
WHERE version='2026-10-04-v2' AND rule_code='TRT-002';
