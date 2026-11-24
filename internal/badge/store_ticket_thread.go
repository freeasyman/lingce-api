package badge

import (
	"context"
	"fmt"
	"time"
)

// AddTicketMessage 追加一条工单消息（回复客户/客户留言）。
func (s *Store) AddTicketMessage(ctx context.Context, ticketID int64, authorType string, authorID *int64, authorName, body string, emailedAt *time.Time) (*BadgeTicketMessage, error) {
	var msg BadgeTicketMessage
	err := s.pool.QueryRow(ctx, `
		INSERT INTO badge_ticket_messages (ticket_id, author_type, author_id, author_name, body, emailed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, ticket_id, author_type, author_id, author_name, body, emailed_at, created_at
	`, ticketID, authorType, authorID, authorName, body, emailedAt).Scan(
		&msg.ID, &msg.TicketID, &msg.AuthorType, &msg.AuthorID, &msg.AuthorName, &msg.Body, &msg.EmailedAt, &msg.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("add ticket message: %w", err)
	}
	return &msg, nil
}

// ListTicketMessages 返回工单的全部往来消息（按时间正序）。
func (s *Store) ListTicketMessages(ctx context.Context, ticketID int64) ([]*BadgeTicketMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, ticket_id, author_type, author_id, author_name, body, emailed_at, created_at
		FROM badge_ticket_messages WHERE ticket_id = $1 ORDER BY id
	`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("list ticket messages: %w", err)
	}
	defer rows.Close()
	items := make([]*BadgeTicketMessage, 0)
	for rows.Next() {
		var msg BadgeTicketMessage
		if err := rows.Scan(&msg.ID, &msg.TicketID, &msg.AuthorType, &msg.AuthorID, &msg.AuthorName, &msg.Body, &msg.EmailedAt, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ticket message: %w", err)
		}
		items = append(items, &msg)
	}
	return items, rows.Err()
}

// SetTicketReplacement 记录换货留痕：换出的设备号与物流单号。
func (s *Store) SetTicketReplacement(ctx context.Context, ticketID int64, resolutionType string, swapDeviceID *int64, swapDeviceNo, outboundTracking, inboundTracking, recipientName, recipientPhone, recipientAddress string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE badge_tickets
		SET resolution_type = $2, swap_device_id = $3, swap_device_no = NULLIF($4, ''),
		    outbound_tracking_no = NULLIF($5, ''), inbound_tracking_no = NULLIF($6, ''),
		    recipient_name = COALESCE(NULLIF($7, ''), recipient_name),
		    recipient_phone = COALESCE(NULLIF($8, ''), recipient_phone),
		    recipient_address = COALESCE(NULLIF($9, ''), recipient_address),
		    shipped_at = CASE WHEN NULLIF($4, '') IS NULL AND NULLIF($5, '') IS NULL THEN shipped_at ELSE NOW() END,
		    updated_at = NOW()
		WHERE id = $1
	`, ticketID, resolutionType, swapDeviceID, swapDeviceNo, outboundTracking, inboundTracking, recipientName, recipientPhone, recipientAddress)
	if err != nil {
		return fmt.Errorf("record ticket replacement: %w", err)
	}
	return nil
}

// TicketContactEmail 取工单的客户联系邮箱：提交人优先，其次机构联系人邮箱。
func (s *Store) TicketContactEmail(ctx context.Context, ticket *BadgeTicket) (string, error) {
	if ticket == nil {
		return "", nil
	}
	if ticket.SubmitterID > 0 {
		var email string
		if err := s.pool.QueryRow(ctx, `
			SELECT COALESCE(NULLIF(trim(email), ''), '') FROM employees
			WHERE id = $1 AND deleted_at IS NULL
		`, ticket.SubmitterID).Scan(&email); err == nil && email != "" {
			return email, nil
		}
	}
	if ticket.TenantID != nil && *ticket.TenantID > 0 {
		var email string
		if err := s.pool.QueryRow(ctx, `
			SELECT COALESCE(NULLIF(trim(contact_email), ''), '') FROM tenants WHERE id = $1
		`, *ticket.TenantID).Scan(&email); err == nil && email != "" {
			return email, nil
		}
	}
	return "", nil
}
