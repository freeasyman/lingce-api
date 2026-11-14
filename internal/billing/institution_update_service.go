package billing

// 运营端「编辑机构档案」服务层。
//
// 复用 internal/tenant 的 Store 更新 tenants 的基础档案列（名称/联系人/电话/邮箱/行业），
// 计费档案列（legal_name / contact_title / 省市区 / customer_type）在本模块单独更新；
// billing_mode、code 是内部/不可变字段，本方法不读也不写。
//
// 写入顺序：先校验完所有入参（含行业/科室/手机号），再写「计费档案列 + 科室」事务，
// 最后走 tenant.Store 更新基础列；两次写入之间没有业务失败点，避免校验失败留下半成品。

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/tenant"
)

// UpdateInstitution 编辑机构档案，返回与 GET /institutions/{id} 同结构的详情。
func (s *InstitutionService) UpdateInstitution(ctx context.Context, tenantID int64, req UpdateInstitutionRequest, adminID int64) (*Institution, error) {
	// 数据权限（越权 403）；机构不存在交给 GetInstitutionBase 返回 404。
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	before, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	req, err = req.normalize()
	if err != nil {
		return nil, err
	}

	// 省市区是「整体」：只改其中一个也要保证合并后三项齐全。
	province, city, district := before.Province, before.City, before.District
	if req.ProvinceCode != nil {
		province = *req.ProvinceCode
	}
	if req.CityCode != nil {
		city = *req.CityCode
	}
	if req.DistrictCode != nil {
		district = *req.DistrictCode
	}
	if province == "" || city == "" || district == "" {
		return nil, invalidf("请选择省 / 市 / 区")
	}

	// 行业必须在字典里（任务硬要求，返回 400）。
	if req.Industry != nil {
		if _, found, lookupErr := s.store.GetIndustryByName(ctx, *req.Industry); lookupErr != nil {
			return nil, lookupErr
		} else if !found {
			return nil, invalidf("行业不存在，请先在行业字典里新增")
		}
	}

	// 联系人手机号冲突：employees.phone 全局唯一，先给运营可读的 409。
	if req.ContactPhone != nil {
		owner, found, lookupErr := s.store.InstitutionPhoneOwnerExcludingTenant(ctx, *req.ContactPhone, tenantID)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if found {
			return nil, &ConflictError{
				Sentinel: ErrInstitutionPhoneConflict,
				Message:  fmt.Sprintf("该手机号已被「%s」使用", owner),
			}
		}
	}

	var beforeDepartmentIDs []int64
	summaryBefore, summaryAfter := institutionUpdateSummary(before, req, beforeDepartmentIDs)

	// 计费档案列：同一事务。
	if req.hasBillingProfileFields() {
		tx, beginErr := s.store.pool.Begin(ctx)
		if beginErr != nil {
			return nil, fmt.Errorf("begin institution update transaction: %w", beginErr)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if updateErr := s.store.UpdateInstitutionProfileFieldsTx(ctx, tx, tenantID, req); updateErr != nil {
			return nil, updateErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, fmt.Errorf("commit institution update transaction: %w", commitErr)
		}
	}

	// 基础档案列复用 tenant.Store（不碰 code / billing_mode / account_mode）。
	if req.hasBaseTenantFields() {
		update := tenant.UpdateTenantRequest{}
		if req.Name != nil {
			update.Name = req.Name
		}
		if req.ContactName != nil {
			update.ContactName = req.ContactName
		}
		if req.ContactPhone != nil {
			update.ContactPhone = req.ContactPhone
		}
		if req.ContactEmail != nil {
			update.ContactEmail = req.ContactEmail
		}
		if req.Industry != nil {
			update.Industry = req.Industry
		}
		if _, updateErr := s.tenantStore.UpdateTenant(ctx, tenantID, update); updateErr != nil {
			return nil, fmt.Errorf("update tenant profile: %w", updateErr)
		}
	}

	item, err := s.GetInstitution(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	// 空请求（没有任何可写字段）不产生变更日志。
	if len(summaryBefore) > 0 {
		s.recordInstitutionUpdateLog(ctx, tenantID, item.Name, adminID, summaryBefore, summaryAfter)
	}
	return item, nil
}

// institutionUpdateSummary 生成变更日志的前后摘要：只记录本次显式传入的字段。
func institutionUpdateSummary(before institutionBaseRow, req UpdateInstitutionRequest, beforeDepartmentIDs []int64) (map[string]any, map[string]any) {
	beforeSummary := make(map[string]any, 12)
	afterSummary := make(map[string]any, 12)
	add := func(key string, oldValue any, next any) {
		beforeSummary[key] = oldValue
		afterSummary[key] = next
	}
	if req.Name != nil {
		add("name", before.Name, *req.Name)
	}
	if req.LegalName != nil {
		add("legalName", before.Legal, *req.LegalName)
	}
	if req.Industry != nil {
		add("industry", before.Industry, *req.Industry)
	}
	if req.ContactName != nil {
		add("contactName", before.Contact, *req.ContactName)
	}
	if req.ContactTitle != nil {
		add("contactTitle", before.Title, *req.ContactTitle)
	}
	if req.ContactPhone != nil {
		add("contactPhone", before.Phone, *req.ContactPhone)
	}
	if req.ContactEmail != nil {
		add("contactEmail", before.Email, *req.ContactEmail)
	}
	if req.ProvinceCode != nil {
		add("provinceCode", before.Province, *req.ProvinceCode)
	}
	if req.CityCode != nil {
		add("cityCode", before.City, *req.CityCode)
	}
	if req.DistrictCode != nil {
		add("districtCode", before.District, *req.DistrictCode)
	}
	if req.CustomerType != nil {
		add("customerType", "", *req.CustomerType)
	}
	if req.DepartmentIDs != nil {
		add("departmentIds", beforeDepartmentIDs, *req.DepartmentIDs)
	}
	return beforeSummary, afterSummary
}

// recordInstitutionUpdateLog 写一条变更日志；失败只告警，不影响业务结果。
func (s *InstitutionService) recordInstitutionUpdateLog(ctx context.Context, tenantID int64, tenantName string, adminID int64, before, after map[string]any) {
	actorName, _ := s.store.GetOperationsAdminName(ctx, adminID)
	err := s.store.InsertSystemActionLog(ctx, institutionActionLog{
		TenantID:   tenantID,
		TenantName: tenantName,
		ActorID:    adminID,
		ActorName:  actorName,
		ActionCode: "institution.update",
		ActionName: "编辑机构档案",
		ObjectType: "institution",
		ObjectID:   strconv.FormatInt(tenantID, 10),
		ObjectName: tenantName,
		Before:     before,
		After:      after,
	})
	if err != nil {
		slog.Warn("record institution update log failed", "tenant_id", tenantID, "actor_id", adminID, "error", err)
	}
}
