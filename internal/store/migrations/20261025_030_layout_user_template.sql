-- 内容获客：从链接提取排版的用户提示词模板进表（含 {{article_html}} 占位符），
-- 对齐「表里保存 → 代码提取 → 拼接」链路，不再把 HTML 硬拼在代码里。
-- 署名：DSH
-- 时间：2026-10-25

UPDATE recording_analysis_prompts
SET user_prompt_template = $p$下面是公众号文章的 HTML（含行内样式）：

{{article_html}}$p$, updated_at = now()
WHERE code = 'content_layout_extract';
