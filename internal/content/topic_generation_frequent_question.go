package content

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// GenerateTopicsFromFrequentQuestionAsync 异步：创建任务并后台生成，立即返回 job_id。
// 复用 AI 口碑的 run 模式：create → goroutine → 前端轮询。
func (s *Service) GenerateTopicsFromFrequentQuestionAsync(ctx context.Context, tenantID, frequentQuestionID int64) (int64, error) {
	fq, err := s.store.getFrequentQuestionByID(ctx, tenantID, frequentQuestionID)
	if err != nil {
		return 0, err
	}
	if fq == nil {
		return 0, NewBizError("FREQUENT_QUESTION_NOT_FOUND", "高频问题不存在，请先重新梳理高频问题")
	}
	// 数量上限：已做够直接拒绝（建任务前就拦掉）
	if len(fq.InsightIDs) > 0 {
		if existing, _ := s.store.listExistingTopicTitles(ctx, tenantID, fq.InsightIDs, fq.Topic); len(existing) >= maxTopicsPerFrequentQuestion {
			return 0, NewBizError("TOPIC_LIMIT_REACHED",
				fmt.Sprintf("这个高频问题已经做过 %d 个选题了，角度覆盖得差不多了。建议先挑几个发布看效果，不要硬凑同款", len(existing)))
		}
	}
	// 防重复：同一高频问题已有 running 任务则直接复用，不重复提交
	if running, err := s.store.getRunningTopicGenerationJob(ctx, tenantID, frequentQuestionID); err == nil && running > 0 {
		return running, nil
	}
	jobID, err := s.store.createTopicGenerationJob(ctx, tenantID, frequentQuestionID)
	if err != nil {
		return 0, err
	}
	go s.loadTopicGenerationJob(context.Background(), tenantID, jobID, frequentQuestionID)
	return jobID, nil
}

// loadTopicGenerationJob 后台执行选题生成，边跑边更新进度；完成/失败都写回任务。
func (s *Service) loadTopicGenerationJob(ctx context.Context, tenantID, jobID, frequentQuestionID int64) {
	_ = s.store.updateTopicGenerationJobProgress(ctx, tenantID, jobID, 20)
	topics, err := s.generateTopicsFromFrequentQuestionCore(ctx, tenantID, frequentQuestionID)
	if err != nil {
		_ = s.store.failTopicGenerationJob(ctx, tenantID, jobID, err.Error())
		return
	}
	resultJSON, _ := json.Marshal(topics)
	if err := s.store.finishTopicGenerationJob(ctx, tenantID, jobID, resultJSON); err != nil {
		slog.Error("finish topic generation job", "error", err, "job_id", jobID)
	}
}

// maxTopicsPerFrequentQuestion 一个高频问题最多做多少个选题。
// 素材有限，做太多必然同质化；够了就先发布看效果，不要硬凑。
const maxTopicsPerFrequentQuestion = 3

// generateTopicsFromFrequentQuestionCore 从「高频问题」整体生成 1~3 个选题（不做逐条洞察 fan-out）。
func (s *Service) generateTopicsFromFrequentQuestionCore(ctx context.Context, tenantID, frequentQuestionID int64) ([]map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	fq, err := s.store.getFrequentQuestionByID(ctx, tenantID, frequentQuestionID)
	if err != nil {
		return nil, err
	}
	if fq == nil {
		return nil, NewBizError("FREQUENT_QUESTION_NOT_FOUND", "高频问题不存在")
	}
	if len(fq.InsightIDs) == 0 {
		return nil, NewBizError("FREQUENT_QUESTION_NO_INSIGHTS", "这个高频问题没有关联到灵感，无法生成选题")
	}

	payloads, err := s.store.fetchInsightPayloads(ctx, tenantID, fq.InsightIDs)
	if err != nil {
		return nil, err
	}

	var quotes []string
	_ = json.Unmarshal(fq.Quotes, &quotes)

	// 结果去重：把该高频问题已生成过的选题标题带进提示词，避免重复
	existingTitles, _ := s.store.listExistingTopicTitles(ctx, tenantID, fq.InsightIDs, fq.Topic)
	if len(existingTitles) >= maxTopicsPerFrequentQuestion {
		return nil, NewBizError("TOPIC_LIMIT_REACHED",
			fmt.Sprintf("这个高频问题已经做过 %d 个选题了，角度覆盖得差不多了。建议先挑几个发布看效果，不要硬凑同款", len(existingTitles)))
	}
	existingBlock := ""
	if len(existingTitles) > 0 {
		existingBlock = "\n\n【已经做过的选题——不要重复，也不要换个说法重写同一个角度】\n" +
			strings.Join(existingTitles, "\n") +
			"\n\n如果上面的选题已经覆盖了某个角度，你必须换一个【完全不同的切入角度】（换人群/换场景/换疑问/换结论）；" +
			"同时【不要复用同一个开头钩子/引语】（例如不要在已用过「我选3.0行不行？」后再次用它起头）；" +
			"如果实在没有新角度，就返回空数组，绝不硬凑、绝不换词重写。"
	}

	userPrompt := strings.Join([]string{
		"高频问题：" + fq.Topic,
		fmt.Sprintf("被问次数：%d 次", fq.Frequency),
		"代表原话：" + strings.Join(quotes, "\n"),
		"聚合素材（JSON）：" + payloads,
	}, "\n\n") + existingBlock

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
		Billing:       newBillingMetadata("content", "frequent_question", frequentQuestionID, "frequent_question_topic_generation", "content_topic"),
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

	// 质量兜底：clamp 到 1~3；同标题去重；与已有选题去重
	topics = dedupTopics(topics)
	topics = filterDuplicateTitles(topics, existingTitles)
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

// dedupTopics 选题去重：标题语义高度相近（字符二元组 Jaccard ≥ 0.55）的只留第一个。
func dedupTopics(in []TopicV2) []TopicV2 {
	var out []TopicV2
	for _, t := range in {
		dup := false
		for _, e := range out {
			if titleSimilar(t.Title, e.Title) {
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

// titleSimilar 判断两个标题是否"同款"：
// 完全相等 / 包含且占比高 / 字符二元组 Jaccard ≥ 0.35 / 复用同一个开头钩子。
func titleSimilar(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		short, long := a, b
		if len(b) < len(a) {
			short, long = b, a
		}
		if float64(len([]rune(short)))/float64(len([]rune(long))) >= 0.8 {
			return true
		}
	}
	// 复用同一个开头钩子（例如都从「我选3.0行不行？」起头）也算同款
	ha, hb := titleHook(a), titleHook(b)
	if ha != "" && ha == hb && len([]rune(ha)) >= 6 {
		return true
	}
	return bigramJaccard(a, b) >= 0.35
}

// titleHook 取标题的开头钩子：优先取开头「...」引语，否则取前 10 个字符。
func titleHook(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "「") {
		if idx := strings.Index(s, "」"); idx > 0 {
			return s[:idx+len("」")]
		}
	}
	r := []rune(s)
	if len(r) > 10 {
		return string(r[:10])
	}
	return s
}

// bigramJaccard 字符二元组 Jaccard 相似度（对中文标题足够稳）。
func bigramJaccard(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) < 2 || len(rb) < 2 {
		return 0
	}
	setA := make(map[string]struct{}, len(ra))
	for i := 0; i < len(ra)-1; i++ {
		setA[string(ra[i:i+2])] = struct{}{}
	}
	setB := make(map[string]struct{}, len(rb))
	for i := 0; i < len(rb)-1; i++ {
		setB[string(rb[i:i+2])] = struct{}{}
	}
	inter := 0
	for k := range setA {
		if _, ok := setB[k]; ok {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// filterDuplicateTitles 丢掉与已有选题标题"同款"的。
func filterDuplicateTitles(in []TopicV2, existing []string) []TopicV2 {
	if len(existing) == 0 {
		return in
	}
	var out []TopicV2
	for _, t := range in {
		dup := false
		for _, e := range existing {
			if titleSimilar(t.Title, e) {
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
