package badge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SetTicketResolved 受理工单并写入处理方式：远程解决（completed）或不处理（rejected）。
// 处理方式复用 resolution_type，处理时间复用 executed_at，不新增列。
func (s *Store) SetTicketResolved(ctx context.Context, id int64, status, method, notes string, executorID int64) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE badge_tickets
		SET status = $2, resolution_type = $3, executor_id = $4,
		    execute_notes = NULLIF($5, ''), executed_at = NOW(),
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
		SET status = 'completed', resolution_type = 'swap', executor_id = $2,
		    swap_device_id = $3, swap_device_no = NULLIF($4, ''),
		    outbound_tracking_no = NULLIF($5, ''), inbound_tracking_no = NULLIF($6, ''),
		    recipient_name = COALESCE(NULLIF($7, ''), recipient_name),
		    recipient_phone = COALESCE(NULLIF($8, ''), recipient_phone),
		    recipient_address = COALESCE(NULLIF($9, ''), recipient_address),
		    execute_notes = NULLIF($10, ''), executed_at = NOW(), completed_at = NOW(),
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

// ReopenTicket 重新打开已关闭/不处理的工单。
func (s *Store) ReopenTicket(ctx context.Context, id, operatorID int64) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE badge_tickets
		SET status = 'pending', resolution_type = NULL, executor_id = NULL,
		    executed_at = NULL, completed_at = NULL, execute_notes = NULL, updated_at = NOW()
		WHERE id = $1 AND status IN ('closed', 'rejected')
	`, id)
	if err != nil {
		return fmt.Errorf("failed to reopen ticket: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("只有已关闭或不处理的工单才能重新打开")
	}
	return nil
}

// SetTicketReturnTracking 登记旧机寄回单号（内部进度，不改变工单完成状态）。
func (s *Store) SetTicketReturnTracking(ctx context.Context, id int64, inboundTracking string, operatorID int64) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE badge_tickets
		SET inbound_tracking_no = NULLIF($2, ''),
		    extra_data = COALESCE(extra_data, '{}'::jsonb) || jsonb_build_object(
		        'return_recorded_at', to_char(NOW() AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD"T"HH24:MI:SS'),
		        'return_recorded_by', $3::bigint
		    ),
		    updated_at = NOW()
		WHERE id = $1 AND resolution_type = 'swap'
	`, id, strings.TrimSpace(inboundTracking), operatorID)
	if err != nil {
		return fmt.Errorf("failed to record return tracking: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("只有换新工单才能登记旧机寄回")
	}
	return nil
}

// MarkTicketCustomerNotified 把「已发送给客户」写进 extra_data，不再新增列。
func (s *Store) MarkTicketCustomerNotified(ctx context.Context, id, operatorID int64) error {
	payload, err := json.Marshal(map[string]any{
		"customer_notified_at": time.Now().Format("2006-01-02T15:04:05"),
		"customer_notified_by": operatorID,
	})
	if err != nil {
		return fmt.Errorf("failed to build notify payload: %w", err)
	}
	result, err := s.pool.Exec(ctx, `
		UPDATE badge_tickets
		SET extra_data = COALESCE(extra_data, '{}'::jsonb) || $2::jsonb, updated_at = NOW()
		WHERE id = $1 AND resolution_type = 'swap'
	`, id, string(payload))
	if err != nil {
		return fmt.Errorf("failed to mark customer notified: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("只有换新工单才能标记已发送给客户")
	}
	return nil
}
