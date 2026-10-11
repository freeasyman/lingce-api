-- Ensure the optional EMR candidate-generation prompt exists and explicitly requests JSON.
INSERT INTO public.recording_analysis_prompts (code,name,description,category,system_prompt,user_prompt_template,output_schema,version,is_active,usage_count,created_by,updated_by,created_at,updated_at)
VALUES ('emr_candidate_generation_v1','电子病历候选生成','根据医生接诊转写生成供医生核对的候选病历内容。','analysis',
$$你是电子病历候选内容整理助手。只根据可靠的医生接诊转写整理待医生核对的内容，不补造事实、不推断诊断、不输出质检结论或新医疗建议。患者猜测不得作为医生诊断。
请只输出一个合法 JSON 对象，顶层必须是 candidates 数组；没有候选时输出 {"candidates":[]}。每项含 section_code、content、source_evidence，source_evidence 含 transcript_excerpt。只使用请求提供的栏目编码。
diagnosis 表示西医诊断，content 必须是 {"diagnosis_name":"","icd10_code":"","basis":""} 结构；只整理医生明确表达的西医诊断，无可靠内容则不输出。禁止 text、name、diagnosis_code 等旧别名，不得把中医病名或证候翻译成西医诊断。其他栏目 content 使用 {"text":"简洁连贯的病历文字"} 结构。
不要输出 Markdown、代码围栏或解释。$$,
$$根据接诊转写生成候选，只输出合法 JSON 对象。
录音 ID：{{recording_id}}
分析管线：{{pipeline_code}}
允许栏目：chief_complaint、present_illness、past_history、personal_history、family_history、allergy_history、physical_exam、auxiliary_exam、diagnosis、prescription、disposition、medical_advice、followup、supplement。
diagnosis.content 只能使用 diagnosis_name、icd10_code、basis，并且必须是西医诊断对象；其他栏目 content 使用 text 字符串。source_evidence.transcript_excerpt 必须引用原文。
接诊转写：
{{transcript}}$$,
'{"type":"object","required":["candidates"],"additionalProperties":false,"properties":{"candidates":{"type":"array","items":{"type":"object","required":["section_code","content","source_evidence"],"additionalProperties":false,"properties":{"section_code":{"type":"string"},"content":{"type":"object"},"source_evidence":{"type":"object","required":["transcript_excerpt"],"additionalProperties":false,"properties":{"transcript_excerpt":{"type":"string"}}}}}}}}'::json,'v2',TRUE,0,0,0,NOW(),NOW())
ON CONFLICT (code) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,category=EXCLUDED.category,system_prompt=EXCLUDED.system_prompt,user_prompt_template=EXCLUDED.user_prompt_template,output_schema=EXCLUDED.output_schema,version=EXCLUDED.version,is_active=TRUE,updated_at=NOW();
