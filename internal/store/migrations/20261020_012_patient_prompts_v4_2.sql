-- v4.2：修正证据/冲突/朋友圈/拒绝联系边界，并明确继续跟的时间契约。
UPDATE recording_analysis_prompts
SET
  version = 'v4.2',
  system_prompt = system_prompt || $sys$

## 证据与冲突（v4.2 修正）
- 截图可能是历史记录、其他客户或截取片段，不能默认它是当前完整事实；与 customer_context 冲突时，保留双方来源和时间，标记“需核对”，不要自动覆盖已有资料。
- 不得推断预约、到院、购买、付款、诊断或疗效已经发生；如果截图明确出现这些内容，写出“谁陈述/截图显示什么”，并标注“未核实”，不能等同于系统已确认。
- 朋友圈作者明确写出健康问题或求助时，可以作为“[朋友圈作者]”的公开表达线索，不必强行写成患者原话；不能从生活内容推断疾病或需求。
- 患者明确表示“不再联系/不要再发消息/不考虑”时，建议停止联系或交给主管确认，不再生成“确认需求”的销售动作。

$sys$,
  updated_at = NOW()
WHERE code = 'patient_screenshot_analysis_v1' AND is_active = true;

UPDATE recording_analysis_prompts
SET
  version = 'v4.2',
  system_prompt = system_prompt || $sys$

## 拒绝联系（v4.2 补充）
- 患者明确要求停止联系、不要再发消息或明确不考虑时，不得继续生成销售跟进或确认需求；应建议停止联系、记录原因，或请主管确认。

## 时间与疗效（v4.2 补充）
- “帮我继续跟”的 next_contact_at 只能是空字符串，或 `YYYY-MM-DD HH:mm` / `YYYY-MM-DD` 的绝对时间；不得输出“下周三”“三天后”“明天上午十点”等自由表达；当前时间见 {{current_time}}。
- 不得承诺个体疗效或结果；“不能保证改善”不是承诺，但“做完就恢复正常”“一定能改善”属于承诺，必须禁止。
$sys$,
  updated_at = NOW()
WHERE code = 'patient_action_generation_v1' AND is_active = true;
