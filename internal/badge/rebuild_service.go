package badge

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) RebuildListBadgeDevices(ctx context.Context, req RebuildBadgeDeviceListRequest) ([]*BadgeDeviceV2, int, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
	if req.BadgeStatus != nil {
		status := normalizeBadgeStatus(*req.BadgeStatus)
		if status == "" {
			return nil, 0, fmt.Errorf("invalid badge_status")
		}
		req.BadgeStatus = &status
	}
	if req.HealthLevel != nil {
		level, ok := parseHealthStatusFilter(*req.HealthLevel)
		if !ok {
			return nil, 0, fmt.Errorf("invalid health_level")
		}
		req.HealthLevel = &level
	}
	return s.store.RebuildListBadgeDevices(ctx, req)
}

func (s *Service) RebuildGetBadgeDeviceByID(ctx context.Context, id int64) (*BadgeDeviceV2, error) {
	return s.store.RebuildGetBadgeDeviceByID(ctx, id)
}

func (s *Service) RebuildListPendingAcceptanceDevices(ctx context.Context, req RebuildBadgeDeviceListRequest) ([]*BadgeDeviceV2, int, error) {
	status := BadgeStatusPendingAcceptance
	req.BadgeStatus = &status
	return s.RebuildListBadgeDevices(ctx, req)
}

func (s *Service) RebuildListMonitoringDevices(ctx context.Context, req RebuildBadgeDeviceListRequest) ([]*BadgeDeviceV2, int, error) {
	return s.RebuildListBadgeDevices(ctx, req)
}

func (s *Service) RebuildListBadgeDeviceLogs(ctx context.Context, id int64) ([]*BadgeDeviceLogV2, error) {
	return s.store.RebuildListBadgeDeviceLogs(ctx, id)
}

func (s *Service) RebuildListBadgeAssignmentLogs(ctx context.Context, id int64) ([]*BadgeAssignmentLogV2, error) {
	return s.store.RebuildListBadgeAssignmentLogs(ctx, id)
}

func (s *Service) RebuildImportBadgeDevices(ctx context.Context, req RebuildBadgeImportRequest, operatorID int64, operatorName string) (*RebuildBadgeImportResponse, error) {
	if strings.TrimSpace(req.ManufacturerCode) == "" {
		return nil, fmt.Errorf("manufacturer_code is required")
	}
	if len(req.Devices) == 0 {
		return nil, fmt.Errorf("devices is required")
	}
	return s.store.RebuildImportBadgeDevices(ctx, req, operatorID, operatorName)
}

func (s *Service) RebuildAcceptBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.store.RebuildAcceptBadgeDevice(ctx, id, reason, operatorID, operatorName)
}

func (s *Service) RebuildRejectAcceptance(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.store.RebuildRejectAcceptance(ctx, id, reason, operatorID, operatorName)
}

func (s *Service) RebuildAssignBadgeDevice(ctx context.Context, id int64, req RebuildBadgeAssignRequest, operatorID int64, operatorName string) error {
	if req.TenantID <= 0 {
		return fmt.Errorf("tenant_id is required")
	}
	if req.EmployeeID <= 0 {
		return fmt.Errorf("employee_id is required")
	}
	return s.store.RebuildAssignBadgeDevice(ctx, id, req.TenantID, req.EmployeeID, operatorID, operatorName)
}

func (s *Service) RebuildReclaimBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.store.RebuildReclaimBadgeDevice(ctx, id, reason, operatorID, operatorName)
}

func (s *Service) RebuildRestockBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.store.RebuildRestockBadgeDevice(ctx, id, reason, operatorID, operatorName)
}

func (s *Service) RebuildRetireBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.store.RebuildRetireBadgeDevice(ctx, id, reason, operatorID, operatorName)
}
