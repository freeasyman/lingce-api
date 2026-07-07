package badge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func normalizeBadgeStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case BadgeStatusPendingAcceptance:
		return BadgeStatusPendingAcceptance
	case BadgeStatusInStock:
		return BadgeStatusInStock
	case BadgeStatusAssigned:
		return BadgeStatusAssigned
	case BadgeStatusReturned:
		return BadgeStatusReturned
	case BadgeStatusUnusable:
		return BadgeStatusUnusable
	case BadgeStatusRetired:
		return BadgeStatusRetired
	default:
		return ""
	}
}

func (s *Store) RebuildListBadgeDevices(ctx context.Context, req RebuildBadgeDeviceListRequest) ([]*BadgeDeviceV2, int, error) {
	conditions := []string{"bd.deleted_at IS NULL"}
	args := make([]interface{}, 0, 8)
	argIndex := 1

	if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("bd.current_tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}
	if req.EmployeeID != nil {
		conditions = append(conditions, fmt.Sprintf("bd.current_employee_id = $%d", argIndex))
		args = append(args, *req.EmployeeID)
		argIndex++
	}
	if req.BadgeStatus != nil {
		conditions = append(conditions, fmt.Sprintf("bd.badge_status = $%d", argIndex))
		args = append(args, *req.BadgeStatus)
		argIndex++
	}
	if req.HealthLevel != nil {
		conditions = append(conditions, fmt.Sprintf("bd.health_level = $%d", argIndex))
		args = append(args, *req.HealthLevel)
		argIndex++
	}
	if req.DeviceNo != nil {
		conditions = append(conditions, fmt.Sprintf("bd.device_no ILIKE $%d", argIndex))
		args = append(args, "%"+strings.TrimSpace(*req.DeviceNo)+"%")
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM badge_devices_v2 bd WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count badge devices: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	listSQL := fmt.Sprintf(`
		SELECT id, device_no, manufacturer_id, manufacturer_code, manufacturer_name, app_id, device_uid,
		       hardware_model, badge_status, current_tenant_id, current_tenant_name, current_employee_id,
		       current_employee_name, current_employee_phone, assigned_at, health_level, battery_level,
		       last_online_at, last_health_check_at, health_check_result, import_batch_no, acceptance_batch_no,
		       accepted_at, acceptance_result, metadata, created_at, updated_at, deleted_at
		FROM badge_devices_v2 bd
		WHERE %s
		ORDER BY bd.created_at DESC, bd.id DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)
	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list badge devices: %w", err)
	}
	defer rows.Close()

	items := make([]*BadgeDeviceV2, 0, req.PageSize)
	for rows.Next() {
		item, err := scanBadgeDeviceV2(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, nil
}

func (s *Store) RebuildGetBadgeDeviceByID(ctx context.Context, id int64) (*BadgeDeviceV2, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, device_no, manufacturer_id, manufacturer_code, manufacturer_name, app_id, device_uid,
		       hardware_model, badge_status, current_tenant_id, current_tenant_name, current_employee_id,
		       current_employee_name, current_employee_phone, assigned_at, health_level, battery_level,
		       last_online_at, last_health_check_at, health_check_result, import_batch_no, acceptance_batch_no,
		       accepted_at, acceptance_result, metadata, created_at, updated_at, deleted_at
		FROM badge_devices_v2
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	item, err := scanBadgeDeviceV2(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("badge device not found")
		}
		return nil, err
	}
	return item, nil
}

func scanBadgeDeviceV2(row pgx.Row) (*BadgeDeviceV2, error) {
	var item BadgeDeviceV2
	if err := row.Scan(
		&item.ID,
		&item.DeviceNo,
		&item.ManufacturerID,
		&item.ManufacturerCode,
		&item.ManufacturerName,
		&item.AppID,
		&item.DeviceUID,
		&item.HardwareModel,
		&item.BadgeStatus,
		&item.CurrentTenantID,
		&item.CurrentTenantName,
		&item.CurrentEmployeeID,
		&item.CurrentEmployeeName,
		&item.CurrentEmployeePhone,
		&item.AssignedAt,
		&item.HealthLevel,
		&item.BatteryLevel,
		&item.LastOnlineAt,
		&item.LastHealthCheckAt,
		&item.HealthCheckResult,
		&item.ImportBatchNo,
		&item.AcceptanceBatchNo,
		&item.AcceptedAt,
		&item.AcceptanceResult,
		&item.Metadata,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.DeletedAt,
	); err != nil {
		return nil, err
	}
	item.HealthLevel = normalizeHealthStatus(item.HealthLevel)
	return &item, nil
}

func (s *Store) RebuildListBadgeDeviceLogs(ctx context.Context, deviceID int64) ([]*BadgeDeviceLogV2, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, badge_device_id, device_no, operation, from_badge_status, to_badge_status,
		       operator_id, operator_name, operator_type, detail, created_at
		FROM badge_device_logs_v2
		WHERE badge_device_id = $1
		ORDER BY created_at DESC, id DESC
	`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list badge logs: %w", err)
	}
	defer rows.Close()

	items := make([]*BadgeDeviceLogV2, 0)
	for rows.Next() {
		var item BadgeDeviceLogV2
		if err := rows.Scan(
			&item.ID,
			&item.BadgeDeviceID,
			&item.DeviceNo,
			&item.Operation,
			&item.FromBadgeStatus,
			&item.ToBadgeStatus,
			&item.OperatorID,
			&item.OperatorName,
			&item.OperatorType,
			&item.Detail,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan badge log: %w", err)
		}
		items = append(items, &item)
	}
	return items, nil
}

func (s *Store) RebuildListBadgeAssignmentLogs(ctx context.Context, deviceID int64) ([]*BadgeAssignmentLogV2, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, badge_device_id, device_no, tenant_id, tenant_name, employee_id, employee_name,
		       action, reason, operator_id, operator_name, created_at
		FROM badge_assignment_logs_v2
		WHERE badge_device_id = $1
		ORDER BY created_at DESC, id DESC
	`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list assignment logs: %w", err)
	}
	defer rows.Close()

	items := make([]*BadgeAssignmentLogV2, 0)
	for rows.Next() {
		var item BadgeAssignmentLogV2
		if err := rows.Scan(
			&item.ID,
			&item.BadgeDeviceID,
			&item.DeviceNo,
			&item.TenantID,
			&item.TenantName,
			&item.EmployeeID,
			&item.EmployeeName,
			&item.Action,
			&item.Reason,
			&item.OperatorID,
			&item.OperatorName,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan assignment log: %w", err)
		}
		items = append(items, &item)
	}
	return items, nil
}

func (s *Store) RebuildImportBadgeDevices(ctx context.Context, req RebuildBadgeImportRequest, operatorID int64, operatorName string) (*RebuildBadgeImportResponse, error) {
	manufacturerCode := strings.TrimSpace(req.ManufacturerCode)
	if manufacturerCode == "" {
		return nil, fmt.Errorf("manufacturer_code is required")
	}
	if len(req.Devices) == 0 {
		return nil, fmt.Errorf("devices is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	batchNo := strings.TrimSpace(req.ImportBatchNo)
	if batchNo == "" {
		batchNo = fmt.Sprintf("import-%s", time.Now().Format("20060102150405"))
	}

	resp := &RebuildBadgeImportResponse{Duplicates: make([]string, 0)}
	for _, device := range req.Devices {
		deviceNo := strings.TrimSpace(device.DeviceNo)
		if deviceNo == "" {
			resp.FailedCount++
			continue
		}

		var exists int64
		err := tx.QueryRow(ctx, `
			SELECT id FROM badge_devices_v2
			WHERE device_no = $1 AND deleted_at IS NULL
			LIMIT 1
		`, deviceNo).Scan(&exists)
		if err == nil {
			resp.Duplicates = append(resp.Duplicates, deviceNo)
			continue
		}
		if err != nil && err != pgx.ErrNoRows {
			return nil, fmt.Errorf("failed to check duplicate: %w", err)
		}

		var createdID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO badge_devices_v2 (
				device_no, manufacturer_code, manufacturer_name, hardware_model,
				badge_status, health_level, import_batch_no, metadata, created_at, updated_at
			) VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6, $7, '{}'::jsonb, NOW(), NOW())
			RETURNING id
		`, deviceNo, manufacturerCode, strings.TrimSpace(req.ManufacturerName), strings.TrimSpace(device.HardwareModel), BadgeStatusPendingAcceptance, BadgeHealthUnknown, batchNo).Scan(&createdID)
		if err != nil {
			resp.FailedCount++
			continue
		}

		resp.SuccessCount++
		if err := s.rebuildInsertBadgeLogTx(ctx, tx, createdID, deviceNo, "import", nil, strPtr(BadgeStatusPendingAcceptance), &operatorID, &operatorName, JSONObject{
			"import_batch_no":   batchNo,
			"manufacturer_code": manufacturerCode,
			"manufacturer_name": strings.TrimSpace(req.ManufacturerName),
			"hardware_model":    strings.TrimSpace(device.HardwareModel),
		}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit tx: %w", err)
	}
	return resp, nil
}

func (s *Store) RebuildAcceptBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.rebuildChangeBadgeStatus(ctx, id, "accept", BadgeStatusPendingAcceptance, BadgeStatusInStock, reason, operatorID, operatorName, func(ctx context.Context, tx pgx.Tx, device *BadgeDeviceV2) error {
		_, err := tx.Exec(ctx, `
			UPDATE badge_devices_v2
			SET badge_status = $2,
			    accepted_at = NOW(),
			    acceptance_result = 'accepted',
			    updated_at = NOW()
			WHERE id = $1
		`, id, BadgeStatusInStock)
		return err
	})
}

func (s *Store) RebuildRejectAcceptance(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.rebuildChangeBadgeStatus(ctx, id, "reject_acceptance", BadgeStatusPendingAcceptance, BadgeStatusUnusable, reason, operatorID, operatorName, func(ctx context.Context, tx pgx.Tx, device *BadgeDeviceV2) error {
		_, err := tx.Exec(ctx, `
			UPDATE badge_devices_v2
			SET badge_status = $2,
			    acceptance_result = 'rejected',
			    updated_at = NOW()
			WHERE id = $1
		`, id, BadgeStatusUnusable)
		return err
	})
}

func (s *Store) RebuildAssignBadgeDevice(ctx context.Context, id int64, tenantID, employeeID int64, operatorID int64, operatorName string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	device, err := s.rebuildGetBadgeDeviceForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if device.BadgeStatus != BadgeStatusInStock {
		return fmt.Errorf("only in_stock badge can be assigned")
	}

	var tenantName string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(name, '')
		FROM tenants
		WHERE id = $1 AND deleted_at IS NULL
	`, tenantID).Scan(&tenantName); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("tenant not found")
		}
		return fmt.Errorf("failed to load tenant: %w", err)
	}

	var employeeName string
	var employeePhone *string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(
			NULLIF(NULLIF(full_name, 'unknown'), ''),
			NULLIF(NULLIF(name, 'unknown'), ''),
			NULLIF(username, ''),
			NULLIF(phone, ''),
			'未知员工'
		), NULLIF(phone, '')
		FROM employees
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
	`, employeeID, tenantID).Scan(&employeeName, &employeePhone); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("employee not found in tenant")
		}
		return fmt.Errorf("failed to load employee: %w", err)
	}

	var occupiedID int64
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM badge_devices_v2
		WHERE current_employee_id = $1
		  AND badge_status = $2
		  AND deleted_at IS NULL
		  AND id <> $3
		LIMIT 1
	`, employeeID, BadgeStatusAssigned, id).Scan(&occupiedID)
	if err == nil {
		return fmt.Errorf("employee already has an assigned badge")
	}
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("failed to check employee badge occupancy: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE badge_devices_v2
		SET badge_status = $2,
		    current_tenant_id = $3,
		    current_tenant_name = $4,
		    current_employee_id = $5,
		    current_employee_name = $6,
		    current_employee_phone = $7,
		    assigned_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, id, BadgeStatusAssigned, tenantID, tenantName, employeeID, employeeName, employeePhone)
	if err != nil {
		return fmt.Errorf("failed to assign badge: %w", err)
	}

	if err := s.rebuildInsertBadgeLogTx(ctx, tx, device.ID, device.DeviceNo, "assign", &device.BadgeStatus, strPtr(BadgeStatusAssigned), &operatorID, &operatorName, JSONObject{
		"tenant_id":     tenantID,
		"tenant_name":   tenantName,
		"employee_id":   employeeID,
		"employee_name": employeeName,
	}); err != nil {
		return err
	}
	if err := s.rebuildInsertAssignmentLogTx(ctx, tx, device.ID, device.DeviceNo, "assign", &tenantID, tenantName, &employeeID, employeeName, "", &operatorID, &operatorName); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) RebuildReclaimBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	device, err := s.rebuildGetBadgeDeviceForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if device.BadgeStatus != BadgeStatusAssigned {
		return fmt.Errorf("only assigned badge can be reclaimed")
	}

	_, err = tx.Exec(ctx, `
		UPDATE badge_devices_v2
		SET badge_status = $2,
		    current_tenant_id = NULL,
		    current_tenant_name = NULL,
		    current_employee_id = NULL,
		    current_employee_name = NULL,
		    current_employee_phone = NULL,
		    updated_at = NOW()
		WHERE id = $1
	`, id, BadgeStatusReturned)
	if err != nil {
		return fmt.Errorf("failed to reclaim badge: %w", err)
	}

	if err := s.rebuildInsertBadgeLogTx(ctx, tx, device.ID, device.DeviceNo, "reclaim", &device.BadgeStatus, strPtr(BadgeStatusReturned), &operatorID, &operatorName, JSONObject{
		"reason": reason,
	}); err != nil {
		return err
	}
	if err := s.rebuildInsertAssignmentLogTx(ctx, tx, device.ID, device.DeviceNo, "reclaim", nil, "", nil, "", reason, &operatorID, &operatorName); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) RebuildRestockBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.rebuildChangeBadgeStatus(ctx, id, "restock", BadgeStatusReturned, BadgeStatusInStock, reason, operatorID, operatorName, func(ctx context.Context, tx pgx.Tx, device *BadgeDeviceV2) error {
		_, err := tx.Exec(ctx, `
			UPDATE badge_devices_v2
			SET badge_status = $2,
			    updated_at = NOW()
			WHERE id = $1
		`, id, BadgeStatusInStock)
		return err
	})
}

func (s *Store) RebuildMarkBadgeUnusable(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	device, err := s.rebuildGetBadgeDeviceForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if device.BadgeStatus != BadgeStatusPendingAcceptance && device.BadgeStatus != BadgeStatusReturned && device.BadgeStatus != BadgeStatusAssigned {
		return fmt.Errorf("badge status does not allow mark unusable")
	}

	_, err = tx.Exec(ctx, `
		UPDATE badge_devices_v2
		SET badge_status = $2,
		    current_tenant_id = NULL,
		    current_tenant_name = NULL,
		    current_employee_id = NULL,
		    current_employee_name = NULL,
		    current_employee_phone = NULL,
		    updated_at = NOW()
		WHERE id = $1
	`, id, BadgeStatusUnusable)
	if err != nil {
		return fmt.Errorf("failed to mark badge unusable: %w", err)
	}

	if err := s.rebuildInsertBadgeLogTx(ctx, tx, device.ID, device.DeviceNo, "mark_unusable", &device.BadgeStatus, strPtr(BadgeStatusUnusable), &operatorID, &operatorName, JSONObject{
		"reason": reason,
	}); err != nil {
		return err
	}
	if device.BadgeStatus == BadgeStatusAssigned {
		if err := s.rebuildInsertAssignmentLogTx(ctx, tx, device.ID, device.DeviceNo, "reclaim", nil, "", nil, "", reason, &operatorID, &operatorName); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) RebuildRetireBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.rebuildChangeBadgeStatus(ctx, id, "retire", BadgeStatusUnusable, BadgeStatusRetired, reason, operatorID, operatorName, func(ctx context.Context, tx pgx.Tx, device *BadgeDeviceV2) error {
		_, err := tx.Exec(ctx, `
			UPDATE badge_devices_v2
			SET badge_status = $2,
			    updated_at = NOW()
			WHERE id = $1
		`, id, BadgeStatusRetired)
		return err
	})
}

func (s *Store) rebuildChangeBadgeStatus(ctx context.Context, id int64, operation, fromStatus, toStatus, reason string, operatorID int64, operatorName string, updateFn func(context.Context, pgx.Tx, *BadgeDeviceV2) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	device, err := s.rebuildGetBadgeDeviceForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if device.BadgeStatus != fromStatus {
		return fmt.Errorf("badge status must be %s", fromStatus)
	}
	if err := updateFn(ctx, tx, device); err != nil {
		return err
	}
	if err := s.rebuildInsertBadgeLogTx(ctx, tx, device.ID, device.DeviceNo, operation, &device.BadgeStatus, &toStatus, &operatorID, &operatorName, JSONObject{
		"reason": reason,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) rebuildGetBadgeDeviceForUpdate(ctx context.Context, tx pgx.Tx, id int64) (*BadgeDeviceV2, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, device_no, manufacturer_id, manufacturer_code, manufacturer_name, app_id, device_uid,
		       hardware_model, badge_status, current_tenant_id, current_tenant_name, current_employee_id,
		       current_employee_name, current_employee_phone, assigned_at, health_level, battery_level,
		       last_online_at, last_health_check_at, health_check_result, import_batch_no, acceptance_batch_no,
		       accepted_at, acceptance_result, metadata, created_at, updated_at, deleted_at
		FROM badge_devices_v2
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, id)
	item, err := scanBadgeDeviceV2(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("badge device not found")
		}
		return nil, fmt.Errorf("failed to load badge device: %w", err)
	}
	return item, nil
}

func (s *Store) rebuildInsertBadgeLogTx(ctx context.Context, tx pgx.Tx, deviceID int64, deviceNo, operation string, fromStatus, toStatus *string, operatorID *int64, operatorName *string, detail JSONObject) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO badge_device_logs_v2 (
			badge_device_id, device_no, operation, from_badge_status, to_badge_status,
			operator_id, operator_name, operator_type, detail, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), 'admin', $8, NOW())
	`, deviceID, deviceNo, operation, fromStatus, toStatus, operatorID, operatorName, detail)
	if err != nil {
		return fmt.Errorf("failed to insert badge log: %w", err)
	}
	return nil
}

func (s *Store) rebuildInsertAssignmentLogTx(ctx context.Context, tx pgx.Tx, deviceID int64, deviceNo, action string, tenantID *int64, tenantName string, employeeID *int64, employeeName, reason string, operatorID *int64, operatorName *string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO badge_assignment_logs_v2 (
			badge_device_id, device_no, tenant_id, tenant_name, employee_id, employee_name,
			action, reason, operator_id, operator_name, created_at
		) VALUES ($1, $2, $3, NULLIF($4, ''), $5, NULLIF($6, ''), $7, NULLIF($8, ''), $9, NULLIF($10, ''), NOW())
	`, deviceID, deviceNo, tenantID, tenantName, employeeID, employeeName, action, reason, operatorID, operatorName)
	if err != nil {
		return fmt.Errorf("failed to insert assignment log: %w", err)
	}
	return nil
}
