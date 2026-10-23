package content

import (
	"context"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// 通用文稿生成：按 form 分发，选题 → 结构化文稿。

var formDraftPrompts = map[string]string{
	"article": `你是医疗科普公众号作者。根据选题，写一篇公众号文章文稿，返回 JSON：
{"title":"标题","body":"正文（可含 markdown 小标题/加粗）","publish_package":{"summary":"摘要（分享时显示）","share_copy":"分享朋友圈的推荐文案"}}
要求：正文要有小标题分段、专业准确、不承诺疗效。只返回 JSON。`,

	"script": `你是医疗短视频口播稿策划。根据选题，写一条 30 秒口播稿，返回 JSON：
{"segments":[{"phase":"开场","words":"口播词","action":"画面/动作提示","subtitle":"字幕关键句","duration":"0-3s"},{"phase":"正文","words":"...","action":"...","subtitle":"...","duration":"3-24s"},{"phase":"结尾","words":"...","action":"...","subtitle":"...","duration":"24-30s"}],"publish_package":{"title":"视频标题","tags":["#话题"],"description":"视频描述"}}
要求：口语化、有钩子、有记忆点；字幕是关键词不是全文。只返回 JSON。`,

	"storyboard": `你是医疗短视频分镜师。根据选题，写分镜脚本，返回 JSON：
{"shots":[{"scene":"景别(特写/中景/全景)","camera":"运镜(固定/推/摇)","dialogue":"台词","duration":"时长","subtitle":"字幕","description":"画面描述(给手绘分镜用)"}]}
要求：3~5 个镜头，画面描述具体到人物动作和构图。只返回 JSON。`,

	"poster": `你是医疗科普海报文案。根据选题，写海报文案，返回 JSON：
{"title":"主标题","subtitle":"副标题","bullets":["信息点1","信息点2","信息点3"],"publish_package":{"copy":"发布配文","tags":["#话题"],"cta":"行动号召"}}
要求：主标题短而有钩子，信息点简洁。只返回 JSON。`,

	"moments": `你是医疗科普朋友圈文案。根据选题，写朋友圈内容，返回 JSON：
{"copy":"朋友圈配文(注意分行和emoji)","image_points":["图片要点1","图片要点2","图片要点3"],"publish_package":{"mention":"@提醒(可空)","visibility_note":"可见范围提醒"}}
要求：配文口语化、有温度，不超过6行避免折叠。只返回 JSON。`,
}

// GenerateDraft 按 form 生成文稿。
func (s *Service) GenerateDraft(ctx context.Context, tenantID int64, form string, req GenerateGraphicNoteDraftRequest) (map[string]any, error) {
	if form == "graphic" || form == "" {
		return s.GenerateGraphicNoteDraft(ctx, tenantID, req)
	}
	prompt, ok := formDraftPrompts[form]
	if !ok {
		return nil, fmt.Errorf("unsupported form: %s", form)
	}
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	if strings.TrimSpace(req.TopicTitle) == "" {
		return nil, fmt.Errorf("topic_title is required")
	}

	var user strings.Builder
	user.WriteString(s.buildDraftUserPrompt(ctx, tenantID, req))

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_generation", "content_"+form, "chat"})
	if err != nil {
		return nil, fmt.Errorf("resolve llm config for draft: %w", err)
	}
	llmReq := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("draft_generation"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: prompt},
			{Role: "user", Content: user.String()},
		},
	}
	applyModelParamsToLLMRequest(&llmReq, selectedModel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, llmReq)
	if err != nil {
		return nil, fmt.Errorf("generate draft: %w", err)
	}
	return extractJSONObject(resp.Content)
}
