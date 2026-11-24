package badge

import (
	"context"
	"fmt"
	"strings"
)

// ResolveTicket 受理工单：一次性选择处理方式（换新 / 远程解决 / 不处理）。
func (s *Service) ResolveTicket(ctx context.Context, id, operatorID int64, req TicketResolveRequest) error {
	method := strings.TrimSpace(req.Method)
	ticket, err := s.store.GetTicketByID(ctx, id)
	if err != nil {
		return err
	}
	if ticket.Status != "pending" && ticket.Status != "progress" {
		return fmt.Errorf("工单已处理或状态已变化，请刷新后重试")
	}

	switch method {
	case "swap":
		if ticket.DeviceID == nil || *ticket.DeviceID <= 0 {
			return fmt.Errorf("工单没有关联设备，无法换新")
		}
		if req.SwapDeviceID == nil || *req.SwapDeviceID <= 0 {
			return fmt.Errorf("请选择换上的设备")
		}
		if strings.TrimSpace(req.OutboundTracking) == "" {
			return fmt.Errorf("请填写寄出物流单号")
		}
		recipientAddress := strings.TrimSpace(req.RecipientAddress)
		if recipientAddress == "" {
			recipientAddress = derefString(ticket.RecipientAddress)
		}
		if recipientAddress == "" {
			return fmt.Errorf("请填写收件地址（寄新机用）")
		}
		tenantID := int64(0)
		if ticket.TenantID != nil {
			tenantID = *ticket.TenantID
		}
		device, err := s.store.RebuildSwapBadgeDevices(ctx, *ticket.DeviceID, *req.SwapDeviceID, "报修换新", operatorID, "", &tenantID)
		if err != nil {
			return err
		}
		swapDeviceNo := ""
		if device != nil {
			swapDeviceNo = device.DeviceNo
		}
		return s.store.SetTicketSwapped(ctx, id, req.SwapDeviceID, swapDeviceNo, req.OutboundTracking, req.InboundTracking,
			strings.TrimSpace(req.RecipientName), strings.TrimSpace(req.RecipientPhone), recipientAddress, strings.TrimSpace(req.Note), operatorID)

	case "remote":
		if strings.TrimSpace(req.Note) == "" {
			return fmt.Errorf("请填写远程处理说明")
		}
		return s.store.SetTicketResolved(ctx, id, "completed", "remote", req.Note, operatorID)

	case "none":
		if strings.TrimSpace(req.Note) == "" {
			return fmt.Errorf("请填写不处理原因")
		}
		return s.store.SetTicketResolved(ctx, id, "rejected", "none", req.Note, operatorID)

	case "close":
		if strings.TrimSpace(req.Note) == "" {
			return fmt.Errorf("请选择关闭原因")
		}
		return s.store.SetTicketResolved(ctx, id, "closed", "closed", req.Note, operatorID)

	default:
		return fmt.Errorf("未知的处理方式")
	}
}

// ReopenTicket 把已关闭/不处理的工单重新打开到待受理。
func (s *Service) ReopenTicket(ctx context.Context, id, operatorID int64) error {
	return s.store.ReopenTicket(ctx, id, operatorID)
}

// RecordReturnTracking 登记旧机寄回单号（内部进度，不影响工单完成）。
func (s *Service) RecordReturnTracking(ctx context.Context, id, operatorID int64, inboundTracking string) error {
	if strings.TrimSpace(inboundTracking) == "" {
		return fmt.Errorf("请填写旧机寄回单号")
	}
	return s.store.SetTicketReturnTracking(ctx, id, inboundTracking, operatorID)
}

// MarkCustomerNotified 记录换新通知单已由运营发送给客户。
func (s *Service) MarkCustomerNotified(ctx context.Context, id, operatorID int64) error {
	return s.store.MarkTicketCustomerNotified(ctx, id, operatorID)
}
