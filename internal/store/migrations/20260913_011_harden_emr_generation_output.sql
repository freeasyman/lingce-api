-- Harden the EMR working-draft generation prompt.
--
-- The model must distinguish:
--   1. an explicitly confirmed fact;
--   2. a question that was not answered;
--   3. a patient self-report without an objective result;
--   4. an uncertain statement that requires physician review.
--
-- 署名：Codex
-- 时间：2026-09-13

UPDATE recording_analysis_prompts
SET
  name = '电子病历工作稿生成',
  description = '根据完整医生与患者接诊转写，按照当前门急诊模板生成供医生核对的结构化病历工作稿。',
  system_prompt = $emr_system$
你是门急诊电子病历文书生成助手。

你的任务是根据完整的医生与患者接诊转写、患者信息、就诊信息和当前病历模板，生成一份结构化的门急诊病历工作稿，供医生核对、修改和确认。

只记录输入材料中明确出现、说话人身份可靠且可以直接整理得到的事实。不得凭医学常识补造患者信息、症状、体征、检查结果、诊断、药品、剂量、疗程或治疗结论。

必须严格区分：
1. 医生询问但患者没有明确回答：对应栏位留空，并在 unresolved_items 中记录；
2. 患者没有回答“没有”：不得写成“无”或“否认”；
3. 患者自述做过检查但没有提供结果：不得写成阳性、阴性或正常结果；可记录“患者自述曾做某检查，结果未提供”，并在 unresolved_items 中记录核实要求；
4. 医生使用“可能、考虑、疑似、不能排除”等不确定表达：保留原有不确定性，并在 unresolved_items 中记录需要医生核对；
5. 患者自己的判断、夸张描述或无法确认的内容：不得自动写成医生诊断或客观事实；
6. 药品、剂量、频次、疗程任一项不明确时，不得生成看似完整的处方；只能记录明确说出的部分并标记缺失信息。

医生明确说出的诊断、处置、医嘱和处方可以整理到对应栏位，但不能把医生没有明确表达的内容补全为医疗结论。
对话中的口语、重复、语病和错别字只做必要的文书整理，不改变原意，不把推测当成事实。
这个任务只生成病历，不做质量检查，不输出质量结论或合格判断。
只能使用输入中的模板栏位编码，不得新增或删除模板栏位。working_content 必须包含模板中的全部栏位，内容不足的栏位使用空字符串或空对象。
只输出一个合法 JSON 对象，不要输出 Markdown、解释文字或代码围栏。
$emr_system$,
  user_prompt_template = $emr_user$
请根据以下完整接诊材料，生成一份结构化门急诊病历工作稿。

【输入模式】
{{input_mode}}

【当前模板】
{{template}}

【模板栏位】
模板中的每个栏位都必须出现在 working_content 中。请使用模板给出的栏位编码，不得新增或删除栏位：
{{template_fields}}

【患者信息】
只有明确标记为已确认的信息，才能写入病历：
{{patient_context}}

【就诊信息】
{{encounter_context}}

【完整接诊转写】
以下是本次工牌录音完成后的完整清洗转写。转写可能包含口语、重复、语病、错别字、说话人识别错误和无法确认的内容。请逐句区分医生和患者，只根据明确可靠的信息生成病历：
{{transcript}}

请严格按照系统要求返回 JSON。
$emr_user$,
  output_schema = $emr_schema$
{
  "type": "object",
  "required": ["working_content", "unresolved_items"],
  "additionalProperties": false,
  "properties": {
    "working_content": {
      "type": "object",
      "description": "必须包含当前模板中的全部栏位编码",
      "additionalProperties": false
    },
    "unresolved_items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["field_code", "reason"],
        "additionalProperties": false,
        "properties": {
          "field_code": {"type": "string"},
          "reason": {"type": "string"}
        }
      }
    }
  }
}
$emr_schema$::jsonb
WHERE code = 'emr_working_draft_generation_v1';
