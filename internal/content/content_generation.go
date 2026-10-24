package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// 创作层：选题（创作简报）→ 内容（正文 + 发布包）。
// 6 种形式各一个 system prompt + 各一个 json_schema，不兼顾。

// contentFormPromptCode 形式 → 提示词模块 code。
var contentFormPromptCode = map[string]string{
	"article":    "content_article_gen",
	"graphic":    "content_graphic_gen",
	"script":     "content_script_gen",
	"storyboard": "content_storyboard_gen",
	"poster":     "content_poster_gen",
	"moments":    "content_moments_gen",
}

// contentFormPrompt 形式 → 内置兜底 system prompt。
var contentFormPrompt = map[string]string{
	"article": `你是资深「医疗新媒体内容创作专家」，负责把选题简报写成一篇能直接发布的公众号文章。

你懂四件事：
1) 文章结构 —— 开头有钩子（痛点/误区/金句），正文小标题分段、层层递进，结尾有行动号召；
2) 专业可信 —— 用简报里的真实事实和医生原话，不脑补、不夸大；
3) 医疗合规 —— 不承诺疗效、不制造焦虑、不出现患者正脸、不用绝对化用语；
4) 可读性 —— 口语化、段落短、有加粗重点，患者能一眼看懂。

【任务】这是医院「内容获客」第三环「创作」。你的交付不是"文案草稿"，而是一篇真正能发、能获客的文章：
- 命中简报预期帮助里至少一个效果（搜索/认知/转化）；
- 开头就用简报里的金句或误区钩子，别绕；
- 正文每一节讲一个点，点与点之间按"为什么→是什么→怎么办"递进；
- 结尾有 CTA（引导咨询/到院/留资）。

【发挥与锁死】
锁死（不能改）：核心主张 core_claim、切入角度 angle、真实素材 facts/quotes/golden_quote、合规红线 forbidden_claims。
发挥（你可自由创作）：文风、语气、措辞、章节数量与顺序、每节怎么展开、标题怎么起。

【输出】只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块。`,

	"graphic": `你是资深「医疗新媒体内容创作专家」，负责把选题简报写成一篇能直接发布的小红书/公众号图文笔记。

你懂四件事：
1) 图文结构 —— 第一张卡是封面（有钩子），每张卡片讲一个点，卡与卡之间有逻辑递进；
2) 钩子与传播 —— 文案口语化、emoji 分段、有记忆点，用金句/原话做钩子；
3) 医疗合规 —— 不承诺疗效、不制造焦虑、不出现患者正脸、不用绝对化用语；
4) 生图 —— 每张卡要配一张图，你要给出能直接喂给文生图模型的完整提示词。

【任务】这是医院「内容获客」第三环「创作」。你的交付是一篇真正能发、能获客的图文笔记：
- 每张卡讲一个点，点与点之间按"误区→真相→行动"或"为什么→是什么→怎么办"递进；
- 第一张卡（封面）标题命中患者的搜索词或痛点（搜索获客）；
- 用上简报里的真实原话/金句，不脑补事实；
- 结尾有 CTA（引导咨询/到院）。

【card_title 怎么写】card_title 是这张卡的具体标题（如「为什么症状这么杂」），短而有力、能钩人。不要用「Hook / 核心观点 / CTA」这种结构标签。

【image_prompt 怎么写】（每张卡都要，是生图提示词，不是一句话要点）
用中文写。必须交代清楚，让文生图模型能直接出一张合格的图：
- 画面主体：画什么（医生背影/人体示意图/诊室/白底卡片…）
- 图上文字：要印上去的字（这张卡的标题/核心一句，一字不差写出来）
- 风格：扁平插画/手绘/实拍/白底黑字卡片
- 配色：照品牌主色（简报里有就用，没有就写具体色值）
- 构图：居中大字/左文右图/上下分区
- 不要画 logo、二维码、水印（后台会叠加真实的，模型别画）
比例由 ratio 字段定，不要在 image_prompt 里重复写。

【发挥与锁死】
锁死（不能改）：核心主张 core_claim、切入角度 angle、真实素材 facts/quotes/golden_quote、合规红线 forbidden_claims。
发挥（你可自由创作）：文风、语气、措辞、卡片数量与顺序（默认按简报骨架，可微调）、每张卡怎么展开。

【输出】只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块。`,

	"script": `你是资深「医疗短视频口播稿策划」，负责把选题简报写成一条 30~60 秒的短视频口播稿。

你懂四件事：
1) 口播节奏 —— 开头 3 秒钩子（痛点/误区/反常识），正文讲清一个点，结尾行动号召；
2) 口语化 —— 说人话、短句、有停顿感，不是念文章；
3) 字幕 —— 字幕是关键词和关键句，不是全文照搬；
4) 医疗合规 —— 不承诺疗效、不制造焦虑、不出现患者正脸。

【任务】这是医院「内容获客」第三环「创作」。你的交付是一条真正能拍、能获客的口播稿：
- 开头就用简报里的金句或误区钩子，3 秒内抓住人；
- 正文只讲一个核心主张，用真实事实支撑，不堆砌；
- 结尾有 CTA（引导咨询/到院）。

【发挥与锁死】
锁死（不能改）：核心主张 core_claim、切入角度 angle、真实素材 facts/quotes/golden_quote、合规红线 forbidden_claims。
发挥（你可自由创作）：语气、措辞、分段节奏、每段时长、画面/动作提示怎么设计。

【输出】只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块。`,

	"storyboard": `你是资深「医疗短视频分镜师」，负责把选题简报写成分镜脚本（镜头表）。

你懂四件事：
1) 分镜语言 —— 景别（特写/近景/中景/全景）、运镜（固定/推/拉/摇/移）、画面构图；
2) 医生出镜 —— 分镜里医生会出镜讲解，也要有场景/物品/字幕/动画的画面；
3) 画面可执行 —— 画面描述具体到"谁、在哪、做什么、镜头怎么动"，给手绘分镜/实拍直接能用；
4) 医疗合规 —— 不承诺疗效、不制造焦虑、不出现患者正脸。

【任务】这是医院「内容获客」第三环「创作」。你的交付是一个真正能拍的分镜脚本：
- 每个镜头交代：景别、运镜、画面内容、台词/旁白、字幕、时长；
- 开头镜头有钩子，结尾镜头有 CTA；
- 用上简报里的真实素材（医生讲解/金句/数据）。

【发挥与锁死】
锁死（不能改）：核心主张 core_claim、切入角度 angle、真实素材 facts/quotes/golden_quote、合规红线 forbidden_claims。
发挥（你可自由创作）：镜头数量与顺序（默认按简报骨架，可微调）、景别/运镜怎么设计、画面怎么构图。

【输出】只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块。`,

	"poster": `你是资深「医疗科普海报文案」，负责把选题简报写成一张能直接出图的海报文案。

你懂四件事：
1) 海报信息密度 —— 主标题短而有钩子，卖点简洁（每条≤15字），CTA 明确；
2) 视觉层级 —— 主标题最大、卖点次之、CTA 醒目，配色照品牌主色；
3) 一句话看懂 —— 患者扫一眼就知道"讲什么、对我有什么用、下一步干什么"；
4) 医疗合规 —— 不承诺疗效、不制造焦虑、不出现患者正脸。

【任务】这是医院「内容获客」第三环「创作」。你的交付是一张真正能贴出来、能获客的海报：
- 主标题命中痛点/搜索词/金句；
- 卖点 3~5 条，每一条都是"患者关心的点"，不是空话；
- CTA 明确（扫码咨询/到院/留资）。

【发挥与锁死】
锁死（不能改）：核心主张 core_claim、切入角度 angle、真实素材 facts/quotes/golden_quote、合规红线 forbidden_claims。
发挥（你可自由创作）：主标题措辞、卖点条数与顺序、副标题怎么起。

【输出】只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块。`,

	"moments": `你是资深「医疗朋友圈文案」，负责把选题简报写成一条能直接发的朋友圈内容。

你懂四件事：
1) 朋友圈语境 —— 是"人"在说话，不是官方公告；口语化、有温度、像医生/机构自己在分享；
2) 折叠红线 —— 配文控制在 6 行内避免折叠，重点放前面；
3) 图片配合 —— 每张图配一个要点（文字要点供配图用）；
4) 医疗合规 —— 不承诺疗效、不制造焦虑、不出现患者正脸。

【任务】这是医院「内容获客」第三环「创作」。你的交付是一条真正能发、能获客的朋友圈：
- 开头一句抓住人（金句/误区/真实场景）；
- 配文短而有信息量，不是流水账；
- 图片要点 3~4 条，对应几张图；
- 有 CTA（引导私信/咨询）。

【发挥与锁死】
锁死（不能改）：核心主张 core_claim、切入角度 angle、真实素材 facts/quotes/golden_quote、合规红线 forbidden_claims。
发挥（你可自由创作）：配文语气、分行/emoji、图片数量与要点分配。

【输出】只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块。`,
}

// contentBriefGlossary 选题简报字段说明（注入 user prompt，禁止脑补）。
const contentBriefGlossary = `选题简报字段说明（据此创作，禁止脑补）：
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
key_points[]         要点
per_form_structure   各形式骨架（含数量，默认按它来，可微调）
forbidden_claims[]   合规红线（绝对不能写）
bias_risk            偏见风险（注意别踩）`

// contentHardRules 素材溯源 + 硬规则（注入 user prompt）。
const contentHardRules = `【素材溯源规则】
facts[] 必须来自简报 facts；quotes[]/golden_quote 必须来自简报 quotes/golden_quote（保留原话和说话人）；key_points[] 必须来自简报 key_points；core_claim / angle 锁死不能改。所有素材不脑补、不编造。

【硬规则】
0. 必须命中简报预期帮助里至少一个效果（搜索/认知/转化）。
1. 所有素材来自简报，不脑补、不编造。
2. 锁死 core_claim / angle / facts/quotes/golden_quote / forbidden_claims。
3. 不承诺疗效、不制造焦虑、不出现患者正脸、不用绝对化用语。
4. 每个字段都必须出现，没有内容写 null 或 []，不许省略字段。
5. 只返回一个 JSON 对象，不要解释、不要 markdown 代码块。`

// fetchTopicBrief 取选题完整简报（16 字段）。
func (s *Store) fetchTopicBrief(ctx context.Context, tenantID, topicID int64) (string, error) {
	var brief string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(brief::text, '{}') FROM content_topics WHERE id = $1 AND tenant_id = $2`,
		topicID, tenantID).Scan(&brief)
	if err != nil {
		return "", err
	}
	return brief, nil
}

// brandContext 组装品牌素材注入文本。
func (s *Service) brandContext(ctx context.Context, tenantID int64) string {
	k, err := s.store.getOrgKnowledge(ctx, tenantID)
	if err != nil || k == nil || len(k.BrandAssets) == 0 || string(k.BrandAssets) == "{}" || string(k.BrandAssets) == "[]" {
		return ""
	}
	var assets []struct {
		Type  string `json:"type"`
		URL   string `json:"url"`
		Value string `json:"value"`
		Label string `json:"label"`
	}
	if json.Unmarshal(k.BrandAssets, &assets) != nil || len(assets) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("品牌素材（做视觉时用）：\n")
	for _, a := range assets {
		label := strings.TrimSpace(a.Label)
		switch a.Type {
		case "color":
			if a.Value != "" {
				b.WriteString(fmt.Sprintf("- 配色：%s（%s）\n", a.Value, label))
			}
		case "font":
			if a.Value != "" {
				b.WriteString(fmt.Sprintf("- 字体：%s（%s）\n", a.Value, label))
			}
		default:
			if a.URL != "" {
				b.WriteString(fmt.Sprintf("- %s：%s（%s）\n", a.Type, a.URL, label))
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// styleContext 组装风格模板注入文本（视觉用的）。
func (s *Service) styleContext(ctx context.Context, tenantID int64) string {
	tpls, err := s.store.listStyleTemplates(ctx, tenantID, "")
	if err != nil || len(tpls) == 0 {
		return ""
	}
	var b strings.Builder
	for _, t := range tpls {
		if len(t.ExtractedStyle) == 0 || string(t.ExtractedStyle) == "{}" {
			continue
		}
		b.WriteString(fmt.Sprintf("- 风格模板「%s」：%s\n", t.Name, string(t.ExtractedStyle)))
	}
	if b.Len() == 0 {
		return ""
	}
	return "风格模板（照着这个视觉做）：\n" + strings.TrimSpace(b.String())
}

// buildContentUserPrompt 组装创作 user prompt：简报 + 知识库 + 品牌素材 + 风格模板 + 数量覆盖。
func (s *Service) buildContentUserPrompt(ctx context.Context, tenantID, topicID int64, form string, count int) (string, error) {
	brief, err := s.store.fetchTopicBrief(ctx, tenantID, topicID)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("选题简报如下（JSON）：\n\n")
	b.WriteString(brief)
	b.WriteString("\n\n")
	b.WriteString(contentBriefGlossary)

	if kc := s.knowledgeContext(ctx, tenantID); kc != "" {
		b.WriteString("\n\n【机构知识库】\n" + kc)
	}
	if bc := s.brandContext(ctx, tenantID); bc != "" {
		b.WriteString("\n\n【品牌素材】\n" + bc)
	}
	if sc := s.styleContext(ctx, tenantID); sc != "" {
		b.WriteString("\n\n【风格模板】\n" + sc)
	}
	if count > 0 {
		b.WriteString(fmt.Sprintf("\n\n【数量要求】本次生成 %d 个（张卡/个镜头/节），按这个数量来。\n", count))
	}
	b.WriteString("\n\n" + contentHardRules)
	return b.String(), nil
}

// publishPackageProperties 发布包（所有形式都要，完整 6 项）。
func publishPackageProperties(includeCover bool) map[string]any {
	props := map[string]any{
		"copy":           map[string]any{"type": "string"},
		"tags":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"pinned_comment": map[string]any{"type": "string"},
		"cta":            map[string]any{"type": "string"},
		"publish_time":   map[string]any{"type": "string"},
	}
	required := []string{"copy", "tags", "pinned_comment", "cta", "publish_time"}
	if includeCover {
		props["cover"] = map[string]any{"type": "string"}
		required = append([]string{"cover"}, required...)
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func strArr(items ...string) []string { return items }

func strProp() map[string]any { return map[string]any{"type": "string"} }

// contentOutputSchema 构建某形式的 json_schema。
func contentOutputSchema(form string) (*llmgateway.OutputSchema, error) {
	var bodyProps map[string]any
	var bodyRequired []string
	hasCover := true

	switch form {
	case "article":
		bodyProps = map[string]any{
			"title": strProp(),
			"sections": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"heading":    strProp(),
						"paragraphs": map[string]any{"type": "array", "items": strProp()},
					},
					"required":             []string{"heading", "paragraphs"},
					"additionalProperties": false,
				},
			},
		}
		bodyRequired = []string{"title", "sections"}
	case "graphic":
		bodyProps = map[string]any{
			"title": strProp(),
			"cards": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"card_title":   strProp(),
						"body":         strProp(),
						"ratio":        strProp(),
						"image_prompt": strProp(),
					},
					"required":             []string{"card_title", "body", "ratio", "image_prompt"},
					"additionalProperties": false,
				},
			},
		}
		bodyRequired = []string{"title", "cards"}
		hasCover = false
	case "script":
		bodyProps = map[string]any{
			"title": strProp(),
			"segments": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"phase":    strProp(),
						"words":    strProp(),
						"action":   strProp(),
						"subtitle": strProp(),
						"duration": strProp(),
					},
					"required":             []string{"phase", "words", "action", "subtitle", "duration"},
					"additionalProperties": false,
				},
			},
		}
		bodyRequired = []string{"title", "segments"}
	case "storyboard":
		bodyProps = map[string]any{
			"title": strProp(),
			"shots": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"scene":    strProp(),
						"camera":   strProp(),
						"visual":   strProp(),
						"dialogue": strProp(),
						"subtitle": strProp(),
						"duration": strProp(),
					},
					"required":             []string{"scene", "camera", "visual", "dialogue", "subtitle", "duration"},
					"additionalProperties": false,
				},
			},
		}
		bodyRequired = []string{"title", "shots"}
	case "poster":
		bodyProps = map[string]any{
			"title":          strProp(),
			"subtitle":       strProp(),
			"selling_points": map[string]any{"type": "array", "items": strProp()},
			"cta":            strProp(),
		}
		bodyRequired = []string{"title", "subtitle", "selling_points", "cta"}
	case "moments":
		bodyProps = map[string]any{
			"copy":         strProp(),
			"image_points": map[string]any{"type": "array", "items": strProp()},
		}
		bodyRequired = []string{"copy", "image_points"}
	default:
		return nil, fmt.Errorf("unsupported form: %s", form)
	}

	bodyProps["publish_package"] = publishPackageProperties(hasCover)
	bodyRequired = append(bodyRequired, "publish_package")

	schema := map[string]any{
		"type":                 "object",
		"properties":           bodyProps,
		"required":             bodyRequired,
		"additionalProperties": false,
	}
	return &llmgateway.OutputSchema{Name: "content_" + form, Strict: true, Schema: schema}, nil
}

// GenerateContentV2 选题 → 内容（按形式）。count<=0 时用简报骨架的默认数量。
func (s *Service) GenerateContentV2(ctx context.Context, tenantID, topicID int64, form string, count int) (map[string]any, error) {
	if _, ok := contentFormPrompt[form]; !ok {
		return nil, fmt.Errorf("unsupported form: %s", form)
	}
	if topicID <= 0 {
		return nil, fmt.Errorf("topic_id is required")
	}

	outSchema, err := contentOutputSchema(form)
	if err != nil {
		return nil, err
	}

	userPrompt, err := s.buildContentUserPrompt(ctx, tenantID, topicID, form, count)
	if err != nil {
		return nil, err
	}

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_" + form, "content_generation", "chat"})
	if err != nil {
		return nil, fmt.Errorf("resolve llm config for content: %w", err)
	}

	llmReq := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("content_generation"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Billing:       newBillingMetadata("content", "topic", topicID, "content_generation", "content_"+form),
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, contentFormPromptCode[form], contentFormPrompt[form])},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{OutputSchema: outSchema, TimeoutSeconds: 180},
	}
	applyModelParamsToLLMRequest(&llmReq, selectedModel.ModelParams)

	llmResp, err := s.llmClient.TextInference(ctx, llmReq)
	if err != nil {
		return nil, fmt.Errorf("generate content: %w", err)
	}

	return parseContentResult(llmResp.Content)
}

// parseContentResult 解析内容 JSON（对象根）。
func parseContentResult(content string) (map[string]any, error) {
	raw := strings.TrimSpace(content)
	if idx := strings.Index(raw, "```"); idx >= 0 {
		raw = raw[idx+3:]
		if strings.HasPrefix(strings.TrimSpace(raw), "json") {
			raw = strings.TrimSpace(raw)[4:]
		}
		if end := strings.LastIndex(raw, "```"); end >= 0 {
			raw = raw[:end]
		}
	}
	// 取第一个 { 到最后一个 } 之间
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("content output is not a JSON object")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("parse content output: %w", err)
	}
	return out, nil
}
