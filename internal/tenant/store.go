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

type TrialDemoEmployeeSeed struct {
	FullName       string
	Username       string
	Phone          string
	DepartmentName string
	RoleCode       string
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

func (s *Store) tableExistsTx(ctx context.Context, tx pgx.Tx, tableName string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+tableName).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to detect table %s: %w", tableName, err)
	}
	return exists, nil
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
		       COALESCE(NULLIF(trim(t.account_mode), ''), 'formal') AS account_mode,
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
			&t.AccountMode,
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
		       COALESCE(NULLIF(trim(t.account_mode), ''), 'formal') AS account_mode,
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
		&t.AccountMode,
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
	accountMode := normalizeAccountMode(req.AccountMode)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin tenant creation transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO tenants (name, code, account_mode, contact_name, contact_phone, contact_email, industry, is_active, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		RETURNING id, name, code, COALESCE(NULLIF(trim(account_mode), ''), 'formal'), COALESCE(contact_name, ''), COALESCE(contact_phone, ''), COALESCE(contact_email, ''), COALESCE(industry, ''),
		          CASE
		              WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		              ELSE false
		          END AS is_active,
		          valid_from, valid_to,
		          '' AS plan_name, '' AS service_status, NULL::timestamp AS expires_at, NULL::bigint AS feature_group_id,
		          created_at, updated_at, deleted_at
	`

	var t Tenant
	err = tx.QueryRow(ctx, query, req.Name, req.Code, accountMode, req.ContactName, req.ContactPhone, req.ContactEmail, req.Industry, isActiveValue, validFrom, validTo).Scan(
		&t.ID,
		&t.Name,
		&t.Code,
		&t.AccountMode,
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

	hasInstitutionEmployeeRoles, err := s.tableExistsTx(ctx, tx, "institution_employee_roles")
	if err != nil {
		return err
	}
	hasInstitutionRoleMenus, err := s.tableExistsTx(ctx, tx, "institution_role_menus")
	if err != nil {
		return err
	}
	hasInstitutionMenus, err := s.tableExistsTx(ctx, tx, "institution_menus")
	if err != nil {
		return err
	}

	if hasInstitutionEmployeeRoles {
		_, err = tx.Exec(ctx, `
			DELETE FROM institution_employee_roles
			WHERE tenant_id = $1 AND employee_id = $2
		`, tenantID, employeeID)
		if err != nil {
			return fmt.Errorf("failed to clear existing institution tenant admin role mirror: %w", err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO institution_employee_roles (employee_id, role_id, tenant_id, role_code, source, created_at, updated_at)
			VALUES ($1, $2, $3, 'admin', 'tenant_init', NOW(), NOW())
		`, employeeID, adminRoleID, tenantID)
		if err != nil {
			return fmt.Errorf("failed to assign institution tenant admin role mirror: %w", err)
		}
	}
	if hasInstitutionRoleMenus && hasInstitutionMenus {
		_, err = tx.Exec(ctx, `
			INSERT INTO institution_role_menus (tenant_id, role_code, menu_code, created_at, updated_at)
			SELECT $1, 'admin', m.code, NOW(), NOW()
			FROM institution_menus m
			WHERE m.deleted_at IS NULL
			  AND (m.tenant_id = $1 OR m.tenant_id IS NULL)
			  AND COALESCE(m.is_active, true) = true
			  AND COALESCE(m.is_default_for_admin, false) = true
			ON CONFLICT (tenant_id, role_code, menu_code) DO NOTHING
		`, tenantID)
		if err != nil {
			return fmt.Errorf("failed to assign institution tenant admin menus: %w", err)
		}
	} else {
		return fmt.Errorf("institution role menu tables not prepared for tenant initialization")
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
		roleCode := strings.TrimSpace(item.DefaultRole)
		roleID, roleExists := roleIDByCode[roleCode]
		if !roleExists {
			roleID = adminRoleID
			roleCode = "admin"
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO institution_department_roles (department_id, role_id, tenant_id, role_code, is_default, created_at, updated_at)
			VALUES ($1, $2, $3, $4, true, NOW(), NOW())
			ON CONFLICT (department_id) DO UPDATE
			SET role_id = EXCLUDED.role_id,
			    tenant_id = EXCLUDED.tenant_id,
			    role_code = EXCLUDED.role_code,
			    is_default = EXCLUDED.is_default,
			    updated_at = NOW()
		`, departmentID, roleID, tenantID, roleCode)
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

func normalizeAccountMode(v *string) string {
	mode := strings.ToLower(strings.TrimSpace(derefString(v)))
	switch mode {
	case "trial":
		return "trial"
	default:
		return "formal"
	}
}

func (s *Store) GetTrialEmployeeID(ctx context.Context, tenantID int64, usernamePrefix string) (int64, error) {
	var employeeID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id
		FROM employees
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
		  AND username = $2
		ORDER BY id ASC
		LIMIT 1
	`, tenantID, fmt.Sprintf("%s_%d", strings.TrimSpace(usernamePrefix), tenantID)).Scan(&employeeID)
	if err != nil {
		return 0, fmt.Errorf("failed to get trial employee %s: %w", usernamePrefix, err)
	}
	return employeeID, nil
}

func (s *Store) EnsureTrialInstitutionMenus(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin trial menu transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	hasInstitutionMenus, err := s.tableExistsTx(ctx, tx, "institution_menus")
	if err != nil {
		return err
	}
	if !hasInstitutionMenus {
		return fmt.Errorf("institution menus table not prepared")
	}

	parentID, err := s.ensureInstitutionMenuTx(ctx, tx, nil, "试用体验", "trial_experience", nil, 0, false)
	if err != nil {
		return err
	}
	if _, err := s.ensureInstitutionMenuTx(ctx, tx, parentID, "试用首页", "trial_home", stringPtrValue("/trial-home"), 0, true); err != nil {
		return err
	}
	if _, err := s.ensureInstitutionMenuTx(ctx, tx, parentID, "上传录音", "recording_upload", stringPtrValue("/recording-upload"), 1, true); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit trial menu transaction: %w", err)
	}
	return nil
}

func (s *Store) ensureInstitutionMenuTx(ctx context.Context, tx pgx.Tx, parentID *int64, name, code string, path *string, sortOrder int, featureAssignable bool) (*int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM institution_menus
		WHERE tenant_id IS NULL
		  AND lower(code) = lower($1)
		  AND deleted_at IS NULL
		ORDER BY id ASC
		LIMIT 1
	`, code).Scan(&id)
	if err == nil {
		_, updateErr := tx.Exec(ctx, `
			UPDATE institution_menus
			SET name = $1,
			    path = $2,
			    parent_id = $3,
			    sort_order = $4,
			    is_active = true,
			    is_feature_assignable = $5,
			    updated_at = NOW()
			WHERE id = $6
		`, name, path, parentID, sortOrder, featureAssignable, id)
		if updateErr != nil {
			return nil, fmt.Errorf("failed to refresh institution menu %s: %w", code, updateErr)
		}
		return &id, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("failed to query institution menu %s: %w", code, err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO institution_menus (
			tenant_id, name, code, path, icon, parent_id, sort_order, is_active,
			is_feature_assignable, is_default_for_admin, feature_code, feature_name, created_at, updated_at
		)
		VALUES (NULL, $1, $2, $3, NULL, $4, $5, true, $6, false, NULL, NULL, NOW(), NOW())
		RETURNING id
	`, name, code, path, parentID, sortOrder, featureAssignable).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to create institution menu %s: %w", code, err)
	}
	return &id, nil
}

func (s *Store) EnsureTrialDemoEmployees(ctx context.Context, tenantID int64, seeds []TrialDemoEmployeeSeed) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin trial employee transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	departmentIDs, err := s.ensureDefaultDepartmentsTx(ctx, tx, tenantID)
	if err != nil {
		return err
	}
	for index, seed := range seeds {
		if err := s.ensureTrialDemoEmployeeTx(ctx, tx, tenantID, departmentIDs, seed, index); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit trial employee transaction: %w", err)
	}
	return nil
}

func (s *Store) ensureTrialDemoEmployeeTx(ctx context.Context, tx pgx.Tx, tenantID int64, departmentIDs map[string]int64, seed TrialDemoEmployeeSeed, index int) error {
	username := fmt.Sprintf("%s_%d", seed.Username, tenantID)
	phone := fmt.Sprintf("9%010d", tenantID*10+int64(index+1))

	var employeeID int64
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM employees
		WHERE tenant_id = $1
		  AND username = $2
		  AND deleted_at IS NULL
		ORDER BY id ASC
		LIMIT 1
	`, tenantID, username).Scan(&employeeID)
	if err == pgx.ErrNoRows {
		passwordHashBytes, hashErr := bcrypt.GenerateFromPassword([]byte(defaultTenantAdminPassword), bcrypt.DefaultCost)
		if hashErr != nil {
			return fmt.Errorf("failed to hash trial demo password: %w", hashErr)
		}
		departmentID, ok := departmentIDs[seed.DepartmentName]
		if !ok {
			return fmt.Errorf("missing department for trial employee: %s", seed.DepartmentName)
		}
		resolvedUsername, resolvedPhone, resolveErr := s.resolveUniqueEmployeeIdentityTx(ctx, tx, tenantID, username, phone)
		if resolveErr != nil {
			return resolveErr
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO employees (tenant_id, username, password_hash, name, full_name, phone, email, department_id, session_version, is_active, created_at, updated_at)
			VALUES ($1, $2, $3, $4::varchar, $5::text, $6, $7, $8, 0, '1', NOW(), NOW())
			RETURNING id
		`, tenantID, resolvedUsername, string(passwordHashBytes), seed.FullName, seed.FullName, resolvedPhone, "", departmentID).Scan(&employeeID)
		if err != nil {
			return fmt.Errorf("failed to create trial demo employee %s: %w", seed.Username, err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to query trial demo employee %s: %w", seed.Username, err)
	}

	return s.ensureEmployeeRoleAssignmentTx(ctx, tx, tenantID, employeeID, seed.RoleCode)
}

func (s *Store) ensureEmployeeRoleAssignmentTx(ctx context.Context, tx pgx.Tx, tenantID, employeeID int64, roleCode string) error {
	var roleID int64
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM institution_roles
		WHERE tenant_id = $1
		  AND lower(code) = lower($2)
		  AND deleted_at IS NULL
		ORDER BY id DESC
		LIMIT 1
	`, tenantID, roleCode).Scan(&roleID)
	if err != nil {
		return fmt.Errorf("failed to resolve trial employee role %s: %w", roleCode, err)
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM institution_employee_roles
		WHERE tenant_id = $1
		  AND employee_id = $2
		  AND lower(role_code) = lower($3)
	`, tenantID, employeeID, roleCode)
	if err != nil {
		return fmt.Errorf("failed to clear trial employee role %s: %w", roleCode, err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO institution_employee_roles (employee_id, role_id, tenant_id, role_code, source, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'trial_bootstrap', NOW(), NOW())
	`, employeeID, roleID, tenantID, roleCode)
	if err != nil {
		return fmt.Errorf("failed to assign trial employee role %s: %w", roleCode, err)
	}
	return nil
}

func stringPtrValue(v string) *string {
	return &v
}

func (s *Store) EnsureTrialAnalysisRoutes(ctx context.Context, tenantID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin trial analysis route transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		WITH target_roles AS (
			SELECT tenant_id, id AS role_id, lower(code) AS role_code
			FROM institution_roles
			WHERE tenant_id = $1
			  AND lower(code) IN ('doctor', 'consultant')
			  AND deleted_at IS NULL
		),
		route_seed AS (
			SELECT tenant_id, role_id, role_code, 'doctor_patient'::text AS pipeline_code, 'v1'::text AS pipeline_version
			FROM target_roles
			WHERE role_code = 'doctor'
			UNION ALL
			SELECT tenant_id, role_id, role_code, 'consultant_conversion'::text AS pipeline_code, 'v1'::text AS pipeline_version
			FROM target_roles
			WHERE role_code = 'consultant'
		)
		INSERT INTO worker_analysis_routes (
			tenant_id, role_id, role_code, pipeline_code, pipeline_version,
			enabled, effective_from, remark, created_at, updated_at
		)
		SELECT
			rs.tenant_id, rs.role_id, rs.role_code, rs.pipeline_code, rs.pipeline_version,
			TRUE, NOW(), 'trial bootstrap route sync', NOW(), NOW()
		FROM route_seed rs
		WHERE NOT EXISTS (
			SELECT 1
			FROM worker_analysis_routes war
			WHERE war.tenant_id = rs.tenant_id
			  AND war.role_id = rs.role_id
			  AND war.pipeline_code = rs.pipeline_code
			  AND COALESCE(war.pipeline_version, '') = rs.pipeline_version
			  AND war.enabled = TRUE
			  AND war.deleted_at IS NULL
		)
	`, tenantID); err != nil {
		return fmt.Errorf("seed worker analysis routes for trial tenant: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit trial analysis route transaction: %w", err)
	}
	return nil
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

	if req.AccountMode != nil {
		setClauses = append(setClauses, fmt.Sprintf("account_mode = $%d", argIndex))
		args = append(args, normalizeAccountMode(req.AccountMode))
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
		RETURNING id, name, code, COALESCE(NULLIF(trim(account_mode), ''), 'formal'), COALESCE(contact_name, ''), COALESCE(contact_phone, ''), COALESCE(contact_email, ''), COALESCE(industry, ''),
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
		&t.AccountMode,
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

type trialDemoAssetSource struct {
	AssetID               int64
	TemplateCode          string
	AssetCode             string
	RoleCode              string
	Title                 string
	SourceTenantID        int64
	SourceRecordingID     int64
	SourceCustomerID      *int64
	FileURL               string
	FileName              string
	Duration              *int
	MIMEType              string
	Source                *string
	Scene                 *string
	BusinessScope         string
	TranscriptText        *string
	AnalysisResult        []byte
	AnalysisDisplay       []byte
	CleanedTranscription  []byte
	TranscriptionSegments []byte
	AnalysisStatus        *string
	QualityScore          *float64
	RecordedAt            *time.Time
	OSSKey                string
}

func (s *Store) ListTenantTrialDemoRecordings(ctx context.Context, tenantID int64) ([]TenantTrialDemoRecording, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tenant_id, template_code, role_code, asset_id, recording_id, employee_id, customer_id, title
		FROM tenant_trial_demo_recordings
		WHERE tenant_id = $1
		ORDER BY role_code ASC, id ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query tenant trial demo recordings: %w", err)
	}
	defer rows.Close()
	items := make([]TenantTrialDemoRecording, 0)
	for rows.Next() {
		var item TenantTrialDemoRecording
		if err := rows.Scan(&item.TenantID, &item.TemplateCode, &item.RoleCode, &item.AssetID, &item.RecordingID, &item.EmployeeID, &item.CustomerID, &item.Title); err != nil {
			return nil, fmt.Errorf("scan tenant trial demo recording: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant trial demo recordings: %w", err)
	}
	return items, nil
}

func (s *Store) EnsureTrialDemoRecordings(ctx context.Context, tenantID int64, templateCode string) ([]TenantTrialDemoRecording, error) {
	templateCode = strings.TrimSpace(templateCode)
	if templateCode == "" {
		templateCode = "intent_trial_v1"
	}
	existing, err := s.ListTenantTrialDemoRecordings(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	byRole := make(map[string]TenantTrialDemoRecording, len(existing))
	for _, item := range existing {
		byRole[strings.ToLower(strings.TrimSpace(item.RoleCode))] = item
	}
	assets, err := s.listActiveTrialDemoAssets(ctx, templateCode)
	if err != nil {
		return nil, err
	}
	for _, asset := range assets {
		roleCode := strings.ToLower(strings.TrimSpace(asset.RoleCode))
		if existingItem, ok := byRole[roleCode]; ok {
			if err := s.syncTrialDemoAssetToExistingRecording(ctx, existingItem, asset); err != nil {
				return nil, err
			}
			continue
		}
		usernamePrefix := "trial_doctor"
		if roleCode == "consultant" {
			usernamePrefix = "trial_consultant"
		}
		employeeID, err := s.GetTrialEmployeeID(ctx, tenantID, usernamePrefix)
		if err != nil {
			return nil, err
		}
		created, err := s.cloneTrialDemoAssetToTenant(ctx, tenantID, employeeID, asset)
		if err != nil {
			return nil, err
		}
		byRole[roleCode] = created
	}
	out := make([]TenantTrialDemoRecording, 0, len(byRole))
	for _, role := range []string{"doctor", "consultant"} {
		if item, ok := byRole[role]; ok {
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *Store) syncTrialDemoAssetToExistingRecording(ctx context.Context, existing TenantTrialDemoRecording, asset trialDemoAssetSource) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin sync existing trial demo transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var customerID *int64
	if asset.SourceCustomerID != nil && *asset.SourceCustomerID > 0 {
		cid, err := s.cloneTrialDemoCustomerTx(ctx, tx, existing.TenantID, *asset.SourceCustomerID)
		if err != nil {
			return err
		}
		customerID = &cid
	}

	if _, err := tx.Exec(ctx, `
		UPDATE recordings
		SET customer_id = $1,
		    file_url = $2,
		    file_name = $3,
		    duration = $4,
		    mime_type = $5,
		    source = $6,
		    scene = $7,
		    business_scope = $8,
		    transcription_status = 'completed',
		    cleaned_transcription_status = 'completed',
		    analysis_status = $9,
		    transcription_text = $10,
		    analysis_result = $11::jsonb,
		    analysis_display = $12::jsonb,
		    cleaned_transcription = $13::jsonb,
		    transcription_segments = $14::jsonb,
		    quality_score = $15,
		    recorded_at = COALESCE($16, recorded_at),
		    oss_key = NULLIF($17, ''),
		    source_tenant_id = $18,
		    source_recording_id = $19,
		    updated_at = NOW()
		WHERE id = $20
	`, customerID, asset.FileURL, asset.FileName, asset.Duration, asset.MIMEType, nullableStringPtr(asset.Source), nullableStringPtr(asset.Scene), asset.BusinessScope, defaultString(nullableStringPtr(asset.AnalysisStatus), "completed"), nullableStringPtr(asset.TranscriptText), string(asset.AnalysisResult), string(asset.AnalysisDisplay), string(asset.CleanedTranscription), string(asset.TranscriptionSegments), asset.QualityScore, asset.RecordedAt, asset.OSSKey, asset.SourceTenantID, asset.SourceRecordingID, existing.RecordingID); err != nil {
		return fmt.Errorf("update existing trial demo recording: %w", err)
	}

	if err := s.clearTrialDemoDerivedDataTx(ctx, tx, existing.RecordingID); err != nil {
		return err
	}
	if err := s.cloneTrialDemoAnalysisRowsTx(ctx, tx, existing.TenantID, asset.SourceRecordingID, existing.RecordingID); err != nil {
		return err
	}
	if err := s.cloneTrialDemoRouteResultsTx(ctx, tx, existing.TenantID, asset.SourceRecordingID, existing.RecordingID); err != nil {
		return err
	}
	if err := s.cloneTrialDemoSegueScoresTx(ctx, tx, existing.TenantID, asset.SourceRecordingID, existing.RecordingID, existing.EmployeeID); err != nil {
		return err
	}
	if err := s.cloneTrialDemoEMRDraftsTx(ctx, tx, existing.TenantID, asset.SourceRecordingID, existing.RecordingID, customerID); err != nil {
		return err
	}
	if err := s.cloneTrialDemoContentSeedsTx(ctx, tx, existing.TenantID, asset.SourceRecordingID, existing.RecordingID, existing.EmployeeID); err != nil {
		return err
	}
	if err := s.cloneTrialDemoAnalysisRunsTx(ctx, tx, existing.TenantID, asset.SourceRecordingID, existing.RecordingID, existing.EmployeeID); err != nil {
		return err
	}
	if err := s.cloneTrialDemoWorkerRunsTx(ctx, tx, existing.TenantID, asset.SourceRecordingID, existing.RecordingID, existing.EmployeeID); err != nil {
		return err
	}
	if err := s.cloneTrialDemoTasksTx(ctx, tx, existing.TenantID, asset.SourceRecordingID, existing.RecordingID, customerID, existing.EmployeeID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tenant_trial_demo_recordings
		SET asset_id = $1,
		    customer_id = $2,
		    title = $3
		WHERE tenant_id = $4 AND template_code = $5 AND role_code = $6
	`, asset.AssetID, customerID, asset.Title, existing.TenantID, existing.TemplateCode, existing.RoleCode); err != nil {
		return fmt.Errorf("refresh tenant trial demo recording mapping: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit sync existing trial demo transaction: %w", err)
	}
	return nil
}

func (s *Store) clearTrialDemoDerivedDataTx(ctx context.Context, tx pgx.Tx, recordingID int64) error {
	statements := []string{
		`DELETE FROM recording_tasks WHERE recording_id = $1`,
		`DELETE FROM recording_analysis_results WHERE recording_id = $1`,
		`DELETE FROM recording_route_results WHERE recording_id = $1`,
		`DELETE FROM recording_segue_scores WHERE recording_id = $1`,
		`DELETE FROM recording_emr_drafts WHERE recording_id = $1`,
		`DELETE FROM recording_content_seeds WHERE recording_id = $1`,
		`DELETE FROM analysis_step_runs WHERE recording_id = $1`,
		`DELETE FROM analysis_runs WHERE recording_id = $1`,
		`DELETE FROM worker_analysis_run_steps WHERE recording_id = $1`,
		`DELETE FROM worker_analysis_runs WHERE recording_id = $1`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement, recordingID); err != nil {
			return fmt.Errorf("clear existing trial demo derived data: %w", err)
		}
	}
	return nil
}

func (s *Store) listActiveTrialDemoAssets(ctx context.Context, templateCode string) ([]trialDemoAssetSource, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			a.id,
			a.template_code,
			COALESCE(a.asset_code, ''),
			lower(trim(a.role_code)) AS role_code,
			a.title,
			a.source_tenant_id,
			a.source_recording_id,
			a.source_customer_id,
			COALESCE(r.file_url, ''),
			COALESCE(r.file_name, ''),
			r.duration,
			COALESCE(r.mime_type, ''),
			r.source,
			r.scene,
			COALESCE(NULLIF(r.business_scope, ''), lower(trim(a.role_code))) AS business_scope,
			r.transcription_text,
			COALESCE(r.analysis_result::jsonb, '{}'::jsonb)::text,
			COALESCE(r.analysis_display::jsonb, '{}'::jsonb)::text,
			COALESCE(r.cleaned_transcription::jsonb, '[]'::jsonb)::text,
			COALESCE(r.transcription_segments::jsonb, '[]'::jsonb)::text,
			r.analysis_status,
			r.quality_score,
			r.recorded_at,
			COALESCE(r.oss_key, '')
		FROM trial_demo_recording_assets a
		JOIN recordings r ON r.id = a.source_recording_id AND r.tenant_id = a.source_tenant_id
		WHERE a.is_active = TRUE
		  AND a.template_code = $1
		ORDER BY a.sort_order ASC, a.id ASC
	`, templateCode)
	if err != nil {
		return nil, fmt.Errorf("query active trial demo assets: %w", err)
	}
	defer rows.Close()
	items := make([]trialDemoAssetSource, 0)
	for rows.Next() {
		var item trialDemoAssetSource
		var analysisResultText, analysisDisplayText, cleanedText, segmentsText string
		if err := rows.Scan(
			&item.AssetID,
			&item.TemplateCode,
			&item.AssetCode,
			&item.RoleCode,
			&item.Title,
			&item.SourceTenantID,
			&item.SourceRecordingID,
			&item.SourceCustomerID,
			&item.FileURL,
			&item.FileName,
			&item.Duration,
			&item.MIMEType,
			&item.Source,
			&item.Scene,
			&item.BusinessScope,
			&item.TranscriptText,
			&analysisResultText,
			&analysisDisplayText,
			&cleanedText,
			&segmentsText,
			&item.AnalysisStatus,
			&item.QualityScore,
			&item.RecordedAt,
			&item.OSSKey,
		); err != nil {
			return nil, fmt.Errorf("scan active trial demo asset: %w", err)
		}
		item.AnalysisResult = []byte(analysisResultText)
		item.AnalysisDisplay = []byte(analysisDisplayText)
		item.CleanedTranscription = []byte(cleanedText)
		item.TranscriptionSegments = []byte(segmentsText)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active trial demo assets: %w", err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no active trial demo assets found for template %s", templateCode)
	}
	return items, nil
}

func (s *Store) cloneTrialDemoAssetToTenant(ctx context.Context, tenantID, employeeID int64, asset trialDemoAssetSource) (TenantTrialDemoRecording, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TenantTrialDemoRecording{}, fmt.Errorf("begin trial demo clone transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var customerID *int64
	if asset.SourceCustomerID != nil && *asset.SourceCustomerID > 0 {
		cid, err := s.cloneTrialDemoCustomerTx(ctx, tx, tenantID, *asset.SourceCustomerID)
		if err != nil {
			return TenantTrialDemoRecording{}, err
		}
		customerID = &cid
	}

	analysisStatus := "completed"
	if asset.AnalysisStatus != nil && strings.TrimSpace(*asset.AnalysisStatus) != "" {
		status := strings.TrimSpace(*asset.AnalysisStatus)
		if status != "pending" && status != "queued" {
			analysisStatus = status
		}
	}

	var recordingID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO recordings (
			tenant_id, employee_id, customer_id, file_url, file_name, duration, mime_type,
			source, scene, business_scope, status,
			transcription_status, cleaned_transcription_status, analysis_status,
			transcription_text, analysis_result, analysis_display,
			cleaned_transcription, transcription_segments, quality_score, recorded_at,
			oss_key, source_tenant_id, source_recording_id,
			created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, 'uploaded',
			'completed', 'completed', $11,
			$12, $13::jsonb, $14::jsonb,
			$15::jsonb, $16::jsonb, $17, COALESCE($18, NOW()),
			NULLIF($19, ''), $20, $21,
			NOW(), NOW()
		)
		RETURNING id
	`, tenantID, employeeID, customerID, asset.FileURL, asset.FileName, asset.Duration, asset.MIMEType, nullableStringPtr(asset.Source), nullableStringPtr(asset.Scene), asset.BusinessScope, analysisStatus, nullableStringPtr(asset.TranscriptText), string(asset.AnalysisResult), string(asset.AnalysisDisplay), string(asset.CleanedTranscription), string(asset.TranscriptionSegments), asset.QualityScore, asset.RecordedAt, asset.OSSKey, asset.SourceTenantID, asset.SourceRecordingID).Scan(&recordingID)
	if err != nil {
		return TenantTrialDemoRecording{}, fmt.Errorf("insert cloned trial demo recording: %w", err)
	}

	if err := s.cloneTrialDemoAnalysisRowsTx(ctx, tx, tenantID, asset.SourceRecordingID, recordingID); err != nil {
		return TenantTrialDemoRecording{}, err
	}
	if err := s.cloneTrialDemoRouteResultsTx(ctx, tx, tenantID, asset.SourceRecordingID, recordingID); err != nil {
		return TenantTrialDemoRecording{}, err
	}
	if err := s.cloneTrialDemoSegueScoresTx(ctx, tx, tenantID, asset.SourceRecordingID, recordingID, employeeID); err != nil {
		return TenantTrialDemoRecording{}, err
	}
	if err := s.cloneTrialDemoEMRDraftsTx(ctx, tx, tenantID, asset.SourceRecordingID, recordingID, customerID); err != nil {
		return TenantTrialDemoRecording{}, err
	}
	if err := s.cloneTrialDemoContentSeedsTx(ctx, tx, tenantID, asset.SourceRecordingID, recordingID, employeeID); err != nil {
		return TenantTrialDemoRecording{}, err
	}
	if err := s.cloneTrialDemoAnalysisRunsTx(ctx, tx, tenantID, asset.SourceRecordingID, recordingID, employeeID); err != nil {
		return TenantTrialDemoRecording{}, err
	}
	if err := s.cloneTrialDemoWorkerRunsTx(ctx, tx, tenantID, asset.SourceRecordingID, recordingID, employeeID); err != nil {
		return TenantTrialDemoRecording{}, err
	}
	if err := s.cloneTrialDemoTasksTx(ctx, tx, tenantID, asset.SourceRecordingID, recordingID, customerID, employeeID); err != nil {
		return TenantTrialDemoRecording{}, err
	}

	var result TenantTrialDemoRecording
	err = tx.QueryRow(ctx, `
		INSERT INTO tenant_trial_demo_recordings (
			tenant_id, template_code, role_code, asset_id, recording_id, employee_id, customer_id, title, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (tenant_id, template_code, role_code)
		DO UPDATE SET
			asset_id = EXCLUDED.asset_id,
			recording_id = EXCLUDED.recording_id,
			employee_id = EXCLUDED.employee_id,
			customer_id = EXCLUDED.customer_id,
			title = EXCLUDED.title
		RETURNING tenant_id, template_code, role_code, asset_id, recording_id, employee_id, customer_id, title
	`, tenantID, asset.TemplateCode, asset.RoleCode, asset.AssetID, recordingID, employeeID, customerID, asset.Title).Scan(
		&result.TenantID,
		&result.TemplateCode,
		&result.RoleCode,
		&result.AssetID,
		&result.RecordingID,
		&result.EmployeeID,
		&result.CustomerID,
		&result.Title,
	)
	if err != nil {
		return TenantTrialDemoRecording{}, fmt.Errorf("upsert tenant trial demo recording: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return TenantTrialDemoRecording{}, fmt.Errorf("commit trial demo clone transaction: %w", err)
	}
	return result, nil
}

func (s *Store) cloneTrialDemoCustomerTx(ctx context.Context, tx pgx.Tx, tenantID, sourceCustomerID int64) (int64, error) {
	var existingID int64
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM customers
		WHERE tenant_id = $1
		  AND COALESCE(extra_data->>'trial_demo_source_customer_id', '') = $2::text
		  AND deleted_at IS NULL
		ORDER BY id ASC
		LIMIT 1
	`, tenantID, fmt.Sprintf("%d", sourceCustomerID)).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if err != pgx.ErrNoRows {
		return 0, fmt.Errorf("query cloned trial demo customer: %w", err)
	}

	var clonedID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO customers (
			tenant_id, name, phone, email, gender, age, source, lifecycle_stage, status, momentum,
			assigned_to, next_follow_up_at, notes, extra_data, created_by, created_at, updated_at
		)
		SELECT
			$1,
			name,
			phone,
			email,
			gender,
			age,
			COALESCE(source, 'trial_demo'),
			COALESCE(NULLIF(lifecycle_stage, ''), 'unknown'),
			COALESCE(NULLIF(status, ''), 'lead'),
			COALESCE(momentum, 50),
			NULL,
			next_follow_up_at,
			notes,
			COALESCE(extra_data, '{}'::jsonb) || jsonb_build_object('trial_demo_source_customer_id', ($2::bigint)::text),
			0,
			NOW(),
			NOW()
		FROM customers
		WHERE id = $2::bigint
		RETURNING id
	`, tenantID, sourceCustomerID).Scan(&clonedID)
	if err != nil {
		return 0, fmt.Errorf("clone trial demo customer: %w", err)
	}
	return clonedID, nil
}

func (s *Store) cloneTrialDemoAnalysisRowsTx(ctx context.Context, tx pgx.Tx, tenantID, sourceRecordingID, targetRecordingID int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO recording_analysis_results (
			recording_id, tenant_id, prompt_code, result_data,
			run_id, step_run_id, pipeline_code, pipeline_version,
			is_active, created_at, updated_at
		)
		SELECT
			$3, $1, prompt_code, result_data,
			run_id, step_run_id, pipeline_code, pipeline_version,
			is_active, NOW(), NOW()
		FROM recording_analysis_results
		WHERE recording_id = $2
	`, tenantID, sourceRecordingID, targetRecordingID)
	if err != nil {
		return fmt.Errorf("clone trial demo analysis rows: %w", err)
	}
	return nil
}

func (s *Store) cloneTrialDemoRouteResultsTx(ctx context.Context, tx pgx.Tx, tenantID, sourceRecordingID, targetRecordingID int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO recording_route_results (
			recording_id, tenant_id, role_type, specialty_group, scene_type,
			care_goal_type, route_source, route_trace, routed_at, routed_by
		)
		SELECT
			$3, $1, role_type, specialty_group, scene_type,
			care_goal_type, route_source, route_trace, routed_at, routed_by
		FROM recording_route_results
		WHERE recording_id = $2
	`, tenantID, sourceRecordingID, targetRecordingID)
	if err != nil {
		return fmt.Errorf("clone trial demo route results: %w", err)
	}
	return nil
}

func (s *Store) cloneTrialDemoSegueScoresTx(ctx context.Context, tx pgx.Tx, tenantID, sourceRecordingID, targetRecordingID, employeeID int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO recording_segue_scores (
			recording_id, tenant_id, employee_id, segue_raw, segue_percent,
			g1_score, g2_score, g3_score, g4_score, g5_score, g6_score,
			critical_gap, critical_missing_items, uncertain_rate, low_confidence,
			tcm_m_score, tcm_composite_score, prompt_bundle_version, analyzed_at
		)
		SELECT
			$3, $1, $4, segue_raw, segue_percent,
			g1_score, g2_score, g3_score, g4_score, g5_score, g6_score,
			critical_gap, critical_missing_items, uncertain_rate, low_confidence,
			tcm_m_score, tcm_composite_score, prompt_bundle_version, analyzed_at
		FROM recording_segue_scores
		WHERE recording_id = $2
	`, tenantID, sourceRecordingID, targetRecordingID, employeeID)
	if err != nil {
		return fmt.Errorf("clone trial demo segue scores: %w", err)
	}
	return nil
}

func (s *Store) cloneTrialDemoEMRDraftsTx(ctx context.Context, tx pgx.Tx, tenantID, sourceRecordingID, targetRecordingID int64, customerID *int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO recording_emr_drafts (
			recording_id, tenant_id, patient_id, patient_name_extracted, patient_match_source,
			emr_content, confidence, missing_fields, is_confirmed, confirmed_by,
			confirmed_at, generated_at, prompt_version, customer_id
		)
		SELECT
			$3, $1, patient_id, patient_name_extracted, patient_match_source,
			emr_content, confidence, missing_fields, is_confirmed, confirmed_by,
			confirmed_at, generated_at, prompt_version, $4
		FROM recording_emr_drafts
		WHERE recording_id = $2
		ON CONFLICT (recording_id)
		DO UPDATE SET
			tenant_id = EXCLUDED.tenant_id,
			patient_id = EXCLUDED.patient_id,
			patient_name_extracted = EXCLUDED.patient_name_extracted,
			patient_match_source = EXCLUDED.patient_match_source,
			emr_content = EXCLUDED.emr_content,
			confidence = EXCLUDED.confidence,
			missing_fields = EXCLUDED.missing_fields,
			is_confirmed = EXCLUDED.is_confirmed,
			confirmed_by = EXCLUDED.confirmed_by,
			confirmed_at = EXCLUDED.confirmed_at,
			generated_at = EXCLUDED.generated_at,
			prompt_version = EXCLUDED.prompt_version,
			customer_id = EXCLUDED.customer_id
	`, tenantID, sourceRecordingID, targetRecordingID, customerID)
	if err != nil {
		return fmt.Errorf("clone trial demo emr drafts: %w", err)
	}
	return nil
}

func (s *Store) cloneTrialDemoContentSeedsTx(ctx context.Context, tx pgx.Tx, tenantID, sourceRecordingID, targetRecordingID, employeeID int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO recording_content_seeds (
			tenant_id, recording_id, employee_id, seed_type, topic, seed_data,
			content_angle, suggested_platforms, viral_potential, patient_concern,
			concern_cluster_id, status, used_content_id, created_at
		)
		SELECT
			$1, $3, $4, seed_type, topic, seed_data,
			content_angle, suggested_platforms, viral_potential, patient_concern,
			concern_cluster_id, status, used_content_id, NOW()
		FROM recording_content_seeds
		WHERE recording_id = $2
	`, tenantID, sourceRecordingID, targetRecordingID, employeeID)
	if err != nil {
		return fmt.Errorf("clone trial demo content seeds: %w", err)
	}
	return nil
}

func (s *Store) cloneTrialDemoAnalysisRunsTx(ctx context.Context, tx pgx.Tx, tenantID, sourceRecordingID, targetRecordingID, employeeID int64) error {
	rows, err := tx.Query(ctx, `
		SELECT
			id, tenant_id, employee_id, resolved_role_id, resolved_role_code,
			scene_scope, pipeline_code, pipeline_version, status, trigger_source,
			trace_id, route_id, snapshot_version, vendor_recording_id, route_snapshot,
			error_code, error_message, started_at, ended_at, created_at, updated_at
		FROM analysis_runs
		WHERE recording_id = $1
		ORDER BY id ASC
	`, sourceRecordingID)
	if err != nil {
		return fmt.Errorf("query source trial demo analysis runs: %w", err)
	}
	defer rows.Close()

	type sourceRun struct {
		id                int64
		employeeID        *int64
		resolvedRoleID    *int64
		resolvedRoleCode  *string
		sceneScope        *string
		pipelineCode      string
		pipelineVersion   *string
		status            string
		triggerSource     *string
		traceID           *string
		routeID           *int64
		snapshotVersion   *string
		vendorRecordingID *string
		routeSnapshot     []byte
		errorCode         *string
		errorMessage      *string
		startedAt         *time.Time
		endedAt           *time.Time
		createdAt         *time.Time
		updatedAt         *time.Time
	}
	sourceRuns := make([]sourceRun, 0)
	for rows.Next() {
		var item sourceRun
		var sourceTenantID int64
		var routeSnapshotText string
		if err := rows.Scan(
			&item.id,
			&sourceTenantID,
			&item.employeeID,
			&item.resolvedRoleID,
			&item.resolvedRoleCode,
			&item.sceneScope,
			&item.pipelineCode,
			&item.pipelineVersion,
			&item.status,
			&item.triggerSource,
			&item.traceID,
			&item.routeID,
			&item.snapshotVersion,
			&item.vendorRecordingID,
			&routeSnapshotText,
			&item.errorCode,
			&item.errorMessage,
			&item.startedAt,
			&item.endedAt,
			&item.createdAt,
			&item.updatedAt,
		); err != nil {
			return fmt.Errorf("scan source trial demo analysis run: %w", err)
		}
		item.routeSnapshot = []byte(routeSnapshotText)
		sourceRuns = append(sourceRuns, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source trial demo analysis runs: %w", err)
	}
	if len(sourceRuns) == 0 {
		return nil
	}

	runIDMap := make(map[int64]int64, len(sourceRuns))
	for _, run := range sourceRuns {
		var newRunID int64
		err := tx.QueryRow(ctx, `
			INSERT INTO analysis_runs (
				recording_id, tenant_id, employee_id, resolved_role_id, resolved_role_code,
				scene_scope, pipeline_code, pipeline_version, status, trigger_source,
				trace_id, route_id, snapshot_version, vendor_recording_id, route_snapshot,
				error_code, error_message, started_at, ended_at, created_at, updated_at
			)
			VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, $8, $9, $10,
				$11, $12, $13, $14, $15::jsonb,
				$16, $17, $18, $19, COALESCE($20, NOW()), COALESCE($21, NOW())
			)
			RETURNING id
		`, targetRecordingID, tenantID, employeeID, run.resolvedRoleID, nullableStringPtr(run.resolvedRoleCode), nullableStringPtr(run.sceneScope), run.pipelineCode, nullableStringPtr(run.pipelineVersion), run.status, nullableStringPtr(run.triggerSource), nullableStringPtr(run.traceID), run.routeID, nullableStringPtr(run.snapshotVersion), nullableStringPtr(run.vendorRecordingID), string(run.routeSnapshot), nullableStringPtr(run.errorCode), nullableStringPtr(run.errorMessage), run.startedAt, run.endedAt, run.createdAt, run.updatedAt).Scan(&newRunID)
		if err != nil {
			return fmt.Errorf("insert cloned analysis run: %w", err)
		}
		runIDMap[run.id] = newRunID
	}

	stepRows, err := tx.Query(ctx, `
		SELECT
			id, run_id, step_code, step_name, step_type, prompt_code, prompt_version,
			status, attempt, input_digest, output_digest, tokens_used, cost,
			execution_time_ms, started_at, ended_at, error_code, error_message,
			created_at, updated_at
		FROM analysis_step_runs
		WHERE recording_id = $1
		ORDER BY id ASC
	`, sourceRecordingID)
	if err != nil {
		return fmt.Errorf("query source trial demo analysis step runs: %w", err)
	}
	defer stepRows.Close()

	type sourceAnalysisStepRun struct {
		runID         int64
		stepCode      *string
		stepName      *string
		stepType      *string
		promptCode    *string
		promptVersion *string
		status        string
		attempt       *int
		inputDigest   *string
		outputDigest  *string
		tokensUsed    *int
		cost          *float64
		executionMS   *int
		startedAt     *time.Time
		endedAt       *time.Time
		errorCode     *string
		errorMessage  *string
		createdAt     *time.Time
		updatedAt     *time.Time
	}
	stepRuns := make([]sourceAnalysisStepRun, 0)
	for stepRows.Next() {
		var (
			sourceID int64
			item     sourceAnalysisStepRun
		)
		if err := stepRows.Scan(&sourceID, &item.runID, &item.stepCode, &item.stepName, &item.stepType, &item.promptCode, &item.promptVersion, &item.status, &item.attempt, &item.inputDigest, &item.outputDigest, &item.tokensUsed, &item.cost, &item.executionMS, &item.startedAt, &item.endedAt, &item.errorCode, &item.errorMessage, &item.createdAt, &item.updatedAt); err != nil {
			return fmt.Errorf("scan source trial demo analysis step run: %w", err)
		}
		stepRuns = append(stepRuns, item)
	}
	if err := stepRows.Err(); err != nil {
		return fmt.Errorf("iterate source trial demo analysis step runs: %w", err)
	}
	for _, item := range stepRuns {
		newRunID, ok := runIDMap[item.runID]
		if !ok {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO analysis_step_runs (
				run_id, recording_id, step_code, step_name, step_type, prompt_code, prompt_version,
				status, attempt, input_digest, output_digest, tokens_used, cost, execution_time_ms,
				started_at, ended_at, error_code, error_message, created_at, updated_at
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7,
				$8, $9, $10, $11, $12, $13, $14,
				$15, $16, $17, $18, COALESCE($19, NOW()), COALESCE($20, NOW())
			)
		`, newRunID, targetRecordingID, nullableStringPtr(item.stepCode), nullableStringPtr(item.stepName), nullableStringPtr(item.stepType), nullableStringPtr(item.promptCode), nullableStringPtr(item.promptVersion), item.status, item.attempt, nullableStringPtr(item.inputDigest), nullableStringPtr(item.outputDigest), item.tokensUsed, item.cost, item.executionMS, item.startedAt, item.endedAt, nullableStringPtr(item.errorCode), nullableStringPtr(item.errorMessage), item.createdAt, item.updatedAt); err != nil {
			return fmt.Errorf("insert cloned analysis step run: %w", err)
		}
	}
	return nil
}

func (s *Store) cloneTrialDemoWorkerRunsTx(ctx context.Context, tx pgx.Tx, tenantID, sourceRecordingID, targetRecordingID, employeeID int64) error {
	rows, err := tx.Query(ctx, `
		SELECT
			id, run_id, trace_id, role_id, role_code, pipeline_code, pipeline_version,
			vendor_recording_id, source_type, trigger_source, status, error_code,
			error_message, started_at, ended_at, duration_ms, created_at, updated_at
		FROM worker_analysis_runs
		WHERE recording_id = $1
		ORDER BY id ASC
	`, sourceRecordingID)
	if err != nil {
		return fmt.Errorf("query source trial demo worker analysis runs: %w", err)
	}
	defer rows.Close()

	type workerRun struct {
		runID             string
		traceID           *string
		roleID            *int64
		roleCode          *string
		pipelineCode      *string
		pipelineVersion   *string
		vendorRecordingID *string
		sourceType        *string
		triggerSource     *string
		status            string
		errorCode         *string
		errorMessage      *string
		startedAt         *time.Time
		endedAt           *time.Time
		durationMS        *int
		createdAt         *time.Time
		updatedAt         *time.Time
	}
	workerRuns := make([]workerRun, 0)
	for rows.Next() {
		var item workerRun
		var sourceID int64
		if err := rows.Scan(&sourceID, &item.runID, &item.traceID, &item.roleID, &item.roleCode, &item.pipelineCode, &item.pipelineVersion, &item.vendorRecordingID, &item.sourceType, &item.triggerSource, &item.status, &item.errorCode, &item.errorMessage, &item.startedAt, &item.endedAt, &item.durationMS, &item.createdAt, &item.updatedAt); err != nil {
			return fmt.Errorf("scan source trial demo worker analysis run: %w", err)
		}
		workerRuns = append(workerRuns, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source trial demo worker analysis runs: %w", err)
	}
	if len(workerRuns) == 0 {
		return nil
	}

	runIDMap := make(map[string]string, len(workerRuns))
	for _, run := range workerRuns {
		clonedRunID := run.runID + "_trial_" + fmt.Sprintf("%d", targetRecordingID)
		runIDMap[run.runID] = clonedRunID
		if _, err := tx.Exec(ctx, `
			INSERT INTO worker_analysis_runs (
				run_id, trace_id, recording_id, tenant_id, employee_id, role_id, role_code,
				pipeline_code, pipeline_version, vendor_recording_id, source_type, trigger_source,
				status, error_code, error_message, started_at, ended_at, duration_ms, created_at, updated_at
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7,
				$8, $9, $10, $11, $12,
				$13, $14, $15, $16, $17, $18, COALESCE($19, NOW()), COALESCE($20, NOW())
			)
		`, clonedRunID, nullableStringPtr(run.traceID), targetRecordingID, tenantID, employeeID, run.roleID, nullableStringPtr(run.roleCode), nullableStringPtr(run.pipelineCode), nullableStringPtr(run.pipelineVersion), nullableStringPtr(run.vendorRecordingID), nullableStringPtr(run.sourceType), nullableStringPtr(run.triggerSource), run.status, nullableStringPtr(run.errorCode), nullableStringPtr(run.errorMessage), run.startedAt, run.endedAt, run.durationMS, run.createdAt, run.updatedAt); err != nil {
			return fmt.Errorf("insert cloned worker analysis run: %w", err)
		}
	}

	stepRows, err := tx.Query(ctx, `
		SELECT
			step_run_id, run_id, pipeline_code, step_code, step_name, step_type,
			sequence_no, input_summary, output_summary, status, error_code, error_message,
			started_at, ended_at, duration_ms, created_at, updated_at
		FROM worker_analysis_run_steps
		WHERE recording_id = $1
		ORDER BY id ASC
	`, sourceRecordingID)
	if err != nil {
		return fmt.Errorf("query source trial demo worker analysis step runs: %w", err)
	}
	defer stepRows.Close()

	type sourceWorkerStepRun struct {
		stepRunID     string
		runID         string
		pipelineCode  *string
		stepCode      *string
		stepName      *string
		stepType      *string
		sequenceNo    *int
		inputSummary  []byte
		outputSummary []byte
		status        string
		errorCode     *string
		errorMessage  *string
		startedAt     *time.Time
		endedAt       *time.Time
		durationMS    *int
		createdAt     *time.Time
		updatedAt     *time.Time
	}
	stepRuns := make([]sourceWorkerStepRun, 0)
	for stepRows.Next() {
		var item sourceWorkerStepRun
		var inputSummaryText, outputSummaryText string
		if err := stepRows.Scan(&item.stepRunID, &item.runID, &item.pipelineCode, &item.stepCode, &item.stepName, &item.stepType, &item.sequenceNo, &inputSummaryText, &outputSummaryText, &item.status, &item.errorCode, &item.errorMessage, &item.startedAt, &item.endedAt, &item.durationMS, &item.createdAt, &item.updatedAt); err != nil {
			return fmt.Errorf("scan source trial demo worker analysis step run: %w", err)
		}
		item.inputSummary = []byte(inputSummaryText)
		item.outputSummary = []byte(outputSummaryText)
		stepRuns = append(stepRuns, item)
	}
	if err := stepRows.Err(); err != nil {
		return fmt.Errorf("iterate source trial demo worker analysis step runs: %w", err)
	}
	for _, item := range stepRuns {
		clonedRunID, ok := runIDMap[item.runID]
		if !ok {
			continue
		}
		clonedStepRunID := item.stepRunID + "_trial_" + fmt.Sprintf("%d", targetRecordingID)
		if _, err := tx.Exec(ctx, `
			INSERT INTO worker_analysis_run_steps (
				step_run_id, run_id, recording_id, tenant_id, pipeline_code, step_code, step_name,
				step_type, sequence_no, input_summary, output_summary, status, error_code,
				error_message, started_at, ended_at, duration_ms, created_at, updated_at
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7,
				$8, $9, $10::jsonb, $11::jsonb, $12, $13,
				$14, $15, $16, $17, COALESCE($18, NOW()), COALESCE($19, NOW())
			)
		`, clonedStepRunID, clonedRunID, targetRecordingID, tenantID, nullableStringPtr(item.pipelineCode), nullableStringPtr(item.stepCode), nullableStringPtr(item.stepName), nullableStringPtr(item.stepType), item.sequenceNo, string(item.inputSummary), string(item.outputSummary), item.status, nullableStringPtr(item.errorCode), nullableStringPtr(item.errorMessage), item.startedAt, item.endedAt, item.durationMS, item.createdAt, item.updatedAt); err != nil {
			return fmt.Errorf("insert cloned worker analysis step run: %w", err)
		}
	}
	return nil
}

func (s *Store) cloneTrialDemoTasksTx(ctx context.Context, tx pgx.Tx, tenantID, sourceRecordingID, targetRecordingID int64, customerID *int64, defaultAssigneeID int64) error {
	if customerID == nil || *customerID <= 0 {
		return nil
	}
	var customerName, customerPhone string
	_ = tx.QueryRow(ctx, `SELECT COALESCE(name, ''), COALESCE(phone, '') FROM customers WHERE id = $1`, *customerID).Scan(&customerName, &customerPhone)
	type sourceTask struct {
		title         string
		description   *string
		priority      *string
		script        *string
		contactReason *string
		sourceType    *string
		sourceDetail  *string
		dueAt         *time.Time
		status        string
	}
	rows, err := tx.Query(ctx, `
		SELECT
			title,
			description,
			priority,
			script,
			contact_reason,
			source_type,
			source_detail,
			due_at,
			COALESCE(status, 'pending') AS status
		FROM recording_tasks
		WHERE recording_id = $1
		ORDER BY id ASC
	`, sourceRecordingID)
	if err != nil {
		return fmt.Errorf("query source trial demo tasks: %w", err)
	}
	tasks := make([]sourceTask, 0)
	for rows.Next() {
		var task sourceTask
		if err := rows.Scan(&task.title, &task.description, &task.priority, &task.script, &task.contactReason, &task.sourceType, &task.sourceDetail, &task.dueAt, &task.status); err != nil {
			return fmt.Errorf("scan source trial demo task: %w", err)
		}
		tasks = append(tasks, task)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source trial demo tasks: %w", err)
	}
	for _, task := range tasks {
		_, err := tx.Exec(ctx, `
			INSERT INTO recording_tasks (
				tenant_id, recording_id, customer_id, customer_name, customer_phone,
				title, description, priority, script, contact_reason,
				assigned_to, status, source_type, source_detail, due_at, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, NULLIF($9, ''), NULLIF($10, ''), $11, $12, NULLIF($13, ''), NULLIF($14, ''), $15, NOW(), NOW())
		`, tenantID, targetRecordingID, customerID, customerName, customerPhone, task.title, nullableStringPtr(task.description), defaultStringPtr(task.priority, "medium"), nullableStringPtr(task.script), nullableStringPtr(task.contactReason), defaultAssigneeID, defaultString(task.status, "pending"), nullableStringPtr(task.sourceType), nullableStringPtr(task.sourceDetail), task.dueAt)
		if err != nil {
			return fmt.Errorf("clone trial demo task: %w", err)
		}
	}
	return nil
}

func nullableStringPtr(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func defaultStringPtr(v *string, fallback string) string {
	if v == nil {
		return fallback
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func defaultString(v string, fallback string) string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
