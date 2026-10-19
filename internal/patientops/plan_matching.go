package patientops

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// planFilter 是智能筛选的可调参数。名单判断规则内置，经理只调整少量阈值。
type planFilter struct {
	ContactWindowDays int
	FollowupDays      int
	DormantDays       int
}

func normalizePlanFilter(raw map[string]any) planFilter {
	f := planFilter{ContactWindowDays: 7, FollowupDays: 60, DormantDays: 90}
	read := func(key string, target *int) {
		v, ok := raw[key]
		if !ok {
			return
		}
		switch n := v.(type) {
		case int:
			*target = n
		case int64:
			*target = int(n)
		case float64:
			*target = int(n)
		case string:
			var parsed int
			if _, err := fmt.Sscanf(n, "%d", &parsed); err == nil {
				*target = parsed
			}
		}
	}
	read("contact_window_days", &f.ContactWindowDays)
	read("followup_days", &f.FollowupDays)
	read("dormant_days", &f.DormantDays)
	if f.ContactWindowDays < 0 {
		f.ContactWindowDays = 0
	}
	if f.FollowupDays < 1 {
		f.FollowupDays = 60
	}
	if f.DormantDays <= f.FollowupDays {
		f.DormantDays = f.FollowupDays + 30
	}
	return f
}

func filterConfigWithDefaults(raw map[string]any) map[string]any {
	f := normalizePlanFilter(raw)
	return map[string]any{
		"contact_window_days": f.ContactWindowDays,
		"followup_days":       f.FollowupDays,
		"dormant_days":        f.DormantDays,
	}
}

// loadPlanCustomerFacts 加载租户客户事实，供筛选与匹配使用。
func (s *Service) loadPlanCustomerFacts(ctx context.Context, plan *Plan) ([]planCustomerFact, error) {
	extra := ` AND EXISTS (SELECT 1 FROM patient_conversation_sources s2 WHERE s2.tenant_id=c.tenant_id AND s2.customer_id=c.id)`
	switch {
	case plan.PlanType == "sales":
		extra = ` AND (EXISTS (SELECT 1 FROM patient_conversation_sources s2 WHERE s2.tenant_id=c.tenant_id AND s2.customer_id=c.id) OR EXISTS (SELECT 1 FROM external_data_source_identities i2 WHERE i2.tenant_id=c.tenant_id AND i2.customer_id=c.id))`
	case plan.Scenario == "post_visit" || plan.Scenario == "followup_due" || plan.Scenario == "dormant_wake":
		extra = ` AND EXISTS (SELECT 1 FROM encounters e2 WHERE e2.tenant_id=c.tenant_id AND e2.patient_id=c.id)`
	}
	query := fmt.Sprintf(`
		WITH external_visit AS (
		  SELECT DISTINCT ON (i.customer_id)
		         i.customer_id,
		         COALESCE(d.payload->'data'->>'diagnosis','') AS diagnosis,
		         COALESCE(d.payload->'data'->>'chief_complaint','') AS chief_complaint,
		         COALESCE(d.payload->'data'->>'department','') AS department
		  FROM external_data_source_identities i
		  JOIN external_data_deliveries d
		    ON d.tenant_id=i.tenant_id
		   AND d.source_system=i.source_system
		   AND d.object_type='visits'
		   AND d.payload->'patient'->>'source_patient_id'=i.source_patient_id
		  WHERE i.tenant_id=$1
		  ORDER BY i.customer_id, d.source_updated_at DESC NULLS LAST, d.created_at DESC
		)
		SELECT c.id, COALESCE(c.name,''), COALESCE(c.phone,''), COALESCE(c.gender,''),
		       COALESCE(NULLIF(c.age,0), EXTRACT(YEAR FROM AGE(CURRENT_DATE, c.birth_date))::int, 0),
		       COALESCE(c.tags::text,'[]'), COALESCE(c.status,''), COALESCE(c.source,''), COALESCE(c.lifecycle_stage,''),
		       c.assigned_to,
		       EXISTS (SELECT 1 FROM customer_identities ci WHERE ci.customer_id=c.id) AS has_identity,
		       COALESCE(t.open_tasks,0)::int,
		       COALESCE(to_char(e.last_visit_at,'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM'),''), COALESCE(e.last_visit_type,''), COALESCE(e.visit_count,0)::int,
		       COALESCE(to_char(c.last_contacted_at,'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM'),''),
		       COALESCE(to_char(s.last_source_at,'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM'),''), COALESCE(s.platform,''), COALESCE(s.nickname,''),
		       COALESCE(s.interest,''), COALESCE(s.concern,''), COALESCE(s.question,''), COALESCE(s.decision_maker,''),
		       COALESCE(xv.diagnosis,''), COALESCE(xv.chief_complaint,''), COALESCE(xv.department,''),
		       EXISTS (
		         SELECT 1 FROM patient_ops_plan_members m2
		         JOIN patient_ops_plans p2 ON p2.id=m2.plan_id AND p2.tenant_id=m2.tenant_id
		         WHERE m2.tenant_id=c.tenant_id AND m2.customer_id=c.id
		           AND p2.status='published' AND m2.status IN ('selected','published') AND p2.id<>$2
		       ) AS in_other_plan
		FROM customers c
		LEFT JOIN LATERAL (
		  SELECT MAX(started_at) AS last_visit_at, COUNT(*) AS visit_count,
		         COALESCE((ARRAY_AGG(visit_type ORDER BY started_at DESC))[1],'') AS last_visit_type
		  FROM encounters WHERE tenant_id=c.tenant_id AND patient_id=c.id
		) e ON TRUE
		LEFT JOIN LATERAL (
		  SELECT COUNT(*) FILTER (WHERE status NOT IN ('已完成','已取消')) AS open_tasks
		  FROM patient_operation_tasks WHERE tenant_id=c.tenant_id AND customer_id=c.id
		) t ON TRUE
		LEFT JOIN LATERAL (
		  SELECT captured_at AS last_source_at,
		         COALESCE(recognition_payload->>'platform','') AS platform,
		         COALESCE(recognition_payload->>'nickname','') AS nickname,
		         COALESCE(recognition_payload->>'product_interest','') AS interest,
		         COALESCE(recognition_payload->>'concern','') AS concern,
		         COALESCE(recognition_payload->>'patient_question','') AS question,
		         COALESCE(recognition_payload->>'decision_maker','') AS decision_maker
		  FROM patient_conversation_sources
		  WHERE tenant_id=c.tenant_id AND customer_id=c.id
		  ORDER BY captured_at DESC, id DESC LIMIT 1
		) s ON TRUE
		LEFT JOIN external_visit xv ON xv.customer_id=c.id
		WHERE c.tenant_id=$1 AND c.deleted_at IS NULL %s
		ORDER BY c.updated_at DESC NULLS LAST, c.id DESC
		LIMIT 50000
	`, extra)
	rows, err := s.pool.Query(ctx, query, plan.TenantID, plan.ID)
	if err != nil {
		return nil, fmt.Errorf("load plan customer facts: %w", err)
	}
	defer rows.Close()

	items := make([]planCustomerFact, 0, 256)
	for rows.Next() {
		var f planCustomerFact
		if err := rows.Scan(
			&f.CustomerID, &f.Name, &f.Phone, &f.Gender, &f.Age,
			&f.Tags, &f.Status, &f.Source, &f.LifecycleStage,
			&f.AssignedTo, &f.HasIdentity, &f.OpenTasks,
			&f.LastVisitAt, &f.LastVisitType, &f.VisitCount,
			&f.LastContactedAt,
			&f.LastSourceAt, &f.Platform, &f.Nickname,
			&f.Interest, &f.Concern, &f.Question, &f.DecisionMaker,
			&f.Diagnosis, &f.ChiefComplaint, &f.Department,
			&f.InOtherPlan,
		); err != nil {
			return nil, fmt.Errorf("scan plan customer fact: %w", err)
		}
		f.ClinicalText = strings.TrimSpace(f.Diagnosis + " " + f.ChiefComplaint)
		items = append(items, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plan customer facts: %w", err)
	}
	return items, nil
}

func (s *Service) loadPlanProducts(ctx context.Context, tenantID int64, productIDs []int64) ([]planProduct, error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, aliases, price, COALESCE(applicable,''), COALESCE(selling_point,''),
		       COALESCE(upgrade_to,''), COALESCE(combine_with,''), applicable_keywords,
		       age_min, age_max, gender, COALESCE(contraindications,'')
		FROM products
		WHERE tenant_id=$1 AND id = ANY($2) AND status='active' AND is_template=false AND review_status='approved'
		ORDER BY id
	`, tenantID, productIDs)
	if err != nil {
		return nil, fmt.Errorf("load plan products: %w", err)
	}
	defer rows.Close()

	items := make([]planProduct, 0, len(productIDs))
	for rows.Next() {
		var p planProduct
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Aliases, &p.Price, &p.Applicable, &p.SellingPoint,
			&p.UpgradeTo, &p.CombineWith, &p.ApplicableKeywords,
			&p.AgeMin, &p.AgeMax, &p.Gender, &p.Contraindications,
		); err != nil {
			return nil, fmt.Errorf("scan plan product: %w", err)
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plan products: %w", err)
	}
	return items, nil
}

func parsePlanTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return &parsed
	}
	if parsed, err := time.Parse("2006-01-02T15:04:05-07", value); err == nil {
		return &parsed
	}
	if parsed, err := time.Parse("2006-01-02T15:04:05", value); err == nil {
		return &parsed
	}
	return nil
}

func planDaysSince(value string, now time.Time) (int, bool) {
	parsed := parsePlanTime(value)
	if parsed == nil {
		return 0, false
	}
	return int(now.Sub(*parsed).Hours() / 24), true
}

func normalizePlanGender(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "男", "m", "male":
		return "male"
	case "女", "f", "female":
		return "female"
	}
	return ""
}

func planHasContact(f planCustomerFact) bool {
	return strings.TrimSpace(f.Phone) != "" || f.HasIdentity
}

// planScenarioFor 判断客户当前落在哪个状态推进场景；返回空字符串表示没有可用场景。
func planScenarioFor(f planCustomerFact, filter planFilter, now time.Time) (string, string) {
	visitDays, hasVisit := planDaysSince(f.LastVisitAt, now)
	if hasVisit {
		switch {
		case visitDays <= 30:
			return "post_visit", ""
		case visitDays >= filter.FollowupDays && visitDays <= 180:
			return "followup_due", ""
		case visitDays > 180:
			return "dormant_wake", ""
		default:
			return "", fmt.Sprintf("最近就诊 %d 天，未到复诊节点", visitDays)
		}
	}
	if planHasContact(f) {
		if strings.TrimSpace(f.LastSourceAt) == "" {
			return "", "没有沟通来源，未进入本次经营范围"
		}
		if !planHasIntent(f) {
			return "", "沟通内容没有明确医疗需求"
		}
		activityDays, hasActivity := planDaysSince(planLastActivity(f), now)
		if hasActivity && activityDays > filter.DormantDays {
			return "dormant_wake", ""
		}
		return "invite_visit", ""
	}
	if strings.TrimSpace(f.LastSourceAt) != "" {
		return "new_lead", ""
	}
	return "", "没有有效联系方式，也没有沟通来源"
}

func planLastActivity(f planCustomerFact) string {
	latest := ""
	for _, value := range []string{f.LastContactedAt, f.LastSourceAt, f.LastVisitAt} {
		parsed := parsePlanTime(value)
		if parsed == nil {
			continue
		}
		current := parsePlanTime(latest)
		if current == nil || parsed.After(*current) {
			latest = value
		}
	}
	return latest
}

// planJoinList 把识别结果里的 JSON 数组文本转成可读字符串。
func planJoinList(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "[]" || value == "null" {
		return ""
	}
	var list []string
	if json.Unmarshal([]byte(value), &list) == nil {
		cleaned := make([]string, 0, len(list))
		for _, item := range list {
			if item = strings.TrimSpace(item); item != "" {
				cleaned = append(cleaned, item)
			}
		}
		return strings.Join(cleaned, "、")
	}
	return value
}

func planHasIntent(f planCustomerFact) bool {
	if planJoinList(f.Interest) != "" {
		return true
	}
	question := strings.TrimSpace(f.Question)
	if question != "" && question != "待确认" {
		return true
	}
	concern := strings.TrimSpace(f.Concern)
	return concern != "" && concern != "待确认"
}

func planExclusionReason(f planCustomerFact, filter planFilter, now time.Time, requireContact bool) string {
	if f.OpenTasks > 0 {
		return "已有未完成任务"
	}
	if f.InOtherPlan {
		return "已在其他进行中计划"
	}
	status := strings.ToLower(strings.TrimSpace(f.Status))
	if status == "lost" || strings.Contains(f.Tags, "暂停联系") || strings.Contains(f.Tags, "明确拒绝") {
		return "已明确拒绝或暂停联系"
	}
	if lastContacted, ok := planDaysSince(f.LastContactedAt, now); ok && lastContacted < filter.ContactWindowDays {
		return fmt.Sprintf("近 %d 天已触达", filter.ContactWindowDays)
	}
	if requireContact && !planHasContact(f) {
		return "缺少有效联系方式"
	}
	return ""
}

// matchPlanProduct 在所选产品中为单个客户找最合适的一个。没有命中返回 nil。
func matchPlanProduct(f planCustomerFact, products []planProduct) (*planProduct, int, string, string) {
	text := strings.ToLower(strings.Join([]string{
		f.Interest, f.Concern, f.Question, f.Tags, f.Source, f.Platform, f.Nickname, f.ClinicalText,
	}, " "))
	if strings.TrimSpace(text) == "" {
		return nil, 0, "", ""
	}

	var best *planProduct
	bestScore := 0
	bestReason := ""
	bestRisk := ""
	for i := range products {
		p := &products[i]
		if p.Gender == "male" || p.Gender == "female" {
			customerGender := normalizePlanGender(f.Gender)
			if customerGender != "" && customerGender != p.Gender {
				continue
			}
		}
		if (p.AgeMin != nil || p.AgeMax != nil) && f.Age <= 0 {
			// 产品有明确年龄范围但客户年龄未知时，不自动推荐，交由人工确认。
			continue
		}
		ageOK := true
		if f.Age > 0 {
			if p.AgeMin != nil && f.Age < *p.AgeMin {
				ageOK = false
			}
			if p.AgeMax != nil && f.Age > *p.AgeMax {
				ageOK = false
			}
		}
		if !ageOK {
			continue
		}

		reasons := []string{}
		score := 50
		hit := false
		if strings.TrimSpace(p.Name) != "" && strings.Contains(text, strings.ToLower(p.Name)) {
			hit = true
			reasons = append(reasons, "沟通内容提到"+p.Name)
			score += 10
		}
		for _, alias := range p.Aliases {
			alias = strings.TrimSpace(alias)
			if alias != "" && strings.Contains(text, strings.ToLower(alias)) {
				hit = true
				reasons = append(reasons, "沟通内容提到"+alias)
				score += 6
				break
			}
		}
		hits := 0
		for _, keyword := range p.ApplicableKeywords {
			keyword = strings.TrimSpace(keyword)
			if keyword == "" {
				continue
			}
			if strings.Contains(text, strings.ToLower(keyword)) {
				hit = true
				hits++
				if hits <= 3 {
					reasons = append(reasons, "命中「"+keyword+"」")
					score += 12
				}
			}
		}
		if !hit {
			continue
		}
		if f.Age > 0 && (p.AgeMin != nil || p.AgeMax != nil) {
			score += 8
			reasons = append(reasons, fmt.Sprintf("年龄 %d 在适用范围内", f.Age))
		}
		if f.Interest != "" {
			score += 6
		}
		if score > 99 {
			score = 99
		}
		if score > bestScore {
			best = p
			bestScore = score
			bestReason = strings.Join(reasons, "；")
			if bestReason == "" {
				bestReason = "客户事实与产品适用条件匹配"
			}
			clinical := strings.TrimSpace(f.Diagnosis)
			if clinical == "" {
				clinical = strings.TrimSpace(f.ChiefComplaint)
			}
			if clinical == "" {
				clinical = strings.TrimSpace(f.ClinicalText)
			}
			if clinical != "" {
				bestReason = "就诊：" + truncatePlanText(clinical, 30) + "；" + bestReason
			}
			bestRisk = planRiskText(f, p)
		}
	}
	return best, bestScore, bestReason, bestRisk
}

func planRiskText(f planCustomerFact, p *planProduct) string {
	medical := "需医生评估适用条件（待确认）"
	if strings.TrimSpace(p.Contraindications) != "" {
		medical = "需医生评估；产品禁忌：" + strings.TrimSpace(p.Contraindications) + "（待确认）"
	}
	commercial := "常规跟进，避免过度承诺"
	concern := f.Concern + f.Question + f.Interest
	if strings.Contains(concern, "价格") || strings.Contains(concern, "贵") || strings.Contains(concern, "费用") {
		commercial = "价格顾虑，先解释价值与方案差异，不直接报价对比"
	}
	if strings.Contains(concern, "家属") || strings.Contains(concern, "家人") || f.DecisionMaker != "" {
		commercial = "需家属共同决策，沟通时准备可共同理解的材料"
	}
	return medical + "\n" + commercial
}

// buildPlanCandidates 执行内置排除与场景/产品匹配，返回预览候选。
func (s *Service) buildPlanCandidates(ctx context.Context, plan *Plan) (*PlanPreview, error) {
	now := time.Now()
	filter := normalizePlanFilter(plan.FilterConfig)
	facts, err := s.loadPlanCustomerFacts(ctx, plan)
	if err != nil {
		return nil, err
	}
	var products []planProduct
	if plan.PlanType == "sales" {
		products, err = s.loadPlanProducts(ctx, plan.TenantID, plan.ProductIDs)
		if err != nil {
			return nil, err
		}
	}
	preview := &PlanPreview{PlanID: plan.ID, PlanType: plan.PlanType, Scenario: plan.Scenario}
	for _, f := range facts {
		if reason := planExclusionReason(f, filter, now, plan.PlanType == "sales"); reason != "" {
			if len(preview.Excluded) < 200 {
				preview.Excluded = append(preview.Excluded, PlanExcluded{CustomerID: f.CustomerID, CustomerName: f.Name, Reason: reason})
			}
			preview.ExcludedCount++
			continue
		}
		scenario := plan.Scenario
		if plan.PlanType == "status" {
			currentScenario, reason := planScenarioFor(f, filter, now)
			if reason != "" || currentScenario != plan.Scenario {
				if len(preview.Excluded) < 200 {
					preview.Excluded = append(preview.Excluded, PlanExcluded{CustomerID: f.CustomerID, CustomerName: f.Name, Reason: reasonOr(reason, "不符合本次场景")})
				}
				preview.ExcludedCount++
				continue
			}
			scenario = currentScenario
		}

		candidate := PlanCandidate{
			CustomerID:     f.CustomerID,
			CustomerName:   f.Name,
			Phone:          f.Phone,
			Platform:       f.Platform,
			Nickname:       f.Nickname,
			Age:            f.Age,
			Gender:         f.Gender,
			Scenario:       scenario,
			LastVisitAt:    f.LastVisitAt,
			LastVisitType:  f.LastVisitType,
			LastActivityAt: planLastActivity(f),
			Interest:       planJoinList(f.Interest),
			Concern:        strings.TrimSpace(f.Concern),
			ClinicalText:   f.ClinicalText,
			ReasonSource:   "fact",
			AssignedTo:     f.AssignedTo,
		}
		if plan.PlanType == "sales" {
			product, score, reason, risk := matchPlanProduct(f, products)
			if product == nil {
				reason := "暂无适配套餐（P99）"
				if f.Age <= 0 && planProductsRequireAge(products) {
					reason = "年龄未知，需人工确认适用产品"
				}
				if len(preview.Excluded) < 200 {
					preview.Excluded = append(preview.Excluded, PlanExcluded{CustomerID: f.CustomerID, CustomerName: f.Name, Reason: reason})
				}
				preview.ExcludedCount++
				continue
			}
			productID := product.ID
			candidate.RecommendedProductID = &productID
			candidate.RecommendedProductName = product.Name
			candidate.ProductApplicable = product.Applicable
			candidate.ProductUpgradeTo = product.UpgradeTo
			candidate.ProductCombineWith = product.CombineWith
			candidate.MatchScore = &score
			candidate.Reason = reason
			candidate.ReasonSource = planReasonSource(f)
			candidate.RiskMedical = medicalPart(risk)
			candidate.RiskCommercial = commercialPart(risk)
			candidate.ExpectedResult = "确认意向并推进到店/成交"
			candidate.TaskType = "活动触达"
			candidate.TaskSubtype = "产品推荐"
		} else {
			reason, source := planStatusReason(f, scenario, plan.FilterConfig)
			candidate.Reason = reason
			candidate.ReasonSource = source
			candidate.RiskMedical = "按患者实际情况由医生判断（待确认）"
			candidate.RiskCommercial = "按患者当前阶段沟通，避免重复触达"
			candidate.TaskType, candidate.TaskSubtype = planTaskType(scenario)
			candidate.ExpectedResult = planExpectedResult(scenario)
		}
		preview.Candidates = append(preview.Candidates, candidate)
	}
	preview.EligibleCount = len(preview.Candidates)
	sortPlanCandidates(preview.Candidates, plan.PlanType, plan.Scenario)
	if len(preview.Candidates) > 200 {
		preview.Candidates = preview.Candidates[:200]
	}
	if plan.PlanType == "sales" {
		preview.Coverage = planCoverage(preview.EligibleCount, products, plan.TargetAmount)
	}
	return preview, nil
}

func reasonOr(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func planReasonSource(f planCustomerFact) string {
	if f.LastVisitAt != "" || f.Age > 0 {
		return "fact"
	}
	if f.Interest != "" || f.Concern != "" || f.Question != "" {
		return "insight"
	}
	return "fact"
}

func planStatusReason(f planCustomerFact, scenario string, rawFilter map[string]any) (string, string) {
	now := time.Now()
	source := "fact"
	switch scenario {
	case "new_lead":
		return "截图/渠道新线索，尚无有效联系方式或尚未首次响应", "insight"
	case "invite_visit":
		if f.LastSourceAt != "" {
			source = "insight"
		}
		if interest := planJoinList(f.Interest); interest != "" {
			return fmt.Sprintf("已有联系方式，尚无就诊记录；沟通中关注%s，最近沟通 %s", truncatePlanText(interest, 40), planDateText(f.LastSourceAt)), source
		}
		return "已有联系方式，尚无就诊记录，可邀约首次到诊", "fact"
	case "post_visit":
		return fmt.Sprintf("最近就诊 %s（%s），尚无下一步安排", planDateText(f.LastVisitAt), visitTypeText(f.LastVisitType)), "fact"
	case "followup_due":
		days, _ := planDaysSince(f.LastVisitAt, now)
		return fmt.Sprintf("最近就诊 %s（%s），已 %d 天未复诊", planDateText(f.LastVisitAt), visitTypeText(f.LastVisitType), days), "fact"
	case "dormant_wake":
		activity := planLastActivity(f)
		days, _ := planDaysSince(activity, now)
		return fmt.Sprintf("最近有效互动 %s，已 %d 天未联系", planDateText(activity), days), "fact"
	}
	return "符合本次经营场景", "fact"
}

func planTaskType(scenario string) (string, string) {
	switch scenario {
	case "new_lead":
		return "接住新咨询", "新线索首次响应"
	case "invite_visit":
		return "咨询推进", "邀约到诊"
	case "post_visit":
		return "到院后促成", "到诊后跟进"
	case "followup_due":
		return "咨询推进", "复诊提醒"
	case "dormant_wake":
		return "咨询推进", "沉睡唤醒"
	}
	return "咨询推进", ""
}

func planExpectedResult(scenario string) string {
	switch scenario {
	case "new_lead":
		return "拿到有效联系方式或明确拒绝"
	case "invite_visit":
		return "约定到诊时间或记录暂不考虑"
	case "post_visit":
		return "明确下一步方案或记录真实卡点"
	case "followup_due":
		return "确认复诊时间或记录暂不复查"
	case "dormant_wake":
		return "确认是否仍有需求或暂停联系"
	}
	return "记录真实结果与下一步"
}

func planCoverage(matched int, products []planProduct, target *float64) *PlanCoverage {
	if matched <= 0 || len(products) == 0 {
		return nil
	}
	minPrice, maxPrice := 0.0, 0.0
	for _, p := range products {
		if p.Price <= 0 {
			continue
		}
		if minPrice == 0 || p.Price < minPrice {
			minPrice = p.Price
		}
		if p.Price > maxPrice {
			maxPrice = p.Price
		}
	}
	if minPrice == 0 {
		return nil
	}
	coverage := &PlanCoverage{MatchedCount: matched, MinAmount: float64(matched) * minPrice, MaxAmount: float64(matched) * maxPrice}
	if target != nil && *target > 0 {
		coverage.TargetAmount = *target
		coverage.MinCoverage = coverage.MinAmount / *target * 100
		coverage.MaxCoverage = coverage.MaxAmount / *target * 100
	}
	return coverage
}

func sortPlanCandidates(items []PlanCandidate, planType, scenario string) {
	sort.SliceStable(items, func(i, j int) bool {
		if planType == "sales" {
			si, sj := 0, 0
			if items[i].MatchScore != nil {
				si = *items[i].MatchScore
			}
			if items[j].MatchScore != nil {
				sj = *items[j].MatchScore
			}
			return si > sj
		}
		ti := parsePlanTime(items[i].LastActivityAt)
		tj := parsePlanTime(items[j].LastActivityAt)
		if ti == nil || tj == nil {
			return items[i].CustomerID < items[j].CustomerID
		}
		if scenario == "followup_due" || scenario == "dormant_wake" {
			return ti.Before(*tj)
		}
		return ti.After(*tj)
	})
}

func truncatePlanText(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "…"
}

func planDateText(value string) string {
	parsed := parsePlanTime(value)
	if parsed == nil {
		return "未知"
	}
	return parsed.Format("2006-01-02")
}

func visitTypeText(value string) string {
	switch strings.TrimSpace(value) {
	case "first_visit":
		return "初诊"
	case "return_visit":
		return "复诊"
	case "consultation":
		return "咨询"
	case "follow_up":
		return "随访"
	}
	return "就诊"
}

func medicalPart(risk string) string {
	parts := strings.SplitN(risk, "\n", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func commercialPart(risk string) string {
	parts := strings.SplitN(risk, "\n", 2)
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}

func planProductsRequireAge(products []planProduct) bool {
	if len(products) == 0 {
		return false
	}
	for _, p := range products {
		if p.AgeMin == nil && p.AgeMax == nil {
			return false
		}
	}
	return true
}
