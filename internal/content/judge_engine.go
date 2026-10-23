package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// 判断层：从洞察生成选题（带推荐理由 + 预期帮助）。

const topicGenerationFromInsightPrompt = `你是医疗内容策划专家。根据以下洞察（来自真实问诊转写），生成选题建议。

每个选题包含：
1. title：能直接发出去的标题
2. description：这篇讲什么、什么方向
3. form：建议形式（article/graphic/poster/video/script/storyboard 之一）
4. goal：目标（science/brand/doctor_ip/conversion/geo 之一）
5. recommendation：推荐理由（一句话，引用洞察类型，如"因为这是医生金句/患者反复问的问题"）
6. expected_help：预期帮助，JSON 对象 {"search":["搜索词"],"ai_citation":"补AI缺口说明","cognition":"纠正/建立认知","conversion":"转化线索"}

要求：
- 一个洞察可以生成 1~3 个选题。
- 每个选题都要有依据（来自洞察），不编造。
- 预期帮助要克制，获客不承诺数字。

只返回 JSON 数组。`

// TopicV2 是判断层生成的选题（对应 content_topics）。
type TopicV2 struct {
	Title          string            `json:"title"`
	Description    string            `json:"description"`
	Form           string            `json:"form"`
	Goal           string            `json:"goal"`
	Recommendation string            `json:"recommendation"`
	ExpectedHelp   map[string]any    `json:"expected_help"`
}

// GenerateTopicsFromInsights 从洞察生成选题，落库 content_topics。
func (s *Service) GenerateTopicsFromInsights(ctx context.Context, tenantID int64, insightIDs []int64) ([]map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	if len(insightIDs) == 0 {
		return nil, fmt.Errorf("insight_ids is required")
	}

	// 取洞察
	insights, err := s.store.fetchInsightsByIDs(ctx, tenantID, insightIDs)
	if err != nil {
		return nil, err
	}
	if len(insights) == 0 {
		return nil, fmt.Errorf("no insights found")
	}

	// 组装洞察上下文
	var b strings.Builder
	for _, it := range insights {
		b.WriteString(fmt.Sprintf("- [%s] %s（原话：%s）\n", it.Type, it.Topic, it.Quote))
	}

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_topic", "topic_generation", "chat"})
	if err != nil {
		return nil, fmt.Errorf("resolve llm config for topic generation: %w", err)
	}

	llmReq := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("topic_generation"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: topicGenerationFromInsightPrompt},
			{Role: "user", Content: "洞察如下：\n\n" + b.String()},
		},
	}
	applyModelParamsToLLMRequest(&llmReq, selectedModel.ModelParams)

	llmResp, err := s.llmClient.TextInference(ctx, llmReq)
	if err != nil {
		return nil, fmt.Errorf("generate topics from insights: %w", err)
	}

	topics, err := parseTopicsV2(llmResp.Content)
	if err != nil {
		return nil, err
	}

	return s.store.insertTopicsV2(ctx, tenantID, insightIDs[0], topics)
}

// parseTopicsV2 解析选题 JSON。
func parseTopicsV2(content string) ([]TopicV2, error) {
	raw := strings.TrimSpace(content)
	if idx := strings.Index(raw, "```"); idx >= 0 {
		raw = raw[idx+3:]
		if strings.HasPrefix(strings.TrimSpace(raw), "json") {
			raw = strings.TrimSpace(raw)[4:]
		}
		if end := strings.LastIndex(raw, "```"); end >= 0 {
			raw = raw[:end]
		}
		raw = strings.TrimSpace(raw)
	}
	if start := strings.Index(raw, "["); start >= 0 {
		if end := strings.LastIndex(raw, "]"); end > start {
			raw = raw[start : end+1]
		}
	}
	var out []TopicV2
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("unmarshal topics: %w (raw: %.200s)", err, raw)
	}
	filtered := out[:0]
	for _, t := range out {
		if strings.TrimSpace(t.Title) == "" {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered, nil
}

type insightBrief struct {
	Type  string
	Topic string
	Quote string
}

// fetchInsightsByIDs 取洞察简报。
func (s *Store) fetchInsightsByIDs(ctx context.Context, tenantID int64, ids []int64) ([]insightBrief, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT type, topic, COALESCE(evidence->'quotes'->0->>'text', '')
		FROM content_insights WHERE tenant_id = $1 AND id = ANY($2)`,
		tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("query insights: %w", err)
	}
	defer rows.Close()
	var out []insightBrief
	for rows.Next() {
		var it insightBrief
		if err := rows.Scan(&it.Type, &it.Topic, &it.Quote); err != nil {
			return nil, fmt.Errorf("scan insight: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// insertTopicsV2 落库选题。
func (s *Store) insertTopicsV2(ctx context.Context, tenantID, evidenceRef int64, topics []TopicV2) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(topics))
	for _, t := range topics {
		helpJSON, _ := json.Marshal(t.ExpectedHelp)
		var id int64
		err := s.pool.QueryRow(ctx, `
			INSERT INTO content_topics (tenant_id, title, description, form, goal, source, recommendation, expected_help, evidence_ref)
			VALUES ($1,$2,$3,$4,$5,'insight',$6,$7::jsonb,$8)
			RETURNING id`,
			tenantID, t.Title, t.Description, t.Form, t.Goal, t.Recommendation, string(helpJSON), evidenceRef,
		).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("insert topic: %w", err)
		}
		out = append(out, map[string]any{"id": id, "title": t.Title, "form": t.Form, "goal": t.Goal, "recommendation": t.Recommendation})
	}
	return out, nil
}

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
			{Role: "system", Content: topicEnrichPrompt},
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
			{Role: "system", Content: topicBatchEnrichPrompt},
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
