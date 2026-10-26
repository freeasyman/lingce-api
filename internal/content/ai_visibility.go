package content

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// AI 答案可见性：同一患者问题，在多个 AI 引擎各问一遍，
// 逐引擎分析（是否提到机构/医生、信源、竞品、事实错误），最后汇总缺口诊断。

const aiVisibilityAnswerPrompt = `你是一个通用医疗 AI 助手。请中立、客观地回答患者的问题，站在通用知识角度，不主动推荐任何具体医院或医生。回答控制在 300 字以内。`

const aiVisibilityAnalyzePrompt = `你是医疗内容运营专家。下面是患者问题和某个通用 AI 助手的回答。
请分析这个回答，输出 JSON（我方机构名：%s）：
{
  "mentioned_org": false,
  "mentioned_doctor": false,
  "sources": [],
  "competitors": [],
  "fact_errors": [],
  "local_hint": ""
}
sources = AI 回答里提到的信源/引用来源（如官网、公众号、某平台），没有就空数组；
competitors = AI 推荐的竞品机构/医生，没有就空数组；
fact_errors = AI 关于我方机构的事实错误或缺失，没有就空数组；
local_hint = 有没有给出本地具体指向（一句话）。
只返回 JSON。`

const aiVisibilityGapPrompt = `你是医疗内容运营专家。下面是同一个患者问题在多个 AI 引擎的回答分析。
请汇总成机构缺口诊断 + 意见，输出 JSON：
{
  "source_analysis": "信源分析：哪些来源被引用了、哪些没被引用（官网/公众号是否被收录采信）",
  "gap": "缺口：哪类知识没被覆盖、机构事实哪些错了/缺失",
  "advice": "意见：官网该补哪个栏目页面、公众号该写什么、GEO 怎么调"
}
只返回 JSON。`

// aiEngineAnswer 一个引擎的回答 + 分析。
type aiEngineAnswer struct {
	Engine    string         `json:"engine"`
	Provider  string         `json:"provider"`
	ModelCode string         `json:"model_code"`
	Answer    string         `json:"answer"`
	Error     string         `json:"error,omitempty"`
	Analysis  map[string]any `json:"analysis,omitempty"`
}

// listAIEngines 取所有启用的 ai_assistant 引擎配置（按 provider+model 去重）。
func (s *Service) listAIEngines(ctx context.Context, tenantID int64) ([]llmModelSelection, error) {
	query := `
		SELECT COALESCE(function_type,''), COALESCE(provider,''), COALESCE(model_code,''), COALESCE(model_params, extra_params, '{}'::json)
		FROM llm_model_configs
		WHERE deleted_at IS NULL AND COALESCE(is_active,true)=true
		  AND function_type = 'ai_assistant'
		  AND tenant_id IN ($1, 0)
		ORDER BY CASE WHEN tenant_id=$1 THEN 0 ELSE 1 END, COALESCE(is_default,false), id`
	rows, err := s.store.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []llmModelSelection
	for rows.Next() {
		var sel llmModelSelection
		if err := rows.Scan(&sel.FunctionType, &sel.Provider, &sel.ModelCode, &sel.ModelParams); err != nil {
			return nil, err
		}
		key := sel.Provider + "|" + sel.ModelCode
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, sel)
	}
	return out, rows.Err()
}

// engineLabel 引擎显示名。
func engineLabel(provider, modelCode string) string {
	switch strings.ToLower(provider) {
	case "dashscope":
		return "千问"
	case "deepseek":
		return "DeepSeek"
	case "doubao":
		return "豆包"
	case "hunyuan":
		return "元宝"
	}
	return provider + "/" + modelCode
}

// aiEngineAnswer 调一个引擎回答。
func (s *Service) aiEngineAnswer(ctx context.Context, tenantID int64, sel llmModelSelection, question string) (string, error) {
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("ai_visibility_answer"),
		FunctionType:  sel.FunctionType,
		Provider:      sel.Provider,
		ModelCode:     sel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_ai_visibility_answer", aiVisibilityAnswerPrompt)},
			{Role: "user", Content: question},
		},
	}
	applyModelParamsToLLMRequest(&req, sel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

// aiEngineAnalyze 分析一个引擎的回答。
func (s *Service) aiEngineAnalyze(ctx context.Context, tenantID int64, sel llmModelSelection, question, answer, orgName string) (map[string]any, error) {
	prompt := fmt.Sprintf(s.resolvePromptByCode(ctx, tenantID, "content_ai_visibility_analyze", aiVisibilityAnalyzePrompt), orgName)
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("ai_visibility_analyze"),
		FunctionType:  sel.FunctionType,
		Provider:      sel.Provider,
		ModelCode:     sel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: prompt},
			{Role: "user", Content: fmt.Sprintf("患者问题：%s\n\nAI 回答：%s", question, answer)},
		},
	}
	applyModelParamsToLLMRequest(&req, sel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return nil, err
	}
	return extractJSONObject(resp.Content)
}

// aiVisibilityGap 汇总缺口诊断。
func (s *Service) aiVisibilityGap(ctx context.Context, tenantID int64, question string, results []aiEngineAnswer) (map[string]any, error) {
	sel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"ai_assistant", "content_topic", "chat"})
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "患者问题：%s\n\n", question)
	for _, r := range results {
		fmt.Fprintf(&b, "引擎 %s：\n回答：%s\n分析：%v\n\n", r.Engine, r.Answer, r.Analysis)
	}
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("ai_visibility_gap"),
		FunctionType:  sel.FunctionType,
		Provider:      sel.Provider,
		ModelCode:     sel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_ai_visibility_gap", aiVisibilityGapPrompt)},
			{Role: "user", Content: b.String()},
		},
	}
	applyModelParamsToLLMRequest(&req, sel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return nil, err
	}
	return extractJSONObject(resp.Content)
}

// AnalyzeAIVisibility 多引擎查询 + 逐引擎分析 + 缺口诊断。
func (s *Service) AnalyzeAIVisibility(ctx context.Context, tenantID int64, question, region string) (map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}
	region = strings.TrimSpace(region)

	orgName := "我们机构"
	if profile, err := s.store.GetContentOrgProfile(ctx, tenantID); err == nil && profile != nil && profile.InstitutionName != "" {
		orgName = profile.InstitutionName
	}

	q := question
	if region != "" {
		q = region + "，" + question
	}

	engines, err := s.listAIEngines(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if len(engines) == 0 {
		return nil, fmt.Errorf("未配置 AI 引擎（ai_assistant）")
	}

	results := make([]aiEngineAnswer, 0, len(engines))
	for _, eng := range engines {
		item := aiEngineAnswer{Engine: engineLabel(eng.Provider, eng.ModelCode), Provider: eng.Provider, ModelCode: eng.ModelCode}
		answer, err := s.aiEngineAnswer(ctx, tenantID, eng, q)
		if err != nil {
			item.Error = err.Error()
			slog.Warn("ai visibility: engine answer failed", "engine", item.Engine, "error", err)
			results = append(results, item)
			continue
		}
		item.Answer = answer
		analysis, err := s.aiEngineAnalyze(ctx, tenantID, eng, q, answer, orgName)
		if err != nil {
			item.Error = err.Error()
			slog.Warn("ai visibility: engine analyze failed", "engine", item.Engine, "error", err)
		} else {
			item.Analysis = analysis
		}
		results = append(results, item)
	}

	gap := map[string]any{}
	if g, err := s.aiVisibilityGap(ctx, tenantID, q, results); err == nil {
		gap = g
	} else {
		slog.Warn("ai visibility: gap failed", "error", err)
	}

	engAny := make([]any, 0, len(results))
	for _, r := range results {
		engAny = append(engAny, r)
	}
	runID, saveErr := s.store.saveAIVisibilityRun(ctx, tenantID, q, region, orgName, engAny, gap)
	if saveErr != nil {
		slog.Warn("ai visibility: save run failed", "error", saveErr)
	}
	return map[string]any{
		"run_id":   runID,
		"question": q,
		"region":   region,
		"org_name": orgName,
		"engines":  results,
		"gap":      gap,
	}, nil
}
