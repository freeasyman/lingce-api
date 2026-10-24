package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// 判断层：给"裸选题"补全判断信息（推荐理由 + 预期帮助 4 维度）。

const topicEnrichPrompt = `你是医疗内容策划专家。给下面这个选题补全"判断"信息，返回 JSON：
{"recommendation":"推荐理由（一句话，讲为什么值得发，引用洞察类型）","expected_help":{"search":["患者会搜的词"],"ai_citation":"补什么 AI 知识缺口","cognition":"纠正或建立什么认知","conversion":"可能带来什么转化线索"}}
要求：判断要有依据，不编造；获客是"预期"不是"承诺"；宁短勿空。只返回 JSON。`

// EnrichTopic 补全单个选题的判断信息（推荐理由 + 预期帮助）。
func (s *Service) EnrichTopic(ctx context.Context, tenantID, topicID int64) (map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	topic, err := s.store.getTopicV2(ctx, tenantID, topicID)
	if err != nil {
		return nil, err
	}
	if topic == nil {
		return nil, fmt.Errorf("topic %d not found", topicID)
	}

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_topic", "topic_generation", "chat"})
	if err != nil {
		return nil, fmt.Errorf("resolve llm config: %w", err)
	}
	user := fmt.Sprintf("选题标题：%s\n选题说明：%s\n建议形式：%s\n目标：%s", topic.Title, topic.Description, topic.Form, topic.Goal)
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("topic_generation"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_topic_enrich", topicEnrichPrompt)},
			{Role: "user", Content: user},
		},
	}
	applyModelParamsToLLMRequest(&req, selectedModel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("enrich topic: %w", err)
	}

	obj, err := extractJSONObject(resp.Content)
	if err != nil {
		return nil, err
	}
	recommendation, _ := obj["recommendation"].(string)
	help, _ := obj["expected_help"].(map[string]any)
	helpJSON, _ := json.Marshal(help)

	if err := s.store.updateTopicEnrichment(ctx, tenantID, topicID, recommendation, helpJSON); err != nil {
		return nil, fmt.Errorf("update topic enrichment: %w", err)
	}
	return map[string]any{
		"id":             topicID,
		"recommendation": recommendation,
		"expected_help":  help,
	}, nil
}

// 批量补全选题判断信息。

const topicBatchEnrichPrompt = `你是医疗内容策划专家。给下面每个选题补全"判断"信息，输出 JSON 数组：
[{"id":选题ID,"recommendation":"推荐理由（一句话，为什么值得发）","expected_help":{"search":["患者会搜的词"],"ai_citation":"补什么 AI 知识缺口","cognition":"纠正或建立什么认知","conversion":"可能带来什么转化线索"}}]
要求：判断有依据，不编造；获客是"预期"不是"承诺"；宁短勿空。只返回 JSON 数组。`

// EnrichTopicsBatch 批量补全选题的判断信息（推荐理由 + 预期帮助 4 维度）。
func (s *Service) EnrichTopicsBatch(ctx context.Context, tenantID int64, limit int) (int, error) {
	if s.llmClient == nil {
		return 0, fmt.Errorf("llm gateway is not configured")
	}
	topics, err := s.store.listTopicsWithoutHelp(ctx, tenantID, limit)
	if err != nil {
		return 0, err
	}
	if len(topics) == 0 {
		return 0, nil
	}

	var user strings.Builder
	for _, t := range topics {
		user.WriteString(fmt.Sprintf("ID=%d 标题：%s（说明：%s，形式：%s，目标：%s）\n", t.ID, t.Title, t.Description, t.Form, t.Goal))
	}

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_topic", "topic_generation", "chat"})
	if err != nil {
		return 0, err
	}
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("topic_generation"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_topic_enrich_batch", topicBatchEnrichPrompt)},
			{Role: "user", Content: user.String()},
		},
	}
	applyModelParamsToLLMRequest(&req, selectedModel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("batch enrich: %w", err)
	}

	raw := strings.TrimSpace(resp.Content)
	if idx := strings.Index(raw, "```"); idx >= 0 {
		raw = raw[idx+3:]
		if strings.HasPrefix(strings.TrimSpace(raw), "json") {
			raw = strings.TrimSpace(raw)[4:]
		}
		if end := strings.LastIndex(raw, "```"); end >= 0 {
			raw = raw[:end]
		}
	}
	if start := strings.Index(raw, "["); start >= 0 {
		if end := strings.LastIndex(raw, "]"); end > start {
			raw = raw[start : end+1]
		}
	}
	var results []struct {
		ID             int64          `json:"id"`
		Recommendation string         `json:"recommendation"`
		ExpectedHelp   map[string]any `json:"expected_help"`
	}
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
		return 0, fmt.Errorf("parse batch enrich: %w (raw: %.300s)", err, raw)
	}

	n := 0
	for _, r := range results {
		if r.ID <= 0 {
			continue
		}
		helpJSON, _ := json.Marshal(r.ExpectedHelp)
		if err := s.store.updateTopicEnrichment(ctx, tenantID, r.ID, r.Recommendation, helpJSON); err != nil {
			continue
		}
		n++
	}
	return n, nil
}
