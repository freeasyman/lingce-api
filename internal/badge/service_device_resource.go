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

type DeviceRealtimeInfo struct {
	Online        bool
	LastOnlineAt  *time.Time
	BatteryLevel  *int
	HardwareModel string
}

type ManufacturerAdapter interface {
	CheckDeviceExists(ctx context.Context, deviceNo string) (bool, error)
	CheckOnline(ctx context.Context, deviceNo string) (bool, *time.Time, error)
	GetBatteryLevel(ctx context.Context, deviceNo string) (*int, error)
	GetDeviceRealtimeInfo(ctx context.Context, deviceNo string) (*DeviceRealtimeInfo, error)
	StartRecording(ctx context.Context, deviceNo string) error
	StopRecording(ctx context.Context, deviceNo string) error
	SyncDevices(ctx context.Context) ([]VendorDeviceSnapshot, error)
}

var manufacturerSyncLock sync.Mutex
var manufacturerSyncInFlight = map[string]bool{}

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

	info, err := adapter.GetDeviceRealtimeInfo(ctx, device.DeviceNo)

	resp := JSONObject{
		"device_id":  deviceID,
		"checked_at": time.Now().Format(time.RFC3339),
	}

	if err != nil {
		resp["error"] = err.Error()
		return resp, nil
	}

	if info == nil {
		resp["error"] = "device not found"
		return resp, nil
	}

	if info.Online && info.LastOnlineAt == nil {
		now := time.Now()
		info.LastOnlineAt = &now
	}

	// Update database snapshot with fresh data
	var hardwareModelPtr *string
	if info.HardwareModel != "" {
		hardwareModelPtr = &info.HardwareModel
	}
	_ = s.store.V2UpdateRealtimeSnapshot(ctx, device.ID, info.BatteryLevel, info.LastOnlineAt, hardwareModelPtr)

	resp["online"] = info.Online
	resp["battery"] = info.BatteryLevel
	if info.LastOnlineAt != nil {
		resp["online_at"] = info.LastOnlineAt.Format(time.RFC3339)
	}
	if info.HardwareModel != "" {
		resp["hardware_model"] = info.HardwareModel
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
	switch current {
	case "retired":
		return "retired"
	case "assigned", "in_use":
		return "assigned"
	case "returned":
		return "returned"
	case "pending_acceptance", "pending":
		return "pending_acceptance"
	default:
		return "in_stock"
	}
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

			result, err := s.V2HealthCheckAndPersist(perCtx, deviceID, 0, "")
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
			info, err := adapter.GetDeviceRealtimeInfo(perReqCtx, device.DeviceNo)

			// If API call fails, keep the database values unchanged
			// Do NOT set to nil - that would be mocking empty data
			if err != nil || info == nil {
				return
			}

			// If device is online but no timestamp, use current time
			if info.Online && info.LastOnlineAt == nil {
				now := time.Now()
				info.LastOnlineAt = &now
			}

			// Update in-memory device object with fresh data
			device.BatteryLevel = info.BatteryLevel
			device.LastOnlineAt = info.LastOnlineAt
			if info.HardwareModel != "" {
				device.HardwareModel = &info.HardwareModel
			}

			// Update database snapshot with fresh data
			var hardwareModelPtr *string
			if info.HardwareModel != "" {
				hardwareModelPtr = &info.HardwareModel
			}
			_ = s.store.V2UpdateRealtimeSnapshot(ctx, device.ID, info.BatteryLevel, info.LastOnlineAt, hardwareModelPtr)
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
		toStatus := BadgeStatusInStock
		if !health.Passed {
			toStatus = BadgeStatusReturned
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
	if len(req.DeviceIDs) > 1 {
		return nil, fmt.Errorf("each assignment can only target one device")
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
	success, failed, errors, err := s.store.V2BatchReclaim(ctx, req, operatorID, operatorName)
	if err != nil {
		return nil, err
	}
	return JSONObject{"success": success, "failed": failed, "errors": errors}, nil
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

func (s *Service) V2ListAllDeviceLogs(ctx context.Context, req V2DeviceLogListRequest) ([]*BadgeDeviceLog, int, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
	return s.store.V2ListAllDeviceLogs(ctx, req)
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
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return nil, fmt.Errorf("manufacturer code is required")
	}
	if !acquireManufacturerSync(code) {
		return nil, fmt.Errorf("manufacturer sync is already in progress")
	}
	defer releaseManufacturerSync(code)

	adapter := s.getManufacturerAdapter(code)
	vendorDevices, err := adapter.SyncDevices(ctx)
	if err != nil {
		return nil, err
	}

	manufacturers, err := s.store.ListManufacturers(ctx)
	if err != nil {
		return nil, err
	}
	var manufacturerID int64
	manufacturerName := code
	for _, item := range manufacturers {
		if strings.EqualFold(strings.TrimSpace(item.Code), code) {
			manufacturerID = item.ID
			manufacturerName = strings.TrimSpace(item.Name)
			break
		}
	}
	if manufacturerID == 0 {
		return nil, fmt.Errorf("manufacturer not found: %s", code)
	}
	manufacturerAppID, err := s.store.resolveManufacturerAppID(ctx, code)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.pool.Exec(ctx, `
		UPDATE badge_devices
		SET app_id = $2, updated_at = NOW()
		WHERE manufacturer_code = $1
		  AND deleted_at IS NULL
		  AND (
		    app_id IS NULL OR
		    TRIM(app_id) = '' OR
		    LOWER(TRIM(app_id)) = LOWER($1)
		  )
	`, code, manufacturerAppID); err != nil {
		return nil, fmt.Errorf("failed to repair manufacturer app_id mapping: %w", err)
	}

	type existingDevice struct {
		ID           int64
		DeviceNo     string
		Status       string
		EmployeeID   *int64
		Hardware     *string
		BatteryLevel *int
		LastOnlineAt *time.Time
		HealthStatus string
	}

	rows, err := s.store.pool.Query(ctx, `
		SELECT id, device_no, status, employee_id, hardware_model, battery_level, last_online_at, health_status
		FROM badge_devices
		WHERE manufacturer_code = $1 AND deleted_at IS NULL
	`, code)
	if err != nil {
		return nil, fmt.Errorf("failed to list internal devices: %w", err)
	}
	defer rows.Close()

	existingMap := make(map[string]*existingDevice)
	for rows.Next() {
		var item existingDevice
		if scanErr := rows.Scan(
			&item.ID,
			&item.DeviceNo,
			&item.Status,
			&item.EmployeeID,
			&item.Hardware,
			&item.BatteryLevel,
			&item.LastOnlineAt,
			&item.HealthStatus,
		); scanErr != nil {
			return nil, fmt.Errorf("failed to scan internal device: %w", scanErr)
		}
		existingMap[item.DeviceNo] = &item
	}

	newCount := 0
	updatedCount := 0
	unchangedCount := 0
	missingCount := 0
	pendingAssignmentCount := 0
	failedCount := 0
	failedItems := make([]JSONObject, 0)
	missingDevices := make([]string, 0)
	vendorSet := make(map[string]struct{}, len(vendorDevices))

	for _, item := range vendorDevices {
		deviceNo := strings.TrimSpace(item.DeviceNo)
		if deviceNo == "" {
			failedCount++
			failedItems = append(failedItems, JSONObject{
				"device_no": "",
				"reason":    "empty device_no in vendor payload",
			})
			continue
		}
		vendorSet[deviceNo] = struct{}{}

		healthStatus := HealthStatusUnknown
		if item.VendorStatus != "" {
			mapped, ok := mapVendorStatusToHealthStatus(item.VendorStatus)
			if !ok {
				failedCount++
				failedItems = append(failedItems, JSONObject{
					"device_no":      deviceNo,
					"vendor_status":  item.VendorStatus,
					"reason":         "unknown vendor status",
					"manufacturer":   code,
					"hardware_model": item.HardwareModel,
				})
				continue
			}
			healthStatus = mapped
		}

		existing, ok := existingMap[deviceNo]
		if ok {
			if existing.EmployeeID == nil {
				pendingAssignmentCount++
			}
			targetHardware := strings.TrimSpace(item.HardwareModel)
			existingHardware := strings.TrimSpace(valueOrEmptyString(existing.Hardware))
			changed := existingHardware != targetHardware ||
				intPtrValue(existing.BatteryLevel, -1) != intPtrValue(item.BatteryLevel, -1) ||
				!timePtrEqual(existing.LastOnlineAt, item.LastOnlineAt) ||
				normalizeHealthStatus(existing.HealthStatus) != normalizeHealthStatus(healthStatus)

			_, err = s.store.pool.Exec(ctx, `
				UPDATE badge_devices
				SET manufacturer_id = $2,
				    app_id = $3,
				    manufacturer_name = $4,
				    hardware_model = NULLIF($5, ''),
				    battery_level = $6,
				    last_online_at = $7,
				    health_status = $8,
				    updated_at = NOW()
				WHERE id = $1
			`, existing.ID, manufacturerID, manufacturerAppID, manufacturerName, targetHardware, item.BatteryLevel, item.LastOnlineAt, normalizeHealthStatus(healthStatus))
			if err != nil {
				failedCount++
				failedItems = append(failedItems, JSONObject{
					"device_no": deviceNo,
					"reason":    fmt.Sprintf("failed to update local device: %v", err),
				})
				continue
			}
			if changed {
				updatedCount++
			} else {
				unchangedCount++
			}
			continue
		}

		deviceUID := fmt.Sprintf("%s:%d:%s", code, manufacturerID, deviceNo)
		err = tryInsertVendorDevice(ctx, s, manufacturerID, code, manufacturerAppID, deviceNo, deviceUID, manufacturerName, item.HardwareModel, normalizeHealthStatus(healthStatus), item.BatteryLevel, item.LastOnlineAt)
		if err != nil {
			failedCount++
			failedItems = append(failedItems, JSONObject{
				"device_no": deviceNo,
				"reason":    fmt.Sprintf("failed to insert local device: %v", err),
			})
			continue
		}
		newCount++
	}

	for deviceNo := range existingMap {
		if _, ok := vendorSet[deviceNo]; ok {
			continue
		}
		missingCount++
		if len(missingDevices) < 200 {
			missingDevices = append(missingDevices, deviceNo)
		}
	}

	vendorTotal := len(vendorSet)
	syncBatchID := fmt.Sprintf("%d", time.Now().Unix())

	return JSONObject{
		"sync_batch_id":            syncBatchID,
		"manufacturer_code":        code,
		"manufacturer_name":        manufacturerName,
		"vendor_total":             vendorTotal,
		"internal_total":           len(existingMap),
		"new_devices":              newCount,
		"updated_devices":          updatedCount,
		"unchanged_devices":        unchangedCount,
		"missing_devices_count":    missingCount,
		"missing_devices":          missingDevices,
		"failed_count":             failedCount,
		"failed_items":             failedItems,
		"new_arrival_count":        newCount,
		"pending_assignment_count": pendingAssignmentCount,
		"anomaly_count":            missingCount,
		"consistent_count":         maxInt(vendorTotal-newCount, 0),
		"synced_at":                time.Now().Format(time.RFC3339),
	}, nil
}

func mapVendorStatusToHealthStatus(vendorStatus string) (string, bool) {
	status := strings.ToLower(strings.TrimSpace(vendorStatus))
	switch status {
	case "", "unknown":
		return HealthStatusUnknown, true
	case "online", "recording", "idle":
		return HealthStatusHealthy, true
	case "offline", "sleep":
		return HealthStatusWarning, true
	case "fault", "error", "abnormal":
		return HealthStatusError, true
	default:
		return "", false
	}
}

func intPtrValue(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Equal(*b)
}

func maxInt(a, b int) int {
	if a >= b {
		return a
	}
	return b
}

func isBadgeStatusConstraintViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "ck_badge_devices_status") && strings.Contains(msg, "sqlstate 23514")
}

func tryInsertVendorDevice(
	ctx context.Context,
	s *Service,
	manufacturerID int64,
	code string,
	appID string,
	deviceNo string,
	deviceUID string,
	manufacturerName string,
	hardwareModel string,
	healthStatus string,
	batteryLevel *int,
	lastOnlineAt *time.Time,
) error {
	insertWithDBDefaults := func() error {
		_, err := s.store.pool.Exec(ctx, `
			INSERT INTO badge_devices (
				manufacturer_id, app_id, device_no, device_uid,
				manufacturer_code, manufacturer_name, hardware_model,
				health_status, battery_level, last_online_at,
				metadata, ext_json, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, NULLIF($7, ''),
				$8, $9, $10,
				'{}'::jsonb, '{}'::jsonb, NOW(), NOW()
			)
		`, manufacturerID, appID, deviceNo, deviceUID, code, manufacturerName, hardwareModel, healthStatus, batteryLevel, lastOnlineAt)
		return err
	}

	insertWithStatus := func(currentStatus, status string) error {
		_, err := s.store.pool.Exec(ctx, `
			INSERT INTO badge_devices (
				manufacturer_id, app_id, device_no, device_uid,
				manufacturer_code, manufacturer_name, hardware_model,
				status, health_status, battery_level, last_online_at,
				metadata, ext_json, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, NULLIF($7, ''),
				$8, $9, $10, $11,
				'{}'::jsonb, '{}'::jsonb, NOW(), NOW()
			)
		`, manufacturerID, appID, deviceNo, deviceUID, code, manufacturerName, hardwareModel, status, healthStatus, batteryLevel, lastOnlineAt)
		return err
	}

	// Retry matrix for status-constraint compatibility across mixed schemas:
	// 1) new/new, 2) old/new, 3) old/old, 4) DB defaults.
	if err := insertWithStatus("pending", "pending"); err != nil {
		if !isBadgeStatusConstraintViolation(err) {
			return err
		}
		if err2 := insertWithStatus("draft", "pending"); err2 != nil {
			if !isBadgeStatusConstraintViolation(err2) {
				return err2
			}
			if err3 := insertWithStatus("draft", "draft"); err3 != nil {
				if !isBadgeStatusConstraintViolation(err3) {
					return err3
				}
				if err4 := insertWithDBDefaults(); err4 != nil {
					return err4
				}
			}
		}
	}
	return nil
}

func acquireManufacturerSync(code string) bool {
	manufacturerSyncLock.Lock()
	defer manufacturerSyncLock.Unlock()
	if manufacturerSyncInFlight[code] {
		return false
	}
	manufacturerSyncInFlight[code] = true
	return true
}

func releaseManufacturerSync(code string) {
	manufacturerSyncLock.Lock()
	defer manufacturerSyncLock.Unlock()
	delete(manufacturerSyncInFlight, code)
}
