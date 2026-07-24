package opportunityalert

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) ListForAdmin(ctx context.Context, req AdminListRequest) ([]*Alert, int, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
	where := []string{"1=1"}
	args := []interface{}{}
	if req.TenantID != nil && *req.TenantID > 0 {
		args = append(args, *req.TenantID)
		where = append(where, fmt.Sprintf("a.tenant_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(req.Status); status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("a.status = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf(`(
			COALESCE(a.title, '') ILIKE $%d
			OR COALESCE(a.summary, '') ILIKE $%d
			OR COALESCE(a.reason, '') ILIKE $%d
			OR COALESCE(a.customer_name, '') ILIKE $%d
			OR a.recording_id::text ILIKE $%d
			OR a.employee_id::text ILIKE $%d
		)`, len(args), len(args), len(args), len(args), len(args), len(args)))
	}
	whereClause := strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM opportunity_alerts a WHERE "+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, req.PageSize, (req.Page-1)*req.PageSize)
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.tenant_id, a.recording_id, a.employee_id, a.customer_id, COALESCE(a.customer_name, ''),
		       a.alert_type, a.title, COALESCE(a.summary, ''), COALESCE(a.reason, ''), COALESCE(a.customer_objection, ''),
		       COALESCE(a.evidence, ''), COALESCE(a.suggested_action, ''), COALESCE(a.suggested_script, ''),
		       COALESCE(a.priority, 'medium'), a.status, a.viewed_at, a.handled_at, a.ignored_at,
		       a.dedupe_key, a.raw_payload, a.created_at, a.updated_at,
		       NULL::text, NULL::timestamp
		FROM opportunity_alerts a
		WHERE `+whereClause+`
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := scanAlerts(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Store) ListForEmployee(ctx context.Context, tenantID, employeeID int64, req ListRequest) ([]*Alert, int, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
	where := []string{
		"a.tenant_id = $1",
		"EXISTS (SELECT 1 FROM opportunity_alert_recipients r WHERE r.alert_id = a.id AND r.employee_id = $2)",
	}
	args := []interface{}{tenantID, employeeID}
	if status := strings.TrimSpace(req.Status); status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("a.status = $%d", len(args)))
	}
	whereClause := strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM opportunity_alerts a WHERE "+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, req.PageSize, (req.Page-1)*req.PageSize)
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.tenant_id, a.recording_id, a.employee_id, a.customer_id, COALESCE(a.customer_name, ''),
		       a.alert_type, a.title, COALESCE(a.summary, ''), COALESCE(a.reason, ''), COALESCE(a.customer_objection, ''),
		       COALESCE(a.evidence, ''), COALESCE(a.suggested_action, ''), COALESCE(a.suggested_script, ''),
		       COALESCE(a.priority, 'medium'), a.status, a.viewed_at, a.handled_at, a.ignored_at,
		       a.dedupe_key, a.raw_payload, a.created_at, a.updated_at,
		       r.recipient_type, r.read_at
		FROM opportunity_alerts a
		JOIN opportunity_alert_recipients r ON r.alert_id = a.id AND r.employee_id = $2
		WHERE `+whereClause+`
		ORDER BY a.created_at DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := scanAlerts(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Store) GetForEmployee(ctx context.Context, tenantID, employeeID, alertID int64) (*Alert, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT a.id, a.tenant_id, a.recording_id, a.employee_id, a.customer_id, COALESCE(a.customer_name, ''),
		       a.alert_type, a.title, COALESCE(a.summary, ''), COALESCE(a.reason, ''), COALESCE(a.customer_objection, ''),
		       COALESCE(a.evidence, ''), COALESCE(a.suggested_action, ''), COALESCE(a.suggested_script, ''),
		       COALESCE(a.priority, 'medium'), a.status, a.viewed_at, a.handled_at, a.ignored_at,
		       a.dedupe_key, a.raw_payload, a.created_at, a.updated_at,
		       r.recipient_type, r.read_at
		FROM opportunity_alerts a
		JOIN opportunity_alert_recipients r ON r.alert_id = a.id AND r.employee_id = $3
		WHERE a.tenant_id = $1 AND a.id = $2
	`, tenantID, alertID, employeeID)
	item, err := scanAlert(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("opportunity alert not found")
		}
		return nil, err
	}
	return item, nil
}

func (s *Store) MarkViewed(ctx context.Context, tenantID, employeeID, alertID int64) (*Alert, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE opportunity_alert_recipients
		SET read_at = COALESCE(read_at, NOW()), updated_at = NOW()
		WHERE alert_id = $1 AND tenant_id = $2 AND employee_id = $3
	`, alertID, tenantID, employeeID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE opportunity_alerts
		SET status = CASE WHEN status = 'pending' THEN 'viewed' ELSE status END,
		    viewed_at = COALESCE(viewed_at, NOW()),
		    updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		  AND EXISTS (SELECT 1 FROM opportunity_alert_recipients r WHERE r.alert_id = opportunity_alerts.id AND r.employee_id = $3)
	`, alertID, tenantID, employeeID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetForEmployee(ctx, tenantID, employeeID, alertID)
}

func (s *Store) MarkHandled(ctx context.Context, tenantID, employeeID, alertID int64) (*Alert, error) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE opportunity_alerts
		SET status = 'handled', handled_at = COALESCE(handled_at, NOW()), updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND employee_id = $3
	`, alertID, tenantID, employeeID); err != nil {
		return nil, err
	}
	return s.GetForEmployee(ctx, tenantID, employeeID, alertID)
}

func (s *Store) MarkIgnored(ctx context.Context, tenantID, employeeID, alertID int64) (*Alert, error) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE opportunity_alerts
		SET status = 'ignored', ignored_at = COALESCE(ignored_at, NOW()), updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND employee_id = $3
	`, alertID, tenantID, employeeID); err != nil {
		return nil, err
	}
	return s.GetForEmployee(ctx, tenantID, employeeID, alertID)
}

func (s *Store) Create(ctx context.Context, input CreateAlertInput, recipientIDs []int64) (*Alert, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO opportunity_alerts (
			tenant_id, recording_id, employee_id, customer_id, customer_name, alert_type, title, summary, reason,
			customer_objection, evidence, suggested_action, suggested_script, priority, dedupe_key, raw_payload,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,NOW(),NOW())
		ON CONFLICT (dedupe_key) DO NOTHING
		RETURNING id
	`, input.TenantID, input.RecordingID, input.EmployeeID, input.CustomerID, input.CustomerName, input.AlertType, input.Title, input.Summary, input.Reason,
		input.CustomerObjection, input.Evidence, input.SuggestedAction, input.SuggestedScript, normalizePriority(input.Priority), input.DedupeKey, input.RawPayload).Scan(&id)
	created := true
	if err != nil {
		if err == pgx.ErrNoRows {
			created = false
			if err := tx.QueryRow(ctx, `SELECT id FROM opportunity_alerts WHERE dedupe_key = $1`, input.DedupeKey).Scan(&id); err != nil {
				return nil, false, err
			}
		} else {
			return nil, false, err
		}
	}
	if created {
		seen := map[int64]bool{}
		for _, rid := range append([]int64{input.EmployeeID}, recipientIDs...) {
			if rid <= 0 || seen[rid] {
				continue
			}
			seen[rid] = true
			rtype := string(RecipientTypeCC)
			if rid == input.EmployeeID {
				rtype = string(RecipientTypeOwner)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO opportunity_alert_recipients (alert_id, tenant_id, employee_id, recipient_type, created_at, updated_at)
				VALUES ($1,$2,$3,$4,NOW(),NOW())
				ON CONFLICT (alert_id, employee_id) DO NOTHING
			`, id, input.TenantID, rid, rtype); err != nil {
				return nil, false, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	item, err := s.GetByID(ctx, id)
	return item, created, err
}

func (s *Store) GetRecordingAlertSource(ctx context.Context, recordingID int64) (*RecordingAlertSource, error) {
	var item RecordingAlertSource
	err := s.pool.QueryRow(ctx, `
		SELECT r.id,
		       r.tenant_id,
		       r.employee_id,
		       r.customer_id,
	       COALESCE(NULLIF(c.name, ''), '') AS customer_name,
	       COALESCE(NULLIF(r.business_scope, ''), 'unknown') AS business_scope,
	       COALESCE(r.analysis_result::jsonb, '{}'::jsonb),
	       COALESCE(r.analysis_display, '{}'::jsonb)
	FROM recordings r
	LEFT JOIN customers c ON c.id = r.customer_id
	WHERE r.id = $1
	`, recordingID).Scan(
		&item.ID,
		&item.TenantID,
		&item.EmployeeID,
		&item.CustomerID,
		&item.CustomerName,
		&item.BusinessScope,
		&item.AnalysisResult,
		&item.AnalysisDisplay,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("recording not found")
		}
		return nil, err
	}
	return &item, nil
}

func (s *Store) ListCCRules(ctx context.Context, tenantID int64, employeeID *int64) ([]*CCRule, error) {
	where := []string{"r.tenant_id = $1", "lower(COALESCE(r.is_active::text, 'true')) IN ('1', 't', 'true', 'yes')"}
	args := []interface{}{tenantID}
	if employeeID != nil && *employeeID > 0 {
		args = append(args, *employeeID)
		where = append(where, fmt.Sprintf("r.employee_id = $%d", len(args)))
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.tenant_id, r.employee_id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.phone, '员工#' || e.id::text) AS employee_name,
		       r.cc_employee_id,
		       COALESCE(NULLIF(cc.full_name, ''), NULLIF(cc.name, ''), cc.phone, '员工#' || cc.id::text) AS cc_employee_name,
		       lower(COALESCE(r.is_active::text, 'true')) IN ('1', 't', 'true', 'yes'), r.created_at, r.updated_at
		FROM opportunity_alert_cc_rules r
		JOIN employees e ON e.id = r.employee_id AND e.tenant_id = r.tenant_id AND e.deleted_at IS NULL
		JOIN employees cc ON cc.id = r.cc_employee_id AND cc.tenant_id = r.tenant_id AND cc.deleted_at IS NULL
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY r.updated_at DESC, r.id DESC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*CCRule, 0)
	for rows.Next() {
		var item CCRule
		if err := rows.Scan(&item.ID, &item.TenantID, &item.EmployeeID, &item.EmployeeName, &item.CCEmployeeID, &item.CCEmployeeName, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertCCRule(ctx context.Context, tenantID, employeeID, ccEmployeeID int64) (*CCRule, error) {
	var sameTenantCount int
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM employees
		WHERE tenant_id = $1
		  AND id = ANY($2::bigint[])
		  AND deleted_at IS NULL
		  AND lower(COALESCE(is_active::text, 'true')) IN ('1', 't', 'true', 'yes')
	`, tenantID, []int64{employeeID, ccEmployeeID}).Scan(&sameTenantCount); err != nil {
		return nil, err
	}
	if sameTenantCount != 2 {
		return nil, fmt.Errorf("employee and cc employee must belong to the same active tenant")
	}
	var id int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO opportunity_alert_cc_rules (tenant_id, employee_id, cc_employee_id, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, true, NOW(), NOW())
		ON CONFLICT (tenant_id, employee_id, cc_employee_id)
		DO UPDATE SET is_active = true, updated_at = NOW()
		RETURNING id
	`, tenantID, employeeID, ccEmployeeID).Scan(&id); err != nil {
		return nil, err
	}
	items, err := s.ListCCRules(ctx, tenantID, &employeeID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return nil, fmt.Errorf("cc rule not found")
}

func (s *Store) DeleteCCRule(ctx context.Context, tenantID, ruleID int64) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE opportunity_alert_cc_rules
		SET is_active = false, updated_at = NOW()
		WHERE tenant_id = $1
		  AND id = $2
		  AND lower(COALESCE(is_active::text, 'true')) IN ('1', 't', 'true', 'yes')
	`, tenantID, ruleID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("cc rule not found")
	}
	return nil
}

func (s *Store) ListActiveCCEmployeeIDs(ctx context.Context, tenantID, employeeID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cc_employee_id
		FROM opportunity_alert_cc_rules
		WHERE tenant_id = $1
		  AND employee_id = $2
		  AND lower(COALESCE(is_active::text, 'true')) IN ('1', 't', 'true', 'yes')
		ORDER BY id ASC
	`, tenantID, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id > 0 {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

func (s *Store) ListRecipientEmployeeIDs(ctx context.Context, alertID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT employee_id
		FROM opportunity_alert_recipients
		WHERE alert_id = $1
		ORDER BY CASE WHEN recipient_type = 'owner' THEN 0 ELSE 1 END, id ASC
	`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id > 0 {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

func (s *Store) SyncWeComDeliveryStatuses(ctx context.Context, alertID int64, dedupeKey string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE opportunity_alert_recipients r
		SET delivery_status = COALESCE(w.status, r.delivery_status),
		    wecom_message_log_id = w.id,
		    sent_at = CASE WHEN w.status = 'sent' THEN COALESCE(r.sent_at, w.updated_at, NOW()) ELSE r.sent_at END,
		    updated_at = NOW()
		FROM wecom_message_logs w
		WHERE r.alert_id = $1
		  AND w.message_scene = 'opportunity_alert'
		  AND w.employee_id = r.employee_id
		  AND w.dedupe_key = $2 || ':' || r.employee_id::text
	`, alertID, dedupeKey)
	return err
}

func (s *Store) SyncWeComDeliveryStatusesForEmployees(ctx context.Context, alertID int64, dedupeKey string, employeeIDs []int64) error {
	if len(employeeIDs) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE opportunity_alert_recipients r
		SET delivery_status = COALESCE(w.status, r.delivery_status),
		    wecom_message_log_id = w.id,
		    sent_at = CASE WHEN w.status = 'sent' THEN COALESCE(r.sent_at, w.updated_at, NOW()) ELSE r.sent_at END,
		    updated_at = NOW()
		FROM wecom_message_logs w
		WHERE r.alert_id = $1
		  AND r.employee_id = ANY($3::bigint[])
		  AND w.message_scene = 'opportunity_alert'
		  AND w.employee_id = r.employee_id
		  AND w.dedupe_key = $2 || ':' || r.employee_id::text
	`, alertID, dedupeKey, employeeIDs)
	return err
}

func (s *Store) GetByID(ctx context.Context, alertID int64) (*Alert, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, recording_id, employee_id, customer_id, COALESCE(customer_name, ''),
		       alert_type, title, COALESCE(summary, ''), COALESCE(reason, ''), COALESCE(customer_objection, ''),
		       COALESCE(evidence, ''), COALESCE(suggested_action, ''), COALESCE(suggested_script, ''),
		       COALESCE(priority, 'medium'), status, viewed_at, handled_at, ignored_at,
		       dedupe_key, raw_payload, created_at, updated_at,
		       NULL::text, NULL::timestamp
		FROM opportunity_alerts WHERE id = $1
	`, alertID)
	return scanAlert(row)
}

func (s *Store) ListRecipients(ctx context.Context, alertID int64) ([]*AlertRecipient, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.alert_id,
		       r.tenant_id,
		       r.employee_id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.phone, '员工#' || e.id::text) AS employee_name,
		       COALESCE(e.phone, ''),
		       COALESCE(r.recipient_type, ''),
		       COALESCE(r.delivery_status, ''),
		       r.wecom_message_log_id,
		       COALESCE(w.wecom_user_id, ''),
		       r.sent_at,
		       r.read_at,
		       r.updated_at
		FROM opportunity_alert_recipients r
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN wecom_message_logs w ON w.id = r.wecom_message_log_id
		WHERE r.alert_id = $1
		ORDER BY CASE WHEN r.recipient_type = 'owner' THEN 0 ELSE 1 END, r.id ASC
	`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*AlertRecipient, 0)
	for rows.Next() {
		var item AlertRecipient
		if err := rows.Scan(
			&item.AlertID,
			&item.TenantID,
			&item.EmployeeID,
			&item.EmployeeName,
			&item.EmployeePhone,
			&item.RecipientType,
			&item.DeliveryStatus,
			&item.WeComMessageLogID,
			&item.WeComUserID,
			&item.SentAt,
			&item.ReadAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) ListDeliveryLogs(ctx context.Context, alertID int64) ([]*AlertDeliveryLog, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.id,
		       w.tenant_id,
		       w.employee_id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.phone, '员工#' || e.id::text) AS employee_name,
		       COALESCE(w.wecom_user_id, ''),
		       COALESCE(w.message_scene, ''),
		       COALESCE(w.dedupe_key, ''),
		       COALESCE(w.title, ''),
		       COALESCE(w.content, ''),
		       COALESCE(w.target_url, ''),
		       COALESCE(w.status, ''),
		       COALESCE(w.error_message, ''),
		       COALESCE(w.request_payload, '{}'::jsonb),
		       COALESCE(w.response_payload, '{}'::jsonb),
		       w.created_at,
		       w.updated_at
		FROM wecom_message_logs w
		LEFT JOIN employees e ON e.id = w.employee_id
		WHERE w.message_scene = 'opportunity_alert'
		  AND (
		    w.dedupe_key LIKE 'opportunity_alert:' || $1::text || ':%'
		    OR w.dedupe_key = 'opportunity_alert:' || $1::text
		  )
		ORDER BY w.created_at DESC, w.id DESC
	`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*AlertDeliveryLog, 0)
	for rows.Next() {
		var item AlertDeliveryLog
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.EmployeeID,
			&item.EmployeeName,
			&item.WeComUserID,
			&item.MessageScene,
			&item.DedupeKey,
			&item.Title,
			&item.Content,
			&item.TargetURL,
			&item.Status,
			&item.ErrorMessage,
			&item.RequestPayload,
			&item.ResponsePayload,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) ListRecentDeliveries(ctx context.Context, req RecentDeliveriesRequest) ([]*RecentDelivery, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	where := []string{"1=1"}
	args := []interface{}{}
	if req.TenantID != nil && *req.TenantID > 0 {
		args = append(args, *req.TenantID)
		where = append(where, fmt.Sprintf("r.tenant_id = $%d", len(args)))
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, `
		SELECT r.alert_id,
		       r.tenant_id,
		       a.recording_id,
		       r.employee_id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.phone, '员工#' || e.id::text) AS employee_name,
		       COALESCE(a.customer_name, ''),
		       COALESCE(a.title, ''),
		       COALESCE(a.status, ''),
		       COALESCE(r.recipient_type, ''),
		       COALESCE(r.delivery_status, ''),
		       COALESCE(w.wecom_user_id, ''),
		       r.sent_at,
		       a.created_at
		FROM opportunity_alert_recipients r
		JOIN opportunity_alerts a ON a.id = r.alert_id
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN wecom_message_logs w ON w.id = r.wecom_message_log_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY COALESCE(r.sent_at, a.created_at) DESC, r.updated_at DESC, r.alert_id DESC
		LIMIT $`+fmt.Sprintf("%d", len(args))+`
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*RecentDelivery, 0, limit)
	for rows.Next() {
		var item RecentDelivery
		if err := rows.Scan(
			&item.AlertID,
			&item.TenantID,
			&item.RecordingID,
			&item.EmployeeID,
			&item.EmployeeName,
			&item.CustomerName,
			&item.Title,
			&item.Status,
			&item.RecipientType,
			&item.DeliveryStatus,
			&item.WeComUserID,
			&item.SentAt,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) ListSkippedUnboundAlertIDsByEmployee(ctx context.Context, tenantID, employeeID int64, limit int) ([]int64, error) {
	if tenantID <= 0 || employeeID <= 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.alert_id
		FROM opportunity_alert_recipients r
		JOIN opportunity_alerts a ON a.id = r.alert_id
		WHERE r.tenant_id = $1
		  AND r.employee_id = $2
		  AND COALESCE(r.delivery_status, '') = 'skipped_unbound'
		  AND COALESCE(a.status, '') <> 'handled'
		ORDER BY a.created_at DESC, r.alert_id DESC
		LIMIT $3
	`, tenantID, employeeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, limit)
	seen := make(map[int64]struct{}, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type alertScanner interface {
	Scan(dest ...interface{}) error
}

func scanAlert(row alertScanner) (*Alert, error) {
	var item Alert
	var status string
	var recipientType *string
	if err := row.Scan(
		&item.ID, &item.TenantID, &item.RecordingID, &item.EmployeeID, &item.CustomerID, &item.CustomerName,
		&item.AlertType, &item.Title, &item.Summary, &item.Reason, &item.CustomerObjection, &item.Evidence,
		&item.SuggestedAction, &item.SuggestedScript, &item.Priority, &status, &item.ViewedAt, &item.HandledAt,
		&item.IgnoredAt, &item.DedupeKey, &item.RawPayload, &item.CreatedAt, &item.UpdatedAt, &recipientType, &item.RecipientReadAt,
	); err != nil {
		return nil, err
	}
	item.Status = AlertStatus(status)
	if recipientType != nil && strings.TrimSpace(*recipientType) != "" {
		rt := RecipientType(*recipientType)
		item.RecipientType = &rt
	}
	return &item, nil
}

func scanAlerts(rows pgx.Rows) ([]*Alert, error) {
	items := make([]*Alert, 0)
	for rows.Next() {
		item, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizePriority(priority string) string {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "high", "medium", "low":
		return strings.ToLower(strings.TrimSpace(priority))
	default:
		return "medium"
	}
}
