package wecom

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) SaveSuiteTicket(ctx context.Context, record SuiteTicketRecord) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO wecom_suite_tickets (suite_id, suite_ticket, created_at) VALUES ($1, $2, NOW())`, record.SuiteID, record.SuiteTicket)
	return err
}

func (s *Store) GetLatestSuiteTicket(ctx context.Context, suiteID string) (string, error) {
	var ticket string
	err := s.pool.QueryRow(ctx, `SELECT suite_ticket FROM wecom_suite_tickets WHERE suite_id = $1 ORDER BY id DESC LIMIT 1`, suiteID).Scan(&ticket)
	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("suite ticket not found")
	}
	if err != nil {
		return "", err
	}
	return ticket, nil
}

func (s *Store) UpsertCorpInstall(ctx context.Context, record CorpInstallRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wecom_corp_installs (corp_id, corp_name, permanent_code, agent_id, status, created_at, updated_at, cancelled_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW(), NULL)
		ON CONFLICT (corp_id)
		DO UPDATE SET
			corp_name = EXCLUDED.corp_name,
			permanent_code = EXCLUDED.permanent_code,
			agent_id = EXCLUDED.agent_id,
			status = EXCLUDED.status,
			updated_at = NOW(),
			cancelled_at = NULL
	`, record.CorpID, record.CorpName, record.PermanentCode, record.AgentID, record.Status)
	return err
}

func (s *Store) GetCorpInstallByCorpID(ctx context.Context, corpID string) (*CorpInstallRecord, error) {
	var item CorpInstallRecord
	err := s.pool.QueryRow(ctx, `SELECT corp_id, COALESCE(corp_name, ''), permanent_code, COALESCE(agent_id, 0), COALESCE(status, 'active') FROM wecom_corp_installs WHERE corp_id = $1 AND COALESCE(status, 'active') = 'active'`, corpID).Scan(
		&item.CorpID,
		&item.CorpName,
		&item.PermanentCode,
		&item.AgentID,
		&item.Status,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("corp install not found")
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) MarkCorpInstallCancelled(ctx context.Context, corpID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE wecom_corp_installs SET status = 'cancelled', updated_at = NOW(), cancelled_at = NOW() WHERE corp_id = $1`, corpID)
	return err
}

func (s *Store) ListTenantApps(ctx context.Context, tenantID *int64) ([]*TenantWeComAppRecord, error) {
	where := "1=1"
	args := []interface{}{}
	if tenantID != nil && *tenantID > 0 {
		where = "tenant_id = $1"
		args = append(args, *tenantID)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, corp_id, COALESCE(corp_name, ''), COALESCE(agent_id, 0),
		       COALESCE(secret_ciphertext, ''), COALESCE(token, ''), COALESCE(encoding_aes_key, ''),
		       COALESCE(home_url, ''), COALESCE(trusted_domain, ''), COALESCE(jsapi_domain, ''),
		       COALESCE(enabled, false), COALESCE(config_confirmed, false), access_token, access_token_expired_at, last_sync_at,
		       created_at, updated_at
		FROM tenant_wecom_apps
		WHERE `+where+`
		ORDER BY updated_at DESC, id DESC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*TenantWeComAppRecord, 0)
	for rows.Next() {
		item, err := scanTenantApp(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetTenantAppByID(ctx context.Context, id int64) (*TenantWeComAppRecord, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, corp_id, COALESCE(corp_name, ''), COALESCE(agent_id, 0),
		       COALESCE(secret_ciphertext, ''), COALESCE(token, ''), COALESCE(encoding_aes_key, ''),
		       COALESCE(home_url, ''), COALESCE(trusted_domain, ''), COALESCE(jsapi_domain, ''),
		       COALESCE(enabled, false), COALESCE(config_confirmed, false), access_token, access_token_expired_at, last_sync_at,
		       created_at, updated_at
		FROM tenant_wecom_apps
		WHERE id = $1
	`, id)
	return scanTenantApp(row)
}

func (s *Store) GetTenantAppByCorpID(ctx context.Context, corpID string) (*TenantWeComAppRecord, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, corp_id, COALESCE(corp_name, ''), COALESCE(agent_id, 0),
		       COALESCE(secret_ciphertext, ''), COALESCE(token, ''), COALESCE(encoding_aes_key, ''),
		       COALESCE(home_url, ''), COALESCE(trusted_domain, ''), COALESCE(jsapi_domain, ''),
		       COALESCE(enabled, false), COALESCE(config_confirmed, false), access_token, access_token_expired_at, last_sync_at,
		       created_at, updated_at
		FROM tenant_wecom_apps
		WHERE corp_id = $1
	`, corpID)
	return scanTenantApp(row)
}

func (s *Store) GetTenantAppByTenantID(ctx context.Context, tenantID int64) (*TenantWeComAppRecord, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, corp_id, COALESCE(corp_name, ''), COALESCE(agent_id, 0),
		       COALESCE(secret_ciphertext, ''), COALESCE(token, ''), COALESCE(encoding_aes_key, ''),
		       COALESCE(home_url, ''), COALESCE(trusted_domain, ''), COALESCE(jsapi_domain, ''),
		       COALESCE(enabled, false), COALESCE(config_confirmed, false), access_token, access_token_expired_at, last_sync_at,
		       created_at, updated_at
		FROM tenant_wecom_apps
		WHERE tenant_id = $1
	`, tenantID)
	return scanTenantApp(row)
}

func (s *Store) ListEnabledTenantCallbackApps(ctx context.Context) ([]*TenantWeComAppRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, corp_id, COALESCE(corp_name, ''), COALESCE(agent_id, 0),
		       COALESCE(secret_ciphertext, ''), COALESCE(token, ''), COALESCE(encoding_aes_key, ''),
		       COALESCE(home_url, ''), COALESCE(trusted_domain, ''), COALESCE(jsapi_domain, ''),
		       COALESCE(enabled, false), COALESCE(config_confirmed, false), access_token, access_token_expired_at, last_sync_at,
		       created_at, updated_at
		FROM tenant_wecom_apps
		WHERE COALESCE(enabled, false) = TRUE
		  AND COALESCE(token, '') <> ''
		  AND COALESCE(encoding_aes_key, '') <> ''
		ORDER BY updated_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*TenantWeComAppRecord, 0)
	for rows.Next() {
		item, err := scanTenantApp(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertTenantApp(ctx context.Context, item TenantWeComAppRecord) (*TenantWeComAppRecord, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tenant_wecom_apps (
			tenant_id, corp_id, corp_name, agent_id, secret_ciphertext, token, encoding_aes_key,
			home_url, trusted_domain, jsapi_domain, enabled, config_confirmed, access_token, access_token_expired_at,
			last_sync_at, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NOW(),NOW()
		)
		ON CONFLICT (tenant_id)
		DO UPDATE SET
			corp_id = EXCLUDED.corp_id,
			corp_name = EXCLUDED.corp_name,
			agent_id = EXCLUDED.agent_id,
			secret_ciphertext = EXCLUDED.secret_ciphertext,
			token = EXCLUDED.token,
			encoding_aes_key = EXCLUDED.encoding_aes_key,
			home_url = EXCLUDED.home_url,
			trusted_domain = EXCLUDED.trusted_domain,
			jsapi_domain = EXCLUDED.jsapi_domain,
			enabled = EXCLUDED.enabled,
			config_confirmed = EXCLUDED.config_confirmed,
			updated_at = NOW()
		RETURNING id
	`, item.TenantID, item.CorpID, item.CorpName, item.AgentID, item.SecretCiphertext, item.Token, item.EncodingAESKey, item.HomeURL, item.TrustedDomain, item.JSAPIDomain, item.Enabled, item.ConfigConfirmed, item.AccessToken, item.AccessTokenExpiredAt, item.LastSyncAt).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetTenantAppByID(ctx, id)
}

func (s *Store) DeleteTenantApp(ctx context.Context, id int64) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM tenant_wecom_apps WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("tenant wecom app not found")
	}
	return nil
}

func (s *Store) UpdateTenantAppAccessToken(ctx context.Context, id int64, accessToken string, expiredAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE tenant_wecom_apps
		SET access_token = $2, access_token_expired_at = $3, last_sync_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, id, accessToken, expiredAt)
	return err
}

func (s *Store) UpsertUserBinding(ctx context.Context, record UserBindingRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wecom_user_bindings (corp_id, wecom_user_id, employee_id, tenant_id, source, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (corp_id, wecom_user_id)
		DO UPDATE SET
			employee_id = EXCLUDED.employee_id,
			tenant_id = EXCLUDED.tenant_id,
			source = EXCLUDED.source,
			updated_at = NOW()
	`, record.CorpID, record.WeComUserID, record.EmployeeID, record.TenantID, record.Source)
	return err
}

func (s *Store) GetUserBinding(ctx context.Context, corpID, wecomUserID string) (*UserBindingRecord, error) {
	var item UserBindingRecord
	err := s.pool.QueryRow(ctx, `SELECT id, corp_id, wecom_user_id, employee_id, tenant_id, source FROM wecom_user_bindings WHERE corp_id = $1 AND wecom_user_id = $2`, corpID, wecomUserID).Scan(
		&item.ID,
		&item.CorpID,
		&item.WeComUserID,
		&item.EmployeeID,
		&item.TenantID,
		&item.Source,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) ListBindingStatuses(ctx context.Context, tenantID *int64, keyword, status string, page, pageSize int) ([]*BindingStatusRecord, int, error) {
	where := []string{"e.deleted_at IS NULL"}
	args := []interface{}{}
	if tenantID != nil && *tenantID > 0 {
		args = append(args, *tenantID)
		where = append(where, fmt.Sprintf("e.tenant_id = $%d", len(args)))
	}
	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf(`(
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '') ILIKE $%d
			OR COALESCE(e.phone, '') ILIKE $%d
			OR COALESCE(wub.wecom_user_id, '') ILIKE $%d
			OR COALESCE(wta.corp_name, '') ILIKE $%d
			OR COALESCE(wta.corp_id, '') ILIKE $%d
		)`, len(args), len(args), len(args), len(args), len(args)))
	}
	switch strings.TrimSpace(status) {
	case "bound":
		where = append(where, "wub.id IS NOT NULL")
	case "unbound":
		where = append(where, "wub.id IS NULL")
	}
	whereClause := strings.Join(where, " AND ")
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	countSQL := `
		SELECT COUNT(*)
		FROM employees e
		JOIN tenants t ON t.id = e.tenant_id AND t.deleted_at IS NULL
		LEFT JOIN wecom_user_bindings wub ON wub.employee_id = e.id
		LEFT JOIN tenant_wecom_apps wta ON wta.tenant_id = e.tenant_id
		WHERE ` + whereClause
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, pageSize, offset)
	rows, err := s.pool.Query(ctx, `
		SELECT
			wub.id,
			e.tenant_id,
			COALESCE(t.name, ''),
			e.id,
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.phone, '员工#' || e.id::text) AS employee_name,
			COALESCE(e.phone, ''),
			COALESCE(wub.corp_id, wta.corp_id, ''),
			COALESCE(wta.corp_name, ''),
			COALESCE(wub.wecom_user_id, ''),
			COALESCE(wub.source, ''),
			COALESCE(wta.enabled, false),
			(wub.id IS NOT NULL) AS is_bound,
			wub.created_at,
			wub.updated_at
		FROM employees e
		JOIN tenants t ON t.id = e.tenant_id AND t.deleted_at IS NULL
		LEFT JOIN wecom_user_bindings wub ON wub.employee_id = e.id
		LEFT JOIN tenant_wecom_apps wta ON wta.tenant_id = e.tenant_id
		WHERE `+whereClause+`
		ORDER BY e.tenant_id ASC, e.id ASC
		LIMIT $`+fmt.Sprintf("%d", len(args)-1)+` OFFSET $`+fmt.Sprintf("%d", len(args))+`
	`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*BindingStatusRecord, 0)
	for rows.Next() {
		var item BindingStatusRecord
		if err := rows.Scan(
			&item.BindingID,
			&item.TenantID,
			&item.TenantName,
			&item.EmployeeID,
			&item.EmployeeName,
			&item.EmployeePhone,
			&item.CorpID,
			&item.CorpName,
			&item.WeComUserID,
			&item.Source,
			&item.AppEnabled,
			&item.IsBound,
			&item.BoundAt,
			&item.BindingUpdated,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, &item)
	}
	return items, total, rows.Err()
}

func (s *Store) GetBindingByID(ctx context.Context, id int64) (*UserBindingRecord, error) {
	var item UserBindingRecord
	err := s.pool.QueryRow(ctx, `
		SELECT id, corp_id, wecom_user_id, employee_id, tenant_id, source
		FROM wecom_user_bindings
		WHERE id = $1
	`, id).Scan(
		&item.ID,
		&item.CorpID,
		&item.WeComUserID,
		&item.EmployeeID,
		&item.TenantID,
		&item.Source,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) DeleteBinding(ctx context.Context, id int64) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM wecom_user_bindings WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("binding not found")
	}
	return nil
}

func (s *Store) FindUniqueEmployeeIDByPhone(ctx context.Context, phone string) (*int64, error) {
	return s.FindUniqueEmployeeIDByPhoneAndTenant(ctx, phone, 0)
}

func (s *Store) FindUniqueEmployeeIDByPhoneAndTenant(ctx context.Context, phone string, tenantID int64) (*int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id
		FROM employees e
		JOIN tenants t ON t.id = e.tenant_id
		WHERE e.phone = $1
		  AND e.deleted_at IS NULL
		  AND t.deleted_at IS NULL
		  AND t.is_active::text IN ('1','t','true','TRUE')
		  AND ($2::bigint <= 0 OR e.tenant_id = $2)
		ORDER BY e.id ASC
		LIMIT 2
	`, phone, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	if len(ids) != 1 {
		return nil, nil
	}
	return &ids[0], nil
}

func (s *Store) FindActiveEmployeesByNormalizedPhoneAndTenant(ctx context.Context, phone string, tenantID int64) ([]*BindingStatusRecord, error) {
	if tenantID <= 0 || strings.TrimSpace(phone) == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT
			NULL::bigint,
			e.tenant_id,
			COALESCE(t.name, ''),
			e.id,
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.phone, '员工#' || e.id::text) AS employee_name,
			COALESCE(e.phone, ''),
			'' AS corp_id,
			'' AS corp_name,
			'' AS wecom_user_id,
			'' AS source,
			false AS app_enabled,
			false AS is_bound,
			NULL::timestamp,
			NULL::timestamp
		FROM employees e
		JOIN tenants t ON t.id = e.tenant_id AND t.deleted_at IS NULL
		WHERE e.tenant_id = $1
		  AND e.deleted_at IS NULL
		  AND lower(COALESCE(e.is_active::text, 'true')) IN ('1', 't', 'true', 'yes')
		  AND regexp_replace(COALESCE(e.phone, ''), '\D', '', 'g') = $2
		ORDER BY e.id ASC
		LIMIT 3
	`, tenantID, phone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*BindingStatusRecord, 0, 3)
	for rows.Next() {
		var item BindingStatusRecord
		if err := rows.Scan(
			&item.BindingID,
			&item.TenantID,
			&item.TenantName,
			&item.EmployeeID,
			&item.EmployeeName,
			&item.EmployeePhone,
			&item.CorpID,
			&item.CorpName,
			&item.WeComUserID,
			&item.Source,
			&item.AppEnabled,
			&item.IsBound,
			&item.BoundAt,
			&item.BindingUpdated,
		); err != nil {
			return nil, err
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertDirectoryMember(ctx context.Context, record DirectoryMemberRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wecom_directory_members (
			tenant_id, corp_id, wecom_user_id, name, mobile, department_ids, wecom_status,
			match_status, matched_employee_id, last_synced_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, COALESCE(NULLIF($6, '')::jsonb, '[]'::jsonb), $7,
			$8, $9, NOW(), NOW(), NOW()
		)
		ON CONFLICT (corp_id, wecom_user_id)
		DO UPDATE SET
			tenant_id = EXCLUDED.tenant_id,
			name = EXCLUDED.name,
			mobile = EXCLUDED.mobile,
			department_ids = EXCLUDED.department_ids,
			wecom_status = EXCLUDED.wecom_status,
			match_status = EXCLUDED.match_status,
			matched_employee_id = EXCLUDED.matched_employee_id,
			last_synced_at = NOW(),
			updated_at = NOW()
	`, record.TenantID, record.CorpID, record.WeComUserID, record.Name, record.Mobile, record.DepartmentIDsJSON, record.WeComStatus, record.MatchStatus, record.MatchedEmployeeID)
	return err
}

func (s *Store) ListDirectoryMembers(ctx context.Context, params DirectoryMemberListParams) ([]*DirectoryMemberListRecord, int, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	if params.TenantID != nil && *params.TenantID > 0 {
		args = append(args, *params.TenantID)
		where = append(where, fmt.Sprintf("m.tenant_id = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(params.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf(`(
			COALESCE(m.name, '') ILIKE $%d
			OR COALESCE(m.mobile, '') ILIKE $%d
			OR COALESCE(m.wecom_user_id, '') ILIKE $%d
			OR COALESCE(e.phone, '') ILIKE $%d
		)`, len(args), len(args), len(args), len(args)))
	}
	if status := strings.TrimSpace(params.Status); status != "" && status != "all" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("m.match_status = $%d", len(args)))
	}
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}
	whereClause := strings.Join(where, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM wecom_directory_members m
		LEFT JOIN employees e ON e.id = m.matched_employee_id
		WHERE `+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, params.PageSize, (params.Page-1)*params.PageSize)
	rows, err := s.pool.Query(ctx, `
		SELECT
			m.id,
			m.tenant_id,
			COALESCE(t.name, ''),
			m.corp_id,
			COALESCE(a.corp_name, ''),
			m.wecom_user_id,
			COALESCE(m.name, ''),
			COALESCE(m.mobile, ''),
			COALESCE(m.match_status, ''),
			m.matched_employee_id,
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.phone, '') AS matched_employee_name,
			b.employee_id,
			COALESCE(b.source, ''),
			m.last_synced_at
		FROM wecom_directory_members m
		LEFT JOIN tenants t ON t.id = m.tenant_id
		LEFT JOIN tenant_wecom_apps a ON a.corp_id = m.corp_id
		LEFT JOIN employees e ON e.id = m.matched_employee_id
		LEFT JOIN wecom_user_bindings b ON b.corp_id = m.corp_id AND b.wecom_user_id = m.wecom_user_id
		WHERE `+whereClause+`
		ORDER BY m.updated_at DESC, m.id DESC
		LIMIT $`+fmt.Sprintf("%d", len(args)-1)+` OFFSET $`+fmt.Sprintf("%d", len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*DirectoryMemberListRecord, 0, params.PageSize)
	for rows.Next() {
		var item DirectoryMemberListRecord
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.TenantName,
			&item.CorpID,
			&item.CorpName,
			&item.WeComUserID,
			&item.Name,
			&item.Mobile,
			&item.MatchStatus,
			&item.MatchedEmployeeID,
			&item.MatchedEmployeeName,
			&item.BindingEmployeeID,
			&item.BindingSource,
			&item.LastSyncedAt,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, &item)
	}
	return items, total, rows.Err()
}

func marshalDepartmentIDs(ids []int64) string {
	if len(ids) == 0 {
		return "[]"
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func (s *Store) CountEventLogsByPayload(ctx context.Context, infoType, rawPayload string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(1) FROM wecom_event_logs WHERE info_type = $1 AND raw_payload = $2`, infoType, rawPayload).Scan(&count)
	return count, err
}

func (s *Store) SaveEventLog(ctx context.Context, corpID, infoType, rawPayload string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO wecom_event_logs (corp_id, info_type, raw_payload, created_at) VALUES ($1, $2, $3, NOW())`, corpID, infoType, rawPayload)
	return err
}

func (s *Store) GetActiveBindingByEmployeeID(ctx context.Context, employeeID int64) (*EmployeeBindingRecord, error) {
	var item EmployeeBindingRecord
	err := s.pool.QueryRow(ctx, `
		SELECT
			wub.corp_id,
			COALESCE(wta.corp_name, ''),
			wub.wecom_user_id,
			wub.employee_id,
			wub.tenant_id,
			COALESCE(wta.agent_id, 0)
		FROM wecom_user_bindings wub
		INNER JOIN tenant_wecom_apps wta
		  ON wta.corp_id = wub.corp_id
		 AND COALESCE(wta.enabled, false) = true
		WHERE wub.employee_id = $1
		ORDER BY wub.updated_at DESC, wub.id DESC
		LIMIT 1
	`, employeeID).Scan(
		&item.CorpID,
		&item.CorpName,
		&item.WeComUserID,
		&item.EmployeeID,
		&item.TenantID,
		&item.AgentID,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) GetActivePartnerBindingByEmployeeID(ctx context.Context, employeeID int64) (*EmployeeBindingRecord, error) {
	var item EmployeeBindingRecord
	var permanentCode string
	err := s.pool.QueryRow(ctx, `
		SELECT
			wub.corp_id,
			COALESCE(wci.corp_name, ''),
			wub.wecom_user_id,
			wub.employee_id,
			wub.tenant_id,
			COALESCE(wci.agent_id, 0),
			COALESCE(wci.permanent_code, '')
		FROM wecom_user_bindings wub
		INNER JOIN wecom_corp_installs wci
		  ON wci.corp_id = wub.corp_id
		 AND COALESCE(wci.status, 'active') = 'active'
		WHERE wub.employee_id = $1
		ORDER BY wub.updated_at DESC, wub.id DESC
		LIMIT 1
	`, employeeID).Scan(
		&item.CorpID,
		&item.CorpName,
		&item.WeComUserID,
		&item.EmployeeID,
		&item.TenantID,
		&item.AgentID,
		&permanentCode,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) InsertMessageLog(ctx context.Context, record MessageLogRecord) (int64, bool, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO wecom_message_logs (
			corp_id, tenant_id, employee_id, wecom_user_id, message_scene, dedupe_key,
			title, content, target_url, status, error_message, request_payload, response_payload, biz_date, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, NULLIF($9, ''), $10, NULLIF($11, ''), COALESCE(NULLIF($12, '')::jsonb, '{}'::jsonb), COALESCE(NULLIF($13, '')::jsonb, '{}'::jsonb), NULLIF($14, '')::date, NOW(), NOW()
		)
		ON CONFLICT (dedupe_key) DO NOTHING
		RETURNING id
	`,
		record.CorpID,
		record.TenantID,
		record.EmployeeID,
		record.WeComUserID,
		record.MessageScene,
		record.DedupeKey,
		record.Title,
		record.Content,
		record.TargetURL,
		record.Status,
		record.ErrorMessage,
		record.RequestPayload,
		record.ResponsePayload,
		nilIfEmptyString(record.BizDate),
	).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (s *Store) UpdateMessageLogStatus(ctx context.Context, id int64, status, errorMessage, responsePayload string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE wecom_message_logs
		SET status = $2,
			error_message = NULLIF($3, ''),
			response_payload = COALESCE(NULLIF($4, '')::jsonb, response_payload),
			updated_at = NOW()
		WHERE id = $1
	`, id, status, errorMessage, responsePayload)
	return err
}

func nilIfEmptyString(value *string) *string {
	if value == nil {
		return nil
	}
	if *value == "" {
		return nil
	}
	return value
}

func scanTenantApp(row interface {
	Scan(dest ...interface{}) error
}) (*TenantWeComAppRecord, error) {
	var item TenantWeComAppRecord
	if err := row.Scan(
		&item.ID,
		&item.TenantID,
		&item.CorpID,
		&item.CorpName,
		&item.AgentID,
		&item.SecretCiphertext,
		&item.Token,
		&item.EncodingAESKey,
		&item.HomeURL,
		&item.TrustedDomain,
		&item.JSAPIDomain,
		&item.Enabled,
		&item.ConfigConfirmed,
		&item.AccessToken,
		&item.AccessTokenExpiredAt,
		&item.LastSyncAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &item, nil
}
