package badge

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
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
			VALUES ($1, $2, $3, 'pending_assignment', NOW(), NOW(), NOW())
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
			VALUES ($1, 'acceptance', 'pending_assignment', $2, NOW())
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
			SET tenant_id = $1, status = 'in_use', assigned_to_tenant_at = NOW(), updated_at = NOW()
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
			SET employee_id = $1, assigned_to_emp_at = NOW(), updated_at = NOW()
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
			SET employee_id = NULL, assigned_to_emp_at = NULL, updated_at = NOW()
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
			VALUES ($1, 'reclaim_employee', 'in_use', $2, $3, NOW())
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
			SET tenant_id = NULL, employee_id = NULL, status = 'pending_assignment',
			    assigned_to_tenant_at = NULL, assigned_to_emp_at = NULL, updated_at = NOW()
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
			VALUES ($1, 'reclaim_tenant', 'pending_assignment', $2, $3, NOW())
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
			COUNT(CASE WHEN status = 'pending_acceptance' THEN 1 END) as pending_acceptance,
			COUNT(CASE WHEN status = 'pending_assignment' THEN 1 END) as pending_assignment,
			COUNT(CASE WHEN status = 'in_use' THEN 1 END) as in_use,
			COUNT(CASE WHEN status = 'maintenance' THEN 1 END) as maintenance,
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

	var manufacturers []*BadgeManufacturer
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
