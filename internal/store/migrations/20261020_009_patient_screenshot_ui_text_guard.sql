-- 截图理解 v3.2：菜单、按钮、系统卡片不是患者原话，避免污染证据和需求判断。
UPDATE recording_analysis_prompts
SET
  version = 'v3.2',
  system_prompt = system_prompt || $sys$

再强调：
- 菜单、按钮、卡片、快捷回复、机构自动消息、系统提示不是患者原话，不能写入 patient_utterances、patient_question、concern、product_interest，也不能作为“患者说”的证据；
- “门店地址”“服务内容”“预约”“立即咨询”“价格表”“企业微信联系卡”等界面文本，即使被点击，也只能写成“[系统] 患者点击了……”，不能写成患者原话；
- 只有患者/家长/家属真正输入或说出的文字，才能进入 patient_utterances；
- relationship_clues 每一项必须包含“角色 + 截图中的证据”，不能只写角色名；没有证据时不要输出这一项；
- 朋友圈生活内容只能用于自然互动，不主动寻找医美、抗衰或医疗机会。
$sys$,
  user_prompt_template = user_prompt_template || $user$

【界面文本边界】
菜单、按钮、卡片和系统自动消息不是患者原话；patient_utterances 只放患者/家长/家属实际输入或说出的句子。门店地址、服务内容、预约、立即咨询、价格表、企业微信联系卡等只能作为系统操作写入 evidence，前缀写“[系统]”。
$user$,
  updated_at = NOW()
WHERE code = 'patient_screenshot_analysis_v1' AND is_active = true;
