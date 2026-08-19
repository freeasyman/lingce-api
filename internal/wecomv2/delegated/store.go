package delegated

import (
	"context"
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

func (s *Store) ListCorpInstalls(ctx context.Context, tenantID *int64, corpID string) ([]*corpInstallRecord, error) {
	where := "1=1"
	args := []any{}
	if tenantID != nil && *tenantID > 0 {
		args = append(args, *tenantID)
		where += fmt.Sprintf(" AND tenant_id = $%d", len(args))
	}
	corpID = strings.TrimSpace(corpID)
	if corpID != "" {
		args = append(args, corpID)
		where += fmt.Sprintf(" AND corp_id = $%d", len(args))
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(provider_app, ''), COALESCE(tenant_id, 0), corp_id, COALESCE(corp_name, ''),
		       COALESCE(permanent_code, ''), COALESCE(agent_id, 0), COALESCE(status, 'active'), updated_at, cancelled_at
		FROM wecom_corp_installs
		WHERE `+where+`
		ORDER BY updated_at DESC NULLS LAST, corp_id ASC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*corpInstallRecord, 0)
	for rows.Next() {
		var item corpInstallRecord
		var updatedAt *time.Time
		var cancelledAt *time.Time
		if err := rows.Scan(&item.ID, &item.ProviderApp, &item.TenantID, &item.CorpID, &item.CorpName, &item.PermanentCode, &item.AgentID, &item.Status, &updatedAt, &cancelledAt); err != nil {
			return nil, err
		}
		if updatedAt != nil {
			v := updatedAt.Format(time.RFC3339)
			item.UpdatedAt = &v
		}
		if cancelledAt != nil {
			v := cancelledAt.Format(time.RFC3339)
			item.CancelledAt = &v
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) BindCorpInstallTenant(ctx context.Context, providerApp, corpID string, tenantID int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE wecom_corp_installs
		SET tenant_id = $3, updated_at = NOW()
		WHERE provider_app = $1 AND corp_id = $2
	`, strings.TrimSpace(providerApp), strings.TrimSpace(corpID), tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("wecom corp install not found")
	}
	return nil
}

func (s *Store) GetCorpInstallByCorpID(ctx context.Context, providerApp, corpID string) (*corpInstallRecord, error) {
	var item corpInstallRecord
	var updatedAt *time.Time
	var cancelledAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, COALESCE(provider_app, ''), COALESCE(tenant_id, 0), corp_id, COALESCE(corp_name, ''),
		       COALESCE(permanent_code, ''), COALESCE(agent_id, 0), COALESCE(status, 'active'), updated_at, cancelled_at
		FROM wecom_corp_installs
		WHERE provider_app = $1 AND corp_id = $2
	`, providerApp, corpID).Scan(&item.ID, &item.ProviderApp, &item.TenantID, &item.CorpID, &item.CorpName, &item.PermanentCode, &item.AgentID, &item.Status, &updatedAt, &cancelledAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("wecom corp install not found")
	}
	if err != nil {
		return nil, err
	}
	if updatedAt != nil {
		v := updatedAt.Format(time.RFC3339)
		item.UpdatedAt = &v
	}
	if cancelledAt != nil {
		v := cancelledAt.Format(time.RFC3339)
		item.CancelledAt = &v
	}
	return &item, nil
}

func (s *Store) UpsertRuntimeStateSuiteTicket(ctx context.Context, providerApp, suiteTicket string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wecom_runtime_state (
			provider_app, suite_ticket, suite_ticket_received_at, created_at, updated_at
		) VALUES ($1, $2, NOW(), NOW(), NOW())
		ON CONFLICT (provider_app)
		DO UPDATE SET
			suite_ticket = EXCLUDED.suite_ticket,
			suite_ticket_received_at = NOW(),
			updated_at = NOW()
	`, strings.TrimSpace(providerApp), strings.TrimSpace(suiteTicket))
	return err
}

func (s *Store) GetRuntimeStateSuiteTicket(ctx context.Context, providerApp string) (string, error) {
	var suiteTicket string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(suite_ticket, '')
		FROM wecom_runtime_state
		WHERE provider_app = $1
	`, strings.TrimSpace(providerApp)).Scan(&suiteTicket)
	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("suite ticket is missing")
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(suiteTicket) == "" {
		return "", fmt.Errorf("suite ticket is missing")
	}
	return suiteTicket, nil
}

func (s *Store) UpsertCorpInstall(ctx context.Context, record corpInstallRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wecom_corp_installs (
			provider_app, tenant_id, corp_id, corp_name, permanent_code, agent_id, status, created_at, updated_at, cancelled_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW(), NULL)
		ON CONFLICT (provider_app, corp_id)
		DO UPDATE SET
			tenant_id = CASE
				WHEN EXCLUDED.tenant_id > 0 THEN EXCLUDED.tenant_id
				ELSE wecom_corp_installs.tenant_id
			END,
			corp_name = EXCLUDED.corp_name,
			permanent_code = EXCLUDED.permanent_code,
			agent_id = EXCLUDED.agent_id,
			status = EXCLUDED.status,
			updated_at = NOW(),
			cancelled_at = NULL
	`, strings.TrimSpace(record.ProviderApp), record.TenantID, strings.TrimSpace(record.CorpID), strings.TrimSpace(record.CorpName), strings.TrimSpace(record.PermanentCode), record.AgentID, strings.TrimSpace(record.Status))
	return err
}

func (s *Store) MarkCorpInstallCancelled(ctx context.Context, providerApp, corpID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE wecom_corp_installs
		SET status = 'cancelled', updated_at = NOW(), cancelled_at = NOW()
		WHERE provider_app = $1 AND corp_id = $2
	`, strings.TrimSpace(providerApp), strings.TrimSpace(corpID))
	return err
}

func (s *Store) SaveEventLog(ctx context.Context, corpID, infoType, rawPayload string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wecom_event_logs (corp_id, info_type, raw_payload, created_at)
		VALUES ($1, $2, $3, NOW())
	`, strings.TrimSpace(corpID), strings.TrimSpace(infoType), rawPayload)
	return err
}

func (s *Store) CountEventLogsByPayload(ctx context.Context, infoType, rawPayload string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(1)
		FROM wecom_event_logs
		WHERE info_type = $1 AND raw_payload = $2
	`, strings.TrimSpace(infoType), rawPayload).Scan(&count)
	return count, err
}

func (s *Store) GetUserBinding(ctx context.Context, providerApp, corpID, wecomUserID string) (*userBindingRecord, error) {
	var item userBindingRecord
	err := s.pool.QueryRow(ctx, `
		SELECT id, COALESCE(provider_app, ''), corp_id, wecom_user_id, employee_id, tenant_id, COALESCE(source, '')
		FROM wecom_user_bindings
		WHERE provider_app = $1 AND corp_id = $2 AND wecom_user_id = $3
	`, strings.TrimSpace(providerApp), strings.TrimSpace(corpID), strings.TrimSpace(wecomUserID)).Scan(
		&item.ID,
		&item.ProviderApp,
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

func (s *Store) UpsertUserBinding(ctx context.Context, record userBindingRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wecom_user_bindings (provider_app, corp_id, wecom_user_id, employee_id, tenant_id, source, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (provider_app, corp_id, wecom_user_id)
		DO UPDATE SET
			employee_id = EXCLUDED.employee_id,
			tenant_id = EXCLUDED.tenant_id,
			source = EXCLUDED.source,
			updated_at = NOW()
	`, strings.TrimSpace(record.ProviderApp), strings.TrimSpace(record.CorpID), strings.TrimSpace(record.WeComUserID), record.EmployeeID, record.TenantID, strings.TrimSpace(record.Source))
	return err
}

func (s *Store) GetActiveBindingByEmployeeID(ctx context.Context, providerApp string, employeeID int64) (*employeeBindingRecord, error) {
	var item employeeBindingRecord
	err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(wub.provider_app, ''),
			wub.corp_id,
			COALESCE(wci.corp_name, ''),
			wub.wecom_user_id,
			wub.employee_id,
			wub.tenant_id,
			COALESCE(wci.agent_id, 0)
		FROM wecom_user_bindings wub
		INNER JOIN wecom_corp_installs wci
		  ON wci.provider_app = wub.provider_app
		 AND wci.corp_id = wub.corp_id
		 AND COALESCE(wci.status, 'active') = 'active'
		WHERE wub.provider_app = $1
		  AND wub.employee_id = $2
		ORDER BY wub.updated_at DESC, wub.id DESC
		LIMIT 1
	`, strings.TrimSpace(providerApp), employeeID).Scan(
		&item.ProviderApp,
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

func (s *Store) InsertMessageLog(ctx context.Context, record messageLogRecord) (int64, bool, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO wecom_message_logs (
			provider_app, corp_id, tenant_id, employee_id, wecom_user_id, message_scene, dedupe_key,
			title, content, target_url, status, error_message, request_payload, response_payload, biz_date, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, NULLIF($10, ''), $11, NULLIF($12, ''), COALESCE(NULLIF($13, '')::jsonb, '{}'::jsonb), COALESCE(NULLIF($14, '')::jsonb, '{}'::jsonb), NULLIF($15, '')::date, NOW(), NOW()
		)
		ON CONFLICT (dedupe_key) DO NOTHING
		RETURNING id
	`,
		record.ProviderApp,
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

func (s *Store) DeleteUserBindingsByCorpID(ctx context.Context, providerApp, corpID string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM wecom_user_bindings
		WHERE provider_app = $1 AND corp_id = $2
	`, strings.TrimSpace(providerApp), strings.TrimSpace(corpID))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func nilIfEmptyString(value *string) *string {
	if value == nil {
		return nil
	}
	if strings.TrimSpace(*value) == "" {
		return nil
	}
	return value
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
	`, strings.TrimSpace(phone), tenantID)
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

func (s *Store) ListRecentEventLogsByCorpID(ctx context.Context, corpID string, limit int) ([]*eventLogRecord, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(corp_id, ''), COALESCE(info_type, ''), COALESCE(raw_payload, ''), created_at
		FROM wecom_event_logs
		WHERE corp_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, strings.TrimSpace(corpID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*eventLogRecord, 0, limit)
	for rows.Next() {
		var item eventLogRecord
		var createdAt *time.Time
		if err := rows.Scan(&item.ID, &item.CorpID, &item.InfoType, &item.RawPayload, &createdAt); err != nil {
			return nil, err
		}
		if createdAt != nil {
			v := createdAt.Format(time.RFC3339)
			item.CreatedAt = &v
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) GetRuntimeState(ctx context.Context, providerApp string) (*runtimeStateRecord, error) {
	var item runtimeStateRecord
	var suiteTicketReceivedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(provider_app, ''), COALESCE(suite_ticket, ''), suite_ticket_received_at
		FROM wecom_runtime_state
		WHERE provider_app = $1
	`, strings.TrimSpace(providerApp)).Scan(&item.ProviderApp, &item.SuiteTicket, &suiteTicketReceivedAt)
	if err == pgx.ErrNoRows {
		return &item, nil
	}
	if err != nil {
		return nil, err
	}
	if suiteTicketReceivedAt != nil {
		v := suiteTicketReceivedAt.Format(time.RFC3339)
		item.SuiteTicketReceivedAt = &v
	}
	return &item, nil
}

func (s *Store) CountActiveCorpInstalls(ctx context.Context, providerApp string) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(1)
		FROM wecom_corp_installs
		WHERE provider_app = $1 AND status = 'active'
	`, strings.TrimSpace(providerApp)).Scan(&count)
	return count, err
}

func (s *Store) ListRecentEventLogs(ctx context.Context, limit int) ([]*eventLogRecord, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(corp_id, ''), COALESCE(info_type, ''), COALESCE(raw_payload, ''), created_at
		FROM wecom_event_logs
		ORDER BY created_at DESC, id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*eventLogRecord, 0, limit)
	for rows.Next() {
		var item eventLogRecord
		var createdAt *time.Time
		if err := rows.Scan(&item.ID, &item.CorpID, &item.InfoType, &item.RawPayload, &createdAt); err != nil {
			return nil, err
		}
		if createdAt != nil {
			v := createdAt.Format(time.RFC3339)
			item.CreatedAt = &v
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

type runtimeStateRecord struct {
	ProviderApp           string
	SuiteTicket           string
	SuiteTicketReceivedAt *string
}
