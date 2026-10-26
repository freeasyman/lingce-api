-- 内容获客：朋友圈 user_prompt_template 加 {{who_posts}}（谁在发注入）。
-- 背景：057 只落了 system_prompt + output_schema，但 user_prompt_template 还是旧的、
--       没有 {{who_posts}} 占位符，导致"谁在发"注入不到提示词里。
-- 署名：DSH
-- 时间：2026-10-26

UPDATE recording_analysis_prompts
SET user_prompt_template = REPLACE(user_prompt_template, '{{count_requirement}}', '{{who_posts}}

{{count_requirement}}'),
    updated_at = now()
WHERE code = 'content_moments_gen';
