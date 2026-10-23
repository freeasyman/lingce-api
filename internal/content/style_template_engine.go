package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

const learnStyleTemplatePrompt = `你是视觉设计分析专家。分析这张图片的视觉风格，提取可复用的设计参数，返回 JSON：
{"colors":["#hex"],"fonts":"字体风格描述","spacing":"间距/留白描述","decorations":"装饰元素描述(色块/徽章/虚线框/渐变等)","layout":"版式描述","highlights":"重点字高亮方式"}。
只返回 JSON，不要其他内容。`

const verifyImageTextPrompt = `逐字原样读出图中所有文字。然后与下面"预期文字"逐字对比，找出：1) 错字/乱码 2) 缺失的字 3) 多出的字。
若完全一致返回 {"pass":true,"errors":[]}；否则返回 {"pass":false,"errors":["图中"X"应为"Y""]}。
只返回 JSON，不要其他内容。`

// LearnStyleTemplateRequest 学风格模板请求。
type LearnStyleTemplateRequest struct {
	ImageURL string `json:"image_url"`
	Name     string `json:"name"`
	Type     string `json:"type"` // visual / layout
	Industry string `json:"industry,omitempty"`
	Level    string `json:"level,omitempty"` // global / industry / tenant
}

// VerifyImageTextResult OCR 校验结果。
type VerifyImageTextResult struct {
	Pass   bool     `json:"pass"`
	Errors []string `json:"errors"`
}

// multimodalCall 调多模态 llm，返回文本内容。
func (s *Service) multimodalCall(ctx context.Context, tenantID int64, imageURL, prompt string) (string, error) {
	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"image_analysis", "image_understanding", "chat"})
	if err != nil {
		return "", fmt.Errorf("resolve llm config for multimodal: %w", err)
	}
	req := llmgateway.ImageInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("multimodal"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Messages: []llmgateway.ImageMessage{
			{Role: "user", Content: []llmgateway.ImageContentPart{
				{Image: imageURL},
				{Text: prompt},
			}},
		},
	}
	resp, err := s.llmClient.ImageInference(ctx, req)
	if err != nil {
		return "", fmt.Errorf("multimodal inference failed: %w", err)
	}
	return resp.Content, nil
}

// extractJSONObject 从 llm 文本中提取 JSON 对象（容忍 markdown 包裹）。
func extractJSONObject(content string) (map[string]any, error) {
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
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("unmarshal json: %w (raw: %.200s)", err, raw)
	}
	return out, nil
}

// LearnStyleTemplate 从截图学风格模板，落库 content_style_templates。
func (s *Service) LearnStyleTemplate(ctx context.Context, tenantID int64, req LearnStyleTemplateRequest) (map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	if strings.TrimSpace(req.ImageURL) == "" {
		return nil, fmt.Errorf("image_url is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "学到的模板"
	}
	tplType := strings.TrimSpace(req.Type)
	if tplType != "layout" {
		tplType = "visual"
	}
	level := strings.TrimSpace(req.Level)
	if level == "" {
		level = "tenant"
	}

	content, err := s.multimodalCall(ctx, tenantID, req.ImageURL, s.resolvePromptByCode(ctx, tenantID, "content_learn_style", learnStyleTemplatePrompt))
	if err != nil {
		return nil, err
	}
	style, err := extractJSONObject(content)
	if err != nil {
		return nil, err
	}
	styleJSON, _ := json.Marshal(style)

	var id int64
	err = s.store.pool.QueryRow(ctx, `
		INSERT INTO content_style_templates (tenant_id, level, industry, type, name, reference_url, extracted_style)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)
		RETURNING id`,
		tenantID, level, req.Industry, tplType, name, req.ImageURL, string(styleJSON),
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("insert style template: %w", err)
	}

	result := map[string]any{"id": id, "name": name, "type": tplType, "style": style}
	return result, nil
}

// VerifyImageText 校验图片文字 vs 预期文字，返回错字/缺失。
func (s *Service) VerifyImageText(ctx context.Context, tenantID int64, imageURL, expectedText string) (*VerifyImageTextResult, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	if strings.TrimSpace(imageURL) == "" {
		return nil, fmt.Errorf("image_url is required")
	}
	prompt := s.resolvePromptByCode(ctx, tenantID, "content_verify_image_text", verifyImageTextPrompt) + "\n\n预期文字：\n" + expectedText
	content, err := s.multimodalCall(ctx, tenantID, imageURL, prompt)
	if err != nil {
		return nil, err
	}
	obj, err := extractJSONObject(content)
	if err != nil {
		return nil, err
	}
	result := &VerifyImageTextResult{Pass: true}
	if v, ok := obj["pass"].(bool); ok {
		result.Pass = v
	}
	if errs, ok := obj["errors"].([]any); ok {
		for _, e := range errs {
			if s, ok := e.(string); ok {
				result.Errors = append(result.Errors, s)
			}
		}
	}
	return result, nil
}
