package wecom

import (
	"context"
	"fmt"
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
		       COALESCE(enabled, false), access_token, access_token_expired_at, last_sync_at,
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
		       COALESCE(enabled, false), access_token, access_token_expired_at, last_sync_at,
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
		       COALESCE(enabled, false), access_token, access_token_expired_at, last_sync_at,
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
		       COALESCE(enabled, false), access_token, access_token_expired_at, last_sync_at,
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
		       COALESCE(enabled, false), access_token, access_token_expired_at, last_sync_at,
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
			home_url, trusted_domain, jsapi_domain, enabled, access_token, access_token_expired_at,
			last_sync_at, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NOW(),NOW()
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
			updated_at = NOW()
		RETURNING id
	`, item.TenantID, item.CorpID, item.CorpName, item.AgentID, item.SecretCiphertext, item.Token, item.EncodingAESKey, item.HomeURL, item.TrustedDomain, item.JSAPIDomain, item.Enabled, item.AccessToken, item.AccessTokenExpiredAt, item.LastSyncAt).Scan(&id)
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
	err := s.pool.QueryRow(ctx, `SELECT corp_id, wecom_user_id, employee_id, tenant_id, source FROM wecom_user_bindings WHERE corp_id = $1 AND wecom_user_id = $2`, corpID, wecomUserID).Scan(
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
