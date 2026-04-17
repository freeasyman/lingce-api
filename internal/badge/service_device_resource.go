package badge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type ManufacturerAdapter interface {
	CheckDeviceExists(ctx context.Context, deviceNo string) (bool, error)
	CheckOnline(ctx context.Context, deviceNo string) (bool, *time.Time, error)
	GetBatteryLevel(ctx context.Context, deviceNo string) (*int, error)
	StartRecording(ctx context.Context, deviceNo string) error
	StopRecording(ctx context.Context, deviceNo string) error
	SyncDevices(ctx context.Context) ([]string, error)
}

func (s *Service) getManufacturerAdapter(vendorCode string) ManufacturerAdapter {
	return newManufacturerAdapter(vendorCode, s.middlewareClient)
}

func (s *Service) V2ListDevices(ctx context.Context, req V2DeviceListRequest) ([]*BadgeDevice, int, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
	if req.HealthStatus != nil {
		parsed, ok := parseHealthStatusFilter(*req.HealthStatus)
		if !ok {
			return nil, 0, fmt.Errorf("invalid health_status, allowed: unknown, healthy, warning, error")
		}
		req.HealthStatus = &parsed
	}
	return s.store.V2ListDevices(ctx, req)
}

func (s *Service) V2GetDeviceByID(ctx context.Context, id int64) (*BadgeDevice, []*BadgeDeviceLog, error) {
	return s.store.V2GetDeviceByID(ctx, id)
}

func (s *Service) V2ImportDevices(ctx context.Context, req V2BatchImportRequest, operatorID int64, operatorName string) (JSONObject, error) {
	if strings.TrimSpace(req.ManufacturerCode) == "" {
		return nil, fmt.Errorf("manufacturer_code is required")
	}
	if len(req.Devices) == 0 {
		return nil, fmt.Errorf("devices is required")
	}
	success, failed, duplicates, createdIDs, err := s.store.V2ImportDevices(ctx, req, operatorID, operatorName)
	if err != nil {
		return nil, err
	}
	return JSONObject{
		"success":     success,
		"failed":      failed,
		"duplicates":  duplicates,
		"created_ids": createdIDs,
	}, nil
}

func (s *Service) V2HealthCheck(ctx context.Context, deviceID int64) (*V2HealthCheckResult, error) {
	device, _, err := s.store.V2GetDeviceByID(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	adapter := s.getManufacturerAdapter(device.ManufacturerCode)

	result := JSONObject{
		"device_exists": true,
		"online":        false,
		"battery_level": device.BatteryLevel,
		"recording_ok":  false,
	}

	exists, err := adapter.CheckDeviceExists(ctx, device.DeviceNo)
	if err != nil {
		result["device_exists_error"] = err.Error()
	}
	result["device_exists"] = exists

	online, onlineAt, err := adapter.CheckOnline(ctx, device.DeviceNo)
	if err != nil {
		result["online_error"] = err.Error()
	}
	result["online"] = online
	if onlineAt != nil {
		result["online_at"] = onlineAt.Format(time.RFC3339)
	}

	batteryLevel, err := adapter.GetBatteryLevel(ctx, device.DeviceNo)
	if err == nil && batteryLevel != nil {
		result["battery_level"] = *batteryLevel
	}

	recordingOK := true
	if err := adapter.StartRecording(ctx, device.DeviceNo); err != nil {
		recordingOK = false
		result["start_recording_error"] = err.Error()
	}
	time.Sleep(2 * time.Second)
	if err := adapter.StopRecording(ctx, device.DeviceNo); err != nil {
		recordingOK = false
		result["stop_recording_error"] = err.Error()
	}
	result["recording_ok"] = recordingOK

	lastOnline := device.LastOnlineAt
	if onlineAt != nil {
		lastOnline = onlineAt
	}
	var offlineDuration *time.Duration
	if lastOnline != nil {
		d := time.Since(*lastOnline)
		offlineDuration = &d
	}
	resolvedBattery := batteryLevel
	if resolvedBattery == nil {
		resolvedBattery = device.BatteryLevel
	}
	healthStatus := determineHealthStatus(online, resolvedBattery, recordingOK, offlineDuration, exists)

	return &V2HealthCheckResult{
		DeviceID:          device.ID,
		DeviceNo:          device.DeviceNo,
		Passed:            healthStatus != "error",
		HealthStatus:      healthStatus,
		HealthCheckResult: result,
	}, nil
}

func (s *Service) V2BatchHealthCheck(ctx context.Context, deviceIDs []int64) (JSONObject, error) {
	if len(deviceIDs) == 0 {
		return nil, fmt.Errorf("device_ids is required")
	}

	type item struct {
		result *V2HealthCheckResult
		err    error
	}

	sem := make(chan struct{}, 10)
	out := make(chan item, len(deviceIDs))
	var wg sync.WaitGroup

	for _, id := range deviceIDs {
		deviceID := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			perCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			result, err := s.V2HealthCheck(perCtx, deviceID)
			out <- item{result: result, err: err}
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	results := make([]*V2HealthCheckResult, 0, len(deviceIDs))
	errors := make([]string, 0)
	success := 0
	failed := 0

	for it := range out {
		if it.err != nil {
			failed++
			errors = append(errors, it.err.Error())
			continue
		}
		success++
		results = append(results, it.result)
	}

	return JSONObject{
		"success": success,
		"failed":  failed,
		"results": results,
		"errors":  errors,
	}, nil
}

func determineHealthStatus(online bool, batteryLevel *int, recordingOK bool, offlineDuration *time.Duration, exists bool) string {
	if !exists || !recordingOK {
		return HealthStatusError
	}

	if batteryLevel != nil {
		if *batteryLevel < 10 {
			return HealthStatusError
		}
	}

	if offlineDuration != nil && *offlineDuration > 24*time.Hour {
		return HealthStatusError
	}

	if batteryLevel != nil && *batteryLevel >= 10 && *batteryLevel <= 20 {
		return HealthStatusWarning
	}

	if offlineDuration != nil && *offlineDuration > 12*time.Hour {
		return HealthStatusWarning
	}

	if !online {
		return HealthStatusWarning
	}

	if batteryLevel != nil && *batteryLevel > 20 {
		return HealthStatusHealthy
	}

	// Unknown battery, online and recording is healthy: keep healthy by default.
	return HealthStatusHealthy
}

func (s *Service) V2BatchAccept(ctx context.Context, req V2BatchAcceptRequest, operatorID int64, operatorName string) (JSONObject, error) {
	if len(req.DeviceIDs) == 0 {
		return nil, fmt.Errorf("device_ids is required")
	}
	success := 0
	failed := 0
	results := make([]*V2HealthCheckResult, 0, len(req.DeviceIDs))
	for _, id := range req.DeviceIDs {
		var health *V2HealthCheckResult
		var err error
		if req.SkipHealthCheck {
			device, _, getErr := s.store.V2GetDeviceByID(ctx, id)
			if getErr != nil {
				failed++
				continue
			}
			health = &V2HealthCheckResult{
				DeviceID:          device.ID,
				DeviceNo:          device.DeviceNo,
				Passed:            true,
				HealthStatus:      HealthStatusUnknown,
				HealthCheckResult: JSONObject{"skipped": true},
			}
		} else {
			health, err = s.V2HealthCheck(ctx, id)
			if err != nil {
				failed++
				continue
			}
		}
		toStatus := "ready"
		if !health.Passed {
			toStatus = "blocked"
		}
		if err := s.store.V2UpdateDeviceStatusWithHealth(ctx, id, toStatus, health.HealthStatus, health.HealthCheckResult, operatorID, operatorName, "accept", JSONObject{
			"acceptance_batch_no": req.AcceptanceBatchNo,
			"skip_health_check":   req.SkipHealthCheck,
		}); err != nil {
			failed++
			continue
		}
		success++
		results = append(results, health)
	}
	return JSONObject{
		"success": success,
		"failed":  failed,
		"results": results,
	}, nil
}

func (s *Service) V2BatchAssign(ctx context.Context, req V2BatchAssignRequest, operatorID int64, operatorName string) (JSONObject, error) {
	if len(req.DeviceIDs) == 0 || req.TenantID <= 0 || req.EmployeeID <= 0 {
		return nil, fmt.Errorf("device_ids, tenant_id and employee_id are required")
	}
	success, failed, errors, err := s.store.V2BatchAssign(ctx, req, operatorID, operatorName)
	if err != nil {
		return nil, err
	}
	return JSONObject{"success": success, "failed": failed, "errors": errors}, nil
}

func (s *Service) V2BatchReclaim(ctx context.Context, req V2BatchReclaimRequest, operatorID int64, operatorName string) (JSONObject, error) {
	if len(req.DeviceIDs) == 0 {
		return nil, fmt.Errorf("device_ids is required")
	}
	success, failed, err := s.store.V2BatchReclaim(ctx, req, operatorID, operatorName)
	if err != nil {
		return nil, err
	}
	return JSONObject{"success": success, "failed": failed}, nil
}

func (s *Service) V2Transfer(ctx context.Context, id int64, req V2TransferRequest, operatorID int64, operatorName string) error {
	if req.ToTenantID <= 0 || req.ToEmployeeID <= 0 {
		return fmt.Errorf("to_tenant_id and to_employee_id are required")
	}
	return s.store.V2Transfer(ctx, id, req, operatorID, operatorName)
}

func (s *Service) V2UpdateDevice(ctx context.Context, id int64, req V2UpdateDeviceRequest, operatorID int64, operatorName string) error {
	return s.store.V2UpdateDevice(ctx, id, req, operatorID, operatorName)
}

func (s *Service) V2Dashboard(ctx context.Context) (JSONObject, error) {
	return s.store.V2Dashboard(ctx)
}

func (s *Service) V2ListDeviceLogs(ctx context.Context, deviceID int64, operation *string, page, pageSize int) ([]*BadgeDeviceLog, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return s.store.V2ListDeviceLogs(ctx, deviceID, operation, page, pageSize)
}

func (s *Service) V2ExportDevicesCSV(ctx context.Context, req V2DeviceListRequest) (string, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 10000
	}
	return s.store.V2ExportDevicesCSV(ctx, req)
}

func (s *Service) V2ListManufacturers(ctx context.Context) ([]*BadgeManufacturer, error) {
	return s.store.ListManufacturers(ctx)
}

func (s *Service) V2SyncManufacturer(ctx context.Context, code string) (JSONObject, error) {
	adapter := s.getManufacturerAdapter(code)
	vendorDevices, err := adapter.SyncDevices(ctx)
	if err != nil {
		return nil, err
	}
	internalReq := V2DeviceListRequest{ManufacturerCode: &code, Page: 1, PageSize: 10000}
	internalDevices, _, err := s.V2ListDevices(ctx, internalReq)
	if err != nil {
		return nil, err
	}
	internalSet := make(map[string]struct{}, len(internalDevices))
	for _, d := range internalDevices {
		internalSet[d.DeviceNo] = struct{}{}
	}
	newCount := 0
	missing := make([]string, 0)
	for _, no := range vendorDevices {
		if _, ok := internalSet[no]; !ok {
			newCount++
		}
	}
	for no := range internalSet {
		found := false
		for _, vendorNo := range vendorDevices {
			if no == vendorNo {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, no)
		}
	}
	return JSONObject{
		"manufacturer_code": code,
		"vendor_total":      len(vendorDevices),
		"internal_total":    len(internalDevices),
		"new_devices":       newCount,
		"missing_devices":   missing,
	}, nil
}
