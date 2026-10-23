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

func (s *Service) RebuildBatchSetAvailability(ctx context.Context, req RebuildBadgeBatchRequest, operatorID int64) (int, []int64, error) {
	if len(req.DeviceIDs) == 0 {
		return 0, nil, fmt.Errorf("device_ids is required")
	}
	if len(req.DeviceIDs) > 500 {
		return 0, nil, fmt.Errorf("一次最多处理 500 台")
	}
	if !validAvailability(req.Availability) {
		return 0, nil, fmt.Errorf("invalid availability: %s", req.Availability)
	}
	return s.store.RebuildBatchSetAvailability(ctx, req.DeviceIDs, req.Availability, req.Reason, operatorID)
}

func (s *Service) RebuildChangeBadgePlacement(ctx context.Context, id int64, req RebuildBadgePlacementRequest, operatorID int64, operatorName string) (*BadgeDeviceV2, error) {
	req.Position = strings.TrimSpace(req.Position)
	req.Availability = strings.TrimSpace(req.Availability)
	if req.Position == "" && req.Availability == "" {
		return nil, fmt.Errorf("position or availability is required")
	}
	return s.store.RebuildChangeBadgePlacement(ctx, id, req, operatorID, operatorName)
}

func (s *Service) RebuildSwapBadgeDevices(ctx context.Context, oldID int64, req RebuildBadgeSwapRequest, operatorID int64, operatorName string) (*BadgeDeviceV2, error) {
	if req.NewDeviceID <= 0 {
		return nil, fmt.Errorf("new_device_id is required")
	}
	return s.store.RebuildSwapBadgeDevices(ctx, oldID, req.NewDeviceID, req.Reason, operatorID, operatorName)
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
