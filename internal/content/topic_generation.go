package content

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// 判断层：从洞察生成选题（创作简报）。

//go:embed topic_schema.json
var topicSchemaJSON []byte

// topicOutputSchema 解析内嵌选题 JSON Schema，包装成网关 output_schema 结构。
func topicOutputSchema() (*llmgateway.OutputSchema, error) {
	var schema map[string]interface{}
	if err := json.Unmarshal(topicSchemaJSON, &schema); err != nil {
		return nil, fmt.Errorf("parse topic schema: %w", err)
	}
	return &llmgateway.OutputSchema{Name: "content_topics", Strict: true, Schema: schema}, nil
}

const topicGenerationFromInsightPrompt = `你是资深「医疗内容策划专家」，做「判断 → 策划」两段式工作：从洞察（已经挖好的内容点）里判断"我们该做什么内容、从哪个角度做"，并决定"用什么表现形式、发到哪个媒体"，再策划成一份能直接交给创作层动笔的内容简报。

你懂四件事：
1) 选题判断 —— 什么样的内容点值得做、从哪个角度切入最有传播力；
2) 媒体与形式 —— 公众号/小红书/抖音/视频号/朋友圈/官网各自适合发什么；文章/图文/口播/海报/分镜各自适合表达什么，怎么选；
3) 医疗合规 —— 不承诺疗效、不制造焦虑、不出现患者正脸；
4) 内容价值分级 —— 判断信息普遍性/稀缺性：普遍做 FAQ 吃长尾流量，稀缺做差异化权威。

【任务】这是医院「内容获客」第二环「选题」。
你的交付不是"几条标题"，而是——每个选题做出来的内容，最终都要能命中至少一个【获客效果】：
  搜索 —— 让患者搜索到我们（命中患者的搜索词）
  认知 —— 纠正一个误区、建立信任（患者看完"原来如此"）
  转化 —— 带来咨询 / 到院 / 留资的转化线索
判断一个选题成不成立，标准就是：它做出来的内容，能不能实实在在命中上面至少一个效果。命中不了的，就不该生成。

【重要】一条洞察不强求生成多个选题。只有存在多个【有意义的、能命中不同获客效果的】角度时，才拆分。没有第二个有意义的角度，就只出 1 条，绝不硬凑。整条洞察平庸，就返回空数组。

【输出字段定义】
- title（标题）：能直接发出去的标题，一眼看出讲什么、戳中谁，落到具体点（误区/数据/症状）。技巧：必须借用洞察的钩子——数字钩子→"3个信号…"；反常识→"别把X当Y"；身份共鸣→"30岁+的男人…"；金句→直接用医生金句；痛点直击→"你是不是也…"；误区纠正→"X不是Y，是Z"。原则：有钩子才用钩子型标题，没有就朴实准确，不硬套模板。不是："关于前列腺的那些事"这种空泛标题。
- recommendation（推荐理由）：用洞察类型的语言说清"为什么值得做"（因为这是金句/误区/医生讲解/患者反复问的问题）。不是："这个内容很有价值"这种空话。
- goal（目标）：从 科普/品牌/医生IP/转化/GEO 选一个，必须和切入角度一致。不是：随便填一个、和角度对不上。
- expected_help（预期帮助）：搜索/认知/转化三维，每维都要有依据（搜索←search_queries，认知←误区/顾虑，转化=预期）。不是：编数据、承诺"带来XX个咨询"。
- core_claim（核心主张）：这篇内容的结论，一句话说清，来自洞察的价值判断/事实。不是："要重视前列腺健康"这种没有具体结论的话。
- angle（切入角度）：必须说清"从哪个点切入"（从误区纠正/从患者顾虑/从医生金句）。不是："科普一下"这种空话。
- audience（目标人群）：具体到"哪类人看到会说这就是我"（身份+处境）。不是："男性""患者"这种泛称。
- suggested_forms（建议形式）：从 文章/图文/口播/海报/分镜/朋友圈 里，选适合这个角度+目标的（多个，按优先级排）。不是：把 6 种形式全列。
- facts[]（事实）：只写洞察 fact_statement 里有的，逐字照搬或最小改写。不是：脑补机制、添加转写里没有的事实。
- quotes[]（原话）：从洞察 quotes 里精选，选这个角度用得到的原话，保留说话人。不是：改写原话、编造原话。
- golden_quote（金句）：洞察里 golden_quote 有就放，没有就 null。不是：硬凑一句"金句"。
- key_points[]（要点）：正文要讲的关键点，来自洞察 key_points，精选。不是：把 key_points 全塞。
- per_form_structure（结构骨架）：按每种建议形式给骨架，具体到"每一节/每张卡讲什么"。文章→sections[]（章节大纲）；图文→cards[]（卡片序列）；口播→segments[]（开头钩子/正文/结尾CTA）；分镜→shots[]（每个镜头=画面{医生出镜/医生动作/场景空镜/物品特写/字幕动画}+台词/旁白）；朋友圈→images 数+caption_points[]；海报→headline+selling_points[]+cta。不是：只写"开头-正文-结尾"这种空骨架。
- forbidden_claims[]（合规红线）：从洞察 risks.forbidden_claims 传递，明确"不能说什么"。不是：有风险却不标、留空。
- bias_risk（误导风险）：从洞察 risks.bias_risk 传递，明确"哪里可能误导"。不是：漏掉。

【输出 JSON 格式】（只返回这个结构的 JSON 对象，不要解释）
{"topics":[{"title":"string","recommendation":"string","goal":"科普|品牌|医生IP|转化|GEO","expected_help":{"search":["string"],"cognition":"string|null","conversion":"string|null"},"core_claim":"string","angle":"string","audience":"string","suggested_forms":["文章|图文|口播|海报|分镜|朋友圈"],"facts":["string"],"quotes":[{"text":"string","speaker":"string"}],"golden_quote":"string|null","key_points":["string"],"per_form_structure":{"文章":{"sections":[{"title":"string","point":"string"}]},"图文":{"cards":[{"title":"string","text":"string"}]},"口播":{"segments":[{"role":"开头|正文|结尾","text":"string"}]},"分镜":{"shots":[{"visual":"string","audio":"string"}]},"朋友圈":{"images":"number","caption_points":["string"]},"海报":{"headline":"string","selling_points":["string"],"cta":"string"}},"forbidden_claims":["string"],"bias_risk":"string|null"}]}

说明：suggested_forms 里选了哪几种形式，per_form_structure 就只出现哪几种（不列没选的形式）；expected_help.cognition/conversion、golden_quote、bias_risk 没有内容写 null；facts/quotes/key_points/forbidden_claims 没有内容写 []。

【拆分规则】1 条洞察生成 1~N 个选题，N 由"有几个有意义的、命中不同获客效果的角度"决定。拆分标准：不同角度 = 不同 goal（科普角度→科普；医生IP角度→医生IP；转化角度→转化）。规则：只有 1 个有意义的角度就只出 1 条；有 2-3 个有意义的角度才拆 2-3 条，每个角度一条；拆分时素材共享（事实/原话/金句/要点共用），但每个选题的标题/角度/目标/结构不同；没有第二个有意义的角度绝不硬凑。

【素材溯源规则】facts[] 必须来自 fact_statement；quotes[] 必须来自 quotes（保留原话和说话人）；golden_quote 有就放、没有就 null；key_points[] 必须来自 key_points；forbidden_claims[] / bias_risk 必须来自 risks；core_claim / angle 必须能追溯到 value_judgment / concern / misconception / conflict / golden_quote。所有素材不脑补、不编造。

【硬规则】
0. 每个选题必须命中至少一个获客效果（搜索/认知/转化），命中不了的就不生成。
1. 所有素材必须来自洞察，不脑补、不编造。
2. 标题必须借用洞察的钩子，没有钩子就朴实准确，不硬套模板。
3. goal 必须和 angle 一致。
4. suggested_forms 必须和 angle+goal 匹配，不把 6 种形式全列。
5. 不承诺疗效、不制造焦虑、不出现患者正脸。
6. 每个字段都必须出现，没有内容写 null 或 []，不许省略字段。
7. 只返回 JSON 对象，不要解释。`

// insightFieldGlossary 洞察字段说明（选题模型的输入定义）。
const insightFieldGlossary = `洞察字段说明（你据此判断，禁止脑补）：
type：洞察类型（问题/顾虑/误区/共鸣场景/金句/认知瞬间/医生观点/医生讲解/患者故事/数据事实）
topic：规范化主题
value_level：价值等级（高/中/低）
value_judgment：值不值得做 + 为什么
prevalence：普遍性（普遍/稀缺/中）
fact_statement：客观事实（只写转写里有的）
evidence_strength：证据强度（强/中/弱）
quotes[]：原话（text + speaker）
golden_quote：金句（可能为 null）
key_points[]：要点
concern_to_resolve：患者顾虑
misconception_to_correct：纠正认知
conflict：冲突（A vs B）
emotion_hook：情绪钩子
counterintuitive_point：反常识
number_hook：数字钩子
identity_resonance：戳中人群
search_queries[]：患者会搜的词
help_dimensions[]：预期帮助维度（搜索/认知/转化）
suggested_forms[]：建议形式
risks：{ forbidden_claims[], bias_risk }`

// TopicV2 是判断层生成的选题（创作简报，对应 content_topics.brief）。
type TopicV2 struct {
	Title            string             `json:"title"`
	Recommendation   string             `json:"recommendation"`
	Goal             string             `json:"goal"`
	ExpectedHelp     *TopicExpectedHelp `json:"expected_help"`
	CoreClaim        string             `json:"core_claim"`
	Angle            string             `json:"angle"`
	Audience         string             `json:"audience"`
	SuggestedForms   []string           `json:"suggested_forms"`
	Facts            []string           `json:"facts"`
	Quotes           []TopicQuote       `json:"quotes"`
	GoldenQuote      string             `json:"golden_quote"`
	KeyPoints        []string           `json:"key_points"`
	PerFormStructure map[string]any     `json:"per_form_structure"`
	ForbiddenClaims  []string           `json:"forbidden_claims"`
	BiasRisk         string             `json:"bias_risk"`
}

// TopicExpectedHelp 预期帮助（搜索/认知/转化，智能引用暂缓）。
type TopicExpectedHelp struct {
	Search     []string `json:"search"`
	Cognition  string   `json:"cognition"`
	Conversion string   `json:"conversion"`
}

// TopicQuote 选题引用的原话。
type TopicQuote struct {
	Text    string `json:"text"`
	Speaker string `json:"speaker"`
}

// GenerateTopicsFromInsights 从洞察生成选题（逐条洞察，1 洞察 → N 选题）。
func (s *Service) GenerateTopicsFromInsights(ctx context.Context, tenantID int64, insightIDs []int64) ([]map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	if len(insightIDs) == 0 {
		return nil, fmt.Errorf("insight_ids is required")
	}

	outSchema, err := topicOutputSchema()
	if err != nil {
		return nil, err
	}

	var all []map[string]any
	for _, insightID := range insightIDs {
		stored, err := s.generateTopicsForOneInsight(ctx, tenantID, insightID, outSchema)
		if err != nil {
			return nil, err
		}
		all = append(all, stored...)
	}
	return all, nil
}

// generateTopicsForOneInsight 对单条洞察生成选题（共享素材 + 不同角度）。
func (s *Service) generateTopicsForOneInsight(ctx context.Context, tenantID, insightID int64, outSchema *llmgateway.OutputSchema) ([]map[string]any, error) {
	payload, err := s.store.fetchInsightPayload(ctx, tenantID, insightID)
	if err != nil {
		return nil, err
	}

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_topic", "topic_generation", "chat"})
	if err != nil {
		return nil, fmt.Errorf("resolve llm config for topic generation: %w", err)
	}

	userPrompt := "洞察如下（JSON）：\n\n" + payload + "\n\n" + insightFieldGlossary

	llmReq := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("topic_generation"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Billing:       newBillingMetadata("content", "insight", insightID, "topic_generation", "content_topic"),
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_topic_gen", topicGenerationFromInsightPrompt)},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{OutputSchema: outSchema, TimeoutSeconds: 180},
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

	return s.store.insertTopicsV2(ctx, tenantID, topicSourceInfo{Source: "insight"}, []int64{insightID}, topics)
}

// parseTopicsV2 解析选题 JSON（对象根 {"topics":[...]}）。
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
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	var wrapper struct {
		Topics []TopicV2 `json:"topics"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal topics: %w (raw: %.200s)", err, raw)
	}
	filtered := wrapper.Topics[:0]
	for _, t := range wrapper.Topics {
		if strings.TrimSpace(t.Title) == "" {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered, nil
}

// fetchInsightPayload 取洞察的完整 28 字段 JSON（选题模型的输入）。
func (s *Store) fetchInsightPayload(ctx context.Context, tenantID, insightID int64) (string, error) {
	var payload json.RawMessage
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(payload, evidence, '{}'::jsonb)
		FROM content_insights WHERE id = $1 AND tenant_id = $2`,
		insightID, tenantID).Scan(&payload)
	if err != nil {
		return "", fmt.Errorf("fetch insight payload: %w", err)
	}
	return string(payload), nil
}

// topicSourceInfo 选题来源信息（洞察 / 高频问题 / 手动）。
type topicSourceInfo struct {
	Source           string // insight / cluster / manual
	ClusterID        int64
	ClusterQuestion  string
	ClusterFrequency int
}

// insertTopicsV2 落库选题：完整创作简报存 brief，冗余列供列表展示。
func (s *Store) insertTopicsV2(ctx context.Context, tenantID int64, src topicSourceInfo, insightIDs []int64, topics []TopicV2) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(topics))
	for _, t := range topics {
		briefJSON, _ := json.Marshal(t)
		idsJSON, _ := json.Marshal(insightIDs)
		helpJSON, _ := json.Marshal(t.ExpectedHelp)
		form := ""
		if len(t.SuggestedForms) > 0 {
			form = t.SuggestedForms[0]
		}
		var id int64
		err := s.pool.QueryRow(ctx, `
			INSERT INTO content_topics (tenant_id, title, form, goal, source, recommendation, expected_help, brief, insight_ids,
			                            source_cluster_id, source_cluster_question, source_cluster_frequency)
			VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9::jsonb,
			        NULLIF($10,0), NULLIF($11,''), NULLIF($12,0))
			RETURNING id`,
			tenantID, t.Title, form, t.Goal, src.Source, t.Recommendation, string(helpJSON), string(briefJSON), string(idsJSON),
			src.ClusterID, src.ClusterQuestion, src.ClusterFrequency,
		).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("insert topic: %w", err)
		}
		out = append(out, map[string]any{"id": id, "title": t.Title, "goal": t.Goal, "recommendation": t.Recommendation})
	}
	return out, nil
}
