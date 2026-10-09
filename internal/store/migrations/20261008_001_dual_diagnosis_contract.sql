-- 中医门诊病历同时使用西医诊断和中医诊断；两者都是正式独立栏位。
BEGIN;

UPDATE public.emr_fields
SET name='西医诊断', input_type='结构化内容',
    structure_json='{"items":["diagnosis_name","icd10_code","basis"]}'::jsonb,
    description='diagnosis：西医诊断；使用 ICD-10 编码。', updated_at=NOW()
WHERE tenant_id=0 AND code='diagnosis';

UPDATE public.emr_fields
SET name='中医诊断', input_type='结构化内容',
    structure_json='{"items":["diagnosis_name","pattern_name","basis"]}'::jsonb,
    description='tcm_diagnosis：中医诊断；填写中医病名、证候和辨证依据，不填写 ICD-10。', updated_at=NOW()
WHERE tenant_id=0 AND code='tcm_diagnosis';

DELETE FROM public.emr_template_fields tf
USING public.emr_fields f, public.emr_templates t
WHERE tf.field_id=f.id AND tf.template_id=t.id AND f.tenant_id=0
  AND f.code='tcm_diagnosis' AND t.tenant_id=0 AND t.code<>'tcm';

INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by)
SELECT 0,t.id,f.id,95,TRUE,TRUE,'中医门诊必须填写中医诊断',0
FROM public.emr_templates t JOIN public.emr_fields f ON f.tenant_id=0 AND f.code='tcm_diagnosis'
WHERE t.tenant_id=0 AND t.code='tcm' AND t.status='published'
ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=TRUE,is_required=TRUE,description=EXCLUDED.description,updated_at=NOW();

UPDATE public.emr_template_fields tf
SET display_order=90,is_visible=TRUE,is_required=TRUE,description='必须填写西医诊断',updated_at=NOW()
WHERE tf.template_id IN (SELECT id FROM public.emr_templates WHERE tenant_id=0 AND code='tcm')
  AND tf.field_id IN (SELECT id FROM public.emr_fields WHERE tenant_id=0 AND code='diagnosis');

INSERT INTO public.emr_quality_rule_definitions
(rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status)
VALUES
('TCM-DX-001','2026-10-04-v2','diagnosis','未填写中医诊断','中医门诊病历必须填写中医诊断','中医门诊病历必须填写中医诊断',10,'program',
 '{"paths":["record.content.tcm_diagnosis","record.context.template_code"]}'::jsonb,
 '{"mode":"tcm_diagnosis_presence_only","per_item":false,"instruction":"仅中医模板适用；没有中医诊断时阻断。","scoring_group":""}'::jsonb,
 '{"save":{"enabled":true,"blocking":false},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}'::jsonb,
 '{"save":"请填写中医诊断。","complete":"没有填写中医诊断，不能完成病历。","archive":"没有填写中医诊断，不能归档。"}'::jsonb,'published')
ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();

INSERT INTO public.emr_quality_rule_set_versions(version,status,scoring_ready,note)
VALUES ('2026-10-08-v1','published',TRUE,'双诊断独立字段；中医模板要求中西医诊断分别填写')
ON CONFLICT (version) DO UPDATE SET status='published', scoring_ready=TRUE, note=EXCLUDED.note, updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions
(rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status)
SELECT rule_code,'2026-10-08-v1',section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,'published'
FROM public.emr_quality_rule_definitions WHERE version='2026-10-04-v2'
ON CONFLICT (rule_code,version) DO NOTHING;
INSERT INTO public.emr_quality_rule_definitions
(rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status)
VALUES
('TCM-DX-001','2026-10-08-v1','diagnosis','未填写中医诊断','中医门诊病历必须填写中医诊断','中医门诊病历必须填写中医诊断',10,'program','{"paths":["record.content.tcm_diagnosis","record.context.template_code"]}', '{"mode":"tcm_diagnosis_presence_only"}', '{"save":{"enabled":true,"blocking":false},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}', '{"save":"请填写中医诊断。","complete":"没有填写中医诊断，不能完成病历。","archive":"没有填写中医诊断，不能归档。"}','published'),
('TCM-DX-002','2026-10-08-v1','diagnosis','中医诊断依据不足','中医诊断应有证候和辨证依据','中医诊断应有证候和辨证依据',5,'program','{"paths":["record.content.tcm_diagnosis"]}', '{"mode":"tcm_diagnosis_basis"}', '{"save":{"enabled":true,"blocking":false},"complete":{"enabled":false,"blocking":false},"archive":{"enabled":false,"blocking":false}}', '{"save":"请补充中医诊断的证候或辨证依据。"}','published'),
('TCM-DX-003','2026-10-08-v1','diagnosis','中医诊断填写了西医编码','中医诊断不使用ICD-10编码','中医诊断不使用ICD-10编码',5,'program','{"paths":["record.content.tcm_diagnosis"]}', '{"mode":"tcm_diagnosis_no_icd"}', '{"save":{"enabled":true,"blocking":false},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}', '{"save":"中医诊断不能填写ICD-10编码。","complete":"中医诊断不能填写ICD-10编码。","archive":"中医诊断不能填写ICD-10编码。"}','published')
ON CONFLICT (rule_code,version) DO UPDATE SET status='published', updated_at=NOW();
UPDATE public.emr_quality_rule_set_versions SET status='retired', updated_at=NOW() WHERE version='2026-10-04-v2';
COMMIT;
