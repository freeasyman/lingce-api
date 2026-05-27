package wecom

import (
	"context"
	"fmt"

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
	rows, err := s.pool.Query(ctx, `
		SELECT e.id
		FROM employees e
		JOIN tenants t ON t.id = e.tenant_id
		WHERE e.phone = $1
		  AND e.deleted_at IS NULL
		  AND t.deleted_at IS NULL
		  AND t.is_active::text IN ('1','t','true','TRUE')
		ORDER BY e.id ASC
		LIMIT 2
	`, phone)
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
			COALESCE(wci.corp_name, ''),
			wub.wecom_user_id,
			wub.employee_id,
			wub.tenant_id,
			wci.permanent_code,
			COALESCE(wci.agent_id, 0)
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
		&item.PermanentCode,
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
