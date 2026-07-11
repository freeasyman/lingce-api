package tenant

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type OpsOrganization struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	ParentID  *int64     `json:"parent_id,omitempty"`
	Status    string     `json:"status"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type AdminOrgScope struct {
	AdminID   int64
	OrgID     *int64
	OrgName   string
	OrgType   string
	CanSeeAll bool
}

func normalizeOrgType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "platform":
		return "platform"
	default:
		return "agency"
	}
}

func (s *Store) ListOpsOrganizations(ctx context.Context) ([]*OpsOrganization, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, COALESCE(type, 'agency') AS type, parent_id, COALESCE(status, 'active') AS status, created_at, updated_at
		FROM ops_organizations
		WHERE COALESCE(status, 'active') <> 'disabled'
		ORDER BY CASE WHEN COALESCE(type, 'agency') = 'platform' THEN 0 ELSE 1 END, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list ops organizations: %w", err)
	}
	defer rows.Close()

	items := make([]*OpsOrganization, 0)
	for rows.Next() {
		item := &OpsOrganization{}
		if err := rows.Scan(&item.ID, &item.Name, &item.Type, &item.ParentID, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan ops organization: %w", err)
		}
		item.Type = normalizeOrgType(item.Type)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ops organizations: %w", err)
	}
	return items, nil
}

func (s *Store) GetOpsOrganizationByID(ctx context.Context, orgID int64) (*OpsOrganization, error) {
	item := &OpsOrganization{}
	if err := s.pool.QueryRow(ctx, `
		SELECT id, name, COALESCE(type, 'agency') AS type, parent_id, COALESCE(status, 'active') AS status, created_at, updated_at
		FROM ops_organizations
		WHERE id = $1
	`, orgID).Scan(&item.ID, &item.Name, &item.Type, &item.ParentID, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, fmt.Errorf("get ops organization: %w", err)
	}
	item.Type = normalizeOrgType(item.Type)
	return item, nil
}

func (s *Store) GetAdminOrgScope(ctx context.Context, adminID int64) (*AdminOrgScope, error) {
	scope := &AdminOrgScope{AdminID: adminID}
	if err := s.pool.QueryRow(ctx, `
		SELECT a.id,
		       a.org_id,
		       COALESCE(o.name, '') AS org_name,
		       COALESCE(o.type, '') AS org_type
		FROM operations_admins a
		LEFT JOIN ops_organizations o ON o.id = a.org_id
		WHERE a.id = $1
		  AND a.deleted_at IS NULL
	`, adminID).Scan(&scope.AdminID, &scope.OrgID, &scope.OrgName, &scope.OrgType); err != nil {
		return nil, fmt.Errorf("get admin org scope: %w", err)
	}
	if scope.OrgID != nil && *scope.OrgID > 0 {
		scope.OrgType = normalizeOrgType(scope.OrgType)
		scope.CanSeeAll = scope.OrgType == "platform"
	} else {
		scope.OrgType = ""
		scope.OrgName = ""
		scope.CanSeeAll = false
	}
	return scope, nil
}
