package content

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
)

//go:embed insight_schema.json
var insightSchemaJSON []byte

// 洞察提取提示词（v12）：语义由提示词管（角色/10类/字段判断标准/溯源规则），
// 结构由 insight_schema.json 的 JSON Schema 强制（28 字段、类型、必填）。
const insightExtractionSystemPrompt = `你是资深「医疗新媒体专家」，做「发现 → 洞察 → 判断」三段式工作：从真实医患对话里发现值得传播的内容点、剖析它好在哪戳中谁、判断它值不值得做。
你懂四件事：1) 医患沟通，听得懂患者在担心什么、犹豫什么、没问出口什么；2) 内容传播，知道内容怎么在小红书/公众号/抖音/视频号被搜到刷到；3) 医疗合规，不承诺疗效、不出现患者正脸、不制造焦虑；4) 内容价值分级，判断信息普遍性/稀缺性——普遍做FAQ吃长尾流量，稀缺做差异化权威内容。

【任务】这是医院「内容获客」第一环「洞察」：从问诊转写里把值得做成内容的点一条条挖出来、剖析清楚。下游会变成图文/口播/文章，发到小红书/公众号/抖音/视频号；最终目标：让犹豫患者来咨询/到院、纠正误区建立信任、让患者搜索到/让AI提到我们、建立医生IP。

【重要】一条录音不强求生成洞察，整段平庸就 insights 返回空数组。

【洞察10类】
1 问题：患者自己问出口的问题。2 顾虑：担心、犹豫、下不了决心的点。3 误区：错误认知+医生纠正。4 共鸣场景：让人"这就是我"的具体情境（有画面能复述）。5 金句：医生说的有传播力的话（有比喻/反差/判断/鲜明态度 + 脱离上下文能独立成立 + 医生亲口说的）。金句极稀缺，大多数就诊没有，没有就写 null，绝不硬凑；陈述事实/罗列数据/普通解释/套话安慰都不是金句。6 认知瞬间：颠覆常识"原来如此"。7 医生观点：明确立场判断建议（有态度有取舍）。8 医生讲解：把病/治疗讲透（机制/原理/为什么）。9 患者故事：患者自述经历，有时间线+结果。10 数据事实：可核验的具体数字/指标/分级/时间。

【创作类字段判断标准】
conflict：两个对立诉求/事实同时存在，必须指出对立两方（A vs B）。
emotion_hook：能触发情绪的原话，必须指出"什么情绪+哪句原话"。
counterintuitive_point：颠覆大众认知，必须指出"大众以为X，实际Y"。
number_hook：原话里的具体数字（几周/几分钟/几岁/多少钱）。
identity_resonance：戳中某类人群，必须具体到"哪类人会说这就是我"。
story_arc：有时间线+结果。
trust_arc：犹豫→安心→仍担心，从原话归纳不脑补。

【evidence_refs 规则】evidence_refs 只给【claim 类】字段溯源（这些是能精确追溯到某句原话的断言）：conflict、emotion_hook、counterintuitive_point、number_hook、identity_resonance、golden_quote、story_arc、trust_arc、concern_to_resolve、misconception_to_correct、knowledge_gap。有内容就给它的 quote 序号，null 不列。type/topic/prevalence/fact_statement 等是整条洞察的归纳，不需要逐句溯源。写不出依据的 claim 字段必须写 null，不许编。

【speculated 规则】speculated 填【字段名】数组（如 ["search_queries"]），不要填句子。search_queries 是推测用户会搜什么，必须给出内容并放进 speculated。

【硬规则】一条洞察只聚焦一个点，quotes 只放 2-4 句原话，不同点拆成不同洞察。quotes 是唯一事实源，从 0 编号。fact_statement 只写转写里有的。value_judgment 只写「值不值得做 + 为什么」的理由，不要在文里写「高价值/中价值/低价值」前缀，等级单独填 value_level 字段。value_level 分级：高 = 有强冲突或强情绪或高搜索刚需，优先做；中 = 有价值但不够突出；低 = 可做可不做。value_judgment 禁"最大/全网最高频"这类无数据断言。所有字段值一律用中文：speaker 用「医生/患者/家属/咨询师/客服」，help_dimensions 用「搜索/智能引用/认知/转化」。宁缺毋滥。
`

// Insight 是一条洞察（28 字段，与 insight_schema.json 严格一致）。
type Insight struct {
	Quotes                []InsightQuote          `json:"quotes"`
	Exchange              InsightExchange         `json:"exchange"`
	ContentMarkers        InsightMarkers          `json:"content_markers"`
	Type                  string                  `json:"type"`
	Topic                 string                  `json:"topic"`
	Prevalence            string                  `json:"prevalence"`
	FactStatement         string                  `json:"fact_statement"`
	ValueJudgment         string                  `json:"value_judgment"`
	ValueLevel            string                  `json:"value_level"`
	EvidenceStrength      string                  `json:"evidence_strength"`
	HelpDimensions        []string                `json:"help_dimensions"`
	SearchQueries         []string                `json:"search_queries"`
	KnowledgeGap          string                  `json:"knowledge_gap"`
	MisconceptionToCorrect string                 `json:"misconception_to_correct"`
	ConcernToResolve      string                  `json:"concern_to_resolve"`
	Conflict              string                  `json:"conflict"`
	EmotionHook           string                  `json:"emotion_hook"`
	CounterintuitivePoint string                  `json:"counterintuitive_point"`
	NumberHook            string                  `json:"number_hook"`
	IdentityResonance     string                  `json:"identity_resonance"`
	GoldenQuote           string                  `json:"golden_quote"`
	StoryArc              string                  `json:"story_arc"`
	TrustArc              InsightTrustArc         `json:"trust_arc"`
	SuggestedForms        []string                `json:"suggested_forms"`
	PerForm               map[string]InsightFormScript `json:"per_form"`
	KeyPoints             []string                `json:"key_points"`
	Risks                 InsightRisks            `json:"risks"`
	EvidenceRefs          map[string][]int        `json:"evidence_refs"`
	Speculated            []string                `json:"speculated"`
	ID                    int64                   `json:"id,omitempty"`
}

type InsightQuote struct {
	Text           string `json:"text"`
	Speaker        string `json:"speaker"`
	Position       string `json:"position,omitempty"`
	RoleInExchange string `json:"role_in_exchange,omitempty"`
}

type InsightExchange struct {
	Trigger   string `json:"trigger"`
	Response  string `json:"response"`
	TurnCount int    `json:"turn_count"`
}

type InsightMarkers struct {
	HasNumber           bool `json:"has_number"`
	HasContrast         bool `json:"has_contrast"`
	HasEmotion          bool `json:"has_emotion"`
	HasCounterintuitive bool `json:"has_counterintuitive"`
	HasQuestion         bool `json:"has_question"`
	HasHesitation       bool `json:"has_hesitation"`
	HasDecisionConflict bool `json:"has_decision_conflict"`
}

type InsightTrustArc struct {
	Hesitation  string `json:"hesitation"`
	Reassurance string `json:"reassurance"`
	Unresolved  string `json:"unresolved"`
}

type InsightFormScript struct {
	Hook    string `json:"hook"`
	Opening string `json:"opening"`
	Cta     string `json:"cta"`
}

type InsightRisks struct {
	ForbiddenClaims []string `json:"forbidden_claims"`
	BiasRisk        string   `json:"bias_risk"`
}

var validInsightTypes = map[string]bool{
	"问题": true, "顾虑": true, "误区": true, "共鸣场景": true, "金句": true,
	"认知瞬间": true, "医生观点": true, "医生讲解": true, "患者故事": true, "数据事实": true,
}

// insightOutputSchema 解析内嵌 JSON Schema，包装成网关 output_schema 结构。
func insightOutputSchema() (*llmgateway.OutputSchema, error) {
	var schema map[string]interface{}
	if err := json.Unmarshal(insightSchemaJSON, &schema); err != nil {
		return nil, fmt.Errorf("parse insight schema: %w", err)
	}
	return &llmgateway.OutputSchema{Name: "content_insights", Strict: true, Schema: schema}, nil
}

// fetchRecordingTranscript 取录音转写（优先分段拼接，否则原始转写文本）。
func (s *Store) fetchRecordingTranscript(ctx context.Context, tenantID, recordingID int64) (string, error) {
	var transcript string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(
			NULLIF(CASE
				WHEN jsonb_typeof(cleaned_transcription) = 'array' THEN (
					SELECT string_agg(COALESCE(seg->>'speaker_role', seg->>'speaker', '') || '：' || COALESCE(seg->>'text',''), E'\n')
					FROM jsonb_array_elements(cleaned_transcription) AS seg
				)
				WHEN jsonb_typeof(cleaned_transcription) = 'object' THEN COALESCE(cleaned_transcription->>'full_text', cleaned_transcription->>'text', '')
				WHEN jsonb_typeof(cleaned_transcription) = 'string' THEN trim(both '"' from cleaned_transcription::text)
				ELSE NULL
			END, ''),
			NULLIF(transcription_text, ''),
			''
		)
		FROM recordings WHERE id = $1 AND tenant_id = $2`,
		recordingID, tenantID).Scan(&transcript)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", fmt.Errorf("recording %d not found", recordingID)
		}
		return "", fmt.Errorf("fetch recording transcript: %w", err)
	}
	if strings.TrimSpace(transcript) == "" {
		return "", fmt.Errorf("recording %d has no transcript", recordingID)
	}
	return transcript, nil
}

// ExtractInsights 从录音转写提取洞察（28 字段），落库 content_insights。
func (s *Service) ExtractInsights(ctx context.Context, tenantID, recordingID int64) ([]Insight, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	transcript, err := s.store.fetchRecordingTranscript(ctx, tenantID, recordingID)
	if err != nil {
		return nil, err
	}

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_insight", "content_topic", "chat"})
	if err != nil {
		return nil, fmt.Errorf("resolve llm config for insight extraction: %w", err)
	}

	outSchema, err := insightOutputSchema()
	if err != nil {
		return nil, err
	}

	llmReq := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("insight_extraction"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Billing:       newBillingMetadata("content", "recording", recordingID, "insight_extraction", "content_insight"),
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_insight_extract", insightExtractionSystemPrompt)},
			{Role: "user", Content: "转写文本：\n\n" + transcript},
		},
		Params: &llmgateway.Params{OutputSchema: outSchema},
	}
	applyModelParamsToLLMRequest(&llmReq, selectedModel.ModelParams)

	llmResp, err := s.llmClient.TextInference(ctx, llmReq)
	if err != nil {
		return nil, fmt.Errorf("extract insights via llm: %w", err)
	}

	insights, err := parseInsights(llmResp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse insights: %w", err)
	}

	stored, err := s.store.replaceInsights(ctx, tenantID, recordingID, insights)
	if err != nil {
		return nil, err
	}
	return stored, nil
}

// parseInsights 解析 llm 返回的对象根 {"insights":[...]}（容忍 markdown 代码块包裹）。
func parseInsights(content string) ([]Insight, error) {
	raw := strings.TrimSpace(content)
	if idx := strings.Index(raw, "```"); idx >= 0 {
		raw = raw[idx+3:]
		if j := strings.Index(raw, "json"); j == 0 {
			raw = raw[4:]
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
		Insights []Insight `json:"insights"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal insights: %w (raw: %.200s)", err, raw)
	}
	filtered := wrapper.Insights[:0]
	for _, it := range wrapper.Insights {
		it.Type = strings.TrimSpace(it.Type)
		it.Topic = strings.TrimSpace(it.Topic)
		if !validInsightTypes[it.Type] || it.Topic == "" {
			continue
		}
		it.ValueLevel, it.ValueJudgment = normalizeValueLevel(it.ValueLevel, it.ValueJudgment)
		filtered = append(filtered, it)
	}
	return filtered, nil
}

// normalizeValueLevel 把价值等级与理由彻底分开：
// value_level 为空时从 value_judgment 前缀补出（高/中/低）；
// 无论模型是否遵守提示词，都把 value_judgment 里的「高价值/中等价值/低价值/高/中/低」前缀剥掉，
// 保证 value_judgment 只留理由。
var valueTokenLevel = map[string]string{
	"极高价值": "高", "较高价值": "高", "较低价值": "低",
	"高价值": "高", "中等价值": "中", "中价值": "中", "低价值": "低",
}

func normalizeValueLevel(level, judgment string) (string, string) {
	j := strings.TrimSpace(judgment)
	level = strings.TrimSpace(level)
	// 带「价值」的前缀：极/较/高/中/低 + 价值（不会误伤「高血压」这类词）。
	for _, tok := range []string{"极高价值", "较高价值", "较低价值", "高价值", "中等价值", "中价值", "低价值"} {
		if strings.HasPrefix(j, tok) {
			rest := strings.TrimSpace(strings.TrimLeft(j[len(tok):], ":：。、，"))
			if level == "" {
				level = valueTokenLevel[tok]
			}
			return level, rest
		}
	}
	// 纯「高/中/低」+ 冒号（如「高：」），必须带冒号才剥，避免误伤。
	for _, tok := range []string{"高", "中", "低"} {
		for _, sep := range []string{"：", ":"} {
			prefix := tok + sep
			if strings.HasPrefix(j, prefix) {
				if level == "" {
					level = tok
				}
				return level, strings.TrimSpace(j[len(prefix):])
			}
		}
	}
	return level, j
}

// replaceInsights 用新洞察替换该录音旧的 recording 洞察（重新分析 = 替换，不是追加）。
// worker seed（source_type='worker'）由 worker 自己维护，这里不碰。
// 事务内加 recording 级 advisory lock，避免同一录音并发分析互相覆盖。
func (s *Store) replaceInsights(ctx context.Context, tenantID, recordingID int64, insights []Insight) ([]Insight, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin replace insights tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// 同一录音的洞察替换串行化。
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, fmt.Sprintf("content_insight:%d", recordingID)); err != nil {
		return nil, fmt.Errorf("lock insight extraction: %w", err)
	}

	// 只删本层写的数据（source_type='recording'），worker seed 不动。
	if _, err := tx.Exec(ctx, `
		DELETE FROM content_insights
		WHERE tenant_id=$1 AND recording_id=$2 AND source_type='recording'`,
		tenantID, recordingID); err != nil {
		return nil, fmt.Errorf("delete old insights: %w", err)
	}

	// 提取那一刻固化来源医生/科室（冗余列，避免后续 JOIN 归属漂移）。
	employeeID, employeeName, medicalDeptCode, medicalDeptName := s.recordingOwner(ctx, tenantID, recordingID)

	out := make([]Insight, 0, len(insights))
	for i := range insights {
		it := &insights[i]
		payload, err := json.Marshal(it)
		if err != nil {
			return nil, fmt.Errorf("marshal insight payload: %w", err)
		}
		var id int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO content_insights (tenant_id, recording_id, source_type, type, topic, payload,
				employee_id, employee_name, medical_department_code, medical_department_name)
			VALUES ($1,$2,'recording',$3,$4,$5::jsonb,$6,$7,$8,$9)
			RETURNING id`,
			tenantID, recordingID, it.Type, it.Topic, string(payload),
			employeeID, employeeName, medicalDeptCode, medicalDeptName,
		).Scan(&id); err != nil {
			return nil, fmt.Errorf("insert insight: %w", err)
		}
		it.ID = id
		out = append(out, *it)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit replace insights: %w", err)
	}
	return out, nil
}

// recordingOwner 取录音的医生 + 临床科室（medical_department_code → 中文名），
// 用于提取时固化到洞察冗余列。姓名优先 full_name，其次 name/username。
func (s *Store) recordingOwner(ctx context.Context, tenantID, recordingID int64) (employeeID int64, employeeName, medicalDeptCode, medicalDeptName string) {
	var rawCode string
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(r.employee_id,0),
		       COALESCE(NULLIF(e.full_name,''), NULLIF(e.name,''), NULLIF(e.username,''), ''),
		       COALESCE(e.medical_department_code,'')
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		WHERE r.id = $1 AND r.tenant_id = $2`,
		recordingID, tenantID).Scan(&employeeID, &employeeName, &rawCode)
	return employeeID, employeeName, rawCode, medicalDepartmentName(rawCode)
}

// medicalDepartmentName 把 medical_department_code 映射为中文临床科室名（与 EMR 质量规则一致）。
func medicalDepartmentName(code string) string {
	switch strings.TrimSpace(code) {
	case "internal_medicine":
		return "内科"
	case "surgery":
		return "外科"
	case "gynecology":
		return "妇科"
	case "pediatrics":
		return "儿科"
	case "tcm":
		return "中医科"
	case "stomatology":
		return "口腔科"
	case "ophthalmology":
		return "眼科"
	case "dermatology":
		return "皮肤科"
	case "ent":
		return "耳鼻喉科"
	case "general":
		return "全科"
	case "general_outpatient":
		return "普通门诊"
	default:
		return code
	}
}
