package badge

import (
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) resolveManufacturerAppID(ctx context.Context, manufacturerCode string) (string, error) {
	manufacturerCode = strings.TrimSpace(manufacturerCode)
	if manufacturerCode == "" {
		return "", fmt.Errorf("manufacturer_code is required")
	}

	var appID string
	if err := s.pool.QueryRow(ctx, `
		SELECT NULLIF(TRIM(COALESCE(app_id, '')), '')
		FROM badge_manufacturers
		WHERE code = $1
		LIMIT 1
	`, manufacturerCode).Scan(&appID); err != nil {
		if err == pgx.ErrNoRows {
			return "", fmt.Errorf("manufacturer not found: %s", manufacturerCode)
		}
		return "", fmt.Errorf("failed to resolve manufacturer app_id: %w", err)
	}
	if strings.EqualFold(strings.TrimSpace(appID), manufacturerCode) {
		return "", fmt.Errorf("invalid manufacturer app_id: app_id must be vendor app id, got manufacturer code %s", manufacturerCode)
	}
	return strings.TrimSpace(appID), nil
}

func (s *Store) V2ListDevices(ctx context.Context, req V2DeviceListRequest) ([]*BadgeDevice, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "bd.deleted_at IS NULL")

	// Add tenant filtering for institution users
	if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("bd.tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("bd.status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}
	if req.HealthStatus != nil {
		conditions = append(conditions, fmt.Sprintf("bd.health_status = $%d", argIndex))
		args = append(args, *req.HealthStatus)
		argIndex++
	}
	if req.ManufacturerCode != nil {
		conditions = append(conditions, fmt.Sprintf("bd.manufacturer_code = $%d", argIndex))
		args = append(args, *req.ManufacturerCode)
		argIndex++
	}
	if req.DeviceNo != nil {
		conditions = append(conditions, fmt.Sprintf("bd.device_no ILIKE $%d", argIndex))
		args = append(args, "%"+*req.DeviceNo+"%")
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM badge_devices bd LEFT JOIN employees e ON e.id = bd.employee_id LEFT JOIN departments d ON d.id = e.department_id WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count devices: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT bd.id, bd.device_no, bd.manufacturer_code, bd.manufacturer_name, bd.hardware_model,
		       bd.status, bd.health_status, bd.health_check_result,
		       bd.tenant_id, bd.tenant_name, bd.employee_id, bd.employee_name, bd.employee_phone,
		       e.department_id, d.name as department_name,
		       bd.assigned_at, bd.battery_level, bd.last_check_at, bd.last_online_at, bd.import_batch_no,
		       bd.metadata, bd.created_at, bd.updated_at
		FROM badge_devices bd
		LEFT JOIN employees e ON e.id = bd.employee_id
		LEFT JOIN departments d ON d.id = e.department_id
		WHERE %s
		ORDER BY bd.created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)
	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list devices: %w", err)
	}
	defer rows.Close()

	devices := make([]*BadgeDevice, 0)
	for rows.Next() {
		var d BadgeDevice
		if err := rows.Scan(
			&d.ID, &d.DeviceNo, &d.ManufacturerCode, &d.ManufacturerName, &d.HardwareModel,
			&d.Status, &d.HealthStatus, &d.HealthCheckResult,
			&d.TenantID, &d.TenantName, &d.EmployeeID, &d.EmployeeName, &d.EmployeePhone,
			&d.DepartmentID, &d.DepartmentName,
			&d.AssignedAt, &d.BatteryLevel, &d.LastCheckAt, &d.LastOnlineAt, &d.ImportBatchNo,
			&d.Metadata, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan device: %w", err)
		}
		d.HealthStatus = normalizeHealthStatus(d.HealthStatus)
		normalizeShanghaiTimePtr(&d.AssignedAt)
		normalizeShanghaiTimePtr(&d.LastCheckAt)
		normalizeShanghaiTimePtr(&d.LastOnlineAt)
		normalizeShanghaiTimeValue(&d.CreatedAt)
		normalizeShanghaiTimeValue(&d.UpdatedAt)
		devices = append(devices, &d)
	}
	return devices, total, nil
}

func (s *Store) V2GetDeviceByID(ctx context.Context, id int64) (*BadgeDevice, []*BadgeDeviceLog, error) {
	var d BadgeDevice
	err := s.pool.QueryRow(ctx, `
		SELECT id, device_no, manufacturer_code, manufacturer_name, hardware_model,
		       status, health_status, health_check_result,
		       tenant_id, tenant_name, employee_id, employee_name, employee_phone,
		       assigned_at, battery_level, last_check_at, last_online_at, import_batch_no,
		       metadata, created_at, updated_at
		FROM badge_devices
		WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(
		&d.ID, &d.DeviceNo, &d.ManufacturerCode, &d.ManufacturerName, &d.HardwareModel,
		&d.Status, &d.HealthStatus, &d.HealthCheckResult,
		&d.TenantID, &d.TenantName, &d.EmployeeID, &d.EmployeeName, &d.EmployeePhone,
		&d.AssignedAt, &d.BatteryLevel, &d.LastCheckAt, &d.LastOnlineAt, &d.ImportBatchNo,
		&d.Metadata, &d.CreatedAt, &d.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil, fmt.Errorf("device not found")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get device: %w", err)
	}
	d.HealthStatus = normalizeHealthStatus(d.HealthStatus)
	normalizeShanghaiTimePtr(&d.AssignedAt)
	normalizeShanghaiTimePtr(&d.LastCheckAt)
	normalizeShanghaiTimePtr(&d.LastOnlineAt)
	normalizeShanghaiTimeValue(&d.CreatedAt)
	normalizeShanghaiTimeValue(&d.UpdatedAt)

	rows, err := s.pool.Query(ctx, `
		SELECT id, device_id, device_no, operation, from_status, to_status,
		       operator_id, operator_name, operator_type, detail, created_at
		FROM badge_device_logs
		WHERE device_id = $1
		ORDER BY created_at DESC
		LIMIT 20
	`, id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get device logs: %w", err)
	}
	defer rows.Close()
	logs := make([]*BadgeDeviceLog, 0, 20)
	for rows.Next() {
		var l BadgeDeviceLog
		if err := rows.Scan(&l.ID, &l.DeviceID, &l.DeviceNo, &l.Operation, &l.FromStatus, &l.ToStatus, &l.OperatorID, &l.OperatorName, &l.OperatorType, &l.Detail, &l.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("failed to scan device log: %w", err)
		}
		normalizeShanghaiTimeValue(&l.CreatedAt)
		logs = append(logs, &l)
	}
	return &d, logs, nil
}

func (s *Store) V2ImportDevices(ctx context.Context, req V2BatchImportRequest, operatorID int64, operatorName string) (int, int, []string, []int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, nil, nil, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	batchNo := strings.TrimSpace(req.ImportBatchNo)
	if batchNo == "" {
		batchNo = fmt.Sprintf("BATCH-%s", time.Now().Format("20060102-150405"))
	}
	var manufacturerID int64
	if err := tx.QueryRow(ctx, `
		SELECT id
		FROM badge_manufacturers
		WHERE code = $1
		LIMIT 1
	`, req.ManufacturerCode).Scan(&manufacturerID); err != nil {
		if err == pgx.ErrNoRows {
			return 0, 0, nil, nil, fmt.Errorf("manufacturer not found: %s", req.ManufacturerCode)
		}
		return 0, 0, nil, nil, fmt.Errorf("failed to load manufacturer: %w", err)
	}
	appID, err := s.resolveManufacturerAppID(ctx, req.ManufacturerCode)
	if err != nil {
		return 0, 0, nil, nil, err
	}

	success := 0
	failed := 0
	duplicates := make([]string, 0)
	createdIDs := make([]int64, 0, len(req.Devices))

	for _, item := range req.Devices {
		deviceNo := strings.TrimSpace(item.DeviceNo)
		if deviceNo == "" {
			failed++
			continue
		}
		var existing int64
		err := tx.QueryRow(ctx, `SELECT id FROM badge_devices WHERE device_no = $1 AND deleted_at IS NULL LIMIT 1`, deviceNo).Scan(&existing)
		if err == nil {
			duplicates = append(duplicates, deviceNo)
			continue
		}
		if err != nil && err != pgx.ErrNoRows {
			return 0, 0, nil, nil, fmt.Errorf("failed to check duplicate: %w", err)
		}

		var createdID int64
		deviceUID := fmt.Sprintf("%s:%s:%s", req.ManufacturerCode, appID, deviceNo)
		if err := tx.QueryRow(ctx, `
			INSERT INTO badge_devices (
				manufacturer_id, app_id, device_no, device_uid,
				manufacturer_code, manufacturer_name, hardware_model,
				status, health_status, import_batch_no, metadata, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, NULLIF($7, ''),
				'pending_acceptance', 'unknown', $8, '{}'::jsonb, NOW(), NOW()
			)
			RETURNING id
		`, manufacturerID, appID, deviceNo, deviceUID, req.ManufacturerCode, req.ManufacturerName, item.HardwareModel, batchNo).Scan(&createdID); err != nil {
			failed++
			continue
		}
		success++
		createdIDs = append(createdIDs, createdID)

		if err := s.v2InsertDeviceLogTx(ctx, tx, createdID, deviceNo, "import", nil, strPtr("pending_acceptance"), &operatorID, &operatorName, strPtr("admin"), JSONObject{
			"import_batch_no":   batchNo,
			"manufacturer_code": req.ManufacturerCode,
			"manufacturer_name": req.ManufacturerName,
			"hardware_model":    item.HardwareModel,
		}); err != nil {
			return 0, 0, nil, nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, nil, nil, fmt.Errorf("failed to commit tx: %w", err)
	}
	return success, failed, duplicates, createdIDs, nil
}

func (s *Store) V2UpdateDeviceStatusWithHealth(ctx context.Context, deviceID int64, toStatus string, healthStatus string, healthResult JSONObject, operatorID int64, operatorName, operation string, detail JSONObject) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var deviceNo string
	var fromStatus string
	if err := tx.QueryRow(ctx, `SELECT device_no, status FROM badge_devices WHERE id = $1 AND deleted_at IS NULL`, deviceID).Scan(&deviceNo, &fromStatus); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("device not found")
		}
		return fmt.Errorf("failed to load device: %w", err)
	}

	normalizedHealthStatus := normalizeHealthStatus(healthStatus)
	if _, err := tx.Exec(ctx, `
		UPDATE badge_devices
		SET status = $2,
		    health_status = $3,
		    health_check_result = $4,
		    last_check_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, deviceID, toStatus, normalizedHealthStatus, healthResult); err != nil {
		return fmt.Errorf("failed to update device status: %w", err)
	}

	if err := s.v2InsertDeviceLogTx(ctx, tx, deviceID, deviceNo, operation, &fromStatus, &toStatus, &operatorID, &operatorName, strPtr("admin"), detail); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) V2UpdateRealtimeSnapshot(ctx context.Context, deviceID int64, batteryLevel *int, lastOnlineAt *time.Time, hardwareModel *string) error {
	setClauses := make([]string, 0, 4)
	args := make([]interface{}, 0, 5)
	argIndex := 1

	if batteryLevel != nil {
		setClauses = append(setClauses, fmt.Sprintf("battery_level = $%d", argIndex))
		args = append(args, *batteryLevel)
		argIndex++
	}
	if lastOnlineAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_online_at = $%d", argIndex))
		args = append(args, *lastOnlineAt)
		argIndex++
	}
	if hardwareModel != nil && strings.TrimSpace(*hardwareModel) != "" {
		setClauses = append(setClauses, fmt.Sprintf("hardware_model = $%d", argIndex))
		args = append(args, strings.TrimSpace(*hardwareModel))
		argIndex++
	}
	if len(setClauses) == 0 {
		return nil
	}
	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, deviceID)

	query := fmt.Sprintf("UPDATE badge_devices SET %s WHERE id = $%d AND deleted_at IS NULL", strings.Join(setClauses, ", "), argIndex)
	_, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update realtime snapshot: %w", err)
	}
	return nil
}

func (s *Store) V2UpdateRealtimeSnapshotWithCheckAt(ctx context.Context, deviceID int64, batteryLevel *int, lastOnlineAt *time.Time, hardwareModel *string) error {
	setClauses := make([]string, 0, 5)
	args := make([]interface{}, 0, 6)
	argIndex := 1

	if batteryLevel != nil {
		setClauses = append(setClauses, fmt.Sprintf("battery_level = $%d", argIndex))
		args = append(args, *batteryLevel)
		argIndex++
	}
	if lastOnlineAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_online_at = $%d", argIndex))
		args = append(args, *lastOnlineAt)
		argIndex++
	}
	if hardwareModel != nil && strings.TrimSpace(*hardwareModel) != "" {
		setClauses = append(setClauses, fmt.Sprintf("hardware_model = $%d", argIndex))
		args = append(args, strings.TrimSpace(*hardwareModel))
		argIndex++
	}
	setClauses = append(setClauses, "last_check_at = NOW()")
	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, deviceID)

	query := fmt.Sprintf("UPDATE badge_devices SET %s WHERE id = $%d AND deleted_at IS NULL", strings.Join(setClauses, ", "), argIndex)
	_, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update realtime snapshot with check time: %w", err)
	}
	return nil
}

func (s *Store) V2BatchAssign(ctx context.Context, req V2BatchAssignRequest, operatorID int64, operatorName string) (int, int, []string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, nil, err
	}
	defer tx.Rollback(ctx)

	success := 0
	failed := 0
	errors := make([]string, 0)
	for i, deviceID := range req.DeviceIDs {
		savepoint := fmt.Sprintf("sp_assign_%d", i)
		if _, err := tx.Exec(ctx, "SAVEPOINT "+savepoint); err != nil {
			return success, failed, errors, fmt.Errorf("failed to create savepoint: %w", err)
		}

		var deviceNo string
		var fromStatus string
		err := tx.QueryRow(ctx, `SELECT device_no, status FROM badge_devices WHERE id=$1 AND deleted_at IS NULL`, deviceID).Scan(&deviceNo, &fromStatus)
		if err != nil {
			_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
			_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint)
			failed++
			errors = append(errors, fmt.Sprintf("device %d not found", deviceID))
			continue
		}
		if fromStatus != BadgeStatusInStock {
			_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
			_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint)
			failed++
			errors = append(errors, fmt.Sprintf("device %s status is %s, only in_stock devices can be assigned", deviceNo, fromStatus))
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE badge_devices
			SET status='assigned',
			    tenant_id=$2,
			    tenant_name=$3,
			    employee_id=$4,
			    employee_name=$5,
			    employee_phone=NULLIF($6, ''),
			    assigned_at=NOW(),
			    updated_at=NOW()
			WHERE id=$1
		`, deviceID, req.TenantID, req.TenantName, req.EmployeeID, req.EmployeeName, req.EmployeePhone); err != nil {
			_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
			_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint)
			failed++
			errors = append(errors, fmt.Sprintf("device %s assign failed: %v", deviceNo, err))
			continue
		}
		if err := s.v2InsertDeviceLogTx(ctx, tx, deviceID, deviceNo, "assign", &fromStatus, strPtr("assigned"), &operatorID, &operatorName, strPtr("admin"), JSONObject{
			"tenant_id":      req.TenantID,
			"tenant_name":    req.TenantName,
			"employee_id":    req.EmployeeID,
			"employee_name":  req.EmployeeName,
			"employee_phone": req.EmployeePhone,
		}); err != nil {
			_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
			_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint)
			failed++
			errors = append(errors, fmt.Sprintf("device %s log failed: %v", deviceNo, err))
			continue
		}
		if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
			return success, failed, errors, fmt.Errorf("failed to release savepoint: %w", err)
		}
		success++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, nil, err
	}
	return success, failed, errors, nil
}

func (s *Store) V2BatchReclaim(ctx context.Context, req V2BatchReclaimRequest, operatorID int64, operatorName string) (int, int, []string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, nil, err
	}
	defer tx.Rollback(ctx)
	success := 0
	failed := 0
	errors := make([]string, 0)
	for i, deviceID := range req.DeviceIDs {
		savepoint := fmt.Sprintf("sp_reclaim_%d", i)
		if _, err := tx.Exec(ctx, "SAVEPOINT "+savepoint); err != nil {
			return success, failed, errors, fmt.Errorf("failed to create savepoint: %w", err)
		}

		var deviceNo string
		var fromStatus string
		err := tx.QueryRow(ctx, `SELECT device_no, status FROM badge_devices WHERE id=$1 AND deleted_at IS NULL`, deviceID).Scan(&deviceNo, &fromStatus)
		if err != nil {
			_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
			_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint)
			failed++
			errors = append(errors, fmt.Sprintf("device %d not found", deviceID))
			continue
		}
		if fromStatus != BadgeStatusAssigned && fromStatus != BadgeStatusInStock {
			_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
			_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint)
			failed++
			errors = append(errors, fmt.Sprintf("device %s status is %s, only assigned or in_stock devices can be reclaimed", deviceNo, fromStatus))
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE badge_devices
			SET status='returned',
			    tenant_id=NULL,
			    tenant_name=NULL,
			    employee_id=NULL,
			    employee_name=NULL,
			    employee_phone=NULL,
			    assigned_at=NULL,
			    updated_at=NOW()
			WHERE id=$1
		`, deviceID); err != nil {
			_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
			_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint)
			failed++
			errors = append(errors, fmt.Sprintf("device %s reclaim failed: %v", deviceNo, err))
			continue
		}
		if err := s.v2InsertDeviceLogTx(ctx, tx, deviceID, deviceNo, "reclaim", &fromStatus, strPtr("returned"), &operatorID, &operatorName, strPtr("admin"), JSONObject{"reason": req.Reason}); err != nil {
			_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
			_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint)
			failed++
			errors = append(errors, fmt.Sprintf("device %s log failed: %v", deviceNo, err))
			continue
		}
		if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
			return success, failed, errors, fmt.Errorf("failed to release savepoint: %w", err)
		}
		success++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, nil, err
	}
	return success, failed, errors, nil
}

func (s *Store) V2Transfer(ctx context.Context, deviceID int64, req V2TransferRequest, operatorID int64, operatorName string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var deviceNo string
	var fromStatus string
	if err := tx.QueryRow(ctx, `SELECT device_no, status FROM badge_devices WHERE id=$1 AND deleted_at IS NULL`, deviceID).Scan(&deviceNo, &fromStatus); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("device not found")
		}
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE badge_devices
		SET status='assigned',
		    tenant_id=$2,
		    tenant_name=$3,
		    employee_id=$4,
		    employee_name=$5,
		    assigned_at=NOW(),
		    updated_at=NOW()
		WHERE id=$1
	`, deviceID, req.ToTenantID, req.ToTenantName, req.ToEmployeeID, req.ToEmployeeName); err != nil {
		return err
	}
	if err := s.v2InsertDeviceLogTx(ctx, tx, deviceID, deviceNo, "transfer", &fromStatus, strPtr("assigned"), &operatorID, &operatorName, strPtr("admin"), JSONObject{
		"to_tenant_id":     req.ToTenantID,
		"to_tenant_name":   req.ToTenantName,
		"to_employee_id":   req.ToEmployeeID,
		"to_employee_name": req.ToEmployeeName,
		"reason":           req.Reason,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) V2UpdateDevice(ctx context.Context, id int64, req V2UpdateDeviceRequest, operatorID int64, operatorName string) error {
	setClauses := make([]string, 0, 2)
	args := make([]interface{}, 0, 4)
	argIndex := 1
	if req.HardwareModel != nil {
		setClauses = append(setClauses, fmt.Sprintf("hardware_model = $%d", argIndex))
		args = append(args, *req.HardwareModel)
		argIndex++
	}
	if req.Metadata != nil {
		setClauses = append(setClauses, fmt.Sprintf("metadata = $%d", argIndex))
		args = append(args, req.Metadata)
		argIndex++
	}
	if len(setClauses) == 0 {
		return nil
	}
	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf("UPDATE badge_devices SET %s WHERE id = $%d AND deleted_at IS NULL", strings.Join(setClauses, ", "), argIndex)
	res, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("device not found")
	}
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO badge_device_logs (device_id, device_no, operation, operator_id, operator_name, operator_type, detail, created_at)
		SELECT id, device_no, 'check', $2, $3, 'admin', $4, NOW() FROM badge_devices WHERE id = $1
	`, id, operatorID, operatorName, JSONObject{"action": "update"})
	return nil
}

func (s *Store) V2Dashboard(ctx context.Context) (JSONObject, error) {
	resp := JSONObject{
		"total":           int64(0),
		"by_status":       JSONObject{},
		"by_health":       JSONObject{},
		"assigned_count":  int64(0),
		"available_count": int64(0),
		"today_imported":  int64(0),
		"today_accepted":  int64(0),
		"today_assigned":  int64(0),
	}
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL`).Scan(&total); err != nil {
		return nil, err
	}
	resp["total"] = total
	statusRows, err := s.pool.Query(ctx, `SELECT status, COUNT(*) FROM badge_devices WHERE deleted_at IS NULL GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer statusRows.Close()
	byStatus := JSONObject{}
	for statusRows.Next() {
		var k string
		var v int64
		if err := statusRows.Scan(&k, &v); err != nil {
			return nil, err
		}
		byStatus[k] = v
	}
	resp["by_status"] = byStatus

	healthRows, err := s.pool.Query(ctx, `SELECT health_status, COUNT(*) FROM badge_devices WHERE deleted_at IS NULL GROUP BY health_status`)
	if err != nil {
		return nil, err
	}
	defer healthRows.Close()
	byHealth := JSONObject{
		HealthStatusUnknown: int64(0),
		HealthStatusHealthy: int64(0),
		HealthStatusWarning: int64(0),
		HealthStatusError:   int64(0),
	}
	for healthRows.Next() {
		var k string
		var v int64
		if err := healthRows.Scan(&k, &v); err != nil {
			return nil, err
		}
		nk := normalizeHealthStatus(k)
		current, _ := byHealth[nk].(int64)
		byHealth[nk] = current + v
	}
	resp["by_health"] = byHealth
	var assignedCount int64
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND employee_id IS NOT NULL`).Scan(&assignedCount)
	resp["assigned_count"] = assignedCount
	var availableCount int64
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL AND status = 'in_stock'`).Scan(&availableCount)
	resp["available_count"] = availableCount
	var todayImported int64
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM badge_device_logs WHERE operation = 'import' AND created_at >= CURRENT_DATE`).Scan(&todayImported)
	resp["today_imported"] = todayImported
	var todayAccepted int64
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM badge_device_logs WHERE operation = 'accept' AND created_at >= CURRENT_DATE`).Scan(&todayAccepted)
	resp["today_accepted"] = todayAccepted
	var todayAssigned int64
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM badge_device_logs WHERE operation = 'assign' AND created_at >= CURRENT_DATE`).Scan(&todayAssigned)
	resp["today_assigned"] = todayAssigned
	return resp, nil
}

func (s *Store) V2ListDeviceLogs(ctx context.Context, deviceID int64, operation *string, page, pageSize int) ([]*BadgeDeviceLog, int, error) {
	where := "device_id = $1"
	args := []interface{}{deviceID}
	argIndex := 2
	if operation != nil && strings.TrimSpace(*operation) != "" {
		where += fmt.Sprintf(" AND operation = $%d", argIndex)
		args = append(args, strings.TrimSpace(*operation))
		argIndex++
	}
	var total int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM badge_device_logs WHERE %s", where)
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	query := fmt.Sprintf(`
		SELECT id, device_id, device_no, operation, from_status, to_status, operator_id, operator_name, operator_type, detail, created_at
		FROM badge_device_logs
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argIndex, argIndex+1)
	args = append(args, pageSize, offset)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*BadgeDeviceLog, 0, pageSize)
	for rows.Next() {
		var l BadgeDeviceLog
		if err := rows.Scan(&l.ID, &l.DeviceID, &l.DeviceNo, &l.Operation, &l.FromStatus, &l.ToStatus, &l.OperatorID, &l.OperatorName, &l.OperatorType, &l.Detail, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, &l)
	}
	return items, total, nil
}

func (s *Store) V2ListAllDeviceLogs(ctx context.Context, req V2DeviceLogListRequest) ([]*BadgeDeviceLog, int, error) {
	conditions := []string{"d.deleted_at IS NULL"}
	args := make([]interface{}, 0, 8)
	argIndex := 1

	if req.DeviceID != nil && *req.DeviceID > 0 {
		conditions = append(conditions, fmt.Sprintf("l.device_id = $%d", argIndex))
		args = append(args, *req.DeviceID)
		argIndex++
	}
	if req.DeviceNo != nil && strings.TrimSpace(*req.DeviceNo) != "" {
		conditions = append(conditions, fmt.Sprintf("l.device_no ILIKE $%d", argIndex))
		args = append(args, "%"+strings.TrimSpace(*req.DeviceNo)+"%")
		argIndex++
	}
	if req.ManufacturerCode != nil && strings.TrimSpace(*req.ManufacturerCode) != "" {
		conditions = append(conditions, fmt.Sprintf("d.manufacturer_code = $%d", argIndex))
		args = append(args, strings.TrimSpace(*req.ManufacturerCode))
		argIndex++
	}
	if req.Operation != nil && strings.TrimSpace(*req.Operation) != "" {
		conditions = append(conditions, fmt.Sprintf("l.operation = $%d", argIndex))
		args = append(args, strings.TrimSpace(*req.Operation))
		argIndex++
	}
	if req.OperatorName != nil && strings.TrimSpace(*req.OperatorName) != "" {
		conditions = append(conditions, fmt.Sprintf("COALESCE(l.operator_name,'') ILIKE $%d", argIndex))
		args = append(args, "%"+strings.TrimSpace(*req.OperatorName)+"%")
		argIndex++
	}
	if req.StartDate != nil && strings.TrimSpace(*req.StartDate) != "" {
		conditions = append(conditions, fmt.Sprintf("l.created_at >= $%d::date", argIndex))
		args = append(args, strings.TrimSpace(*req.StartDate))
		argIndex++
	}
	if req.EndDate != nil && strings.TrimSpace(*req.EndDate) != "" {
		conditions = append(conditions, fmt.Sprintf("l.created_at < ($%d::date + INTERVAL '1 day')", argIndex))
		args = append(args, strings.TrimSpace(*req.EndDate))
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")
	countSQL := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM badge_device_logs l
		JOIN badge_devices d ON d.id = l.device_id
		WHERE %s
	`, whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count badge_device_logs: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT l.id, l.device_id, l.device_no, l.operation, l.from_status, l.to_status,
		       l.operator_id, l.operator_name, l.operator_type, l.detail, l.created_at
		FROM badge_device_logs l
		JOIN badge_devices d ON d.id = l.device_id
		WHERE %s
		ORDER BY l.created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)
	queryArgs := append(args, req.PageSize, offset)
	rows, err := s.pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list badge_device_logs: %w", err)
	}
	defer rows.Close()

	items := make([]*BadgeDeviceLog, 0, req.PageSize)
	for rows.Next() {
		var l BadgeDeviceLog
		if err := rows.Scan(&l.ID, &l.DeviceID, &l.DeviceNo, &l.Operation, &l.FromStatus, &l.ToStatus, &l.OperatorID, &l.OperatorName, &l.OperatorType, &l.Detail, &l.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan badge_device_log: %w", err)
		}
		items = append(items, &l)
	}
	return items, total, nil
}

func (s *Store) V2ExportDevicesCSV(ctx context.Context, req V2DeviceListRequest) (string, error) {
	items, _, err := s.V2ListDevices(ctx, req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"id", "device_no", "manufacturer_code", "manufacturer_name", "hardware_model", "status", "health_status", "tenant_id", "tenant_name", "employee_id", "employee_name", "battery_level", "last_online_at", "created_at"})
	for _, d := range items {
		tenantID := ""
		if d.TenantID != nil {
			tenantID = strconv.FormatInt(*d.TenantID, 10)
		}
		employeeID := ""
		if d.EmployeeID != nil {
			employeeID = strconv.FormatInt(*d.EmployeeID, 10)
		}
		battery := ""
		if d.BatteryLevel != nil {
			battery = strconv.Itoa(*d.BatteryLevel)
		}
		lastOnline := ""
		if d.LastOnlineAt != nil {
			lastOnline = d.LastOnlineAt.Format(time.RFC3339)
		}
		_ = w.Write([]string{
			strconv.FormatInt(d.ID, 10),
			d.DeviceNo,
			d.ManufacturerCode,
			valueOrEmptyString(d.ManufacturerName),
			valueOrEmptyString(d.HardwareModel),
			d.Status,
			d.HealthStatus,
			tenantID,
			valueOrEmptyString(d.TenantName),
			employeeID,
			valueOrEmptyString(d.EmployeeName),
			battery,
			lastOnline,
			d.CreatedAt.Format(time.RFC3339),
		})
	}
	w.Flush()
	return b.String(), w.Error()
}

func (s *Store) v2InsertDeviceLogTx(ctx context.Context, tx pgx.Tx, deviceID int64, deviceNo, operation string, fromStatus, toStatus *string, operatorID *int64, operatorName, operatorType *string, detail JSONObject) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO badge_device_logs (
			device_id, device_no, operation, from_status, to_status,
			operator_id, operator_name, operator_type, detail, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
	`, deviceID, deviceNo, operation, fromStatus, toStatus, operatorID, operatorName, operatorType, detail)
	if err != nil {
		return fmt.Errorf("failed to insert badge_device_log: %w", err)
	}
	return nil
}

func strPtr(s string) *string { return &s }

func valueOrEmptyString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
