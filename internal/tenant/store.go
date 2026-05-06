package tenant

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Store struct {
	pool *pgxpool.Pool
}

const defaultTenantAdminPassword = "123456"

var defaultTenantDepartments = []struct {
	Name        string
	Code        string
	DefaultRole string
}{
	{Name: "医疗部", Code: "medical_department", DefaultRole: "doctor"},
	{Name: "运营部", Code: "operations_department", DefaultRole: "operating_manager"},
	{Name: "市场部", Code: "marketing_department", DefaultRole: "marketing_manager"},
	{Name: "咨询部", Code: "consulting_department", DefaultRole: "consultant"},
	{Name: "客服部", Code: "service_department", DefaultRole: "customer_service"},
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) tenantIsActiveUsesInteger(ctx context.Context) (bool, error) {
	var dataType string
	err := s.pool.QueryRow(ctx, `
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'tenants'
		  AND column_name = 'is_active'
		LIMIT 1
	`).Scan(&dataType)
	if err != nil {
		return false, fmt.Errorf("failed to inspect tenants.is_active type: %w", err)
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(dataType)), "int"), nil
}

// ListTenants retrieves a paginated list of tenants
func (s *Store) ListTenants(ctx context.Context, req TenantListRequest) ([]*Tenant, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	// Build WHERE clause
	conditions = append(conditions, "deleted_at IS NULL")

	if req.Name != "" {
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", argIndex))
		args = append(args, "%"+req.Name+"%")
		argIndex++
	}

	if req.Code != "" {
		conditions = append(conditions, fmt.Sprintf("code ILIKE $%d", argIndex))
		args = append(args, "%"+req.Code+"%")
		argIndex++
	}

	if req.IsActive != nil {
		if *req.IsActive {
			conditions = append(conditions, "is_active::text IN ('1','t','true','TRUE')")
		} else {
			conditions = append(conditions, "is_active::text NOT IN ('1','t','true','TRUE')")
		}
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM tenants WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count tenants: %w", err)
	}

	// Query tenants
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT t.id, t.name, COALESCE(t.code, '') AS code,
		       COALESCE(t.contact_name, '') AS contact_name,
		       COALESCE(t.contact_phone, '') AS contact_phone,
		       COALESCE(t.contact_email, '') AS contact_email,
		       COALESCE(t.industry, '') AS industry,
		       CASE
		           WHEN t.is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       COALESCE(t.valid_from, t.service_started_on) AS valid_from,
		       COALESCE(t.valid_to, t.service_expired_on) AS valid_to,
		       '' AS plan_name,
		       '' AS service_status,
		       NULL::timestamp AS expires_at,
		       NULL::bigint AS feature_group_id,
		       t.created_at, t.updated_at, t.deleted_at
		FROM tenants t
		WHERE %s
		ORDER BY t.created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(
			&t.ID,
			&t.Name,
			&t.Code,
			&t.ContactName,
			&t.ContactPhone,
			&t.ContactEmail,
			&t.Industry,
			&t.IsActive,
			&t.ValidFrom,
			&t.ValidTo,
			&t.SubscriptionPlan,
			&t.SubscriptionState,
			&t.SubscriptionEndAt,
			&t.FeatureGroupID,
			&t.CreatedAt,
			&t.UpdatedAt,
			&t.DeletedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan tenant: %w", err)
		}
		tenants = append(tenants, &t)
	}

	return tenants, total, nil
}

// GetTenantByID retrieves a tenant by ID
func (s *Store) GetTenantByID(ctx context.Context, id int64) (*Tenant, error) {
	query := `
		SELECT t.id, t.name, COALESCE(t.code, '') AS code,
		       COALESCE(t.contact_name, '') AS contact_name,
		       COALESCE(t.contact_phone, '') AS contact_phone,
		       COALESCE(t.contact_email, '') AS contact_email,
		       COALESCE(t.industry, '') AS industry,
		       CASE
		           WHEN t.is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       COALESCE(t.valid_from, t.service_started_on) AS valid_from,
		       COALESCE(t.valid_to, t.service_expired_on) AS valid_to,
		       '' AS plan_name,
		       '' AS service_status,
		       NULL::timestamp AS expires_at,
		       NULL::bigint AS feature_group_id,
		       t.created_at, t.updated_at, t.deleted_at
		FROM tenants t
		WHERE t.id = $1 AND t.deleted_at IS NULL
	`

	var t Tenant
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&t.ID,
		&t.Name,
		&t.Code,
		&t.ContactName,
		&t.ContactPhone,
		&t.ContactEmail,
		&t.Industry,
		&t.IsActive,
		&t.ValidFrom,
		&t.ValidTo,
		&t.SubscriptionPlan,
		&t.SubscriptionState,
		&t.SubscriptionEndAt,
		&t.FeatureGroupID,
		&t.CreatedAt,
		&t.UpdatedAt,
		&t.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("tenant not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query tenant: %w", err)
	}

	return &t, nil
}

// CreateTenant creates a new tenant
func (s *Store) CreateTenant(ctx context.Context, req CreateTenantRequest) (*Tenant, error) {
	usesInteger, err := s.tenantIsActiveUsesInteger(ctx)
	if err != nil {
		return nil, err
	}
	isActive := true
	var isActiveValue interface{} = isActive
	if usesInteger {
		if isActive {
			isActiveValue = 1
		} else {
			isActiveValue = 0
		}
	}
	validFrom := req.ValidFrom
	validTo := req.ValidTo
	if validFrom == nil {
		now := time.Now()
		validFrom = &now
	}
	if validTo == nil {
		end := validFrom.Add(365 * 24 * time.Hour)
		validTo = &end
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin tenant creation transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO tenants (name, code, contact_name, contact_phone, contact_email, industry, is_active, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		RETURNING id, name, code, COALESCE(contact_name, ''), COALESCE(contact_phone, ''), COALESCE(contact_email, ''), COALESCE(industry, ''),
		          CASE
		              WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		              ELSE false
		          END AS is_active,
		          valid_from, valid_to,
		          '' AS plan_name, '' AS service_status, NULL::timestamp AS expires_at, NULL::bigint AS feature_group_id,
		          created_at, updated_at, deleted_at
	`

	var t Tenant
	err = tx.QueryRow(ctx, query, req.Name, req.Code, req.ContactName, req.ContactPhone, req.ContactEmail, req.Industry, isActiveValue, validFrom, validTo).Scan(
		&t.ID,
		&t.Name,
		&t.Code,
		&t.ContactName,
		&t.ContactPhone,
		&t.ContactEmail,
		&t.Industry,
		&t.IsActive,
		&t.ValidFrom,
		&t.ValidTo,
		&t.SubscriptionPlan,
		&t.SubscriptionState,
		&t.SubscriptionEndAt,
		&t.FeatureGroupID,
		&t.CreatedAt,
		&t.UpdatedAt,
		&t.DeletedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	if err := s.ensureInstitutionRolesForTenantTx(ctx, tx, t.ID); err != nil {
		return nil, err
	}
	if err := s.createDefaultTenantAdminTx(ctx, tx, t.ID, req); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit tenant creation transaction: %w", err)
	}

	return &t, nil
}

func (s *Store) ensureInstitutionRolesForTenantTx(ctx context.Context, tx pgx.Tx, tenantID int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO institution_roles (tenant_id, name, code, description, is_active, created_at, updated_at)
		SELECT $1,
		       COALESCE(NULLIF(r.name_cn, ''), r.code),
		       r.code,
		       r.description,
		       COALESCE(r.is_active, true),
		       COALESCE(r.created_at, NOW()),
		       COALESCE(r.updated_at, COALESCE(r.created_at, NOW()))
		FROM inst_roles r
		WHERE lower(COALESCE(r.code, '')) NOT IN ('lingce_sales', 'inst_test_225052')
		  AND COALESCE(r.name_cn, '') <> '联调角色225052已编辑'
		ON CONFLICT (tenant_id, code) WHERE deleted_at IS NULL
		DO UPDATE SET
		    name = EXCLUDED.name,
		    description = EXCLUDED.description,
		    is_active = EXCLUDED.is_active,
		    updated_at = NOW()
	`, tenantID)
	if err != nil {
		return fmt.Errorf("failed to initialize tenant roles: %w", err)
	}
	return nil
}

func (s *Store) createDefaultTenantAdminTx(ctx context.Context, tx pgx.Tx, tenantID int64, req CreateTenantRequest) error {
	baseUsername := strings.TrimSpace(derefString(req.ContactPhone))
	if baseUsername == "" {
		baseUsername = strings.TrimSpace(req.Code) + "_admin"
	}
	fullName := strings.TrimSpace(derefString(req.ContactName))
	if fullName == "" {
		fullName = strings.TrimSpace(req.Name) + "管理员"
	}
	phone := strings.TrimSpace(derefString(req.ContactPhone))
	email := strings.TrimSpace(derefString(req.ContactEmail))
	if phone == "" {
		return fmt.Errorf("contact_phone is required for default tenant admin")
	}
	username := baseUsername

	var dupPhoneCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM employees
		WHERE phone = $1 AND deleted_at IS NULL
	`, phone).Scan(&dupPhoneCount); err != nil {
		return fmt.Errorf("failed to check duplicate admin phone: %w", err)
	}
	if dupPhoneCount > 0 {
		return fmt.Errorf("contact phone '%s' already exists", phone)
	}

	var dupUsernameCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM employees
		WHERE username = $1 AND deleted_at IS NULL
	`, username).Scan(&dupUsernameCount); err != nil {
		return fmt.Errorf("failed to check duplicate admin username: %w", err)
	}
	if dupUsernameCount > 0 {
		return fmt.Errorf("default admin username '%s' already exists", username)
	}

	passwordHashBytes, err := bcrypt.GenerateFromPassword([]byte(defaultTenantAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash default tenant admin password: %w", err)
	}
	departmentIDs, err := s.ensureDefaultDepartmentsTx(ctx, tx, tenantID)
	if err != nil {
		return err
	}
	adminDepartmentID, ok := departmentIDs["运营部"]
	if !ok {
		return fmt.Errorf("failed to resolve default operations department")
	}

	var employeeID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO employees (tenant_id, username, password_hash, name, full_name, phone, email, department_id, session_version, created_at, updated_at)
		VALUES ($1, $2, $3, $4::varchar, $5::text, $6, $7, $8, 1, NOW(), NOW())
		RETURNING id
	`, tenantID, username, string(passwordHashBytes), fullName, fullName, phone, email, adminDepartmentID).Scan(&employeeID)
	if err != nil {
		return fmt.Errorf("failed to create default tenant admin employee: %w", err)
	}

	var adminRoleID int64
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM institution_roles
		WHERE tenant_id = $1 AND lower(code) = 'admin' AND deleted_at IS NULL
		ORDER BY id DESC
		LIMIT 1
	`, tenantID).Scan(&adminRoleID)
	if err == pgx.ErrNoRows {
		err = tx.QueryRow(ctx, `
			INSERT INTO institution_roles (tenant_id, name, code, description, is_active, created_at, updated_at)
			VALUES ($1, '管理员', 'admin', '系统管理员角色', true, NOW(), NOW())
			RETURNING id
		`, tenantID).Scan(&adminRoleID)
	}
	if err != nil {
		return fmt.Errorf("failed to resolve tenant admin role: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO institution_employee_roles (employee_id, role_id, created_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (employee_id) DO UPDATE SET role_id = EXCLUDED.role_id
	`, employeeID, adminRoleID)
	if err != nil {
		return fmt.Errorf("failed to assign tenant admin role: %w", err)
	}
	roleIDByCode := map[string]int64{
		"admin": adminRoleID,
	}
	for _, item := range defaultTenantDepartments {
		roleCode := strings.TrimSpace(item.DefaultRole)
		if roleCode == "" {
			continue
		}
		if _, exists := roleIDByCode[roleCode]; exists {
			continue
		}
		var roleID int64
		roleErr := tx.QueryRow(ctx, `
			SELECT id
			FROM institution_roles
			WHERE tenant_id = $1 AND lower(code) = lower($2) AND deleted_at IS NULL
			ORDER BY id DESC
			LIMIT 1
		`, tenantID, roleCode).Scan(&roleID)
		if roleErr == nil {
			roleIDByCode[roleCode] = roleID
		}
	}
	for _, item := range defaultTenantDepartments {
		departmentID, exists := departmentIDs[item.Name]
		if !exists {
			continue
		}
		roleID, roleExists := roleIDByCode[item.DefaultRole]
		if !roleExists {
			roleID = adminRoleID
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO institution_department_roles (department_id, role_id, is_default, created_at)
			VALUES ($1, $2, true, NOW())
			ON CONFLICT (department_id) DO UPDATE SET role_id = EXCLUDED.role_id, is_default = EXCLUDED.is_default
		`, departmentID, roleID)
		if err != nil {
			return fmt.Errorf("failed to assign default role for department %s: %w", item.Name, err)
		}
	}

	return nil
}

func (s *Store) ensureDefaultDepartmentsTx(ctx context.Context, tx pgx.Tx, tenantID int64) (map[string]int64, error) {
	result := make(map[string]int64, len(defaultTenantDepartments))
	for _, item := range defaultTenantDepartments {
		var departmentID int64
		err := tx.QueryRow(ctx, `
			SELECT id
			FROM departments
			WHERE tenant_id = $1
			  AND deleted_at IS NULL
			  AND (code = $2 OR name = $3)
			ORDER BY id ASC
			LIMIT 1
		`, tenantID, item.Code, item.Name).Scan(&departmentID)
		if err == pgx.ErrNoRows {
			err = tx.QueryRow(ctx, `
				INSERT INTO departments (tenant_id, name, code, parent_id, is_active, created_at, updated_at)
				VALUES ($1, $2, $3, NULL, true, NOW(), NOW())
				RETURNING id
			`, tenantID, item.Name, item.Code).Scan(&departmentID)
			if err != nil {
				return nil, fmt.Errorf("failed to create default department %s: %w", item.Name, err)
			}
		} else if err != nil {
			return nil, fmt.Errorf("failed to query default department %s: %w", item.Name, err)
		} else {
			if _, err := tx.Exec(ctx, `
				UPDATE departments
				SET name = $1, code = $2, is_active = true, updated_at = NOW()
				WHERE id = $3
			`, item.Name, item.Code, departmentID); err != nil {
				return nil, fmt.Errorf("failed to refresh default department %s: %w", item.Name, err)
			}
		}
		result[item.Name] = departmentID
	}
	return result, nil
}

func (s *Store) resolveUniqueEmployeeIdentityTx(ctx context.Context, tx pgx.Tx, tenantID int64, baseUsername, phone string) (string, string, error) {
	username := strings.TrimSpace(baseUsername)
	if username == "" {
		username = fmt.Sprintf("tenant_%d_admin", tenantID)
	}

	isUsernameTaken := func(candidate string) (bool, error) {
		var count int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM employees
			WHERE username = $1 AND deleted_at IS NULL
		`, candidate).Scan(&count); err != nil {
			return false, fmt.Errorf("failed to check duplicate username: %w", err)
		}
		return count > 0, nil
	}

	taken, err := isUsernameTaken(username)
	if err != nil {
		return "", "", err
	}
	if taken {
		base := username
		for i := 0; i < 1000; i++ {
			candidate := fmt.Sprintf("%s_t%d_%d", base, tenantID, i+1)
			taken, err = isUsernameTaken(candidate)
			if err != nil {
				return "", "", err
			}
			if !taken {
				username = candidate
				break
			}
		}
	}

	isPhoneTaken := func(candidate string) (bool, error) {
		var count int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM employees
			WHERE phone = $1 AND deleted_at IS NULL
		`, candidate).Scan(&count); err != nil {
			return false, fmt.Errorf("failed to check duplicate employee phone: %w", err)
		}
		return count > 0, nil
	}

	phoneValue := strings.TrimSpace(phone)
	if phoneValue == "" {
		phoneValue = fmt.Sprintf("9%010d", tenantID)
	}
	phoneTaken, err := isPhoneTaken(phoneValue)
	if err != nil {
		return "", "", err
	}
	if phoneTaken {
		for i := 0; i < 1000; i++ {
			// deterministic unique fallback, still numeric-like and non-empty
			candidate := fmt.Sprintf("9%06d%04d", tenantID%1000000, i+1)
			taken, checkErr := isPhoneTaken(candidate)
			if checkErr != nil {
				return "", "", checkErr
			}
			if !taken {
				phoneValue = candidate
				break
			}
		}
	}

	return username, phoneValue, nil
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (s *Store) ResolveSubscriptionPlanID(ctx context.Context, planID *int64, planName *string) (*int64, error) {
	if planID != nil && *planID > 0 {
		var id int64
		err := s.pool.QueryRow(ctx, `
			SELECT id
			FROM tenant_subscription_plans
			WHERE id = $1
			  AND is_active::text IN ('1','t','true','TRUE')
			LIMIT 1
		`, *planID).Scan(&id)
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("subscription plan not found: %d", *planID)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to resolve subscription plan by id: %w", err)
		}
		return &id, nil
	}

	name := strings.TrimSpace(derefString(planName))
	if name == "" {
		return nil, nil
	}

	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT id
		FROM tenant_subscription_plans
		WHERE (name = $1 OR code = $1)
		  AND is_active::text IN ('1','t','true','TRUE')
		ORDER BY sort_order, created_at
		LIMIT 1
	`, name).Scan(&id)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("subscription plan not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve subscription plan by name: %w", err)
	}
	return &id, nil
}

// UpdateTenant updates a tenant
func (s *Store) UpdateTenant(ctx context.Context, id int64, req UpdateTenantRequest) (*Tenant, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *req.Name)
		argIndex++
	}

	if req.Code != nil {
		setClauses = append(setClauses, fmt.Sprintf("code = $%d", argIndex))
		args = append(args, *req.Code)
		argIndex++
	}

	if req.ContactName != nil {
		setClauses = append(setClauses, fmt.Sprintf("contact_name = $%d", argIndex))
		args = append(args, *req.ContactName)
		argIndex++
	}

	if req.ContactPhone != nil {
		setClauses = append(setClauses, fmt.Sprintf("contact_phone = $%d", argIndex))
		args = append(args, *req.ContactPhone)
		argIndex++
	}

	if req.ContactEmail != nil {
		setClauses = append(setClauses, fmt.Sprintf("contact_email = $%d", argIndex))
		args = append(args, *req.ContactEmail)
		argIndex++
	}

	if req.Industry != nil {
		setClauses = append(setClauses, fmt.Sprintf("industry = $%d", argIndex))
		args = append(args, *req.Industry)
		argIndex++
	}

	if req.IsActive != nil {
		usesInteger, typeErr := s.tenantIsActiveUsesInteger(ctx)
		if typeErr != nil {
			return nil, typeErr
		}
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIndex))
		if usesInteger {
			if *req.IsActive {
				args = append(args, 1)
			} else {
				args = append(args, 0)
			}
		} else {
			args = append(args, *req.IsActive)
		}
		argIndex++
	}

	if req.ValidFrom != nil {
		setClauses = append(setClauses, fmt.Sprintf("valid_from = $%d", argIndex))
		args = append(args, *req.ValidFrom)
		argIndex++
	}

	if req.ValidTo != nil {
		setClauses = append(setClauses, fmt.Sprintf("valid_to = $%d", argIndex))
		args = append(args, *req.ValidTo)
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetTenantByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE tenants
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, name, code, COALESCE(contact_name, ''), COALESCE(contact_phone, ''), COALESCE(contact_email, ''), COALESCE(industry, ''),
		          CASE
		              WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		              ELSE false
		          END AS is_active,
		          valid_from, valid_to,
		          '' AS plan_name, '' AS service_status, NULL::timestamp AS expires_at, NULL::bigint AS feature_group_id,
		          created_at, updated_at, deleted_at
	`, strings.Join(setClauses, ", "), argIndex)

	var t Tenant
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&t.ID,
		&t.Name,
		&t.Code,
		&t.ContactName,
		&t.ContactPhone,
		&t.ContactEmail,
		&t.Industry,
		&t.IsActive,
		&t.ValidFrom,
		&t.ValidTo,
		&t.SubscriptionPlan,
		&t.SubscriptionState,
		&t.SubscriptionEndAt,
		&t.FeatureGroupID,
		&t.CreatedAt,
		&t.UpdatedAt,
		&t.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("tenant not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update tenant: %w", err)
	}

	return &t, nil
}

// DeleteTenant soft deletes a tenant
func (s *Store) DeleteTenant(ctx context.Context, id int64) error {
	query := `
		UPDATE tenants
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("tenant not found")
	}

	return nil
}

// ListMedicalSpecialties retrieves all medical specialties.
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

	specialties := make([]*MedicalSpecialty, 0)
	for rows.Next() {
		var ms MedicalSpecialty
		if err := rows.Scan(&ms.ID, &ms.Name, &ms.Code, &ms.ParentID, &ms.Level, &ms.SortOrder); err != nil {
			return nil, fmt.Errorf("failed to scan medical specialty: %w", err)
		}
		specialties = append(specialties, &ms)
	}
	return specialties, nil
}

func (s *Store) GetInstitutionStatistics(ctx context.Context, tenantID int64) (*InstitutionStatistics, error) {
	stats := &InstitutionStatistics{TenantID: tenantID}

	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM employees WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID).
		Scan(&stats.TotalEmployees); err != nil {
		return nil, fmt.Errorf("failed to query employee stats: %w", err)
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM departments WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID).
		Scan(&stats.TotalDepartments); err != nil {
		return nil, fmt.Errorf("failed to query department stats: %w", err)
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM badge_devices WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID).
		Scan(&stats.TotalBadgeDevices); err != nil {
		stats.TotalBadgeDevices = 0
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM medical_recordings WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID).
		Scan(&stats.TotalRecordings); err != nil {
		return nil, fmt.Errorf("failed to query recording stats: %w", err)
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM medical_recordings
		WHERE tenant_id = $1 AND deleted_at IS NULL
		  AND created_at >= date_trunc('week', NOW())
	`, tenantID).Scan(&stats.RecordingsThisWeek); err != nil {
		return nil, fmt.Errorf("failed to query weekly recording stats: %w", err)
	}

	return stats, nil
}

// GetTenantStatistics retrieves tenant statistics
func (s *Store) GetTenantStatistics(ctx context.Context) (map[string]interface{}, error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE is_active = true) as active
		FROM tenants
		WHERE deleted_at IS NULL
	`

	var total, active int64
	if err := s.pool.QueryRow(ctx, query).Scan(&total, &active); err != nil {
		return nil, fmt.Errorf("failed to query tenant stats: %w", err)
	}

	return map[string]interface{}{
		"total_tenants":  total,
		"active_tenants": active,
	}, nil
}
