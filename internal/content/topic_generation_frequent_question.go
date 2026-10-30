package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// topicGenerationFromFrequentQuestionPrompt 高频问题 → 选题 的策划提示词。
// 与「单条洞察 → 选题」的区别：输入是一个已经聚合的高频问题（多份素材），
// 且【数量硬约束 1~3，只有命中不同获客效果的角度才拆】。
const topicGenerationFromFrequentQuestionPrompt = `你是资深「医疗内容策划专家」，做「判断 → 策划」两段式工作。

【输入】是一个【高频问题】——它由多位患者反复询问的同一个问题聚合而来，是真实、普遍的需求：
- 高频问题：患者反复在问什么
- 被问次数：说明这是高频真实需求
- 代表原话：来自真实录音
- 聚合素材：支撑它的多条洞察（含患者顾虑、误区、事实、医生讲解）

【任务】把这个高频问题策划成选题，交付标准与「从单条洞察生成选题」一致：
每个选题做出来的内容，必须能命中至少一个【获客效果】——搜索（吃长尾流量）/ 认知（纠正误区、建立信任）/ 转化（带来咨询、到院、留资）。命中不了任何效果的，就不该生成。

【数量硬约束——最重要】
- 默认只出 1 个选题。
- 只有当存在【真正不同、命中不同获客效果】的角度时，才拆第 2、3 个（例如：一个吃搜索流量、一个做误区认知、一个促转化）。
- 同质角度（同一个意思换个说法，比如"全飞秒3.0和4.0的区别"与"全飞秒pro和3.0的区别"）必须合并，绝不硬拆。
- 最多 3 个，绝不超过。

【每个选题必须写清】：
- goal（目标）：科普/品牌/医生IP/转化/GEO 选一个，与切入角度一致。
- recommendation（推荐理由）：说清"为什么值得单独做这个角度"，并点明它命中哪个获客效果。
- angle（切入角度）：从哪个点切入（误区纠正/患者顾虑/医生讲解/数据事实）。
- expected_help（预期帮助）：搜索/认知/转化，每维有依据。
- title / core_claim / audience / suggested_forms / facts / quotes / golden_quote / key_points / per_form_structure / forbidden_claims / bias_risk：与标准定义一致，从聚合素材里取真实内容，绝不编造事实、不改写原话。

【输出字段】与「从单条洞察生成选题」完全一致（title、recommendation、goal、expected_help、core_claim、angle、audience、suggested_forms、facts、quotes、golden_quote、key_points、per_form_structure、forbidden_claims、bias_risk）。`

// getFrequentQuestionByID 按 id 取一个高频问题（含 insight_ids）。
func (s *Store) getFrequentQuestionByID(ctx context.Context, tenantID, frequentQuestionID int64) (*FrequentQuestion, error) {
	var c FrequentQuestion
	err := s.pool.QueryRow(ctx, `
		SELECT id, topic, COALESCE((frequency->>'total')::int, (frequency->>'recent_30d')::int, 0),
		       COALESCE(representative_quotes, '[]'::jsonb),
		       COALESCE(insight_ids, '[]'::jsonb)
		FROM content_frequent_questions
		WHERE id = $1 AND tenant_id = $2`, frequentQuestionID, tenantID).
		Scan(&c.ID, &c.Topic, &c.Frequency, &c.Quotes, &c.InsightIDs)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// fetchInsightPayloads 批量取洞察 payload（聚合成一个 JSON 数组，供高频问题选题策划使用）。
func (s *Store) fetchInsightPayloads(ctx context.Context, tenantID int64, insightIDs []int64) (string, error) {
	if len(insightIDs) == 0 {
		return "[]", nil
	}
	var agg json.RawMessage
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(jsonb_agg(jsonb_build_object('id', id, 'type', type, 'topic', topic, 'payload', COALESCE(payload, evidence, '{}'::jsonb)) ORDER BY id), '[]'::jsonb)
		FROM content_insights
		WHERE tenant_id = $1 AND id = ANY($2)`, tenantID, insightIDs).Scan(&agg)
	if err != nil {
		return "", fmt.Errorf("fetch insight payloads: %w", err)
	}
	return string(agg), nil
}

// GenerateTopicsFromFrequentQuestion 从「高频问题」整体生成 1~3 个选题（不做逐条洞察 fan-out）。
func (s *Service) GenerateTopicsFromFrequentQuestion(ctx context.Context, tenantID, frequentQuestionID int64) ([]map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	fq, err := s.store.getFrequentQuestionByID(ctx, tenantID, frequentQuestionID)
	if err != nil {
		return nil, err
	}
	if fq == nil {
		return nil, fmt.Errorf("高频问题不存在")
	}
	if len(fq.InsightIDs) == 0 {
		return nil, fmt.Errorf("该高频问题缺少关联洞察，无法生成选题")
	}

	payloads, err := s.store.fetchInsightPayloads(ctx, tenantID, fq.InsightIDs)
	if err != nil {
		return nil, err
	}

	var quotes []string
	_ = json.Unmarshal(fq.Quotes, &quotes)

	userPrompt := strings.Join([]string{
		"高频问题：" + fq.Topic,
		fmt.Sprintf("被问次数：%d 次", fq.Frequency),
		"代表原话：" + strings.Join(quotes, "\n"),
		"聚合素材（JSON）：" + payloads,
	}, "\n\n")

	outSchema, err := topicOutputSchema()
	if err != nil {
		return nil, err
	}

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_topic", "topic_generation", "chat"})
	if err != nil {
		return nil, fmt.Errorf("resolve llm config for topic generation: %w", err)
	}

	llmReq := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("topic_generation_frequent_question"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Billing:       newBillingMetadata("content", "frequent_question", frequentQuestionID, "topic_generation", "content_topic"),
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_topic_gen_frequent_question", topicGenerationFromFrequentQuestionPrompt)},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{OutputSchema: outSchema, TimeoutSeconds: 180},
	}
	applyModelParamsToLLMRequest(&llmReq, selectedModel.ModelParams)

	llmResp, err := s.llmClient.TextInference(ctx, llmReq)
	if err != nil {
		return nil, fmt.Errorf("generate topics from frequent question: %w", err)
	}

	topics, err := parseTopicsV2(llmResp.Content)
	if err != nil {
		return nil, err
	}

	// 质量兜底：clamp 到 1~3；同标题去重
	topics = dedupTopics(topics)
	if len(topics) == 0 {
		return []map[string]any{}, nil
	}
	if len(topics) > 3 {
		topics = topics[:3]
	}

	src := topicSourceInfo{
		Source:                    "frequent_question",
		FrequentQuestionID:        fq.ID,
		FrequentQuestionTopic:     fq.Topic,
		FrequentQuestionFrequency: fq.Frequency,
	}
	return s.store.insertTopicsV2(ctx, tenantID, src, fq.InsightIDs, topics)
}

// dedupTopics 选题去重：标题去空白后完全相同或高度重叠（>=80% 字符重叠）的只留第一个。
func dedupTopics(in []TopicV2) []TopicV2 {
	var out []TopicV2
	for _, t := range in {
		title := strings.ToLower(strings.TrimSpace(t.Title))
		dup := false
		for _, e := range out {
			et := strings.ToLower(strings.TrimSpace(e.Title))
			if title == et || (len(title) > 0 && strings.Contains(et, title) && float64(len(title))/float64(len(et)) >= 0.8) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, t)
		}
	}
	return out
}
