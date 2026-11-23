package complianceguard

import (
	"context"
	"fmt"
	"strings"
)

// CheckItemListParams 是核查台账（/pending）分页列表的入参。
// Tab 取值与页面一致：pending（待核实）/ reviewing（待复查）/ closed（已关闭）/ training（培训池）。
type CheckItemListParams struct {
	Tab        string
	Query      string
	EmployeeID *int64
	// Person 按「员工显示名」过滤，供人员核查记录详情页只取该人的事项。
	Person   string
	Page     int
	PageSize int
	// ViewerID 当前查看人：用于算「未读」角标（进入页面前没看过的待核查事项）。
	ViewerID int64
}

// checkItemPhaseExpr 把数据库状态映射成页面三分法，口径与前端 statusOf 一致：
// 关闭 → closed；已出决定未关闭或 in_progress → reviewing；其余 → pending。
const checkItemPhaseExpr = `(CASE
		WHEN i.decision_action = 'close' OR i.closed_at IS NOT NULL THEN 'closed'
		WHEN i.decision_action IS NOT NULL OR i.status = 'in_progress' THEN 'reviewing'
		ELSE 'pending'
	END)`

// checkItemTrainExpr 对应页面的「培训池」：priority=normal 的事项不算待核实。
const checkItemTrainExpr = `(i.priority = 'normal')`

// checkItemEnrichedFrom 是列表用的公共 FROM：随行带出员工、科室、规则名，
// 前端不再为显示人名额外拉 200 条发现记录。
const checkItemEnrichedFrom = `
	FROM compliance_check_items i
	LEFT JOIN employees e ON e.id = i.employee_id AND e.tenant_id = i.tenant_id AND e.deleted_at IS NULL
	LEFT JOIN departments d ON d.id = e.department_id
	LEFT JOIN LATERAL (
		SELECT src.rule_code
		FROM compliance_check_item_sources src
		WHERE src.item_id = i.id
		ORDER BY src.created_at DESC
		LIMIT 1
	) src ON true
	LEFT JOIN LATERAL (
		SELECT r.rule_name
		FROM compliance_rules r
		WHERE r.rule_code = src.rule_code AND r.status = 'published'
		ORDER BY r.updated_at DESC, r.id DESC
		LIMIT 1
	) rn ON true
	WHERE i.tenant_id = $1`

const checkItemEnrichedColumns = `, COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '') AS employee_name,
	COALESCE(d.name, '') AS department,
	COALESCE(src.rule_code, '') AS rule_code,
	COALESCE(rn.rule_name, '') AS rule_name`

func checkItemFilters(params CheckItemListParams, args *[]any) string {
	filter := ""
	if q := strings.TrimSpace(params.Query); q != "" {
		*args = append(*args, "%"+q+"%")
		n := len(*args)
		filter += fmt.Sprintf(` AND (i.question ILIKE $%[1]d OR i.risk_hypothesis ILIKE $%[1]d OR COALESCE(e.full_name, e.name, '') ILIKE $%[1]d OR COALESCE(d.name, '') ILIKE $%[1]d)`, n)
	}
	if person := strings.TrimSpace(params.Person); person != "" {
		*args = append(*args, person)
		filter += fmt.Sprintf(" AND COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未记录人员') = $%d", len(*args))
	}
	if params.EmployeeID != nil && *params.EmployeeID > 0 {
		*args = append(*args, *params.EmployeeID)
		filter += fmt.Sprintf(" AND i.employee_id = $%d", len(*args))
	}
	return filter
}

// tabCondition 把 Tab 翻译成 SQL 条件；空 Tab 表示不过滤。
func tabCondition(tab string) string {
	switch strings.TrimSpace(tab) {
	case "pending":
		return " AND " + checkItemPhaseExpr + " = 'pending' AND NOT " + checkItemTrainExpr
	case "reviewing":
		return " AND " + checkItemPhaseExpr + " = 'reviewing'"
	case "closed":
		return " AND " + checkItemPhaseExpr + " = 'closed'"
	case "training":
		return " AND " + checkItemTrainExpr
	default:
		return ""
	}
}

// ListCheckItemsPaged 返回一页核查事项（含员工/科室/规则）以及各 Tab 的条数。
func (s *Service) ListCheckItemsPaged(ctx context.Context, tenantID int64, params CheckItemListParams) ([]CheckItemListItem, int, map[string]int64, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 || params.PageSize > 200 {
		params.PageSize = 20
	}
	args := []any{tenantID}
	filter := checkItemFilters(params, &args)

	// 计数：只算 phase / 是否培训，不取重字段。
	// 未读数：当前查看人没看过的待核查事项（角标用）。
	var unreadPending, unreadPersons int64
	if params.ViewerID > 0 {
		unreadArgs := append(append([]any{}, args...), params.ViewerID)
		viewerArg := fmt.Sprintf("$%d", len(unreadArgs))
		unreadQuery := `WITH base AS (
				SELECT ` + checkItemPhaseExpr + ` AS phase, ` + checkItemTrainExpr + ` AS is_train, i.employee_id, i.id AS item_id` + checkItemEnrichedFrom + filter + `
			)
			SELECT count(*) FILTER (WHERE phase = 'pending' AND NOT is_train AND NOT EXISTS (
			           SELECT 1 FROM user_resource_reads m WHERE m.tenant_id = $1 AND m.employee_id = ` + viewerArg + `
			             AND m.resource_type = 'compliance_finding' AND m.resource_id = base.item_id::text)),
			       count(DISTINCT employee_id) FILTER (WHERE phase <> 'closed' AND NOT EXISTS (
			           SELECT 1 FROM user_resource_reads m WHERE m.tenant_id = $1 AND m.employee_id = ` + viewerArg + `
			             AND m.resource_type = 'compliance_finding' AND m.resource_id = base.item_id::text))
			FROM base`
		if err := s.pool.QueryRow(ctx, unreadQuery, unreadArgs...).Scan(&unreadPending, &unreadPersons); err != nil {
			return nil, 0, nil, fmt.Errorf("count unread compliance check items: %w", err)
		}
	}

	countQuery := `WITH base AS (
			SELECT ` + checkItemPhaseExpr + ` AS phase, ` + checkItemTrainExpr + ` AS is_train, i.employee_id` + checkItemEnrichedFrom + filter + `
		)
		SELECT count(*) FILTER (WHERE phase = 'pending' AND NOT is_train),
		       count(*) FILTER (WHERE phase = 'reviewing'),
		       count(*) FILTER (WHERE phase = 'closed'),
		       count(*) FILTER (WHERE is_train),
		       count(*),
		       count(DISTINCT employee_id) FILTER (WHERE phase <> 'closed')
		FROM base`
	var pendingCount, reviewingCount, closedCount, trainingCount, totalCount, personCount int64
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(
		&pendingCount, &reviewingCount, &closedCount, &trainingCount, &totalCount, &personCount,
	); err != nil {
		return nil, 0, nil, fmt.Errorf("count compliance check items: %w", err)
	}
	counts := map[string]int64{
		"pending":        pendingCount,
		"reviewing":      reviewingCount,
		"closed":         closedCount,
		"training":       trainingCount,
		"total":          totalCount,
		"persons":        personCount,
		"unread_pending": unreadPending,
		"unread_persons": unreadPersons,
	}

	query := `SELECT ` + checkItemSelectColumnsAliased + checkItemEnrichedColumns +
		checkItemEnrichedFrom + filter + tabCondition(params.Tab) +
		fmt.Sprintf(" ORDER BY i.created_at DESC, i.id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)

	rows, err := s.pool.Query(ctx, query, append(append([]any{}, args...), params.PageSize, (params.Page-1)*params.PageSize)...)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("list compliance check items: %w", err)
	}
	defer rows.Close()

	items := []CheckItemListItem{}
	for rows.Next() {
		var item CheckItemListItem
		if err := scanCheckItem(rows, &item, &item.EmployeeName, &item.Department, &item.RuleCode, &item.RuleName); err != nil {
			return nil, 0, nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, nil, err
	}
	// total 跟随当前 Tab（用于分页），counts 保留各 Tab 的数，供页签显示。
	total := totalCount
	if tab := strings.TrimSpace(params.Tab); tab != "" {
		if v, ok := counts[tab]; ok {
			total = v
		}
	}
	return items, int(total), counts, nil
}

// ListCheckItemsAll 返回全量核查事项（带员工/科室/规则）。仅用于仍需要全量做聚合的
// 页面（风险总览、人员核查记录）；台账页一律走分页接口。
func (s *Service) ListCheckItemsAll(ctx context.Context, tenantID int64) ([]CheckItemListItem, error) {
	query := `SELECT ` + checkItemSelectColumnsAliased + checkItemEnrichedColumns +
		checkItemEnrichedFrom + ` ORDER BY i.created_at DESC`
	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list compliance check items: %w", err)
	}
	defer rows.Close()
	items := []CheckItemListItem{}
	for rows.Next() {
		var item CheckItemListItem
		if err := scanCheckItem(rows, &item, &item.EmployeeName, &item.Department, &item.RuleCode, &item.RuleName); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
