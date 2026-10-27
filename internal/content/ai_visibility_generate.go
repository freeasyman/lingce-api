package content

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

const aiVisibilityGeneratePrompt = `你是医疗内容运营专家。下面是患者问题和 AI 可见性诊断的缺口分析。
请基于这个缺口，写一篇【%s】，输出 JSON：
{
  "title": "标题",
  "body": "正文（分段落，可直接发布）"
}
要求：
  · 主题紧扣患者问题，补上 AI 漏掉的知识
  · 医疗合规：不承诺疗效、不制造焦虑、不诱导就诊
  · 只返回 JSON。`

// ListAIVisibilityRuns 列出历史诊断。
func (s *Service) ListAIVisibilityRuns(ctx context.Context, tenantID int64, limit, offset int, deleted bool) ([]AIVisibilityRun, int, error) {
	// 顺手清理重启导致卡在 running 的僵尸记录
	if n, err := s.store.failStaleAIVisibilityRuns(ctx); err == nil && n > 0 {
		slog.Warn("ai visibility: marked stale running runs as failed", "count", n)
	}
	return s.store.listAIVisibilityRuns(ctx, tenantID, limit, offset, deleted)
}

// GenerateFromAIVisibility 基于缺口诊断生成内容（公众号深度文 / 官网栏目页）。
func (s *Service) GenerateFromAIVisibility(ctx context.Context, tenantID, runID int64, target string, overwrite bool) (map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	run, err := s.store.getAIVisibilityRun(ctx, tenantID, runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("诊断记录不存在")
	}
	channel := ValidateChannel(target)
	if channel == "" {
		return nil, fmt.Errorf("目标渠道不合法：%s", target)
	}
	targetLabel := channel
	sel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_article", "chat"})
	if err != nil {
		return nil, err
	}
	gapText, _ := json.Marshal(run.Gap)
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("ai_visibility_generate"),
		FunctionType:  sel.FunctionType,
		Provider:      sel.Provider,
		ModelCode:     sel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: fmt.Sprintf(s.resolvePromptByCode(ctx, tenantID, "content_ai_visibility_generate", aiVisibilityGeneratePrompt), targetLabel)},
			{Role: "user", Content: fmt.Sprintf("患者问题：%s\n\n缺口分析：%s", run.Question, string(gapText))},
		},
	}
	applyModelParamsToLLMRequest(&req, sel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return nil, err
	}
	obj, err := extractJSONObject(resp.Content)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(fmt.Sprint(obj["title"]))
	body := strings.TrimSpace(fmt.Sprint(obj["body"]))
	if title == "" || title == "<nil>" {
		title = targetLabel + "：" + run.Question
	}
	// 该选题已有文章且用户选择覆盖 → 直接更新那一篇（保留图片/发布包，只换标题正文）
	if overwrite && run.TopicID > 0 {
		if existingID, _, ferr := s.store.findContentIDByTopic(ctx, tenantID, run.TopicID); ferr == nil && existingID > 0 {
			if uerr := s.store.overwriteContentItemBody(ctx, tenantID, existingID, title, body, channel); uerr != nil {
				return nil, uerr
			}
			return map[string]any{
				"content_id": existingID, "title": title, "channel": channel,
				"topic_id": run.TopicID, "insight_id": run.InsightID, "overwritten": true,
			}, nil
		}
	}

	// 带上来源：文章 → 诊断 → 选题/灵感，保证内容库里「来自选题」和溯源能对上
	id, err := s.store.insertContentItemWithSource(ctx, tenantID, run.TopicID, run.InsightID, "article", title, body)
	if err != nil {
		return nil, err
	}
	if err := s.store.updateContentItemChannel(ctx, tenantID, id, channel); err != nil {
		return nil, err
	}
	return map[string]any{
		"content_id": id, "title": title, "channel": channel,
		"topic_id": run.TopicID, "insight_id": run.InsightID,
	}, nil
}

// DeleteAIVisibilityRuns 软删除诊断历史。
func (s *Service) DeleteAIVisibilityRuns(ctx context.Context, tenantID int64, ids []int64) (int64, error) {
	return s.store.softDeleteAIVisibilityRuns(ctx, tenantID, ids)
}

// RestoreAIVisibilityRuns 从回收站恢复诊断记录。
func (s *Service) RestoreAIVisibilityRuns(ctx context.Context, tenantID int64, ids []int64) (int64, error) {
	return s.store.restoreAIVisibilityRuns(ctx, tenantID, ids)
}

// ExistingContentForRun 生成前检查：该诊断的来源选题是否已有文章。
// 用于前端弹窗让用户选「覆盖原文章」还是「新建一条」。
func (s *Service) ExistingContentForRun(ctx context.Context, tenantID, runID int64) (map[string]any, error) {
	run, err := s.store.getAIVisibilityRun(ctx, tenantID, runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, NewBizError("RUN_NOT_FOUND", "诊断记录不存在")
	}
	if run.TopicID <= 0 {
		return map[string]any{"has_existing": false}, nil
	}
	id, title, err := s.store.findContentIDByTopic(ctx, tenantID, run.TopicID)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return map[string]any{"has_existing": false}, nil
	}
	return map[string]any{"has_existing": true, "content_id": id, "title": title}, nil
}
