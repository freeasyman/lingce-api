package dealintervention

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrSignalNotFound = errors.New("intervention signal not found")

// Signal 是介入信号读模型：Web 工作台与 H5 共用。
type Signal struct {
	ID             int64      `json:"id"`
	RecordingID    int64      `json:"recording_id"`
	CustomerName   string     `json:"customer_name"`
	ProjectName    string     `json:"project_name"`
	ConsultantName string     `json:"consultant_name"`
	RecordedAt     string     `json:"recorded_at,omitempty"`
	Level          string     `json:"level"`
	Reason         string     `json:"reason"`
	Action         string     `json:"action"`
	Evidence       string     `json:"evidence,omitempty"`
	Status         string     `json:"status"`
	ShortCode      string     `json:"short_code,omitempty"`
	Note           string     `json:"note,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	HandledAt      *time.Time `json:"handled_at,omitempty"`
	HandledBy      *int64     `json:"handled_by,omitempty"`
	HandledByName  string     `json:"handled_by_name,omitempty"`
	ManagerID      int64      `json:"manager_employee_id"`
	ConsultantID   int64      `json:"consultant_employee_id"`
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

const signalColumns = `s.id, s.recording_id, s.customer_name, s.project_name, s.level, s.reason, s.action,
	s.evidence, s.status, s.short_code, s.note, s.created_at, s.handled_at, s.handled_by,
	COALESCE(NULLIF(e.full_name,''), NULLIF(e.name,''), ''), COALESCE(r.recorded_at::text, ''),
	COALESCE(NULLIF(h.full_name,''), NULLIF(h.name,''), ''), s.manager_employee_id, s.consultant_employee_id`

func scanSignal(row rowScanner) (*Signal, error) {
	var item Signal
	err := row.Scan(&item.ID, &item.RecordingID, &item.CustomerName, &item.ProjectName, &item.Level, &item.Reason, &item.Action,
		&item.Evidence, &item.Status, &item.ShortCode, &item.Note, &item.CreatedAt, &item.HandledAt, &item.HandledBy,
		&item.ConsultantName, &item.RecordedAt, &item.HandledByName, &item.ManagerID, &item.ConsultantID)
	return &item, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

// List 返回介入信号列表：status=pending/handled（intervened+dismissed）。
// employeeIDs 来自统一数据可见范围：nil 表示全机构，否则只看这些咨询师名下的信号。
func (s *Service) List(ctx context.Context, tenantID int64, status string, employeeIDs []int64, page, pageSize int) ([]*Signal, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	statusCond := "1=1"
	switch status {
	case "pending":
		statusCond = "s.status = 'pending'"
	case "handled":
		statusCond = "s.status IN ('intervened','dismissed')"
	}
	args := []any{tenantID}
	where := fmt.Sprintf("s.tenant_id = $1 AND %s", statusCond)
	if employeeIDs != nil {
		args = append(args, employeeIDs)
		where += fmt.Sprintf(" AND s.consultant_employee_id = ANY($%d::bigint[])", len(args))
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM deal_intervention_signals s WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count intervention signals: %w", err)
	}

	args = append(args, pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(`
		SELECT %s
		FROM deal_intervention_signals s
		LEFT JOIN recordings r ON r.id = s.recording_id AND r.tenant_id = s.tenant_id AND r.deleted_at IS NULL
		LEFT JOIN employees e ON e.id = s.consultant_employee_id
		LEFT JOIN employees h ON h.id = s.handled_by
		WHERE %s
		ORDER BY s.created_at DESC, s.id DESC
		LIMIT $%d OFFSET $%d`, signalColumns, where, len(args)-1, len(args))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list intervention signals: %w", err)
	}
	defer rows.Close()

	items := make([]*Signal, 0, pageSize)
	for rows.Next() {
		item, err := scanSignal(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan intervention signal: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// GetByRecording 返回一条接待最新的介入信号。
func (s *Service) GetByRecording(ctx context.Context, tenantID, recordingID int64) (*Signal, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM deal_intervention_signals s
		LEFT JOIN recordings r ON r.id = s.recording_id AND r.tenant_id = s.tenant_id AND r.deleted_at IS NULL
		LEFT JOIN employees e ON e.id = s.consultant_employee_id
		LEFT JOIN employees h ON h.id = s.handled_by
		WHERE s.tenant_id = $1 AND s.recording_id = $2
		ORDER BY s.id DESC LIMIT 1`, signalColumns)
	item, err := scanSignal(s.pool.QueryRow(ctx, query, tenantID, recordingID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSignalNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get intervention signal: %w", err)
	}
	return item, nil
}

// Act 处理介入信号：intervened/dismissed；幂等——已处理返回当前状态，不报错。
// 同时写 analysis_actions（action_type=intervened/dismissed，note 同步）。
func (s *Service) Act(ctx context.Context, tenantID, recordingID, actorID int64, actorName, action, note string) (*Signal, error) {
	if action != "intervened" && action != "dismissed" {
		return nil, fmt.Errorf("invalid action")
	}
	current, err := s.GetByRecording(ctx, tenantID, recordingID)
	if err != nil {
		return nil, err
	}
	if current.Status != "pending" {
		// 幂等：已处理直接返回当前状态。
		return current, nil
	}

	var actorNameResolved = actorName
	if actorNameResolved == "" {
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(NULLIF(full_name,''), name, '') FROM employees WHERE id = $1
		`, actorID).Scan(&actorNameResolved)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE deal_intervention_signals
		SET status = $3, note = $4, handled_at = NOW(), handled_by = $5
		WHERE tenant_id = $1 AND recording_id = $2
	`, tenantID, recordingID, action, note, actorID); err != nil {
		return nil, fmt.Errorf("update intervention signal: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO analysis_actions (tenant_id, recording_id, action_type, note, actor_employee_id, actor_name)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, tenantID, recordingID, action, note, actorID, actorNameResolved); err != nil {
		return nil, fmt.Errorf("insert analysis action: %w", err)
	}
	return s.GetByRecording(ctx, tenantID, recordingID)
}

// ActionRequest 是处理介入信号的请求体。
type ActionRequest struct {
	Action string `json:"action"`
	Note   string `json:"note"`
}
