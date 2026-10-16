INSERT INTO recording_analysis_prompts (code,name,description,category,system_prompt,user_prompt_template,output_schema,version,is_active,usage_count,created_by,updated_by,created_at,updated_at)
SELECT 'patient_recording_analysis_v1','患者运营录音分析','从既有录音清洗转写中提取患者运营事实、判断和建议动作','analysis',
$$你是医疗机构患者运营分析助手。只依据录音清洗转写，区分患者原话、可支持的判断和建议动作。不得编造预约、购买、诊断或疗效事实；员工承诺必须保留证据并标记待确认。只输出符合 JSON 结构的对象。$$,
$$客户：{{customer_name}}
业务范围：{{business_scope}}
员工角色：{{role_code}}
录音清洗转写：
{{transcript}}
请提取患者原话、患者问题、顾虑、关系人/决策人线索、员工承诺、建议下一步和证据。$$,
'{}'::jsonb,'v1',true,0,0,0,NOW(),NOW()
WHERE NOT EXISTS (SELECT 1 FROM recording_analysis_prompts WHERE code='patient_recording_analysis_v1');

INSERT INTO llm_model_configs (tenant_id,model_code,function_type,model_name,provider,model_params,extra_params,is_default,is_active,description,created_by,created_at,updated_at)
SELECT 0,'qwen3.8-flash','patient_recording_analysis','患者运营录音分析模型','dashscope','{"temperature":0.2,"max_tokens":1800,"timeout_seconds":60}'::json,'{}',true,true,'患者运营录音分析',0,NOW(),NOW()
WHERE NOT EXISTS (SELECT 1 FROM llm_model_configs WHERE tenant_id=0 AND function_type='patient_recording_analysis' AND deleted_at IS NULL);
