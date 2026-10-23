-- 内容获客的提示词统一收归「旧提示词管理模块」(recording_analysis_prompts)，
-- 废弃新建的 content_creation_schemes。

-- 1) 6 个创作类 prompt 进旧模块（生成时按 code 读取；机构可覆盖）
INSERT INTO recording_analysis_prompts (code, name, description, category, system_prompt, user_prompt_template, output_schema, version, is_active, usage_count)
VALUES
('content_article_gen', '内容获客·公众号文章', '创作：公众号文章文稿', 'content',
 '你是医疗科普公众号作者。根据选题，写一篇公众号文章文稿。要求：正文要有小标题分段、专业准确、不承诺疗效；充分用上选题依据里的患者原话与顾虑。',
 '返回 JSON：{"title":"标题","body":"正文（可含 markdown 小标题/加粗）","publish_package":{"summary":"摘要（分享时显示）","share_copy":"分享朋友圈的推荐文案"}}',
 NULL, '1.0', true, 0),
('content_graphic_gen', '内容获客·小红书图文笔记', '创作：小红书图文笔记（封面+卡片）', 'content',
 '你是小红书医疗科普笔记策划。根据选题，生成一篇笔记的完整文稿。要求：1. 文案口语化、有记忆点，emoji 分段。2. 用上证据里的金句/原话（如果有）。3. 每张卡片讲一个点，不堆砌。4. 不承诺疗效、不出现患者正脸描述。',
 '返回 JSON：{"cover_title":"封面标题（≤20字，有钩子）","cover_subtitle":"封面副标题","cards":["卡片1文案","卡片2文案","卡片3文案"],"publish_package":{"title":"发布标题","tags":["#话题1","#话题2"],"pinned_comment":"首条评论"}}',
 NULL, '1.0', true, 0),
('content_script_gen', '内容获客·口播稿', '创作：短视频口播稿', 'content',
 '你是医疗短视频口播稿策划。根据选题，写一条 30 秒口播稿。要求：口语化、有钩子、有记忆点；字幕是关键词不是全文。',
 '返回 JSON：{"segments":[{"phase":"开场","words":"口播词","action":"画面/动作提示","subtitle":"字幕关键句","duration":"0-3s"},{"phase":"正文","words":"...","action":"...","subtitle":"...","duration":"3-24s"},{"phase":"结尾","words":"...","action":"...","subtitle":"...","duration":"24-30s"}],"publish_package":{"title":"视频标题","tags":["#话题"],"description":"视频描述"}}',
 NULL, '1.0', true, 0),
('content_storyboard_gen', '内容获客·分镜脚本', '创作：短视频分镜', 'content',
 '你是医疗短视频分镜师。根据选题，写分镜脚本。要求：3~5 个镜头，画面描述具体到人物动作和构图。',
 '返回 JSON：{"shots":[{"scene":"景别(特写/中景/全景)","camera":"运镜(固定/推/摇)","dialogue":"台词","duration":"时长","subtitle":"字幕","description":"画面描述(给手绘分镜用)"}]}',
 NULL, '1.0', true, 0),
('content_poster_gen', '内容获客·海报文案', '创作：科普海报文案', 'content',
 '你是医疗科普海报文案。根据选题，写海报文案。要求：主标题短而有钩子，信息点简洁。',
 '返回 JSON：{"title":"主标题","subtitle":"副标题","bullets":["信息点1","信息点2","信息点3"],"publish_package":{"copy":"发布配文","tags":["#话题"],"cta":"行动号召"}}',
 NULL, '1.0', true, 0),
('content_moments_gen', '内容获客·朋友圈文案', '创作：朋友圈内容', 'content',
 '你是医疗科普朋友圈文案。根据选题，写朋友圈内容。要求：配文口语化、有温度，不超过6行避免折叠。',
 '返回 JSON：{"copy":"朋友圈配文(注意分行和emoji)","image_points":["图片要点1","图片要点2","图片要点3"],"publish_package":{"mention":"@提醒(可空)","visibility_note":"可见范围提醒"}}',
 NULL, '1.0', true, 0)
ON CONFLICT DO NOTHING;

-- 2) 旧 content_creation_schemes 里的历史 prompt 迁到旧模块（保留，不丢数据）
INSERT INTO recording_analysis_prompts (code, name, description, category, system_prompt, user_prompt_template, version, is_active, usage_count)
SELECT 'content_legacy_' || id::text, name, '迁移自 content_creation_schemes（旧创作方案）', 'content',
       COALESCE(prompt, ''), '', '1.0', COALESCE(is_active, false), 0
FROM content_creation_schemes
ON CONFLICT DO NOTHING;

-- 3) 废弃新建的 content_creation_schemes
DROP TABLE IF EXISTS content_creation_schemes CASCADE;
