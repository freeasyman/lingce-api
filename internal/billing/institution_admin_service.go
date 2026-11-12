package billing

// 运营端「机构管理员管理」服务层。
//
// 权限：所有读写都走 requireTenantAccess（平台账号放行，渠道账号只看自己组织名下的机构），
// adminId 必须属于路径里的机构，否则 404。
//
// 业务约束：
//   - 每个机构最多一个主管理员（employees.is_primary + 部分唯一索引）；
//   - 每个机构至少一个启用主管理员：不能停用当前唯一启用的主管理员；
//   - 重置密码 = 默认口令 + session_version+1（旧 token 立即失效），响应不含明文；
//   - 默认口令沿用 tenant.defaultTenantAdminPassword（仓库里是 "123456"）。

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// institutionAdminDefaultPassword 沿用 tenant.defaultTenantAdminPassword（internal/tenant/store.go）。
// 该常量在 tenant 包内未导出，这里复用 billing 中同值的 defaultOpsAdminPassword，
// 口径与「机构建档时默认管理员」完全一致：用户首次登录后自行修改。
const institutionAdminDefaultPassword = defaultOpsAdminPassword

// ListInstitutionAdmins 返回机构的管理员列表。
func (s *InstitutionService) ListInstitutionAdmins(ctx context.Context, tenantID, actorID int64) (*InstitutionAdminListResult, error) {
	if err := s.requireTenantAccess(ctx, tenantID, actorID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetInstitutionBase(ctx, tenantID); err != nil {
		return nil, err
	}
	rows, err := s.store.ListInstitutionAdminRows(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	rows = s.ensureInstitutionPrimaryAdmin(ctx, tenantID, rows)
	items := make([]InstitutionAdminAccount, 0, len(rows))
	for _, row := range rows {
		items = append(items, institutionAdminAccountOf(row))
	}
	return &InstitutionAdminListResult{Items: items}, nil
}

// CreateInstitutionAdmin 新增机构管理员。
func (s *InstitutionService) CreateInstitutionAdmin(ctx context.Context, tenantID int64, req CreateInstitutionAdminRequest, actorID int64) (*InstitutionAdminAccount, error) {
	if err := s.requireTenantAccess(ctx, tenantID, actorID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetInstitutionBase(ctx, tenantID); err != nil {
		return nil, err
	}
	req, err := req.normalize()
	if err != nil {
		return nil, err
	}

	// 手机号全局唯一（employees 全局唯一索引 + operations_admins 唯一索引）。
	if owner, found, lookupErr := s.store.InstitutionAdminPhoneOwner(ctx, req.Phone); lookupErr != nil {
		return nil, lookupErr
	} else if found {
		return nil, &ConflictError{
			Sentinel: ErrInstitutionAdminPhoneConflict,
			Message:  fmt.Sprintf("该手机号已被「%s」使用", owner),
		}
	}

	username, err := s.resolveInstitutionAdminUsername(ctx, tenantID, req.Phone)
	if err != nil {
		return nil, err
	}
	departmentID, err := s.store.ResolveInstitutionAdminDepartmentID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(institutionAdminDefaultPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash institution admin password: %w", err)
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin institution admin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.store.LockInstitutionAdminsTx(ctx, tx, tenantID); err != nil {
		return nil, err
	}

	// 机构还没有启用主管理员时，本次创建的人自动成为主管理员（锁内判定，避免并发重复）。
	primaryCount, err := s.store.CountActivePrimaryAdminsTx(ctx, tx, tenantID)
	if err != nil {
		return nil, err
	}
	isPrimary := primaryCount == 0
	if isPrimary {
		// 兼容历史上「primary 但已停用」的脏数据：先清标记再由本次创建接管。
		if err := s.store.ClearInstitutionAdminPrimaryTx(ctx, tx, tenantID); err != nil {
			return nil, err
		}
	}

	roleID, roleCode, _, err := s.store.ResolveInstitutionAdminRoleTx(ctx, tx, tenantID, req.Role)
	if err != nil {
		return nil, err
	}
	employeeID, err := s.store.InsertInstitutionAdminTx(ctx, tx, institutionAdminInsert{
		TenantID:     tenantID,
		Username:     username,
		PasswordHash: string(passwordHash),
		Name:         req.Name,
		Phone:        req.Phone,
		Email:        req.Email,
		DepartmentID: departmentID,
		RoleID:       roleID,
		RoleCode:     roleCode,
		IsPrimary:    isPrimary,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit institution admin transaction: %w", err)
	}

	row, err := s.store.GetInstitutionAdminRow(ctx, tenantID, employeeID)
	if err != nil {
		return nil, err
	}
	account := institutionAdminAccountOf(row)
	s.recordInstitutionAdminLog(ctx, tenantID, actorID, "institution.admin.create", "新增机构管理员", row, nil, map[string]any{
		"name":      row.Name,
		"phone":     row.Phone,
		"email":     row.Email,
		"roleCode":  row.RoleCode,
		"isPrimary": row.IsPrimary,
	})
	return &account, nil
}

// UpdateInstitutionAdmin 修改管理员基本信息/启停。
func (s *InstitutionService) UpdateInstitutionAdmin(ctx context.Context, tenantID, adminID int64, req UpdateInstitutionAdminRequest, actorID int64) (*InstitutionAdminAccount, error) {
	if err := s.requireTenantAccess(ctx, tenantID, actorID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetInstitutionBase(ctx, tenantID); err != nil {
		return nil, err
	}
	target, err := s.store.GetInstitutionAdminRow(ctx, tenantID, adminID)
	if err != nil {
		return nil, err
	}
	req, err = req.normalize()
	if err != nil {
		return nil, err
	}
	if req.Name == nil && req.Email == nil && req.IsActive == nil {
		account := institutionAdminAccountOf(target)
		return &account, nil
	}

	bumpSession := req.IsActive != nil && !*req.IsActive
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin institution admin update transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.store.LockInstitutionAdminsTx(ctx, tx, tenantID); err != nil {
		return nil, err
	}

	// 「最后一个启用的主管理员」不能停用：否则机构就没人能管了。
	// 每租户最多一个主管理员（唯一索引），所以目标只要是启用的主管理员就属于最后一人；
	// 判定放在租户锁内，避免与并发的「设为主管理员」互相踩踏。
	if req.IsActive != nil && !*req.IsActive && target.IsPrimary && target.IsActive {
		count, countErr := s.store.CountActivePrimaryAdminsTx(ctx, tx, tenantID)
		if countErr != nil {
			return nil, countErr
		}
		if count <= 1 {
			return nil, invalidf("该管理员是机构唯一启用的主管理员，不能停用；请先把其他管理员设为主管理员")
		}
	}

	if err := s.store.UpdateInstitutionAdminTx(ctx, tx, tenantID, adminID, req.Name, req.Email, req.IsActive, bumpSession); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit institution admin update transaction: %w", err)
	}

	row, err := s.store.GetInstitutionAdminRow(ctx, tenantID, adminID)
	if err != nil {
		return nil, err
	}
	account := institutionAdminAccountOf(row)
	s.recordInstitutionAdminLog(ctx, tenantID, actorID, "institution.admin.update", "编辑机构管理员", row,
		map[string]any{
			"name":     target.Name,
			"email":    target.Email,
			"isActive": target.IsActive,
		},
		map[string]any{
			"name":     row.Name,
			"email":    row.Email,
			"isActive": row.IsActive,
		})
	return &account, nil
}

// ResetInstitutionAdminPassword 重置为默认口令并 bump session_version；响应不含明文。
func (s *InstitutionService) ResetInstitutionAdminPassword(ctx context.Context, tenantID, adminID, actorID int64) (*InstitutionAdminAccount, error) {
	if err := s.requireTenantAccess(ctx, tenantID, actorID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetInstitutionBase(ctx, tenantID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetInstitutionAdminRow(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(institutionAdminDefaultPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash institution admin password: %w", err)
	}
	if err := s.store.ResetInstitutionAdminPassword(ctx, tenantID, adminID, string(passwordHash)); err != nil {
		return nil, err
	}
	row, err := s.store.GetInstitutionAdminRow(ctx, tenantID, adminID)
	if err != nil {
		return nil, err
	}
	account := institutionAdminAccountOf(row)
	s.recordInstitutionAdminLog(ctx, tenantID, actorID, "institution.admin.reset_password", "重置机构管理员密码", row, nil, nil)
	return &account, nil
}

// SetInstitutionAdminPrimary 把指定管理员设为主管理员（同一机构只能有一个）。
func (s *InstitutionService) SetInstitutionAdminPrimary(ctx context.Context, tenantID, adminID, actorID int64) (*InstitutionAdminAccount, error) {
	if err := s.requireTenantAccess(ctx, tenantID, actorID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetInstitutionBase(ctx, tenantID); err != nil {
		return nil, err
	}
	target, err := s.store.GetInstitutionAdminRow(ctx, tenantID, adminID)
	if err != nil {
		return nil, err
	}
	if !target.IsActive {
		return nil, invalidf("已停用的管理员不能设为主管理员")
	}
	if !target.IsPrimary {
		tx, beginErr := s.store.pool.Begin(ctx)
		if beginErr != nil {
			return nil, fmt.Errorf("begin set primary admin transaction: %w", beginErr)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := s.store.LockInstitutionAdminsTx(ctx, tx, tenantID); err != nil {
			return nil, err
		}
		if err := s.store.SetPrimaryInstitutionAdminTx(ctx, tx, tenantID, adminID); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit set primary admin transaction: %w", err)
		}
		s.recordInstitutionAdminLog(ctx, tenantID, actorID, "institution.admin.set_primary", "设为主管理员", target, nil, nil)
	}
	row, err := s.store.GetInstitutionAdminRow(ctx, tenantID, adminID)
	if err != nil {
		return nil, err
	}
	account := institutionAdminAccountOf(row)
	return &account, nil
}

// ensureInstitutionPrimaryAdmin 兜底自愈：历史/其它入口创建的机构可能还没 is_primary，
// 在首次读详情或管理员列表时补一个（优先联系人手机号，其次最早的管理员）。
// 写失败只告警，不影响读接口返回。
func (s *InstitutionService) ensureInstitutionPrimaryAdmin(ctx context.Context, tenantID int64, rows []institutionAdminRow) []institutionAdminRow {
	if len(rows) == 0 {
		return rows
	}
	for _, row := range rows {
		if row.IsPrimary {
			return rows
		}
	}
	if err := s.store.EnsurePrimaryInstitutionAdmin(ctx, tenantID); err != nil {
		slog.Warn("ensure institution primary admin failed", "tenant_id", tenantID, "error", err)
		return rows
	}
	refreshed, err := s.store.ListInstitutionAdminRows(ctx, tenantID)
	if err != nil {
		return rows
	}
	return refreshed
}

// resolveInstitutionAdminUsername 生成机构内唯一的登录名：默认用手机号，冲突时加租户后缀。
func (s *InstitutionService) resolveInstitutionAdminUsername(ctx context.Context, tenantID int64, phone string) (string, error) {
	candidates := make([]string, 0, 4)
	candidates = append(candidates, phone)
	candidates = append(candidates, fmt.Sprintf("%s_t%d", phone, tenantID))
	for i := 2; i <= 20; i++ {
		candidates = append(candidates, fmt.Sprintf("%s_t%d_%d", phone, tenantID, i))
	}
	for _, candidate := range candidates {
		taken, err := s.store.InstitutionAdminUsernameTaken(ctx, tenantID, candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", invalidf("无法生成唯一登录名，请更换手机号")
}

// recordInstitutionAdminLog 写管理员变更日志（actor 是运营账号，target 是员工 id）。
func (s *InstitutionService) recordInstitutionAdminLog(ctx context.Context, tenantID, actorID int64, actionCode, actionName string, target institutionAdminRow, before, after map[string]any) {
	tenantName := ""
	if base, err := s.store.GetInstitutionBase(ctx, tenantID); err == nil {
		tenantName = base.Name
	}
	actorName, _ := s.store.GetOperationsAdminName(ctx, actorID)
	if err := s.store.InsertSystemActionLog(ctx, institutionActionLog{
		TenantID:   tenantID,
		TenantName: tenantName,
		ActorID:    actorID,
		ActorName:  actorName,
		ActionCode: actionCode,
		ActionName: actionName,
		ObjectType: "institution_admin",
		ObjectID:   strconv.FormatInt(target.ID, 10),
		ObjectName: target.Name,
		Before:     before,
		After:      after,
	}); err != nil {
		slog.Warn("record institution admin log failed", "tenant_id", tenantID, "admin_id", target.ID, "action", actionCode, "error", err)
	}
}

// institutionAdminsFromRows 把管理员行转成机构详情里的 admins（兼容旧前端：主管理员 role=主管理员）。
func institutionAdminsFromRows(rows []institutionAdminRow) []InstitutionAdmin {
	items := make([]InstitutionAdmin, 0, len(rows))
	for _, row := range rows {
		role := strings.TrimSpace(row.RoleName)
		if role == "" {
			role = institutionAdminRoleName
		}
		if row.IsPrimary {
			role = "主管理员"
		}
		lastLogin := "—"
		if row.LastLoginAt != nil {
			lastLogin = row.LastLoginAt.Format("2006-01-02 15:04")
		}
		items = append(items, InstitutionAdmin{
			ID:        row.ID,
			Name:      row.Name,
			Phone:     row.Phone,
			Role:      role,
			LastLogin: lastLogin,
			Active:    row.IsActive,
		})
	}
	return items
}

// institutionAdminAccountOf 把管理员行转成新接口的 camelCase DTO。
func institutionAdminAccountOf(row institutionAdminRow) InstitutionAdminAccount {
	role := strings.TrimSpace(row.RoleName)
	if role == "" {
		role = institutionAdminRoleName
	}
	lastLoginAt := ""
	if row.LastLoginAt != nil {
		lastLoginAt = row.LastLoginAt.Format("2006-01-02 15:04")
	}
	return InstitutionAdminAccount{
		ID:          row.ID,
		Name:        row.Name,
		Phone:       row.Phone,
		Email:       row.Email,
		Role:        role,
		RoleCode:    row.RoleCode,
		IsPrimary:   row.IsPrimary,
		IsActive:    row.IsActive,
		LastLoginAt: lastLoginAt,
	}
}
