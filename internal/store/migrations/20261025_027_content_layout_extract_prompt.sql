-- 内容获客：新增「从链接提取排版」的提示词。
--
-- 背景：公众号文章支持「粘贴链接 → 学它的排版」。该提取用大模型分析文章 HTML，
-- 提示词按全站标准存进提示词表（code=content_layout_extract），代码兜底。
--
-- 署名：DSH
-- 时间：2026-10-25

INSERT INTO recording_analysis_prompts (code, name, description, category, system_prompt, user_prompt_template, version, is_active, usage_count)
VALUES ('content_layout_extract', '内容获客·从链接提取排版', '提取：分析公众号文章 HTML，抽排版样式', 'content',
$p$你是公众号排版分析专家。下面是一篇公众号文章的 HTML（含行内样式）。分析它的排版，提取一套可复用的排版样式，返回 JSON：
{"bg":"页面/容器背景色(hex)","container":"容器样式","title":"标题样式","lead":"导语样式","heading":"小节标题样式","paragraph":"正文样式","quote":"引用块样式","cta":"行动号召样式"}
每个值是 CSS 内联样式字符串（只含 font-size/color/line-height/margin/font-weight/border/background/padding/text-align 等，不带选择器）。颜色取文章里实际出现的值。只返回 JSON，不要其他内容。$p$,
'', 'v1', true, 0)
ON CONFLICT (code) DO UPDATE SET system_prompt = EXCLUDED.system_prompt, is_active = true;
