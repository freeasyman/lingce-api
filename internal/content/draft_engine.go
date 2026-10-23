package content

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// 文稿生成引擎：选题 → 结构化文稿（按形式）。


// GenerateGraphicNoteDraftRequest 图文笔记文稿生成请求。
type GenerateGraphicNoteDraftRequest struct {
	TopicTitle       string `json:"topic_title"`
	TopicDescription string `json:"topic_description"`
	Evidence         string `json:"evidence,omitempty"`
	CardCount        int    `json:"card_count,omitempty"`
	TopicID          *int64 `json:"topic_id,omitempty"`
}

// GenerateGraphicNoteDraft 生成图文笔记文稿（封面 + 各卡片 + 发布包）。
func (s *Service) GenerateGraphicNoteDraft(ctx context.Context, tenantID int64, req GenerateGraphicNoteDraftRequest) (map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	if strings.TrimSpace(req.TopicTitle) == "" {
		return nil, fmt.Errorf("topic_title is required")
	}
	cardCount := req.CardCount
	if cardCount <= 0 {
		cardCount = 3
	}

	user := s.buildDraftUserPrompt(ctx, tenantID, req)
	user += fmt.Sprintf("卡片数量：%d 张\n", cardCount)

	systemPrompt, modelRef, source := s.resolveDraftSystemPrompt(ctx, tenantID, "graphic")
	slog.Info("draft prompt resolved", "form", "graphic", "source", source, "model_ref", modelRef)

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_graphic_note", "content_generation", "chat"})
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
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: user},
		},
	}
	applyModelParamsToLLMRequest(&llmReq, selectedModel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, llmReq)
	if err != nil {
		return nil, fmt.Errorf("generate draft: %w", err)
	}
	return extractJSONObject(resp.Content)
}

// SaveContentV2 保存内容到 content_items。
func (s *Service) SaveContentV2(ctx context.Context, tenantID int64, topicID *int64, form, title, body string, publishPackage, images json.RawMessage) (int64, error) {
	return s.store.insertContentItemV2(ctx, tenantID, valueOrZero(topicID), form, title, body, publishPackage, images)
}

func valueOrZero(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
