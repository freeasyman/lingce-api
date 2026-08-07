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

// RequireTenantID resolves a concrete tenant ID for tenant-bound operations.
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

// RequireSameTenant ensures the target tenant matches the caller tenant unless caller is admin.
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
