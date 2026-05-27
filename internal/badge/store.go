package badge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	ossutil "github.com/freeasyman/lingce-api/pkg/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

type AudioCallbackIngestResult struct {
	RecordingID int64
	TenantID    int64
	Created     bool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Device Methods

// ListDevices retrieves a paginated list of devices
func (s *Store) ListDevices(ctx context.Context, req DeviceListRequest) ([]*BadgeDevice, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "deleted_at IS NULL")

	if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.EmployeeID != nil {
		conditions = append(conditions, fmt.Sprintf("employee_id = $%d", argIndex))
		args = append(args, *req.EmployeeID)
		argIndex++
	}

	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}

	if req.ManufacturerCode != nil {
		conditions = append(conditions, fmt.Sprintf("manufacturer_code = $%d", argIndex))
		args = append(args, *req.ManufacturerCode)
		argIndex++
	}

	if req.DeviceNo != nil {
		conditions = append(conditions, fmt.Sprintf("device_no ILIKE $%d", argIndex))
		args = append(args, "%"+*req.DeviceNo+"%")
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM badge_devices WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count devices: %w", err)
	}

	// Query devices
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, device_no, device_id, manufacturer_code, model, status, tenant_id, employee_id,
		       accepted_at, assigned_to_tenant_at, assigned_to_emp_at, last_online_at,
		       battery_level, firmware_version, extra_data, created_at, updated_at
		FROM badge_devices
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query devices: %w", err)
	}
	defer rows.Close()

	var devices []*BadgeDevice
	for rows.Next() {
		var d BadgeDevice
		if err := rows.Scan(&d.ID, &d.DeviceNo, &d.DeviceID, &d.ManufacturerCode, &d.Model,
			&d.Status, &d.TenantID, &d.EmployeeID, &d.AcceptedAt, &d.AssignedToTenantAt,
			&d.AssignedToEmpAt, &d.LastOnlineAt, &d.BatteryLevel, &d.FirmwareVersion,
			&d.ExtraData, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan device: %w", err)
		}
		devices = append(devices, &d)
	}

	return devices, total, nil
}

// GetDeviceByID retrieves a device by ID
func (s *Store) GetDeviceByID(ctx context.Context, id int64) (*BadgeDevice, error) {
	query := `
		SELECT id, device_no, device_id, manufacturer_code, model, status, tenant_id, employee_id,
		       accepted_at, assigned_to_tenant_at, assigned_to_emp_at, last_online_at,
		       battery_level, firmware_version, extra_data, created_at, updated_at
		FROM badge_devices
		WHERE id = $1 AND deleted_at IS NULL
	`

	var d BadgeDevice
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&d.ID, &d.DeviceNo, &d.DeviceID, &d.ManufacturerCode, &d.Model,
		&d.Status, &d.TenantID, &d.EmployeeID, &d.AcceptedAt, &d.AssignedToTenantAt,
		&d.AssignedToEmpAt, &d.LastOnlineAt, &d.BatteryLevel, &d.FirmwareVersion,
		&d.ExtraData, &d.CreatedAt, &d.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("device not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query device: %w", err)
	}

	return &d, nil
}

// GetDeviceByDeviceNo retrieves a device by device number
func (s *Store) GetDeviceByDeviceNo(ctx context.Context, deviceNo string) (*BadgeDevice, error) {
	query := `
		SELECT id, device_no, device_id, manufacturer_code, model, status, tenant_id, employee_id,
		       accepted_at, assigned_to_tenant_at, assigned_to_emp_at, last_online_at,
		       battery_level, firmware_version, extra_data, created_at, updated_at
		FROM badge_devices
		WHERE device_no = $1 AND deleted_at IS NULL
	`

	var d BadgeDevice
	err := s.pool.QueryRow(ctx, query, deviceNo).Scan(
		&d.ID, &d.DeviceNo, &d.DeviceID, &d.ManufacturerCode, &d.Model,
		&d.Status, &d.TenantID, &d.EmployeeID, &d.AcceptedAt, &d.AssignedToTenantAt,
		&d.AssignedToEmpAt, &d.LastOnlineAt, &d.BatteryLevel, &d.FirmwareVersion,
		&d.ExtraData, &d.CreatedAt, &d.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("device not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query device: %w", err)
	}

	return &d, nil
}

// AcceptDevices accepts devices
func (s *Store) AcceptDevices(ctx context.Context, devices []AcceptanceDeviceInput, operatorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, device := range devices {
		// Insert device
		query := `
			INSERT INTO badge_devices (device_no, manufacturer_code, model, status, accepted_at, created_at, updated_at)
			VALUES ($1, $2, $3, 'ready', NOW(), NOW(), NOW())
			RETURNING id
		`
		var deviceID int64
		err := tx.QueryRow(ctx, query, device.DeviceNo, device.ManufacturerCode, device.Model).Scan(&deviceID)
		if err != nil {
			return fmt.Errorf("failed to insert device: %w", err)
		}

		// Insert lifecycle log
		logQuery := `
			INSERT INTO badge_device_lifecycle_logs (device_id, action, to_status, operator_id, created_at)
			VALUES ($1, 'acceptance', 'ready', $2, NOW())
		`
		if _, err := tx.Exec(ctx, logQuery, deviceID, operatorID); err != nil {
			return fmt.Errorf("failed to insert lifecycle log: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// AssignToTenant assigns devices to tenant
func (s *Store) AssignToTenant(ctx context.Context, deviceIDs []int64, tenantID, operatorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, deviceID := range deviceIDs {
		// Update device
		query := `
			UPDATE badge_devices
			SET tenant_id = $1, status = 'in_use', current_status = 'in_use', lifecycle_status = 'active',
			    assignment_status = 'tenant', assigned_to_tenant_at = NOW(), updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL
		`
		result, err := tx.Exec(ctx, query, tenantID, deviceID)
		if err != nil {
			return fmt.Errorf("failed to update device: %w", err)
		}
		if result.RowsAffected() == 0 {
			return fmt.Errorf("device not found: %d", deviceID)
		}

		// Insert lifecycle log
		logQuery := `
			INSERT INTO badge_device_lifecycle_logs (device_id, action, to_status, tenant_id, operator_id, created_at)
			VALUES ($1, 'assign_tenant', 'in_use', $2, $3, NOW())
		`
		if _, err := tx.Exec(ctx, logQuery, deviceID, tenantID, operatorID); err != nil {
			return fmt.Errorf("failed to insert lifecycle log: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// AssignToEmployee assigns devices to employee
func (s *Store) AssignToEmployee(ctx context.Context, deviceIDs []int64, employeeID, operatorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, deviceID := range deviceIDs {
		// Update device
		query := `
			UPDATE badge_devices
			SET employee_id = $1, status = 'in_use', current_status = 'in_use', lifecycle_status = 'active',
			    assignment_status = 'employee', assigned_to_emp_at = NOW(), updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL
		`
		result, err := tx.Exec(ctx, query, employeeID, deviceID)
		if err != nil {
			return fmt.Errorf("failed to update device: %w", err)
		}
		if result.RowsAffected() == 0 {
			return fmt.Errorf("device not found: %d", deviceID)
		}

		// Insert lifecycle log
		logQuery := `
			INSERT INTO badge_device_lifecycle_logs (device_id, action, to_status, employee_id, operator_id, created_at)
			VALUES ($1, 'assign_employee', 'in_use', $2, $3, NOW())
		`
		if _, err := tx.Exec(ctx, logQuery, deviceID, employeeID, operatorID); err != nil {
			return fmt.Errorf("failed to insert lifecycle log: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ReclaimFromEmployee reclaims devices from employee
func (s *Store) ReclaimFromEmployee(ctx context.Context, deviceIDs []int64, operatorID int64, notes *string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, deviceID := range deviceIDs {
		// Update device
		query := `
			UPDATE badge_devices
			SET status = 'ready',
			    current_status = 'ready',
			    lifecycle_status = 'active',
			    assignment_status = CASE WHEN tenant_id IS NULL THEN 'unassigned' ELSE 'tenant' END,
			    inspection_result = COALESCE(inspection_result, 'pass'),
			    employee_id = NULL,
			    employee_name = NULL,
			    employee_phone = NULL,
			    assigned_to_emp_at = NULL,
			    updated_at = NOW()
			WHERE id = $1 AND deleted_at IS NULL
		`
		result, err := tx.Exec(ctx, query, deviceID)
		if err != nil {
			return fmt.Errorf("failed to update device: %w", err)
		}
		if result.RowsAffected() == 0 {
			return fmt.Errorf("device not found: %d", deviceID)
		}

		// Insert lifecycle log
		logQuery := `
			INSERT INTO badge_device_lifecycle_logs (device_id, action, to_status, operator_id, notes, created_at)
			VALUES ($1, 'reclaim_employee', 'ready', $2, $3, NOW())
		`
		if _, err := tx.Exec(ctx, logQuery, deviceID, operatorID, notes); err != nil {
			return fmt.Errorf("failed to insert lifecycle log: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ReclaimFromTenant reclaims devices from tenant
func (s *Store) ReclaimFromTenant(ctx context.Context, deviceIDs []int64, operatorID int64, notes *string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, deviceID := range deviceIDs {
		// Update device
		query := `
			UPDATE badge_devices
			SET status = 'ready',
			    current_status = 'ready',
			    lifecycle_status = 'active',
			    assignment_status = 'unassigned',
			    inspection_result = COALESCE(inspection_result, 'pass'),
			    tenant_id = NULL,
			    tenant_name = NULL,
			    employee_id = NULL,
			    employee_name = NULL,
			    employee_phone = NULL,
			    assigned_to_tenant_at = NULL,
			    assigned_to_emp_at = NULL,
			    updated_at = NOW()
			WHERE id = $1 AND deleted_at IS NULL
		`
		result, err := tx.Exec(ctx, query, deviceID)
		if err != nil {
			return fmt.Errorf("failed to update device: %w", err)
		}
		if result.RowsAffected() == 0 {
			return fmt.Errorf("device not found: %d", deviceID)
		}

		// Insert lifecycle log
		logQuery := `
			INSERT INTO badge_device_lifecycle_logs (device_id, action, to_status, operator_id, notes, created_at)
			VALUES ($1, 'reclaim_tenant', 'ready', $2, $3, NOW())
		`
		if _, err := tx.Exec(ctx, logQuery, deviceID, operatorID, notes); err != nil {
			return fmt.Errorf("failed to insert lifecycle log: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetDashboardSummary retrieves dashboard summary
func (s *Store) GetDashboardSummary(ctx context.Context) (*DashboardSummaryResponse, error) {
	query := `
		SELECT
			COUNT(*) as total_devices,
			COUNT(CASE WHEN status = 'pending' THEN 1 END) as pending_acceptance,
			COUNT(CASE WHEN status = 'ready' THEN 1 END) as pending_assignment,
			COUNT(CASE WHEN status = 'in_use' THEN 1 END) as in_use,
			COUNT(CASE WHEN status = 'blocked' THEN 1 END) as maintenance,
			COUNT(CASE WHEN status = 'retired' THEN 1 END) as retired
		FROM badge_devices
		WHERE deleted_at IS NULL
	`

	var summary DashboardSummaryResponse
	err := s.pool.QueryRow(ctx, query).Scan(
		&summary.TotalDevices, &summary.PendingAcceptance, &summary.PendingAssignment,
		&summary.InUse, &summary.Maintenance, &summary.Retired,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get dashboard summary: %w", err)
	}

	return &summary, nil
}

// Recording Control Methods

// CreateRecordingControlLog creates a recording control log
func (s *Store) CreateRecordingControlLog(ctx context.Context, deviceNo string, action string, status string, operatorID *int64, errorMsg *string, extraData JSONObject) error {
	query := `
		INSERT INTO badge_recording_control_logs (device_no, action, status, operator_id, error_msg, extra_data, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`

	_, err := s.pool.Exec(ctx, query, deviceNo, action, status, operatorID, errorMsg, extraData)
	if err != nil {
		return fmt.Errorf("failed to create recording control log: %w", err)
	}

	return nil
}

// UpsertRecordingFromAudioCallback ingests an audio callback as recordings + smart_badge_audio_events.
func (s *Store) UpsertRecordingFromAudioCallback(ctx context.Context, payload CallbackPayload) (*AudioCallbackIngestResult, error) {
	deviceNo := strings.TrimSpace(payload.DeviceNo)
	if deviceNo == "" {
		return nil, fmt.Errorf("device_no is required")
	}

	data := payload.Data
	if data == nil {
		data = JSONObject{}
	}
	eventID := firstNonEmptyCallback(pickString(data, "event_id"), pickString(data, "eventId"))
	orderNo := firstNonEmptyCallback(pickString(data, "order_no"), pickString(data, "orderNo"), eventID)
	fileURL := firstNonEmptyCallback(pickString(data, "file_url"), pickString(data, "fileUrl"), pickString(data, "audio_file"))
	if orderNo == "" {
		return nil, fmt.Errorf("missing order_no/event_id")
	}
	if fileURL == "" {
		return nil, fmt.Errorf("missing file_url")
	}
	originalFileURL := fileURL
	fileName := firstNonEmptyCallback(pickString(data, "file_name"), pickString(data, "fileName"))
	if fileName == "" {
		base := path.Base(fileURL)
		if strings.TrimSpace(base) != "" && base != "." && base != "/" {
			fileName = base
		} else {
			fileName = orderNo + ".mp3"
		}
	}
	seconds := pickInt(data, "seconds")
	if seconds <= 0 {
		seconds = pickInt(data, "duration")
	}
	if seconds <= 0 {
		seconds = 1
	}
	startTime := parseOptionalTime(firstNonEmptyCallback(pickString(data, "start_time"), pickString(data, "startTime")))
	endTime := parseOptionalTime(firstNonEmptyCallback(pickString(data, "end_time"), pickString(data, "endTime"), pickString(data, "stop_time"), pickString(data, "stopTime")))
	appID := firstNonEmptyCallback(pickString(data, "app_id"), pickString(data, "appId"), "unknown")

	var tenantID, employeeID int64
	if err := s.pool.QueryRow(ctx, `
		SELECT tenant_id, employee_id
		FROM badge_devices
		WHERE device_no = $1
		  AND tenant_id IS NOT NULL
		  AND employee_id IS NOT NULL
		  AND deleted_at IS NULL
		ORDER BY updated_at DESC
		LIMIT 1
	`, deviceNo).Scan(&tenantID, &employeeID); err != nil {
		return nil, fmt.Errorf("device mapping not ready for %s: %w", deviceNo, err)
	}
	normalizedURL, ossKey, err := normalizeAudioToOwnedOSS(ctx, tenantID, orderNo, fileName, originalFileURL)
	if err != nil {
		return nil, fmt.Errorf("normalize callback audio to owned oss: %w", err)
	}
	fileURL = normalizedURL

	var recordingID int64
	var created bool
	if err := s.pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO recordings (
				tenant_id, employee_id, file_url, file_name, duration, mime_type,
				source, business_scope, status, transcription_status, cleaned_transcription_status, analysis_status,
				recorded_at, order_no, oss_key, created_at, updated_at
			)
			SELECT
				$1, $2, $3, $4, $5, 'audio/mpeg',
				'smart_badge',
				CASE
					WHEN EXISTS (
						SELECT 1 FROM inst_employee_roles ier
						WHERE ier.tenant_id = $1 AND ier.employee_id = $2
						  AND lower(ier.role_code) IN ('frontdesk','receptionist','reception')
					) THEN 'frontdesk'
					WHEN EXISTS (
						SELECT 1 FROM inst_employee_roles ier
						WHERE ier.tenant_id = $1 AND ier.employee_id = $2
						  AND lower(ier.role_code) IN ('doctor','doctor_assistant')
					) THEN 'doctor'
					WHEN EXISTS (
						SELECT 1 FROM inst_employee_roles ier
						WHERE ier.tenant_id = $1 AND ier.employee_id = $2
						  AND lower(ier.role_code) IN ('consultant')
					) THEN 'consultant'
					WHEN EXISTS (
						SELECT 1 FROM inst_employee_roles ier
						WHERE ier.tenant_id = $1 AND ier.employee_id = $2
						  AND lower(ier.role_code) IN ('therapist')
					) THEN 'therapist'
					WHEN EXISTS (
						SELECT 1 FROM inst_employee_roles ier
						WHERE ier.tenant_id = $1 AND ier.employee_id = $2
						  AND lower(ier.role_code) IN ('nurse')
					) THEN 'nurse'
					WHEN EXISTS (
						SELECT 1 FROM inst_employee_roles ier
						WHERE ier.tenant_id = $1 AND ier.employee_id = $2
						  AND lower(ier.role_code) IN ('lingce_sales')
					) THEN 'lingce_sales'
					ELSE 'unknown'
				END,
				'uploaded', 'queued', 'pending', 'pending',
				$6::timestamp, $7::text, NULLIF($8::text, ''), NOW(), NOW()
			WHERE NOT EXISTS (
				SELECT 1 FROM recordings WHERE order_no = $7::text
			)
			RETURNING id
		)
		SELECT id, true FROM ins
		UNION ALL
		SELECT id, false FROM recordings
		WHERE order_no = $7::text AND NOT EXISTS (SELECT 1 FROM ins)
		LIMIT 1
	`, tenantID, employeeID, fileURL, fileName, seconds, startTime, orderNo, ossKey).Scan(&recordingID, &created); err != nil {
		return nil, fmt.Errorf("upsert recording from callback: %w", err)
	}

	_, _ = s.pool.Exec(ctx, `
		INSERT INTO smart_badge_audio_events (
			app_id, event_id, device_no, order_no, audio_file, start_time, end_time,
			seconds, raw_payload, status, recording_id, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6::timestamp, $7::timestamp,
			$8, $9::jsonb, 'received', $10, NOW(), NOW()
		)
		ON CONFLICT (app_id, event_id) DO UPDATE SET
			device_no = EXCLUDED.device_no,
			order_no = EXCLUDED.order_no,
			audio_file = EXCLUDED.audio_file,
			start_time = COALESCE(EXCLUDED.start_time, smart_badge_audio_events.start_time),
			end_time = COALESCE(EXCLUDED.end_time, smart_badge_audio_events.end_time),
			seconds = COALESCE(EXCLUDED.seconds, smart_badge_audio_events.seconds),
			raw_payload = EXCLUDED.raw_payload,
			recording_id = COALESCE(smart_badge_audio_events.recording_id, EXCLUDED.recording_id),
			updated_at = NOW()
	`, appID, eventID, deviceNo, orderNo, originalFileURL, startTime, endTime, seconds, data, recordingID)

	return &AudioCallbackIngestResult{
		RecordingID: recordingID,
		TenantID:    tenantID,
		Created:     created,
	}, nil
}

func normalizeAudioToOwnedOSS(ctx context.Context, tenantID int64, orderNo, fileName, sourceURL string) (string, string, error) {
	endpoint := firstNonEmptyEnv("OSS_ENDPOINT", "ALIYUN_OSS_ENDPOINT")
	bucket := firstNonEmptyEnv("OSS_BUCKET", "ALIYUN_OSS_BUCKET")
	accessKeyID := firstNonEmptyEnv("OSS_ACCESS_KEY_ID", "ALIYUN_OSS_ACCESS_KEY_ID")
	accessKeySecret := firstNonEmptyEnv("OSS_ACCESS_KEY_SECRET", "ALIYUN_OSS_ACCESS_KEY_SECRET")
	if endpoint == "" || bucket == "" || accessKeyID == "" || accessKeySecret == "" {
		return "", "", fmt.Errorf("OSS env is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("build source request: %w", err)
	}
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download source audio: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("download source audio status=%d", resp.StatusCode)
	}

	maxBytes := int64(150 * 1024 * 1024)
	if resp.ContentLength > maxBytes {
		return "", "", fmt.Errorf("source audio too large: %d", resp.ContentLength)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("read source audio: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return "", "", fmt.Errorf("source audio exceeds max bytes")
	}

	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(fileName)))
	if ext == "" {
		ext = ".mp3"
	}
	ossKey := ossutil.GenerateObjectKey(fmt.Sprintf("recordings/%d/%s", tenantID, time.Now().UTC().Format("2006/01/02")), ext)
	clientOSS, err := ossutil.NewClient(endpoint, accessKeyID, accessKeySecret, bucket)
	if err != nil {
		return "", "", fmt.Errorf("init oss client: %w", err)
	}
	ownedURL, err := clientOSS.UploadBytes(ctx, ossKey, body, &ossutil.UploadOptions{
		ContentType: "audio/mpeg",
		Metadata: map[string]string{
			"source-order-no": strings.TrimSpace(orderNo),
			"source-url":      sourceURL,
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("upload source audio to oss: %w", err)
	}
	return ownedURL, ossKey, nil
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func firstNonEmptyCallback(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func pickString(data JSONObject, key string) string {
	v, ok := data[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}

func pickInt(data JSONObject, key string) int {
	v, ok := data[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func parseOptionalTime(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return &t
		}
	}
	return nil
}

// ListRecordingControlLogs retrieves a paginated list of recording control logs
func (s *Store) ListRecordingControlLogs(ctx context.Context, req RecordingControlLogListRequest) ([]*BadgeRecordingControlLog, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.EmployeeID != nil {
		conditions = append(conditions, fmt.Sprintf("employee_id = $%d", argIndex))
		args = append(args, *req.EmployeeID)
		argIndex++
	}

	if req.DeviceNo != nil {
		conditions = append(conditions, fmt.Sprintf("device_no = $%d", argIndex))
		args = append(args, *req.DeviceNo)
		argIndex++
	}

	if req.Action != nil {
		conditions = append(conditions, fmt.Sprintf("action = $%d", argIndex))
		args = append(args, *req.Action)
		argIndex++
	}

	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}

	if req.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIndex))
		args = append(args, *req.StartDate)
		argIndex++
	}

	if req.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIndex))
		args = append(args, *req.EndDate)
		argIndex++
	}

	whereClause := "1=1"
	if len(conditions) > 0 {
		whereClause = strings.Join(conditions, " AND ")
	}

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM badge_recording_control_logs WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count logs: %w", err)
	}

	// Query logs
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, device_no, device_id, tenant_id, employee_id, action, status, operator_id,
		       error_msg, extra_data, created_at
		FROM badge_recording_control_logs
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query logs: %w", err)
	}
	defer rows.Close()

	var logs []*BadgeRecordingControlLog
	for rows.Next() {
		var l BadgeRecordingControlLog
		if err := rows.Scan(&l.ID, &l.DeviceNo, &l.DeviceID, &l.TenantID, &l.EmployeeID,
			&l.Action, &l.Status, &l.OperatorID, &l.ErrorMsg, &l.ExtraData, &l.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan log: %w", err)
		}
		logs = append(logs, &l)
	}

	return logs, total, nil
}

func (s *Store) GetLatestAudioEventStatus(ctx context.Context, deviceNo string) (bool, bool, error) {
	var status string
	var recordingID *int64
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT status, recording_id, created_at
		FROM smart_badge_audio_events
		WHERE device_no = $1
		  AND created_at >= (NOW() - INTERVAL '30 minutes')
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, deviceNo).Scan(&status, &recordingID, &createdAt)
	if err == pgx.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("failed to query latest audio event: %w", err)
	}
	callbackOK := strings.TrimSpace(status) != ""
	ingestOK := recordingID != nil && *recordingID > 0
	return callbackOK, ingestOK, nil
}

type RecordingControlState struct {
	Action    string
	CreatedAt time.Time
}

func (s *Store) GetLatestRecordingControlState(ctx context.Context, deviceNo string) (*RecordingControlState, error) {
	var state RecordingControlState
	err := s.pool.QueryRow(ctx, `
		SELECT action, created_at
		FROM badge_recording_control_logs
		WHERE device_no = $1
		  AND status = 'success'
		  AND action IN ('start', 'stop')
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, deviceNo).Scan(&state.Action, &state.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query latest recording control state: %w", err)
	}
	return &state, nil
}

// Manufacturer Methods

// ListManufacturers retrieves all manufacturers
func (s *Store) ListManufacturers(ctx context.Context) ([]*BadgeManufacturer, error) {
	query := `
		SELECT id, code, name, contact_person, contact_phone, contact_email, api_endpoint,
		       api_key, is_active, config, created_at, updated_at
		FROM badge_manufacturers
		ORDER BY name
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query manufacturers: %w", err)
	}
	defer rows.Close()

	manufacturers := make([]*BadgeManufacturer, 0)
	for rows.Next() {
		var m BadgeManufacturer
		if err := rows.Scan(&m.ID, &m.Code, &m.Name, &m.ContactPerson, &m.ContactPhone,
			&m.ContactEmail, &m.APIEndpoint, &m.APIKey, &m.IsActive, &m.Config,
			&m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan manufacturer: %w", err)
		}
		manufacturers = append(manufacturers, &m)
	}

	return manufacturers, nil
}

// GetManufacturerByCode retrieves a manufacturer by code
func (s *Store) GetManufacturerByCode(ctx context.Context, code string) (*BadgeManufacturer, error) {
	query := `
		SELECT id, code, name, contact_person, contact_phone, contact_email, api_endpoint,
		       api_key, is_active, config, created_at, updated_at
		FROM badge_manufacturers
		WHERE code = $1
	`

	var m BadgeManufacturer
	err := s.pool.QueryRow(ctx, query, code).Scan(
		&m.ID, &m.Code, &m.Name, &m.ContactPerson, &m.ContactPhone,
		&m.ContactEmail, &m.APIEndpoint, &m.APIKey, &m.IsActive, &m.Config,
		&m.CreatedAt, &m.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("manufacturer not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query manufacturer: %w", err)
	}

	return &m, nil
}

// UpdateManufacturerConfig updates manufacturer config
func (s *Store) UpdateManufacturerConfig(ctx context.Context, code string, config JSONObject) error {
	query := `
		UPDATE badge_manufacturers
		SET config = $1, updated_at = NOW()
		WHERE code = $2
	`

	result, err := s.pool.Exec(ctx, query, config, code)
	if err != nil {
		return fmt.Errorf("failed to update manufacturer config: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("manufacturer not found")
	}

	return nil
}

// GetLifecycleLogs retrieves lifecycle logs for a device
func (s *Store) GetLifecycleLogs(ctx context.Context, deviceID int64) ([]*BadgeDeviceLifecycleLog, error) {
	query := `
		SELECT id, device_id, action, from_status, to_status, tenant_id, employee_id,
		       operator_id, notes, extra_data, created_at
		FROM badge_device_lifecycle_logs
		WHERE device_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query lifecycle logs: %w", err)
	}
	defer rows.Close()

	var logs []*BadgeDeviceLifecycleLog
	for rows.Next() {
		var l BadgeDeviceLifecycleLog
		if err := rows.Scan(&l.ID, &l.DeviceID, &l.Action, &l.FromStatus, &l.ToStatus,
			&l.TenantID, &l.EmployeeID, &l.OperatorID, &l.Notes, &l.ExtraData, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan lifecycle log: %w", err)
		}
		logs = append(logs, &l)
	}

	return logs, nil
}

// GetRecordingStats retrieves recording statistics for a tenant
func (s *Store) GetRecordingStats(ctx context.Context, tenantID int64, startDate, endDate time.Time) (*RecordingStatsResponse, error) {
	stats := &RecordingStatsResponse{
		Summary:     RecordingStatsSummary{},
		DailyTrends: []DailyTrend{},
		ByDevice:    []DeviceStats{},
		ByEmployee:  []EmployeeStats{},
	}

	// Calculate previous period for comparison
	duration := endDate.Sub(startDate)
	prevStartDate := startDate.Add(-duration)
	prevEndDate := startDate

	// 1. Summary statistics
	summaryQuery := `
		SELECT
			COUNT(*) as total_recordings,
			COALESCE(AVG(duration), 0) as avg_duration_seconds
		FROM recordings
		WHERE tenant_id = $1
			AND created_at >= $2
			AND created_at < $3
	`

	var totalRecordings int
	var avgDuration float64
	err := s.pool.QueryRow(ctx, summaryQuery, tenantID, startDate, endDate).Scan(&totalRecordings, &avgDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to get summary stats: %w", err)
	}

	// Previous period average duration
	var prevAvgDuration float64
	var prevTotalRecordings int
	err = s.pool.QueryRow(ctx, summaryQuery, tenantID, prevStartDate, prevEndDate).Scan(&prevTotalRecordings, &prevAvgDuration)
	if err != nil {
		prevAvgDuration = 0
	}

	// Active devices count (via smart_badge_audio_events)
	activeDevicesQuery := `
		SELECT COUNT(DISTINCT sbae.device_no)
		FROM recordings r
		INNER JOIN smart_badge_audio_events sbae
			ON (sbae.recording_id = r.id OR (sbae.recording_id IS NULL AND sbae.order_no = r.order_no))
		WHERE r.tenant_id = $1
			AND r.created_at >= $2
			AND r.created_at < $3
			AND sbae.device_no IS NOT NULL
	`
	var activeDeviceCount int
	err = s.pool.QueryRow(ctx, activeDevicesQuery, tenantID, startDate, endDate).Scan(&activeDeviceCount)
	if err != nil {
		// If smart_badge_audio_events doesn't exist or no data, set to 0
		activeDeviceCount = 0
	}

	// Total devices count
	totalDevicesQuery := `SELECT COUNT(*) FROM badge_devices WHERE tenant_id = $1 AND deleted_at IS NULL`
	var totalDeviceCount int
	err = s.pool.QueryRow(ctx, totalDevicesQuery, tenantID).Scan(&totalDeviceCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get total devices: %w", err)
	}

	// Active employees count
	activeEmployeesQuery := `
		SELECT COUNT(DISTINCT employee_id)
		FROM recordings
		WHERE tenant_id = $1
			AND created_at >= $2
			AND created_at < $3
			AND employee_id IS NOT NULL
	`
	var activeEmployeeCount int
	err = s.pool.QueryRow(ctx, activeEmployeesQuery, tenantID, startDate, endDate).Scan(&activeEmployeeCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get active employees: %w", err)
	}

	// Total employees count (employees with assigned devices)
	totalEmployeesQuery := `
		SELECT COUNT(DISTINCT employee_id)
		FROM badge_devices
		WHERE tenant_id = $1
			AND employee_id IS NOT NULL
			AND deleted_at IS NULL
	`
	var totalEmployeeCount int
	err = s.pool.QueryRow(ctx, totalEmployeesQuery, tenantID).Scan(&totalEmployeeCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get total employees: %w", err)
	}

	stats.Summary = RecordingStatsSummary{
		TotalRecordings:              totalRecordings,
		ActiveDeviceCount:            activeDeviceCount,
		TotalDeviceCount:             totalDeviceCount,
		ActiveEmployeeCount:          activeEmployeeCount,
		TotalEmployeeCount:           totalEmployeeCount,
		AvgDurationSeconds:           int(avgDuration),
		PrevPeriodAvgDurationSeconds: int(prevAvgDuration),
	}

	// 2. Daily trends (via smart_badge_audio_events)
	dailyTrendsQuery := `
		SELECT
			DATE(r.created_at) as date,
			COUNT(*) as recording_count,
			COUNT(DISTINCT sbae.device_no) as active_device_count,
			COALESCE(SUM(r.duration), 0) as total_duration_seconds
		FROM recordings r
		LEFT JOIN smart_badge_audio_events sbae
			ON (sbae.recording_id = r.id OR (sbae.recording_id IS NULL AND sbae.order_no = r.order_no))
		WHERE r.tenant_id = $1
			AND r.created_at >= $2
			AND r.created_at < $3
		GROUP BY DATE(r.created_at)
		ORDER BY date
	`

	rows, err := s.pool.Query(ctx, dailyTrendsQuery, tenantID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get daily trends: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var trend DailyTrend
		var date time.Time
		if err := rows.Scan(&date, &trend.RecordingCount, &trend.ActiveDeviceCount, &trend.TotalDurationSeconds); err != nil {
			return nil, fmt.Errorf("failed to scan daily trend: %w", err)
		}
		trend.Date = date.Format("2006-01-02")
		stats.DailyTrends = append(stats.DailyTrends, trend)
	}

	// 3. By device statistics (via smart_badge_audio_events)
	byDeviceQuery := `
		SELECT
			bd.id as device_id,
			bd.device_no,
			bd.employee_id,
			bd.employee_name,
			COUNT(r.id) as recording_count,
			COALESCE(SUM(r.duration), 0) as total_duration_seconds,
			MAX(r.created_at) as last_recording_at,
			CASE
				WHEN bd.last_online_at > NOW() - INTERVAL '30 minutes' THEN true
				ELSE false
			END as is_online
		FROM badge_devices bd
		LEFT JOIN smart_badge_audio_events sbae ON sbae.device_no = bd.device_no
		LEFT JOIN recordings r ON (
			r.id = sbae.recording_id
			OR (sbae.recording_id IS NULL AND r.order_no = sbae.order_no)
		)
			AND r.tenant_id = $1
			AND r.created_at >= $2
			AND r.created_at < $3
		WHERE bd.tenant_id = $1
			AND bd.deleted_at IS NULL
		GROUP BY bd.id, bd.device_no, bd.employee_id, bd.employee_name, bd.last_online_at
		ORDER BY recording_count ASC
	`

	rows, err = s.pool.Query(ctx, byDeviceQuery, tenantID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get device stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ds DeviceStats
		var lastRecordingAt *time.Time
		if err := rows.Scan(&ds.DeviceID, &ds.DeviceNo, &ds.EmployeeID, &ds.EmployeeName,
			&ds.RecordingCount, &ds.TotalDurationSeconds, &lastRecordingAt, &ds.IsOnline); err != nil {
			return nil, fmt.Errorf("failed to scan device stats: %w", err)
		}
		if lastRecordingAt != nil {
			formatted := lastRecordingAt.Format(time.RFC3339)
			ds.LastRecordingAt = &formatted
		}
		stats.ByDevice = append(stats.ByDevice, ds)
	}

	// 4. By employee statistics (via smart_badge_audio_events)
	byEmployeeQuery := `
		SELECT
			bd.employee_id,
			bd.employee_name,
			d.name as department_name,
			bd.id as device_id,
			bd.device_no,
			COUNT(r.id) as recording_count,
			COUNT(DISTINCT DATE(r.created_at)) as recording_days,
			MAX(r.created_at) as last_recording_at
		FROM badge_devices bd
		LEFT JOIN employees e ON e.id = bd.employee_id
		LEFT JOIN departments d ON d.id = e.department_id
		LEFT JOIN smart_badge_audio_events sbae ON sbae.device_no = bd.device_no
		LEFT JOIN recordings r ON (
			r.id = sbae.recording_id
			OR (sbae.recording_id IS NULL AND r.order_no = sbae.order_no)
		)
			AND r.tenant_id = $1
			AND r.created_at >= $2
			AND r.created_at < $3
		WHERE bd.tenant_id = $1
			AND bd.employee_id IS NOT NULL
			AND bd.deleted_at IS NULL
		GROUP BY bd.employee_id, bd.employee_name, d.name, bd.id, bd.device_no
		ORDER BY recording_count ASC
	`

	rows, err = s.pool.Query(ctx, byEmployeeQuery, tenantID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get employee stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var es EmployeeStats
		var lastRecordingAt *time.Time
		if err := rows.Scan(&es.EmployeeID, &es.EmployeeName, &es.Department, &es.DeviceID, &es.DeviceNo,
			&es.RecordingCount, &es.RecordingDays, &lastRecordingAt); err != nil {
			return nil, fmt.Errorf("failed to scan employee stats: %w", err)
		}
		if lastRecordingAt != nil {
			formatted := lastRecordingAt.Format(time.RFC3339)
			es.LastRecordingAt = &formatted
		}
		stats.ByEmployee = append(stats.ByEmployee, es)
	}

	return stats, nil
}
