package patientops

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

const patientOpsPlanContentModel = "patient_ops_plan_content"

type planMemberContentInput struct {
	CustomerID        int64  `json:"customer_id"`
	Name              string `json:"name"`
	Age               int    `json:"age"`
	AgeKnown          bool   `json:"age_known"`
	Gender            string `json:"gender,omitempty"`
	Interest          string `json:"interest,omitempty"`
	Concern           string `json:"concern,omitempty"`
	ClinicalText      string `json:"clinical_text,omitempty"`
	Question          string `json:"question,omitempty"`
	LastVisitAt       string `json:"last_visit_at,omitempty"`
	LastActivityAt    string `json:"last_activity_at,omitempty"`
	Reason            string `json:"reason"`
	ProductName       string `json:"product_name,omitempty"`
	ProductApplicable string `json:"product_applicable,omitempty"`
	RiskMedical       string `json:"risk_medical,omitempty"`
	RiskCommercial    string `json:"risk_commercial,omitempty"`
}

type planMemberContentOutput struct {
	Members []struct {
		CustomerID     int64  `json:"customer_id"`
		Goal           string `json:"goal"`
		Action         string `json:"action"`
		Content        string `json:"content"`
		RiskMedical    string `json:"risk_medical"`
		RiskCommercial string `json:"risk_commercial"`
		ExpectedResult string `json:"expected_result"`
	} `json:"members"`
}

// enrichPlanCandidatesWithLLM 按客户生成话术、目标、风险和完成标准。
// 失败时保留规则模板内容，不让计划发布失败。单次最多处理 40 人，其余回退模板。
func (s *Service) enrichPlanCandidatesWithLLM(ctx context.Context, plan *Plan, candidates []PlanCandidate) []PlanCandidate {
	if s.pool == nil || s.llm == nil || len(candidates) == 0 {
		return candidates
	}
	var systemPrompt, userTemplate string
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(system_prompt,''), COALESCE(user_prompt_template,'')
		FROM recording_analysis_prompts
		WHERE code='patient_plan_member_v1' AND is_active=true
		ORDER BY id DESC LIMIT 1
	`).Scan(&systemPrompt, &userTemplate); err != nil {
		slog.Warn("plan member prompt not available", "error", err)
		return candidates
	}
	if strings.TrimSpace(systemPrompt) == "" || strings.TrimSpace(userTemplate) == "" {
		return candidates
	}
	config, err := s.loadModelConfig(ctx, plan.TenantID, patientOpsPlanContentModel)
	if err != nil {
		slog.Warn("plan member model config not available", "error", err)
		return candidates
	}

	products := planProductSummary(candidates)
	index := make(map[int64]int, len(candidates))
	for i := range candidates {
		index[candidates[i].CustomerID] = i
	}
	const batchSize = 8
	const maxEnrich = 40
	limit := len(candidates)
	if limit > maxEnrich {
		limit = maxEnrich
	}
	for start := 0; start < limit; start += batchSize {
		end := start + batchSize
		if end > limit {
			end = limit
		}
		batch := candidates[start:end]
		inputs := make([]planMemberContentInput, 0, len(batch))
		for _, cand := range batch {
			inputs = append(inputs, planMemberContentInput{
				CustomerID:        cand.CustomerID,
				Name:              cand.CustomerName,
				Age:               cand.Age,
				AgeKnown:          cand.Age > 0,
				Gender:            cand.Gender,
				Interest:          cand.Interest,
				Concern:           cand.Concern,
				ClinicalText:      cand.ClinicalText,
				LastVisitAt:       cand.LastVisitAt,
				LastActivityAt:    cand.LastActivityAt,
				Reason:            cand.Reason,
				ProductName:       cand.RecommendedProductName,
				ProductApplicable: cand.ProductApplicable,
				RiskMedical:       cand.RiskMedical,
				RiskCommercial:    cand.RiskCommercial,
			})
		}
		payload, _ := json.Marshal(inputs)
		user := strings.ReplaceAll(userTemplate, "{{plan_type}}", plan.PlanType)
		user = strings.ReplaceAll(user, "{{scenario}}", plan.Scenario)
		user = strings.ReplaceAll(user, "{{products}}", products)
		user = strings.ReplaceAll(user, "{{customers}}", string(payload))

		params := &llmgateway.Params{Temperature: .3, MaxTokens: 3000, TimeoutSeconds: 90, ResponseFormat: "json"}
		applyPatientParams(params, config.Params)
		llmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 110*time.Second)
		resp, callErr := s.llm.TextInference(llmCtx, llmgateway.TextInferenceRequest{
			TenantID:      plan.TenantID,
			CallerService: "lingce-api",
			CallerModule:  "patientops.plan_content",
			FunctionType:  config.FunctionType,
			Provider:      config.Provider,
			ModelCode:     config.ModelCode,
			Billing:       &llmgateway.BillingMetadata{BusinessDomain: "patient_operations", BusinessObjectType: "plan", BusinessObjectID: plan.ID, BillingSubject: patientOpsPlanContentModel},
			Messages:      []llmgateway.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: user}},
			Params:        params,
		})
		cancel()
		if callErr != nil {
			slog.Warn("plan member content generation failed, keep template", "plan_id", plan.ID, "error", callErr)
			continue
		}
		var parsed planMemberContentOutput
		if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Content)), &parsed); err != nil {
			slog.Warn("plan member content parse failed, keep template", "plan_id", plan.ID, "error", err)
			continue
		}
		for _, item := range parsed.Members {
			i, ok := index[item.CustomerID]
			if !ok {
				continue
			}
			if strings.TrimSpace(item.Goal) != "" {
				candidates[i].Goal = strings.TrimSpace(item.Goal)
			}
			if strings.TrimSpace(item.Action) != "" {
				candidates[i].Action = strings.TrimSpace(item.Action)
			}
			if strings.TrimSpace(item.Content) != "" {
				candidates[i].Content = strings.TrimSpace(item.Content)
			}
			if strings.TrimSpace(item.RiskMedical) != "" {
				candidates[i].RiskMedical = strings.TrimSpace(item.RiskMedical)
			}
			if strings.TrimSpace(item.RiskCommercial) != "" {
				candidates[i].RiskCommercial = strings.TrimSpace(item.RiskCommercial)
			}
			if strings.TrimSpace(item.ExpectedResult) != "" {
				candidates[i].ExpectedResult = strings.TrimSpace(item.ExpectedResult)
			}
		}
	}
	return candidates
}

func planProductSummary(candidates []PlanCandidate) string {
	seen := map[string]struct{}{}
	lines := make([]string, 0, 4)
	for _, cand := range candidates {
		name := strings.TrimSpace(cand.RecommendedProductName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		line := name
		if applicable := strings.TrimSpace(cand.ProductApplicable); applicable != "" {
			line += "（适用：" + applicable + "）"
		}
		if upgrade := strings.TrimSpace(cand.ProductUpgradeTo); upgrade != "" {
			line += "；升级：" + upgrade
		}
		if combine := strings.TrimSpace(cand.ProductCombineWith); combine != "" {
			line += "；联合：" + combine
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "无（状态推进）"
	}
	return strings.Join(lines, "\n")
}
