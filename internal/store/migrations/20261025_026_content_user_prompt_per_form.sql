-- 内容获客：把 user 模板复制到每个形式那一行（对齐全站标准：一行三样齐全）。
--
-- 背景：025 把 user 模板拆成了单独的 content_user_prompt 行，这不符合全站标准
-- （随访/成交/患者运营/合规都是「一行 = system + user + output_schema」）。
-- 本迁移把该模板复制进 6 个形式行，并删除那个多余的共用行。
--
-- 署名：DSH
-- 时间：2026-10-25

UPDATE recording_analysis_prompts f
SET user_prompt_template = u.user_prompt_template
FROM recording_analysis_prompts u
WHERE u.code = 'content_user_prompt'
  AND f.code IN (
    'content_article_gen','content_graphic_gen','content_script_gen',
    'content_storyboard_gen','content_poster_gen','content_moments_gen'
  );

DELETE FROM recording_analysis_prompts WHERE code = 'content_user_prompt';
