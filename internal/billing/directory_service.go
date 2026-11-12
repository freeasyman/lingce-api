package billing

// 产品与价格、渠道与销售、行业与科室字典的服务层。
//
// 渠道与销售复用运营组织/运营账号；渠道类型在库里的映射：
//   platform -> 灵策自营，agency -> 渠道商，agent -> 个人代理。
// 新增/修改渠道、销售、字典属于平台配置，只允许平台账号（或超管）操作。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// displayProductOrder 是产品与价格页的展示顺序（与 billingMock.ts 一致）。
var displayProductOrder = []string{"yingke", "kehu", "suifang", "tingxie", "hegui", "huoke"}

// GetPricing 返回六产品标准档价目与四个固定套餐。
func (s *InstitutionService) GetPricing(ctx context.Context) (*PricingPayload, error) {
	items, err := s.service.repo.ListCatalog(ctx, CatalogFilter{})
	if err != nil {
		return nil, fmt.Errorf("load product catalog: %w", err)
	}
	latest := effectiveCatalogVersions(items, s.today())
	products := make(map[string]ProductDefinition, len(latest))
	for _, item := range latest {
		if normalizeEnum(item.Edition) != EditionStandard {
			continue
		}
		units := make(map[string]Decimal, 4)
		pairs, err := parseQuotaMap(item.Quotas)
		if err != nil {
			return nil, fmt.Errorf("catalog %s quotas: %w", item.ProductCode, err)
		}
		for _, pair := range pairs {
			units[pair.Unit] = pair.Amount
		}
		increments := make(map[string]Decimal, 4)
		incPairs, err := parseQuotaMap(item.Increments)
		if err != nil {
			return nil, fmt.Errorf("catalog %s increments: %w", item.ProductCode, err)
		}
		for _, pair := range incPairs {
			increments[pair.Unit] = pair.Amount
		}
		code := normalizeEnum(item.ProductCode)
		products[code] = ProductDefinition{
			Code:     code,
			Name:     item.ProductName,
			Price:    item.AnnualPrice,
			Units:    units,
			Inc:      increments,
			IncPrice: item.IncrementPrice,
		}
	}

	ordered := make([]ProductDefinition, 0, len(products))
	used := make(map[string]struct{}, len(products))
	for _, code := range displayProductOrder {
		if product, ok := products[code]; ok {
			ordered = append(ordered, product)
			used[code] = struct{}{}
		}
	}
	remaining := make([]string, 0, len(products))
	for code := range products {
		if _, ok := used[code]; !ok {
			remaining = append(remaining, code)
		}
	}
	for _, code := range sortedProductCodes(remaining) {
		ordered = append(ordered, products[code])
	}

	bundles, err := s.store.ListBundleRows(ctx)
	if err != nil {
		return nil, err
	}
	for index := range bundles {
		singleCents := int64(0)
		for _, code := range bundles[index].Items {
			if product, ok := products[normalizeEnum(code)]; ok {
				cents, err := parseAmountCents(product.Price)
				if err == nil {
					singleCents += cents
				}
			}
		}
		bundles[index].SinglePrice = centsToDecimal(singleCents)
	}
	return &PricingPayload{Products: ordered, Bundles: bundles, IsMock: false}, nil
}

// channelTypeFromOrgType 把库里的组织类型映射成前端渠道类型。
func channelTypeFromOrgType(orgType string) string {
	switch normalizeEnum(orgType) {
	case opsOrgTypePlatform:
		return ChannelTypeSelf
	case opsOrgTypeSalesAgent:
		return ChannelTypeAgent
	default:
		return ChannelTypeAgency
	}
}

// orgTypeFromChannelType 把前端渠道类型映射成库里的组织类型。
func orgTypeFromChannelType(channelType string) (string, error) {
	switch strings.TrimSpace(channelType) {
	case "platform":
		return opsOrgTypePlatform, nil
	case "agent":
		return opsOrgTypeSalesAgent, nil
	case "agency", "":
		return opsOrgTypeAgency, nil
	}
	switch strings.TrimSpace(channelType) {
	case ChannelTypeSelf:
		return opsOrgTypePlatform, nil
	case ChannelTypeAgent:
		return opsOrgTypeSalesAgent, nil
	case ChannelTypeAgency:
		return opsOrgTypeAgency, nil
	}
	return "", invalidf("渠道类型必须是 灵策自营/渠道商/个人代理")
}

// statusFromActive 把启停映射成 ops_organizations.status。
func statusFromActive(active bool) string {
	if active {
		return "active"
	}
	return "disabled"
}

// GetChannelDirectory 返回渠道目录：渠道（含机构数）+ 扁平销售列表（含所属渠道与机构数）。
func (s *InstitutionService) GetChannelDirectory(ctx context.Context) (*ChannelDirectory, error) {
	channelRows, err := s.store.ListChannelRows(ctx)
	if err != nil {
		return nil, err
	}
	salesRows, err := s.store.ListSalesRows(ctx)
	if err != nil {
		return nil, err
	}

	salesByChannel := make(map[int64][]ChannelSales, len(channelRows))
	flatSales := make([]SalesWithStats, 0, len(salesRows))
	channelNames := make(map[int64]string, len(channelRows))
	for _, row := range channelRows {
		channelNames[row.ID] = row.Name
	}
	for _, row := range salesRows {
		sale := ChannelSales{
			ID:     row.ID,
			Name:   row.Name,
			Phone:  row.Phone,
			Active: row.Active,
		}
		if row.OrgID > 0 {
			salesByChannel[row.OrgID] = append(salesByChannel[row.OrgID], sale)
		}
		flatSales = append(flatSales, SalesWithStats{
			ChannelSales:     sale,
			ChannelID:        row.OrgID,
			ChannelName:      channelNames[row.OrgID],
			InstitutionCount: row.InstitutionCount,
		})
	}

	channels := make([]ChannelWithStats, 0, len(channelRows))
	for _, row := range channelRows {
		sales := salesByChannel[row.ID]
		if sales == nil {
			sales = []ChannelSales{}
		}
		channels = append(channels, ChannelWithStats{
			Channel: Channel{
				ID:     row.ID,
				Name:   row.Name,
				Type:   channelTypeFromOrgType(row.Type),
				Active: !strings.EqualFold(strings.TrimSpace(row.Status), "disabled"),
				Sales:  sales,
			},
			InstitutionCount: row.InstitutionCount,
		})
	}
	return &ChannelDirectory{Channels: channels, Sales: flatSales, IsMock: false}, nil
}

// SaveChannel 新增或修改渠道（payload.id 为空表示新增）。
func (s *InstitutionService) SaveChannel(ctx context.Context, payload ChannelPayload, adminID int64) (*Channel, error) {
	if err := s.requirePlatformAdmin(ctx, adminID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, invalidf("请填渠道名称")
	}
	if len([]rune(name)) > 100 {
		return nil, invalidf("渠道名称最多 100 个字符")
	}
	// 更新时没传的字段沿用库里现值，避免「只想改名」把启用状态/渠道类型重置。
	orgType := ""
	active := true
	if payload.ID > 0 {
		existing, err := s.store.GetChannelRow(ctx, payload.ID)
		if err != nil {
			return nil, err
		}
		active = !strings.EqualFold(strings.TrimSpace(existing.Status), "disabled")
		orgType, err = orgTypeFromChannelType(existing.Type)
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(payload.Type) != "" {
		parsed, err := orgTypeFromChannelType(payload.Type)
		if err != nil {
			return nil, err
		}
		orgType = parsed
	}
	if payload.Active != nil {
		active = *payload.Active
	}
	status := statusFromActive(active)

	exists, err := s.store.ChannelNameExists(ctx, name, payload.ID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, &ConflictError{Sentinel: ErrPeerConflict, Message: "渠道名已存在"}
	}

	channelID := payload.ID
	if channelID > 0 {
		if err := s.store.UpdateChannel(ctx, channelID, name, orgType, status); err != nil {
			if errors.Is(err, ErrPeerNotFound) {
				return nil, ErrPeerNotFound
			}
			return nil, err
		}
	} else {
		channelID, err = s.store.InsertChannel(ctx, name, orgType, status)
		if err != nil {
			return nil, err
		}
	}
	return s.getChannelDirectoryItem(ctx, channelID)
}

// getChannelDirectoryItem 取单个渠道（含销售列表）。
func (s *InstitutionService) getChannelDirectoryItem(ctx context.Context, channelID int64) (*Channel, error) {
	row, err := s.store.GetChannelRow(ctx, channelID)
	if err != nil {
		return nil, err
	}
	salesRows, err := s.store.ListSalesRows(ctx)
	if err != nil {
		return nil, err
	}
	sales := make([]ChannelSales, 0, 4)
	for _, item := range salesRows {
		if item.OrgID != channelID {
			continue
		}
		sales = append(sales, ChannelSales{
			ID:     item.ID,
			Name:   item.Name,
			Phone:  item.Phone,
			Active: item.Active,
		})
	}
	return &Channel{
		ID:     row.ID,
		Name:   row.Name,
		Type:   channelTypeFromOrgType(row.Type),
		Active: !strings.EqualFold(strings.TrimSpace(row.Status), "disabled"),
		Sales:  sales,
	}, nil
}

// SaveSales 新增或修改销售账号（payload.id 为空表示新增）。
// 新建账号统一给默认口令，由本人首次登录后修改；手机号在运营账号内唯一。
func (s *InstitutionService) SaveSales(ctx context.Context, payload SalesPayload, adminID int64) (*ChannelSales, error) {
	if err := s.requirePlatformAdmin(ctx, adminID); err != nil {
		return nil, err
	}
	if payload.ChannelID == 0 {
		payload.ChannelID = payload.SnakeChannelID
	}
	if payload.ChannelID <= 0 {
		return nil, invalidf("请选择渠道")
	}
	name := strings.TrimSpace(payload.Name)
	phone := strings.TrimSpace(payload.Phone)
	if name == "" {
		return nil, invalidf("请填姓名")
	}
	if len([]rune(name)) > 100 {
		return nil, invalidf("姓名最多 100 个字符")
	}
	if phone != "" && !mobilePhonePattern.MatchString(phone) {
		return nil, invalidf("手机号格式不对")
	}
	active := true
	if payload.ID > 0 {
		existing, err := s.store.GetSalesRow(ctx, payload.ID)
		if err != nil {
			return nil, err
		}
		active = existing.Active
	}
	if payload.Active != nil {
		active = *payload.Active
	}

	channel, err := s.store.GetChannelRow(ctx, payload.ChannelID)
	if err != nil {
		if errors.Is(err, ErrPeerNotFound) {
			return nil, &ValidationError{Message: "渠道不存在"}
		}
		return nil, err
	}

	if payload.ID > 0 {
		existing, err := s.store.GetSalesRow(ctx, payload.ID)
		if err != nil {
			return nil, err
		}
		if phone == "" {
			phone = existing.Phone
		}
		if phone == "" {
			return nil, invalidf("请填手机号")
		}
		exists, err := s.store.SalesPhoneExists(ctx, phone, payload.ID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, &ConflictError{Sentinel: ErrPeerConflict, Message: "手机号已被其它销售使用"}
		}
		if err := s.store.UpdateSales(ctx, payload.ID, name, phone, payload.ChannelID, active); err != nil {
			return nil, err
		}
		row, err := s.store.GetSalesRow(ctx, payload.ID)
		if err != nil {
			return nil, err
		}
		return &ChannelSales{ID: row.ID, Name: row.Name, Phone: row.Phone, Active: row.Active,
			ChannelID: row.OrgID, ChannelName: channel.Name}, nil
	}

	if phone == "" {
		return nil, invalidf("请填手机号")
	}
	exists, err := s.store.SalesPhoneExists(ctx, phone, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, &ConflictError{Sentinel: ErrPeerConflict, Message: "手机号已被其它销售使用"}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(defaultOpsAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash default sales password: %w", err)
	}
	salesID, err := s.store.InsertSales(ctx, name, phone, phone, string(hash), payload.ChannelID, active)
	if err != nil {
		return nil, err
	}
	row, err := s.store.GetSalesRow(ctx, salesID)
	if err != nil {
		return nil, err
	}
	return &ChannelSales{ID: row.ID, Name: row.Name, Phone: row.Phone, Active: row.Active,
		ChannelID: row.OrgID, ChannelName: channel.Name}, nil
}

// GetDictionaries 返回行业与科室字典（含停用项，前端按 active 过滤）。
func (s *InstitutionService) GetDictionaries(ctx context.Context) (*DictionaryPayload, error) {
	industries, err := s.store.ListDictItems(ctx, DictKindIndustry)
	if err != nil {
		return nil, err
	}
	departments, err := s.store.ListDictItems(ctx, DictKindDepartment)
	if err != nil {
		return nil, err
	}
	return &DictionaryPayload{Industries: industries, Departments: departments, IsMock: false}, nil
}

// SaveDictItem 新增或修改字典项（payload.id 为空表示新增）。
func (s *InstitutionService) SaveDictItem(ctx context.Context, kind string, payload DictItemPayload, adminID int64) (*DictItem, error) {
	if err := s.requirePlatformAdmin(ctx, adminID); err != nil {
		return nil, err
	}
	kind = normalizeEnum(kind)
	if _, err := dictTable(kind); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, invalidf("请填名称")
	}
	if len([]rune(name)) > 100 {
		return nil, invalidf("名称最多 100 个字符")
	}
	if payload.Order < 0 || payload.Order > 1000000 {
		return nil, invalidf("排序值不合法")
	}

	exists, err := s.store.DictNameExists(ctx, kind, name, payload.ID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, &ConflictError{Sentinel: ErrPeerConflict, Message: "已存在同名"}
	}

	if payload.ID > 0 {
		item, err := s.store.UpdateDictItem(ctx, kind, payload.ID, name, payload.Order)
		if err != nil {
			return nil, err
		}
		return &item, nil
	}
	item, err := s.store.InsertDictItem(ctx, kind, name, payload.Order)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ToggleDictItem 启停字典项。
func (s *InstitutionService) ToggleDictItem(ctx context.Context, kind string, id int64, adminID int64) (*DictItem, error) {
	if err := s.requirePlatformAdmin(ctx, adminID); err != nil {
		return nil, err
	}
	kind = normalizeEnum(kind)
	if _, err := dictTable(kind); err != nil {
		return nil, err
	}
	item, err := s.store.ToggleDictItem(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// requirePlatformAdmin 限制全局配置类写操作只允许平台账号/超管。
func (s *InstitutionService) requirePlatformAdmin(ctx context.Context, adminID int64) error {
	scope, err := s.store.GetAdminScope(ctx, adminID)
	if err != nil {
		return err
	}
	if !scope.CanSeeAll {
		return &PermissionError{Message: "仅平台运营账号可以修改渠道/销售/字典配置"}
	}
	return nil
}
