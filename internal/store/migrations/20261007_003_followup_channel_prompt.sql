UPDATE recording_analysis_prompts
SET system_prompt = system_prompt || $rules$

十三、结构化渠道输出规则
每项任务必须提供 category（execution 执行类、understanding 理解与疑问类、milestone 节点类）、purpose、evidence、phone_script 和 wechat_content。
phone_script 为自然问候、具体关心和一个明确问题，1～2句，不超过120字；wechat_content 按关怀、事项、明确问题及帮助分段，不超过600字。
script 保留为 phone_script 的兼容副本。内部背景只放在 background，不加入对患者的长篇话术。
同次就诊、同一渠道、同一联系时间的事项合并成一次联系；只有明确不同节点或执行岗位才拆分。没有真实后续服务需求输出 {"tasks":[]}。
不得根据姓名推测年龄性别，不生成催手术、成交、促销、说服消费的任务。理解疑问类须有患者明确疑问或困难证据。
时间优先采用医生明确日期，其次客观复查或治疗节点；缺少时间依据不得任意生成日期。
$rules$
WHERE code='followup_task_generation'
AND position('十三、结构化渠道输出规则' IN system_prompt)=0;
