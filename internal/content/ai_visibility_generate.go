package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

const aiVisibilityGeneratePrompt = `你是医疗内容运营专家。下面是患者问题和 AI 可见性诊断的缺口分析。
请基于这个缺口，写一篇【%s】，输出 JSON：
{
  "title": "标题",
  "body": "正文（分段落，可直接发布）"
}
要求：
  · 主题紧扣患者问题，补上 AI 漏掉的知识
  · 医疗合规：不承诺疗效、不制造焦虑、不诱导就诊
  · 只返回 JSON。`

// ListAIVisibilityRuns 列出历史诊断。
func (s *Service) ListAIVisibilityRuns(ctx context.Context, tenantID int64, limit int) ([]AIVisibilityRun, error) {
	return s.store.listAIVisibilityRuns(ctx, tenantID, limit)
}

// GenerateFromAIVisibility 基于缺口诊断生成内容（公众号深度文 / 官网栏目页）。
func (s *Service) GenerateFromAIVisibility(ctx context.Context, tenantID, runID int64, target string) (map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	run, err := s.store.getAIVisibilityRun(ctx, tenantID, runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("诊断记录不存在")
	}
	targetLabel := "公众号深度文"
	if target == "website" {
		targetLabel = "官网栏目页"
	}
	sel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_article", "chat"})
	if err != nil {
		return nil, err
	}
	gapText, _ := json.Marshal(run.Gap)
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("ai_visibility_generate"),
		FunctionType:  sel.FunctionType,
		Provider:      sel.Provider,
		ModelCode:     sel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: fmt.Sprintf(s.resolvePromptByCode(ctx, tenantID, "content_ai_visibility_generate", aiVisibilityGeneratePrompt), targetLabel)},
			{Role: "user", Content: fmt.Sprintf("患者问题：%s\n\n缺口分析：%s", run.Question, string(gapText))},
		},
	}
	applyModelParamsToLLMRequest(&req, sel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return nil, err
	}
	obj, err := extractJSONObject(resp.Content)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(fmt.Sprint(obj["title"]))
	body := strings.TrimSpace(fmt.Sprint(obj["body"]))
	if title == "" || title == "<nil>" {
		title = targetLabel + "：" + run.Question
	}
	id, err := s.store.insertContentItemV2(ctx, tenantID, 0, "article", title, body, nil, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content_id": id, "title": title, "target": target}, nil
}
