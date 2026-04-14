package tenancy

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Scope struct {
	TenantID  *int64
	TenantIDs []int64
}

func ResolveScope(ctx context.Context, pool *pgxpool.Pool, claims *auth.Claims, tenantIDParam string) (*Scope, error) {
	if claims == nil {
		return nil, fmt.Errorf("invalid token")
	}

	tenantIDParam = strings.TrimSpace(tenantIDParam)
	requestedTenantID, err := parseTenantIDParam(tenantIDParam)
	if err != nil {
		return nil, err
	}

	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || *claims.TenantID <= 0 {
			return nil, fmt.Errorf("no tenant access")
		}
		if requestedTenantID != nil && *requestedTenantID != *claims.TenantID {
			return nil, fmt.Errorf("access denied")
		}
		tid := *claims.TenantID
		return &Scope{
			TenantID:  &tid,
			TenantIDs: []int64{tid},
		}, nil
	}

	visibleTenantIDs, err := loadAdminVisibleTenantIDs(ctx, pool, claims)
	if err != nil {
		return nil, err
	}
	if len(visibleTenantIDs) == 0 {
		return &Scope{
			TenantID:  nil,
			TenantIDs: []int64{},
		}, nil
	}

	if requestedTenantID != nil {
		if !containsTenantID(visibleTenantIDs, *requestedTenantID) {
			return nil, fmt.Errorf("access denied")
		}
		return &Scope{
			TenantID:  requestedTenantID,
			TenantIDs: []int64{*requestedTenantID},
		}, nil
	}

	return &Scope{
		TenantID:  nil,
		TenantIDs: visibleTenantIDs,
	}, nil
}

func parseTenantIDParam(raw string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	tenantID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || tenantID <= 0 {
		return nil, fmt.Errorf("invalid tenant_id")
	}
	return &tenantID, nil
}

func loadAdminVisibleTenantIDs(ctx context.Context, pool *pgxpool.Pool, claims *auth.Claims) ([]int64, error) {
	// If token is tenant-bound admin, restrict to that tenant.
	if claims.TenantID != nil && *claims.TenantID > 0 {
		return []int64{*claims.TenantID}, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT id
		FROM tenants
		WHERE deleted_at IS NULL
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to load visible tenants: %w", err)
	}
	defer rows.Close()

	tenantIDs := make([]int64, 0, 32)
	for rows.Next() {
		var tenantID int64
		if err := rows.Scan(&tenantID); err != nil {
			return nil, fmt.Errorf("failed to scan visible tenant: %w", err)
		}
		tenantIDs = append(tenantIDs, tenantID)
	}

	return tenantIDs, nil
}

func containsTenantID(tenantIDs []int64, target int64) bool {
	for _, tenantID := range tenantIDs {
		if tenantID == target {
			return true
		}
	}
	return false
}

// Context key for tenant scope
type contextKey string

const scopeContextKey contextKey = "tenant_scope"

// WithScope adds a scope to the context
func WithScope(ctx context.Context, scope *Scope) context.Context {
	return context.WithValue(ctx, scopeContextKey, scope)
}

// GetScope retrieves the scope from the context
func GetScope(ctx context.Context) *Scope {
	scope, _ := ctx.Value(scopeContextKey).(*Scope)
	return scope
}
