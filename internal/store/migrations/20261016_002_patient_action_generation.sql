INSERT INTO recording_analysis_prompts (
  code,name,description,category,system_prompt,user_prompt_template,output_schema,version,is_active,usage_count,created_by,updated_by,created_at,updated_at
)
SELECT
  'patient_action_generation_v1','患者运营沟通动作生成','根据截图理解结果生成一线员工可直接执行的沟通动作。','generation',
  $$你是医疗机构患者运营沟通助手。你只能依据提供的截图理解结果生成员工下一步可执行内容。不要编造患者没有说过的事实，不要做诊断或保证疗效。输出自然、具体、可直接使用的中文内容；如果信息不足，明确说明需要确认的事项。$$,
  $$请为动作“{{action}}”生成结果。

截图理解结果（JSON）：
{{analysis}}

要求：
- “帮我破冰”：生成一句自然开场白，并说明为什么适合当前关系阶段；
- “帮我回复”：直接回答患者当前问题，必要时说明还需确认的检查或信息；
- “帮我继续跟”：给出下一步动作、建议联系时间和本次沟通目标；
- “识别真实顾虑”：区分患者明确表达与待确认的顾虑，给出证据；
- “判断决策人”：判断已出现的决策人或关系人线索，并给出确认问题；
- “推荐产品解释”：只基于患者提到的产品兴趣，给出适合一线沟通的解释重点，不编造价格和疗效；
- “请求主管帮助”：整理需要主管判断的问题、已知证据和希望主管提供的支持。

只输出最终内容，不要输出 JSON、Markdown 标题或过程说明。$$,
  '{}'::jsonb,'v1',true,0,0,0,NOW(),NOW()
WHERE NOT EXISTS (SELECT 1 FROM recording_analysis_prompts WHERE code='patient_action_generation_v1');

INSERT INTO llm_model_configs (
  tenant_id,model_code,function_type,model_name,provider,model_params,extra_params,is_default,is_active,description,created_by,created_at,updated_at
)
SELECT 0,'qwen3.8-flash','patient_action_generation','患者运营动作生成模型','dashscope','{"temperature":0.3,"max_tokens":1200,"timeout_seconds":45}'::json,'{}',true,true,'患者截图后的沟通动作生成',0,NOW(),NOW()
WHERE NOT EXISTS (SELECT 1 FROM llm_model_configs WHERE tenant_id=0 AND function_type='patient_action_generation' AND deleted_at IS NULL);
