package tenancy

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/auth"
)

var (
	errInvalidToken    = fmt.Errorf("invalid token")
	errNoTenantAccess  = fmt.Errorf("no tenant access")
	errTenantIDMissing = fmt.Errorf("tenant_id is required")
	errAccessDenied    = fmt.Errorf("access denied")
)

// RequireTenantID resolves the concrete tenant ID for a tenant-bound operation.
//
// Rules:
// - admin may explicitly select a tenant with tenant_id
// - admin without tenant_id falls back to the tenant bound to the token, if any
// - non-admin users must already be bound to a tenant and may not cross tenant boundaries
func RequireTenantID(claims *auth.Claims, tenantIDParam string) (int64, error) {
	if claims == nil {
		return 0, errInvalidToken
	}

	tenantIDParam = strings.TrimSpace(tenantIDParam)
	if claims.UserType == auth.UserTypeAdmin {
		if tenantIDParam != "" {
			tenantID, err := strconv.ParseInt(tenantIDParam, 10, 64)
			if err != nil || tenantID <= 0 {
				return 0, fmt.Errorf("invalid tenant_id")
			}
			return tenantID, nil
		}
		if claims.TenantID != nil && *claims.TenantID > 0 {
			return *claims.TenantID, nil
		}
		return 0, errTenantIDMissing
	}

	if claims.TenantID == nil || *claims.TenantID <= 0 {
		return 0, errNoTenantAccess
	}
	if tenantIDParam != "" {
		tenantID, err := strconv.ParseInt(tenantIDParam, 10, 64)
		if err != nil || tenantID <= 0 {
			return 0, fmt.Errorf("invalid tenant_id")
		}
		if tenantID != *claims.TenantID {
			return 0, errAccessDenied
		}
	}
	return *claims.TenantID, nil
}

// ResolveOptionalTenantID resolves an optional tenant filter for list-style endpoints.
//
// This is for admin-visible query filters where "no tenant_id" can mean
// "all tenants" instead of an error. If the token itself is tenant-bound,
// the bound tenant still wins to keep the response scoped.
func ResolveOptionalTenantID(claims *auth.Claims, tenantIDParam string, allowAdminAll bool) (*int64, error) {
	if claims == nil {
		return nil, errInvalidToken
	}

	tenantIDParam = strings.TrimSpace(tenantIDParam)
	if claims.UserType == auth.UserTypeAdmin && allowAdminAll && tenantIDParam == "" {
		if claims.TenantID != nil && *claims.TenantID > 0 {
			tenantID := *claims.TenantID
			return &tenantID, nil
		}
		return nil, nil
	}

	tenantID, err := RequireTenantID(claims, tenantIDParam)
	if err != nil {
		return nil, err
	}
	return &tenantID, nil
}

// RequireSameTenant enforces that a non-admin caller can only touch data from its own tenant.
func RequireSameTenant(claims *auth.Claims, targetTenantID int64) error {
	if claims == nil {
		return errInvalidToken
	}
	if claims.UserType == auth.UserTypeAdmin {
		return nil
	}
	if claims.TenantID == nil || *claims.TenantID <= 0 {
		return errNoTenantAccess
	}
	if *claims.TenantID != targetTenantID {
		return errAccessDenied
	}
	return nil
}

// RequirePositiveID is the common guard for identifiers that must be present
// and greater than zero before a service/store call is allowed to proceed.
func RequirePositiveID(field string, value int64) error {
	if value <= 0 {
		return fmt.Errorf("%s is required", field)
	}
	return nil
}
