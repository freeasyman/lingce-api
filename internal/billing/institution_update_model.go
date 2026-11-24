package billing

// 运营端「编辑机构档案」的数据契约（PUT /api/v1/ops/billing/institutions/{id}）。
//
// 请求体沿用前端 billingService.ts 的 camelCase；字段全部用指针区分
// 「未传（保持不变）」与「显式传空」。
// billing_mode / code 是内部字段与不可变字段，请求体故意不提供，服务层也不会写。

import (
	"regexp"
	"strings"
)

// institutionContactEmailPattern 是机构联系邮箱的最小校验：必须像 a@b.c。
// 与 createInstitution 只校验长度的口径相比略严格，但编辑入口是用户显式改值，
// 提前挡住明显错填。
var institutionContactEmailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// UpdateInstitutionRequest 是编辑机构档案的请求体。
type UpdateInstitutionRequest struct {
	Name          *string  `json:"name"`
	LegalName     *string  `json:"legalName"`
	Industry      *string  `json:"industry"`
	ContactName   *string  `json:"contactName"`
	ContactTitle  *string  `json:"contactTitle"`
	ContactPhone  *string  `json:"contactPhone"`
	ContactEmail  *string  `json:"contactEmail"`
	Address       *string  `json:"address"`
	ProvinceCode  *string  `json:"provinceCode"`
	CityCode      *string  `json:"cityCode"`
	DistrictCode  *string  `json:"districtCode"`
	DepartmentIDs *[]int64 `json:"departmentIds"`
	CustomerType  *string  `json:"customerType"`
}

// normalize 清洗并校验编辑入参；未传的字段保持 nil。
//
// 这里只做「不依赖数据库」的校验，行业/科室是否存在、手机号是否冲突、
// 省市区合并后是否完整在 service 层用真实数据校验。
func (r UpdateInstitutionRequest) normalize() (UpdateInstitutionRequest, error) {
	trim := func(value *string) *string {
		if value == nil {
			return nil
		}
		trimmed := strings.TrimSpace(*value)
		return &trimmed
	}
	r.Name = trim(r.Name)
	r.LegalName = trim(r.LegalName)
	r.Industry = trim(r.Industry)
	r.ContactName = trim(r.ContactName)
	r.ContactTitle = trim(r.ContactTitle)
	r.ContactPhone = trim(r.ContactPhone)
	r.ContactEmail = trim(r.ContactEmail)
	r.ProvinceCode = trim(r.ProvinceCode)
	r.CityCode = trim(r.CityCode)
	r.DistrictCode = trim(r.DistrictCode)

	if r.Name != nil {
		if *r.Name == "" {
			return UpdateInstitutionRequest{}, invalidf("请填机构简称")
		}
		if len([]rune(*r.Name)) > 255 {
			return UpdateInstitutionRequest{}, invalidf("机构简称过长")
		}
	}
	if r.LegalName != nil {
		if *r.LegalName == "" {
			return UpdateInstitutionRequest{}, invalidf("请填机构全称")
		}
		if len([]rune(*r.LegalName)) > 255 {
			return UpdateInstitutionRequest{}, invalidf("机构全称过长")
		}
	}
	if r.Industry != nil && *r.Industry == "" {
		return UpdateInstitutionRequest{}, invalidf("请选择行业")
	}
	if r.ContactName != nil {
		if *r.ContactName == "" {
			return UpdateInstitutionRequest{}, invalidf("请填联系人姓名")
		}
		if len([]rune(*r.ContactName)) > 100 {
			return UpdateInstitutionRequest{}, invalidf("联系人姓名过长")
		}
	}
	if r.ContactTitle != nil && len([]rune(*r.ContactTitle)) > 100 {
		return UpdateInstitutionRequest{}, invalidf("联系人职务过长")
	}
	if r.ContactPhone != nil {
		if !mobilePhonePattern.MatchString(*r.ContactPhone) {
			return UpdateInstitutionRequest{}, invalidf("手机号格式不对")
		}
	}
	if r.ContactEmail != nil {
		if len([]rune(*r.ContactEmail)) > 255 {
			return UpdateInstitutionRequest{}, invalidf("邮箱过长")
		}
		if *r.ContactEmail != "" && !institutionContactEmailPattern.MatchString(*r.ContactEmail) {
			return UpdateInstitutionRequest{}, invalidf("邮箱格式不对")
		}
	}
	for _, code := range []*string{r.ProvinceCode, r.CityCode, r.DistrictCode} {
		if code != nil && len([]rune(*code)) > 32 {
			return UpdateInstitutionRequest{}, invalidf("行政区划代码过长")
		}
	}
	if r.CustomerType != nil {
		customerType := normalizeEnum(*r.CustomerType)
		if customerType != CustomerTypeSingle && customerType != CustomerTypeGroup {
			return UpdateInstitutionRequest{}, invalidf("customerType must be single/group")
		}
		r.CustomerType = &customerType
	}
	if r.DepartmentIDs != nil {
		ids := make([]int64, 0, len(*r.DepartmentIDs))
		seen := make(map[int64]struct{}, len(*r.DepartmentIDs))
		for _, id := range *r.DepartmentIDs {
			if id <= 0 {
				return UpdateInstitutionRequest{}, invalidf("科室 ID 不合法")
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return UpdateInstitutionRequest{}, invalidf("请至少选一个科室")
		}
		if len(ids) > 50 {
			return UpdateInstitutionRequest{}, invalidf("涉及科室过多")
		}
		r.DepartmentIDs = &ids
	}
	return r, nil
}

// hasBaseTenantFields 判断是否要写 tenants 的基础档案列（复用 tenant.Store.UpdateTenant）。
func (r UpdateInstitutionRequest) hasBaseTenantFields() bool {
	return r.Name != nil || r.ContactName != nil || r.ContactPhone != nil ||
		r.ContactEmail != nil || r.Industry != nil
}

// hasBillingProfileFields 判断是否要写计费档案列（tenants 的 legal_name / contact_title /
// 省市区 / customer_type）。
func (r UpdateInstitutionRequest) hasBillingProfileFields() bool {
	return r.LegalName != nil || r.ContactTitle != nil || r.ProvinceCode != nil ||
		r.CityCode != nil || r.DistrictCode != nil || r.CustomerType != nil || r.Address != nil
}
