package billing

// 渠道 / 销售 / 行业与科室字典 / 套餐价目 的数据访问层。
//
// 渠道与销售复用运营组织表（ops_organizations / operations_admins），不另建账号体系：
//   · 渠道 = 运营组织，沿用 type=platform/agency，新增约定 type=agent 表示个人代理；
//   · 销售 = 归属某组织的运营账号（operations_admins.org_id）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// channelRow 是渠道目录的一行。
type channelRow struct {
	ID               int64
	Name             string
	Type             string
	Status           string
	InstitutionCount int64
}

// ListChannelRows 返回全部渠道（含停用，便于重新启用），平台组织排在最前。
func (s *Store) ListChannelRows(ctx context.Context) ([]channelRow, error) {
	rows, err := s.pool.Query(ctx, `
SELECT o.id,
       COALESCE(o.name, ''),
       COALESCE(o.type, 'agency'),
       COALESCE(o.status, 'active'),
       (SELECT count(*)
        FROM tenant_ownerships ow
        WHERE ow.org_id = o.id AND ow.effective_to IS NULL)
FROM ops_organizations o
ORDER BY CASE WHEN COALESCE(o.type, '') = 'platform' THEN 0 ELSE 1 END, o.id`)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()

	items := make([]channelRow, 0, 8)
	for rows.Next() {
		var item channelRow
		if err := rows.Scan(&item.ID, &item.Name, &item.Type, &item.Status, &item.InstitutionCount); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channels: %w", err)
	}
	return items, nil
}

// GetChannelRow 按 id 取渠道。
func (s *Store) GetChannelRow(ctx context.Context, channelID int64) (channelRow, error) {
	var item channelRow
	err := s.pool.QueryRow(ctx, `
SELECT o.id, COALESCE(o.name, ''), COALESCE(o.type, 'agency'), COALESCE(o.status, 'active'),
       (SELECT count(*)
        FROM tenant_ownerships ow
        WHERE ow.org_id = o.id AND ow.effective_to IS NULL)
FROM ops_organizations o
WHERE o.id = $1`, channelID).Scan(&item.ID, &item.Name, &item.Type, &item.Status, &item.InstitutionCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return channelRow{}, ErrPeerNotFound
	}
	if err != nil {
		return channelRow{}, fmt.Errorf("get channel: %w", err)
	}
	return item, nil
}

// salesRow 是销售目录的一行。
type salesRow struct {
	ID               int64
	Name             string
	Phone            string
	Active           bool
	OrgID            int64
	InstitutionCount int64
}

// ListSalesRows 返回全部销售账号（未删除），按所属组织排序。
func (s *Store) ListSalesRows(ctx context.Context) ([]salesRow, error) {
	rows, err := s.pool.Query(ctx, `
SELECT a.id,
       COALESCE(a.name, ''),
       COALESCE(a.phone, ''),
       CASE WHEN lower(COALESCE(a.is_active::text, 'true')) IN ('1', 't', 'true', 'yes') THEN TRUE ELSE FALSE END,
       COALESCE(a.org_id, 0),
       (SELECT count(*)
        FROM tenant_ownerships ow
        WHERE ow.sales_admin_id = a.id AND ow.effective_to IS NULL)
FROM operations_admins a
WHERE a.deleted_at IS NULL
ORDER BY COALESCE(a.org_id, 0), a.id`)
	if err != nil {
		return nil, fmt.Errorf("list sales: %w", err)
	}
	defer rows.Close()

	items := make([]salesRow, 0, 16)
	for rows.Next() {
		var item salesRow
		if err := rows.Scan(&item.ID, &item.Name, &item.Phone, &item.Active, &item.OrgID, &item.InstitutionCount); err != nil {
			return nil, fmt.Errorf("scan sales: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sales: %w", err)
	}
	return items, nil
}

// GetSalesRow 按 id 取销售。
func (s *Store) GetSalesRow(ctx context.Context, salesID int64) (salesRow, error) {
	var item salesRow
	err := s.pool.QueryRow(ctx, `
SELECT a.id, COALESCE(a.name, ''), COALESCE(a.phone, ''),
       CASE WHEN lower(COALESCE(a.is_active::text, 'true')) IN ('1', 't', 'true', 'yes') THEN TRUE ELSE FALSE END,
       COALESCE(a.org_id, 0),
       (SELECT count(*)
        FROM tenant_ownerships ow
        WHERE ow.sales_admin_id = a.id AND ow.effective_to IS NULL)
FROM operations_admins a
WHERE a.id = $1 AND a.deleted_at IS NULL`, salesID).Scan(&item.ID, &item.Name, &item.Phone, &item.Active, &item.OrgID, &item.InstitutionCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return salesRow{}, ErrPeerNotFound
	}
	if err != nil {
		return salesRow{}, fmt.Errorf("get sales: %w", err)
	}
	return item, nil
}

// ChannelNameExists 判断渠道名（忽略大小写）是否已被其它组织使用。
func (s *Store) ChannelNameExists(ctx context.Context, name string, excludeID int64) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM ops_organizations
    WHERE lower(COALESCE(name, '')) = lower($1)
      AND ($2::bigint = 0 OR id <> $2)
)`, name, excludeID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check channel name: %w", err)
	}
	return exists, nil
}

// SalesPhoneExists 判断销售手机号是否已被其它运营账号使用。
func (s *Store) SalesPhoneExists(ctx context.Context, phone string, excludeID int64) (bool, error) {
	if strings.TrimSpace(phone) == "" {
		return false, nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM operations_admins
    WHERE phone = $1
      AND ($2::bigint = 0 OR id <> $2)
      AND deleted_at IS NULL
)`, phone, excludeID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check sales phone: %w", err)
	}
	return exists, nil
}

// InsertChannel 新建渠道。
func (s *Store) InsertChannel(ctx context.Context, name, orgType, status string) (int64, error) {
	var id int64
	if err := s.pool.QueryRow(ctx, `
INSERT INTO ops_organizations (name, type, status)
VALUES ($1, $2, $3)
RETURNING id`, name, orgType, status).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert channel: %w", err)
	}
	return id, nil
}

// UpdateChannel 修改渠道。
func (s *Store) UpdateChannel(ctx context.Context, channelID int64, name, orgType, status string) error {
	tag, err := s.pool.Exec(ctx, `
UPDATE ops_organizations
SET name = $2, type = $3, status = $4, updated_at = NOW()
WHERE id = $1`, channelID, name, orgType, status)
	if err != nil {
		return fmt.Errorf("update channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPeerNotFound
	}
	return nil
}

// InsertSales 新建销售账号（passwordHash 由 service 生成）。
func (s *Store) InsertSales(ctx context.Context, name, phone, username, passwordHash string, orgID int64, active bool) (int64, error) {
	var id int64
	if err := s.pool.QueryRow(ctx, `
INSERT INTO operations_admins (name, phone, username, password_hash, org_id, is_active, session_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 1, NOW(), NOW())
RETURNING id`, name, phone, username, passwordHash, orgID, activeFlag(active)).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert sales: %w", err)
	}
	return id, nil
}

// UpdateSales 修改销售账号；渠道变化时同步当前归属的 org_id（保持渠道/销售一致）。
func (s *Store) UpdateSales(ctx context.Context, salesID int64, name, phone string, orgID int64, active bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin update sales transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var oldOrgID int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(org_id, 0) FROM operations_admins WHERE id = $1 AND deleted_at IS NULL`, salesID).Scan(&oldOrgID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPeerNotFound
		}
		return fmt.Errorf("query sales org: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE operations_admins
SET name = $2, phone = $3, org_id = $4, is_active = $5, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL`, salesID, name, phone, orgID, activeFlag(active)); err != nil {
		return fmt.Errorf("update sales: %w", err)
	}
	if oldOrgID != orgID {
		// 销售换渠道：把其名下「当前有效」的机构归属一并迁到新渠道，
		// 与 mock 的 saveSales 行为一致（机构侧看到的就是新渠道）。
		if _, err := tx.Exec(ctx, `
UPDATE tenant_ownerships
SET org_id = $2, updated_at = NOW()
WHERE sales_admin_id = $1 AND effective_to IS NULL`, salesID, orgID); err != nil {
			return fmt.Errorf("move sales ownerships: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit update sales transaction: %w", err)
	}
	return nil
}

// activeFlag 把 bool 转成 operations_admins.is_active 的整数表示（兼容历史的 0/1 列）。
func activeFlag(active bool) int {
	if active {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// 行业 / 科室字典
// ---------------------------------------------------------------------------

// dictTable 把字典类型映射到具体表名；只允许两个白名单值，直接拼进 SQL 是安全的。
func dictTable(kind string) (string, error) {
	switch normalizeEnum(kind) {
	case DictKindIndustry:
		return "billing_industry_dict", nil
	case DictKindDepartment:
		return "organization_department_dict", nil
	default:
		return "", invalidf("kind must be industry/department")
	}
}

// ListDictItems 返回一类字典的全部条目（按排序，含停用）。
func (s *Store) ListDictItems(ctx context.Context, kind string) ([]DictItem, error) {
	table, err := dictTable(kind)
	if err != nil {
		return nil, err
	}
	withType := normalizeEnum(kind) == DictKindDepartment
	cols := "id, name, sort_order, is_active"
	if withType {
		cols += ", COALESCE(type, '')"
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
SELECT %s
FROM %s
ORDER BY sort_order, id`, cols, table))
	if err != nil {
		return nil, fmt.Errorf("list dict items: %w", err)
	}
	defer rows.Close()

	items := make([]DictItem, 0, 32)
	for rows.Next() {
		var item DictItem
		var scanErr error
		if withType {
			scanErr = rows.Scan(&item.ID, &item.Name, &item.Order, &item.Active, &item.Type)
		} else {
			scanErr = rows.Scan(&item.ID, &item.Name, &item.Order, &item.Active)
		}
		if scanErr != nil {
			return nil, fmt.Errorf("scan dict item: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dict items: %w", err)
	}
	return items, nil
}

// GetDictItem 取一条字典项。
func (s *Store) GetDictItem(ctx context.Context, kind string, id int64) (DictItem, error) {
	table, err := dictTable(kind)
	if err != nil {
		return DictItem{}, err
	}
	var item DictItem
	if normalizeEnum(kind) == DictKindDepartment {
		err = s.pool.QueryRow(ctx, `SELECT id, name, sort_order, is_active, COALESCE(type,'') FROM organization_department_dict WHERE id = $1`, id).
			Scan(&item.ID, &item.Name, &item.Order, &item.Active, &item.Type)
	} else {
		err = s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT id, name, sort_order, is_active FROM %s WHERE id = $1`, table), id).
			Scan(&item.ID, &item.Name, &item.Order, &item.Active)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return DictItem{}, ErrPeerNotFound
	}
	if err != nil {
		return DictItem{}, fmt.Errorf("get dict item: %w", err)
	}
	return item, nil
}

// DictNameExists 判断同类字典里是否已有同名条目。
func (s *Store) DictNameExists(ctx context.Context, kind, name string, excludeID int64) (bool, error) {
	table, err := dictTable(kind)
	if err != nil {
		return false, err
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
SELECT EXISTS (
    SELECT 1 FROM %s
    WHERE name = $1 AND ($2::bigint = 0 OR id <> $2)
)`, table), name, excludeID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check dict name: %w", err)
	}
	return exists, nil
}

// InsertDictItem 新增字典项；sort_order 为 0 时排到末尾（最大排序 +10）。
func (s *Store) InsertDictItem(ctx context.Context, kind string, payload DictItemPayload) (DictItem, error) {
	if normalizeEnum(kind) == DictKindDepartment {
		var item DictItem
		err := s.pool.QueryRow(ctx, `
INSERT INTO organization_department_dict (code, name, type, sort_order)
VALUES ($1, $2, $3, COALESCE(NULLIF($4::int, 0), (SELECT COALESCE(MAX(sort_order), 0) + 10 FROM organization_department_dict)))
RETURNING id, name, sort_order, is_active, type`, payload.Code, payload.Name, payload.Type, payload.Order).
			Scan(&item.ID, &item.Name, &item.Order, &item.Active, &item.Type)
		if err != nil {
			if isUniqueViolation(err) {
				return DictItem{}, ErrPeerConflict
			}
			return DictItem{}, fmt.Errorf("insert dict item: %w", err)
		}
		return item, nil
	}
	table, err := dictTable(kind)
	if err != nil {
		return DictItem{}, err
	}
	var item DictItem
	err = s.pool.QueryRow(ctx, fmt.Sprintf(`
INSERT INTO %s (name, sort_order)
VALUES ($1, COALESCE(NULLIF($2::int, 0), (SELECT COALESCE(MAX(sort_order), 0) + 10 FROM %s)))
RETURNING id, name, sort_order, is_active`, table, table), payload.Name, payload.Order).
		Scan(&item.ID, &item.Name, &item.Order, &item.Active)
	if err != nil {
		if isUniqueViolation(err) {
			return DictItem{}, ErrPeerConflict
		}
		return DictItem{}, fmt.Errorf("insert dict item: %w", err)
	}
	return item, nil
}

// UpdateDictItem 修改字典项名称与排序。
func (s *Store) UpdateDictItem(ctx context.Context, kind string, payload DictItemPayload) (DictItem, error) {
	if normalizeEnum(kind) == DictKindDepartment {
		var item DictItem
		err := s.pool.QueryRow(ctx, `
UPDATE organization_department_dict
SET name = $2, type = $3, sort_order = $4, updated_at = NOW()
WHERE id = $1
RETURNING id, name, sort_order, is_active, type`, payload.ID, payload.Name, payload.Type, payload.Order).
			Scan(&item.ID, &item.Name, &item.Order, &item.Active, &item.Type)
		if errors.Is(err, pgx.ErrNoRows) {
			return DictItem{}, ErrPeerNotFound
		}
		if err != nil {
			if isUniqueViolation(err) {
				return DictItem{}, ErrPeerConflict
			}
			return DictItem{}, fmt.Errorf("update dict item: %w", err)
		}
		return item, nil
	}
	table, err := dictTable(kind)
	if err != nil {
		return DictItem{}, err
	}
	var item DictItem
	err = s.pool.QueryRow(ctx, fmt.Sprintf(`
UPDATE %s
SET name = $2, sort_order = $3, updated_at = NOW()
WHERE id = $1
RETURNING id, name, sort_order, is_active`, table), payload.ID, payload.Name, payload.Order).
		Scan(&item.ID, &item.Name, &item.Order, &item.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return DictItem{}, ErrPeerNotFound
	}
	if err != nil {
		if isUniqueViolation(err) {
			return DictItem{}, ErrPeerConflict
		}
		return DictItem{}, fmt.Errorf("update dict item: %w", err)
	}
	return item, nil
}

// ToggleDictItem 启停一条字典项。
func (s *Store) ToggleDictItem(ctx context.Context, kind string, id int64) (DictItem, error) {
	table, err := dictTable(kind)
	if err != nil {
		return DictItem{}, err
	}
	var item DictItem
	err = s.pool.QueryRow(ctx, fmt.Sprintf(`
UPDATE %s
SET is_active = NOT is_active, updated_at = NOW()
WHERE id = $1
RETURNING id, name, sort_order, is_active`, table), id).
		Scan(&item.ID, &item.Name, &item.Order, &item.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return DictItem{}, ErrPeerNotFound
	}
	if err != nil {
		return DictItem{}, fmt.Errorf("toggle dict item: %w", err)
	}
	return item, nil
}

// ---------------------------------------------------------------------------
// 套餐
// ---------------------------------------------------------------------------

// ListBundleRows 返回启用的套餐（含单价合计所需的产品代码）。
func (s *Store) ListBundleRows(ctx context.Context) ([]BundleDefinition, error) {
	rows, err := s.pool.Query(ctx, `
SELECT bundle_key, name, price::text, COALESCE(items, '[]'::jsonb)::text
FROM billing_bundles
WHERE is_active
ORDER BY bundle_key`)
	if err != nil {
		return nil, fmt.Errorf("list bundles: %w", err)
	}
	defer rows.Close()

	items := make([]BundleDefinition, 0, 8)
	for rows.Next() {
		var (
			item  BundleDefinition
			price string
			raw   string
		)
		if err := rows.Scan(&item.Key, &item.Name, &price, &raw); err != nil {
			return nil, fmt.Errorf("scan bundle: %w", err)
		}
		item.Price = Decimal(price)
		if err := decodeStringList(raw, &item.Items); err != nil {
			return nil, fmt.Errorf("decode bundle items: %w", err)
		}
		if item.Items == nil {
			item.Items = []string{}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bundles: %w", err)
	}
	return items, nil
}

// GetBundle 按 key 取套餐；不存在返回 ErrPeerNotFound。
func (s *Store) GetBundle(ctx context.Context, key string) (BundleDefinition, error) {
	var (
		item  BundleDefinition
		price string
		raw   string
	)
	err := s.pool.QueryRow(ctx, `
SELECT bundle_key, name, price::text, COALESCE(items, '[]'::jsonb)::text
FROM billing_bundles
WHERE bundle_key = $1 AND is_active`, normalizeEnum(key)).Scan(&item.Key, &item.Name, &price, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return BundleDefinition{}, ErrPeerNotFound
	}
	if err != nil {
		return BundleDefinition{}, fmt.Errorf("get bundle: %w", err)
	}
	item.Price = Decimal(price)
	if err := decodeStringList(raw, &item.Items); err != nil {
		return BundleDefinition{}, fmt.Errorf("decode bundle items: %w", err)
	}
	if item.Items == nil {
		item.Items = []string{}
	}
	return item, nil
}

// decodeStringList 解析 JSONB 字符串数组。
func decodeStringList(raw string, dest *[]string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		*dest = []string{}
		return nil
	}
	return json.Unmarshal([]byte(raw), dest)
}
