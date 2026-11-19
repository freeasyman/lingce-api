package billing

// 机构档案 / 归属 / 服务期的数据访问层。
//
// 只做 SQL 与扫描，不做业务判断（业务规则在 institution_service.go / institution_action_service.go）。
// 与 billing 模块既有 store 一致：金额/额度统一 cast 成 text 由 Decimal 承载，避免 float 往返。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// institutionBaseRow 是机构列表/详情的基础行（不含额度、管理员、应收等展开数据）。
type institutionBaseRow struct {
	ID          int64
	Name        string
	Legal       string
	Code        string
	Industry    string
	Contact     string
	Title       string
	Phone       string
	Email       string
	Province    string
	City        string
	District    string
	AccountMode string
	CreatedAt   time.Time

	PeriodID     *int64
	PeriodStatus string
	StartedOn    *string
	ExpiredOn    *string

	// 主体合同行（formal/trial 中最近到期的一条 active），用于状态推导。
	PrimaryType      string
	PrimaryStartedOn *string
	PrimaryExpiredOn *string
	PrimaryStatus    string

	// 全部合同行（随基础行一次取回，避免多次远程往返）。
	ContractsJSON string
	// TrialCount 历史试用合同条数（含当前），用于「已试用 N 次」与重开权限。
	TrialCount int

	// 服务生命周期标记（migration 20261112_012）：暂停只改这两列，
	// 不改服务期、不改额度。
	PausedAt     *time.Time
	PausedReason string

	OrgID        *int64
	OrgName      string
	SalesAdminID *int64
	SalesName    string
}

// institutionBaseSelect 是列表与详情共用的选择列。
const institutionBaseSelect = `
SELECT t.id,
       COALESCE(t.name, ''),
       COALESCE(t.legal_name, ''),
       COALESCE(t.code, ''),
       COALESCE(t.industry, ''),
       COALESCE(t.contact_name, ''),
       COALESCE(t.contact_title, ''),
       COALESCE(t.contact_phone, ''),
       COALESCE(t.contact_email, ''),
       COALESCE(t.province_code, ''),
       COALESCE(t.city_code, ''),
       COALESCE(t.district_code, ''),
       COALESCE(NULLIF(lower(t.account_mode), ''), 'formal'),
       t.created_at,
       p.id,
       COALESCE(p.status, ''),
       to_char(p.started_on, 'YYYY-MM-DD'),
       to_char(p.expired_on, 'YYYY-MM-DD'),
       COALESCE(pc.contract_type, ''),
       to_char(pc.started_on, 'YYYY-MM-DD'),
       to_char(pc.expired_on, 'YYYY-MM-DD'),
       COALESCE(pc.status, ''),
       t.paused_at,
       COALESCE(t.paused_reason, ''),
       ow.org_id,
       COALESCE(org.name, ''),
       ow.sales_admin_id,
       COALESCE(sales.name, ''),
       (SELECT count(*) FROM subscription_periods spc
         WHERE spc.tenant_id = t.id AND spc.contract_type = 'trial') AS trial_count,
       COALESCE((
           SELECT json_agg(json_build_object(
                      'id', sp2.id, 'type', sp2.contract_type, 'status', sp2.status,
                      'startedOn', to_char(sp2.started_on, 'YYYY-MM-DD'),
                      'expiredOn', to_char(sp2.expired_on, 'YYYY-MM-DD'),
                      'products', COALESCE((
                          SELECT json_agg(DISTINCT e.product_code)
                          FROM product_entitlements e
                          WHERE e.tenant_id = sp2.tenant_id AND e.period_id = sp2.id
                      ), '[]'::json))
                  ORDER BY sp2.id DESC)
           FROM subscription_periods sp2 WHERE sp2.tenant_id = t.id
       ), '[]'::json)::text AS contracts_json
FROM tenants t
LEFT JOIN LATERAL (
    SELECT sp.id, sp.status, sp.started_on, sp.expired_on, sp.contract_type
    FROM subscription_periods sp
    WHERE sp.tenant_id = t.id
    -- 服务期可能有多条：优先主体合同（formal/trial），体验行不抢主体展示位；
    -- 再 active、再 expired，同级按 id 倒序。
    -- 不能按 expired_on 倒序：服务期被置 terminated 后，旧的试用期可能因
    -- 到期日更晚而胜出，导致档案显示错误的服务期。
    ORDER BY (CASE WHEN sp.contract_type IN ('formal','trial') THEN 1 ELSE 0 END) DESC,
             (sp.status = 'active') DESC, (sp.status = 'expired') DESC,
             sp.id DESC
    LIMIT 1
) p ON TRUE
LEFT JOIN LATERAL (
    -- 主体合同（formal/trial）里最当前的一条（active 优先、expired 次之），
    -- 用于推导机构状态与动作门槛；是否覆盖今天在服务层按日期再判断。
    SELECT sp.contract_type, sp.started_on, sp.expired_on, sp.status
    FROM subscription_periods sp
    WHERE sp.tenant_id = t.id
      AND sp.contract_type IN ('formal','trial')
    ORDER BY (sp.status = 'active') DESC, (sp.status = 'expired') DESC, sp.id DESC
    LIMIT 1
) pc ON TRUE
LEFT JOIN tenant_ownerships ow ON ow.tenant_id = t.id AND ow.effective_to IS NULL
LEFT JOIN ops_organizations org ON org.id = ow.org_id
LEFT JOIN operations_admins sales ON sales.id = ow.sales_admin_id AND sales.deleted_at IS NULL
`

// listInstitutionsSQL 在基础列上追加数据权限、关键词与筛选条件。
// $1 orgID（0 = 平台账号可看全部）$2 keyword（小写）$3 filter。
const listInstitutionsSQL = institutionBaseSelect + `
WHERE t.deleted_at IS NULL
  -- 系统租户（tenant_id=0）是平台内置数据容器，不是可计费机构，运营端列表不展示。
  AND t.id > 0
  AND ($1::bigint = 0
       OR ow.org_id = $1
       OR EXISTS (
           SELECT 1 FROM trial_customer_ownerships tc
           WHERE tc.tenant_id = t.id AND tc.owner_org_id = $1
       ))
  AND ($2::text = ''
       OR strpos(lower(COALESCE(t.name, '')), $2) > 0
       OR strpos(lower(COALESCE(t.legal_name, '')), $2) > 0
       OR strpos(lower(COALESCE(t.code, '')), $2) > 0
       OR strpos(lower(COALESCE(t.contact_name, '')), $2) > 0
       OR strpos(lower(COALESCE(t.contact_phone, '')), $2) > 0
       OR EXISTS (
           SELECT 1 FROM employees e
           WHERE e.tenant_id = t.id
             AND e.deleted_at IS NULL
             AND (strpos(lower(COALESCE(e.phone, '')), $2) > 0
                  OR strpos(lower(COALESCE(e.name, '')), $2) > 0)
       ))
  AND ($3::text = 'all'
       OR ($3 = 'unopened' AND p.id IS NULL)
       OR ($3 = 'trial' AND p.id IS NOT NULL AND p.status <> 'terminated'
           AND p.expired_on >= CURRENT_DATE
           AND p.contract_type = 'trial')
       OR ($3 = 'expiring' AND p.status = 'active'
           AND p.contract_type = 'formal'
           AND p.expired_on >= CURRENT_DATE AND p.expired_on <= CURRENT_DATE + 7)
       OR ($3 = 'trial-expiring' AND p.status = 'active'
           AND p.contract_type = 'trial'
           AND p.expired_on >= CURRENT_DATE AND p.expired_on <= CURRENT_DATE + 7)
       OR ($3 = 'expired' AND p.id IS NOT NULL AND p.status <> 'terminated' AND p.expired_on < CURRENT_DATE)
       OR ($3 = 'unpaid' AND EXISTS (
           SELECT 1 FROM billing_orders bo
           WHERE bo.tenant_id = t.id
             AND bo.status NOT IN ('cancelled', 'refunded', 'void')
             AND bo.payable_amount > bo.paid_amount
       )))
ORDER BY t.created_at DESC NULLS LAST, t.id DESC
`

// ListInstitutionBases 返回符合筛选条件的机构基础行（数据权限在 SQL 里收敛）。
func (s *Store) ListInstitutionBases(ctx context.Context, query InstitutionQuery) ([]institutionBaseRow, error) {
	query, err := normalizeInstitutionQuery(query)
	if err != nil {
		return nil, err
	}
	sql := listInstitutionsSQL
	args := []any{query.OrgID, strings.ToLower(query.Keyword), query.Filter}
	if query.PageSize > 0 {
		sql += " LIMIT $4 OFFSET $5"
		args = append(args, query.PageSize, int64(query.Page-1)*int64(query.PageSize))
	}
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list institutions: %w", err)
	}
	defer rows.Close()

	items := make([]institutionBaseRow, 0, 64)
	for rows.Next() {
		item, err := scanInstitutionBase(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan institution: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate institutions: %w", err)
	}
	return items, nil
}

// CountInstitutions 返回符合筛选条件的机构总数（服务端分页用）。
func (s *Store) CountInstitutions(ctx context.Context, query InstitutionQuery) (int64, error) {
	query, err := normalizeInstitutionQuery(query)
	if err != nil {
		return 0, err
	}
	var total int64
	err = s.pool.QueryRow(ctx, `SELECT count(*) FROM (`+listInstitutionsSQL+`) sub`,
		query.OrgID, strings.ToLower(query.Keyword), query.Filter).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("count institutions: %w", err)
	}
	return total, nil
}

// GetInstitutionBase 按租户 id 取基础行；不存在返回 ErrInstitutionNotFound。
func (s *Store) GetInstitutionBase(ctx context.Context, tenantID int64) (institutionBaseRow, error) {
	row := s.pool.QueryRow(ctx, institutionBaseSelect+` WHERE t.id = $1 AND t.deleted_at IS NULL`, tenantID)
	item, err := scanInstitutionBase(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return institutionBaseRow{}, ErrInstitutionNotFound
	}
	if err != nil {
		return institutionBaseRow{}, fmt.Errorf("get institution: %w", err)
	}
	return item, nil
}

// scanInstitutionBase 统一解析基础行（列表与详情共用）。
func scanInstitutionBase(scan func(dest ...any) error) (institutionBaseRow, error) {
	var (
		item     institutionBaseRow
		periodID *int64
	)
	if err := scan(
		&item.ID,
		&item.Name,
		&item.Legal,
		&item.Code,
		&item.Industry,
		&item.Contact,
		&item.Title,
		&item.Phone,
		&item.Email,
		&item.Province,
		&item.City,
		&item.District,
		&item.AccountMode,
		&item.CreatedAt,
		&periodID,
		&item.PeriodStatus,
		&item.StartedOn,
		&item.ExpiredOn,
		&item.PrimaryType,
		&item.PrimaryStartedOn,
		&item.PrimaryExpiredOn,
		&item.PrimaryStatus,
		&item.PausedAt,
		&item.PausedReason,
		&item.OrgID,
		&item.OrgName,
		&item.SalesAdminID,
		&item.SalesName,
		&item.TrialCount,
		&item.ContractsJSON,
	); err != nil {
		return institutionBaseRow{}, err
	}
	item.PeriodID = periodID
	return item, nil
}

// CountTrialContracts 统计机构的试用合同条数（含历史），用于重开次数与权限。
func (s *Store) CountTrialContracts(ctx context.Context, tenantID int64) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `
SELECT count(*) FROM subscription_periods
WHERE tenant_id = $1 AND contract_type = 'trial'`, tenantID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count trial contracts: %w", err)
	}
	return n, nil
}

// ListContracts 返回机构的全部合同行（倒序，最新在前）。
func (s *Store) ListContracts(ctx context.Context, tenantID int64) ([]InstitutionContract, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, contract_type, status,
       to_char(started_on, 'YYYY-MM-DD'), to_char(expired_on, 'YYYY-MM-DD')
FROM subscription_periods
WHERE tenant_id = $1
ORDER BY id DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list institution contracts: %w", err)
	}
	defer rows.Close()
	var out []InstitutionContract
	for rows.Next() {
		var c InstitutionContract
		if err := rows.Scan(&c.ID, &c.Type, &c.Status, &c.StartedOn, &c.ExpiredOn); err != nil {
			return nil, fmt.Errorf("scan institution contract: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate institution contracts: %w", err)
	}
	return out, nil
}

// entitlementRow 是机构额度的一行（按额度类型分组由 service 处理）。
type entitlementRow struct {
	TenantID    int64
	ProductCode string
	Unit        string
	Edition     string
	QuotaType   string
	Granted     Decimal
	Used        Decimal
	Balance     Decimal
	Status      string
}

// ListInstitutionEntitlementRows 批量取机构额度行（列表页一次查完，避免 N+1）。
func (s *Store) ListInstitutionEntitlementRows(ctx context.Context, tenantIDs []int64) ([]entitlementRow, error) {
	if len(tenantIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
SELECT tenant_id, product_code, unit, edition, quota_type,
       granted::text, used::text, balance::text, status
FROM product_entitlements
WHERE tenant_id = ANY($1)
ORDER BY tenant_id, product_code, unit, edition, quota_type`, tenantIDs)
	if err != nil {
		return nil, fmt.Errorf("list institution entitlements: %w", err)
	}
	defer rows.Close()

	items := make([]entitlementRow, 0, len(tenantIDs)*8)
	for rows.Next() {
		var (
			item          entitlementRow
			granted, used string
			balance       string
		)
		if err := rows.Scan(&item.TenantID, &item.ProductCode, &item.Unit, &item.Edition, &item.QuotaType,
			&granted, &used, &balance, &item.Status); err != nil {
			return nil, fmt.Errorf("scan institution entitlement: %w", err)
		}
		item.Granted = Decimal(granted)
		item.Used = Decimal(used)
		item.Balance = Decimal(balance)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate institution entitlements: %w", err)
	}
	return items, nil
}

// ListInstitutionDepartments 批量取机构涉及的科室名（按字典排序）。
func (s *Store) ListInstitutionDepartments(ctx context.Context, tenantIDs []int64) (map[int64][]string, error) {
	result := make(map[int64][]string, len(tenantIDs))
	if len(tenantIDs) == 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, `
SELECT td.tenant_id, d.name
FROM tenant_departments td
JOIN organization_department_dict d ON d.id = td.department_id
WHERE td.tenant_id = ANY($1)
ORDER BY td.tenant_id, d.sort_order, d.id`, tenantIDs)
	if err != nil {
		return nil, fmt.Errorf("list institution departments: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			tenantID int64
			name     string
		)
		if err := rows.Scan(&tenantID, &name); err != nil {
			return nil, fmt.Errorf("scan institution department: %w", err)
		}
		result[tenantID] = append(result[tenantID], name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate institution departments: %w", err)
	}
	return result, nil
}

// ListInstitutionReceivables 取机构的应收单（应收 = 订单应付，已收 = 订单已核销）。
func (s *Store) ListInstitutionReceivables(ctx context.Context, tenantID int64) ([]InstitutionReceivable, error) {
	rows, err := s.pool.Query(ctx, `
SELECT order_no, payable_amount::text, paid_amount::text
FROM billing_orders
WHERE tenant_id = $1
  AND order_type NOT IN ('gift', 'trial')
  AND status NOT IN ('cancelled', 'refunded', 'void')
ORDER BY created_at ASC, id ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list institution receivables: %w", err)
	}
	defer rows.Close()

	items := make([]InstitutionReceivable, 0, 8)
	for rows.Next() {
		var (
			item         InstitutionReceivable
			amount, paid string
		)
		if err := rows.Scan(&item.No, &amount, &paid); err != nil {
			return nil, fmt.Errorf("scan institution receivable: %w", err)
		}
		item.Amount = Decimal(amount)
		item.Paid = Decimal(paid)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate institution receivables: %w", err)
	}
	return items, nil
}

// ListInstitutionPayLogs 取机构维度的收款记录：同一张收款单按机构拆出核销额。
// 开票状态按该收款核销到的订单是否有非作废发票推导，机构档案不再显示写死的「未开票」。
func (s *Store) ListInstitutionPayLogs(ctx context.Context, tenantID int64) ([]InstitutionPayLog, error) {
	rows, err := s.pool.Query(ctx, `
SELECT p.payment_no,
       SUM(a.amount)::text,
       to_char(p.received_on, 'YYYY-MM-DD'),
       p.method,
       CASE WHEN EXISTS (
           SELECT 1
           FROM billing_payment_allocations a2
           JOIN billing_orders o2 ON o2.id = a2.order_id AND o2.tenant_id = $1
           JOIN billing_invoices i ON i.order_id = a2.order_id AND i.status <> 'voided'
           WHERE a2.payment_id = p.id
       ) THEN '已开票' ELSE '未开票' END
FROM billing_payment_allocations a
JOIN billing_payments p ON p.id = a.payment_id
JOIN billing_orders o ON o.id = a.order_id
WHERE o.tenant_id = $1
GROUP BY p.id, p.payment_no, p.received_on, p.method
ORDER BY p.received_on DESC, p.id DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list institution pay logs: %w", err)
	}
	defer rows.Close()

	items := make([]InstitutionPayLog, 0, 8)
	for rows.Next() {
		var (
			item   InstitutionPayLog
			amount string
		)
		if err := rows.Scan(&item.No, &amount, &item.Date, &item.Method, &item.Invoice); err != nil {
			return nil, fmt.Errorf("scan institution pay log: %w", err)
		}
		item.Amount = Decimal(amount)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate institution pay logs: %w", err)
	}
	return items, nil
}

// ListInstitutionConsumptions 取机构最近的额度变动明细。
//
// 口径：
//   - 交付扣量：entitlement_ledger.type='usage'，amount 为负；
//   - 人工调额度更正：source='correction'（正向 type=grant、负向 type=usage），
//     amount 保留带符号原值，前端据此显示「+10 / -5」。
//
// 对扣量，deliverable 是产品名；对更正，固定显示「人工调额度」，原因在 reason 里。
func (s *Store) ListInstitutionConsumptions(ctx context.Context, tenantID int64, limit int) ([]InstitutionConsumption, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
SELECT to_char(l.created_at, 'YYYY-MM-DD HH24:MI'),
       l.product_code,
       CASE WHEN l.source = 'correction' THEN '人工调额度'
            ELSE COALESCE(NULLIF(pc.product_name, ''), l.product_code) END,
       l.unit,
       l.amount::text,
       COALESCE(o.order_no, ''),
       l.type,
       l.source,
       l.reason
FROM entitlement_ledger l
LEFT JOIN billing_orders o ON o.id = l.linked_order_id
LEFT JOIN LATERAL (
    SELECT c.product_name
    FROM product_catalog c
    WHERE c.product_code = l.product_code
      AND c.product_name <> ''
    ORDER BY c.price_version DESC, c.id DESC
    LIMIT 1
) pc ON TRUE
WHERE l.tenant_id = $1
  AND l.amount <> 0
  AND (l.type = 'usage' OR l.source = 'correction')
ORDER BY l.created_at DESC, l.id DESC
LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list institution consumptions: %w", err)
	}
	defer rows.Close()

	items := make([]InstitutionConsumption, 0, limit)
	for rows.Next() {
		var (
			item   InstitutionConsumption
			amount string
		)
		if err := rows.Scan(&item.At, &item.ProductCode, &item.Deliverable, &item.Unit, &amount,
			&item.OrderNo, &item.Type, &item.Source, &item.Reason); err != nil {
			return nil, fmt.Errorf("scan institution consumption: %w", err)
		}
		item.Amount = Decimal(amount)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate institution consumptions: %w", err)
	}
	return items, nil
}

// ---------------------------------------------------------------------------
// 建档所需的查重与字典解析
// ---------------------------------------------------------------------------

// ListInstitutionCodes 返回某个代码前缀下的已有机构代码（用于流水号递增）。
func (s *Store) ListInstitutionCodes(ctx context.Context, upperPrefix string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
SELECT code
FROM tenants
WHERE upper(COALESCE(code, '')) LIKE $1 || '%'`, upperPrefix)
	if err != nil {
		return nil, fmt.Errorf("list institution codes: %w", err)
	}
	defer rows.Close()

	codes := make([]string, 0, 16)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan institution code: %w", err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate institution codes: %w", err)
	}
	return codes, nil
}

// InstitutionCodeExists 判断机构代码（忽略大小写）是否已存在。
func (s *Store) InstitutionCodeExists(ctx context.Context, code string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM tenants WHERE lower(COALESCE(code, '')) = lower($1))`, code).Scan(&exists); err != nil {
		return false, fmt.Errorf("check institution code: %w", err)
	}
	return exists, nil
}

// InstitutionPhoneOwner 返回占用该手机号的机构名（用于冲突提示）。
func (s *Store) InstitutionPhoneOwner(ctx context.Context, phone string) (string, bool, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
SELECT COALESCE(t.name, '')
FROM employees e
JOIN tenants t ON t.id = e.tenant_id
WHERE e.phone = $1
  AND e.deleted_at IS NULL
ORDER BY e.id
LIMIT 1`, phone).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("check institution phone: %w", err)
	}
	return name, true, nil
}

// GetIndustryByName 按名称查行业字典（返回 id 是否存在）。
func (s *Store) GetIndustryByName(ctx context.Context, name string) (int64, bool, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM billing_industry_dict WHERE name = $1`, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("resolve industry: %w", err)
	}
	return id, true, nil
}

// GetDepartmentIDsByNames 按名称批量解析科室 id；返回 name -> id（不存在的名称不在结果里）。
func (s *Store) GetDepartmentIDsByNames(ctx context.Context, names []string) (map[string]int64, error) {
	result := make(map[string]int64, len(names))
	if len(names) == 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT name, id FROM organization_department_dict WHERE name = ANY($1)`, names)
	if err != nil {
		return nil, fmt.Errorf("resolve departments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			name string
			id   int64
		)
		if err := rows.Scan(&name, &id); err != nil {
			return nil, fmt.Errorf("scan department: %w", err)
		}
		result[name] = id
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate departments: %w", err)
	}
	return result, nil
}

// channelRef 是渠道（ops_organizations）的最小引用。
type channelRef struct {
	ID     int64
	Name   string
	Type   string
	Status string
}

// GetChannelByName 按名称查渠道（停用的渠道不能用于新归属）。
func (s *Store) GetChannelByName(ctx context.Context, name string) (channelRef, bool, error) {
	var item channelRef
	err := s.pool.QueryRow(ctx, `
SELECT id, COALESCE(name, ''), COALESCE(type, 'agency'), COALESCE(status, 'active')
FROM ops_organizations
WHERE lower(COALESCE(name, '')) = lower($1)
  AND COALESCE(status, 'active') <> 'disabled'
ORDER BY id
LIMIT 1`, strings.TrimSpace(name)).Scan(&item.ID, &item.Name, &item.Type, &item.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return channelRef{}, false, nil
	}
	if err != nil {
		return channelRef{}, false, fmt.Errorf("resolve channel: %w", err)
	}
	return item, true, nil
}

// salesRef 是销售（operations_admins）的最小引用。
type salesRef struct {
	ID     int64
	Name   string
	Phone  string
	OrgID  *int64
	Active bool
}

// GetSalesByNameAndOrg 在指定渠道下按姓名查销售；orgID 为 0 表示不限渠道。
func (s *Store) GetSalesByNameAndOrg(ctx context.Context, orgID int64, name string) (salesRef, bool, error) {
	var (
		item   salesRef
		active bool
	)
	err := s.pool.QueryRow(ctx, `
SELECT id, COALESCE(name, ''), COALESCE(phone, ''), org_id,
       CASE WHEN lower(COALESCE(is_active::text, 'true')) IN ('1', 't', 'true', 'yes') THEN TRUE ELSE FALSE END
FROM operations_admins
WHERE lower(COALESCE(name, '')) = lower($1)
  AND ($2::bigint = 0 OR org_id = $2)
  AND deleted_at IS NULL
ORDER BY id
LIMIT 1`, strings.TrimSpace(name), orgID).Scan(&item.ID, &item.Name, &item.Phone, &item.OrgID, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return salesRef{}, false, nil
	}
	if err != nil {
		return salesRef{}, false, fmt.Errorf("resolve sales: %w", err)
	}
	item.Active = active
	return item, true, nil
}

// UpdateInstitutionProfileTx 回填 tenants 上的档案列（建档第二步）。
func (s *Store) UpdateInstitutionProfileTx(ctx context.Context, tx pgx.Tx, tenantID int64, req CreateInstitutionRequest) error {
	_, err := tx.Exec(ctx, `
UPDATE tenants
SET legal_name = $2,
    contact_title = $3,
    province_code = $4,
    city_code = $5,
    district_code = $6,
    updated_at = NOW()
WHERE id = $1`, tenantID, req.Legal, req.Title, req.Region.Province, req.Region.City, req.Region.District)
	if err != nil {
		return fmt.Errorf("update institution profile: %w", err)
	}
	return nil
}

// UpdateInstitutionProfileFieldsTx 编辑机构档案时按需更新计费档案列。
// 未传（nil）的列保持原值；billing_mode / code 不在可写范围内。
func (s *Store) UpdateInstitutionProfileFieldsTx(ctx context.Context, tx pgx.Tx, tenantID int64, req UpdateInstitutionRequest) error {
	_, err := tx.Exec(ctx, `
UPDATE tenants
SET legal_name = COALESCE($2, legal_name),
    contact_title = COALESCE($3, contact_title),
    province_code = COALESCE($4, province_code),
    city_code = COALESCE($5, city_code),
    district_code = COALESCE($6, district_code),
    customer_type = COALESCE($7, customer_type),
    updated_at = NOW()
WHERE id = $1
  AND deleted_at IS NULL`,
		tenantID, req.LegalName, req.ContactTitle,
		req.ProvinceCode, req.CityCode, req.DistrictCode, req.CustomerType)
	if err != nil {
		return fmt.Errorf("update institution profile fields: %w", err)
	}
	return nil
}

// GetExistingDepartmentIDs 返回给定科室 id 中真实存在的集合（编辑档案时校验用）。
func (s *Store) GetExistingDepartmentIDs(ctx context.Context, ids []int64) (map[int64]struct{}, error) {
	result := make(map[int64]struct{}, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM organization_department_dict WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("check department ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan department id: %w", err)
		}
		result[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate department ids: %w", err)
	}
	return result, nil
}

// ListTenantDepartmentIDs 返回机构当前绑定科室的 id（写变更日志用）。
func (s *Store) ListTenantDepartmentIDs(ctx context.Context, tenantID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
SELECT department_id
FROM tenant_departments
WHERE tenant_id = $1
ORDER BY department_id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant department ids: %w", err)
	}
	defer rows.Close()
	ids := make([]int64, 0, 8)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan tenant department id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant department ids: %w", err)
	}
	return ids, nil
}

// ReplaceTenantDepartmentsTx 覆盖机构的涉及科室。
func (s *Store) ReplaceTenantDepartmentsTx(ctx context.Context, tx pgx.Tx, tenantID int64, departmentIDs []int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM tenant_departments WHERE tenant_id = $1`, tenantID); err != nil {
		return fmt.Errorf("clear tenant departments: %w", err)
	}
	if len(departmentIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO tenant_departments (tenant_id, department_id)
SELECT $1, unnest($2::bigint[])
ON CONFLICT DO NOTHING`, tenantID, departmentIDs); err != nil {
		return fmt.Errorf("insert tenant departments: %w", err)
	}
	// 勾选的涉及科室同步为机构部门（已存在则跳过）。
	if _, err := tx.Exec(ctx, `
INSERT INTO departments (tenant_id, name, code, dict_code, parent_id, is_active, created_at, updated_at)
SELECT $1, od.name, od.code, od.code, NULL, TRUE, NOW(), NOW()
FROM organization_department_dict od
WHERE od.id = ANY($2::bigint[])
  AND NOT EXISTS (
      SELECT 1 FROM departments d
      WHERE d.tenant_id = $1 AND d.deleted_at IS NULL AND (d.dict_code = od.code OR d.code = od.code)
  )
ON CONFLICT DO NOTHING`, tenantID, departmentIDs); err != nil {
		return fmt.Errorf("sync tenant departments to departments: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 归属
// ---------------------------------------------------------------------------

// ownershipRow 是归属的一行（含组织与销售名称）。
type ownershipRow struct {
	ID            int64
	TenantID      int64
	OrgID         *int64
	OrgName       string
	SalesAdminID  *int64
	SalesName     string
	Source        string
	EffectiveFrom string
	EffectiveTo   *string
	CreatedAt     time.Time
}

const ownershipSelect = `
SELECT o.id, o.tenant_id, o.org_id, COALESCE(org.name, ''), o.sales_admin_id, COALESCE(sales.name, ''),
       o.source, to_char(o.effective_from, 'YYYY-MM-DD'), to_char(o.effective_to, 'YYYY-MM-DD'), o.created_at
FROM tenant_ownerships o
LEFT JOIN ops_organizations org ON org.id = o.org_id
LEFT JOIN operations_admins sales ON sales.id = o.sales_admin_id AND sales.deleted_at IS NULL
`

// ListOwnershipRows 取租户的归属历史（当前有效在前）。
func (s *Store) ListOwnershipRows(ctx context.Context, tenantID int64) ([]ownershipRow, error) {
	rows, err := s.pool.Query(ctx, ownershipSelect+`
WHERE o.tenant_id = $1
ORDER BY (o.effective_to IS NULL) DESC, o.effective_from DESC, o.id DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list ownerships: %w", err)
	}
	defer rows.Close()

	items := make([]ownershipRow, 0, 4)
	for rows.Next() {
		item, err := scanOwnership(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan ownership: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ownerships: %w", err)
	}
	return items, nil
}

// GetOwnershipRow 取租户的一条归属。
func (s *Store) GetOwnershipRow(ctx context.Context, tenantID, ownershipID int64) (ownershipRow, error) {
	item, err := scanOwnership(s.pool.QueryRow(ctx, ownershipSelect+` WHERE o.tenant_id = $1 AND o.id = $2`, tenantID, ownershipID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return ownershipRow{}, ErrPeerNotFound
	}
	if err != nil {
		return ownershipRow{}, fmt.Errorf("get ownership: %w", err)
	}
	return item, nil
}

// GetCurrentOwnershipRow 取租户当前有效归属；没有返回 (nil, nil)。
func (s *Store) GetCurrentOwnershipRow(ctx context.Context, tenantID int64) (*ownershipRow, error) {
	item, err := scanOwnership(s.pool.QueryRow(ctx, ownershipSelect+`
WHERE o.tenant_id = $1 AND o.effective_to IS NULL
ORDER BY o.effective_from DESC, o.id DESC
LIMIT 1`, tenantID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get current ownership: %w", err)
	}
	return &item, nil
}

func scanOwnership(scan func(dest ...any) error) (ownershipRow, error) {
	var item ownershipRow
	if err := scan(&item.ID, &item.TenantID, &item.OrgID, &item.OrgName, &item.SalesAdminID, &item.SalesName,
		&item.Source, &item.EffectiveFrom, &item.EffectiveTo, &item.CreatedAt); err != nil {
		return ownershipRow{}, err
	}
	return item, nil
}

// CloseCurrentOwnershipTx 关闭当前有效归属（effective_to = closeDate）。
func (s *Store) CloseCurrentOwnershipTx(ctx context.Context, tx pgx.Tx, tenantID int64, closeDate time.Time) error {
	_, err := tx.Exec(ctx, `
UPDATE tenant_ownerships
SET effective_to = GREATEST(effective_from, $2::date),
    updated_at = NOW()
WHERE tenant_id = $1
  AND effective_to IS NULL`, tenantID, formatDateOnly(closeDate))
	if err != nil {
		return fmt.Errorf("close current ownership: %w", err)
	}
	return nil
}

// InsertOwnershipTx 插入一条归属（effective_to 为空表示当前有效）。
func (s *Store) InsertOwnershipTx(ctx context.Context, tx pgx.Tx, tenantID, orgID, salesAdminID, createdBy int64, source string, effectiveFrom time.Time) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
INSERT INTO tenant_ownerships (tenant_id, org_id, sales_admin_id, source, effective_from, created_by)
VALUES ($1, $2, $3, $4, $5::date, $6)
RETURNING id`, tenantID, nullableID(orgID), nullableID(salesAdminID), source, formatDateOnly(effectiveFrom), nullableID(createdBy)).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrPeerConflict
		}
		return 0, fmt.Errorf("insert ownership: %w", err)
	}
	return id, nil
}

// UpdateOwnershipTx 修改归属的组织/销售/来源。
func (s *Store) UpdateOwnershipTx(ctx context.Context, tx pgx.Tx, tenantID, ownershipID, orgID, salesAdminID int64, source string) error {
	tag, err := tx.Exec(ctx, `
UPDATE tenant_ownerships
SET org_id = $3,
    sales_admin_id = $4,
    source = $5,
    updated_at = NOW()
WHERE tenant_id = $1 AND id = $2`, tenantID, ownershipID, nullableID(orgID), nullableID(salesAdminID), source)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrPeerConflict
		}
		return fmt.Errorf("update ownership: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPeerNotFound
	}
	return nil
}

// CloseOwnershipRowTx 关闭指定归属。
func (s *Store) CloseOwnershipRowTx(ctx context.Context, tx pgx.Tx, tenantID, ownershipID int64, effectiveTo time.Time) error {
	tag, err := tx.Exec(ctx, `
UPDATE tenant_ownerships
SET effective_to = GREATEST(effective_from, $3::date),
    updated_at = NOW()
WHERE tenant_id = $1 AND id = $2 AND effective_to IS NULL`, tenantID, ownershipID, formatDateOnly(effectiveTo))
	if err != nil {
		return fmt.Errorf("close ownership: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// 已经关闭（或不存在）：区分「不存在」与「已关闭」。
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tenant_ownerships WHERE tenant_id = $1 AND id = $2)`, tenantID, ownershipID).Scan(&exists); err != nil {
			return fmt.Errorf("check ownership: %w", err)
		}
		if !exists {
			return ErrPeerNotFound
		}
	}
	return nil
}

// UpdateOwnershipEffectiveFromTx 修改归属生效日（历史修正用）。
func (s *Store) UpdateOwnershipEffectiveFromTx(ctx context.Context, tx pgx.Tx, tenantID, ownershipID int64, effectiveFrom time.Time) error {
	tag, err := tx.Exec(ctx, `
UPDATE tenant_ownerships
SET effective_from = $3::date,
    updated_at = NOW()
WHERE tenant_id = $1 AND id = $2`, tenantID, ownershipID, formatDateOnly(effectiveFrom))
	if err != nil {
		return fmt.Errorf("update ownership effective from: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPeerNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// 服务期
// ---------------------------------------------------------------------------

// periodRow 是服务期的一行。
type periodRow struct {
	ID        int64
	TenantID  int64
	StartedOn string
	ExpiredOn string
	Status    string
	Source    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

const periodSelect = `
SELECT id, tenant_id, to_char(started_on, 'YYYY-MM-DD'), to_char(expired_on, 'YYYY-MM-DD'),
       status, source, created_at, updated_at
FROM subscription_periods
`

// ListPeriodRows 取租户的服务期历史（最近的在前）。
func (s *Store) ListPeriodRows(ctx context.Context, tenantID int64) ([]periodRow, error) {
	rows, err := s.pool.Query(ctx, periodSelect+`
WHERE tenant_id = $1
ORDER BY (status = 'active') DESC, (status = 'expired') DESC, id DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list subscription periods: %w", err)
	}
	defer rows.Close()

	items := make([]periodRow, 0, 4)
	for rows.Next() {
		item, err := scanPeriod(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan subscription period: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscription periods: %w", err)
	}
	return items, nil
}

// GetPeriodRow 取一条服务期。
func (s *Store) GetPeriodRow(ctx context.Context, tenantID, periodID int64) (periodRow, error) {
	item, err := scanPeriod(s.pool.QueryRow(ctx, periodSelect+` WHERE tenant_id = $1 AND id = $2`, tenantID, periodID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return periodRow{}, ErrPeerNotFound
	}
	if err != nil {
		return periodRow{}, fmt.Errorf("get subscription period: %w", err)
	}
	return item, nil
}

func scanPeriod(scan func(dest ...any) error) (periodRow, error) {
	var item periodRow
	if err := scan(&item.ID, &item.TenantID, &item.StartedOn, &item.ExpiredOn,
		&item.Status, &item.Source, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return periodRow{}, err
	}
	return item, nil
}

// ---------------------------------------------------------------------------
// 运营账号数据权限
// ---------------------------------------------------------------------------

// AdminScope 是运营账号的数据权限范围。
type AdminScope struct {
	AdminID   int64
	OrgID     *int64
	OrgName   string
	OrgType   string
	CanSeeAll bool
}

// GetAdminScope 取运营账号的归属组织与「能否看全部」：
// ops_super_admin 角色或 platform 组织 -> 可看全部；其余按 org_id 收敛。
// 结果进程内缓存 60 秒：远程库每次往返约 70-90ms，列表/详情会各调一次。
func (s *Store) GetAdminScope(ctx context.Context, adminID int64) (AdminScope, error) {
	s.scopeMu.Lock()
	if cached, ok := s.scopeCache[adminID]; ok && time.Now().Before(cached.expires) {
		s.scopeMu.Unlock()
		return cached.scope, nil
	}
	s.scopeMu.Unlock()

	var (
		scope   AdminScope
		orgType string
		isSuper bool
	)
	err := s.pool.QueryRow(ctx, `
SELECT a.id,
       a.org_id,
       COALESCE(o.name, ''),
       COALESCE(o.type, ''),
       EXISTS (
           SELECT 1
           FROM operations_admin_roles ar
           JOIN operations_roles r ON r.id = ar.role_id
           WHERE ar.admin_id = a.id
             AND r.deleted_at IS NULL
             AND r.code = 'ops_super_admin'
       )
FROM operations_admins a
LEFT JOIN ops_organizations o ON o.id = a.org_id
WHERE a.id = $1
  AND a.deleted_at IS NULL`, adminID).Scan(&scope.AdminID, &scope.OrgID, &scope.OrgName, &orgType, &isSuper)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminScope{}, ErrNoPermission
	}
	if err != nil {
		return AdminScope{}, fmt.Errorf("get admin scope: %w", err)
	}
	scope.OrgType = normalizeEnum(orgType)
	scope.CanSeeAll = isSuper || scope.OrgType == opsOrgTypePlatform
	if scope.OrgID == nil || *scope.OrgID <= 0 {
		scope.OrgType = ""
		if !scope.CanSeeAll {
			return AdminScope{}, ErrAdminScopeMissing
		}
	}

	s.scopeMu.Lock()
	if s.scopeCache == nil {
		s.scopeCache = make(map[int64]cachedAdminScope)
	}
	s.scopeCache[adminID] = cachedAdminScope{scope: scope, expires: time.Now().Add(60 * time.Second)}
	s.scopeMu.Unlock()
	return scope, nil
}

// TenantVisibleToOrg 判断租户是否属于该组织（当前归属或历史试用归属）。
func (s *Store) TenantVisibleToOrg(ctx context.Context, tenantID, orgID int64) (bool, error) {
	if orgID <= 0 {
		return false, nil
	}
	var visible bool
	err := s.pool.QueryRow(ctx, `
SELECT EXISTS (
           SELECT 1 FROM tenant_ownerships o
           WHERE o.tenant_id = $1 AND o.effective_to IS NULL AND o.org_id = $2
       )
       OR EXISTS (
           SELECT 1 FROM trial_customer_ownerships tc
           WHERE tc.tenant_id = $1 AND tc.owner_org_id = $2
       )`, tenantID, orgID).Scan(&visible)
	if err != nil {
		return false, fmt.Errorf("check tenant visibility: %w", err)
	}
	return visible, nil
}

// nullableID 把 0 转成 SQL NULL，避免外键/可空列写入 0。
func nullableID(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}
