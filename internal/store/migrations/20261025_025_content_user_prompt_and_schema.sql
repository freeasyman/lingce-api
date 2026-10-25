-- 内容获客：创作用户提示词模板 + 各形式 output_schema 固化进提示词表（全站一致）。
--
-- 背景：内容链路原本 user prompt 和 output schema 都写在代码里，只有 system prompt 进表，
-- 与随访/成交/患者运营/合规不一致。本次把 user 模板（含占位符）和各形式 output_schema
-- 也固化进表，代码改成「读表 + 填充占位符 + 读 schema」。
--
-- user 模板占位符：{{topic_brief}} / {{org_knowledge}} / {{brand_assets}}
--                  {{style_templates}} / {{count_requirement}}
--
-- 署名：DSH
-- 时间：2026-10-25

INSERT INTO recording_analysis_prompts (code, name, description, category, system_prompt, user_prompt_template, version, is_active, usage_count)
VALUES ('content_user_prompt', '内容获客·创作通用用户提示词', '创作：6种形式共用的 user 模板（含占位符）', 'content', '', $tpl$选题简报如下（JSON）：

{{topic_brief}}

选题简报字段说明（据此创作，禁止脑补）：
title                标题（参考，可重新起更好的）
recommendation       推荐理由（为什么发这篇）
goal                 内容目标（科普/品牌/医生IP/转化/GEO）
expected_help        预期帮助（search 患者搜索词 / cognition 认知目标 / conversion 获客预期）
core_claim           核心主张（这篇的结论，锁死不能改）
angle                切入角度（锁死不能改）
audience             目标人群（具体到"哪类人看到会说这就是我"）
suggested_forms      建议形式
facts[]              事实（只写这些，别添加简报外的事实）
quotes[]             原话（可引用做金句，保留说话人）
golden_quote         金句（可能为 null）
key_points[]         每节的要点（用来搭正文骨架，不要漏掉关键点）
per_form_structure   各形式骨架（含数量，默认按它来，可微调）
forbidden_claims[]   合规红线（绝对不能写）
bias_risk            偏见风险（注意别踩）

{{org_knowledge}}

{{brand_assets}}

{{style_templates}}

{{count_requirement}}

【素材溯源规则】
facts[] 必须来自简报 facts；quotes[]/golden_quote 必须来自简报 quotes/golden_quote（保留原话和说话人）；key_points[] 必须来自简报 key_points；core_claim / angle 锁死不能改。所有素材不脑补、不编造。

【硬规则】
0. 必须命中简报预期帮助里至少一个效果（搜索/认知/转化）。
1. 所有素材来自简报，不脑补、不编造。
2. 锁死 core_claim / angle / facts/quotes/golden_quote / forbidden_claims。
3. 不承诺疗效、不制造焦虑、不出现患者正脸、不用绝对化用语。
4. 每个字段都必须出现，没有内容写 null 或 []，不许省略字段。
5. 只返回一个 JSON 对象，不要解释、不要 markdown 代码块。$tpl$, 'v1', true, 0)
ON CONFLICT (code) DO UPDATE SET user_prompt_template = EXCLUDED.user_prompt_template, is_active = true;

UPDATE recording_analysis_prompts SET output_schema = '{"type": "object", "properties": {"title": {"type": "string"}, "cover": {"type": "object", "properties": {"image_prompt": {"type": "string"}}, "required": ["image_prompt"], "additionalProperties": false}, "lead": {"type": "string"}, "sections": {"type": "array", "items": {"type": "object", "properties": {"heading": {"type": "string"}, "paragraphs": {"type": "array", "items": {"type": "string"}}, "image_prompt": {"type": "string"}}, "required": ["heading", "paragraphs", "image_prompt"], "additionalProperties": false}}, "share_quote": {"type": "string"}, "publish_package": {"type": "object", "properties": {"copy": {"type": "string"}, "tags": {"type": "array", "items": {"type": "string"}}, "pinned_comment": {"type": "string"}, "cta": {"type": "string"}, "publish_time": {"type": "string"}}, "required": ["copy", "tags", "pinned_comment", "cta", "publish_time"], "additionalProperties": false}}, "required": ["title", "cover", "lead", "sections", "share_quote", "publish_package"], "additionalProperties": false}'::json WHERE code = 'content_article_gen';
UPDATE recording_analysis_prompts SET output_schema = '{"type": "object", "properties": {"title": {"type": "string"}, "cards": {"type": "array", "items": {"type": "object", "properties": {"card_title": {"type": "string"}, "body": {"type": "string"}, "ratio": {"type": "string"}, "image_prompt": {"type": "string"}}, "required": ["card_title", "body", "ratio", "image_prompt"], "additionalProperties": false}}, "publish_package": {"type": "object", "properties": {"copy": {"type": "string"}, "tags": {"type": "array", "items": {"type": "string"}}, "pinned_comment": {"type": "string"}, "cta": {"type": "string"}, "publish_time": {"type": "string"}}, "required": ["copy", "tags", "pinned_comment", "cta", "publish_time"], "additionalProperties": false}}, "required": ["title", "cards", "publish_package"], "additionalProperties": false}'::json WHERE code = 'content_graphic_gen';
