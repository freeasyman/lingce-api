-- v4.8：患者自称已付款/已预约/已购买，不等于系统事实已核实。
UPDATE recording_analysis_prompts
SET
  version = 'v4.8',
  system_prompt = system_prompt || $sys$

## 客户陈述与系统事实（v4.8）
- 患者说“我已经付款/预约/购买”时，只能写成“患者称已……”，不能写成“已确认”；
- 回复应先表示看到客户陈述，并说明员工会核对系统记录；不得直接承诺已到账、已预约或已安排；
- 只有 business_facts 或系统记录明确确认后，才能写“已确认”。
$sys$,
  updated_at = NOW()
WHERE code = 'patient_action_generation_v1' AND is_active = true;
