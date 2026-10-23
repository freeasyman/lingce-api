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
const insightExtractionSystemPrompt = `你是医疗内容洞察专家，擅长从医生与患者的问诊转写中，挖出"值得做成内容"的洞察。

请从转写中提取以下 10 类洞察（有则提取，没有则跳过；宁缺毋滥）：

1. question     问题：患者真正问的问题
2. concern      顾虑：患者担心、犹豫的点
3. misconception 误区：患者的错误认知，及医生如何纠正
4. scene        共鸣场景：让人"这就是我"的具体情境
5. quote        金句：医生说的、有传播力的原话
6. aha          认知瞬间：颠覆常识、"原来如此"的点
7. viewpoint    医生观点：医生的明确立场、判断、建议
8. explanation  医生讲解：医生对疾病/治疗的讲解
9. story        患者故事：患者自述的经历、过程、结果
10. fact        数据/事实：医生提到的具体数字、指标、可核验事实

要求：
- 每条洞察必须来自转写原文，不得编造。
- 每条给 quote（原话）、speaker（doctor/patient）、position（大致时间点）。
- topic 是规范化的一句话主题。
- 可选：patient_concern（问题/顾虑/误区类）、doctor_point（医生类）、angle（切入角度）、suggested_forms（建议形式）、suggested_targets（建议平台）。
- 不提取低价值、泛泛而谈的内容。

只返回 JSON 数组，格式：
[{"type":"...","topic":"...","quote":"...","speaker":"...","position":"...","patient_concern":"...","doctor_point":"...","angle":"...","suggested_forms":["..."]}]`

// InsightExtraction 是洞察提取的单条结果。
type InsightExtraction struct {
	Type             string   `json:"type"`
	Topic            string   `json:"topic"`
	Quote            string   `json:"quote"`
	Speaker          string   `json:"speaker"`
	Position         string   `json:"position"`
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
			NULLIF((
				SELECT string_agg(COALESCE(seg->>'speaker_role','') || '：' || COALESCE(seg->>'text',''), E'\n')
				FROM jsonb_array_elements(cleaned_transcription) AS seg
			), ''),
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
		_ = id
		out = append(out, it)
	}
	return out, nil
}
