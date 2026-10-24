package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
)

// 洞察提取提示词：从医生-患者问诊转写中提取 10 类洞察。
const insightExtractionSystemPrompt = `你是资深医疗内容策划，专门从医患问诊转写里判断"哪些内容值得做成传播内容、为什么"。你的产出不是摘要，而是能直接变成选题的原料。

## 一、判断标准（一条洞察至少满足 2 条）
1. 真实：转写里患者或医生确实说了，有原话可逐字引用
2. 具体：有场景细节——时间、动作、情绪、数字、前后对比
3. 有张力：反常识 / 有情绪 / 有身份共鸣 / 解决真实决策障碍
4. 可传播：换个没专业背景的人也能看懂、有代入感

## 二、不要提取（宁缺毋滥）
- 纯流程对话：挂号、缴费、预约、复诊时间安排
- 空泛安抚："别担心""没事的""放心吧"
- 医生单方面的教科书式科普，没有患者反应或追问
- 转写里没有依据的推断
- 隐私信息：姓名、电话、住址、身份证、具体单位

## 三、10 类洞察（判断要点 / 反例）
1. question 问题 —— 患者自己问出口的问题；反例："医生你说吧我听着"
2. concern 顾虑 —— 患者担心、犹豫、下不了决心的点；反例："好，可以"
3. misconception 误区 —— 患者的错误认知 + 医生如何纠正；反例：患者本来就理解正确
4. scene 共鸣场景 —— 让人"这就是我"的具体情境（时间/身份/处境）；反例："很多人都有这种情况"
5. quote 金句 —— 医生说的有传播力的原话（判断句、比喻、大实话）；反例：检查数据罗列
6. aha 认知瞬间 —— 颠覆常识、"原来如此"的点
7. viewpoint 医生观点 —— 医生的明确立场、判断、建议（有态度）
8. explanation 医生讲解 —— 对机制/原理的解释，能讲清"为什么"
9. story 患者故事 —— 患者自述的经历、过程、结果，有时间线和结果
10. fact 数据事实 —— 具体数字、指标、分级标准，可核验

## 四、字段要求
- quote：必须是转写原文，逐字摘录，不改写、不拼接
- speaker：doctor / patient / family
- position：大致位置，如"开头" / "中段" / "结尾前"
- topic：一句话主题，要能直接当选题标题用，禁止"患者咨询XX"这类空话
- core_insight：这条洞察的内容价值是什么（一句话，讲清"观众为什么会看"）
- why_valuable：为什么值得做成内容，引用上面的判断标准
- patient_concern / doctor_point / angle：按类型填，没有就留空
- suggested_forms：口播稿 / 图文 / 文章 / 海报 / 朋友圈（可多选）
- suggested_targets：xiaohongshu / douyin / wechat / shipinhao / website

## 五、质量控制
- 一场 30 分钟问诊通常只值 3-8 条洞察；宁可少，不要凑数。
- 整段都没有值得做成内容的，返回 []。
- 同一段转写里 topic 不能重复。

## 六、输出格式
只返回 JSON 数组，不要任何解释、不要 markdown 代码块：
[{"type":"","topic":"","quote":"","speaker":"","position":"","core_insight":"","why_valuable":"","patient_concern":"","doctor_point":"","angle":"","suggested_forms":[],"suggested_targets":[]}]`

// InsightExtraction 是洞察提取的单条结果。
type InsightExtraction struct {
	ID               int64    `json:"id,omitempty"`
	Type             string   `json:"type"`
	Topic            string   `json:"topic"`
	Quote            string   `json:"quote"`
	Speaker          string   `json:"speaker"`
	Position         string   `json:"position"`
	CoreInsight      string   `json:"core_insight,omitempty"`
	WhyValuable      string   `json:"why_valuable,omitempty"`
	PatientConcern   string   `json:"patient_concern,omitempty"`
	DoctorPoint      string   `json:"doctor_point,omitempty"`
	Angle            string   `json:"angle,omitempty"`
	SuggestedForms   []string `json:"suggested_forms,omitempty"`
	SuggestedTargets []string `json:"suggested_targets,omitempty"`
}

var validInsightTypes = map[string]bool{
	"question": true, "concern": true, "misconception": true, "scene": true, "quote": true,
	"aha": true, "viewpoint": true, "explanation": true, "story": true, "fact": true,
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

// ExtractInsights 从录音转写提取 10 类洞察，落库 content_insights。
func (s *Service) ExtractInsights(ctx context.Context, tenantID, recordingID int64) ([]InsightExtraction, error) {
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
			{Role: "user", Content: "问诊转写如下：\n\n" + transcript},
		},
	}
	applyModelParamsToLLMRequest(&llmReq, selectedModel.ModelParams)

	llmResp, err := s.llmClient.TextInference(ctx, llmReq)
	if err != nil {
		return nil, fmt.Errorf("extract insights via llm: %w", err)
	}

	insights, err := parseInsightExtractions(llmResp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse insight extractions: %w", err)
	}

	stored, err := s.store.insertInsights(ctx, tenantID, recordingID, insights)
	if err != nil {
		return nil, err
	}
	return stored, nil
}

// parseInsightExtractions 解析 llm 返回的洞察 JSON（容忍 markdown 代码块包裹）。
func parseInsightExtractions(content string) ([]InsightExtraction, error) {
	raw := strings.TrimSpace(content)
	// 去掉 ```json ... ``` 包裹
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
	// 取第一个 [ 到最后一个 ]
	if start := strings.Index(raw, "["); start >= 0 {
		if end := strings.LastIndex(raw, "]"); end > start {
			raw = raw[start : end+1]
		}
	}
	var out []InsightExtraction
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("unmarshal insights: %w (raw: %.200s)", err, raw)
	}
	// 过滤非法类型和空主题
	filtered := out[:0]
	for _, it := range out {
		it.Type = strings.TrimSpace(strings.ToLower(it.Type))
		it.Topic = strings.TrimSpace(it.Topic)
		if !validInsightTypes[it.Type] || it.Topic == "" {
			continue
		}
		filtered = append(filtered, it)
	}
	return filtered, nil
}

// insertInsights 落库洞察，返回带 id 的结果。
func (s *Store) insertInsights(ctx context.Context, tenantID, recordingID int64, insights []InsightExtraction) ([]InsightExtraction, error) {
	out := make([]InsightExtraction, 0, len(insights))
	for _, it := range insights {
		evidence := map[string]any{
			"quotes": []map[string]string{{
				"speaker":  it.Speaker,
				"text":     it.Quote,
				"position": it.Position,
			}},
		}
		if it.PatientConcern != "" {
			evidence["patient_concern"] = it.PatientConcern
		}
		if it.DoctorPoint != "" {
			evidence["doctor_point"] = it.DoctorPoint
		}
		if it.Angle != "" {
			evidence["angle"] = it.Angle
		}
		if it.CoreInsight != "" {
			evidence["core_insight"] = it.CoreInsight
		}
		if it.WhyValuable != "" {
			evidence["why_valuable"] = it.WhyValuable
		}
		evidenceJSON, _ := json.Marshal(evidence)
		formsJSON, _ := json.Marshal(it.SuggestedForms)
		targetsJSON, _ := json.Marshal(it.SuggestedTargets)

		var id int64
		err := s.pool.QueryRow(ctx, `
			INSERT INTO content_insights
				(tenant_id, recording_id, source_type, type, topic, evidence, suggested_forms, suggested_targets)
			VALUES ($1,$2,'recording',$3,$4,$5::jsonb,$6::jsonb,$7::jsonb)
			RETURNING id`,
			tenantID, recordingID, it.Type, it.Topic, string(evidenceJSON), string(formsJSON), string(targetsJSON),
		).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("insert insight: %w", err)
		}
		it.ID = id
		out = append(out, it)
	}
	return out, nil
}
