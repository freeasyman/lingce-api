package dealexpush

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EmailEvent 是邮件投递所需的事件快照（只读）。
type EmailEvent struct {
	ID                  int64
	TenantID            int64
	RecordingID         int64
	RecipientEmployeeID int64
	AnalysisVersion     string
	RecipientRole       string
	// ConsultantEmployeeID 是该接待的咨询师（recordings.employee_id），负责人事件的 owner。
	ConsultantEmployeeID int64
	CustomerName         string
	CustomerGender       string
}

// AnalysisCopy 是邮件正文可用的、已脱敏前的分析字段。
type AnalysisCopy struct {
	DealStatus     string
	FirstTaskTitle string
	ManagerReason  string
	ManagerAction  string
}

// AlertRoute 是租户内「谁收 / 谁抄送」配置。
type AlertRoute struct {
	ID              int64     `json:"id"`
	TenantID        int64     `json:"tenant_id"`
	OwnerEmployeeID int64     `json:"owner_employee_id"`
	OwnerName       string    `json:"owner_name"`
	OwnerEmail      string    `json:"owner_email"`
	EventType       string    `json:"event_type"`
	ToEmployeeIDs   []int64   `json:"to_employee_ids"`
	CcEmployeeIDs   []int64   `json:"cc_employee_ids"`
	CreatedBy       int64     `json:"created_by"`
	UpdatedBy       int64     `json:"updated_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// EmailEmployee 是路由/员工选择器需要的员工信息。
type EmailEmployee struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Email          string `json:"email"`
	Role           string `json:"role"`
	DepartmentName string `json:"department_name"`
}

// EmailDelivery 是 deal_email_deliveries 的一行。
type EmailDelivery struct {
	ID                  int64      `json:"id"`
	TenantID            int64      `json:"tenant_id"`
	EventID             int64      `json:"event_id"`
	RecipientEmployeeID int64      `json:"recipient_employee_id"`
	Email               string     `json:"email"`
	Kind                string     `json:"kind"`
	Status              string     `json:"status"`
	MessageID           string     `json:"message_id"`
	Error               string     `json:"error"`
	SentAt              *time.Time `json:"sent_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

// EmailStore 抽象邮件路由/投递所需的数据库操作，便于 fake SMTP 单测注入内存实现。
type EmailStore interface {
	LoadEmailEvent(ctx context.Context, eventID int64) (*EmailEvent, error)
	LoadAnalysisCopy(ctx context.Context, tenantID, recordingID int64, analysisVersion string) (*AnalysisCopy, error)
	Employee(ctx context.Context, tenantID, employeeID int64) (*EmailEmployee, error)
	DefaultManager(ctx context.Context, tenantID, ownerEmployeeID int64) (int64, error)
	Route(ctx context.Context, tenantID, ownerEmployeeID int64, eventType string) (*AlertRoute, error)
	ListRoutes(ctx context.Context, tenantID, ownerEmployeeID int64) ([]*AlertRoute, error)
	ListEmployees(ctx context.Context, tenantID int64) ([]*EmailEmployee, error)
	UpsertRoute(ctx context.Context, route *AlertRoute) (*AlertRoute, error)
	EmployeesExist(ctx context.Context, tenantID int64, employeeIDs []int64) (bool, error)
	Delivery(ctx context.Context, eventID, employeeID int64) (*EmailDelivery, error)
	SaveDelivery(ctx context.Context, delivery *EmailDelivery) error
	EnsureShortLink(ctx context.Context, tenantID, recordingID, employeeID int64) (string, error)
}

type pgEmailStore struct {
	pool *pgxpool.Pool

	managerColumnOnce sync.Once
	managerColumn     bool
}

func newPGEmailStore(pool *pgxpool.Pool) *pgEmailStore {
	return &pgEmailStore{pool: pool}
}

// LoadEmailEvent 读取推送事件和接待/客户信息；recipient_role 已写入 payload。
func (s *pgEmailStore) LoadEmailEvent(ctx context.Context, eventID int64) (*EmailEvent, error) {
	var item EmailEvent
	err := s.pool.QueryRow(ctx, `
		SELECT e.id, e.tenant_id, e.recording_id, e.recipient_employee_id,
		       COALESCE(e.analysis_version, ''),
		       COALESCE(e.payload->>'recipient_role', 'consultant'),
		       COALESCE(r.employee_id, 0),
		       COALESCE(c.name, ''), COALESCE(c.gender, '')
		FROM deal_push_events e
		LEFT JOIN recordings r ON r.id = e.recording_id AND r.tenant_id = e.tenant_id
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE e.id = $1
	`, eventID).Scan(&item.ID, &item.TenantID, &item.RecordingID, &item.RecipientEmployeeID,
		&item.AnalysisVersion, &item.RecipientRole, &item.ConsultantEmployeeID,
		&item.CustomerName, &item.CustomerGender)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEmailEventNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load email event: %w", err)
	}
	return &item, nil
}

func (s *pgEmailStore) LoadAnalysisCopy(ctx context.Context, tenantID, recordingID int64, analysisVersion string) (*AnalysisCopy, error) {
	var item AnalysisCopy
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(analysis->>'deal_status', ''),
		       COALESCE(analysis->'sales_tasks'->0->>'title', ''),
		       COALESCE(analysis->'manager_signal'->>'reason', ''),
		       COALESCE(analysis->'manager_signal'->>'action', '')
		FROM consultation_analyses
		WHERE tenant_id = $1 AND recording_id = $2 AND status = 'analyzed'
		ORDER BY CASE WHEN prompt_version = $3 THEN 0 ELSE 1 END, updated_at DESC, id DESC
		LIMIT 1
	`, tenantID, recordingID, strings.TrimSpace(analysisVersion)).Scan(
		&item.DealStatus, &item.FirstTaskTitle, &item.ManagerReason, &item.ManagerAction)
	if errors.Is(err, pgx.ErrNoRows) {
		return &AnalysisCopy{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load analysis copy: %w", err)
	}
	return &item, nil
}

func (s *pgEmailStore) Employee(ctx context.Context, tenantID, employeeID int64) (*EmailEmployee, error) {
	var item EmailEmployee
	err := s.pool.QueryRow(ctx, `
		SELECT e.id,
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), ''),
		       COALESCE(e.email, ''),
		       COALESCE(NULLIF(e.role, ''), (
		           SELECT lower(trim(ier.role_code))
		           FROM institution_employee_roles ier
		           WHERE ier.employee_id = e.id AND ier.tenant_id = e.tenant_id
		             AND trim(COALESCE(ier.role_code, '')) <> ''
		           ORDER BY ier.id LIMIT 1
		       ), ''),
		       COALESCE(d.name, '')
		FROM employees e
		LEFT JOIN departments d ON d.id = e.department_id AND d.tenant_id = e.tenant_id AND d.deleted_at IS NULL
		WHERE e.tenant_id = $1 AND e.id = $2 AND e.deleted_at IS NULL
	`, tenantID, employeeID).Scan(&item.ID, &item.Name, &item.Email, &item.Role, &item.DepartmentName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load email employee: %w", err)
	}
	return &item, nil
}

// DefaultManager 返回 owner 的直属负责人；没有 manager_id 列或未配置时回退租户管理员。
func (s *pgEmailStore) DefaultManager(ctx context.Context, tenantID, ownerEmployeeID int64) (int64, error) {
	if s.hasManagerColumn(ctx) && ownerEmployeeID > 0 {
		var managerID int64
		err := s.pool.QueryRow(ctx, `
			SELECT COALESCE(manager_id, 0) FROM employees
			WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`, ownerEmployeeID, tenantID).Scan(&managerID)
		if err == nil && managerID > 0 {
			exists, err := s.EmployeesExist(ctx, tenantID, []int64{managerID})
			if err != nil {
				return 0, err
			}
			if exists {
				return managerID, nil
			}
		}
	}
	var adminID int64
	err := s.pool.QueryRow(ctx, `
		SELECT e.id
		FROM employees e
		WHERE e.tenant_id = $1 AND e.deleted_at IS NULL
		  AND (
		    e.role = 'admin'
		    OR EXISTS (
		      SELECT 1 FROM institution_employee_roles ier
		      WHERE ier.employee_id = e.id AND ier.tenant_id = e.tenant_id
		        AND lower(trim(ier.role_code)) = 'admin'
		    )
		  )
		ORDER BY CASE WHEN e.role = 'admin' THEN 0 ELSE 1 END, e.id
		LIMIT 1
	`, tenantID).Scan(&adminID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("resolve tenant admin: %w", err)
	}
	return adminID, nil
}

// hasManagerColumn 兼容历史库：manager_id 可能不存在（迁移里未声明），缺列时回退管理员。
func (s *pgEmailStore) hasManagerColumn(ctx context.Context) bool {
	s.managerColumnOnce.Do(func() {
		var exists bool
		err := s.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = current_schema()
				  AND table_name = 'employees' AND column_name = 'manager_id'
			)
		`).Scan(&exists)
		s.managerColumn = err == nil && exists
	})
	return s.managerColumn
}

func (s *pgEmailStore) Route(ctx context.Context, tenantID, ownerEmployeeID int64, eventType string) (*AlertRoute, error) {
	var item AlertRoute
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, owner_employee_id, event_type, to_employee_ids, cc_employee_ids,
		       created_by, updated_by, created_at, updated_at
		FROM deal_alert_routes
		WHERE tenant_id = $1 AND owner_employee_id = $2 AND event_type = $3
	`, tenantID, ownerEmployeeID, eventType).Scan(
		&item.ID, &item.TenantID, &item.OwnerEmployeeID, &item.EventType,
		&item.ToEmployeeIDs, &item.CcEmployeeIDs,
		&item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load alert route: %w", err)
	}
	return &item, nil
}

func (s *pgEmailStore) ListRoutes(ctx context.Context, tenantID, ownerEmployeeID int64) ([]*AlertRoute, error) {
	query := `
		SELECT r.id, r.tenant_id, r.owner_employee_id, r.event_type,
		       r.to_employee_ids, r.cc_employee_ids,
		       r.created_by, r.updated_by, r.created_at, r.updated_at,
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), ''),
		       COALESCE(e.email, '')
		FROM deal_alert_routes r
		LEFT JOIN employees e ON e.id = r.owner_employee_id AND e.tenant_id = r.tenant_id AND e.deleted_at IS NULL
		WHERE r.tenant_id = $1`
	args := []any{tenantID}
	if ownerEmployeeID > 0 {
		args = append(args, ownerEmployeeID)
		query += fmt.Sprintf(" AND r.owner_employee_id = $%d", len(args))
	}
	query += " ORDER BY r.owner_employee_id, r.event_type"
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list alert routes: %w", err)
	}
	defer rows.Close()
	items := make([]*AlertRoute, 0)
	for rows.Next() {
		var item AlertRoute
		if err := rows.Scan(&item.ID, &item.TenantID, &item.OwnerEmployeeID, &item.EventType,
			&item.ToEmployeeIDs, &item.CcEmployeeIDs,
			&item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt,
			&item.OwnerName, &item.OwnerEmail); err != nil {
			return nil, fmt.Errorf("scan alert route: %w", err)
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *pgEmailStore) ListEmployees(ctx context.Context, tenantID int64) ([]*EmailEmployee, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id,
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), ''),
		       COALESCE(e.email, ''),
		       COALESCE(NULLIF(e.role, ''), (
		           SELECT lower(trim(ier.role_code))
		           FROM institution_employee_roles ier
		           WHERE ier.employee_id = e.id AND ier.tenant_id = e.tenant_id
		             AND trim(COALESCE(ier.role_code, '')) <> ''
		           ORDER BY ier.id LIMIT 1
		       ), ''),
		       COALESCE(d.name, '')
		FROM employees e
		LEFT JOIN departments d ON d.id = e.department_id AND d.tenant_id = e.tenant_id AND d.deleted_at IS NULL
		WHERE e.tenant_id = $1 AND e.deleted_at IS NULL
		ORDER BY e.id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list alert employees: %w", err)
	}
	defer rows.Close()
	items := make([]*EmailEmployee, 0)
	for rows.Next() {
		var item EmailEmployee
		if err := rows.Scan(&item.ID, &item.Name, &item.Email, &item.Role, &item.DepartmentName); err != nil {
			return nil, fmt.Errorf("scan alert employee: %w", err)
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *pgEmailStore) UpsertRoute(ctx context.Context, route *AlertRoute) (*AlertRoute, error) {
	if route == nil {
		return nil, fmt.Errorf("route is required")
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO deal_alert_routes
		  (tenant_id, owner_employee_id, event_type, to_employee_ids, cc_employee_ids, created_by, updated_by)
		VALUES ($1,$2,$3,$4::bigint[],$5::bigint[],$6,$7)
		ON CONFLICT (tenant_id, owner_employee_id, event_type)
		DO UPDATE SET to_employee_ids = EXCLUDED.to_employee_ids,
		              cc_employee_ids = EXCLUDED.cc_employee_ids,
		              updated_by = EXCLUDED.updated_by,
		              updated_at = NOW()
		RETURNING id, created_at, updated_at
	`, route.TenantID, route.OwnerEmployeeID, route.EventType,
		route.ToEmployeeIDs, route.CcEmployeeIDs, route.CreatedBy, route.UpdatedBy).Scan(
		&route.ID, &route.CreatedAt, &route.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert alert route: %w", err)
	}
	return route, nil
}

func (s *pgEmailStore) EmployeesExist(ctx context.Context, tenantID int64, employeeIDs []int64) (bool, error) {
	if len(employeeIDs) == 0 {
		return true, nil
	}
	var count int
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM employees
		WHERE tenant_id = $1 AND deleted_at IS NULL AND id = ANY($2::bigint[])
	`, tenantID, employeeIDs).Scan(&count); err != nil {
		return false, fmt.Errorf("check alert employees: %w", err)
	}
	return count == len(uniquePositive(employeeIDs)), nil
}

func (s *pgEmailStore) Delivery(ctx context.Context, eventID, employeeID int64) (*EmailDelivery, error) {
	var item EmailDelivery
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, event_id, recipient_employee_id, email, kind, status,
		       message_id, error, sent_at, created_at
		FROM deal_email_deliveries
		WHERE event_id = $1 AND recipient_employee_id = $2
	`, eventID, employeeID).Scan(&item.ID, &item.TenantID, &item.EventID, &item.RecipientEmployeeID,
		&item.Email, &item.Kind, &item.Status, &item.MessageID, &item.Error, &item.SentAt, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load email delivery: %w", err)
	}
	return &item, nil
}

// SaveDelivery 写入投递结果；已 sent 的行不被覆盖，保证重试幂等。
func (s *pgEmailStore) SaveDelivery(ctx context.Context, delivery *EmailDelivery) error {
	if delivery == nil {
		return fmt.Errorf("delivery is required")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO deal_email_deliveries
		  (tenant_id, event_id, recipient_employee_id, email, kind, status, message_id, error, sent_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (event_id, recipient_employee_id)
		DO UPDATE SET email = EXCLUDED.email,
		              kind = EXCLUDED.kind,
		              status = EXCLUDED.status,
		              message_id = EXCLUDED.message_id,
		              error = EXCLUDED.error,
		              sent_at = EXCLUDED.sent_at
		WHERE deal_email_deliveries.status <> 'sent'
	`, delivery.TenantID, delivery.EventID, delivery.RecipientEmployeeID,
		delivery.Email, delivery.Kind, delivery.Status, delivery.MessageID,
		truncateString(delivery.Error, 500), delivery.SentAt)
	if err != nil {
		return fmt.Errorf("save email delivery: %w", err)
	}
	return nil
}

// EnsureShortLink 复用该员工在该录音下未过期的短链；没有则新建 7 天有效的短链。
func (s *pgEmailStore) EnsureShortLink(ctx context.Context, tenantID, recordingID, employeeID int64) (string, error) {
	var code string
	err := s.pool.QueryRow(ctx, `
		SELECT code FROM deal_short_links
		WHERE tenant_id = $1 AND recording_id = $2 AND employee_id = $3 AND expires_at > NOW()
		ORDER BY created_at DESC LIMIT 1
	`, tenantID, recordingID, employeeID).Scan(&code)
	if err == nil && strings.TrimSpace(code) != "" {
		return code, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("load short link: %w", err)
	}
	for attempt := 0; attempt < 6; attempt++ {
		candidate := newShortCode()
		_, err := s.pool.Exec(ctx, `
			INSERT INTO deal_short_links (code, tenant_id, recording_id, employee_id, expires_at)
			VALUES ($1,$2,$3,$4,$5)
		`, candidate, tenantID, recordingID, employeeID, time.Now().Add(ShortLinkTTL))
		if err == nil {
			return candidate, nil
		}
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			continue
		}
		return "", fmt.Errorf("insert short link: %w", err)
	}
	return "", fmt.Errorf("short code collision after retries")
}
