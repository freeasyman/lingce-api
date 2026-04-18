package badge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

const healthCheckRecordingDuration = 10 * time.Second
const healthCheckCallbackWaitTimeout = 12 * time.Second
const healthCheckCallbackPollInterval = 1 * time.Second

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
	devices, total, err := s.store.V2ListDevices(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	if req.Realtime {
		s.hydrateRealtimeStatus(ctx, devices)
	}
	return devices, total, nil
}

func (s *Service) V2GetDeviceByID(ctx context.Context, id int64) (*BadgeDevice, []*BadgeDeviceLog, error) {
	device, logs, err := s.store.V2GetDeviceByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	s.hydrateRealtimeStatus(ctx, []*BadgeDevice{device})
	return device, logs, nil
}

func (s *Service) V2GetLiveStatus(ctx context.Context, deviceID int64) (JSONObject, error) {
	device, _, err := s.store.V2GetDeviceByID(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	adapter := s.getManufacturerAdapter(device.ManufacturerCode)

	online, onlineAt, onlineErr := adapter.CheckOnline(ctx, device.DeviceNo)
	batteryLevel, batteryErr := adapter.GetBatteryLevel(ctx, device.DeviceNo)

	if online && onlineAt == nil {
		now := time.Now()
		onlineAt = &now
	}

	if onlineErr == nil || batteryErr == nil {
		_ = s.store.V2UpdateRealtimeSnapshot(ctx, device.ID, batteryLevel, onlineAt)
	}

	resp := JSONObject{
		"device_id":  deviceID,
		"online":     online,
		"battery":    batteryLevel,
		"checked_at": time.Now().Format(time.RFC3339),
	}
	if onlineAt != nil {
		resp["online_at"] = onlineAt.Format(time.RFC3339)
	}
	if onlineErr != nil {
		resp["online_error"] = onlineErr.Error()
	}
	if batteryErr != nil {
		resp["battery_error"] = batteryErr.Error()
	}
	return resp, nil
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

	checkStartedAt := time.Now()
	startRecordingOK := true
	stopRecordingOK := true
	recordingOK := true
	if err := adapter.StartRecording(ctx, device.DeviceNo); err != nil {
		startRecordingOK = false
		recordingOK = false
		result["start_recording_error"] = err.Error()
	} else {
		// Hardware requires a minimum recording duration before stop.
		time.Sleep(healthCheckRecordingDuration)
	}

	if err := adapter.StopRecording(ctx, device.DeviceNo); err != nil {
		// Retry once after a short grace period to reduce false negatives
		// when device-side stop arrives a bit later than start acknowledgement.
		time.Sleep(3 * time.Second)
		if retryErr := adapter.StopRecording(ctx, device.DeviceNo); retryErr != nil {
			stopRecordingOK = false
			recordingOK = false
			result["stop_recording_error"] = retryErr.Error()
		}
	}
	result["recording_ok"] = recordingOK
	result["recording_test"] = JSONObject{
		"start": startRecordingOK,
		"stop":  stopRecordingOK,
	}
	result["recording_duration_seconds"] = int(healthCheckRecordingDuration / time.Second)
	if startRecordingOK && stopRecordingOK {
		callbackOK := s.waitForAudioCallback(ctx, device.DeviceNo, checkStartedAt, healthCheckCallbackWaitTimeout)
		result["callback_ok"] = callbackOK
		if recordingTest, ok := result["recording_test"].(JSONObject); ok {
			recordingTest["callback"] = callbackOK
			result["recording_test"] = recordingTest
		}
	}

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
		Status:            device.Status,
		Passed:            healthStatus != "error",
		HealthStatus:      healthStatus,
		HealthCheckResult: result,
	}, nil
}

func (s *Service) V2HealthCheckAndPersist(ctx context.Context, deviceID int64, operatorID int64, operatorName string) (*V2HealthCheckResult, error) {
	health, err := s.V2HealthCheck(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	device, _, err := s.store.V2GetDeviceByID(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	toStatus := deriveStatusAfterHealthCheck(device.Status, health.Passed)
	if err := s.store.V2UpdateDeviceStatusWithHealth(
		ctx,
		deviceID,
		toStatus,
		health.HealthStatus,
		health.HealthCheckResult,
		operatorID,
		operatorName,
		"check",
		JSONObject{
			"trigger": "single_health_check",
			"passed":  health.Passed,
		},
	); err != nil {
		return nil, err
	}

	health.Status = toStatus
	return health, nil
}

func deriveStatusAfterHealthCheck(currentStatus string, passed bool) string {
	current := strings.ToLower(strings.TrimSpace(currentStatus))
	if current == "retired" {
		return "retired"
	}
	if !passed {
		return "blocked"
	}
	if current == "in_use" {
		return "in_use"
	}
	return "ready"
}

func (s *Service) waitForAudioCallback(ctx context.Context, deviceNo string, since time.Time, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	action := "callback"
	status := "success"
	for time.Now().Before(deadline) {
		logs, _, err := s.store.ListRecordingControlLogs(ctx, RecordingControlLogListRequest{
			DeviceNo: &deviceNo,
			Action:   &action,
			Status:   &status,
			Page:     1,
			PageSize: 50,
		})
		if err == nil {
			for _, log := range logs {
				if log == nil {
					continue
				}
				if log.CreatedAt.Before(since.Add(-2 * time.Second)) {
					continue
				}
				source := strings.ToLower(strings.TrimSpace(anyToString(log.ExtraData["source"])))
				eventType := strings.ToLower(strings.TrimSpace(anyToString(log.ExtraData["event_type"])))
				if source == "audio" || strings.Contains(eventType, "audio") {
					return true
				}
			}
		}
		time.Sleep(healthCheckCallbackPollInterval)
	}
	return false
}

func anyToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return ""
	}
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

func (s *Service) hydrateRealtimeStatus(ctx context.Context, devices []*BadgeDevice) {
	if len(devices) == 0 {
		return
	}

	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, d := range devices {
		if d == nil || strings.TrimSpace(d.DeviceNo) == "" {
			continue
		}
		device := d
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			perReqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			adapter := s.getManufacturerAdapter(device.ManufacturerCode)
			online, onlineAt, onlineErr := adapter.CheckOnline(perReqCtx, device.DeviceNo)
			batteryLevel, batteryErr := adapter.GetBatteryLevel(perReqCtx, device.DeviceNo)
			if onlineErr != nil && batteryErr != nil {
				device.LastOnlineAt = nil
				device.BatteryLevel = nil
				return
			}

			if onlineErr == nil && online && onlineAt == nil {
				now := time.Now()
				onlineAt = &now
			}

			if batteryErr != nil {
				device.BatteryLevel = nil
			} else {
				device.BatteryLevel = batteryLevel
			}
			if onlineErr != nil {
				device.LastOnlineAt = nil
			} else {
				device.LastOnlineAt = onlineAt
			}

			_ = s.store.V2UpdateRealtimeSnapshot(ctx, device.ID, batteryLevel, onlineAt)
		}()
	}
	wg.Wait()
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
