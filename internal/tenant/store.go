package tenant

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Store struct {
	pool *pgxpool.Pool

	// columnExistCache 缓存 information_schema 探测结果。列是否存在在运行期不会变，
	// 无需每次列表/重算都查一遍 information_schema。key = "table.column"。
	columnExistCache sync.Map
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

func (s *Store) tableHasColumn(ctx context.Context, tableName, columnName string) (bool, error) {
	cacheKey := tableName + "." + columnName
	if cached, ok := s.columnExistCache.Load(cacheKey); ok {
		return cached.(bool), nil
	}
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = $1
			  AND column_name = $2
		)
	`, tableName, columnName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to inspect %s.%s: %w", tableName, columnName, err)
	}
	s.columnExistCache.Store(cacheKey, exists)
	return exists, nil
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
		       a.sales_owner_admin_id,
		       COALESCE(a.sales_owner_name_snapshot, '') AS trial_sales_owner_name,
		       COALESCE(a.source, '') AS trial_source,
		       COALESCE(a.notes, '') AS trial_notes,
		       t.created_at, t.updated_at, t.deleted_at
		FROM tenants t
		LEFT JOIN trial_customer_assignments a ON a.tenant_id = t.id
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
		&t.TrialSalesOwnerAdminID,
		&t.TrialSalesOwnerName,
		&t.TrialSource,
		&t.TrialNotes,
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

func chooseStringPtr(primary *string, fallback string) *string {
	if primary != nil {
		return primary
	}
	return &fallback
}

func chooseInt64Ptr(primary *int64, fallback *int64) *int64 {
	if primary != nil {
		return primary
	}
	return fallback
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
		          NULL::bigint AS trial_sales_owner_admin_id, '' AS trial_sales_owner_name, '' AS trial_source, '' AS trial_notes,
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
		&t.TrialSalesOwnerAdminID,
		&t.TrialSalesOwnerName,
		&t.TrialSource,
		&t.TrialNotes,
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
		SELECT tenant_id, template_code, role_code, asset_id, recording_id, employee_id, customer_id, title, viewed_at, viewed_by_employee_id
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
		if err := rows.Scan(&item.TenantID, &item.TemplateCode, &item.RoleCode, &item.AssetID, &item.RecordingID, &item.EmployeeID, &item.CustomerID, &item.Title, &item.ViewedAt, &item.ViewedByEmployeeID); err != nil {
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

// trialMetricsFreshness 指标缓存的有效期。列表页只重算超过该时长未刷新的租户，
// 兼顾列表速度与数据新鲜度：常态下秒开，数据最多滞后该时长（详情页仍每次实时重算）。
const trialMetricsFreshness = 5 * time.Minute

// filterStaleTrialTenants 用一条查询批量筛出指标已过期（或从未计算）的租户，
// 避免对本页每个租户逐个判断新鲜度导致的 N+1。
func (s *Store) filterStaleTrialTenants(ctx context.Context, items []*TrialCustomerListItem) ([]int64, error) {
	tenantIDs := make([]int64, 0, len(items))
	for _, item := range items {
		if item != nil && item.TenantID > 0 {
			tenantIDs = append(tenantIDs, item.TenantID)
		}
	}
	if len(tenantIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT t.id
		FROM unnest($1::bigint[]) AS t(id)
		LEFT JOIN trial_customer_metrics m ON m.tenant_id = t.id
		WHERE m.tenant_id IS NULL
		   OR m.updated_at IS NULL
		   OR m.updated_at < NOW() - $2::interval
	`, tenantIDs, trialMetricsFreshness.String())
	if err != nil {
		return nil, fmt.Errorf("filter stale trial tenants: %w", err)
	}
	defer rows.Close()
	stale := make([]int64, 0, len(tenantIDs))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan stale trial tenant id: %w", err)
		}
		stale = append(stale, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stale trial tenants: %w", err)
	}
	return stale, nil
}

func (s *Store) RecomputeTrialCustomerMetrics(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id
		FROM tenants
		WHERE deleted_at IS NULL
		  AND lower(COALESCE(account_mode, 'formal')) = 'trial'
	`)
	if err != nil {
		return fmt.Errorf("query trial tenants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tenantID int64
		if err := rows.Scan(&tenantID); err != nil {
			return fmt.Errorf("scan trial tenant id: %w", err)
		}
		if err := s.RecomputeTrialCustomerMetricsForTenant(ctx, tenantID); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate trial tenants: %w", err)
	}
	return nil
}

func (s *Store) EnsureTrialCustomerMetricsSeed(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO trial_customer_metrics (tenant_id, trial_started_at, trial_expires_at, current_stage, priority_level, blocking_reason, next_action_hint, updated_at)
		SELECT
			t.id,
			COALESCE(t.valid_from, t.created_at),
			t.valid_to,
			CASE
				WHEN t.valid_to IS NOT NULL AND t.valid_to < NOW() THEN 'expired_unconverted'
				ELSE 'not_started'
			END,
			'normal',
			'开通后未登录',
			'建议销售发起首次唤醒沟通，推动客户尽快登录试用后台',
			NOW()
		FROM tenants t
		WHERE t.deleted_at IS NULL
		  AND lower(COALESCE(t.account_mode, 'formal')) = 'trial'
		  AND NOT EXISTS (
			SELECT 1
			FROM trial_customer_metrics m
			WHERE m.tenant_id = t.id
		  )
	`)
	if err != nil {
		return fmt.Errorf("seed trial customer metrics: %w", err)
	}
	return nil
}

func (s *Store) RecomputeTrialCustomerMetricsForTenant(ctx context.Context, tenantID int64) error {
	var trialStartedAt *time.Time
	var trialExpiresAt *time.Time
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(valid_from, created_at) AS started_at, valid_to
		FROM tenants
		WHERE id = $1 AND deleted_at IS NULL
	`, tenantID).Scan(&trialStartedAt, &trialExpiresAt); err != nil {
		return fmt.Errorf("query trial tenant dates: %w", err)
	}

	var firstLoginAt, lastLoginAt *time.Time
	var loginCount int
	if err := s.pool.QueryRow(ctx, `
		SELECT MIN(login_at), MAX(login_at), COUNT(*)
		FROM employee_login_events
		WHERE tenant_id = $1
	`, tenantID).Scan(&firstLoginAt, &lastLoginAt, &loginCount); err != nil {
		return fmt.Errorf("query employee login metrics: %w", err)
	}

	demoItems, err := s.ListTenantTrialDemoRecordings(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("list tenant trial demo recordings: %w", err)
	}
	demoRecordingIDs := make([]int64, 0, len(demoItems))
	roleByRecordingID := make(map[int64]string, len(demoItems))
	for _, item := range demoItems {
		demoRecordingIDs = append(demoRecordingIDs, item.RecordingID)
		roleByRecordingID[item.RecordingID] = strings.ToLower(strings.TrimSpace(item.RoleCode))
	}

	var doctorDemoViewedAt, consultantDemoViewedAt *time.Time
	for _, item := range demoItems {
		switch roleByRecordingID[item.RecordingID] {
		case "doctor":
			if doctorDemoViewedAt == nil || (item.ViewedAt != nil && item.ViewedAt.Before(*doctorDemoViewedAt)) {
				doctorDemoViewedAt = item.ViewedAt
			}
		case "consultant":
			if consultantDemoViewedAt == nil || (item.ViewedAt != nil && item.ViewedAt.Before(*consultantDemoViewedAt)) {
				consultantDemoViewedAt = item.ViewedAt
			}
		}
	}

	var firstUploadAt, lastUploadAt *time.Time
	var uploadCount, analysisCount, doctorUploadCount, consultantUploadCount, generatedCustomerCount, generatedTaskCount int
	var generatedContentCount, wechatContentCount, xiaohongshuContentCount, videoScriptContentCount int
	recordingsHasDeletedAt, err := s.tableHasColumn(ctx, "recordings", "deleted_at")
	if err != nil {
		return err
	}
	recordingsDeletedCondition := "TRUE"
	if recordingsHasDeletedAt {
		recordingsDeletedCondition = "r.deleted_at IS NULL"
	}

	// 用 LEFT JOIN 预聚合替代逐行相关子查询：is_demo 由 demo 表命中判断，
	// task_count 由 recording_tasks 按 recording_id 预聚合，避免每行录音各跑一次子查询。
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT
			r.id,
			COALESCE(r.created_at, NOW()) AS created_at,
			lower(COALESCE(NULLIF(r.business_scope, ''), 'unknown')) AS business_scope,
			CASE WHEN d.recording_id IS NOT NULL THEN true ELSE false END AS is_demo,
			CASE
				WHEN COALESCE(NULLIF(r.analysis_status, ''), '') IN ('completed', 'done', 'success') THEN true
				ELSE false
			END AS analysis_completed,
			CASE WHEN r.customer_id IS NOT NULL THEN 1 ELSE 0 END AS customer_generated,
			COALESCE(rt.task_count, 0) AS task_count
		FROM recordings r
		LEFT JOIN (
			SELECT DISTINCT recording_id
			FROM tenant_trial_demo_recordings
			WHERE tenant_id = $1
		) d ON d.recording_id = r.id
		LEFT JOIN (
			SELECT recording_id, COUNT(*) AS task_count
			FROM recording_tasks
			WHERE tenant_id = $1
			GROUP BY recording_id
		) rt ON rt.recording_id = r.id
		WHERE r.tenant_id = $1
		  AND %s
	`, recordingsDeletedCondition), tenantID)
	if err != nil {
		return fmt.Errorf("query trial recordings metrics: %w", err)
	}
	for rows.Next() {
		var recordingID int64
		var createdAt time.Time
		var businessScope string
		var isDemo bool
		var analysisCompleted bool
		var customerGenerated int
		var taskCount int
		if err := rows.Scan(&recordingID, &createdAt, &businessScope, &isDemo, &analysisCompleted, &customerGenerated, &taskCount); err != nil {
			rows.Close()
			return fmt.Errorf("scan trial recording metrics: %w", err)
		}
		if !isDemo {
			uploadCount++
			if firstUploadAt == nil || createdAt.Before(*firstUploadAt) {
				t := createdAt
				firstUploadAt = &t
			}
			if lastUploadAt == nil || createdAt.After(*lastUploadAt) {
				t := createdAt
				lastUploadAt = &t
			}
			switch businessScope {
			case "doctor":
				doctorUploadCount++
			case "consultant":
				consultantUploadCount++
			}
			if analysisCompleted {
				analysisCount++
			}
			generatedCustomerCount += customerGenerated
			generatedTaskCount += taskCount
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate trial recordings metrics: %w", err)
	}
	rows.Close()

	contentTypeColumnExists, err := s.tableHasColumn(ctx, "content_items", "content_type")
	if err != nil {
		return err
	}
	platformColumnExists, err := s.tableHasColumn(ctx, "content_items", "platform")
	if err != nil {
		return err
	}
	if contentTypeColumnExists || platformColumnExists {
		contentRows, err := s.pool.Query(ctx, `
			SELECT
				COALESCE(NULLIF(lower(trim(content_type)), ''), lower(COALESCE(NULLIF(extra_data->>'content_type', ''), ''))) AS content_type,
				COALESCE(NULLIF(lower(trim(platform)), ''), lower(COALESCE(NULLIF(extra_data->>'platform', ''), NULLIF(extra_data->>'target_platform', ''), ''))) AS platform
			FROM content_items
			WHERE tenant_id = $1
			  AND deleted_at IS NULL
		`, tenantID)
		if err != nil {
			return fmt.Errorf("query trial content metrics: %w", err)
		}
		for contentRows.Next() {
			var contentType string
			var platform string
			if err := contentRows.Scan(&contentType, &platform); err != nil {
				contentRows.Close()
				return fmt.Errorf("scan trial content metrics: %w", err)
			}
			generatedContentCount++
			switch normalizeTrialContentCategory(contentType, platform) {
			case "wechat":
				wechatContentCount++
			case "xiaohongshu":
				xiaohongshuContentCount++
			case "video_script":
				videoScriptContentCount++
			}
		}
		if err := contentRows.Err(); err != nil {
			contentRows.Close()
			return fmt.Errorf("iterate trial content metrics: %w", err)
		}
		contentRows.Close()
	}

	lastActivityAt := latestTime(lastLoginAt, lastUploadAt)
	currentStage := deriveTrialCustomerStage(loginCount, doctorDemoViewedAt, consultantDemoViewedAt, uploadCount, analysisCount, trialExpiresAt)
	priorityLevel := deriveTrialCustomerPriority(trialExpiresAt, uploadCount, analysisCount, lastLoginAt)
	blockingReason := deriveTrialCustomerBlockingReason(currentStage, trialExpiresAt)
	nextActionHint := deriveTrialCustomerNextAction(currentStage, priorityLevel)

	_, err = s.pool.Exec(ctx, `
		INSERT INTO trial_customer_metrics (
			tenant_id, trial_started_at, trial_expires_at, first_login_at, last_login_at, login_count,
			doctor_demo_viewed_at, consultant_demo_viewed_at, first_upload_at, last_upload_at,
			upload_count, analysis_count, doctor_upload_count, consultant_upload_count,
			generated_customer_count, generated_task_count, generated_content_count, wechat_content_count, xiaohongshu_content_count, video_script_content_count, last_activity_at, current_stage,
			priority_level, blocking_reason, next_action_hint, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13, $14,
			$15, $16, $17, $18, $19, $20,
			$21, $22, $23, $24, $25, NOW()
		)
		ON CONFLICT (tenant_id) DO UPDATE
		SET trial_started_at = EXCLUDED.trial_started_at,
		    trial_expires_at = EXCLUDED.trial_expires_at,
		    first_login_at = EXCLUDED.first_login_at,
		    last_login_at = EXCLUDED.last_login_at,
		    login_count = EXCLUDED.login_count,
		    doctor_demo_viewed_at = EXCLUDED.doctor_demo_viewed_at,
		    consultant_demo_viewed_at = EXCLUDED.consultant_demo_viewed_at,
		    first_upload_at = EXCLUDED.first_upload_at,
		    last_upload_at = EXCLUDED.last_upload_at,
		    upload_count = EXCLUDED.upload_count,
		    analysis_count = EXCLUDED.analysis_count,
		    doctor_upload_count = EXCLUDED.doctor_upload_count,
		    consultant_upload_count = EXCLUDED.consultant_upload_count,
		    generated_customer_count = EXCLUDED.generated_customer_count,
		    generated_task_count = EXCLUDED.generated_task_count,
		    generated_content_count = EXCLUDED.generated_content_count,
		    wechat_content_count = EXCLUDED.wechat_content_count,
		    xiaohongshu_content_count = EXCLUDED.xiaohongshu_content_count,
		    video_script_content_count = EXCLUDED.video_script_content_count,
		    last_activity_at = EXCLUDED.last_activity_at,
		    current_stage = EXCLUDED.current_stage,
		    priority_level = EXCLUDED.priority_level,
		    blocking_reason = EXCLUDED.blocking_reason,
		    next_action_hint = EXCLUDED.next_action_hint,
		    updated_at = NOW()
	`, tenantID, trialStartedAt, trialExpiresAt, firstLoginAt, lastLoginAt, loginCount, doctorDemoViewedAt, consultantDemoViewedAt, firstUploadAt, lastUploadAt, uploadCount, analysisCount, doctorUploadCount, consultantUploadCount, generatedCustomerCount, generatedTaskCount, generatedContentCount, wechatContentCount, xiaohongshuContentCount, videoScriptContentCount, lastActivityAt, currentStage, priorityLevel, blockingReason, nextActionHint)
	if err != nil {
		return fmt.Errorf("upsert trial customer metrics: %w", err)
	}
	return nil
}

func (s *Store) ListTrialCustomers(ctx context.Context, req TrialCustomerListRequest) (*TrialCustomerListResponse, error) {
	conditions := []string{
		"t.deleted_at IS NULL",
		"lower(COALESCE(t.account_mode, 'formal')) = 'trial'",
	}
	args := make([]interface{}, 0, 8)
	argIndex := 1
	if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
		conditions = append(conditions, fmt.Sprintf("(t.name ILIKE $%d OR COALESCE(t.contact_name, '') ILIKE $%d OR COALESCE(t.contact_phone, '') ILIKE $%d)", argIndex, argIndex, argIndex))
		args = append(args, "%"+keyword+"%")
		argIndex++
	}
	if req.OwnerAdminID != nil {
		conditions = append(conditions, fmt.Sprintf("a.sales_owner_admin_id = $%d", argIndex))
		args = append(args, *req.OwnerAdminID)
		argIndex++
	}
	if req.VisibleOrgID != nil && *req.VisibleOrgID > 0 {
		conditions = append(conditions, fmt.Sprintf("o.owner_org_id = $%d", argIndex))
		args = append(args, *req.VisibleOrgID)
		argIndex++
	}
	if source := strings.TrimSpace(req.Source); source != "" {
		conditions = append(conditions, fmt.Sprintf("lower(COALESCE(a.source, '')) = lower($%d)", argIndex))
		args = append(args, source)
		argIndex++
	}
	if stage := strings.TrimSpace(req.Stage); stage != "" {
		conditions = append(conditions, fmt.Sprintf("COALESCE(m.current_stage, 'not_started') = $%d", argIndex))
		args = append(args, stage)
		argIndex++
	}
	if activationStatus := strings.TrimSpace(req.ActivationStatus); activationStatus != "" {
		switch activationStatus {
		case "not_activated":
			conditions = append(conditions, "COALESCE(m.login_count, 0) = 0 AND COALESCE(m.upload_count, 0) = 0")
		case "activated":
			conditions = append(conditions, "(COALESCE(m.login_count, 0) > 0 OR COALESCE(m.upload_count, 0) > 0)")
		}
	}
	if req.HasRealRecording != nil {
		if *req.HasRealRecording {
			conditions = append(conditions, "COALESCE(m.upload_count, 0) > 0")
		} else {
			conditions = append(conditions, "COALESCE(m.upload_count, 0) = 0")
		}
	}
	whereClause := strings.Join(conditions, " AND ")

	offset := (req.Page - 1) * req.PageSize
	listArgs := append(append([]interface{}{}, args...), req.PageSize, offset)
	query := fmt.Sprintf(`
		SELECT
			t.id,
			t.name,
			COALESCE(t.code, '') AS tenant_code,
			COALESCE(t.contact_name, '') AS contact_name,
			COALESCE(t.contact_phone, '') AS contact_phone,
			COALESCE(NULLIF(trim(t.account_mode), ''), 'formal') AS account_mode,
			COALESCE(m.current_stage, 'not_started') AS current_stage,
			CASE WHEN (COALESCE(m.login_count, 0) > 0 OR COALESCE(m.upload_count, 0) > 0) THEN 'activated' ELSE 'not_activated' END AS activation_status,
			a.sales_owner_admin_id,
			COALESCE(a.sales_owner_name_snapshot, '') AS sales_owner_name,
			o.owner_org_id,
			COALESCE(org.name, '') AS owner_org_name,
			COALESCE(a.source, '') AS source,
			COALESCE(a.notes, '') AS notes,
			m.trial_started_at,
			m.trial_expires_at,
			m.first_login_at,
			m.last_login_at,
			m.first_upload_at,
			m.last_upload_at,
			COALESCE(m.login_count, 0),
			COALESCE(m.analysis_count, 0),
			COALESCE(m.upload_count, 0),
			CASE WHEN m.doctor_demo_viewed_at IS NULL THEN false ELSE true END AS doctor_demo_viewed,
			CASE WHEN m.consultant_demo_viewed_at IS NULL THEN false ELSE true END AS consultant_demo_viewed,
			COALESCE(m.upload_count, 0) AS real_recording_upload_count,
			COALESCE(m.generated_customer_count, 0),
			COALESCE(m.generated_task_count, 0),
			COALESCE(m.priority_level, 'normal') AS priority_level,
			COALESCE(m.blocking_reason, '') AS blocking_reason,
			COALESCE(m.next_action_hint, '') AS next_action_hint,
			COALESCE((
				SELECT tf.summary
				FROM trial_customer_follow_ups tf
				WHERE tf.tenant_id = t.id
				ORDER BY tf.created_at DESC, tf.id DESC
				LIMIT 1
			), '') AS latest_follow_up_summary,
			(
				SELECT tf.created_at
				FROM trial_customer_follow_ups tf
				WHERE tf.tenant_id = t.id
				ORDER BY tf.created_at DESC, tf.id DESC
				LIMIT 1
			) AS latest_follow_up_at,
			(
				SELECT tf.next_follow_up_at
				FROM trial_customer_follow_ups tf
				WHERE tf.tenant_id = t.id
				  AND tf.next_follow_up_at IS NOT NULL
				ORDER BY tf.created_at DESC, tf.id DESC
				LIMIT 1
			) AS next_follow_up_at
		FROM tenants t
		LEFT JOIN trial_customer_ownerships o ON o.tenant_id = t.id
		LEFT JOIN ops_organizations org ON org.id = o.owner_org_id
		LEFT JOIN trial_customer_assignments a ON a.tenant_id = t.id
		LEFT JOIN trial_customer_metrics m ON m.tenant_id = t.id
		WHERE %s
		ORDER BY COALESCE(m.last_activity_at, m.trial_started_at, t.created_at) DESC, t.id DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	loadItems := func(queryArgs []interface{}) ([]*TrialCustomerListItem, error) {
		rows, err := s.pool.Query(ctx, query, queryArgs...)
		if err != nil {
			return nil, fmt.Errorf("query trial customers: %w", err)
		}
		defer rows.Close()

		items := make([]*TrialCustomerListItem, 0)
		for rows.Next() {
			item := &TrialCustomerListItem{}
			if err := rows.Scan(
				&item.TenantID,
				&item.TenantName,
				&item.TenantCode,
				&item.ContactName,
				&item.ContactPhone,
				&item.AccountMode,
				&item.CurrentStage,
				&item.ActivationStatus,
				&item.SalesOwnerAdminID,
				&item.SalesOwnerName,
				&item.OwnerOrgID,
				&item.OwnerOrgName,
				&item.Source,
				&item.Notes,
				&item.TrialStartedAt,
				&item.TrialExpiresAt,
				&item.FirstLoginAt,
				&item.LastLoginAt,
				&item.FirstUploadAt,
				&item.LastUploadAt,
				&item.LoginCount,
				&item.AnalysisCount,
				&item.UploadCount,
				&item.DoctorDemoViewed,
				&item.ConsultantDemoViewed,
				&item.RealRecordingUploadCount,
				&item.GeneratedCustomerCount,
				&item.GeneratedTaskCount,
				&item.PriorityLevel,
				&item.BlockingReason,
				&item.NextActionHint,
				&item.LatestFollowUpSummary,
				&item.LatestFollowUpAt,
				&item.NextFollowUpAt,
			); err != nil {
				return nil, fmt.Errorf("scan trial customer list item: %w", err)
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate trial customers: %w", err)
		}
		return items, nil
	}

	items, err := loadItems(listArgs)
	if err != nil {
		return nil, err
	}

	// 只对指标已过期的租户重算，避免每次打开列表都对全部租户做昂贵的同步重算。
	// 指标由重算写入 trial_customer_metrics.updated_at；超过 trialMetricsFreshness
	// 或从未算过的租户才需要刷新。常态下列表几乎零重算开销。
	staleTenantIDs, err := s.filterStaleTrialTenants(ctx, items)
	if err != nil {
		return nil, err
	}
	if len(staleTenantIDs) > 0 {
		for _, tenantID := range staleTenantIDs {
			if err := s.RecomputeTrialCustomerMetricsForTenant(ctx, tenantID); err != nil {
				return nil, err
			}
		}
		// 有指标被刷新，重新加载以反映最新值
		items, err = loadItems(listArgs)
		if err != nil {
			return nil, err
		}
	}

	var total int64
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM tenants t
		LEFT JOIN trial_customer_ownerships o ON o.tenant_id = t.id
		LEFT JOIN trial_customer_assignments a ON a.tenant_id = t.id
		LEFT JOIN trial_customer_metrics m ON m.tenant_id = t.id
		WHERE %s
	`, whereClause), args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count trial customers: %w", err)
	}

	return &TrialCustomerListResponse{
		Items:    items,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}

func (s *Store) GetTrialCustomerDetail(ctx context.Context, tenantID int64) (*TrialCustomerDetail, error) {
	selected := &TrialCustomerListItem{}
	if err := s.pool.QueryRow(ctx, `
		SELECT
			t.id,
			t.name,
			COALESCE(t.code, '') AS tenant_code,
			COALESCE(t.contact_name, '') AS contact_name,
			COALESCE(t.contact_phone, '') AS contact_phone,
			COALESCE(NULLIF(trim(t.account_mode), ''), 'formal') AS account_mode,
			COALESCE(m.current_stage, 'not_started') AS current_stage,
			CASE WHEN (COALESCE(m.login_count, 0) > 0 OR COALESCE(m.upload_count, 0) > 0) THEN 'activated' ELSE 'not_activated' END AS activation_status,
			a.sales_owner_admin_id,
			COALESCE(a.sales_owner_name_snapshot, '') AS sales_owner_name,
			o.owner_org_id,
			COALESCE(org.name, '') AS owner_org_name,
			COALESCE(a.source, '') AS source,
			COALESCE(a.notes, '') AS notes,
			m.trial_started_at,
			m.trial_expires_at,
			m.first_login_at,
			m.last_login_at,
			m.first_upload_at,
			m.last_upload_at,
			COALESCE(m.login_count, 0),
			COALESCE(m.analysis_count, 0),
			COALESCE(m.upload_count, 0),
			CASE WHEN m.doctor_demo_viewed_at IS NULL THEN false ELSE true END AS doctor_demo_viewed,
			CASE WHEN m.consultant_demo_viewed_at IS NULL THEN false ELSE true END AS consultant_demo_viewed,
			COALESCE(m.upload_count, 0) AS real_recording_upload_count,
			COALESCE(m.generated_customer_count, 0),
			COALESCE(m.generated_task_count, 0),
			COALESCE(m.priority_level, 'normal') AS priority_level,
			COALESCE(m.blocking_reason, '') AS blocking_reason,
			COALESCE(m.next_action_hint, '') AS next_action_hint
		FROM tenants t
		LEFT JOIN trial_customer_ownerships o ON o.tenant_id = t.id
		LEFT JOIN ops_organizations org ON org.id = o.owner_org_id
		LEFT JOIN trial_customer_assignments a ON a.tenant_id = t.id
		LEFT JOIN trial_customer_metrics m ON m.tenant_id = t.id
		WHERE t.id = $1
		  AND t.deleted_at IS NULL
		  AND lower(COALESCE(t.account_mode, 'formal')) = 'trial'
	`, tenantID).Scan(
		&selected.TenantID,
		&selected.TenantName,
		&selected.TenantCode,
		&selected.ContactName,
		&selected.ContactPhone,
		&selected.AccountMode,
		&selected.CurrentStage,
		&selected.ActivationStatus,
		&selected.SalesOwnerAdminID,
		&selected.SalesOwnerName,
		&selected.OwnerOrgID,
		&selected.OwnerOrgName,
		&selected.Source,
		&selected.Notes,
		&selected.TrialStartedAt,
		&selected.TrialExpiresAt,
		&selected.FirstLoginAt,
		&selected.LastLoginAt,
		&selected.FirstUploadAt,
		&selected.LastUploadAt,
		&selected.LoginCount,
		&selected.AnalysisCount,
		&selected.UploadCount,
		&selected.DoctorDemoViewed,
		&selected.ConsultantDemoViewed,
		&selected.RealRecordingUploadCount,
		&selected.GeneratedCustomerCount,
		&selected.GeneratedTaskCount,
		&selected.PriorityLevel,
		&selected.BlockingReason,
		&selected.NextActionHint,
	); err != nil {
		return nil, fmt.Errorf("trial customer not found")
	}

	metrics := TrialCustomerMetrics{TenantID: tenantID}
	_ = s.pool.QueryRow(ctx, `
		SELECT
			tenant_id, trial_started_at, trial_expires_at, first_login_at, last_login_at, login_count,
			doctor_demo_viewed_at, consultant_demo_viewed_at, first_upload_at, last_upload_at,
			upload_count, analysis_count, doctor_upload_count, consultant_upload_count,
			generated_customer_count, generated_task_count, generated_content_count, wechat_content_count, xiaohongshu_content_count, video_script_content_count, last_activity_at, current_stage,
			priority_level, blocking_reason, next_action_hint, updated_at
		FROM trial_customer_metrics
		WHERE tenant_id = $1
	`, tenantID).Scan(
		&metrics.TenantID, &metrics.TrialStartedAt, &metrics.TrialExpiresAt, &metrics.FirstLoginAt, &metrics.LastLoginAt, &metrics.LoginCount,
		&metrics.DoctorDemoViewedAt, &metrics.ConsultantDemoViewedAt, &metrics.FirstUploadAt, &metrics.LastUploadAt,
		&metrics.UploadCount, &metrics.AnalysisCount, &metrics.DoctorUploadCount, &metrics.ConsultantUploadCount,
		&metrics.GeneratedCustomerCount, &metrics.GeneratedTaskCount, &metrics.GeneratedContentCount, &metrics.WechatContentCount, &metrics.XiaohongshuContentCount, &metrics.VideoScriptContentCount, &metrics.LastActivityAt, &metrics.CurrentStage,
		&metrics.PriorityLevel, &metrics.BlockingReason, &metrics.NextActionHint, &metrics.UpdatedAt,
	)
	if metrics.TrialMaxRecordings > 0 {
		metrics.TrialRemainingUsage = metrics.TrialMaxRecordings - metrics.UploadCount
		if metrics.TrialRemainingUsage < 0 {
			metrics.TrialRemainingUsage = 0
		}
	}

	assignment := TrialCustomerAssignment{TenantID: tenantID}
	_ = s.pool.QueryRow(ctx, `
		SELECT a.tenant_id,
		       a.sales_owner_admin_id,
		       COALESCE(a.sales_owner_name_snapshot, ''),
		       o.owner_org_id,
		       COALESCE(org.name, '') AS owner_org_name,
		       COALESCE(a.source, ''),
		       COALESCE(a.notes, ''),
		       a.assigned_at,
		       a.assigned_by,
		       a.updated_at
		FROM trial_customer_assignments a
		LEFT JOIN trial_customer_ownerships o ON o.tenant_id = a.tenant_id
		LEFT JOIN ops_organizations org ON org.id = o.owner_org_id
		WHERE a.tenant_id = $1
	`, tenantID).Scan(&assignment.TenantID, &assignment.SalesOwnerAdminID, &assignment.SalesOwnerNameSnapshot, &assignment.OwnerOrgID, &assignment.OwnerOrgName, &assignment.Source, &assignment.Notes, &assignment.AssignedAt, &assignment.AssignedBy, &assignment.UpdatedAt)

	roleUsage := []TrialCustomerRoleUsage{
		{
			RoleCode:        "doctor",
			DemoViewed:      metrics.DoctorDemoViewedAt != nil,
			RealUploadCount: metrics.DoctorUploadCount,
			InterestLevel:   deriveRoleInterest(metrics.DoctorDemoViewedAt != nil, metrics.DoctorUploadCount),
			StatusSummary:   deriveRoleStatusSummary("doctor", metrics.DoctorDemoViewedAt != nil, metrics.DoctorUploadCount),
		},
		{
			RoleCode:        "consultant",
			DemoViewed:      metrics.ConsultantDemoViewedAt != nil,
			RealUploadCount: metrics.ConsultantUploadCount,
			InterestLevel:   deriveRoleInterest(metrics.ConsultantDemoViewedAt != nil, metrics.ConsultantUploadCount),
			StatusSummary:   deriveRoleStatusSummary("consultant", metrics.ConsultantDemoViewedAt != nil, metrics.ConsultantUploadCount),
		},
	}

	milestones := buildTrialCustomerMilestones(metrics)
	followUps, err := s.ListTrialCustomerFollowUps(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	recommendation := TrialCustomerAIRecommendation{
		Summary:         deriveTrialCustomerSummary(metrics.CurrentStage, metrics.AnalysisCount, metrics.UploadCount),
		BlockingReason:  metrics.BlockingReason,
		RecommendedStep: metrics.NextActionHint,
		RecommendedTalk: deriveTrialCustomerTalkTrack(metrics.CurrentStage),
		RecommendedWhen: deriveTrialCustomerWhen(metrics.PriorityLevel),
	}

	return &TrialCustomerDetail{
		Item:             *selected,
		Metrics:          metrics,
		Assignment:       assignment,
		Milestones:       milestones,
		RoleUsage:        roleUsage,
		FollowUps:        followUps,
		AIRecommendation: recommendation,
	}, nil
}

func (s *Store) MarkTrialDemoViewed(ctx context.Context, tenantID int64, roleCode string, viewedByEmployeeID int64) error {
	roleCode = strings.ToLower(strings.TrimSpace(roleCode))
	if roleCode != "doctor" && roleCode != "consultant" {
		return fmt.Errorf("invalid role code")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE tenant_trial_demo_recordings
		SET viewed_at = COALESCE(viewed_at, NOW()),
		    viewed_by_employee_id = CASE WHEN viewed_by_employee_id IS NULL AND $3 > 0 THEN $3 ELSE viewed_by_employee_id END
		WHERE tenant_id = $1
		  AND lower(role_code) = $2
	`, tenantID, roleCode, viewedByEmployeeID)
	if err != nil {
		return fmt.Errorf("mark trial demo viewed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("trial demo recording not found")
	}
	return nil
}

func (s *Store) UpsertTrialCustomerAssignment(ctx context.Context, tenantID int64, req TrialCustomerUpsertAssignmentRequest, assignedBy *int64) (*TrialCustomerAssignment, error) {
	var snapshot string
	if req.SalesOwnerAdminID != nil && *req.SalesOwnerAdminID > 0 {
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(NULLIF(name, ''), username, '')
			FROM operations_admins
			WHERE id = $1
			  AND deleted_at IS NULL
		`, *req.SalesOwnerAdminID).Scan(&snapshot)
	}
	item := &TrialCustomerAssignment{TenantID: tenantID}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO trial_customer_assignments (
			tenant_id, sales_owner_admin_id, sales_owner_name_snapshot, source, notes, assigned_at, assigned_by, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, NOW(), $6, NOW())
		ON CONFLICT (tenant_id) DO UPDATE
		SET sales_owner_admin_id = EXCLUDED.sales_owner_admin_id,
		    sales_owner_name_snapshot = EXCLUDED.sales_owner_name_snapshot,
		    source = EXCLUDED.source,
		    notes = EXCLUDED.notes,
		    assigned_at = NOW(),
		    assigned_by = EXCLUDED.assigned_by,
		    updated_at = NOW()
		RETURNING tenant_id, sales_owner_admin_id, sales_owner_name_snapshot, source, notes, assigned_at, assigned_by, updated_at
	`, tenantID, req.SalesOwnerAdminID, snapshot, strings.TrimSpace(derefString(req.Source)), strings.TrimSpace(derefString(req.Notes)), assignedBy).Scan(
		&item.TenantID,
		&item.SalesOwnerAdminID,
		&item.SalesOwnerNameSnapshot,
		&item.Source,
		&item.Notes,
		&item.AssignedAt,
		&item.AssignedBy,
		&item.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert trial customer assignment: %w", err)
	}
	if ownership, ownErr := s.GetTrialCustomerOwnership(ctx, tenantID); ownErr == nil && ownership != nil {
		item.OwnerOrgID = ownership.OwnerOrgID
		item.OwnerOrgName = ownership.OwnerOrgName
	}
	return item, nil
}

func (s *Store) UpsertTrialCustomerOwnership(ctx context.Context, tenantID int64, ownerOrgID int64, salesOwnerUserID *int64, createdSource *string, assignedBy *int64) error {
	if tenantID <= 0 || ownerOrgID <= 0 {
		return fmt.Errorf("tenant_id and owner_org_id are required")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO trial_customer_ownerships (
			tenant_id, owner_org_id, sales_owner_user_id, created_source, assigned_at, assigned_by, updated_at
		)
		VALUES ($1, $2, $3, $4, NOW(), $5, NOW())
		ON CONFLICT (tenant_id) DO UPDATE
		SET owner_org_id = EXCLUDED.owner_org_id,
		    sales_owner_user_id = EXCLUDED.sales_owner_user_id,
		    created_source = EXCLUDED.created_source,
		    assigned_at = NOW(),
		    assigned_by = EXCLUDED.assigned_by,
		    updated_at = NOW()
	`, tenantID, ownerOrgID, salesOwnerUserID, strings.TrimSpace(derefString(createdSource)), assignedBy)
	if err != nil {
		return fmt.Errorf("upsert trial customer ownership: %w", err)
	}
	return nil
}

func (s *Store) GetTrialCustomerOwnership(ctx context.Context, tenantID int64) (*TrialCustomerAssignment, error) {
	item := &TrialCustomerAssignment{}
	if err := s.pool.QueryRow(ctx, `
		SELECT o.tenant_id,
		       o.sales_owner_user_id,
		       COALESCE(a.sales_owner_name_snapshot, ''),
		       o.owner_org_id,
		       COALESCE(org.name, '') AS owner_org_name,
		       COALESCE(a.source, ''),
		       COALESCE(a.notes, ''),
		       o.assigned_at,
		       o.assigned_by,
		       o.updated_at
		FROM trial_customer_ownerships o
		LEFT JOIN trial_customer_assignments a ON a.tenant_id = o.tenant_id
		LEFT JOIN ops_organizations org ON org.id = o.owner_org_id
		WHERE o.tenant_id = $1
	`, tenantID).Scan(&item.TenantID, &item.SalesOwnerAdminID, &item.SalesOwnerNameSnapshot, &item.OwnerOrgID, &item.OwnerOrgName, &item.Source, &item.Notes, &item.AssignedAt, &item.AssignedBy, &item.UpdatedAt); err != nil {
		return nil, fmt.Errorf("get trial customer ownership: %w", err)
	}
	return item, nil
}

func (s *Store) CreateTrialCustomerFollowUp(ctx context.Context, tenantID int64, req TrialCustomerFollowUpCreateRequest, createdBy *int64) (*TrialCustomerFollowUp, error) {
	var nextFollowUpAt *time.Time
	if req.NextFollowUpAt != nil && strings.TrimSpace(*req.NextFollowUpAt) != "" {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.NextFollowUpAt)); err == nil {
			nextFollowUpAt = &parsed
		}
	}
	var ownerAdminID *int64
	_ = s.pool.QueryRow(ctx, `
		SELECT sales_owner_admin_id
		FROM trial_customer_assignments
		WHERE tenant_id = $1
	`, tenantID).Scan(&ownerAdminID)
	item := &TrialCustomerFollowUp{}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO trial_customer_follow_ups (
			tenant_id, sales_owner_admin_id, follow_up_type, summary, result, next_follow_up_at, created_at, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), $7)
		RETURNING id, tenant_id, sales_owner_admin_id, follow_up_type, summary, result, next_follow_up_at, created_at, created_by
	`, tenantID, ownerAdminID, strings.TrimSpace(req.FollowUpType), strings.TrimSpace(req.Summary), strings.TrimSpace(req.Result), nextFollowUpAt, createdBy).Scan(
		&item.ID, &item.TenantID, &item.SalesOwnerAdminID, &item.FollowUpType, &item.Summary, &item.Result, &item.NextFollowUpAt, &item.CreatedAt, &item.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("create trial customer follow up: %w", err)
	}
	return item, nil
}

func (s *Store) ListTrialCustomerFollowUps(ctx context.Context, tenantID int64) ([]TrialCustomerFollowUp, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, sales_owner_admin_id, follow_up_type, summary, result, next_follow_up_at, created_at, created_by
		FROM trial_customer_follow_ups
		WHERE tenant_id = $1
		ORDER BY created_at DESC, id DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query trial customer follow ups: %w", err)
	}
	defer rows.Close()
	items := make([]TrialCustomerFollowUp, 0)
	for rows.Next() {
		var item TrialCustomerFollowUp
		if err := rows.Scan(&item.ID, &item.TenantID, &item.SalesOwnerAdminID, &item.FollowUpType, &item.Summary, &item.Result, &item.NextFollowUpAt, &item.CreatedAt, &item.CreatedBy); err != nil {
			return nil, fmt.Errorf("scan trial customer follow up: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trial customer follow ups: %w", err)
	}
	return items, nil
}

func (s *Store) GetTrialCustomerFunnel(ctx context.Context) (*TrialCustomerFunnelResponse, error) {
	stages := []TrialCustomerFunnelStage{
		{Code: "trial_opened", Label: "已开通试用"},
		{Code: "activated", Label: "已激活"},
		{Code: "viewed_demo", Label: "已查看示范案例"},
		{Code: "uploaded_real", Label: "已上传真实录音"},
		{Code: "completed_analysis", Label: "已完成首条分析"},
		{Code: "focused_followup", Label: "已进入转化跟进"},
		{Code: "converted", Label: "已转正式"},
	}
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tenants WHERE deleted_at IS NULL AND lower(COALESCE(account_mode, 'formal')) = 'trial'`).Scan(&stages[0].Count); err != nil {
		return nil, fmt.Errorf("count trial opened: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM trial_customer_metrics WHERE COALESCE(login_count, 0) > 0 OR COALESCE(upload_count, 0) > 0`).Scan(&stages[1].Count); err != nil {
		return nil, fmt.Errorf("count activated: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM trial_customer_metrics WHERE doctor_demo_viewed_at IS NOT NULL OR consultant_demo_viewed_at IS NOT NULL`).Scan(&stages[2].Count); err != nil {
		return nil, fmt.Errorf("count viewed demo: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM trial_customer_metrics WHERE upload_count > 0`).Scan(&stages[3].Count); err != nil {
		return nil, fmt.Errorf("count uploaded real: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM trial_customer_metrics WHERE analysis_count > 0`).Scan(&stages[4].Count); err != nil {
		return nil, fmt.Errorf("count completed analysis: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM trial_customer_metrics WHERE current_stage = 'focused_followup'`).Scan(&stages[5].Count); err != nil {
		return nil, fmt.Errorf("count focused followup: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tenants WHERE deleted_at IS NULL AND lower(COALESCE(account_mode, 'formal')) = 'formal' AND id IN (SELECT tenant_id FROM trial_customer_metrics)`).Scan(&stages[6].Count); err != nil {
		return nil, fmt.Errorf("count converted: %w", err)
	}

	ownerRows, err := s.pool.Query(ctx, `
		SELECT
			a.sales_owner_admin_id,
			COALESCE(a.sales_owner_name_snapshot, '未分配') AS owner_name,
			COUNT(*) AS trial_count,
			COUNT(*) FILTER (WHERE COALESCE(m.login_count, 0) > 0 OR COALESCE(m.upload_count, 0) > 0) AS activated_count,
			COUNT(*) FILTER (WHERE COALESCE(m.upload_count, 0) > 0) AS uploaded_count,
			COUNT(*) FILTER (WHERE lower(COALESCE(t.account_mode, 'formal')) = 'formal') AS converted_count
		FROM tenants t
		LEFT JOIN trial_customer_assignments a ON a.tenant_id = t.id
		LEFT JOIN trial_customer_metrics m ON m.tenant_id = t.id
		WHERE t.deleted_at IS NULL
		  AND t.id IN (SELECT tenant_id FROM trial_customer_metrics)
		GROUP BY a.sales_owner_admin_id, COALESCE(a.sales_owner_name_snapshot, '未分配')
		ORDER BY trial_count DESC, owner_name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query trial funnel owner breakdown: %w", err)
	}
	defer ownerRows.Close()
	ownerBreakdown := make([]TrialCustomerOwnerFunnel, 0)
	for ownerRows.Next() {
		var item TrialCustomerOwnerFunnel
		if err := ownerRows.Scan(&item.OwnerAdminID, &item.OwnerName, &item.TrialCount, &item.ActivatedCount, &item.UploadedCount, &item.ConvertedCount); err != nil {
			return nil, fmt.Errorf("scan trial funnel owner breakdown: %w", err)
		}
		ownerBreakdown = append(ownerBreakdown, item)
	}
	if err := ownerRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trial funnel owner breakdown: %w", err)
	}

	blockingRows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(blocking_reason, ''), '未识别阻塞') AS blocking_reason, COUNT(*)
		FROM trial_customer_metrics
		GROUP BY COALESCE(NULLIF(blocking_reason, ''), '未识别阻塞')
		ORDER BY COUNT(*) DESC, blocking_reason ASC
		LIMIT 10
	`)
	if err != nil {
		return nil, fmt.Errorf("query trial funnel blocking reasons: %w", err)
	}
	defer blockingRows.Close()
	blockingReasons := make([]TrialCustomerFunnelStage, 0)
	for blockingRows.Next() {
		var item TrialCustomerFunnelStage
		item.Code = "blocking_reason"
		if err := blockingRows.Scan(&item.Label, &item.Count); err != nil {
			return nil, fmt.Errorf("scan trial funnel blocking reason: %w", err)
		}
		blockingReasons = append(blockingReasons, item)
	}
	if err := blockingRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trial funnel blocking reasons: %w", err)
	}

	return &TrialCustomerFunnelResponse{
		Stages:          stages,
		OwnerBreakdown:  ownerBreakdown,
		BlockingReasons: blockingReasons,
	}, nil
}

func latestTime(values ...*time.Time) *time.Time {
	var latest *time.Time
	for _, value := range values {
		if value == nil {
			continue
		}
		if latest == nil || value.After(*latest) {
			t := *value
			latest = &t
		}
	}
	return latest
}

func normalizeTrialContentCategory(contentType, platform string) string {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	platform = strings.ToLower(strings.TrimSpace(platform))

	switch {
	case platform == "wechat":
		return "wechat"
	case platform == "xiaohongshu":
		return "xiaohongshu"
	case platform == "douyin":
		return "video_script"
	}

	switch contentType {
	case "article", "wechat_article", "graphic_article", "公众号文章":
		return "wechat"
	case "graphic", "xhs_note", "graphic_note", "note", "xiaohongshu_note", "小红书", "小红书图文":
		return "xiaohongshu"
	case "video_script", "script", "short_video_script", "douyin_script", "视频脚本":
		return "video_script"
	default:
		return ""
	}
}

func deriveTrialCustomerStage(loginCount int, doctorDemoViewedAt, consultantDemoViewedAt *time.Time, uploadCount, analysisCount int, trialExpiresAt *time.Time) string {
	now := time.Now()
	if trialExpiresAt != nil && trialExpiresAt.Before(now) {
		return "expired_unconverted"
	}
	if analysisCount > 0 {
		return "completed_analysis"
	}
	if uploadCount > 0 {
		return "uploaded_real_recording"
	}
	if doctorDemoViewedAt != nil || consultantDemoViewedAt != nil {
		return "viewed_demo"
	}
	if loginCount > 0 {
		return "logged_in"
	}
	return "not_started"
}

func deriveTrialCustomerPriority(trialExpiresAt *time.Time, uploadCount, analysisCount int, lastLoginAt *time.Time) string {
	now := time.Now()
	if trialExpiresAt != nil {
		if trialExpiresAt.Before(now.Add(72 * time.Hour)) {
			return "high"
		}
	}
	if uploadCount > 0 || analysisCount > 0 {
		return "high"
	}
	if lastLoginAt != nil {
		return "medium"
	}
	return "normal"
}

func deriveTrialCustomerBlockingReason(currentStage string, trialExpiresAt *time.Time) string {
	switch currentStage {
	case "not_started":
		return "开通后未登录"
	case "logged_in":
		return "已登录但未查看示范案例"
	case "viewed_demo":
		return "已看示范但未上传真实录音"
	case "uploaded_real_recording":
		return "已上传真实录音但尚未完成分析"
	case "completed_analysis":
		return "已出分析但尚未进入重点跟进"
	case "expired_unconverted":
		return "试用已到期仍未转化"
	}
	if trialExpiresAt != nil && trialExpiresAt.Before(time.Now()) {
		return "试用已到期仍未转化"
	}
	return ""
}

func deriveTrialCustomerNextAction(currentStage, priority string) string {
	switch currentStage {
	case "not_started":
		return "建议销售发起首次唤醒沟通，推动客户尽快登录试用后台"
	case "logged_in":
		return "建议引导客户先看医生或咨询师示范案例，快速建立价值认知"
	case "viewed_demo":
		return "建议推动客户上传一条真实录音，完成从示范到验证的转化"
	case "uploaded_real_recording":
		return "建议盯分析结果生成，并在结果产出后第一时间发起复盘"
	case "completed_analysis":
		return "建议围绕真实录音结果约一次短复盘，推动转正式讨论"
	case "expired_unconverted":
		return "建议复盘流失原因，判断是否还有二次激活价值"
	}
	if priority == "high" {
		return "建议优先跟进，避免试用窗口流失"
	}
	return "建议继续观察并安排下一次跟进"
}

func deriveRoleInterest(demoViewed bool, uploadCount int) string {
	if uploadCount > 0 {
		return "high"
	}
	if demoViewed {
		return "medium"
	}
	return "low"
}

func deriveRoleStatusSummary(roleCode string, demoViewed bool, uploadCount int) string {
	roleLabel := "医生"
	if roleCode == "consultant" {
		roleLabel = "咨询师"
	}
	switch {
	case uploadCount > 0:
		return fmt.Sprintf("%s已上传真实录音", roleLabel)
	case demoViewed:
		return fmt.Sprintf("%s已看示范案例，待上传真实录音", roleLabel)
	default:
		return fmt.Sprintf("%s尚未启动", roleLabel)
	}
}

func buildTrialCustomerMilestones(metrics TrialCustomerMetrics) []TrialCustomerMilestone {
	return []TrialCustomerMilestone{
		{Code: "trial_started", Label: "已开通试用", OccurredAt: metrics.TrialStartedAt},
		{Code: "first_login", Label: "首次登录", OccurredAt: metrics.FirstLoginAt},
		{Code: "demo_viewed", Label: "首次查看示范案例", OccurredAt: earliestTime(metrics.DoctorDemoViewedAt, metrics.ConsultantDemoViewedAt)},
		{Code: "first_upload", Label: "首次上传真实录音", OccurredAt: metrics.FirstUploadAt},
		{Code: "first_analysis", Label: "首次完成分析", OccurredAt: metrics.LastUploadAt},
	}
}

func earliestTime(values ...*time.Time) *time.Time {
	var earliest *time.Time
	for _, value := range values {
		if value == nil {
			continue
		}
		if earliest == nil || value.Before(*earliest) {
			t := *value
			earliest = &t
		}
	}
	return earliest
}

func deriveTrialCustomerSummary(stage string, analysisCount, uploadCount int) string {
	switch stage {
	case "not_started":
		return "客户已开通试用，但尚未发生有效使用。"
	case "logged_in":
		return "客户已登录，已跨过最低激活门槛，但仍未进入价值体验。"
	case "viewed_demo":
		return "客户已完成示范体验，下一步关键是推动真实录音验证。"
	case "uploaded_real_recording":
		return "客户已经开始用真实录音验证产品，转化意愿明显增强。"
	case "completed_analysis":
		return "客户已拿到真实结果，当前重点是把分析结果转成转化沟通。"
	case "expired_unconverted":
		return "客户试用已到期，当前应复盘流失原因。"
	}
	if analysisCount > 0 {
		return "客户已完成分析体验。"
	}
	if uploadCount > 0 {
		return "客户已上传真实录音。"
	}
	return "客户仍处于早期试用阶段。"
}

func deriveTrialCustomerTalkTrack(stage string) string {
	switch stage {
	case "viewed_demo":
		return "你们已经看过示范案例，下一步最有价值的是用一条自己的真实录音验证结果是否贴合你们场景。"
	case "uploaded_real_recording":
		return "你们已经进入真实验证阶段，建议下一步直接看分析结果怎样落到院内管理动作上。"
	case "completed_analysis":
		return "你们已经拿到真实分析结果，建议我们直接围绕任务、客户建档和管理价值做一次短复盘。"
	default:
		return "建议先从最关键的一步开始，把试用推进到下一阶段。"
	}
}

func deriveTrialCustomerWhen(priority string) string {
	switch priority {
	case "high":
		return "建议24小时内跟进"
	case "medium":
		return "建议48小时内跟进"
	default:
		return "建议本周内跟进"
	}
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
