package content

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// 推荐引擎：反复问 LLM 归一化 + 推荐打分（反复问频次主信号）。

const normalizeQuestionsPrompt = `你是医疗内容运营。把下面这些"患者问的问题"归一化：意思相同或相近的（不同说法、同义表达、口语化差异）合并成一条规范主题，输出 JSON 数组：
[{"topic":"规范主题","items":["原始说法1","原始说法2"]}]
规则：
1. 意思相同/相近的归一个主题，不同意思的分开。
2. items 里保留原始说法，一个不落。
3. 不要凭空新增问题，不要改写原意。
4. 规范主题用患者能听懂的简洁中文。
只返回 JSON 数组。`

// RebuildQuestionClusters 用 LLM 归一化 question 洞察，重建"反复问"簇。
func (s *Service) RebuildQuestionClusters(ctx context.Context, tenantID int64) (int, error) {
	if s.llmClient == nil {
		return 0, fmt.Errorf("llm gateway is not configured")
	}
	counts, err := s.store.listQuestionTopicCounts(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("list question topics: %w", err)
	}
	if len(counts) == 0 {
		return 0, nil
	}

	// 组装原始说法（带频次）
	var lines []string
	countMap := map[string]int{}
	quoteMap := map[string][]string{}
	insightIDMap := map[string][]int64{}
	for _, c := range counts {
		countMap[c.Topic] = c.Count
		quoteMap[c.Topic] = c.Quotes
		insightIDMap[c.Topic] = c.InsightIDs
		lines = append(lines, c.Topic)
	}

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_insight", "content_topic", "chat"})
	if err != nil {
		return 0, err
	}
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("insight_normalize"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_cluster_normalize", normalizeQuestionsPrompt)},
			{Role: "user", Content: strings.Join(lines, "\n")},
		},
	}
	applyModelParamsToLLMRequest(&req, selectedModel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("normalize questions: %w", err)
	}

	normalized, err := parseNormalizedClusters(resp.Content)
	if err != nil {
		return 0, err
	}

	// 重建簇：每个归一化主题汇总频次 + 代表原话 + 原始问题
	var clusters []QuestionCluster
	for _, n := range normalized {
		total := 0
		var quotes []string
		var insightIDs []int64
		for _, item := range n.Items {
			total += countMap[item]
			quotes = append(quotes, quoteMap[item]...)
			insightIDs = append(insightIDs, insightIDMap[item]...)
		}
		if total < 2 {
			continue
		}
		quotes = dedupStrings(quotes)
		if len(quotes) > 3 {
			quotes = quotes[:3]
		}
		insightIDs = dedupInt64s(insightIDs)
		qJSON, _ := json.Marshal(quotes)
		clusters = append(clusters, QuestionCluster{Topic: n.Topic, Frequency: total, Quotes: qJSON, Items: n.Items, InsightIDs: insightIDs})
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].Frequency > clusters[j].Frequency })

	if err := s.store.replaceQuestionClusters(ctx, tenantID, clusters); err != nil {
		return 0, fmt.Errorf("replace clusters: %w", err)
	}
	return len(clusters), nil
}

type normalizedCluster struct {
	Topic string   `json:"topic"`
	Items []string `json:"items"`
}

func parseNormalizedClusters(content string) ([]normalizedCluster, error) {
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
	var out []normalizedCluster
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("unmarshal normalized clusters: %w (raw: %.300s)", err, raw)
	}
	return out, nil
}

func dedupStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func dedupInt64s(in []int64) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, v := range in {
		if v <= 0 || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// RecommendTopics 推荐打分：反复问频次（主信号）。
// 返回 top N 个反复问（问题 + 频次 + 代表原话 + 为什么）。
func (s *Service) RecommendTopics(ctx context.Context, tenantID int64, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 3
	}
	clusters, err := s.store.listQuestionClusters(ctx, tenantID, 2)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	var out []map[string]any
	for i, c := range clusters {
		if i >= limit {
			break
		}
		var quotes []string
		_ = json.Unmarshal(c.Quotes, &quotes)
		out = append(out, map[string]any{
			"cluster_id": c.ID,
			"question":   c.Topic,
			"frequency":  c.Frequency,
			"quotes":     quotes,
			"items":      c.Items,
			"why":        fmt.Sprintf("患者反复问了 %d 次", c.Frequency),
		})
	}
	return out, nil
}
