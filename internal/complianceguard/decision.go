package complianceguard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/compliancepermission"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5"
)

// 本文件承载「处理决定 / 签收 / 复查 / 关闭」的正式落库逻辑。
//
// 页面把核实结论收敛成三分法 close（关闭，误报）/ letter（发改进意见书）/
// rectify（立项整改）；既有 /process 的结论枚举与三分法不是一一对应，
// 因此新增 decision_action 列单独记录三分法，同时把 action 映射到既有
// conclusion 枚举，保证历史统计口径与 /process 兼容。

// decisionActionConclusions 是页面三分法到既有结论枚举的映射。
//
// close → not_confirmed（不成立，误报关闭）
// letter / rectify → confirmed（成立，进入整改/意见书流程）
var decisionActionConclusions = map[string]string{
	"close":   "not_confirmed",
	"letter":  "confirmed",
	"rectify": "confirmed",
}

// decisionReviewConclusions 是复查结论的合法取值（与页面下拉一致）。
var decisionReviewConclusions = map[string]bool{
	"pass":  true,
	"fail":  true,
	"watch": true,
}

// checkItemSelectColumns 是核查事项列表/详情的统一列清单（不带表别名）。
const checkItemSelectColumns = `id::text,tenant_id,source_type,source_id,source_version,scene_code,role_code,employee_id,theme_key,question,risk_hypothesis,primary_risk_type,related_risk_types,evidence,check_basis,suggested_actions,priority,status,COALESCE(conclusion,''),COALESCE(process_reason,''),process_actions,COALESCE(process_note,''),created_at,updated_at,COALESCE(decision_action,''),COALESCE(decision_reason,''),decision_by,decision_at,COALESCE(sign_method,''),signed_at,review_date,COALESCE(review_method,''),COALESCE(review_conclusion,''),closed_at`

// checkItemSelectColumnsAliased 同上，用于带表别名 i 的查询。
const checkItemSelectColumnsAliased = `i.id::text,i.tenant_id,i.source_type,i.source_id,i.source_version,i.scene_code,i.role_code,i.employee_id,i.theme_key,i.question,i.risk_hypothesis,i.primary_risk_type,i.related_risk_types,i.evidence,i.check_basis,i.suggested_actions,i.priority,i.status,COALESCE(i.conclusion,''),COALESCE(i.process_reason,''),i.process_actions,COALESCE(i.process_note,''),i.created_at,i.updated_at,COALESCE(i.decision_action,''),COALESCE(i.decision_reason,''),i.decision_by,i.decision_at,COALESCE(i.sign_method,''),i.signed_at,i.review_date,COALESCE(i.review_method,''),COALESCE(i.review_conclusion,''),i.closed_at`

type checkItemScanner interface {
	Scan(dest ...any) error
}

// scanCheckItem 按 checkItemSelectColumns 的顺序读取一行核查事项。
func scanCheckItem(row checkItemScanner, item *CheckItemListItem) error {
	return row.Scan(
		&item.ID, &item.TenantID, &item.SourceType, &item.SourceID, &item.SourceVersion,
		&item.SceneCode, &item.RoleCode, &item.EmployeeID, &item.ThemeKey, &item.Question,
		&item.RiskHypothesis, &item.PrimaryRiskType, &item.RelatedRiskTypes, &item.Evidence,
		&item.CheckBasis, &item.SuggestedActions, &item.Priority, &item.Status, &item.Conclusion,
		&item.ProcessReason, &item.ProcessActions, &item.ProcessNote, &item.CreatedAt, &item.UpdatedAt,
		&item.DecisionAction, &item.DecisionReason, &item.DecisionBy, &item.DecisionAt,
		&item.SignMethod, &item.SignedAt, &item.ReviewDate, &item.ReviewMethod,
		&item.ReviewConclusion, &item.ClosedAt,
	)
}

// parseDecisionTime 解析前端传来的时间。
//
// 页面既有 yyyy-mm-dd 的日期输入，也可能传完整 RFC3339；两种都接受。
func parseDecisionTime(field, value string) (*time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	// 带时区的优先按 RFC3339 解析；纯日期/无时区时间按服务器本地时区解释，
	// 避免 yyyy-mm-dd 在东八区被存成前一天 16:00。
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return &parsed, nil
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, trimmed, time.Local); err == nil {
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("%s 不是有效日期，请使用 YYYY-MM-DD 或 RFC3339", field)
}

// parseDecisionDate 只保留日期部分（review_date 是 DATE 列）。
func parseDecisionDate(field, value string) (*time.Time, error) {
	parsed, err := parseDecisionTime(field, value)
	if err != nil || parsed == nil {
		return parsed, err
	}
	date := time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, time.UTC)
	return &date, nil
}

// DecideCheckItem 把页面处理决定、签收、复查、关闭写回数据库。
//
// action 缺省时沿用已落库的 decision_action（用于签收 / 复查 / 关闭等后续补充），
// 事项没有处理决定且未传 action 时报错。所有字段按 COALESCE 语义写入，
// 本次没传的字段保持原值，不会把已记录的签收/复查信息清空。
func (s *Service) DecideCheckItem(ctx context.Context, tenantID int64, id string, actor int64, req CheckItemDecisionRequest) (*CheckItemListItem, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	if tenantID <= 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("check item id is required")
	}

	action := strings.TrimSpace(req.Action)
	reason := strings.TrimSpace(req.Reason)
	signMethod := strings.TrimSpace(req.SignMethod)
	reviewMethod := strings.TrimSpace(req.ReviewMethod)
	reviewConclusion := strings.TrimSpace(req.ReviewConclusion)
	if reviewConclusion != "" && !decisionReviewConclusions[reviewConclusion] {
		return nil, fmt.Errorf("review_conclusion 只能是 pass、fail 或 watch")
	}
	signedAt, err := parseDecisionTime("signed_at", req.SignedAt)
	if err != nil {
		return nil, err
	}
	closedAt, err := parseDecisionTime("closed_at", req.ClosedAt)
	if err != nil {
		return nil, err
	}
	reviewDate, err := parseDecisionDate("review_date", req.ReviewDate)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var currentAction string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(decision_action,'') FROM compliance_check_items WHERE tenant_id=$1 AND id::text=$2 FOR UPDATE`, tenantID, id).Scan(&currentAction); err != nil {
		return nil, err
	}
	// 首次提交处理决定时 action 必填；后续签收 / 复查 / 关闭可以省略 action。
	newDecision := currentAction == ""
	if action == "" {
		action = currentAction
	}
	conclusion, ok := decisionActionConclusions[action]
	if !ok {
		return nil, fmt.Errorf("action 只能是 close、letter 或 rectify")
	}
	if newDecision && reason == "" {
		if action == "close" {
			reason = "误报关闭"
		} else {
			return nil, fmt.Errorf("发改进意见书或立项整改必须填写理由")
		}
	}

	// 记录签收但没给时间时，按当前时间入库；关闭同理，见 closeNow。
	if signMethod != "" && signedAt == nil {
		now := time.Now()
		signedAt = &now
	}

	// close 在未显式给出关闭时间时，以决定时间作为关闭时间。
	closeNow := action == "close" && closedAt == nil

	auditActions, _ := json.Marshal(map[string]any{
		"action":            action,
		"reason":            reason,
		"sign_method":       signMethod,
		"signed_at":         formatDecisionTime(signedAt),
		"review_date":       formatDecisionDate(reviewDate),
		"review_method":     reviewMethod,
		"review_conclusion": reviewConclusion,
		"closed_at":         formatDecisionTime(closedAt),
	})
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("decision:%s:%d", action, time.Now().UnixNano())
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO compliance_check_item_process_records(item_id,tenant_id,actor_id,idempotency_key,conclusion,reason,actions,note)
		VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8)
	`, id, tenantID, actor, idempotencyKey, conclusion, reason, string(auditActions), ""); err != nil {
		if isUniqueViolation(err) {
			// 同一幂等键重复提交：已经在库里，按幂等重放返回当前状态。
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return nil, rollbackErr
			}
			return s.GetCheckItem(ctx, tenantID, id)
		}
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE compliance_check_items SET
			status='processed',
			processed_by=COALESCE(processed_by,$3),
			processed_at=COALESCE(processed_at,NOW()),
			conclusion=$4,
			process_reason=COALESCE(NULLIF($5,''),process_reason),
			decision_action=$6,
			decision_reason=COALESCE(NULLIF($5,''),decision_reason),
			decision_by=COALESCE(decision_by,$3),
			decision_at=COALESCE(decision_at,NOW()),
			sign_method=COALESCE(NULLIF($7,''),sign_method),
			signed_at=COALESCE($8,signed_at),
			review_date=COALESCE($9,review_date),
			review_method=COALESCE(NULLIF($10,''),review_method),
			review_conclusion=COALESCE(NULLIF($11,''),review_conclusion),
			closed_at=COALESCE($12,closed_at,CASE WHEN $13 THEN NOW() END),
			updated_at=NOW()
		WHERE tenant_id=$1 AND id::text=$2
	`, tenantID, id, actor, conclusion, reason, action, signMethod, signedAt, reviewDate, reviewMethod, reviewConclusion, closedAt, closeNow); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetCheckItem(ctx, tenantID, id)
}

func formatDecisionTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

func formatDecisionDate(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format("2006-01-02")
}

// monthlyTrend 统计近 4 个自然月（含本月）的核查事项数量。
func (s *Service) monthlyTrend(ctx context.Context, tenantID int64) ([]MonthlyTrendPoint, error) {
	rows, err := s.pool.Query(ctx, `
		WITH months AS (
			SELECT date_trunc('month', NOW()) - make_interval(months => n) AS month_start
			FROM generate_series(0, 3) AS n
		)
		SELECT to_char(months.month_start, 'YYYY-MM'), COUNT(i.id)
		FROM months
		LEFT JOIN compliance_check_items i
		  ON i.tenant_id = $1
		 AND i.created_at >= months.month_start
		 AND i.created_at < months.month_start + interval '1 month'
		GROUP BY months.month_start
		ORDER BY months.month_start
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]MonthlyTrendPoint, 0, 4)
	for rows.Next() {
		var point MonthlyTrendPoint
		if err := rows.Scan(&point.Month, &point.Count); err != nil {
			return nil, err
		}
		points = append(points, point)
	}
	return points, rows.Err()
}

// loadEncounterStats 汇总接诊口径：接诊总量、有问题接诊数、覆盖医生/科室数。
//
// 接诊总量来自 recordings（排除已删除）；有问题接诊数按核查事项的
// source_id 关联 recordings 去重，避免同一条接诊的多条事项重复计数。
func (s *Service) loadEncounterStats(ctx context.Context, tenantID int64, overview *ComplianceOverview) error {
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT r.employee_id), COUNT(DISTINCT e.department_id)
		FROM recordings r
		LEFT JOIN employees e
		  ON e.id = r.employee_id AND e.tenant_id = r.tenant_id AND e.deleted_at IS NULL
		WHERE r.tenant_id = $1 AND r.deleted_at IS NULL
	`, tenantID).Scan(&overview.EncountersTotal, &overview.DoctorsCount, &overview.DepartmentsCount); err != nil {
		return fmt.Errorf("count compliance encounters: %w", err)
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT i.source_id)
		FROM compliance_check_items i
		WHERE i.tenant_id = $1
	`, tenantID).Scan(&overview.AnalyzedEncounters); err != nil {
		return fmt.Errorf("count analyzed encounters: %w", err)
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT r.id)
		FROM compliance_check_items i
		JOIN recordings r
		  ON r.id::text = i.source_id AND r.tenant_id = i.tenant_id AND r.deleted_at IS NULL
		WHERE i.tenant_id = $1
	`, tenantID).Scan(&overview.EncountersWithFindings); err != nil {
		return fmt.Errorf("count compliance encounters with findings: %w", err)
	}

	if overview.AnalyzedEncounters > 0 {
		clean := overview.AnalyzedEncounters - overview.EncountersWithFindings
		if clean < 0 {
			clean = 0
		}
		rate := float64(clean) / float64(overview.AnalyzedEncounters) * 100
		overview.ComplianceRate = math.Round(rate*100) / 100
	}
	return nil
}

// DecideCheckItem 是正式路由：JWT + compliance.finding.review 权限 + 租户隔离。
func (h *Handler) DecideCheckItem(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingReview)
	if !ok {
		return
	}
	var req CheckItemDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	item, err := h.service.DecideCheckItem(r.Context(), tenantID, r.PathValue("id"), claims.UserID, req)
	if err != nil {
		if err == pgx.ErrNoRows {
			httputil.WriteNotFound(w, "check item not found")
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

// DecideCheckItemDebug 是开发环境的免鉴权别名，租户从查询参数读取。
//
// 生产环境不会注册该路由；仅用于本地联调与 Playwright 验证。
func (h *Handler) DecideCheckItemDebug(w http.ResponseWriter, r *http.Request) {
	if !h.debugRoutesEnabled() {
		httputil.WriteForbidden(w, "调试接口仅开发环境可用")
		return
	}
	tenantID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("tenant_id")), 10, 64)
	if err != nil || tenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
		return
	}
	var req CheckItemDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	item, err := h.service.DecideCheckItem(r.Context(), tenantID, r.PathValue("id"), 0, req)
	if err != nil {
		if err == pgx.ErrNoRows {
			httputil.WriteNotFound(w, "check item not found")
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}
