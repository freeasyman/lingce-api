package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 是 service 层依赖的数据访问接口。
// 用接口隔离后，service/handler 的单元测试不需要真实数据库。
type Repository interface {
	ListCatalog(ctx context.Context, filter CatalogFilter) ([]CatalogItem, error)
	InsertCatalogVersion(ctx context.Context, input CatalogVersionInput) (CatalogItem, error)
	ListEntitlements(ctx context.Context, tenantID int64, product string, includeZero bool) ([]Entitlement, error)
	GetServicePeriod(ctx context.Context, tenantID int64) (*ServicePeriod, error)
	ListLedger(ctx context.Context, query LedgerQuery) ([]LedgerEntry, int64, error)

	// 阶段 C：订单录入、回款确认、开通与退款。
	InsertOrder(ctx context.Context, input CreateOrderInput) (Order, error)
	ListOrders(ctx context.Context, query OrderQuery) ([]Order, int64, error)
	GetOrder(ctx context.Context, orderID int64) (Order, error)
	SettleOrder(ctx context.Context, input SettleOrderInput) (Order, error)
	ActivateOrder(ctx context.Context, input ActivateOrderInput) (Order, ServicePeriod, error)
	RefundOrder(ctx context.Context, input RefundOrderInput) (Order, error)
	GetAdminDisplayName(ctx context.Context, adminID int64) (string, error)

	// 阶段 D：扣量。BeginDeduct 开启一个事务，后续锁行/流水/更新/读回都在同事务内完成。
	BeginDeduct(ctx context.Context) (DeductTx, error)
}

// Store 是 Repository 的 pgx 实现：读价目表/额度/流水，写订单、服务期、额度与流水。
type Store struct{ pool *pgxpool.Pool }

// NewStore 构造基于连接池的存储层。
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

var _ Repository = (*Store)(nil)

// catalogColumns 是价目表查询/回写的公共列，金额统一转 text 由 Decimal 承载，避免 float64 丢精度。
const catalogColumns = `product_code, product_name, edition, price_version,
       annual_price::text, COALESCE(quotas, '{}'::jsonb)::text,
       increment_price::text, COALESCE(increments, '{}'::jsonb)::text, is_active`

// listCatalogSQL 返回过滤后的全部版本，由 service 层按 product+edition 选最新版本。
// 这样"版本过滤"是可单测的纯逻辑，不依赖 SQL 方言。
const listCatalogSQL = `
SELECT ` + catalogColumns + `
FROM product_catalog
WHERE ($1::text = '' OR product_code = $1)
  AND ($2::text = '' OR edition = $2)
  AND ($3::text = '' OR price_version = $3)
ORDER BY product_code, edition, price_version DESC, created_at DESC, id DESC
`

// ListCatalog 按条件查询价目表（多版本按 price_version 倒序返回）。
func (s *Store) ListCatalog(ctx context.Context, filter CatalogFilter) ([]CatalogItem, error) {
	rows, err := s.pool.Query(ctx, listCatalogSQL, filter.ProductCode, filter.Edition, filter.PriceVersion)
	if err != nil {
		return nil, fmt.Errorf("query product catalog: %w", err)
	}
	defer rows.Close()

	items := make([]CatalogItem, 0, 16)
	for rows.Next() {
		item, err := scanCatalogItem(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan product catalog: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate product catalog: %w", err)
	}
	return items, nil
}

// insertCatalogVersionSQL 只做 INSERT：已有版本不会 UPDATE，历史价目保持可追溯。
// product_name 在 Go 侧解析（见 resolveProductName），避免同一参数在 varchar 与 text
// 两种上下文里被 PostgreSQL 推断成冲突类型（SQLSTATE 42P08）。
const insertCatalogVersionSQL = `
INSERT INTO product_catalog (product_code, product_name, edition, price_version, annual_price, quotas, increment_price, increments)
VALUES ($1, $2, $3, $4, $5::numeric, $6::jsonb, $7::numeric, $8::jsonb)
RETURNING ` + catalogColumns

// lookupProductNameSQL 取同产品任意一条非空产品名，用于新版本继承名字。
const lookupProductNameSQL = `
SELECT product_name
FROM product_catalog
WHERE product_code = $1
  AND product_name <> ''
ORDER BY price_version DESC, id DESC
LIMIT 1
`

// InsertCatalogVersion 新增一个价目版本；唯一键冲突返回 ErrCatalogVersionExists。
func (s *Store) InsertCatalogVersion(ctx context.Context, input CatalogVersionInput) (CatalogItem, error) {
	productName, err := s.resolveProductName(ctx, input)
	if err != nil {
		return CatalogItem{}, err
	}
	row := s.pool.QueryRow(ctx, insertCatalogVersionSQL,
		input.ProductCode,
		productName,
		input.Edition,
		input.PriceVersion,
		input.AnnualPrice.String(),
		quantitiesJSON(input.Quotas),
		input.IncrementPrice.String(),
		quantitiesJSON(input.Increments),
	)
	item, err := scanCatalogItem(row.Scan)
	if err != nil {
		if isUniqueViolation(err) {
			return CatalogItem{}, ErrCatalogVersionExists
		}
		return CatalogItem{}, fmt.Errorf("insert product catalog version: %w", err)
	}
	return item, nil
}

// resolveProductName 返回新版本应写入的产品名：请求显式给了就用请求值，
// 否则沿用同产品已有版本的名字，最后退回 product_code（保证价目表不出现空名字）。
func (s *Store) resolveProductName(ctx context.Context, input CatalogVersionInput) (string, error) {
	if name := strings.TrimSpace(input.ProductName); name != "" {
		return name, nil
	}
	var name string
	if err := s.pool.QueryRow(ctx, lookupProductNameSQL, input.ProductCode).Scan(&name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return input.ProductCode, nil
		}
		return "", fmt.Errorf("lookup product name: %w", err)
	}
	return name, nil
}

// listEntitlementsSQL 按租户查余额；product_name 从价目表任取一条非空名字（价目版本不参与额度归属）。
// include_zero 为 false 时过滤掉余额为 0 的行。
const listEntitlementsSQL = `
SELECT e.product_code,
       COALESCE(pc.product_name, ''),
       e.unit,
       e.edition,
       e.quota_type,
       e.granted::text,
       e.used::text,
       e.rollover_bal::text,
       e.balance::text,
       to_char(e.period_end, 'YYYY-MM-DD'),
       e.status
FROM product_entitlements e
LEFT JOIN LATERAL (
    SELECT c.product_name
      FROM product_catalog c
     WHERE c.product_code = e.product_code
       AND c.product_name <> ''
     ORDER BY c.price_version DESC, c.id DESC
     LIMIT 1
) pc ON TRUE
WHERE e.tenant_id = $1
  AND ($2::text = '' OR e.product_code = $2)
  AND ($3::boolean OR e.balance <> 0)
ORDER BY e.product_code, e.unit
`

// ListEntitlements 查询租户额度余额。
func (s *Store) ListEntitlements(ctx context.Context, tenantID int64, product string, includeZero bool) ([]Entitlement, error) {
	rows, err := s.pool.Query(ctx, listEntitlementsSQL, tenantID, product, includeZero)
	if err != nil {
		return nil, fmt.Errorf("query product entitlements: %w", err)
	}
	defer rows.Close()

	items := make([]Entitlement, 0, 16)
	for rows.Next() {
		var (
			item        Entitlement
			granted     string
			used        string
			rolloverBal string
			balance     string
		)
		if err := rows.Scan(
			&item.ProductCode,
			&item.ProductName,
			&item.Unit,
			&item.Edition,
			&item.QuotaType,
			&granted,
			&used,
			&rolloverBal,
			&balance,
			&item.PeriodEnd,
			&item.Status,
		); err != nil {
			return nil, fmt.Errorf("scan product entitlements: %w", err)
		}
		item.Granted = Decimal(granted)
		item.Used = Decimal(used)
		item.RolloverBal = Decimal(rolloverBal)
		item.Balance = Decimal(balance)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate product entitlements: %w", err)
	}
	return items, nil
}

// servicePeriodSQL 取租户最新一条订阅的服务期。开通时写 started_on/expired_on（见阶段 C）。
const servicePeriodSQL = `
SELECT to_char(s.started_on, 'YYYY-MM-DD'),
       to_char(s.expired_on, 'YYYY-MM-DD')
FROM tenant_subscriptions s
WHERE s.tenant_id = $1
ORDER BY s.created_at DESC NULLS LAST, s.id DESC
LIMIT 1
`

// GetServicePeriod 返回租户服务期；没有订阅时返回 (nil, nil)。
func (s *Store) GetServicePeriod(ctx context.Context, tenantID int64) (*ServicePeriod, error) {
	var period ServicePeriod
	if err := s.pool.QueryRow(ctx, servicePeriodSQL, tenantID).Scan(&period.StartedOn, &period.ExpiredOn); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query tenant service period: %w", err)
	}
	return &period, nil
}

// ledgerColumns 是流水查询的公共列，金额转 text 保留 decimal 精度。
const ledgerColumns = `id, product_code, unit, type, amount::text, source, reason, linked_order_id, operator, created_at`

// ListLedger 分页查询额度流水，返回当页数据与总条数。
func (s *Store) ListLedger(ctx context.Context, query LedgerQuery) ([]LedgerEntry, int64, error) {
	query = NormalizeLedgerQuery(query)

	conditions := []string{"tenant_id = $1"}
	args := []any{query.TenantID}
	if query.Product != "" {
		args = append(args, query.Product)
		conditions = append(conditions, fmt.Sprintf("product_code = $%d", len(args)))
	}
	if query.Unit != "" {
		args = append(args, query.Unit)
		conditions = append(conditions, fmt.Sprintf("unit = $%d", len(args)))
	}
	if query.From != nil {
		args = append(args, query.From.UTC())
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if query.To != nil {
		args = append(args, query.To.UTC())
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	where := strings.Join(conditions, " AND ")

	var total int64
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM entitlement_ledger WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count entitlement ledger: %w", err)
	}

	pageArgs := append(append(make([]any, 0, len(args)+2), args...), query.Offset(), int64(query.PageSize))
	pageSQL := fmt.Sprintf(`SELECT %s
FROM entitlement_ledger
WHERE %s
ORDER BY created_at DESC, id DESC
LIMIT $%d OFFSET $%d`, ledgerColumns, where, len(args)+1, len(args)+2)

	rows, err := s.pool.Query(ctx, pageSQL, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query entitlement ledger: %w", err)
	}
	defer rows.Close()

	items := make([]LedgerEntry, 0, query.PageSize)
	for rows.Next() {
		var (
			item   LedgerEntry
			amount string
		)
		if err := rows.Scan(
			&item.ID,
			&item.ProductCode,
			&item.Unit,
			&item.Type,
			&amount,
			&item.Source,
			&item.Reason,
			&item.LinkedOrderID,
			&item.Operator,
			&item.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan entitlement ledger: %w", err)
		}
		item.Amount = Decimal(amount)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate entitlement ledger: %w", err)
	}
	return items, total, nil
}

// scanCatalogItem 统一解析价目表一行（Query 与 QueryRow 共用）。
func scanCatalogItem(scan func(dest ...any) error) (CatalogItem, error) {
	var (
		item           CatalogItem
		annualPrice    string
		quotas         string
		incrementPrice string
		increments     string
	)
	if err := scan(
		&item.ProductCode,
		&item.ProductName,
		&item.Edition,
		&item.PriceVersion,
		&annualPrice,
		&quotas,
		&incrementPrice,
		&increments,
		&item.IsActive,
	); err != nil {
		return CatalogItem{}, err
	}
	item.AnnualPrice = Decimal(annualPrice)
	item.Quotas = Quantities(quotas)
	item.IncrementPrice = Decimal(incrementPrice)
	item.Increments = Quantities(increments)
	return item, nil
}

// quantitiesJSON 把 Quantities 转成 JSONB 参数文本，空值落库为 {}。
func quantitiesJSON(quantities Quantities) string {
	if len(quantities) == 0 {
		return "{}"
	}
	return string(quantities)
}

// isUniqueViolation 识别 PostgreSQL 唯一键冲突（product_catalog_identity_uq）。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
