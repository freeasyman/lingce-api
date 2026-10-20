-- 补齐截图理解输出字段：场景、员工承诺、关系线索和建议动作。
UPDATE recording_analysis_prompts
SET
  version = 'v2.1',
  user_prompt_template = $$请阅读这张截图，生成结构化的患者运营理解。

输出字段：
1. content_scene：只能填写“聊天沟通”“朋友圈动态”或“无法判断”；
2. conversation_summary：先写场景，再用一到两句说明发生了什么；必须说明沟通阶段，没有明确需求时直说没有明确需求；
3. platform、account、nickname：仅填写截图明确可见的信息；不确定则为空；
4. patient_utterances：患者、家长或家属明确说出的原话数组，尽量保留原句；没有则为空数组；
5. patient_question：患者或家属最后一个尚未解决的问题；没有明确问题则为空字符串；
6. concern：明确顾虑先引用原话；可能顾虑写“待确认：……”；没有依据则为空字符串；
7. decision_maker：只填写截图出现的关系人或决策线索，并标注“线索/待确认”；不能因为是账号本人或朋友圈作者就自动写成决策人；
8. relationship_clues：关系人、家庭成员或沟通对象线索数组；每项说明角色和证据，不确定时标注“待确认”；没有则为空数组；
9. product_interest：只填写患者或家属明确提到或明确选择的项目；机构自我介绍、菜单按钮和模型推测不得写入；
10. employee_commitments：员工或机构在截图中明确承诺的事项数组；没有则为空数组；不能把建议动作写成已承诺；
11. suggested_actions：基于事实给出一到三项员工下一步动作；每项必须是动作，不是事实或诊断；信息不足时优先建议确认问题；
12. suggested_icebreaker：只给一句自然、低压力的开场；朋友圈没有医疗需求时，优先从公开内容自然交流，不强行转医疗销售；
13. suggested_reply：只有存在明确问题时才生成可直接发送的回复；没有明确问题时写空字符串；
14. next_action：一个具体、可执行且不超过一步的员工动作；
15. next_contact_at：只有截图明确出现联系时间时填写，否则为空；
16. evidence：支持判断的原话或可见事实数组，每条带角色前缀；不得把建议当证据。

返回合法 JSON。$$,
  output_schema = $$
{
  "type":"object",
  "required":["content_scene","conversation_summary","platform","account","nickname","patient_utterances","patient_question","concern","decision_maker","relationship_clues","employee_commitments","suggested_actions","product_interest","suggested_icebreaker","suggested_reply","next_action","next_contact_at","evidence"],
  "properties":{
    "content_scene":{"type":"string","enum":["聊天沟通","朋友圈动态","无法判断"]},
    "conversation_summary":{"type":"string"},"platform":{"type":"string"},"account":{"type":"string"},"nickname":{"type":"string"},
    "patient_utterances":{"type":"array","items":{"type":"string"}},"patient_question":{"type":"string"},"concern":{"type":"string"},"decision_maker":{"type":"string"},
    "relationship_clues":{"type":"array","items":{"type":"string"}},"employee_commitments":{"type":"array","items":{"type":"string"}},"suggested_actions":{"type":"array","items":{"type":"string"}},
    "product_interest":{"type":"array","items":{"type":"string"}},"suggested_icebreaker":{"type":"string"},"suggested_reply":{"type":"string"},"next_action":{"type":"string"},"next_contact_at":{"type":"string"},"evidence":{"type":"array","items":{"type":"string"}}
  }
}$$::jsonb,
  updated_at = NOW()
WHERE code = 'patient_screenshot_analysis_v1' AND is_active = true;
