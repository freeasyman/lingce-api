package billing

// 机构档案、归属与服务期的服务层。
//
// 读模型（列表/详情）按 billingMock.ts 的 Institution 组装：
// 租户档案 + 当前归属 + 最近服务期 + 产品额度 + 应收/收款/消耗。
// 写操作只做「建档 / 归属 / 服务期」这三件与本轮 5 项能力相关的事，
// 开通、加量、赠送、续期、加产品、延长试用、收款在 institution_action_service.go / payment_service.go。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/internal/contract"
	"github.com/freeasyman/lingce-api/internal/tenant"
	"github.com/jackc/pgx/v5"
)

// InstitutionService 是运营端机构能力的服务入口。
type InstitutionService struct {
	service     *Service
	store       *Store
	tenantStore *tenant.Store
	now         func() time.Time
}

// NewInstitutionService 装配机构服务：复用计价/订单号生成（service）与同一套 store。
func NewInstitutionService(service *Service, store *Store, tenantStore *tenant.Store) *InstitutionService {
	return &InstitutionService{service: service, store: store, tenantStore: tenantStore, now: time.Now}
}

// clock 返回可替换的当前时间（单测固定时间）。
func (s *InstitutionService) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// today 返回服务端今天的日历日期（UTC 零点）。
func (s *InstitutionService) today() time.Time {
	return dateOnly(s.clock())
}

// ---------------------------------------------------------------------------
// 读取：列表 / 详情
// ---------------------------------------------------------------------------

// ListInstitutions 返回运营可见的机构列表。
// adminID 的数据权限在 SQL 层收敛：平台账号可看全部，渠道账号只看自己组织名下的机构。
func (s *InstitutionService) ListInstitutions(ctx context.Context, query InstitutionQuery, adminID int64) (*InstitutionListResult, error) {
	scope, err := s.store.GetAdminScope(ctx, adminID)
	if err != nil {
		return nil, err
	}
	query, err = normalizeInstitutionQuery(query)
	if err != nil {
		return nil, err
	}
	if !scope.CanSeeAll {
		if scope.OrgID == nil {
			return nil, ErrAdminScopeMissing
		}
		query.OrgID = *scope.OrgID
	}

	total, err := s.store.CountInstitutions(ctx, query)
	if err != nil {
		return nil, err
	}
	bases, err := s.store.ListInstitutionBases(ctx, query)
	if err != nil {
		return nil, err
	}
	tenantIDs := make([]int64, 0, len(bases))
	for _, base := range bases {
		tenantIDs = append(tenantIDs, base.ID)
	}
	entitlementRows, err := s.store.ListInstitutionEntitlementRows(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}
	entitlementsByTenant := groupEntitlementRows(entitlementRows)
	today := s.today()

	items := make([]Institution, 0, len(bases))
	for _, base := range bases {
		items = append(items, buildInstitution(base, nil, entitlementsByTenant[base.ID], today))
	}
	return &InstitutionListResult{Items: items, Total: total, IsMock: false}, nil
}

// GetInstitution 返回机构详情（不做数据权限校验，内部调用用）。
func (s *InstitutionService) GetInstitution(ctx context.Context, tenantID int64) (*Institution, error) {
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	// 6 个只读查询彼此独立，并发执行：远程库每次往返约 70-90ms，
	// 串行会让详情页累计到 1s 以上；并发后约为最慢一条的耗时。
	var (
		entitlementRows                                []entitlementRow
		admins                                         []institutionAdminRow
		receivables                                    []InstitutionReceivable
		payLogs                                        []InstitutionPayLog
		consumptions                                   []InstitutionConsumption
		documents                                      []Document
		errEnt, errAdm, errRec, errPay, errCon, errDoc error
		wg                                             sync.WaitGroup
	)
	wg.Add(6)
	go func() {
		defer wg.Done()
		entitlementRows, errEnt = s.store.ListInstitutionEntitlementRows(ctx, []int64{tenantID})
	}()
	go func() { defer wg.Done(); admins, errAdm = s.store.ListInstitutionAdminRows(ctx, tenantID) }()
	go func() { defer wg.Done(); receivables, errRec = s.store.ListInstitutionReceivables(ctx, tenantID) }()
	go func() { defer wg.Done(); payLogs, errPay = s.store.ListInstitutionPayLogs(ctx, tenantID) }()
	go func() {
		defer wg.Done()
		consumptions, errCon = s.store.ListInstitutionConsumptions(ctx, tenantID, 100)
	}()
	go func() { defer wg.Done(); documents, errDoc = s.store.ListDocumentsByTenant(ctx, tenantID) }()
	wg.Wait()
	if err := errors.Join(errEnt, errAdm, errRec, errPay, errCon, errDoc); err != nil {
		return nil, err
	}

	item := buildInstitution(base, nil, groupEntitlementRows(entitlementRows)[tenantID], s.today())
	if strings.TrimSpace(base.ContractsJSON) != "" {
		if err := json.Unmarshal([]byte(base.ContractsJSON), &item.Contracts); err != nil {
			return nil, fmt.Errorf("decode institution contracts: %w", err)
		}
	}
	item.Documents = make([]InstitutionDocument, 0, len(documents))
	for _, doc := range documents {
		item.Documents = append(item.Documents, institutionDocumentOf(doc))
	}
	item.Admins = institutionAdminsFromRows(s.ensureInstitutionPrimaryAdmin(ctx, tenantID, admins))
	item.Receivables = receivables
	item.PayLogs = payLogs
	item.Consumptions = consumptions
	return &item, nil
}

// GetInstitutionForAdmin 返回机构详情，并校验运营账号的数据权限。
func (s *InstitutionService) GetInstitutionForAdmin(ctx context.Context, tenantID, adminID int64) (*Institution, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	return s.GetInstitution(ctx, tenantID)
}

// requireTenantAccess 校验运营账号能操作该租户：
// 平台/超管账号放行；其余账号要求该租户当前归属（或历史试用归属）在其组织下。
func (s *InstitutionService) requireTenantAccess(ctx context.Context, tenantID, adminID int64) error {
	if tenantID <= 0 {
		return ErrInstitutionNotFound
	}
	scope, err := s.store.GetAdminScope(ctx, adminID)
	if err != nil {
		return err
	}
	if scope.CanSeeAll {
		return nil
	}
	if scope.OrgID == nil || *scope.OrgID <= 0 {
		return ErrAdminScopeMissing
	}
	visible, err := s.store.TenantVisibleToOrg(ctx, tenantID, *scope.OrgID)
	if err != nil {
		return err
	}
	if !visible {
		return ErrNoPermission
	}
	return nil
}

// buildInstitution 把基础行 + 科室 + 额度组装成前端契约的 Institution。
func buildInstitution(base institutionBaseRow, departments []string, entitlements []entitlementRow, today time.Time) Institution {
	// 模式由主体合同类型推导（两套分开后 account_mode 不再随新产品动作变化）。
	mode := AccountModeFormal
	if normalizeEnum(base.PrimaryType) == string(contract.Trial) {
		mode = AccountModeTrial
	}

	start, end := "", ""
	if base.PeriodID != nil {
		if base.StartedOn != nil {
			start = *base.StartedOn
		}
		if base.ExpiredOn != nil {
			end = *base.ExpiredOn
		}
	}
	status := institutionStatus(base, today)

	item := Institution{
		ID:          base.ID,
		Name:        base.Name,
		Legal:       base.Legal,
		Code:        base.Code,
		Industry:    base.Industry,
		Departments: departments,
		Region: InstitutionRegion{
			Province: base.Province,
			City:     base.City,
			District: base.District,
		},
		Contact: base.Contact,
		Title:   base.Title,
		Phone:   base.Phone,
		Email:   base.Email,
		Address: base.Address,
		Channel: base.OrgName,
		Sales:   base.SalesName,
		Start:   start,
		End:     end,
		Mode:    mode,
		// 历史试用次数（含当前），列表/详情展示「已试用 N 次」。
		TrialCount: base.TrialCount,
		// 暂停仍然算「已开通」：服务期未结束，只是扣量与登录闸门被临时关上。
		Open:         status != InstitutionStatusUnopened,
		Status:       status,
		Products:     buildInstitutionProducts(entitlements, mode),
		Admins:       []InstitutionAdmin{},
		Receivables:  []InstitutionReceivable{},
		PayLogs:      []InstitutionPayLog{},
		Consumptions: []InstitutionConsumption{},
		Logs:         []InstitutionLog{},
		Documents:    []InstitutionDocument{},
	}
	if base.PausedAt != nil {
		item.PausedAt = base.PausedAt.Format("2006-01-02 15:04")
	}
	if base.PausedAt != nil {
		item.PausedReason = base.PausedReason
	}
	if item.Departments == nil {
		item.Departments = []string{}
	}
	return item
}

// institutionStatus 推导机构服务状态，优先级：
// 已暂停 > 未开通 > 已过期 > 试用中 > 正式。
//
// 唯一口径：主体合同行（formal/trial）是否覆盖今天。体验行不参与状态推导。
// 「已过期」覆盖两类：有 active 主体行但到期日已过，或仅有 expired 审计行。
func institutionStatus(base institutionBaseRow, today time.Time) string {
	// 暂停优先于到期：暂停期间即使跨过到期日，状态徽标仍显示「已暂停」，
	// 扣量闸门返回 SUBSCRIPTION_PAUSED；恢复后自然落到「已到期」。
	if base.PausedAt != nil {
		return InstitutionStatusPaused
	}
	primary := primaryCoveringType(base, today)
	if primary == "" {
		if base.PeriodID != nil && (normalizeEnum(base.PeriodStatus) == PeriodStatusActive || normalizeEnum(base.PeriodStatus) == PeriodStatusExpired) {
			return InstitutionStatusExpired
		}
		return InstitutionStatusUnopened
	}
	if primary == string(contract.Trial) {
		return InstitutionStatusTrial
	}
	return InstitutionStatusActive
}

// primaryCoveringType 返回覆盖今天的主体合同类型（formal/trial）；没有则返回 ""。
// 覆盖 = 行 status 为 active 且 [started_on, expired_on] 包含今天。
func primaryCoveringType(base institutionBaseRow, today time.Time) string {
	if base.PrimaryType == "" || normalizeEnum(base.PrimaryStatus) != PeriodStatusActive ||
		base.PrimaryStartedOn == nil || base.PrimaryExpiredOn == nil {
		return ""
	}
	start, startErr := parseDateOnly(*base.PrimaryStartedOn)
	end, endErr := parseDateOnly(*base.PrimaryExpiredOn)
	if startErr != nil || endErr != nil || start.After(today) || end.Before(today) {
		return ""
	}
	return normalizeEnum(base.PrimaryType)
}

// groupEntitlementRows 按租户分组额度行。
func groupEntitlementRows(rows []entitlementRow) map[int64][]entitlementRow {
	result := make(map[int64][]entitlementRow, len(rows))
	for _, row := range rows {
		result[row.TenantID] = append(result[row.TenantID], row)
	}
	return result
}

// buildInstitutionProducts 把额度行组装成 产品 -> 单位 -> {granted, used}。
//
// 正式机构展示 official 额度，试用机构展示 trial 额度；同一 (产品, 单位) 多档位/多行时相加。
// 试用转正式后库里的 trial 行仍在（到期清零），这里按 mode 过滤，避免页面同时显示两套额度。
func buildInstitutionProducts(rows []entitlementRow, mode string) map[string]map[string]InstitutionQuota {
	products := make(map[string]map[string]InstitutionQuota, 8)
	preferred := QuotaTypeOfficial
	if mode == AccountModeTrial {
		preferred = QuotaTypeTrial
	}
	add := func(quotaType string) {
		for _, row := range rows {
			if normalizeEnum(row.QuotaType) != quotaType {
				continue
			}
			productCode := normalizeEnum(row.ProductCode)
			unit := normalizeEnum(row.Unit)
			if productCode == "" || unit == "" {
				continue
			}
			if products[productCode] == nil {
				products[productCode] = make(map[string]InstitutionQuota, 4)
			}
			granted := row.Granted
			used := row.Used
			balance := row.Balance
			if existing, ok := products[productCode][unit]; ok {
				granted = sumDecimal(existing.Granted, granted)
				used = sumDecimal(existing.Used, used)
				balance = sumDecimal(existing.Balance, balance)
			}
			products[productCode][unit] = InstitutionQuota{Granted: granted, Used: used, Balance: balance}
		}
	}
	add(preferred)
	if len(products) == 0 {
		if preferred == QuotaTypeTrial {
			add(QuotaTypeOfficial)
		} else {
			add(QuotaTypeTrial)
		}
	}
	return products
}

// sumDecimal 把两个十进制文本相加（额度只有两位小数，用分做整数运算）。
func sumDecimal(left, right Decimal) Decimal {
	leftCents, err := parseAmountCents(left)
	if err != nil {
		leftCents = 0
	}
	rightCents, err := parseAmountCents(right)
	if err != nil {
		rightCents = 0
	}
	return centsToDecimal(leftCents + rightCents)
}

// ---------------------------------------------------------------------------
// 建档
// ---------------------------------------------------------------------------

// CreateInstitution 建档：建租户 + 主管理员 + 档案列 + 涉及科室 + 归属。
//
// 档案本身不涉及产品与钱；渠道/销售按名称解析到 ops_organizations / operations_admins。
// 联系人手机号全局唯一（employees.phone 唯一索引），冲突返回 ErrInstitutionPhoneConflict。
func (s *InstitutionService) CreateInstitution(ctx context.Context, req CreateInstitutionRequest, adminID int64) (*Institution, error) {
	req, err := req.normalize()
	if err != nil {
		return nil, err
	}
	if err := validateInstitutionCode(req.Code); err != nil {
		return nil, err
	}

	// 数据权限：渠道账号只能给本组织建档；平台账号可给任意渠道建档。
	scope, err := s.store.GetAdminScope(ctx, adminID)
	if err != nil {
		return nil, err
	}
	channel, found, err := s.store.GetChannelByName(ctx, req.Channel)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, invalidf("渠道不存在或已停用")
	}
	if !scope.CanSeeAll {
		if scope.OrgID == nil || *scope.OrgID != channel.ID {
			return nil, ErrNoPermission
		}
	}
	sales, found, err := s.store.GetSalesByNameAndOrg(ctx, channel.ID, req.Sales)
	if err != nil {
		return nil, err
	}
	if !found || !sales.Active {
		return nil, invalidf("销售不存在或已停用")
	}

	if _, found, err := s.store.GetIndustryByName(ctx, req.Industry); err != nil {
		return nil, err
	} else if !found {
		return nil, invalidf("行业不存在，请先在行业字典里新增")
	}
	// 手机号全局唯一：先给运营一个能看懂的冲突提示。
	if owner, found, err := s.store.InstitutionPhoneOwner(ctx, req.Phone); err != nil {
		return nil, err
	} else if found {
		return nil, &ConflictError{Sentinel: ErrInstitutionPhoneConflict, Message: fmt.Sprintf("该手机号已被「%s」使用", owner)}
	}

	code, err := s.resolveInstitutionCode(ctx, req.Name, req.Code)
	if err != nil {
		return nil, err
	}

	accountMode := AccountModeFormal
	contactName, contactPhone, contactEmail := req.Contact, req.Phone, req.Email
	createReq := tenant.CreateTenantRequest{
		Name:         req.Name,
		Code:         code,
		AccountMode:  &accountMode,
		ContactName:  &contactName,
		ContactPhone: &contactPhone,
		ContactEmail: &contactEmail,
		Industry:     &req.Industry,
		Address:      &req.Address,
	}
	created, err := s.tenantStore.CreateTenant(ctx, createReq)
	if err != nil {
		if isUniqueViolation(err) {
			// employees.phone 全局唯一：并发建档同一手机号时兜底成 409。
			if owner, found, lookupErr := s.store.InstitutionPhoneOwner(ctx, req.Phone); lookupErr == nil && found {
				return nil, &ConflictError{Sentinel: ErrInstitutionPhoneConflict, Message: fmt.Sprintf("该手机号已被「%s」使用", owner)}
			}
			return nil, ErrInstitutionCodeConflict
		}
		return nil, fmt.Errorf("create tenant: %w", err)
	}

	// 档案列 + 涉及科室 + 当前归属：同一事务，避免留下「有租户没有归属」的半成品。
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin institution profile transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.store.UpdateInstitutionProfileTx(ctx, tx, created.ID, req); err != nil {
		return nil, err
	}
	// 建档时默认主管理员：tenant.Store.CreateTenant 已经在 employees 建了管理员，
	// 这里把它标成 is_primary，保证「每个机构至少一个启用主管理员」从第一天起成立。
	if err := s.store.EnsurePrimaryInstitutionAdminTx(ctx, tx, created.ID); err != nil {
		return nil, err
	}
	if _, err := s.store.InsertOwnershipTx(ctx, tx, created.ID, channel.ID, sales.ID, adminID, "manual", s.today()); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit institution profile transaction: %w", err)
	}

	return s.GetInstitution(ctx, created.ID)
}

// resolveInstitutionCode 决定最终代码：手工代码可用则沿用（大小写不敏感判重），
// 否则按 LC-<拼音>-<流水> 生成（同前缀独立递增）。
func (s *InstitutionService) resolveInstitutionCode(ctx context.Context, name, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		exists, err := s.store.InstitutionCodeExists(ctx, requested)
		if err != nil {
			return "", err
		}
		if !exists {
			return requested, nil
		}
	}
	prefix := "LC-" + institutionPinyinCode(name) + "-"
	existing, err := s.store.ListInstitutionCodes(ctx, prefix)
	if err != nil {
		return "", err
	}
	taken := make(map[string]struct{}, len(existing))
	for _, code := range existing {
		taken[strings.ToUpper(strings.TrimSpace(code))] = struct{}{}
	}
	for attempt := 0; attempt < 100; attempt++ {
		candidate := nextInstitutionCode(name, existing)
		if _, exists := taken[strings.ToUpper(candidate)]; !exists {
			return candidate, nil
		}
		// 极端并发：把候选代码当成已占用再取下一个流水号。
		existing = append(existing, candidate)
	}
	return "", ErrInstitutionCodeConflict
}

// ---------------------------------------------------------------------------
// 归属 CRUD
// ---------------------------------------------------------------------------

// ListOwnerships 返回机构归属历史。
func (s *InstitutionService) ListOwnerships(ctx context.Context, tenantID, adminID int64) ([]Ownership, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetInstitutionBase(ctx, tenantID); err != nil {
		return nil, err
	}
	rows, err := s.store.ListOwnershipRows(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	items := make([]Ownership, 0, len(rows))
	for _, row := range rows {
		items = append(items, ownershipFromRow(row))
	}
	return items, nil
}

// CreateOwnership 新增当前归属：先关闭旧的当前归属，再插入新行（同一事务）。
func (s *InstitutionService) CreateOwnership(ctx context.Context, tenantID int64, payload OwnershipPayload, adminID int64) (Ownership, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return Ownership{}, err
	}
	if _, err := s.store.GetInstitutionBase(ctx, tenantID); err != nil {
		return Ownership{}, err
	}
	orgID, salesAdminID, source, effectiveFrom, err := s.normalizeOwnershipPayload(ctx, payload)
	if err != nil {
		return Ownership{}, err
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return Ownership{}, fmt.Errorf("begin ownership transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.store.CloseCurrentOwnershipTx(ctx, tx, tenantID, effectiveFrom); err != nil {
		return Ownership{}, err
	}
	ownershipID, err := s.store.InsertOwnershipTx(ctx, tx, tenantID, orgID, salesAdminID, adminID, source, effectiveFrom)
	if err != nil {
		return Ownership{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Ownership{}, fmt.Errorf("commit ownership transaction: %w", err)
	}
	return s.getOwnership(ctx, tenantID, ownershipID)
}

// normalizeOwnershipPayload 校验归属入参并解析组织/销售关系。
func (s *InstitutionService) normalizeOwnershipPayload(ctx context.Context, payload OwnershipPayload) (int64, int64, string, time.Time, error) {
	orgID := payload.OrgID
	if orgID == 0 {
		orgID = payload.SnakeOrgID
	}
	salesAdminID := payload.SalesAdminID
	if salesAdminID == 0 {
		salesAdminID = payload.SnakeSalesAdminID
	}
	source := normalizeEnum(payload.Source)
	if source == "" {
		source = "manual"
	}
	switch source {
	case "manual", "trial", "legacy_trial", "migration":
	default:
		return 0, 0, "", time.Time{}, invalidf("source must be manual/trial/legacy_trial/migration")
	}
	if orgID <= 0 {
		return 0, 0, "", time.Time{}, invalidf("请选择渠道")
	}
	salesAdminID = maxInt64(salesAdminID, 0)
	if salesAdminID <= 0 {
		return 0, 0, "", time.Time{}, invalidf("请选择销售")
	}

	channel, err := s.store.GetChannelRow(ctx, orgID)
	if err != nil {
		if errors.Is(err, ErrPeerNotFound) {
			return 0, 0, "", time.Time{}, invalidf("渠道不存在")
		}
		return 0, 0, "", time.Time{}, err
	}
	sales, err := s.store.GetSalesRow(ctx, salesAdminID)
	if err != nil {
		if errors.Is(err, ErrPeerNotFound) {
			return 0, 0, "", time.Time{}, invalidf("销售不存在")
		}
		return 0, 0, "", time.Time{}, err
	}
	if sales.OrgID > 0 && sales.OrgID != channel.ID {
		return 0, 0, "", time.Time{}, invalidf("销售不属于该渠道")
	}
	if !sales.Active {
		return 0, 0, "", time.Time{}, invalidf("销售已停用")
	}

	effectiveFrom := s.today()
	rawFrom := strings.TrimSpace(payload.EffectiveFrom)
	if rawFrom == "" {
		rawFrom = strings.TrimSpace(payload.SnakeEffectiveFrom)
	}
	if rawFrom != "" {
		parsed, err := parseDateOnly(rawFrom)
		if err != nil {
			return 0, 0, "", time.Time{}, invalidf("生效日必须是 YYYY-MM-DD")
		}
		effectiveFrom = parsed
	}
	if effectiveFrom.After(s.today()) {
		return 0, 0, "", time.Time{}, invalidf("生效日不能晚于今天")
	}
	return orgID, salesAdminID, source, effectiveFrom, nil
}

// UpdateOwnership 修改归属的渠道/销售/来源/生效日。
func (s *InstitutionService) UpdateOwnership(ctx context.Context, tenantID, ownershipID int64, payload OwnershipPayload, adminID int64) (Ownership, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return Ownership{}, err
	}
	if _, err := s.store.GetOwnershipRow(ctx, tenantID, ownershipID); err != nil {
		return Ownership{}, err
	}
	orgID, salesAdminID, source, effectiveFrom, err := s.normalizeOwnershipPayload(ctx, payload)
	if err != nil {
		return Ownership{}, err
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return Ownership{}, fmt.Errorf("begin ownership transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.store.UpdateOwnershipTx(ctx, tx, tenantID, ownershipID, orgID, salesAdminID, source); err != nil {
		return Ownership{}, err
	}
	if err := s.store.UpdateOwnershipEffectiveFromTx(ctx, tx, tenantID, ownershipID, effectiveFrom); err != nil {
		return Ownership{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Ownership{}, fmt.Errorf("commit ownership transaction: %w", err)
	}
	return s.getOwnership(ctx, tenantID, ownershipID)
}

// CloseOwnership 关闭一条归属（effective_to = 指定日期或今天）。
func (s *InstitutionService) CloseOwnership(ctx context.Context, tenantID, ownershipID int64, payload OwnershipClosePayload, adminID int64) (Ownership, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return Ownership{}, err
	}
	existing, err := s.store.GetOwnershipRow(ctx, tenantID, ownershipID)
	if err != nil {
		return Ownership{}, err
	}
	closeDate := s.today()
	if raw := strings.TrimSpace(payload.EffectiveTo); raw != "" {
		parsed, err := parseDateOnly(raw)
		if err != nil {
			return Ownership{}, invalidf("关闭日必须是 YYYY-MM-DD")
		}
		closeDate = parsed
	}
	effectiveFrom, err := parseDateOnly(existing.EffectiveFrom)
	if err != nil {
		return Ownership{}, fmt.Errorf("existing ownership effective_from is invalid: %w", err)
	}
	if closeDate.Before(effectiveFrom) {
		return Ownership{}, invalidf("关闭日不能早于生效日")
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return Ownership{}, fmt.Errorf("begin ownership transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.store.CloseOwnershipRowTx(ctx, tx, tenantID, ownershipID, closeDate); err != nil {
		return Ownership{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Ownership{}, fmt.Errorf("commit ownership transaction: %w", err)
	}
	return s.getOwnership(ctx, tenantID, ownershipID)
}

func (s *InstitutionService) getOwnership(ctx context.Context, tenantID, ownershipID int64) (Ownership, error) {
	row, err := s.store.GetOwnershipRow(ctx, tenantID, ownershipID)
	if err != nil {
		return Ownership{}, err
	}
	return ownershipFromRow(row), nil
}

// ownershipFromRow 把 store 行转成前端 DTO（effective_to 空串表示当前有效）。
func ownershipFromRow(row ownershipRow) Ownership {
	item := Ownership{
		ID:            row.ID,
		TenantID:      row.TenantID,
		Source:        row.Source,
		EffectiveFrom: row.EffectiveFrom,
		CreatedAt:     row.CreatedAt.Format("2006-01-02 15:04"),
	}
	if row.OrgID != nil {
		item.OrgID = *row.OrgID
	}
	item.OrgName = row.OrgName
	if row.SalesAdminID != nil {
		item.SalesAdminID = *row.SalesAdminID
	}
	item.SalesName = row.SalesName
	if row.EffectiveTo != nil {
		item.EffectiveTo = *row.EffectiveTo
	}
	return item
}

// ---------------------------------------------------------------------------
// 服务期 CRUD
// ---------------------------------------------------------------------------

// ListSubscriptionPeriods 返回机构服务期历史。
func (s *InstitutionService) ListSubscriptionPeriods(ctx context.Context, tenantID, adminID int64) ([]SubscriptionPeriod, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetInstitutionBase(ctx, tenantID); err != nil {
		return nil, err
	}
	rows, err := s.store.ListPeriodRows(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	items := make([]SubscriptionPeriod, 0, len(rows))
	for _, row := range rows {
		items = append(items, subscriptionPeriodFromRow(row))
	}
	return items, nil
}

// CreateSubscriptionPeriod 手工补录/新增一条服务期。
// status=active 时会先终止现有 active（保持「一个租户只有一条 active」），并同步租户有效期。
func (s *InstitutionService) CreateSubscriptionPeriod(ctx context.Context, tenantID int64, payload SubscriptionPeriodPayload, adminID int64) (SubscriptionPeriod, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return SubscriptionPeriod{}, err
	}
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return SubscriptionPeriod{}, err
	}
	startedOn, expiredOn, status, source, err := normalizeSubscriptionPeriodPayload(payload, s.today())
	if err != nil {
		return SubscriptionPeriod{}, err
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return SubscriptionPeriod{}, fmt.Errorf("begin subscription period transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if status == PeriodStatusActive {
		if err := s.store.CloseActivePeriodTx(ctx, tx, tenantID, PeriodStatusTerminated); err != nil {
			return SubscriptionPeriod{}, err
		}
	}
	periodID, err := s.store.InsertManualPeriodTx(ctx, tx, tenantID, startedOn, expiredOn, status, source)
	if err != nil {
		return SubscriptionPeriod{}, err
	}
	if status == PeriodStatusActive {
		accountMode := AccountModeFormal
		if normalizeEnum(base.PrimaryType) == string(contract.Trial) || source == PeriodSourceTrial {
			accountMode = AccountModeTrial
		}
		if err := s.ensurePlanAndFeatureTx(ctx, tx, tenantID, accountMode); err != nil {
			return SubscriptionPeriod{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return SubscriptionPeriod{}, fmt.Errorf("commit subscription period transaction: %w", err)
	}
	row, err := s.store.GetPeriodRow(ctx, tenantID, periodID)
	if err != nil {
		return SubscriptionPeriod{}, err
	}
	return subscriptionPeriodFromRow(row), nil
}

// UpdateSubscriptionPeriod 调整服务期日期/状态（开错能改）。
func (s *InstitutionService) UpdateSubscriptionPeriod(ctx context.Context, tenantID, periodID int64, payload SubscriptionPeriodPayload, adminID int64) (SubscriptionPeriod, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return SubscriptionPeriod{}, err
	}
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return SubscriptionPeriod{}, err
	}
	existing, err := s.store.GetPeriodRow(ctx, tenantID, periodID)
	if err != nil {
		return SubscriptionPeriod{}, err
	}

	merged := SubscriptionPeriodPayload{
		StartedOn: existing.StartedOn,
		ExpiredOn: existing.ExpiredOn,
		Status:    existing.Status,
		Source:    existing.Source,
	}
	if strings.TrimSpace(payload.StartedOn) != "" {
		merged.StartedOn = payload.StartedOn
	}
	if strings.TrimSpace(payload.SnakeStartedOn) != "" {
		merged.StartedOn = payload.SnakeStartedOn
	}
	if strings.TrimSpace(payload.ExpiredOn) != "" {
		merged.ExpiredOn = payload.ExpiredOn
	}
	if strings.TrimSpace(payload.SnakeExpiredOn) != "" {
		merged.ExpiredOn = payload.SnakeExpiredOn
	}
	if strings.TrimSpace(payload.Status) != "" {
		merged.Status = payload.Status
	}
	if strings.TrimSpace(payload.Source) != "" {
		merged.Source = payload.Source
	}
	startedOn, expiredOn, status, source, err := normalizeSubscriptionPeriodPayload(merged, s.today())
	if err != nil {
		return SubscriptionPeriod{}, err
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return SubscriptionPeriod{}, fmt.Errorf("begin subscription period transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if status == PeriodStatusActive {
		// 先把其它 active 关掉（不含当前行），再更新当前行。
		if _, err := tx.Exec(ctx, `
UPDATE subscription_periods
SET status = $3, updated_at = NOW()
WHERE tenant_id = $1 AND id <> $2 AND status = 'active'`, tenantID, periodID, PeriodStatusTerminated); err != nil {
			return SubscriptionPeriod{}, fmt.Errorf("close other active subscription periods: %w", err)
		}
	}
	if err := s.store.UpdatePeriodRowTx(ctx, tx, tenantID, periodID, startedOn, expiredOn, status, source); err != nil {
		return SubscriptionPeriod{}, err
	}
	if status == PeriodStatusActive {
		accountMode := AccountModeFormal
		if normalizeEnum(base.PrimaryType) == string(contract.Trial) || source == PeriodSourceTrial {
			accountMode = AccountModeTrial
		}
		if err := s.ensurePlanAndFeatureTx(ctx, tx, tenantID, accountMode); err != nil {
			return SubscriptionPeriod{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return SubscriptionPeriod{}, fmt.Errorf("commit subscription period transaction: %w", err)
	}
	row, err := s.store.GetPeriodRow(ctx, tenantID, periodID)
	if err != nil {
		return SubscriptionPeriod{}, err
	}
	return subscriptionPeriodFromRow(row), nil
}

// normalizeSubscriptionPeriodPayload 校验服务期入参。
func normalizeSubscriptionPeriodPayload(payload SubscriptionPeriodPayload, today time.Time) (time.Time, time.Time, string, string, error) {
	rawStart := strings.TrimSpace(payload.StartedOn)
	if rawStart == "" {
		rawStart = strings.TrimSpace(payload.SnakeStartedOn)
	}
	rawEnd := strings.TrimSpace(payload.ExpiredOn)
	if rawEnd == "" {
		rawEnd = strings.TrimSpace(payload.SnakeExpiredOn)
	}
	if rawStart == "" {
		rawStart = formatDateOnly(today)
	}
	if rawEnd == "" {
		return time.Time{}, time.Time{}, "", "", invalidf("请填到期日")
	}
	startedOn, err := parseDateOnly(rawStart)
	if err != nil {
		return time.Time{}, time.Time{}, "", "", invalidf("开始日必须是 YYYY-MM-DD")
	}
	expiredOn, err := parseDateOnly(rawEnd)
	if err != nil {
		return time.Time{}, time.Time{}, "", "", invalidf("到期日必须是 YYYY-MM-DD")
	}
	if expiredOn.Before(startedOn) {
		return time.Time{}, time.Time{}, "", "", ErrPeriodWindowInvalid
	}
	status := normalizeEnum(payload.Status)
	if status == "" {
		status = PeriodStatusActive
	}
	switch status {
	case PeriodStatusActive, PeriodStatusPending, PeriodStatusExpired, PeriodStatusTerminated:
	default:
		return time.Time{}, time.Time{}, "", "", invalidf("status must be active/pending/expired/terminated")
	}
	source := normalizeEnum(payload.Source)
	if source == "" {
		source = PeriodSourceManual
	}
	if len(source) > 32 {
		return time.Time{}, time.Time{}, "", "", invalidf("source 最多 32 个字符")
	}
	return startedOn, expiredOn, status, source, nil
}

// ensurePlanAndFeatureTx 保证订阅套餐与功能组分配存在（新产品菜单鉴权依赖）。
// 两套分开后，新产品动作只写合同表 + 额度 + 功能组，不再写 tenants / tenant_subscriptions。
func (s *InstitutionService) ensurePlanAndFeatureTx(ctx context.Context, tx pgx.Tx, tenantID int64, accountMode string) error {
	planCode, planName, planDays := "standard", "标准版", 365
	if accountMode == AccountModeTrial {
		planCode, planName, planDays = "trial", "试用版", 14
	}
	planID, err := s.store.EnsureSubscriptionPlanTx(ctx, tx, planCode, planName, planDays)
	if err != nil {
		return err
	}
	return s.store.EnsureTenantFeatureAssignmentTx(ctx, tx, tenantID, planID)
}

// subscriptionPeriodFromRow 转前端 DTO。
func subscriptionPeriodFromRow(row periodRow) SubscriptionPeriod {
	return SubscriptionPeriod{
		ID:        row.ID,
		TenantID:  row.TenantID,
		StartedOn: row.StartedOn,
		ExpiredOn: row.ExpiredOn,
		Status:    row.Status,
		Source:    row.Source,
		CreatedAt: row.CreatedAt.Format("2006-01-02 15:04"),
		UpdatedAt: row.UpdatedAt.Format("2006-01-02 15:04"),
	}
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

// sortedProductCodes 返回排序后的产品代码（稳定输出用）。
func sortedProductCodes(codes []string) []string {
	result := append([]string(nil), codes...)
	sort.Strings(result)
	return result
}

// ListActionLogs 返回机构变更记录（system_action_logs，按数据权限校验）。
func (s *InstitutionService) ListActionLogs(ctx context.Context, tenantID, adminID int64) ([]InstitutionActionLog, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	items, err := s.store.ListSystemActionLogs(ctx, tenantID, 200)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []InstitutionActionLog{}
	}
	return items, nil
}
