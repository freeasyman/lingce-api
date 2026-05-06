package badge

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CreateTicket creates a ticket.
func (s *Store) CreateTicket(ctx context.Context, submitterID int64, req TicketSubmitRequest) (*BadgeTicket, error) {
	ticketTenantID := int64(0)
	if req.TenantID != nil && *req.TenantID > 0 {
		ticketTenantID = *req.TenantID
	} else {
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(
				(SELECT bd.tenant_id FROM badge_devices bd WHERE bd.id = $1::bigint AND bd.deleted_at IS NULL LIMIT 1),
				(SELECT bd.tenant_id FROM badge_devices bd WHERE bd.device_no = $2::text AND bd.deleted_at IS NULL ORDER BY bd.updated_at DESC LIMIT 1),
				0
			)
		`, req.DeviceID, req.DeviceNo).Scan(&ticketTenantID)
	}
	ticketTenantCode, err := s.getTenantCode(ctx, ticketTenantID)
	if err != nil {
		return nil, err
	}

	query := `
		INSERT INTO badge_tickets (ticket_no, type, status, device_id, device_no, tenant_id, employee_id, title, description,
		                           submitter_id, extra_data, submitted_at, created_at, updated_at)
		VALUES (
		    $1, $2, 'pending', $3::bigint,
		    COALESCE($4::text, (SELECT bd.device_no FROM badge_devices bd WHERE bd.id = $3::bigint AND bd.deleted_at IS NULL LIMIT 1)),
		    COALESCE(
		      $5::bigint,
		      (SELECT bd.tenant_id FROM badge_devices bd WHERE bd.id = $3::bigint AND bd.deleted_at IS NULL LIMIT 1),
		      (SELECT bd.tenant_id FROM badge_devices bd WHERE bd.device_no = $4::text AND bd.deleted_at IS NULL ORDER BY bd.updated_at DESC LIMIT 1)
		    ),
		    COALESCE(
		      (SELECT bd.employee_id FROM badge_devices bd WHERE bd.id = $3::bigint AND bd.deleted_at IS NULL LIMIT 1),
		      (SELECT bd.employee_id FROM badge_devices bd WHERE bd.device_no = $4::text AND bd.deleted_at IS NULL ORDER BY bd.updated_at DESC LIMIT 1)
		    ),
		    $6, $7, $8, $9, NOW(), NOW(), NOW()
		)
		RETURNING id, ticket_no, type, status, device_id, device_no, tenant_id, employee_id,
		          submitter_id, reviewer_id, executor_id, title, description, review_notes, execute_notes,
		          submitted_at, reviewed_at, executed_at, completed_at, extra_data, created_at, updated_at
	`

	for i := 0; i < 5; i++ {
		ticketNo, err := generateTicketNo(ticketTenantCode)
		if err != nil {
			return nil, err
		}

		var t BadgeTicket
		err = s.pool.QueryRow(ctx, query, ticketNo, req.Type, req.DeviceID, req.DeviceNo, req.TenantID, req.Title,
			req.Description, submitterID, req.ExtraData).Scan(
			&t.ID, &t.TicketNo, &t.Type, &t.Status, &t.DeviceID, &t.DeviceNo, &t.TenantID, &t.EmployeeID,
			&t.SubmitterID, &t.ReviewerID, &t.ExecutorID, &t.Title, &t.Description, &t.ReviewNotes,
			&t.ExecuteNotes, &t.SubmittedAt, &t.ReviewedAt, &t.ExecutedAt, &t.CompletedAt,
			&t.ExtraData, &t.CreatedAt, &t.UpdatedAt,
		)
		if err == nil {
			full, getErr := s.GetTicketByID(ctx, t.ID)
			if getErr == nil {
				return full, nil
			}
			return &t, nil
		}

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			continue
		}
		return nil, fmt.Errorf("failed to create ticket: %w", err)
	}

	return nil, fmt.Errorf("failed to create ticket: exhausted ticket_no retries")
}

var tenantCodeSanitizer = regexp.MustCompile(`[^A-Z0-9]+`)

func generateTicketNo(tenantCode string) (string, error) {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	const suffixLen = 4
	buf := make([]byte, suffixLen)
	randBytes := make([]byte, suffixLen)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("failed to generate ticket suffix: %w", err)
	}
	for i, b := range randBytes {
		buf[i] = charset[int(b)%len(charset)]
	}
	normalizedCode := normalizeTenantCode(tenantCode)
	return fmt.Sprintf("TK-%s-%s-%s", normalizedCode, time.Now().Format("20060102"), string(buf)), nil
}

func normalizeTenantCode(raw string) string {
	code := strings.ToUpper(strings.TrimSpace(raw))
	code = tenantCodeSanitizer.ReplaceAllString(code, "")
	if code == "" {
		return "UNKNOWN"
	}
	return code
}

func (s *Store) getTenantCode(ctx context.Context, tenantID int64) (string, error) {
	if tenantID <= 0 {
		return "UNKNOWN", nil
	}
	var code string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(NULLIF(code, ''), 'UNKNOWN') FROM tenants WHERE id = $1 AND deleted_at IS NULL`, tenantID).Scan(&code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "UNKNOWN", nil
		}
		return "", fmt.Errorf("failed to query tenant code: %w", err)
	}
	return code, nil
}

// ListTickets retrieves a paginated list of tickets.
func (s *Store) ListTickets(ctx context.Context, req TicketListRequest) ([]*BadgeTicket, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	if req.Type != nil {
		conditions = append(conditions, fmt.Sprintf("type = $%d", argIndex))
		args = append(args, *req.Type)
		argIndex++
	}
	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}
	if req.SubmitterID != nil {
		conditions = append(conditions, fmt.Sprintf("submitter_id = $%d", argIndex))
		args = append(args, *req.SubmitterID)
		argIndex++
	}
	if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf(`COALESCE(
			badge_tickets.tenant_id,
			(SELECT bd.tenant_id FROM badge_devices bd WHERE bd.id = badge_tickets.device_id AND bd.deleted_at IS NULL LIMIT 1),
			(SELECT bd.tenant_id FROM badge_devices bd WHERE bd.device_no = badge_tickets.device_no AND bd.deleted_at IS NULL ORDER BY bd.updated_at DESC LIMIT 1)
		) = $%d`, argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	whereClause := "1=1"
	if len(conditions) > 0 {
		whereClause = strings.Join(conditions, " AND ")
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM badge_tickets WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count tickets: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT badge_tickets.id, badge_tickets.ticket_no, badge_tickets.type, badge_tickets.status, badge_tickets.device_id, badge_tickets.device_no, badge_tickets.tenant_id, badge_tickets.employee_id,
		       badge_tickets.submitter_id, badge_tickets.reviewer_id, badge_tickets.executor_id, badge_tickets.title, badge_tickets.description, badge_tickets.review_notes, badge_tickets.execute_notes,
		       badge_tickets.submitted_at, badge_tickets.reviewed_at, badge_tickets.executed_at, badge_tickets.completed_at, badge_tickets.extra_data, badge_tickets.created_at, badge_tickets.updated_at,
		       t.name AS tenant_name,
		       emp.name AS employee_name,
		       COALESCE(submitter_emp.name, submitter_admin.name, submitter_patient.name, '') AS submitter_name,
		       COALESCE(reviewer_emp.name, reviewer_admin.name) AS reviewer_name,
		       COALESCE(executor_emp.name, executor_admin.name) AS executor_name
		FROM badge_tickets
		LEFT JOIN tenants t ON t.id = badge_tickets.tenant_id
		LEFT JOIN employees emp ON emp.id = badge_tickets.employee_id AND emp.deleted_at IS NULL
		LEFT JOIN employees submitter_emp ON submitter_emp.id = badge_tickets.submitter_id AND submitter_emp.deleted_at IS NULL
		LEFT JOIN operations_admins submitter_admin ON submitter_admin.id = badge_tickets.submitter_id AND submitter_admin.deleted_at IS NULL
		LEFT JOIN user_master submitter_patient ON submitter_patient.user_id = badge_tickets.submitter_id
		LEFT JOIN employees reviewer_emp ON reviewer_emp.id = badge_tickets.reviewer_id AND reviewer_emp.deleted_at IS NULL
		LEFT JOIN operations_admins reviewer_admin ON reviewer_admin.id = badge_tickets.reviewer_id AND reviewer_admin.deleted_at IS NULL
		LEFT JOIN employees executor_emp ON executor_emp.id = badge_tickets.executor_id AND executor_emp.deleted_at IS NULL
		LEFT JOIN operations_admins executor_admin ON executor_admin.id = badge_tickets.executor_id AND executor_admin.deleted_at IS NULL
		WHERE %s
		ORDER BY badge_tickets.created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)
	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query tickets: %w", err)
	}
	defer rows.Close()

	var tickets []*BadgeTicket
	for rows.Next() {
		var t BadgeTicket
		if err := rows.Scan(&t.ID, &t.TicketNo, &t.Type, &t.Status, &t.DeviceID, &t.DeviceNo,
			&t.TenantID, &t.EmployeeID, &t.SubmitterID, &t.ReviewerID, &t.ExecutorID,
			&t.Title, &t.Description, &t.ReviewNotes, &t.ExecuteNotes, &t.SubmittedAt,
			&t.ReviewedAt, &t.ExecutedAt, &t.CompletedAt, &t.ExtraData, &t.CreatedAt, &t.UpdatedAt,
			&t.TenantName, &t.EmployeeName, &t.SubmitterName, &t.ReviewerName, &t.ExecutorName); err != nil {
			return nil, 0, fmt.Errorf("failed to scan ticket: %w", err)
		}
		tickets = append(tickets, &t)
	}

	return tickets, total, nil
}

// GetTicketByID retrieves a ticket by ID.
func (s *Store) GetTicketByID(ctx context.Context, id int64) (*BadgeTicket, error) {
	query := `
		SELECT badge_tickets.id, badge_tickets.ticket_no, badge_tickets.type, badge_tickets.status, badge_tickets.device_id, badge_tickets.device_no, badge_tickets.tenant_id, badge_tickets.employee_id,
		       badge_tickets.submitter_id, badge_tickets.reviewer_id, badge_tickets.executor_id, badge_tickets.title, badge_tickets.description, badge_tickets.review_notes, badge_tickets.execute_notes,
		       badge_tickets.submitted_at, badge_tickets.reviewed_at, badge_tickets.executed_at, badge_tickets.completed_at, badge_tickets.extra_data, badge_tickets.created_at, badge_tickets.updated_at,
		       t.name AS tenant_name,
		       emp.name AS employee_name,
		       COALESCE(submitter_emp.name, submitter_admin.name, submitter_patient.name, '') AS submitter_name,
		       COALESCE(reviewer_emp.name, reviewer_admin.name) AS reviewer_name,
		       COALESCE(executor_emp.name, executor_admin.name) AS executor_name
		FROM badge_tickets
		LEFT JOIN tenants t ON t.id = badge_tickets.tenant_id
		LEFT JOIN employees emp ON emp.id = badge_tickets.employee_id AND emp.deleted_at IS NULL
		LEFT JOIN employees submitter_emp ON submitter_emp.id = badge_tickets.submitter_id AND submitter_emp.deleted_at IS NULL
		LEFT JOIN operations_admins submitter_admin ON submitter_admin.id = badge_tickets.submitter_id AND submitter_admin.deleted_at IS NULL
		LEFT JOIN user_master submitter_patient ON submitter_patient.user_id = badge_tickets.submitter_id
		LEFT JOIN employees reviewer_emp ON reviewer_emp.id = badge_tickets.reviewer_id AND reviewer_emp.deleted_at IS NULL
		LEFT JOIN operations_admins reviewer_admin ON reviewer_admin.id = badge_tickets.reviewer_id AND reviewer_admin.deleted_at IS NULL
		LEFT JOIN employees executor_emp ON executor_emp.id = badge_tickets.executor_id AND executor_emp.deleted_at IS NULL
		LEFT JOIN operations_admins executor_admin ON executor_admin.id = badge_tickets.executor_id AND executor_admin.deleted_at IS NULL
		WHERE badge_tickets.id = $1
	`

	var t BadgeTicket
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.TicketNo, &t.Type, &t.Status, &t.DeviceID, &t.DeviceNo,
		&t.TenantID, &t.EmployeeID, &t.SubmitterID, &t.ReviewerID, &t.ExecutorID,
		&t.Title, &t.Description, &t.ReviewNotes, &t.ExecuteNotes, &t.SubmittedAt,
		&t.ReviewedAt, &t.ExecutedAt, &t.CompletedAt, &t.ExtraData, &t.CreatedAt, &t.UpdatedAt,
		&t.TenantName, &t.EmployeeName, &t.SubmitterName, &t.ReviewerName, &t.ExecutorName,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("ticket not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query ticket: %w", err)
	}
	return &t, nil
}

// ReviewTicket reviews a ticket.
func (s *Store) ReviewTicket(ctx context.Context, id, reviewerID int64, approved bool, notes *string) error {
	status := "approved"
	if !approved {
		status = "rejected"
	}

	query := `
		UPDATE badge_tickets
		SET status = $1, reviewer_id = $2, review_notes = $3, reviewed_at = NOW(), updated_at = NOW()
		WHERE id = $4
	`
	result, err := s.pool.Exec(ctx, query, status, reviewerID, notes, id)
	if err != nil {
		return fmt.Errorf("failed to review ticket: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("ticket not found")
	}
	return nil
}

// ExecuteTicket executes a ticket.
func (s *Store) ExecuteTicket(ctx context.Context, id, executorID int64, success bool, notes *string) error {
	status := "completed"
	if !success {
		status = "failed"
	}

	query := `
		UPDATE badge_tickets
		SET status = $1, executor_id = $2, execute_notes = $3, executed_at = NOW(),
		    completed_at = NOW(), updated_at = NOW()
		WHERE id = $4
	`
	result, err := s.pool.Exec(ctx, query, status, executorID, notes, id)
	if err != nil {
		return fmt.Errorf("failed to execute ticket: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("ticket not found")
	}
	return nil
}
