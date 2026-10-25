-- 内容获客：6 种形式的共用 user 模板补「发布包怎么写」+ 禁止 // 标题 这类标记。
-- 背景：模板里从来没讲过发布包，模型自由发挥出「// 标题」「// 正文」。
-- 署名：DSH 时间：2026-10-25

UPDATE recording_analysis_prompts
SET user_prompt_template = user_prompt_template || $p$
【发布包（publish_package）怎么写】
copy：发布文案，直接能贴到平台描述框里——2~3 句、口语，不重复标题，
     不加前缀符号、不写结构标记，像真人发帖那样。
tags：话题标签，每个以 # 开头，3~5 个。
pinned_comment：置顶评论，一句补充说明或引导互动的话。$p$,
    updated_at = now()
WHERE code LIKE 'content_%_gen' AND code <> 'content_topic_gen';
