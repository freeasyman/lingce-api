package content

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// 通用文稿生成：按 form 分发，选题 → 结构化文稿。


// GenerateDraft 按 form 生成文稿。
func (s *Service) GenerateDraft(ctx context.Context, tenantID int64, form string, req GenerateGraphicNoteDraftRequest) (map[string]any, error) {
	if form == "graphic" || form == "" {
		return s.GenerateGraphicNoteDraft(ctx, tenantID, req)
	}
	if _, ok := formJsonContract[form]; !ok {
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

	systemPrompt, modelRef, source := s.resolveDraftSystemPrompt(ctx, tenantID, form)
	slog.Info("draft prompt resolved", "form", form, "source", source, "model_ref", modelRef)

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
			{Role: "system", Content: systemPrompt},
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
