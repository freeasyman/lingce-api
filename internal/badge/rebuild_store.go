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

func reinterpretAsShanghai(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), shanghaiLocation)
}

func normalizeShanghaiTimePtr(value **time.Time) {
	if value == nil || *value == nil {
		return
	}
	t := reinterpretAsShanghai(**value)
	*value = &t
}

func normalizeShanghaiTimeValue(value *time.Time) {
	if value == nil {
		return
	}
	*value = reinterpretAsShanghai(*value)
}

func normalizeBadgeDeviceV2Times(item *BadgeDeviceV2) {
	if item == nil {
		return
	}
	normalizeShanghaiTimePtr(&item.AssignedAt)
	normalizeShanghaiTimePtr(&item.LastOnlineAt)
	normalizeShanghaiTimePtr(&item.LastHealthCheckAt)
	normalizeShanghaiTimePtr(&item.AcceptedAt)
	normalizeShanghaiTimePtr(&item.DeletedAt)
	normalizeShanghaiTimeValue(&item.CreatedAt)
	normalizeShanghaiTimeValue(&item.UpdatedAt)
}

func normalizeBadgeDeviceLogV2Times(item *BadgeDeviceLogV2) {
	if item == nil {
		return
	}
	normalizeShanghaiTimeValue(&item.CreatedAt)
}

func normalizeBadgeAssignmentLogV2Times(item *BadgeAssignmentLogV2) {
	if item == nil {
		return
	}
	normalizeShanghaiTimeValue(&item.CreatedAt)
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
	if req.Position != nil {
		conditions = append(conditions, fmt.Sprintf("position = $%d", argIndex))
		args = append(args, *req.Position)
		argIndex++
	}
	if req.Availability != nil {
		conditions = append(conditions, fmt.Sprintf("availability = $%d", argIndex))
		args = append(args, *req.Availability)
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
		       hardware_model, status, position, availability, tenant_id, tenant_name, employee_id, employee_name, employee_phone,
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
		       hardware_model, status, position, availability, tenant_id, tenant_name, employee_id, employee_name, employee_phone,
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

func (s *Store) RebuildSetBadgeHealthSnapshot(ctx context.Context, id int64, healthStatus string, healthResult JSONObject, operatorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := s.rebuildGetBadgeDeviceForUpdate(ctx, tx, id); err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		UPDATE badge_devices
		SET health_status = $2,
		    health_check_result = $3,
		    last_check_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, id, healthStatus, healthResult)
	if err != nil {
		return fmt.Errorf("failed to update badge health snapshot: %w", err)
	}

	if err := s.insertRebuildLifecycleLogTx(ctx, tx, rebuildLifecycleLogPayload{
		DeviceID:   id,
		Action:     "acceptance_check",
		FromStatus: strPtr(BadgeStatusPendingAcceptance),
		ToStatus:   strPtr(BadgeStatusPendingAcceptance),
		OperatorID: &operatorID,
		ExtraData: JSONObject{
			"health_status": healthStatus,
			"passed":        healthStatus != BadgeHealthError,
		},
	}); err != nil {
		return fmt.Errorf("failed to insert acceptance_check lifecycle log: %w", err)
	}

	return tx.Commit(ctx)
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
		&item.Position,
		&item.Availability,
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
	normalizeBadgeDeviceV2Times(&item)
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
		normalizeBadgeDeviceLogV2Times(&item)
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
		normalizeBadgeAssignmentLogV2Times(&item)
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
		if err := s.insertRebuildLifecycleLogTx(ctx, tx, rebuildLifecycleLogPayload{
			DeviceID:   createdID,
			Action:     "import",
			ToStatus:   strPtr(BadgeStatusPendingAcceptance),
			OperatorID: &operatorID,
			ExtraData: JSONObject{
				"import_batch_no":   batchNo,
				"manufacturer_code": manufacturerCode,
				"manufacturer_name": strings.TrimSpace(req.ManufacturerName),
				"hardware_model":    strings.TrimSpace(device.HardwareModel),
			},
		}); err != nil {
			return nil, fmt.Errorf("failed to insert import log: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit tx: %w", err)
	}
	return resp, nil
}

// RebuildAcceptBadgeDevice 标记可用：只要在仓库里就可以标为可用，不再要求先处于待验收。
func (s *Store) RebuildAcceptBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	device, err := s.rebuildGetBadgeDeviceForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if device.Position != "warehouse" {
		return fmt.Errorf("only warehouse badge can be marked available, got %s", device.Position)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE badge_devices
		SET availability = 'available',
		    accepted_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, id); err != nil {
		return fmt.Errorf("failed to mark badge available: %w", err)
	}
	if err := s.insertRebuildLifecycleLogTx(ctx, tx, rebuildLifecycleLogPayload{
		DeviceID:   id,
		Action:     "acceptance",
		FromStatus: strPtr(device.BadgeStatus),
		ToStatus:   strPtr(BadgeStatusInStock),
		OperatorID: &operatorID,
		ExtraData: JSONObject{
			"reason":            optionalString(reason),
			"from_position":     device.Position,
			"from_availability": device.Availability,
		},
	}); err != nil {
		return fmt.Errorf("failed to insert acceptance log: %w", err)
	}
	return tx.Commit(ctx)
}

var allowedPlacementTargets = map[string]map[string]bool{
	"in_transit": {"warehouse": true},
	"warehouse":  {"to_return": true, "lost": true, "scrapped": true, "inactive": true},
	"in_use":     {"warehouse": true, "to_return": true, "lost": true, "scrapped": true},
	"to_return":  {"vendor": true, "warehouse": true, "lost": true, "scrapped": true},
	"vendor":     {"warehouse": true, "lost": true, "scrapped": true},
	"lost":       {"warehouse": true, "scrapped": true},
	"inactive":   {"warehouse": true, "scrapped": true, "lost": true},
	"scrapped":   {},
}

func validAvailability(value string) bool {
	return value == "pending_check" || value == "pending_config" || value == "available" || value == "unusable"
}

// RebuildBatchSetAvailability 批量设置可用性（当前用于“批量标记可用”）。
func (s *Store) RebuildBatchSetAvailability(ctx context.Context, ids []int64, availability string, reason string, operatorID int64) (int, []int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	updated := 0
	skipped := make([]int64, 0)
	for _, id := range ids {
		device, err := s.rebuildGetBadgeDeviceForUpdate(ctx, tx, id)
		if err != nil {
			skipped = append(skipped, id)
			continue
		}
		if availability == "available" && device.Position != "warehouse" {
			skipped = append(skipped, id)
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE badge_devices SET availability = $2, updated_at = NOW() WHERE id = $1`, id, availability); err != nil {
			return updated, skipped, fmt.Errorf("failed to update availability for %d: %w", id, err)
		}
		if err := s.insertRebuildLifecycleLogTx(ctx, tx, rebuildLifecycleLogPayload{
			DeviceID:   id,
			Action:     "batch_availability",
			FromStatus: strPtr(device.BadgeStatus),
			ToStatus:   strPtr(device.Position),
			OperatorID: &operatorID,
			ExtraData: JSONObject{
				"reason":            optionalString(reason),
				"from_availability": device.Availability,
				"to_availability":   availability,
			},
		}); err != nil {
			return updated, skipped, fmt.Errorf("failed to insert batch log for %d: %w", id, err)
		}
		updated++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, fmt.Errorf("failed to commit batch availability: %w", err)
	}
	return updated, skipped, nil
}

// RebuildChangeBadgePlacement 调整设备位置与可用性：登记到货、寄出、回机、丢失、停用、报废、标记已配置/可用。
func (s *Store) RebuildChangeBadgePlacement(ctx context.Context, id int64, req RebuildBadgePlacementRequest, operatorID int64, operatorName string) (*BadgeDeviceV2, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	device, err := s.rebuildGetBadgeDeviceForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	targetPosition := device.Position
	if req.Position != "" {
		targetPosition = req.Position
	}
	if targetPosition == "in_use" {
		return nil, fmt.Errorf("在用状态只能通过分配或换机设置")
	}
	if targetPosition != device.Position {
		if !allowedPlacementTargets[device.Position][targetPosition] {
			return nil, fmt.Errorf("不允许从 %s 变更到 %s", device.Position, targetPosition)
		}
	}

	targetAvailability := req.Availability
	if targetAvailability == "" {
		switch targetPosition {
		case "scrapped", "lost", "inactive":
			targetAvailability = "unusable"
		case "warehouse":
			if device.Position == "warehouse" && validAvailability(device.Availability) {
				targetAvailability = device.Availability
			} else {
				targetAvailability = "pending_check"
			}
		default:
			targetAvailability = "pending_check"
		}
	}
	if !validAvailability(targetAvailability) {
		return nil, fmt.Errorf("invalid availability: %s", targetAvailability)
	}
	switch targetAvailability {
	case "available", "pending_config":
		if targetPosition != "warehouse" {
			return nil, fmt.Errorf("%s 只能用于仓库中的设备", targetAvailability)
		}
	case "unusable":
		if targetPosition == "in_transit" {
			return nil, fmt.Errorf("在途设备不能标记为不可用")
		}
	}

	clearOwner := device.Position == "in_use"
	sqlText := `UPDATE badge_devices SET position = $2, availability = $3, updated_at = NOW() WHERE id = $1`
	if clearOwner {
		sqlText = `UPDATE badge_devices SET position = $2, availability = $3, tenant_id = NULL, tenant_name = NULL,
		           employee_id = NULL, employee_name = NULL, employee_phone = NULL, updated_at = NOW() WHERE id = $1`
	}
	if _, err := tx.Exec(ctx, sqlText, id, targetPosition, targetAvailability); err != nil {
		return nil, fmt.Errorf("failed to change badge placement: %w", err)
	}

	if err := s.insertRebuildLifecycleLogTx(ctx, tx, rebuildLifecycleLogPayload{
		DeviceID:   id,
		Action:     "placement",
		FromStatus: strPtr(device.BadgeStatus),
		ToStatus:   strPtr(targetPosition),
		OperatorID: &operatorID,
		ExtraData: JSONObject{
			"reason":            optionalString(req.Reason),
			"from_position":     device.Position,
			"from_availability": device.Availability,
			"to_position":       targetPosition,
			"to_availability":   targetAvailability,
		},
	}); err != nil {
		return nil, fmt.Errorf("failed to insert placement log: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit placement: %w", err)
	}
	return s.RebuildGetBadgeDeviceByID(ctx, id)
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
	// 在库可用 → 领用；已在用 → 换人（同一事务内直接转移归属）。
	transfer := device.Position == "in_use"
	if !transfer && (device.Position != "warehouse" || device.Availability != "available") {
		return fmt.Errorf("badge must be warehouse+available or in_use to assign, got %s/%s", device.Position, device.Availability)
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
		SET position = 'in_use',
		    availability = 'available',
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

	action, fromStatus := "assign_employee", BadgeStatusInStock
	if transfer {
		action, fromStatus = "swap_employee", BadgeStatusAssigned
	}
	if err := s.insertRebuildLifecycleLogTx(ctx, tx, rebuildLifecycleLogPayload{
		DeviceID:   id,
		Action:     action,
		FromStatus: strPtr(fromStatus),
		ToStatus:   strPtr(BadgeStatusAssigned),
		TenantID:   &tenantID,
		EmployeeID: &employeeID,
		OperatorID: &operatorID,
		ExtraData: JSONObject{
			"tenant_name":   tenantName,
			"employee_name": employeeName,
		},
	}); err != nil {
		return fmt.Errorf("failed to insert assign lifecycle log: %w", err)
	}

	return tx.Commit(ctx)
}

// RebuildSwapBadgeDevices 换机：旧机转为待寄回，新机承接原归属。一个事务内完成。
func (s *Store) RebuildSwapBadgeDevices(ctx context.Context, oldID, newID int64, reason string, operatorID int64, operatorName string) (*BadgeDeviceV2, error) {
	if oldID == newID {
		return nil, fmt.Errorf("old and new badge must differ")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	oldDevice, err := s.rebuildGetBadgeDeviceForUpdate(ctx, tx, oldID)
	if err != nil {
		return nil, err
	}
	newDevice, err := s.rebuildGetBadgeDeviceForUpdate(ctx, tx, newID)
	if err != nil {
		return nil, err
	}
	if oldDevice.Position != "in_use" {
		return nil, fmt.Errorf("old badge must be in_use, got %s/%s", oldDevice.Position, oldDevice.Availability)
	}
	if newDevice.Position != "warehouse" || newDevice.Availability != "available" {
		return nil, fmt.Errorf("new badge must be warehouse+available, got %s/%s", newDevice.Position, newDevice.Availability)
	}
	if newDevice.CurrentTenantID != nil || newDevice.CurrentEmployeeID != nil {
		return nil, fmt.Errorf("new badge is already assigned")
	}

	tenantID, tenantName := oldDevice.CurrentTenantID, oldDevice.CurrentTenantName
	employeeID, employeeName, employeePhone := oldDevice.CurrentEmployeeID, oldDevice.CurrentEmployeeName, oldDevice.CurrentEmployeePhone

	if _, err := tx.Exec(ctx, `
		UPDATE badge_devices
		SET position = 'to_return',
		    availability = 'pending_check',
		    tenant_id = NULL,
		    tenant_name = NULL,
		    employee_id = NULL,
		    employee_name = NULL,
		    employee_phone = NULL,
		    updated_at = NOW()
		WHERE id = $1
	`, oldID); err != nil {
		return nil, fmt.Errorf("failed to swap out old badge: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE badge_devices
		SET position = 'in_use',
		    availability = 'available',
		    tenant_id = $2,
		    tenant_name = $3,
		    employee_id = $4,
		    employee_name = $5,
		    employee_phone = $6,
		    assigned_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, newID, tenantID, tenantName, employeeID, employeeName, employeePhone); err != nil {
		return nil, fmt.Errorf("failed to swap in new badge: %w", err)
	}

	if err := s.insertRebuildLifecycleLogTx(ctx, tx, rebuildLifecycleLogPayload{
		DeviceID:   oldID,
		Action:     "swap_out",
		FromStatus: strPtr(BadgeStatusAssigned),
		ToStatus:   strPtr(BadgeStatusReturned),
		TenantID:   tenantID,
		EmployeeID: employeeID,
		OperatorID: &operatorID,
		ExtraData: JSONObject{
			"reason":        optionalString(reason),
			"new_device_id": newID,
			"new_device_no": newDevice.DeviceNo,
		},
	}); err != nil {
		return nil, fmt.Errorf("failed to insert swap_out log: %w", err)
	}

	if err := s.insertRebuildLifecycleLogTx(ctx, tx, rebuildLifecycleLogPayload{
		DeviceID:   newID,
		Action:     "swap_in",
		FromStatus: strPtr(BadgeStatusInStock),
		ToStatus:   strPtr(BadgeStatusAssigned),
		TenantID:   tenantID,
		EmployeeID: employeeID,
		OperatorID: &operatorID,
		ExtraData: JSONObject{
			"reason":        optionalString(reason),
			"old_device_id": oldID,
			"old_device_no": oldDevice.DeviceNo,
			"employee_name": employeeName,
		},
	}); err != nil {
		return nil, fmt.Errorf("failed to insert swap_in log: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit swap: %w", err)
	}
	return s.RebuildGetBadgeDeviceByID(ctx, newID)
}

func (s *Store) RebuildReclaimBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.rebuildTransitionWithReset(ctx, id, BadgeStatusAssigned, BadgeStatusReturned, "reclaim", reason, operatorID)
}

func (s *Store) RebuildRestockBadgeDevice(ctx context.Context, id int64, reason string, operatorID int64, operatorName string) error {
	return s.rebuildChangeBadgeStatus(ctx, id, "restock", BadgeStatusReturned, BadgeStatusPendingAcceptance, reason, operatorID, func(ctx context.Context, tx pgx.Tx, device *BadgeDeviceV2) error {
		_, err := tx.Exec(ctx, `
			UPDATE badge_devices
			SET status = 'pending_acceptance',
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
	if err := s.insertRebuildLifecycleLogTx(ctx, tx, rebuildLifecycleLogPayload{
		DeviceID:   id,
		Action:     action,
		FromStatus: &fromStatus,
		ToStatus:   &toStatus,
		OperatorID: &operatorID,
		Notes:      optionalString(reason),
		ExtraData:  JSONObject{},
	}); err != nil {
		return fmt.Errorf("failed to insert lifecycle log: %w", err)
	}
	return tx.Commit(ctx)
}

type rebuildLifecycleLogPayload struct {
	DeviceID   int64
	Action     string
	FromStatus *string
	ToStatus   *string
	TenantID   *int64
	EmployeeID *int64
	OperatorID *int64
	Notes      *string
	ExtraData  JSONObject
}

func (s *Store) insertRebuildLifecycleLogTx(ctx context.Context, tx pgx.Tx, payload rebuildLifecycleLogPayload) error {
	if payload.ExtraData == nil {
		payload.ExtraData = JSONObject{}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO badge_device_lifecycle_logs (
			device_id, action, operation_type, from_status, to_status,
			tenant_id, employee_id, operator_id, notes, extra_data, created_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10, NOW()
		)
	`, payload.DeviceID, payload.Action, payload.Action, payload.FromStatus, payload.ToStatus, payload.TenantID, payload.EmployeeID, payload.OperatorID, payload.Notes, payload.ExtraData)
	if err != nil {
		return fmt.Errorf("failed to insert badge lifecycle log: %w", err)
	}
	return nil
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (s *Store) rebuildGetBadgeDeviceForUpdate(ctx context.Context, tx pgx.Tx, id int64) (*BadgeDeviceV2, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, device_no, manufacturer_id, manufacturer_code, manufacturer_name, app_id, device_uid,
		       hardware_model, status, position, availability, tenant_id, tenant_name, employee_id, employee_name, employee_phone,
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
