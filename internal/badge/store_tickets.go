package badge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// CreateTicket creates a ticket.
func (s *Store) CreateTicket(ctx context.Context, submitterID int64, req TicketSubmitRequest) (*BadgeTicket, error) {
	ticketNo := fmt.Sprintf("TK%d", time.Now().Unix())

	query := `
		INSERT INTO badge_tickets (ticket_no, type, status, device_id, device_no, title, description,
		                           submitter_id, extra_data, submitted_at, created_at, updated_at)
		VALUES ($1, $2, 'pending', $3, $4, $5, $6, $7, $8, NOW(), NOW(), NOW())
		RETURNING id, ticket_no, type, status, device_id, device_no, tenant_id, employee_id,
		          submitter_id, reviewer_id, executor_id, title, description, review_notes, execute_notes,
		          submitted_at, reviewed_at, executed_at, completed_at, extra_data, created_at, updated_at
	`

	var t BadgeTicket
	err := s.pool.QueryRow(ctx, query, ticketNo, req.Type, req.DeviceID, req.DeviceNo, req.Title,
		req.Description, submitterID, req.ExtraData).Scan(
		&t.ID, &t.TicketNo, &t.Type, &t.Status, &t.DeviceID, &t.DeviceNo, &t.TenantID, &t.EmployeeID,
		&t.SubmitterID, &t.ReviewerID, &t.ExecutorID, &t.Title, &t.Description, &t.ReviewNotes,
		&t.ExecuteNotes, &t.SubmittedAt, &t.ReviewedAt, &t.ExecutedAt, &t.CompletedAt,
		&t.ExtraData, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ticket: %w", err)
	}

	return &t, nil
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
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
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
		SELECT id, ticket_no, type, status, device_id, device_no, tenant_id, employee_id,
		       submitter_id, reviewer_id, executor_id, title, description, review_notes, execute_notes,
		       submitted_at, reviewed_at, executed_at, completed_at, extra_data, created_at, updated_at
		FROM badge_tickets
		WHERE %s
		ORDER BY created_at DESC
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
			&t.ReviewedAt, &t.ExecutedAt, &t.CompletedAt, &t.ExtraData, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan ticket: %w", err)
		}
		tickets = append(tickets, &t)
	}

	return tickets, total, nil
}

// GetTicketByID retrieves a ticket by ID.
func (s *Store) GetTicketByID(ctx context.Context, id int64) (*BadgeTicket, error) {
	query := `
		SELECT id, ticket_no, type, status, device_id, device_no, tenant_id, employee_id,
		       submitter_id, reviewer_id, executor_id, title, description, review_notes, execute_notes,
		       submitted_at, reviewed_at, executed_at, completed_at, extra_data, created_at, updated_at
		FROM badge_tickets
		WHERE id = $1
	`

	var t BadgeTicket
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.TicketNo, &t.Type, &t.Status, &t.DeviceID, &t.DeviceNo,
		&t.TenantID, &t.EmployeeID, &t.SubmitterID, &t.ReviewerID, &t.ExecutorID,
		&t.Title, &t.Description, &t.ReviewNotes, &t.ExecuteNotes, &t.SubmittedAt,
		&t.ReviewedAt, &t.ExecutedAt, &t.CompletedAt, &t.ExtraData, &t.CreatedAt, &t.UpdatedAt,
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
func (s *Store) ExecuteTicket(ctx context.Context, id, executorID int64, notes *string) error {
	query := `
		UPDATE badge_tickets
		SET status = 'completed', executor_id = $1, execute_notes = $2, executed_at = NOW(),
		    completed_at = NOW(), updated_at = NOW()
		WHERE id = $3
	`
	result, err := s.pool.Exec(ctx, query, executorID, notes, id)
	if err != nil {
		return fmt.Errorf("failed to execute ticket: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("ticket not found")
	}
	return nil
}
