package billing

// 机构管理员的数据访问层：employees + institution_employee_roles + institution_roles。
//
// 为什么不用 operations_admins：那张表是运营端登录账号（LoginAdmin 只查它），
// 机构端登录（LoginEmployee）只认 employees；机构管理员必须能登录机构端，
// 且机构详情页的 admins 读的也是 employees，所以这里沿用现有账号体系。
//
// 主管理员用 employees.is_primary 显式标记（迁移 20261112_011），
// 每租户最多一个有效主管理员由部分唯一索引兜底。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/permission"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// institutionAdminRow 是机构管理员的一行（含角色与最后登录时间）。
type institutionAdminRow struct {
	ID          int64
	Name        string
	Phone       string
	Email       string
	RoleCode    string
	RoleName    string
	IsPrimary   bool
	IsActive    bool
	LastLoginAt *time.Time
}

// institutionAdminSelect 是管理员列表/单条共用的选择列。
//
// 角色取员工最近一条 institution_employee_roles；role_code 为空时回退到 institution_roles.code。
// 最后登录时间另走 attachInstitutionAdminLastLogins（表可能不存在，单独探测）。
const institutionAdminSelect = `
SELECT e.id,
       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), ''),
       COALESCE(e.phone, ''),
       COALESCE(e.email, ''),
       COALESCE(NULLIF(ier.role_code, ''), NULLIF(r.code, ''), 'admin'),
       COALESCE(r.name, ''),
       COALESCE(e.is_primary, FALSE),
       CASE WHEN lower(COALESCE(e.is_active::text, 'true')) IN ('1', 't', 'true', 'yes') THEN TRUE ELSE FALSE END
FROM employees e
LEFT JOIN LATERAL (
    SELECT er.role_code, er.role_id
    FROM institution_employee_roles er
    WHERE er.tenant_id = e.tenant_id
      AND er.employee_id = e.id
    ORDER BY er.updated_at DESC NULLS LAST, er.created_at DESC NULLS LAST, er.id DESC
    LIMIT 1
) ier ON TRUE
LEFT JOIN institution_roles r ON r.id = ier.role_id AND r.deleted_at IS NULL
`

// institutionAdminOrder 让主管理员排在最前，其余按创建先后。
const institutionAdminOrder = `
ORDER BY COALESCE(e.is_primary, FALSE) DESC, e.id
`

// ListInstitutionAdminRows 返回机构的全部管理员（admin 角色或主管理员）。
func (s *Store) ListInstitutionAdminRows(ctx context.Context, tenantID int64) ([]institutionAdminRow, error) {
	rows, err := s.pool.Query(ctx, institutionAdminSelect+`
WHERE e.tenant_id = $1
  AND e.deleted_at IS NULL
  AND (COALESCE(e.is_primary, FALSE)
       OR lower(COALESCE(NULLIF(ier.role_code, ''), r.code, '')) = 'admin')
`+institutionAdminOrder, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list institution admins: %w", err)
	}
	defer rows.Close()

	items := make([]institutionAdminRow, 0, 8)
	for rows.Next() {
		item, err := scanInstitutionAdmin(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan institution admin: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate institution admins: %w", err)
	}
	if err := s.attachInstitutionAdminLastLogins(ctx, tenantID, items); err != nil {
		return nil, err
	}
	return items, nil
}

// attachInstitutionAdminLastLogins 批量补最后登录时间。
// employee_login_events 由兼容迁移创建，单独探测表是否存在：缺表时保持空值，
// 不影响机构详情/管理员列表这两个核心读路径。
func (s *Store) attachInstitutionAdminLastLogins(ctx context.Context, tenantID int64, items []institutionAdminRow) error {
	if len(items) == 0 {
		return nil
	}
	var tableExists bool
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('public.employee_login_events') IS NOT NULL`).Scan(&tableExists); err != nil {
		return fmt.Errorf("check employee login events table: %w", err)
	}
	if !tableExists {
		return nil
	}
	rows, err := s.pool.Query(ctx, `
SELECT employee_id, MAX(login_at)
FROM employee_login_events
WHERE tenant_id = $1
GROUP BY employee_id`, tenantID)
	if err != nil {
		return fmt.Errorf("list institution admin last logins: %w", err)
	}
	defer rows.Close()

	lastLogins := make(map[int64]time.Time, len(items))
	for rows.Next() {
		var (
			employeeID int64
			loginAt    time.Time
		)
		if err := rows.Scan(&employeeID, &loginAt); err != nil {
			return fmt.Errorf("scan institution admin last login: %w", err)
		}
		lastLogins[employeeID] = loginAt
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate institution admin last logins: %w", err)
	}
	for index := range items {
		if loginAt, ok := lastLogins[items[index].ID]; ok {
			value := loginAt
			items[index].LastLoginAt = &value
		}
	}
	return nil
}

// GetInstitutionAdminRow 按租户 + 管理员 id 取一条；不存在（或不属于该租户）返回
// ErrInstitutionAdminNotFound，避免跨机构改别人。
func (s *Store) GetInstitutionAdminRow(ctx context.Context, tenantID, adminID int64) (institutionAdminRow, error) {
	row := s.pool.QueryRow(ctx, institutionAdminSelect+`
WHERE e.tenant_id = $1
  AND e.id = $2
  AND e.deleted_at IS NULL`, tenantID, adminID)
	item, err := scanInstitutionAdmin(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return institutionAdminRow{}, ErrInstitutionAdminNotFound
	}
	if err != nil {
		return institutionAdminRow{}, fmt.Errorf("get institution admin: %w", err)
	}
	items := []institutionAdminRow{item}
	if err := s.attachInstitutionAdminLastLogins(ctx, tenantID, items); err != nil {
		return institutionAdminRow{}, err
	}
	return items[0], nil
}

// scanInstitutionAdmin 统一解析管理员行（列表与单条共用）。
func scanInstitutionAdmin(scan func(dest ...any) error) (institutionAdminRow, error) {
	var item institutionAdminRow
	if err := scan(
		&item.ID,
		&item.Name,
		&item.Phone,
		&item.Email,
		&item.RoleCode,
		&item.RoleName,
		&item.IsPrimary,
		&item.IsActive,
	); err != nil {
		return institutionAdminRow{}, err
	}
	return item, nil
}

// rowQuerier 抽象 pool / tx 的 QueryRow（主管理员计数要在事务里也复用）。
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// CountActivePrimaryAdmins 统计机构当前启用的主管理员数量。
func (s *Store) CountActivePrimaryAdmins(ctx context.Context, tenantID int64) (int, error) {
	return countActivePrimaryAdmins(ctx, s.pool, tenantID)
}

// CountActivePrimaryAdminsTx 是事务内版本（首任主管理员判定用）。
func (s *Store) CountActivePrimaryAdminsTx(ctx context.Context, tx pgx.Tx, tenantID int64) (int, error) {
	return countActivePrimaryAdmins(ctx, tx, tenantID)
}

// LockInstitutionAdminsTx 按机构串行化管理员写操作，避免并发创建出两个「首任主管理员」
// 或并发停用/升主管理员绕过「至少一个启用主管理员」判断。
// advisory lock 随事务结束自动释放。
func (s *Store) LockInstitutionAdminsTx(ctx context.Context, tx pgx.Tx, tenantID int64) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(20261112011)+tenantID); err != nil {
		return fmt.Errorf("lock institution admins: %w", err)
	}
	return nil
}

func countActivePrimaryAdmins(ctx context.Context, q rowQuerier, tenantID int64) (int, error) {
	var count int
	if err := q.QueryRow(ctx, `
SELECT COUNT(*)
FROM employees
WHERE tenant_id = $1
  AND is_primary
  AND deleted_at IS NULL
  AND lower(COALESCE(is_active::text, 'true')) IN ('1', 't', 'true', 'yes')`, tenantID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active primary admins: %w", err)
	}
	return count, nil
}

// execer 抽象 pool / tx 的 Exec（补主管理员的两条入口共用同一段 SQL）。
type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// ensurePrimaryInstitutionAdminSQL 选一个启用 admin 员工补成主管理员：
// 优先手机号等于租户联系人，其次 id 最小；已有主管理员时不更新。
const ensurePrimaryInstitutionAdminSQL = `
UPDATE employees e
SET is_primary = TRUE,
    updated_at = NOW()
WHERE e.tenant_id = $1
  AND e.deleted_at IS NULL
  AND lower(COALESCE(e.is_active::text, 'true')) IN ('1', 't', 'true', 'yes')
  AND e.id = (
      SELECT e2.id
      FROM employees e2
      WHERE e2.tenant_id = $1
        AND e2.deleted_at IS NULL
        AND lower(COALESCE(e2.is_active::text, 'true')) IN ('1', 't', 'true', 'yes')
        AND EXISTS (
            SELECT 1
            FROM institution_employee_roles er
            LEFT JOIN institution_roles r ON r.id = er.role_id AND r.deleted_at IS NULL
            WHERE er.tenant_id = e2.tenant_id
              AND er.employee_id = e2.id
              AND lower(COALESCE(NULLIF(er.role_code, ''), r.code, '')) = 'admin'
        )
      ORDER BY CASE
                   WHEN trim(COALESCE(e2.phone, '')) = (
                       SELECT trim(COALESCE(contact_phone, '')) FROM tenants WHERE id = $1
                   ) THEN 0
                   ELSE 1
               END,
               e2.id
      LIMIT 1
  )
  AND NOT EXISTS (
      SELECT 1 FROM employees p
      WHERE p.tenant_id = $1
        AND p.is_primary
        AND p.deleted_at IS NULL
  )`

// EnsurePrimaryInstitutionAdminTx 在机构还没有主管理员时，把默认 admin 员工补成主管理员。
// 供建档链路调用（tenant.Store.CreateTenant 创建默认管理员之后）；已有主管理员时不动。
func (s *Store) EnsurePrimaryInstitutionAdminTx(ctx context.Context, tx pgx.Tx, tenantID int64) error {
	return execEnsurePrimaryInstitutionAdmin(ctx, tx, tenantID)
}

// EnsurePrimaryInstitutionAdmin 是事务外版本：历史/其它入口创建的机构在首次读
// 详情或管理员列表时自愈，避免「有管理员但没有主管理员」。
func (s *Store) EnsurePrimaryInstitutionAdmin(ctx context.Context, tenantID int64) error {
	return execEnsurePrimaryInstitutionAdmin(ctx, s.pool, tenantID)
}

func execEnsurePrimaryInstitutionAdmin(ctx context.Context, exec execer, tenantID int64) error {
	if _, err := exec.Exec(ctx, ensurePrimaryInstitutionAdminSQL, tenantID); err != nil {
		return fmt.Errorf("ensure primary institution admin: %w", err)
	}
	return nil
}

// institutionAdminInsert 是新增管理员落库所需字段。
type institutionAdminInsert struct {
	TenantID     int64
	Username     string
	PasswordHash string
	Name         string
	Phone        string
	Email        string
	DepartmentID *int64
	RoleID       int64
	RoleCode     string
	IsPrimary    bool
}

// InsertInstitutionAdminTx 新增一个机构管理员（员工 + admin 角色 + EMR 权限）。
// 初始密码只落库哈希，绝不回传。
func (s *Store) InsertInstitutionAdminTx(ctx context.Context, tx pgx.Tx, input institutionAdminInsert) (int64, error) {
	var employeeID int64
	if err := tx.QueryRow(ctx, `
INSERT INTO employees (
    tenant_id, username, password_hash, name, full_name, phone, email,
    department_id, session_version, is_active, is_primary, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, 1, $9, NOW(), NOW())
RETURNING id`,
		input.TenantID, input.Username, input.PasswordHash, input.Name, input.Name,
		input.Phone, input.Email, nullableIDFromPtr(input.DepartmentID), input.IsPrimary,
	).Scan(&employeeID); err != nil {
		return 0, fmt.Errorf("insert institution admin: %w", err)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO institution_employee_roles (employee_id, role_id, tenant_id, role_code, source, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'ops_billing', NOW(), NOW())
ON CONFLICT (employee_id) DO UPDATE
SET role_id = EXCLUDED.role_id,
    tenant_id = EXCLUDED.tenant_id,
    role_code = EXCLUDED.role_code,
    source = EXCLUDED.source,
    updated_at = NOW()`,
		employeeID, input.RoleID, input.TenantID, input.RoleCode); err != nil {
		return 0, fmt.Errorf("assign institution admin role: %w", err)
	}
	if _, err := permission.EnsureInstitutionAdminGrants(ctx, tx, input.TenantID, employeeID, employeeID, false); err != nil {
		return 0, fmt.Errorf("initialize institution admin product permissions: %w", err)
	}
	return employeeID, nil
}

// UpdateInstitutionAdminTx 修改管理员基本信息/启停。
// name/email 为 nil 表示不变；bumpSession 为 true 时让该员工的旧 token 失效。
func (s *Store) UpdateInstitutionAdminTx(ctx context.Context, tx pgx.Tx, tenantID, adminID int64,
	name, email *string, active *bool, bumpSession bool) error {
	var activeFlag *int
	if active != nil {
		value := 0
		if *active {
			value = 1
		}
		activeFlag = &value
	}
	tag, err := tx.Exec(ctx, `
UPDATE employees
SET name = COALESCE($3, name),
    full_name = COALESCE($3, full_name),
    email = COALESCE($4, email),
    is_active = COALESCE($5::int, is_active),
    session_version = CASE WHEN $6::boolean THEN COALESCE(session_version, 1) + 1 ELSE session_version END,
    updated_at = NOW()
WHERE id = $1
  AND tenant_id = $2
  AND deleted_at IS NULL`, adminID, tenantID, name, email, activeFlag, bumpSession)
	if err != nil {
		return fmt.Errorf("update institution admin: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInstitutionAdminNotFound
	}
	return nil
}

// ResetInstitutionAdminPassword 重置密码并 bump session_version（旧 token 立即失效）。
func (s *Store) ResetInstitutionAdminPassword(ctx context.Context, tenantID, adminID int64, passwordHash string) error {
	tag, err := s.pool.Exec(ctx, `
UPDATE employees
SET password_hash = $3,
    session_version = COALESCE(session_version, 1) + 1,
    updated_at = NOW()
WHERE id = $1
  AND tenant_id = $2
  AND deleted_at IS NULL`, adminID, tenantID, passwordHash)
	if err != nil {
		return fmt.Errorf("reset institution admin password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInstitutionAdminNotFound
	}
	return nil
}

// ClearInstitutionAdminPrimaryTx 清掉机构当前的 is_primary 标记。
// 用于「没有启用主管理员但存在历史/停用的 primary」时让新建管理员接管。
func (s *Store) ClearInstitutionAdminPrimaryTx(ctx context.Context, tx pgx.Tx, tenantID int64) error {
	if _, err := tx.Exec(ctx, `
UPDATE employees
SET is_primary = FALSE,
    updated_at = NOW()
WHERE tenant_id = $1
  AND is_primary
  AND deleted_at IS NULL`, tenantID); err != nil {
		return fmt.Errorf("clear institution primary admins: %w", err)
	}
	return nil
}

// SetPrimaryInstitutionAdminTx 把指定管理员设为主管理员（先降旧主管理员，再升目标）。
func (s *Store) SetPrimaryInstitutionAdminTx(ctx context.Context, tx pgx.Tx, tenantID, adminID int64) error {
	if _, err := tx.Exec(ctx, `
UPDATE employees
SET is_primary = FALSE,
    updated_at = NOW()
WHERE tenant_id = $1
  AND is_primary
  AND deleted_at IS NULL
  AND id <> $2`, tenantID, adminID); err != nil {
		return fmt.Errorf("demote current primary admin: %w", err)
	}
	tag, err := tx.Exec(ctx, `
UPDATE employees
SET is_primary = TRUE,
    updated_at = NOW()
WHERE id = $1
  AND tenant_id = $2
  AND deleted_at IS NULL`, adminID, tenantID)
	if err != nil {
		return fmt.Errorf("promote institution admin: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInstitutionAdminNotFound
	}
	return nil
}

// ResolveInstitutionAdminRoleTx 解析/补齐机构 admin 角色。
// role 只接受 admin / 管理员 / 主管理员（都归一为 admin 角色）。
func (s *Store) ResolveInstitutionAdminRoleTx(ctx context.Context, tx pgx.Tx, tenantID int64, role string) (int64, string, string, error) {
	roleCode, ok := normalizeInstitutionAdminRole(role)
	if !ok {
		return 0, "", "", invalidf("机构管理员角色只能是 admin")
	}
	var roleID int64
	var name string
	err := tx.QueryRow(ctx, `
SELECT id, COALESCE(name, '')
FROM institution_roles
WHERE tenant_id = $1
  AND lower(code) = $2
  AND deleted_at IS NULL
ORDER BY id DESC
LIMIT 1`, tenantID, roleCode).Scan(&roleID, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx, `
INSERT INTO institution_roles (tenant_id, name, code, description, is_active, created_at, updated_at)
VALUES ($1, $2, $3, '系统管理员角色', TRUE, NOW(), NOW())
ON CONFLICT (tenant_id, code) WHERE deleted_at IS NULL
DO UPDATE SET updated_at = NOW()
RETURNING id`, tenantID, institutionAdminRoleName, roleCode).Scan(&roleID); err != nil {
			return 0, "", "", fmt.Errorf("ensure institution admin role: %w", err)
		}
		name = institutionAdminRoleName
	} else if err != nil {
		return 0, "", "", fmt.Errorf("resolve institution admin role: %w", err)
	}
	if strings.TrimSpace(name) == "" {
		name = institutionAdminRoleName
	}
	return roleID, roleCode, name, nil
}

// normalizeInstitutionAdminRole 归一化角色入参；只接受 admin 系列。
func normalizeInstitutionAdminRole(role string) (string, bool) {
	switch normalizeEnum(role) {
	case "", "admin", "管理员", "主管理员":
		return institutionAdminRoleCode, true
	default:
		return "", false
	}
}

// ResolveInstitutionAdminDepartmentID 取机构「运营部」作为管理员默认部门；没有返回 nil。
func (s *Store) ResolveInstitutionAdminDepartmentID(ctx context.Context, tenantID int64) (*int64, error) {
	var departmentID int64
	err := s.pool.QueryRow(ctx, `
SELECT id
FROM departments
WHERE tenant_id = $1
  AND deleted_at IS NULL
  AND (code = 'operations_department' OR name = '运营部')
ORDER BY id
LIMIT 1`, tenantID).Scan(&departmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve institution admin department: %w", err)
	}
	return &departmentID, nil
}

// InstitutionAdminUsernameTaken 判断同租户内用户名是否已被占用（登录按 username/name/phone 匹配）。
func (s *Store) InstitutionAdminUsernameTaken(ctx context.Context, tenantID int64, username string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM employees
    WHERE tenant_id = $1
      AND username = $2
      AND deleted_at IS NULL
)`, tenantID, username).Scan(&exists); err != nil {
		return false, fmt.Errorf("check institution admin username: %w", err)
	}
	return exists, nil
}

// InstitutionAdminPhoneOwner 返回占用该手机号的账号名（员工优先，其次运营账号）。
// 员工手机号是全局唯一索引（含软删），冲突时给出可读提示而不是让 INSERT 抛 500。
func (s *Store) InstitutionAdminPhoneOwner(ctx context.Context, phone string) (string, bool, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
SELECT COALESCE(NULLIF(t.name, ''), NULLIF(e.name, ''), '其他机构')
FROM employees e
LEFT JOIN tenants t ON t.id = e.tenant_id
WHERE e.phone = $1
ORDER BY e.id
LIMIT 1`, phone).Scan(&name)
	if err == nil {
		return name, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("check institution admin phone (employees): %w", err)
	}
	err = s.pool.QueryRow(ctx, `
SELECT COALESCE(NULLIF(name, ''), NULLIF(username, ''), '其他账号')
FROM operations_admins
WHERE phone = $1
ORDER BY id
LIMIT 1`, phone).Scan(&name)
	if err == nil {
		return name, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return "", false, fmt.Errorf("check institution admin phone (operations_admins): %w", err)
}

// InstitutionPhoneOwnerExcludingTenant 是编辑机构联系人手机号时的冲突检查：
// 该手机号若属于「其它租户」的员工，返回占用机构名。
func (s *Store) InstitutionPhoneOwnerExcludingTenant(ctx context.Context, phone string, tenantID int64) (string, bool, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
SELECT COALESCE(NULLIF(t.name, ''), NULLIF(e.name, ''), '其他机构')
FROM employees e
LEFT JOIN tenants t ON t.id = e.tenant_id
WHERE e.phone = $1
  AND e.tenant_id <> $2
ORDER BY e.id
LIMIT 1`, phone, tenantID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("check institution contact phone: %w", err)
	}
	return name, true, nil
}

// nullableIDFromPtr 把可空 id 指针原样传回 SQL（nil -> NULL）。
func nullableIDFromPtr(value *int64) *int64 {
	if value == nil || *value <= 0 {
		return nil
	}
	return value
}
