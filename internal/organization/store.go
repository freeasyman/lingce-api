package organization

import (
	"context"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool        *pgxpool.Pool
	tenantStore *tenant.Store
}

func NewStore(pool *pgxpool.Pool, tenantStore *tenant.Store) *Store {
	return &Store{
		pool:        pool,
		tenantStore: tenantStore,
	}
}

// ListMedicalSpecialties retrieves all medical specialties
func (s *Store) ListMedicalSpecialties(ctx context.Context) ([]*MedicalSpecialty, error) {
	query := `
		SELECT id, name, code, parent_id, level, sort_order
		FROM medical_specialties
		ORDER BY sort_order, id
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query medical specialties: %w", err)
	}
	defer rows.Close()

	var specialties []*MedicalSpecialty
	for rows.Next() {
		var ms MedicalSpecialty
		if err := rows.Scan(
			&ms.ID,
			&ms.Name,
			&ms.Code,
			&ms.ParentID,
			&ms.Level,
			&ms.SortOrder,
		); err != nil {
			return nil, fmt.Errorf("failed to scan medical specialty: %w", err)
		}
		specialties = append(specialties, &ms)
	}

	return specialties, nil
}

// GetEmployeeAssistants retrieves assistants for an employee
func (s *Store) GetEmployeeAssistants(ctx context.Context, employeeID int64) ([]*AssistantResponse, error) {
	query := `
		SELECT e.id, e.full_name, e.username
		FROM employees e
		INNER JOIN employee_assistant_assignments eaa ON e.id = eaa.assistant_id
		WHERE eaa.employee_id = $1 AND e.deleted_at IS NULL
		ORDER BY e.full_name
	`

	rows, err := s.pool.Query(ctx, query, employeeID)
	if err != nil {
		return nil, fmt.Errorf("failed to query employee assistants: %w", err)
	}
	defer rows.Close()

	var assistants []*AssistantResponse
	for rows.Next() {
		var a AssistantResponse
		if err := rows.Scan(&a.ID, &a.FullName, &a.Username); err != nil {
			return nil, fmt.Errorf("failed to scan assistant: %w", err)
		}
		assistants = append(assistants, &a)
	}

	return assistants, nil
}

// UpdateEmployeeAssistants updates assistant bindings for an employee
func (s *Store) UpdateEmployeeAssistants(ctx context.Context, employeeID int64, assistantIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Delete existing assignments
	deleteQuery := `DELETE FROM employee_assistant_assignments WHERE employee_id = $1`
	if _, err := tx.Exec(ctx, deleteQuery, employeeID); err != nil {
		return fmt.Errorf("failed to delete existing assignments: %w", err)
	}

	// Insert new assignments
	if len(assistantIDs) > 0 {
		insertQuery := `
			INSERT INTO employee_assistant_assignments (employee_id, assistant_id, created_at)
			VALUES ($1, $2, NOW())
		`
		for _, assistantID := range assistantIDs {
			if _, err := tx.Exec(ctx, insertQuery, employeeID, assistantID); err != nil {
				return fmt.Errorf("failed to insert assignment: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetInstitutionStatistics retrieves institution statistics
func (s *Store) GetInstitutionStatistics(ctx context.Context) (*InstitutionStatistics, error) {
	var stats InstitutionStatistics

	// Total and active tenants - use tenantStore
	tenantStats, err := s.tenantStore.GetTenantStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query tenant stats: %w", err)
	}
	stats.TotalTenants = int(tenantStats["total_tenants"].(int64))
	stats.ActiveTenants = int(tenantStats["active_tenants"].(int64))

	// Total employees
	employeeQuery := `SELECT COUNT(*) FROM employees WHERE deleted_at IS NULL`
	if err := s.pool.QueryRow(ctx, employeeQuery).Scan(&stats.TotalEmployees); err != nil {
		return nil, fmt.Errorf("failed to query employee stats: %w", err)
	}

	// Total departments
	deptQuery := `SELECT COUNT(*) FROM departments WHERE deleted_at IS NULL`
	if err := s.pool.QueryRow(ctx, deptQuery).Scan(&stats.TotalDepartments); err != nil {
		return nil, fmt.Errorf("failed to query department stats: %w", err)
	}

	// Total badge devices
	badgeQuery := `SELECT COUNT(*) FROM badge_devices WHERE deleted_at IS NULL`
	if err := s.pool.QueryRow(ctx, badgeQuery).Scan(&stats.TotalBadgeDevices); err != nil {
		// Badge devices table might not exist yet, set to 0
		stats.TotalBadgeDevices = 0
	}

	// Total recordings
	recordingQuery := `SELECT COUNT(*) FROM medical_recordings WHERE deleted_at IS NULL`
	if err := s.pool.QueryRow(ctx, recordingQuery).Scan(&stats.TotalRecordings); err != nil {
		return nil, fmt.Errorf("failed to query recording stats: %w", err)
	}

	// Recordings this week
	weekQuery := `
		SELECT COUNT(*)
		FROM medical_recordings
		WHERE deleted_at IS NULL
		AND created_at >= date_trunc('week', NOW())
	`
	if err := s.pool.QueryRow(ctx, weekQuery).Scan(&stats.RecordingsThisWeek); err != nil {
		return nil, fmt.Errorf("failed to query weekly recording stats: %w", err)
	}

	return &stats, nil
}

// Doctor CRUD over employees table

func (s *Store) ListDoctors(ctx context.Context, tenantID *int64, name *string, departmentID *int64, isActive *bool, page, pageSize int) ([]*Doctor, int, error) {
	conditions := []string{"deleted_at IS NULL"}
	args := []interface{}{}
	argIndex := 1

	if tenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *tenantID)
		argIndex++
	}
	if name != nil && *name != "" {
		conditions = append(conditions, fmt.Sprintf("full_name ILIKE $%d", argIndex))
		args = append(args, "%"+*name+"%")
		argIndex++
	}
	if departmentID != nil {
		conditions = append(conditions, fmt.Sprintf("department_id = $%d", argIndex))
		args = append(args, *departmentID)
		argIndex++
	}
	if isActive != nil {
		conditions = append(conditions, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *isActive)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM employees WHERE %s", whereClause), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count doctors: %w", err)
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, department_id,
		       COALESCE(NULLIF(NULLIF(full_name, 'unknown'), ''), NULLIF(NULLIF(name, 'unknown'), ''), NULLIF(username, ''), NULLIF(phone, ''), '未知医生') AS full_name,
		       COALESCE(phone, '') AS phone,
		       COALESCE(email, '') AS email,
		       CASE
		           WHEN is_active IS NULL THEN true
		           WHEN is_active::text IN ('1', 't', 'true') THEN true
		           ELSE false
		       END AS is_active,
		       created_at, updated_at
		FROM employees
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)
	args = append(args, pageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list doctors: %w", err)
	}
	defer rows.Close()

	var items []*Doctor
	for rows.Next() {
		var d Doctor
		if err := rows.Scan(&d.ID, &d.TenantID, &d.DepartmentID, &d.Name, &d.Phone, &d.Email, &d.IsActive, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan doctor: %w", err)
		}
		items = append(items, &d)
	}
	return items, total, nil
}

func (s *Store) GetDoctorByID(ctx context.Context, id int64) (*Doctor, error) {
	query := `
		SELECT id, tenant_id, department_id,
		       COALESCE(NULLIF(NULLIF(full_name, 'unknown'), ''), NULLIF(NULLIF(name, 'unknown'), ''), NULLIF(username, ''), NULLIF(phone, ''), '未知医生') AS full_name,
		       COALESCE(phone, '') AS phone,
		       COALESCE(email, '') AS email,
		       CASE
		           WHEN is_active IS NULL THEN true
		           WHEN is_active::text IN ('1', 't', 'true') THEN true
		           ELSE false
		       END AS is_active,
		       created_at, updated_at
		FROM employees
		WHERE id = $1 AND deleted_at IS NULL
	`
	var d Doctor
	if err := s.pool.QueryRow(ctx, query, id).Scan(&d.ID, &d.TenantID, &d.DepartmentID, &d.Name, &d.Phone, &d.Email, &d.IsActive, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("doctor not found")
		}
		return nil, fmt.Errorf("failed to query doctor: %w", err)
	}
	return &d, nil
}

func (s *Store) CreateDoctor(ctx context.Context, tenantID int64, fullName, phone, email string, departmentID *int64) (*Doctor, error) {
	query := `
		INSERT INTO employees (tenant_id, username, password_hash, name, full_name, phone, email, department_id, session_version, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4, $5, $6, $7, 0, true, NOW(), NOW())
		RETURNING id, tenant_id, department_id,
		       COALESCE(NULLIF(NULLIF(full_name, 'unknown'), ''), NULLIF(NULLIF(name, 'unknown'), ''), NULLIF(username, ''), NULLIF(phone, ''), '未知医生') AS full_name,
		       phone, email, is_active, created_at, updated_at
	`
	username := fmt.Sprintf("doctor_%d_%s", tenantID, strings.ToLower(strings.ReplaceAll(fullName, " ", "")))
	passwordHash := "placeholder_hash"

	var d Doctor
	if err := s.pool.QueryRow(ctx, query, tenantID, username, passwordHash, fullName, phone, email, departmentID).Scan(
		&d.ID, &d.TenantID, &d.DepartmentID, &d.Name, &d.Phone, &d.Email, &d.IsActive, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("failed to create doctor: %w", err)
	}
	return &d, nil
}

func (s *Store) UpdateDoctor(ctx context.Context, id int64, fullName, phone, email *string, departmentID *int64, isActive *bool) (*Doctor, error) {
	setClauses := []string{}
	args := []interface{}{}
	argIndex := 1
	if fullName != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *fullName)
		argIndex++
		setClauses = append(setClauses, fmt.Sprintf("full_name = $%d", argIndex))
		args = append(args, *fullName)
		argIndex++
	}
	if phone != nil {
		setClauses = append(setClauses, fmt.Sprintf("phone = $%d", argIndex))
		args = append(args, *phone)
		argIndex++
	}
	if email != nil {
		setClauses = append(setClauses, fmt.Sprintf("email = $%d", argIndex))
		args = append(args, *email)
		argIndex++
	}
	if departmentID != nil {
		setClauses = append(setClauses, fmt.Sprintf("department_id = $%d", argIndex))
		args = append(args, *departmentID)
		argIndex++
	}
	if isActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *isActive)
		argIndex++
	}
	if len(setClauses) == 0 {
		return s.GetDoctorByID(ctx, id)
	}
	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf(`
		UPDATE employees
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, tenant_id, department_id,
		       COALESCE(NULLIF(NULLIF(full_name, 'unknown'), ''), NULLIF(NULLIF(name, 'unknown'), ''), NULLIF(username, ''), NULLIF(phone, ''), '未知医生') AS full_name,
		       phone, email, is_active, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIndex)
	var d Doctor
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&d.ID, &d.TenantID, &d.DepartmentID, &d.Name, &d.Phone, &d.Email, &d.IsActive, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("doctor not found")
		}
		return nil, fmt.Errorf("failed to update doctor: %w", err)
	}
	return &d, nil
}

func (s *Store) DeleteDoctor(ctx context.Context, id int64) error {
	result, err := s.pool.Exec(ctx, `UPDATE employees SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("failed to delete doctor: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("doctor not found")
	}
	return nil
}

// Patient CRUD over customers table

func (s *Store) ListPatients(ctx context.Context, tenantID *int64, name *string, phone *string, status *string, page, pageSize int) ([]*Patient, int, error) {
	conditions := []string{"deleted_at IS NULL"}
	args := []interface{}{}
	argIndex := 1

	if tenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *tenantID)
		argIndex++
	}
	if name != nil && *name != "" {
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", argIndex))
		args = append(args, "%"+*name+"%")
		argIndex++
	}
	if phone != nil && *phone != "" {
		conditions = append(conditions, fmt.Sprintf("phone ILIKE $%d", argIndex))
		args = append(args, "%"+*phone+"%")
		argIndex++
	}
	if status != nil && *status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *status)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM customers WHERE %s", whereClause), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count patients: %w", err)
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, name, phone, email, gender, age, status, momentum, assigned_to, created_at, updated_at
		FROM customers
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)
	args = append(args, pageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list patients: %w", err)
	}
	defer rows.Close()

	var items []*Patient
	for rows.Next() {
		var p Patient
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.Phone, &p.Email, &p.Gender, &p.Age, &p.Status, &p.Momentum, &p.AssignedTo, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan patient: %w", err)
		}
		items = append(items, &p)
	}
	return items, total, nil
}

func (s *Store) GetPatientByID(ctx context.Context, id int64) (*Patient, error) {
	query := `
		SELECT id, tenant_id, name, phone, email, gender, age, status, momentum, assigned_to, created_at, updated_at
		FROM customers
		WHERE id = $1 AND deleted_at IS NULL
	`
	var p Patient
	if err := s.pool.QueryRow(ctx, query, id).Scan(&p.ID, &p.TenantID, &p.Name, &p.Phone, &p.Email, &p.Gender, &p.Age, &p.Status, &p.Momentum, &p.AssignedTo, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("patient not found")
		}
		return nil, fmt.Errorf("failed to query patient: %w", err)
	}
	return &p, nil
}

func (s *Store) CreatePatient(ctx context.Context, tenantID int64, name string, phone, email, gender *string, age *int, createdBy int64) (*Patient, error) {
	query := `
		INSERT INTO customers (tenant_id, name, phone, email, gender, age, status, momentum, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'lead', 50, $7, NOW(), NOW())
		RETURNING id, tenant_id, name, phone, email, gender, age, status, momentum, assigned_to, created_at, updated_at
	`
	var p Patient
	if err := s.pool.QueryRow(ctx, query, tenantID, name, phone, email, gender, age, createdBy).Scan(
		&p.ID, &p.TenantID, &p.Name, &p.Phone, &p.Email, &p.Gender, &p.Age, &p.Status, &p.Momentum, &p.AssignedTo, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("failed to create patient: %w", err)
	}
	return &p, nil
}

func (s *Store) UpdatePatient(ctx context.Context, id int64, name, phone, email, gender, status *string, age *int) (*Patient, error) {
	setClauses := []string{}
	args := []interface{}{}
	argIndex := 1
	if name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *name)
		argIndex++
	}
	if phone != nil {
		setClauses = append(setClauses, fmt.Sprintf("phone = $%d", argIndex))
		args = append(args, *phone)
		argIndex++
	}
	if email != nil {
		setClauses = append(setClauses, fmt.Sprintf("email = $%d", argIndex))
		args = append(args, *email)
		argIndex++
	}
	if gender != nil {
		setClauses = append(setClauses, fmt.Sprintf("gender = $%d", argIndex))
		args = append(args, *gender)
		argIndex++
	}
	if age != nil {
		setClauses = append(setClauses, fmt.Sprintf("age = $%d", argIndex))
		args = append(args, *age)
		argIndex++
	}
	if status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *status)
		argIndex++
	}
	if len(setClauses) == 0 {
		return s.GetPatientByID(ctx, id)
	}
	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf(`
		UPDATE customers
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, tenant_id, name, phone, email, gender, age, status, momentum, assigned_to, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIndex)
	var p Patient
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&p.ID, &p.TenantID, &p.Name, &p.Phone, &p.Email, &p.Gender, &p.Age, &p.Status, &p.Momentum, &p.AssignedTo, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("patient not found")
		}
		return nil, fmt.Errorf("failed to update patient: %w", err)
	}
	return &p, nil
}

func (s *Store) DeletePatient(ctx context.Context, id int64) error {
	result, err := s.pool.Exec(ctx, `UPDATE customers SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("failed to delete patient: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("patient not found")
	}
	return nil
}

// Department advanced methods

func (s *Store) SyncDepartmentsFromVisits(ctx context.Context, tenantID *int64) (map[string]int64, error) {
	result := map[string]int64{
		"synced":  0,
		"created": 0,
		"updated": 0,
	}

	var opVisitsExists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass('public.op_visits') IS NOT NULL").Scan(&opVisitsExists); err != nil {
		return nil, fmt.Errorf("failed to check op_visits table: %w", err)
	}
	if !opVisitsExists {
		return result, nil
	}

	colRows, err := s.pool.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'op_visits'
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect op_visits columns: %w", err)
	}
	defer colRows.Close()

	columns := map[string]bool{}
	for colRows.Next() {
		var name string
		if err := colRows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan op_visits column: %w", err)
		}
		columns[name] = true
	}

	deptColumnCandidates := []string{"department_name", "department", "dept_name"}
	deptColumn := ""
	for _, col := range deptColumnCandidates {
		if columns[col] {
			deptColumn = col
			break
		}
	}
	if deptColumn == "" {
		return result, nil
	}

	tenantColumnCandidates := []string{"tenant_id", "org_id"}
	tenantColumn := ""
	for _, col := range tenantColumnCandidates {
		if columns[col] {
			tenantColumn = col
			break
		}
	}

	query := fmt.Sprintf("SELECT DISTINCT TRIM(%s)", deptColumn)
	if tenantColumn != "" {
		query = fmt.Sprintf("SELECT DISTINCT %s::bigint AS tenant_id, TRIM(%s) AS dept_name", tenantColumn, deptColumn)
	} else {
		query += " AS dept_name"
	}
	query += fmt.Sprintf(" FROM op_visits WHERE %s IS NOT NULL AND TRIM(%s) <> ''", deptColumn, deptColumn)
	args := []interface{}{}
	if tenantID != nil && tenantColumn != "" {
		query += " AND " + tenantColumn + " = $1"
		args = append(args, *tenantID)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query departments from op_visits: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var candidateTenantID int64
		var deptName string
		if tenantColumn != "" {
			if err := rows.Scan(&candidateTenantID, &deptName); err != nil {
				return nil, fmt.Errorf("failed to scan op_visits department: %w", err)
			}
		} else {
			if err := rows.Scan(&deptName); err != nil {
				return nil, fmt.Errorf("failed to scan op_visits department: %w", err)
			}
			if tenantID == nil {
				continue
			}
			candidateTenantID = *tenantID
		}

		if tenantID != nil {
			candidateTenantID = *tenantID
		}
		if candidateTenantID == 0 {
			continue
		}

		result["synced"]++

		var existingID int64
		var existingActive bool
		err := s.pool.QueryRow(ctx, `
			SELECT id, is_active
			FROM departments
			WHERE tenant_id = $1 AND LOWER(name) = LOWER($2) AND deleted_at IS NULL
			LIMIT 1
		`, candidateTenantID, deptName).Scan(&existingID, &existingActive)
		if err == pgx.ErrNoRows {
			code := buildDepartmentCode(deptName)
			if _, err := s.pool.Exec(ctx, `
				INSERT INTO departments (tenant_id, name, code, parent_id, is_active, created_at, updated_at)
				VALUES ($1, $2, $3, NULL, true, NOW(), NOW())
			`, candidateTenantID, deptName, code); err != nil {
				return nil, fmt.Errorf("failed to create synced department: %w", err)
			}
			result["created"]++
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to query existing department: %w", err)
		}

		if !existingActive {
			if _, err := s.pool.Exec(ctx, `
				UPDATE departments
				SET is_active = true, updated_at = NOW()
				WHERE id = $1
			`, existingID); err != nil {
				return nil, fmt.Errorf("failed to reactivate department: %w", err)
			}
			result["updated"]++
		}
	}

	return result, nil
}

func (s *Store) GetDepartmentPerformance(ctx context.Context, departmentID int64, period string) (map[string]interface{}, error) {
	var departmentExists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM departments WHERE id = $1 AND deleted_at IS NULL)
	`, departmentID).Scan(&departmentExists); err != nil {
		return nil, fmt.Errorf("failed to check department: %w", err)
	}
	if !departmentExists {
		return nil, fmt.Errorf("department not found")
	}

	dateFilter := ""
	switch strings.ToLower(period) {
	case "week":
		dateFilter = " AND r.created_at >= date_trunc('week', NOW())"
	case "year":
		dateFilter = " AND r.created_at >= date_trunc('year', NOW())"
	case "all":
		dateFilter = ""
	default:
		period = "month"
		dateFilter = " AND r.created_at >= date_trunc('month', NOW())"
	}

	var doctorCount int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM employees
		WHERE department_id = $1 AND deleted_at IS NULL
	`, departmentID).Scan(&doctorCount); err != nil {
		return nil, fmt.Errorf("failed to count department doctors: %w", err)
	}

	visitSQL := fmt.Sprintf(`
		SELECT
			COUNT(*)::bigint AS total_visits,
			COUNT(DISTINCT r.customer_id)::bigint AS patient_count
		FROM recordings r
		JOIN employees e ON e.id = r.employee_id
		WHERE e.department_id = $1 %s
	`, dateFilter)
	var totalVisits int64
	var patientCount int64
	if err := s.pool.QueryRow(ctx, visitSQL, departmentID).Scan(&totalVisits, &patientCount); err != nil {
		return nil, fmt.Errorf("failed to aggregate department visits: %w", err)
	}

	totalRevenue := 0.0
	avgVisitValue := 0.0
	if totalVisits > 0 {
		avgVisitValue = totalRevenue / float64(totalVisits)
	}

	return map[string]interface{}{
		"department_id":   departmentID,
		"total_visits":    totalVisits,
		"total_revenue":   totalRevenue,
		"avg_visit_value": avgVisitValue,
		"patient_count":   patientCount,
		"doctor_count":    doctorCount,
		"period":          period,
	}, nil
}

func buildDepartmentCode(name string) string {
	normalized := strings.ToUpper(strings.TrimSpace(name))
	if normalized == "" {
		return "AUTO_DEPT"
	}
	var b strings.Builder
	for _, ch := range normalized {
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
		} else {
			b.WriteRune('_')
		}
	}
	code := strings.Trim(b.String(), "_")
	if code == "" {
		return "AUTO_DEPT"
	}
	if len(code) > 20 {
		code = code[:20]
	}
	return "AUTO_" + code
}
