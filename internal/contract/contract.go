// Package contract 是租户服务期有效性的唯一判断入口。
//
// 规则只有一条：今天的日期落在哪些 active 合同行里，那些行就有效。
// 登录、录音、扣量、运营台等所有业务判断有效期时，只允许调用本包的
// Effective / CanAccess / Primary，禁止各自写日期比较 SQL。
package contract

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Type 合同类型。新增运营方案 = 新增一种 Type，判定逻辑无需改动。
type Type string

const (
	// Formal 正式合同（收费）。
	Formal Type = "formal"
	// Trial 试用合同（白送，到期即失效）。
	Trial Type = "trial"
)

// Contract 一行合同。
type Contract struct {
	ID        int64     `json:"id"`
	TenantID  int64     `json:"tenant_id"`
	Type      Type      `json:"type"`
	StartedOn time.Time `json:"started_on"`
	ExpiredOn time.Time `json:"expired_on"`
	Status    string    `json:"status"`
}

// Queryer 是 contract 包允许使用的数据库访问面（*pgxpool.Pool 与 pgx.Tx 均满足）。
type Queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const effectiveSQL = `
SELECT id, tenant_id, contract_type, started_on, expired_on, status
FROM subscription_periods
WHERE tenant_id = $1
  AND status = 'active'
  AND started_on <= $2::date
  AND expired_on >= $2::date
ORDER BY expired_on DESC`

// Effective 返回今天覆盖的合同行（可能多条：正式/试用与体验并行）。
func Effective(ctx context.Context, q Queryer, tenantID int64, now time.Time) ([]Contract, error) {
	rows, err := q.Query(ctx, effectiveSQL, tenantID, now)
	if err != nil {
		return nil, fmt.Errorf("contract: effective: %w", err)
	}
	defer rows.Close()
	var out []Contract
	for rows.Next() {
		var c Contract
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Type, &c.StartedOn, &c.ExpiredOn, &c.Status); err != nil {
			return nil, fmt.Errorf("contract: scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("contract: iterate: %w", err)
	}
	return out, nil
}

// CanAccess 今天是否至少有一份生效合同。
func CanAccess(ctx context.Context, q Queryer, tenantID int64, now time.Time) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
SELECT EXISTS (
	SELECT 1 FROM subscription_periods
	WHERE tenant_id = $1
	  AND status = 'active'
	  AND started_on <= $2::date
	  AND expired_on >= $2::date
)`, tenantID, now).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("contract: can access: %w", err)
	}
	return ok, nil
}

// HasPeriods 判断租户是否已有服务期（合同）记录。
//
// 这是「合同优先、老表兜底」的分叉点：只要租户出现过服务期记录，
// 服务期判定就以合同为准，不再回退到 tenants.valid_to。
func HasPeriods(ctx context.Context, q Queryer, tenantID int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM subscription_periods WHERE tenant_id = $1)
`, tenantID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("contract: has periods: %w", err)
	}
	return ok, nil
}

// ProductActive 判断租户当前是否有指定产品的生效额度——即「当前覆盖合同开通了该产品」。
// 这是新产品侧判断「产品权限（合同维度）」的唯一接口；各产品入口只传产品码。
func ProductActive(ctx context.Context, q Queryer, tenantID int64, productCode string, now time.Time) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM product_entitlements e
    JOIN subscription_periods p ON p.id = e.period_id
    WHERE e.tenant_id = $1
      AND e.product_code = $2
      AND e.status = 'active'
      AND p.status = 'active'
      AND p.started_on <= $3::date
      AND p.expired_on >= $3::date
)`, tenantID, productCode, now).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("contract: product active: %w", err)
	}
	return ok, nil
}

// Primary 返回主体合同（formal/trial 中覆盖今天的那条；没有则返回 nil）。
func Primary(ctx context.Context, q Queryer, tenantID int64, now time.Time) (*Contract, error) {
	var c Contract
	err := q.QueryRow(ctx, `
SELECT id, tenant_id, contract_type, started_on, expired_on, status
FROM subscription_periods
WHERE tenant_id = $1
  AND status = 'active'
  AND contract_type IN ('formal', 'trial')
  AND started_on <= $2::date
  AND expired_on >= $2::date
ORDER BY expired_on DESC
LIMIT 1`, tenantID, now).Scan(&c.ID, &c.TenantID, &c.Type, &c.StartedOn, &c.ExpiredOn, &c.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("contract: primary: %w", err)
	}
	return &c, nil
}
