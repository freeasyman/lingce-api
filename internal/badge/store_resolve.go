package badge

import (
	"context"
	"fmt"
)

// SetTicketResolved 受理工单并写入处理方式：远程解决（completed）或不处理（rejected）。
func (s *Store) SetTicketResolved(ctx context.Context, id int64, status, method, notes string, executorID int64) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE badge_tickets
		SET status = $2, resolution = $3, executor_id = $4,
		    execute_notes = NULLIF($5, ''), executed_at = NOW(), resolved_at = NOW(),
		    completed_at = CASE WHEN $2 = 'completed' THEN NOW() ELSE completed_at END,
		    updated_at = NOW()
		WHERE id = $1 AND status IN ('pending', 'progress')
	`, id, status, method, executorID, notes)
	if err != nil {
		return fmt.Errorf("failed to resolve ticket: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("工单已处理或状态已变化，请刷新后重试")
	}
	return nil
}

// SetTicketSwapped 记录换新结果：工单进入「待寄回」，同时保存设备号与物流信息。
func (s *Store) SetTicketSwapped(ctx context.Context, id int64, swapDeviceID *int64, swapDeviceNo, outboundTracking, inboundTracking, recipientName, recipientPhone, recipientAddress, notes string, executorID int64) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE badge_tickets
		SET status = 'waiting', resolution = 'swap', executor_id = $2,
		    resolution_type = 'our_issue',
		    swap_device_id = $3, swap_device_no = NULLIF($4, ''),
		    outbound_tracking_no = NULLIF($5, ''), inbound_tracking_no = NULLIF($6, ''),
		    recipient_name = COALESCE(NULLIF($7, ''), recipient_name),
		    recipient_phone = COALESCE(NULLIF($8, ''), recipient_phone),
		    recipient_address = COALESCE(NULLIF($9, ''), recipient_address),
		    execute_notes = NULLIF($10, ''), executed_at = NOW(), resolved_at = NOW(),
		    shipped_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status IN ('pending', 'progress')
	`, id, executorID, swapDeviceID, swapDeviceNo, outboundTracking, inboundTracking, recipientName, recipientPhone, recipientAddress, notes)
	if err != nil {
		return fmt.Errorf("failed to save swap: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("工单已处理或状态已变化，请刷新后重试")
	}
	return nil
}

// MarkTicketCustomerNotified 记录运营已把通知单发给客户（微信/企业微信）。
func (s *Store) MarkTicketCustomerNotified(ctx context.Context, id, operatorID int64) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE badge_tickets
		SET customer_notified_at = NOW(), customer_notified_by = $2, updated_at = NOW()
		WHERE id = $1 AND resolution = 'swap'
	`, id, operatorID)
	if err != nil {
		return fmt.Errorf("failed to mark customer notified: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("只有换新工单才能标记已发送给客户")
	}
	return nil
}
