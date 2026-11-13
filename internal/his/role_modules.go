package his

import (
	"errors"
	"strings"
)

// HIS 角色与模块目录（与迁移 20261112_085_his_roles_modules.sql 及前端导航保持一致）。
// 角色顺序即管理页权限矩阵行顺序；模块顺序即列顺序。
var hisRoleOrder = []string{
	"admin", "doctor", "assistant", "nurse", "therapist",
	"labtech", "checktech", "chief", "other",
}

// dictionary 是权限点而不是导航模块：占 role_modules 一行（可在角色与模块里分配），
// 但不进入 HIS 前端导航（apps/his/src/nav.ts 仍只有 13 个导航模块）。
var hisModuleOrder = []string{
	"home", "register", "clinic", "billing", "pharmacy", "execute", "lab",
	"check", "inventory", "purchase", "patients", "stats", "admin", "dictionary",
}

var hisRoleSet = func() map[string]bool {
	set := make(map[string]bool, len(hisRoleOrder))
	for _, role := range hisRoleOrder {
		set[role] = true
	}
	return set
}()

var hisModuleSet = func() map[string]bool {
	set := make(map[string]bool, len(hisModuleOrder))
	for _, module := range hisModuleOrder {
		set[module] = true
	}
	return set
}()

// 角色模块配置的业务校验错误，统一映射为 400。
var (
	ErrInvalidRole      = errors.New("invalid his role")
	ErrAdminRoleLocked  = errors.New("admin role modules are locked")
	ErrInvalidModule    = errors.New("invalid his module")
	ErrEmptyRoleModules = errors.New("role modules must not be empty")
	ErrScheduleOccupied = errors.New("schedule has occupied slots")
)

// HISRoles 返回 9 个角色的副本（管理页展示用）。
func HISRoles() []string {
	out := make([]string, len(hisRoleOrder))
	copy(out, hisRoleOrder)
	return out
}

// HISModules 返回全部 14 个模块/权限点副本（13 导航模块 + dictionary 权限点）。
func HISModules() []string {
	out := make([]string, len(hisModuleOrder))
	copy(out, hisModuleOrder)
	return out
}

// normalizeRoleModules 校验并规范化「角色 → 模块」覆盖配置。
// 角色必须是 9 角色之一；module 必须是 14 个模块/权限点之一；去重后按模块目录顺序返回。
// admin 是超级角色，固定拥有全部模块，不允许通过配置修改。
// 空集合无法用当前表结构表达“显式无权限”（删除租户行会回退默认模板），因此拒绝。
func normalizeRoleModules(role string, modules []string) ([]string, error) {
	role = strings.TrimSpace(role)
	if !hisRoleSet[role] {
		return nil, ErrInvalidRole
	}
	if role == "admin" {
		return nil, ErrAdminRoleLocked
	}
	seen := make(map[string]bool, len(modules))
	for _, raw := range modules {
		module := strings.TrimSpace(raw)
		if !hisModuleSet[module] {
			return nil, ErrInvalidModule
		}
		seen[module] = true
	}
	if len(seen) == 0 {
		return nil, ErrEmptyRoleModules
	}
	out := make([]string, 0, len(seen))
	for _, module := range hisModuleOrder {
		if seen[module] {
			out = append(out, module)
		}
	}
	return out, nil
}

// canonicalModules 将模块集合按目录顺序输出（丢弃未知模块）。
func canonicalModules(modules []string) []string {
	seen := make(map[string]bool, len(modules))
	for _, module := range modules {
		if hisModuleSet[module] {
			seen[module] = true
		}
	}
	out := make([]string, 0, len(seen))
	for _, module := range hisModuleOrder {
		if seen[module] {
			out = append(out, module)
		}
	}
	return out
}

// buildRoleModuleConfig 合并默认模板与租户覆盖：租户对某角色存在任一配置行时整体覆盖默认，
// 否则回退 tenant_id=0 模板；admin 固定全量。返回值包含全部 9 个角色。
func buildRoleModuleConfig(defaults, tenant map[string][]string) map[string][]string {
	out := make(map[string][]string, len(hisRoleOrder))
	for _, role := range hisRoleOrder {
		switch {
		case role == "admin":
			out[role] = HISModules()
		case tenant[role] != nil:
			out[role] = canonicalModules(tenant[role])
		default:
			out[role] = canonicalModules(defaults[role])
		}
	}
	return out
}
