package content

import (
	"context"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// AI 先答：患者问 AI 时，AI 怎么答、提不提我们、漏了什么。

const aiAnswerPrompt = `你是通用医疗 AI 助手（类似豆包、元宝）。请中立、客观地回答下面这位患者的问题，站在通用知识角度，不主动推荐任何具体医院或医生。回答控制在 300 字以内。`

const aiAnalysisPrompt = `你是医疗内容运营专家。下面是一条患者问题和"通用 AI 助手"给它的回答。请分析这个 AI 回答，输出 JSON：
{
  "region": "地域：AI 的回答有没有涉及本地/具体机构，有没有提到我们（机构名：%s）",
  "competitors": "竞品：AI 提到了哪些机构/医生/品牌（没有就写"未提及任何机构"）",
  "description": "描述：AI 是否提到、如何描述我们机构（没提到就写"未提及"）",
  "sources": "信源：AI 的信息看起来来自哪里（通用知识/网页/我们的官网）",
  "gap": "缺口：AI 漏掉了什么关键信息、我们有哪些特色优势没被 AI 覆盖（一句话）"
}
只返回 JSON。`

// AnalyzeAIAnswer 分析"患者问 AI"的回答（4 维度）。
func (s *Service) AnalyzeAIAnswer(ctx context.Context, tenantID int64, question string) (map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}

	// 机构名（用于分析"提不提我们"）
	orgName := "我们机构"
	if profile, err := s.store.GetContentOrgProfile(ctx, tenantID); err == nil && profile != nil && profile.InstitutionName != "" {
		orgName = profile.InstitutionName
	}

	// 1. 模拟 AI 回答
	aiAnswer, err := s.aiAnswer(ctx, tenantID, question)
	if err != nil {
		return nil, err
	}

	// 2. 分析
	analysis, err := s.aiAnalyze(ctx, tenantID, question, aiAnswer, orgName)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"question":  question,
		"ai_answer": aiAnswer,
		"org_name":  orgName,
		"analysis":  analysis,
	}, nil
}

func (s *Service) aiAnswer(ctx context.Context, tenantID int64, question string) (string, error) {
	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"chat", "content_topic"})
	if err != nil {
		return "", err
	}
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("ai_answer"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_ai_answer", aiAnswerPrompt)},
			{Role: "user", Content: question},
		},
	}
	applyModelParamsToLLMRequest(&req, selectedModel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

func (s *Service) aiAnalyze(ctx context.Context, tenantID int64, question, aiAnswer, orgName string) (map[string]any, error) {
	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_topic", "chat"})
	if err != nil {
		return nil, err
	}
	prompt := fmt.Sprintf(s.resolvePromptByCode(ctx, tenantID, "content_ai_analyze", aiAnalysisPrompt), orgName)
	user := fmt.Sprintf("患者问题：%s\n\nAI 回答：%s", question, aiAnswer)
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("ai_analysis"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: prompt},
			{Role: "user", Content: user},
		},
	}
	applyModelParamsToLLMRequest(&req, selectedModel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return nil, err
	}
	return extractJSONObject(resp.Content)
}
