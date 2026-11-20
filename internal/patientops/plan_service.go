package patientops

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/customercontact"
	"github.com/jackc/pgx/v5"
)

const planSelectColumns = `
	p.id, p.tenant_id, p.name, p.plan_type, p.scenario,
	COALESCE(to_char(p.period_start,'YYYY-MM-DD'),''),
	COALESCE(to_char(p.period_end,'YYYY-MM-DD'),''),
	p.target_count, p.target_amount, p.product_ids, p.filter_config, p.status, p.created_by,
	COALESCE(to_char(p.published_at,'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM'),''),
	to_char(p.created_at,'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM'),
	to_char(p.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.USTZH:TZM'),
	(SELECT COUNT(*) FROM patient_ops_plan_members m WHERE m.tenant_id=p.tenant_id AND m.plan_id=p.id AND m.status<>'removed'),
	(SELECT COUNT(*) FROM patient_ops_plan_members m WHERE m.tenant_id=p.tenant_id AND m.plan_id=p.id AND m.status='completed'),
	COALESCE((SELECT array_agg(pr.name ORDER BY pr.id) FROM products pr WHERE pr.id IN (SELECT jsonb_array_elements_text(p.product_ids)::bigint) AND pr.deleted_at IS NULL), ARRAY[]::text[])
`

func scanPlanRow(row interface {
	Scan(dest ...any) error
}) (*Plan, error) {
	var p Plan
	var targetCount sql.NullInt64
	var targetAmount sql.NullFloat64
	var productIDs []byte
	var filterConfig []byte
	var createdBy sql.NullInt64
	var productNames []string
	if err := row.Scan(
		&p.ID, &p.TenantID, &p.Name, &p.PlanType, &p.Scenario,
		&p.PeriodStart, &p.PeriodEnd,
		&targetCount, &targetAmount, &productIDs, &filterConfig, &p.Status, &createdBy,
		&p.PublishedAt, &p.CreatedAt, &p.UpdatedAt,
		&p.MemberCount, &p.CompletedCount, &productNames,
	); err != nil {
		return nil, err
	}
	if targetCount.Valid {
		value := int(targetCount.Int64)
		p.TargetCount = &value
	}
	if targetAmount.Valid {
		value := targetAmount.Float64
		p.TargetAmount = &value
	}
	if createdBy.Valid {
		value := createdBy.Int64
		p.CreatedBy = &value
	}
	p.ProductIDs = []int64{}
	if len(productIDs) > 0 {
		_ = json.Unmarshal(productIDs, &p.ProductIDs)
	}
	p.FilterConfig = map[string]any{}
	if len(filterConfig) > 0 {
		_ = json.Unmarshal(filterConfig, &p.FilterConfig)
	}
	p.ProductNames = productNames
	return &p, nil
}

func (s *Service) ListPlans(ctx context.Context, tenantID int64, page, pageSize int, status string) ([]Plan, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	conditions := []string{"p.tenant_id=$1", "p.deleted_at IS NULL"}
	args := []any{tenantID}
	argIdx := 2
	if strings.TrimSpace(status) != "" {
		conditions = append(conditions, fmt.Sprintf("p.status=$%d", argIdx))
		args = append(args, strings.TrimSpace(status))
		argIdx++
	}
	where := strings.Join(conditions, " AND ")
	var total int64
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM patient_ops_plans p WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count plans: %w", err)
	}
	query := fmt.Sprintf("SELECT %s FROM patient_ops_plans p WHERE %s ORDER BY p.created_at DESC,p.id DESC LIMIT $%d OFFSET $%d", planSelectColumns, where, argIdx, argIdx+1)
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()
	items := make([]Plan, 0, pageSize)
	for rows.Next() {
		item, err := scanPlanRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan plan: %w", err)
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate plans: %w", err)
	}
	return items, total, nil
}

func (s *Service) loadPlan(ctx context.Context, tenantID, planID int64) (*Plan, error) {
	query := fmt.Sprintf("SELECT %s FROM patient_ops_plans p WHERE p.tenant_id=$1 AND p.id=$2 AND p.deleted_at IS NULL", planSelectColumns)
	plan, err := scanPlanRow(s.pool.QueryRow(ctx, query, tenantID, planID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("plan not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load plan: %w", err)
	}
	return plan, nil
}

func (s *Service) GetPlan(ctx context.Context, tenantID, planID int64) (*PlanDetail, error) {
	plan, err := s.loadPlan(ctx, tenantID, planID)
	if err != nil {
		return nil, err
	}
	members, err := s.loadPlanMembers(ctx, tenantID, planID)
	if err != nil {
		return nil, err
	}
	stats, err := s.loadPlanStats(ctx, tenantID, planID)
	if err != nil {
		return nil, err
	}
	return &PlanDetail{Plan: plan, Members: members, Stats: stats}, nil
}

func (s *Service) loadPlanMembers(ctx context.Context, tenantID, planID int64) ([]PlanMember, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.plan_id, m.customer_id, COALESCE(c.name,''), COALESCE(c.phone,''),
		       m.recommended_product_id, COALESCE(p.name,''), m.match_score,
		       m.reason, m.reason_source, m.risk_medical, m.risk_commercial, m.task_id, m.status,
		       to_char(m.created_at,'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM')
		FROM patient_ops_plan_members m
		JOIN customers c ON c.id=m.customer_id AND c.tenant_id=m.tenant_id
		LEFT JOIN products p ON p.id=m.recommended_product_id
		WHERE m.tenant_id=$1 AND m.plan_id=$2
		ORDER BY m.id
	`, tenantID, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan members: %w", err)
	}
	defer rows.Close()
	items := make([]PlanMember, 0, 32)
	for rows.Next() {
		var m PlanMember
		var productID sql.NullInt64
		var score sql.NullInt64
		var taskID sql.NullInt64
		if err := rows.Scan(
			&m.ID, &m.PlanID, &m.CustomerID, &m.CustomerName, &m.Phone,
			&productID, &m.RecommendedProductName, &score,
			&m.Reason, &m.ReasonSource, &m.RiskMedical, &m.RiskCommercial, &taskID, &m.Status,
			&m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan plan member: %w", err)
		}
		if productID.Valid {
			value := productID.Int64
			m.RecommendedProductID = &value
		}
		if score.Valid {
			value := int(score.Int64)
			m.MatchScore = &value
		}
		if taskID.Valid {
			value := taskID.Int64
			m.TaskID = &value
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plan members: %w", err)
	}
	rows.Close()
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.CustomerID)
	}
	contacts, err := customercontact.Load(ctx, s.pool, tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("load plan member contacts: %w", err)
	}
	for i := range items {
		items[i].Contacts = contacts[items[i].CustomerID]
	}
	return items, nil
}

func (s *Service) loadPlanStats(ctx context.Context, tenantID, planID int64) (PlanStats, error) {
	var stats PlanStats
	var dealAmount sql.NullFloat64
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE status='已完成'),
		       COUNT(*) FILTER (WHERE result IN ('已回复','患者有回应')),
		       COUNT(*) FILTER (WHERE result='已预约'),
		       COUNT(*) FILTER (WHERE result='已购买'),
		       COALESCE(SUM(result_amount) FILTER (WHERE amount_source<>''),0)::float8
		FROM patient_operation_tasks
		WHERE tenant_id=$1 AND plan_id=$2
	`, tenantID, planID).Scan(&stats.TaskTotal, &stats.TaskDone, &stats.TaskReplied, &stats.TaskBooked, &stats.TaskDeals, &dealAmount)
	if err != nil {
		return stats, fmt.Errorf("load plan stats: %w", err)
	}
	if dealAmount.Valid {
		stats.DealAmount = dealAmount.Float64
	}
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(t.result_product_id,0),t.product_id,0) AS pid,
		       COALESCE(MAX(p.name),''), COUNT(*),
		       COALESCE(SUM(t.result_amount),0)::float8
		FROM patient_operation_tasks t
		LEFT JOIN products p ON p.id=COALESCE(NULLIF(t.result_product_id,0),t.product_id)
		WHERE t.tenant_id=$1 AND t.plan_id=$2 AND t.status='已完成'
		  AND (t.product_id IS NOT NULL OR t.result_product_id IS NOT NULL)
		GROUP BY 1
		ORDER BY 2
	`, tenantID, planID)
	if err != nil {
		return stats, fmt.Errorf("load plan product stats: %w", err)
	}
	defer rows.Close()
	stats.ProductBreakdown = []PlanProductStat{}
	for rows.Next() {
		var item PlanProductStat
		var amount sql.NullFloat64
		if err := rows.Scan(&item.ProductID, &item.ProductName, &item.TaskCount, &amount); err != nil {
			return stats, fmt.Errorf("scan plan product stat: %w", err)
		}
		if amount.Valid {
			item.DealAmount = amount.Float64
		}
		stats.ProductBreakdown = append(stats.ProductBreakdown, item)
	}
	return stats, rows.Err()
}

func planJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}

func (s *Service) verifyPlanProducts(ctx context.Context, tenantID int64, productIDs []int64) error {
	if len(productIDs) == 0 {
		return fmt.Errorf("sales plan requires at least one product")
	}
	var count int
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM products
		WHERE tenant_id=$1 AND id=ANY($2) AND status='active' AND is_template=false AND review_status='approved' AND deleted_at IS NULL
	`, tenantID, productIDs).Scan(&count); err != nil {
		return fmt.Errorf("verify plan products: %w", err)
	}
	if count != len(productIDs) {
		return fmt.Errorf("selected products must be active, approved and belong to the tenant")
	}
	return nil
}

func validatePlan(plan *Plan) error {
	if strings.TrimSpace(plan.Name) == "" {
		return fmt.Errorf("plan name is required")
	}
	switch plan.PlanType {
	case "status":
		switch plan.Scenario {
		case "new_lead", "invite_visit", "post_visit", "followup_due", "dormant_wake":
		default:
			return fmt.Errorf("unsupported status plan scenario")
		}
		if len(plan.ProductIDs) > 0 {
			return fmt.Errorf("status plan must not select products")
		}
	case "sales":
		if len(plan.ProductIDs) == 0 {
			return fmt.Errorf("sales plan requires at least one product")
		}
		if plan.TargetAmount == nil || *plan.TargetAmount <= 0 {
			return fmt.Errorf("sales plan requires target_amount > 0")
		}
	default:
		return fmt.Errorf("unsupported plan type")
	}
	if plan.PeriodStart != "" {
		if _, err := time.Parse("2006-01-02", plan.PeriodStart); err != nil {
			return fmt.Errorf("invalid period_start")
		}
	}
	if plan.PeriodEnd != "" {
		if _, err := time.Parse("2006-01-02", plan.PeriodEnd); err != nil {
			return fmt.Errorf("invalid period_end")
		}
	}
	if plan.PeriodStart != "" && plan.PeriodEnd != "" && plan.PeriodStart > plan.PeriodEnd {
		return fmt.Errorf("period_start must be before period_end")
	}
	if plan.TargetCount != nil && *plan.TargetCount < 0 {
		return fmt.Errorf("target_count must be >= 0")
	}
	return nil
}

func (s *Service) CreatePlan(ctx context.Context, tenantID, employeeID int64, req CreatePlanRequest) (*PlanDetail, error) {
	plan := &Plan{
		TenantID:     tenantID,
		Name:         strings.TrimSpace(req.Name),
		PlanType:     strings.TrimSpace(req.PlanType),
		Scenario:     strings.TrimSpace(req.Scenario),
		PeriodStart:  strings.TrimSpace(req.PeriodStart),
		PeriodEnd:    strings.TrimSpace(req.PeriodEnd),
		TargetCount:  req.TargetCount,
		TargetAmount: req.TargetAmount,
		ProductIDs:   req.ProductIDs,
		FilterConfig: filterConfigWithDefaults(req.FilterConfig),
		Status:       "draft",
		CreatedBy:    &employeeID,
	}
	if plan.ProductIDs == nil {
		plan.ProductIDs = []int64{}
	}
	if err := validatePlan(plan); err != nil {
		return nil, err
	}
	if plan.PlanType == "sales" {
		if err := s.verifyPlanProducts(ctx, tenantID, plan.ProductIDs); err != nil {
			return nil, err
		}
	}
	var planID int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO patient_ops_plans(
			tenant_id,name,plan_type,scenario,period_start,period_end,target_count,target_amount,
			product_ids,filter_config,status,created_by
		) VALUES($1,$2,$3,$4,NULLIF($5,'')::date,NULLIF($6,'')::date,$7,$8,$9::jsonb,$10::jsonb,'draft',$11)
		RETURNING id
	`, tenantID, plan.Name, plan.PlanType, plan.Scenario, plan.PeriodStart, plan.PeriodEnd,
		plan.TargetCount, plan.TargetAmount, planJSON(plan.ProductIDs), planJSON(plan.FilterConfig), employeeID).Scan(&planID)
	if err != nil {
		return nil, fmt.Errorf("create plan: %w", err)
	}
	return s.GetPlan(ctx, tenantID, planID)
}

func (s *Service) UpdatePlan(ctx context.Context, tenantID, planID int64, req UpdatePlanRequest) (*PlanDetail, error) {
	plan, err := s.loadPlan(ctx, tenantID, planID)
	if err != nil {
		return nil, err
	}
	if plan.Status == "closed" {
		return nil, fmt.Errorf("closed plans cannot be edited")
	}
	if plan.Status == "published" {
		if req.PlanType != nil || req.Scenario != nil || req.ProductIDs != nil || req.FilterConfig != nil {
			return nil, fmt.Errorf("published plan only allows name, period and target changes")
		}
	}
	if req.Name != nil {
		plan.Name = strings.TrimSpace(*req.Name)
	}
	if req.PlanType != nil {
		plan.PlanType = strings.TrimSpace(*req.PlanType)
	}
	if req.Scenario != nil {
		plan.Scenario = strings.TrimSpace(*req.Scenario)
	}
	if req.PeriodStart != nil {
		plan.PeriodStart = strings.TrimSpace(*req.PeriodStart)
	}
	if req.PeriodEnd != nil {
		plan.PeriodEnd = strings.TrimSpace(*req.PeriodEnd)
	}
	if req.TargetCount != nil {
		plan.TargetCount = req.TargetCount
	}
	if req.TargetAmount != nil {
		plan.TargetAmount = req.TargetAmount
	}
	if req.ProductIDs != nil {
		plan.ProductIDs = *req.ProductIDs
	}
	if req.FilterConfig != nil {
		plan.FilterConfig = filterConfigWithDefaults(*req.FilterConfig)
	}
	if err := validatePlan(plan); err != nil {
		return nil, err
	}
	if plan.PlanType == "sales" {
		if err := s.verifyPlanProducts(ctx, tenantID, plan.ProductIDs); err != nil {
			return nil, err
		}
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE patient_ops_plans SET
			name=$2,plan_type=$3,scenario=$4,period_start=NULLIF($5,'')::date,period_end=NULLIF($6,'')::date,
			target_count=$7,target_amount=$8,product_ids=$9::jsonb,filter_config=$10::jsonb,updated_at=NOW()
		WHERE tenant_id=$1 AND id=$11 AND status IN ('draft','published') AND deleted_at IS NULL
	`, tenantID, plan.Name, plan.PlanType, plan.Scenario, plan.PeriodStart, plan.PeriodEnd,
		plan.TargetCount, plan.TargetAmount, planJSON(plan.ProductIDs), planJSON(plan.FilterConfig), planID)
	if err != nil {
		return nil, fmt.Errorf("update plan: %w", err)
	}
	return s.GetPlan(ctx, tenantID, planID)
}

func (s *Service) SoftDeletePlan(ctx context.Context, tenantID, planID, employeeID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete plan: %w", err)
	}
	defer tx.Rollback(ctx)
	var status string
	err = tx.QueryRow(ctx, `
		UPDATE patient_ops_plans
		SET deleted_at=NOW(), deleted_by=$3, updated_at=NOW()
		WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL
		RETURNING status
	`, tenantID, planID, employeeID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("plan not found or already deleted")
	}
	if err != nil {
		return fmt.Errorf("delete plan: %w", err)
	}
	// 软删除保留计划、名单和已完成任务；未完成任务取消，避免继续占用客户。
	if _, err := tx.Exec(ctx, `
		UPDATE patient_operation_tasks
		SET status='已取消', updated_at=NOW()
		WHERE tenant_id=$1 AND plan_id=$2 AND status NOT IN ('已完成','已取消')
	`, tenantID, planID); err != nil {
		return fmt.Errorf("cancel tasks of deleted plan: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete plan: %w", err)
	}
	return nil
}

func (s *Service) PreviewPlan(ctx context.Context, tenantID, planID int64) (*PlanPreview, error) {
	plan, err := s.loadPlan(ctx, tenantID, planID)
	if err != nil {
		return nil, err
	}
	return s.buildPlanCandidates(ctx, plan)
}

func (s *Service) insertPlanMember(ctx context.Context, tx pgx.Tx, plan *Plan, cand PlanCandidate) (int64, error) {
	cand = preservePlanAgeWarning(cand)
	var score any
	if cand.MatchScore != nil {
		score = *cand.MatchScore
	}
	var memberID int64
	err := tx.QueryRow(ctx, `
		INSERT INTO patient_ops_plan_members(
			plan_id,tenant_id,customer_id,recommended_product_id,match_score,reason,reason_source,
			risk_medical,risk_commercial,status
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'selected')
		ON CONFLICT (plan_id,customer_id) DO NOTHING
		RETURNING id
	`, plan.ID, plan.TenantID, cand.CustomerID, cand.RecommendedProductID, score, cand.Reason, cand.ReasonSource,
		cand.RiskMedical, cand.RiskCommercial).Scan(&memberID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx, `
			SELECT id FROM patient_ops_plan_members WHERE plan_id=$1 AND customer_id=$2
		`, plan.ID, cand.CustomerID).Scan(&memberID); err != nil {
			return 0, fmt.Errorf("load existing plan member: %w", err)
		}
		return memberID, nil
	}
	if err != nil {
		return 0, fmt.Errorf("insert plan member: %w", err)
	}
	return memberID, nil
}

func planTaskContent(plan *Plan, cand PlanCandidate, interest string) (goal, action, content, expected string) {
	name := cand.CustomerName
	if strings.TrimSpace(name) == "" {
		name = "您好"
	}
	interest = strings.TrimSpace(interest)
	switch plan.PlanType {
	case "sales":
		goal = "推动" + cand.RecommendedProductName + "成交"
		action = "联系客户，说明产品适用条件并记录真实结果"
		content = fmt.Sprintf("%s，您好。结合您之前咨询的%s，%s比较适合（%s）。", name, fallbackPlanText(interest, "相关需求"), cand.RecommendedProductName, fallbackPlanText(cand.ProductApplicable, "具体适用条件需医生评估"))
		if strings.TrimSpace(cand.Concern) != "" {
			content += "了解到您的顾虑：" + truncatePlanText(cand.Concern, 80) + "。"
		}
		if strings.TrimSpace(cand.ProductUpgradeTo) != "" {
			content += "如果预算或条件不同，也可以先了解升级方案：" + cand.ProductUpgradeTo + "。"
		}
		if strings.TrimSpace(cand.ProductCombineWith) != "" {
			content += "如有需要可一起评估联合方案：" + cand.ProductCombineWith + "。"
		}
		content += "方便的话我给您详细说明一下，看看是否适合您。"
		expected = "确认意向并推进到店/成交"
	default:
		switch plan.Scenario {
		case "new_lead":
			goal = "接住新线索并拿到有效联系方式"
			action = "首次响应客户，确认需求并留下电话或微信"
			content = fmt.Sprintf("%s，您好。看到您在%s咨询，想先确认一下您的具体需求，方便留个电话或微信吗？", name, fallbackPlanText(cand.Interest, "我们"))
			expected = "拿到有效联系方式或明确拒绝"
		case "invite_visit":
			goal = "邀约客户首次到诊并确认时间"
			action = "结合客户关注点邀约到诊，确认时间"
			content = fmt.Sprintf("%s，您好。您之前咨询的%s，建议先安排一次到诊评估，医生看过之后才能判断适合的方案。您这周或下周哪天方便？", name, fallbackPlanText(interest, "相关问题"))
			expected = "约定到诊时间或记录暂不考虑"
		case "post_visit":
			goal = "确认到诊后的下一步安排"
			action = "回访到诊体验，确认方案理解与下一步"
			content = fmt.Sprintf("%s，您好。上次到院后想跟进一下您的感受和下一步安排，%s您方便说说现在的想法吗？", name, concernHint(cand.Concern))
			expected = "明确下一步方案或记录真实卡点"
		case "followup_due":
			goal = "提醒复诊并确认时间"
			action = "联系客户，了解当前情况并提醒复诊"
			content = fmt.Sprintf("%s，您好。距离上次到诊已经一段时间了，想了解一下您现在的恢复/使用情况，需不需要帮您安排复诊？", name)
			expected = "确认复诊时间或记录暂不复查"
		case "dormant_wake":
			goal = "唤醒沉睡客户并确认是否仍有需求"
			action = "先问候近况，再确认是否仍有未解决的需求"
			content = fmt.Sprintf("%s，您好。很久没联系了，想问候一下您最近的情况，之前您关注的%s还需要继续了解吗？", name, fallbackPlanText(interest, "健康问题"))
			expected = "确认是否仍有需求或暂停联系"
		}
	}
	return
}

func fallbackPlanText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return truncatePlanText(value, 60)
}

func concernHint(concern string) string {
	concern = strings.TrimSpace(concern)
	if concern == "" {
		return ""
	}
	return "您之前提到的顾虑是「" + truncatePlanText(concern, 60) + "」，"
}

func (s *Service) insertPlanTask(ctx context.Context, tx pgx.Tx, plan *Plan, cand PlanCandidate) (int64, error) {
	cand = preservePlanAgeWarning(cand)
	taskType := cand.TaskType
	if strings.TrimSpace(taskType) == "" {
		taskType = "咨询推进"
	}
	owner := plan.CreatedBy
	if cand.AssignedTo != nil && *cand.AssignedTo > 0 {
		owner = cand.AssignedTo
	}
	if owner == nil || *owner <= 0 {
		return 0, fmt.Errorf("plan task requires an owner")
	}
	due := time.Now().AddDate(0, 0, 3)
	if plan.PlanType == "sales" {
		due = time.Now().AddDate(0, 0, 7)
	}
	if plan.PeriodEnd != "" {
		if end, err := time.ParseInLocation("2006-01-02", plan.PeriodEnd, time.FixedZone("Asia/Shanghai", 8*3600)); err == nil {
			end = end.Add(23*time.Hour + 59*time.Minute)
			if due.After(end) {
				due = end
			}
		}
	}
	goal, action, content, expected := planTaskContent(plan, cand, cand.Interest)
	if strings.TrimSpace(cand.Goal) != "" {
		goal = cand.Goal
	}
	if strings.TrimSpace(cand.Action) != "" {
		action = cand.Action
	}
	if strings.TrimSpace(cand.Content) != "" {
		content = cand.Content
	}
	if strings.TrimSpace(cand.ExpectedResult) != "" {
		expected = cand.ExpectedResult
	}
	if cand.AdmissionStatus == "needs_confirmation" {
		action = "先核实实际使用者的年龄和适用条件，再介绍产品；" + action
		content = fmt.Sprintf("%s，您好。关于您咨询的%s，方便先确认实际使用者的年龄吗？我们核对适用条件后，再向您说明产品和评估安排。", cand.CustomerName, fallbackPlanText(cand.Interest, "相关需求"))
	}
	evidence := cand.Reason
	if strings.TrimSpace(cand.ConditionSummary) != "" {
		summary := strings.TrimRight(strings.TrimSpace(cand.ConditionSummary), "。；;,.，、")
		if summary == "" {
			summary = strings.TrimSpace(cand.ConditionSummary)
		}
		evidence = "病情摘要：" + summary + "；匹配依据：" + cand.Reason
	}
	dedupeKey := fmt.Sprintf("plan:%d:customer:%d", plan.ID, cand.CustomerID)
	priority := 5
	if cand.MatchScore != nil {
		if *cand.MatchScore >= 80 {
			priority = 8
		} else if *cand.MatchScore >= 60 {
			priority = 6
		}
	}
	var matchScore any
	if cand.MatchScore != nil {
		matchScore = *cand.MatchScore
	}
	var taskID int64
	err := tx.QueryRow(ctx, `
		INSERT INTO patient_operation_tasks(
			tenant_id,customer_id,source_id,employee_id,task_type,goal,action,content,evidence,due_at,status,
			plan_id,plan_member_id,task_subtype,product_id,priority,match_score,risk_medical,risk_commercial,
			next_action,next_due_at,dedupe_key,expected_result,amount_source,created_by
		) VALUES($1,$2,NULL,$3,$4,$5,$6,$7,$8,$9,'待处理',
			$10,NULL,$11,$12,$13,$14,$15,$16,'',NULL,$17,$18,'',$19)
		ON CONFLICT DO NOTHING
		RETURNING id
	`, plan.TenantID, cand.CustomerID, *owner, taskType, goal, action, content,
		evidence, due, plan.ID, cand.TaskSubtype, cand.RecommendedProductID, priority, matchScore,
		cand.RiskMedical, cand.RiskCommercial, dedupeKey, expected, *owner).Scan(&taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx, `SELECT id FROM patient_operation_tasks WHERE tenant_id=$1 AND dedupe_key=$2`, plan.TenantID, dedupeKey).Scan(&taskID); err != nil {
			return 0, fmt.Errorf("load existing plan task: %w", err)
		}
		return taskID, nil
	}
	if err != nil {
		return 0, fmt.Errorf("insert plan task: %w", err)
	}
	return taskID, nil
}

func (s *Service) PublishPlan(ctx context.Context, tenantID, employeeID, planID int64, req PublishPlanRequest) (*PlanDetail, error) {
	// 经营方案内容生成入口的交付前预检：person 1 人。额度不足拒绝生成，预检故障放行。
	if err := s.ensurePlanContentQuota(ctx, tenantID); err != nil {
		return nil, err
	}
	plan, err := s.loadPlan(ctx, tenantID, planID)
	if err != nil {
		return nil, err
	}
	if plan.Status != "draft" {
		return nil, fmt.Errorf("only draft plans can be published")
	}
	if err := s.rejectUnconfirmedPlanCustomers(ctx, tenantID, req.CustomerIDs); err != nil {
		return nil, err
	}
	preview, err := s.buildPlanCandidates(ctx, plan)
	if err != nil {
		return nil, err
	}
	selected := req.CustomerIDs
	if len(selected) == 0 {
		for _, cand := range preview.Candidates {
			if cand.AdmissionStatus == "ready" {
				selected = append(selected, cand.CustomerID)
			}
		}
	}
	if len(selected) > 200 {
		return nil, fmt.Errorf("at most 200 customers can be published at once")
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no eligible customers to publish")
	}
	ordered, err := selectPlanCandidates(preview, selected, req.SuitabilityConfirmations)
	if err != nil {
		return nil, err
	}
	ordered = s.enrichPlanCandidatesWithLLM(ctx, plan, ordered)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin publish plan: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := validatePlanAdmissionSnapshot(ctx, tx, plan, ordered); err != nil {
		return nil, err
	}
	if err := savePlanSuitabilityReviews(ctx, tx, plan, employeeID, ordered, req.SuitabilityConfirmations); err != nil {
		return nil, err
	}
	published := 0
	generatedMemberIDs := make([]int64, 0, len(ordered))
	for _, cand := range ordered {
		memberID, err := s.insertPlanMember(ctx, tx, plan, cand)
		if err != nil {
			return nil, err
		}
		taskID, err := s.insertPlanTask(ctx, tx, plan, cand)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE patient_ops_plan_members SET task_id=$1,status='published',updated_at=NOW() WHERE id=$2`, taskID, memberID); err != nil {
			return nil, fmt.Errorf("link plan member task: %w", err)
		}
		if cand.ContentGenerated {
			generatedMemberIDs = append(generatedMemberIDs, memberID)
		}
		published++
	}
	if published == 0 {
		return nil, fmt.Errorf("no eligible customers were published")
	}
	if _, err := tx.Exec(ctx, `UPDATE patient_ops_plans SET status='published',published_at=NOW(),updated_at=NOW() WHERE tenant_id=$1 AND id=$2 AND status='draft'`, tenantID, planID); err != nil {
		return nil, fmt.Errorf("mark plan published: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit publish plan: %w", err)
	}
	// 方案内容已随发布落库：按会员扣 person 1，重复发布由幂等键拦截。
	s.chargePlanMembersContent(ctx, plan.TenantID, planID, generatedMemberIDs)
	return s.GetPlan(ctx, tenantID, planID)
}

func (s *Service) AddPlanMembers(ctx context.Context, tenantID, employeeID, planID int64, req AddPlanMembersRequest) (*PlanDetail, error) {
	// 经营方案内容生成入口的交付前预检：person 1 人。额度不足拒绝生成，预检故障放行。
	if err := s.ensurePlanContentQuota(ctx, tenantID); err != nil {
		return nil, err
	}
	plan, err := s.loadPlan(ctx, tenantID, planID)
	if err != nil {
		return nil, err
	}
	if plan.Status != "published" {
		return nil, fmt.Errorf("members can only be added to a published plan")
	}
	if len(req.CustomerIDs) == 0 {
		return nil, fmt.Errorf("customer_ids is required")
	}
	if len(req.CustomerIDs) > 200 {
		return nil, fmt.Errorf("at most 200 customers can be added at once")
	}
	if err := s.rejectUnconfirmedPlanCustomers(ctx, tenantID, req.CustomerIDs); err != nil {
		return nil, err
	}
	preview, err := s.buildPlanCandidates(ctx, plan)
	if err != nil {
		return nil, err
	}
	ordered, err := selectPlanCandidates(preview, req.CustomerIDs, req.SuitabilityConfirmations)
	if err != nil {
		return nil, err
	}
	ordered = s.enrichPlanCandidatesWithLLM(ctx, plan, ordered)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin add plan members: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := validatePlanAdmissionSnapshot(ctx, tx, plan, ordered); err != nil {
		return nil, err
	}
	if err := savePlanSuitabilityReviews(ctx, tx, plan, employeeID, ordered, req.SuitabilityConfirmations); err != nil {
		return nil, err
	}
	generatedMemberIDs := make([]int64, 0, len(ordered))
	for _, cand := range ordered {
		customerID := cand.CustomerID
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patient_ops_plan_members WHERE plan_id=$1 AND customer_id=$2)`, planID, customerID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check plan member: %w", err)
		}
		if exists {
			continue
		}
		memberID, err := s.insertPlanMember(ctx, tx, plan, cand)
		if err != nil {
			return nil, err
		}
		taskID, err := s.insertPlanTask(ctx, tx, plan, cand)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE patient_ops_plan_members SET task_id=$1,status='published',updated_at=NOW() WHERE id=$2`, taskID, memberID); err != nil {
			return nil, fmt.Errorf("link added plan member task: %w", err)
		}
		if cand.ContentGenerated {
			generatedMemberIDs = append(generatedMemberIDs, memberID)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit add plan members: %w", err)
	}
	// 方案内容已随加员落库：按会员扣 person 1，重复加员由幂等键拦截。
	s.chargePlanMembersContent(ctx, plan.TenantID, planID, generatedMemberIDs)
	return s.GetPlan(ctx, tenantID, planID)
}

func (s *Service) ClosePlan(ctx context.Context, tenantID, planID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin close plan: %w", err)
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE patient_ops_plans SET status='closed',updated_at=NOW() WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND status IN ('draft','published')`, tenantID, planID)
	if err != nil {
		return fmt.Errorf("close plan: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("plan not found or already closed")
	}
	// 结束计划后，未完成任务不再占用客户，避免阻塞后续计划筛选。
	if _, err := tx.Exec(ctx, `UPDATE patient_operation_tasks SET status='已取消',updated_at=NOW() WHERE tenant_id=$1 AND plan_id=$2 AND status NOT IN ('已完成','已取消')`, tenantID, planID); err != nil {
		return fmt.Errorf("cancel open plan tasks: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit close plan: %w", err)
	}
	return nil
}

const patientTaskSelect = `
	t.id,t.customer_id,t.employee_id,COALESCE(c.name,''),COALESCE(c.phone,''),COALESCE(emp.name,''),t.source_id,t.task_type,t.goal,t.action,t.content,t.evidence,
	COALESCE(t.due_at::text,'') AS due_at,t.status,t.created_at::text AS created_at,t.result,t.result_note,COALESCE(t.completed_at::text,''),
	t.parent_task_id,t.plan_id,t.plan_member_id,t.task_subtype,t.product_id,COALESCE(p.name,''),
	t.priority,t.match_score,t.risk_medical,t.risk_commercial,t.next_action,COALESCE(t.next_due_at::text,''),
	t.expected_result,t.result_amount,t.result_product_id,t.amount_source`

const patientTaskFrom = `
	FROM patient_operation_tasks t
	JOIN customers c ON c.id=t.customer_id AND c.tenant_id=t.tenant_id
	LEFT JOIN employees emp ON emp.id=t.employee_id
	LEFT JOIN products p ON p.id=t.product_id`

// scanPatientTask 解析 patientTaskSelect 的列。extra 用于查询在任务列之后追加的列
// （例如按客户分组时附带的 pending_count）。
func scanPatientTask(row interface {
	Scan(dest ...any) error
}, extra ...any) (PatientTaskResponse, error) {
	var item PatientTaskResponse
	var planID, planMemberID, productID, parentTaskID, matchScore, resultProductID sql.NullInt64
	var resultAmount sql.NullFloat64
	dest := []any{
		&item.ID, &item.CustomerID, &item.OwnerID, &item.CustomerName, &item.CustomerPhone, &item.OwnerName, &item.SourceID, &item.TaskType, &item.Goal, &item.Action, &item.Content, &item.Evidence,
		&item.DueAt, &item.Status, &item.CreatedAt, &item.Result, &item.ResultNote, &item.CompletedAt,
		&parentTaskID, &planID, &planMemberID, &item.TaskSubtype, &productID, &item.ProductName,
		&item.Priority, &matchScore, &item.RiskMedical, &item.RiskCommercial, &item.NextAction, &item.NextDueAt,
		&item.ExpectedResult, &resultAmount, &resultProductID, &item.AmountSource,
	}
	dest = append(dest, extra...)
	err := row.Scan(dest...)
	if err != nil {
		return item, err
	}
	if planID.Valid {
		value := planID.Int64
		item.PlanID = &value
	}
	if planMemberID.Valid {
		value := planMemberID.Int64
		item.PlanMemberID = &value
	}
	if productID.Valid {
		value := productID.Int64
		item.ProductID = &value
	}
	if parentTaskID.Valid {
		value := parentTaskID.Int64
		item.ParentTaskID = &value
	}
	if matchScore.Valid {
		value := int(matchScore.Int64)
		item.MatchScore = &value
	}
	if resultAmount.Valid {
		value := resultAmount.Float64
		item.ResultAmount = &value
	}
	if resultProductID.Valid {
		value := resultProductID.Int64
		item.ResultProductID = &value
	}
	return item, nil
}

type UnconfirmedPlanCustomersError struct {
	Customers []PlanExcluded `json:"excluded_customers"`
}

func (e *UnconfirmedPlanCustomersError) Error() string {
	names := make([]string, 0, len(e.Customers))
	for _, c := range e.Customers {
		names = append(names, fmt.Sprintf("%s（ID %d）", c.CustomerName, c.CustomerID))
	}
	return "以下客户尚未确认身份：" + strings.Join(names, "、") + "。本次请求未执行，请先在待确认客户中确认，或移除这些客户后重试"
}
func (s *Service) rejectUnconfirmedPlanCustomers(ctx context.Context, tenantID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id,name FROM customers WHERE tenant_id=$1 AND id=ANY($2::bigint[]) AND deleted_at IS NULL AND extra_data->>'identity_status'='pending_confirm' ORDER BY id`, tenantID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	e := &UnconfirmedPlanCustomersError{}
	for rows.Next() {
		var c PlanExcluded
		if err := rows.Scan(&c.CustomerID, &c.CustomerName); err != nil {
			return err
		}
		c.Reason = "尚未确认身份，请先在待确认客户中确认"
		e.Customers = append(e.Customers, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(e.Customers) > 0 {
		return e
	}
	return nil
}
