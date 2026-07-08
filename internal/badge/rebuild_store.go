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
	case BadgeStatusRetired:
		return BadgeStatusRetired
	default:
		return ""
	}
}

func (s *Store) RebuildListBadgeDevices(ctx context.Context, req RebuildBadgeDeviceListRequest) ([]*BadgeDeviceV2, int, error) {
	conditions := []string{"deleted_at IS NULL"}
	args := make([]interface{}, 0, 8)
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
	if req.EmployeeKey != nil {
		conditions = append(conditions, fmt.Sprintf("(employee_name ILIKE $%d OR employee_phone ILIKE $%d)", argIndex, argIndex))
		args = append(args, "%"+strings.TrimSpace(*req.EmployeeKey)+"%")
		argIndex++
	}
	if req.BadgeStatus != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, normalizeBadgeStatus(*req.BadgeStatus))
		argIndex++
	}
	if req.HealthLevel != nil {
		conditions = append(conditions, fmt.Sprintf("health_status = $%d", argIndex))
		args = append(args, *req.HealthLevel)
		argIndex++
	}
	if req.DeviceNo != nil {
		conditions = append(conditions, fmt.Sprintf("device_no ILIKE $%d", argIndex))
		args = append(args, "%"+strings.TrimSpace(*req.DeviceNo)+"%")
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM badge_devices WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count badge devices: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	listSQL := fmt.Sprintf(`
		SELECT id, device_no, manufacturer_id, manufacturer_code, manufacturer_name, app_id, device_uid,
		       hardware_model, status, tenant_id, tenant_name, employee_id, employee_name, employee_phone,
		       assigned_at, health_status, battery_level, last_online_at, last_check_at, health_check_result,
		       import_batch_no, acceptance_batch_no, accepted_at, metadata, created_at, updated_at, deleted_at
		FROM badge_devices
		WHERE %s
		ORDER BY created_at DESC, id DESC
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
		item, err := scanRebuildBadgeDevice(rows)
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
		       hardware_model, status, tenant_id, tenant_name, employee_id, employee_name, employee_phone,
		       assigned_at, health_status, battery_level, last_online_at, last_check_at, health_check_result,
		       import_batch_no, acceptance_batch_no, accepted_at, metadata, created_at, updated_at, deleted_at
		FROM badge_devices
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	item, err := scanRebuildBadgeDevice(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("badge device not found")
		}
		return nil, err
	}
	return item, nil
}

func scanRebuildBadgeDevice(row pgx.Row) (*BadgeDeviceV2, error) {
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
		&item.Metadata,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.DeletedAt,
	); err != nil {
		return nil, err
	}
	item.AcceptanceResult = nil
	item.BadgeStatus = normalizeBadgeStatus(item.BadgeStatus)
	item.HealthLevel = normalizeHealthStatus(item.HealthLevel)
	return &item, nil
}

func (s *Store) RebuildListBadgeDeviceLogs(ctx context.Context, deviceID int64) ([]*BadgeDeviceLogV2, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, device_id, action, from_status, to_status, operator_id, NULL::text, NULL::text, extra_data, created_at
		FROM badge_device_lifecycle_logs
		WHERE device_id = $1
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
		item.DeviceNo = ""
		if item.FromBadgeStatus != nil {
			value := normalizeBadgeStatus(*item.FromBadgeStatus)
			item.FromBadgeStatus = &value
		}
		if item.ToBadgeStatus != nil {
			value := normalizeBadgeStatus(*item.ToBadgeStatus)
			item.ToBadgeStatus = &value
		}
		items = append(items, &item)
	}
	return items, nil
}

func (s *Store) RebuildListBadgeAssignmentLogs(ctx context.Context, deviceID int64) ([]*BadgeAssignmentLogV2, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, device_id, action, tenant_id, employee_id, operator_id, notes, created_at
		FROM badge_device_lifecycle_logs
		WHERE device_id = $1
		  AND action IN ('assign_tenant', 'assign_employee', 'reclaim')
		ORDER BY created_at DESC, id DESC
	`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list assignment logs: %w", err)
	}
	defer rows.Close()

	items := make([]*BadgeAssignmentLogV2, 0)
	for rows.Next() {
		var item BadgeAssignmentLogV2
		var legacyAction string
		if err := rows.Scan(
			&item.ID,
			&item.BadgeDeviceID,
			&legacyAction,
			&item.TenantID,
			&item.EmployeeID,
			&item.OperatorID,
			&item.Reason,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan assignment log: %w", err)
		}
		item.Action = "assign"
		if legacyAction == "reclaim" {
			item.Action = "reclaim"
		}
		if item.TenantID != nil {
			_ = s.pool.QueryRow(ctx, `SELECT COALESCE(name, '') FROM tenants WHERE id = $1`, *item.TenantID).Scan(&item.TenantName)
		}
		if item.EmployeeID != nil {
			var employeeName string
			_ = s.pool.QueryRow(ctx, `SELECT COALESCE(NULLIF(full_name, ''), NULLIF(name, ''), NULLIF(username, ''), NULLIF(phone, ''), '未知员工') FROM employees WHERE id = $1`, *item.EmployeeID).Scan(&employeeName)
			item.EmployeeName = &employeeName
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
		err := tx.QueryRow(ctx, `SELECT id FROM badge_devices WHERE device_no = $1 AND deleted_at IS NULL LIMIT 1`, deviceNo).Scan(&exists)
		if err == nil {
			resp.Duplicates = append(resp.Duplicates, deviceNo)
			continue
		}
		if err != nil && err != pgx.ErrNoRows {
			return nil, fmt.Errorf("failed to check duplicate: %w", err)
		}

		var createdID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO badge_devices (
				device_no, manufacturer_code, manufacturer_name, hardware_model,
				status, health_status, import_batch_no, metadata, created_at, updated_at
			) VALUES (
				$1, $2, NULLIF($3, ''), NULLIF($4, ''),
				'pending_acceptance', 'unknown', $5, '{}'::jsonb, NOW(), NOW()
			)
			RETURNING id
		`, deviceNo, manufacturerCode, strings.TrimSpace(req.ManufacturerName), strings.TrimSpace(device.HardwareModel), batchNo).Scan(&createdID)
		if err != nil {
			resp.FailedCount++
			continue
		}

		resp.SuccessCount++
		if _, err := tx.Exec(ctx, `
			INSERT INTO badge_device_lifecycle_logs (device_id, action, from_status, to_status, operator_id, notes, extra_data, created_at)
			VALUES ($1, 'import', NULL, 'pending', $2, NULL, $3, NOW())
		`, createdID, operatorID, JSONObject{
			"import_batch_no":   batchNo,
			"manufacturer_code": manufacturerCode,
			"manufacturer_name": strings.TrimSpace(req.ManufacturerName),
			"hardware_model":    strings.TrimSpace(device.HardwareModel),
		}); err != nil {
			return nil, fmt.Errorf("failed to insert import log: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit tx: %w", err)
	}
	return resp, nil
}

func (s *Store) RebuildAcceptBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.rebuildChangeBadgeStatus(ctx, id, "acceptance", BadgeStatusPendingAcceptance, BadgeStatusInStock, reason, operatorID, func(ctx context.Context, tx pgx.Tx, device *BadgeDeviceV2) error {
		_, err := tx.Exec(ctx, `
			UPDATE badge_devices
			SET status = 'in_stock',
			    accepted_at = NOW(),
			    updated_at = NOW()
			WHERE id = $1
		`, id)
		return err
	})
}

func (s *Store) RebuildRejectAcceptance(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.rebuildChangeBadgeStatus(ctx, id, "reject_acceptance", BadgeStatusPendingAcceptance, BadgeStatusReturned, reason, operatorID, func(ctx context.Context, tx pgx.Tx, device *BadgeDeviceV2) error {
		_, err := tx.Exec(ctx, `
			UPDATE badge_devices
			SET status = 'returned',
			    updated_at = NOW()
			WHERE id = $1
		`, id)
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
	if err := tx.QueryRow(ctx, `SELECT COALESCE(name, '') FROM tenants WHERE id = $1 AND deleted_at IS NULL`, tenantID).Scan(&tenantName); err != nil {
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
		FROM badge_devices
		WHERE employee_id = $1
		  AND status = 'assigned'
		  AND deleted_at IS NULL
		  AND id <> $2
		LIMIT 1
	`, employeeID, id).Scan(&occupiedID)
	if err == nil {
		return fmt.Errorf("employee already has an assigned badge")
	}
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("failed to check employee badge occupancy: %w", err)
	}

		_, err = tx.Exec(ctx, `
		UPDATE badge_devices
		SET status = 'assigned',
		    tenant_id = $2,
		    tenant_name = $3,
		    employee_id = $4,
		    employee_name = $5,
		    employee_phone = $6,
		    assigned_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, id, tenantID, tenantName, employeeID, employeeName, employeePhone)
	if err != nil {
		return fmt.Errorf("failed to assign badge: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO badge_device_lifecycle_logs (device_id, action, from_status, to_status, tenant_id, employee_id, operator_id, notes, extra_data, created_at)
		VALUES ($1, 'assign_employee', 'in_stock', 'assigned', $2, $3, $4, NULL, $5, NOW())
	`, id, tenantID, employeeID, operatorID, JSONObject{
		"tenant_name":   tenantName,
		"employee_name": employeeName,
	}); err != nil {
		return fmt.Errorf("failed to insert assign lifecycle log: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *Store) RebuildReclaimBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.rebuildTransitionWithReset(ctx, id, BadgeStatusAssigned, BadgeStatusReturned, "reclaim", reason, operatorID)
}

func (s *Store) RebuildRestockBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.rebuildChangeBadgeStatus(ctx, id, "restock", BadgeStatusReturned, BadgeStatusInStock, reason, operatorID, func(ctx context.Context, tx pgx.Tx, device *BadgeDeviceV2) error {
		_, err := tx.Exec(ctx, `
			UPDATE badge_devices
			SET status = 'in_stock',
			    updated_at = NOW()
			WHERE id = $1
		`, id)
		return err
	})
}

func (s *Store) RebuildRetireBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.rebuildChangeBadgeStatus(ctx, id, "retire", BadgeStatusReturned, BadgeStatusRetired, reason, operatorID, func(ctx context.Context, tx pgx.Tx, device *BadgeDeviceV2) error {
		_, err := tx.Exec(ctx, `
			UPDATE badge_devices
			SET status = 'retired',
			    updated_at = NOW()
			WHERE id = $1
		`, id)
		return err
	})
}

func (s *Store) rebuildTransitionWithReset(ctx context.Context, id int64, fromStatus, toStatus, action, reason string, operatorID int64) error {
	return s.rebuildChangeBadgeStatus(ctx, id, action, fromStatus, toStatus, reason, operatorID, func(ctx context.Context, tx pgx.Tx, device *BadgeDeviceV2) error {
		_, err := tx.Exec(ctx, `
			UPDATE badge_devices
			SET status = 'returned',
			    tenant_id = NULL,
			    tenant_name = NULL,
			    employee_id = NULL,
			    employee_name = NULL,
			    employee_phone = NULL,
			    updated_at = NOW()
			WHERE id = $1
		`, id)
		return err
	})
}

func (s *Store) rebuildChangeBadgeStatus(ctx context.Context, id int64, action, fromStatus, toStatus, reason string, operatorID int64, updateFn func(context.Context, pgx.Tx, *BadgeDeviceV2) error) error {
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
	if _, err := tx.Exec(ctx, `
		INSERT INTO badge_device_lifecycle_logs (device_id, action, from_status, to_status, operator_id, notes, extra_data, created_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), '{}'::jsonb, NOW())
	`, id, action, fromStatus, toStatus, operatorID, reason); err != nil {
		return fmt.Errorf("failed to insert lifecycle log: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Store) rebuildGetBadgeDeviceForUpdate(ctx context.Context, tx pgx.Tx, id int64) (*BadgeDeviceV2, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, device_no, manufacturer_id, manufacturer_code, manufacturer_name, app_id, device_uid,
		       hardware_model, status, tenant_id, tenant_name, employee_id, employee_name, employee_phone,
		       assigned_at, health_status, battery_level, last_online_at, last_check_at, health_check_result,
		       import_batch_no, acceptance_batch_no, accepted_at, metadata, created_at, updated_at, deleted_at
		FROM badge_devices
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, id)
	item, err := scanRebuildBadgeDevice(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("badge device not found")
		}
		return nil, fmt.Errorf("failed to load badge device: %w", err)
	}
	return item, nil
}
