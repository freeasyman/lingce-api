package content

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// 计划引擎：做计划（生成月度计划草案 + 计划说明）、排选题。

const planSummaryPrompt = `你是医疗内容运营专家。根据下面"本月计划"（账号 + 选题 + 反复问依据），写一份「本月计划说明」，讲清楚"为什么这么排"，而不是复述工作量。

严格按这四段写（用【】标段名）：
【方向总览】本月照顾哪几个方向，一句话。
【分方向说明】每个方向：为什么做 + 做多少 + 依据（洞察类型 / 反复问频次 / 节令 / 角色）。
【配比逻辑】为什么是这个比例，明确取舍了什么（不做什么 + 为什么不做）。
【节奏预期】本月预期补什么，用"预期 / 可能 / 参考"，不承诺数字。

要求：
- 多主题平衡，不是单一主线。
- 判断必须有依据，没依据的宁可不写。
- 不写正确的废话（如"提升服务质量"这种）。
- 宁短勿空，总共 200~400 字。`

// GeneratePlan 生成月度计划：建计划周期 + 按账号目标分配选题 + 生成计划说明。
func (s *Service) GeneratePlan(ctx context.Context, tenantID int64, period, startDate string) (map[string]any, error) {
	period = strings.TrimSpace(period)
	if period == "" {
		period = "month"
	}
	startDate = strings.TrimSpace(startDate)
	if startDate == "" {
		startDate = time.Now().Format("2006-01-02")
	}

	// 一个时间窗口一个计划：已存在则返回现有（不重复生成）
	if existingID, exists, err := s.store.findPlanPeriod(ctx, tenantID, period, startDate); err != nil {
		return nil, fmt.Errorf("find plan period: %w", err)
	} else if exists {
		plan, err := s.loadPlan(ctx, tenantID, existingID, period, startDate)
		if err != nil {
			return nil, err
		}
		// 已有计划且有条目 → 直接返回；空计划（上次失败残留）→ 清掉重建
		if items, ok := plan["items"].([]map[string]any); ok && len(items) > 0 {
			return plan, nil
		}
		if _, err := s.store.pool.Exec(ctx, `DELETE FROM content_plan_items WHERE plan_period_id = $1`, existingID); err != nil {
			return nil, fmt.Errorf("clear empty plan items: %w", err)
		}
		if _, err := s.store.pool.Exec(ctx, `DELETE FROM content_plan_periods WHERE id = $1`, existingID); err != nil {
			return nil, fmt.Errorf("clear empty plan period: %w", err)
		}
	}

	// 1. 账号
	accounts, err := s.store.listMediaAccounts(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no media accounts; create accounts first")
	}

	// 2. 选题（未排进计划的，按 id 倒序）
	topics, err := s.store.listTopicsV2(ctx, tenantID, 200)
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}

	// 3. 建计划周期
	planPeriodID, err := s.store.getOrCreatePlanPeriod(ctx, tenantID, period, startDate)
	if err != nil {
		return nil, fmt.Errorf("create plan period: %w", err)
	}

	// 4. 分配选题到账号（按目标量，round-robin）
	type assigned struct {
		accountIdx int
		topic      ContentTopicV2Record
	}
	var assignedList []assigned
	topicIdx := 0
	totalTarget := 0
	for _, a := range accounts {
		totalTarget += effectiveMonthlyTarget(a)
	}
	for topicIdx < len(topics) {
		assignedAny := false
		for ai := range accounts {
			a := &accounts[ai]
			target := effectiveMonthlyTarget(*a)
			// 该账号已分配数量
			count := 0
			for _, x := range assignedList {
				if x.accountIdx == ai {
					count++
				}
			}
			if count >= target {
				continue
			}
			if topicIdx >= len(topics) {
				break
			}
			assignedList = append(assignedList, assigned{accountIdx: ai, topic: topics[topicIdx]})
			topicIdx++
			assignedAny = true
		}
		if !assignedAny {
			break
		}
	}

	// 5. 落库计划项（按周内排期：每周二）
	var inserted []map[string]any
	base := time.Now()
	if d, err := time.Parse("2006-01-02", startDate); err == nil {
		base = d
	}
	for i, x := range assignedList {
		a := accounts[x.accountIdx]
		date := base.AddDate(0, 0, (i%4)*7).Format("2006-01-02")
		id, err := s.store.insertPlanItemV2(ctx, tenantID, planPeriodID, a.ID, &x.topic.ID, &date, i)
		if err != nil {
			return nil, fmt.Errorf("insert plan item: %w", err)
		}
		inserted = append(inserted, map[string]any{
			"id": id, "account_id": a.ID, "account": a.Name, "topic_id": x.topic.ID,
			"topic": x.topic.Title, "goal": x.topic.Goal, "form": x.topic.Form, "planned_date": date,
		})
	}

	// 6. 生成计划说明
	summary := ""
	if len(inserted) > 0 && s.llmClient != nil {
		summary, err = s.generatePlanSummary(ctx, tenantID, accounts, inserted)
		if err != nil {
			summary = "" // 说明生成失败不阻塞做计划
		}
		if summary != "" {
			_ = s.store.updatePlanPeriodSummary(ctx, tenantID, planPeriodID, summary)
		}
	}

	return map[string]any{
		"plan_period_id": planPeriodID,
		"period":         period,
		"start_date":     startDate,
		"total_target":   totalTarget,
		"items":          inserted,
		"summary":        summary,
	}, nil
}

func (s *Service) generatePlanSummary(ctx context.Context, tenantID int64, accounts []ContentMediaAccount, items []map[string]any) (string, error) {
	var b strings.Builder
	for _, a := range accounts {
		target := effectiveMonthlyTarget(a)
		b.WriteString(fmt.Sprintf("账号[%s｜角色 %s] 目标 %d 篇：\n", a.Name, a.Role, target))
		for _, it := range items {
			if it["account_id"] == a.ID {
				b.WriteString(fmt.Sprintf("  - %s（目标 %s，形式 %s）\n", it["topic"], it["goal"], it["form"]))
			}
		}
	}

	// 反复问依据
	if clusters, err := s.store.listQuestionClusters(ctx, tenantID, 2); err == nil && len(clusters) > 0 {
		b.WriteString("\n患者反复问（依据）：\n")
		for _, c := range clusters {
			if c.Frequency >= 3 {
				b.WriteString(fmt.Sprintf("  - %s（被问 %d 次）\n", c.Topic, c.Frequency))
			}
		}
	}

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_plan", "content_topic", "chat"})
	if err != nil {
		return "", err
	}
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("plan_summary"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_plan_summary", planSummaryPrompt)},
			{Role: "user", Content: "本月计划：\n" + b.String()},
		},
	}
	applyModelParamsToLLMRequest(&req, selectedModel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

// ScheduleTopic 排选题：把选题放进计划的某账号某天。
func (s *Service) ScheduleTopic(ctx context.Context, tenantID, planPeriodID, accountID, topicID int64, plannedDate *string) (int64, error) {
	return s.store.insertPlanItemV2(ctx, tenantID, planPeriodID, accountID, &topicID, plannedDate, 0)
}

// loadPlan 加载已存在的计划（说明 + 排期项，带账号/选题名）。
func (s *Service) loadPlan(ctx context.Context, tenantID, planPeriodID int64, period, startDate string) (map[string]any, error) {
	p, err := s.store.getPlanPeriod(ctx, tenantID, planPeriodID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("plan period %d not found", planPeriodID)
	}
	items, err := s.store.listPlanItemsV2(ctx, tenantID, planPeriodID)
	if err != nil {
		return nil, err
	}
	accName := map[int64]string{}
	if accounts, err := s.store.listMediaAccounts(ctx, tenantID); err == nil {
		for _, a := range accounts {
			accName[a.ID] = a.Name
		}
	}
	topicTitle := map[int64]string{}
	topicGoal := map[int64]string{}
	if topics, err := s.store.listTopicsV2(ctx, tenantID, 500); err == nil {
		for _, t := range topics {
			topicTitle[t.ID] = t.Title
			topicGoal[t.ID] = t.Goal
		}
	}
	var itemList []map[string]any
	for _, it := range items {
		var tid int64
		if it.TopicID != nil {
			tid = *it.TopicID
		}
		itemList = append(itemList, map[string]any{
			"id": it.ID, "account_id": it.AccountID, "account": accName[it.AccountID],
			"topic_id": tid, "topic": topicTitle[tid], "goal": topicGoal[tid],
			"planned_date": it.PlannedDate, "status": it.Status,
		})
	}
	return map[string]any{
		"plan_period_id": planPeriodID, "period": period, "start_date": startDate,
		"summary": p.SummaryText, "existing": true, "items": itemList,
	}, nil
}

// GetPlan 取某个月的现有计划（有则返回，无则 nil）。
func (s *Service) GetPlan(ctx context.Context, tenantID int64, period, startDate string) (map[string]any, error) {
	if period == "" {
		period = "month"
	}
	if startDate == "" {
		startDate = time.Now().Format("2006-01-02")
	}
	id, exists, err := s.store.findPlanPeriod(ctx, tenantID, period, startDate)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	return s.loadPlan(ctx, tenantID, id, period, startDate)
}

// defaultMonthlyTarget 按平台兜底默认发布量（账号没填时用）。
func defaultMonthlyTarget(platform string) int {
	switch platform {
	case "wechat", "gongzhonghao":
		return 8
	case "xiaohongshu":
		return 8
	case "website":
		return 2
	default:
		return 4
	}
}

// effectiveMonthlyTarget 账号的每月发布量：填了就用，没填/0 用平台默认。
func effectiveMonthlyTarget(a ContentMediaAccount) int {
	if a.MonthlyTarget != nil && *a.MonthlyTarget > 0 {
		return *a.MonthlyTarget
	}
	return defaultMonthlyTarget(a.Platform)
}
