package his

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// 检查（PACS）数据访问：申请单 + 报告（20261113_004_his_exam）。

// CreateExamRequest 开检查申请：校验 encounter 属于本租户，按 scope='exam' 发号后落库。
func (s *Store) CreateExamRequest(ctx context.Context, tenantID, createdBy int64, req CreateExamRequestRequest) (ExamRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ExamRequest{}, err
	}
	defer tx.Rollback(ctx)

	var customerID int64
	var customerName, mrn string
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(e.patient_id, r.customer_id),
		       COALESCE(c.name, ''),
		       COALESCE((
		           SELECT p.value FROM patient_identifiers p
		           WHERE p.tenant_id = e.tenant_id
		             AND p.customer_id = COALESCE(e.patient_id, r.customer_id)
		             AND p.identifier_type = 'mrn' AND p.status = 'active'
		           ORDER BY p.id DESC LIMIT 1
		       ), '')
		FROM encounters e
		LEFT JOIN his_registrations r
		       ON r.tenant_id = e.tenant_id AND e.source_type = 'his' AND e.source_id = r.id
		LEFT JOIN customers c
		       ON c.tenant_id = e.tenant_id AND c.id = COALESCE(e.patient_id, r.customer_id)
		WHERE e.tenant_id = $1 AND e.id = $2`, tenantID, req.EncounterID).
		Scan(&customerID, &customerName, &mrn)
	if err == pgx.ErrNoRows {
		return ExamRequest{}, ErrNotFound
	}
	if err != nil {
		return ExamRequest{}, err
	}
	if customerID <= 0 {
		return ExamRequest{}, ErrNotFound
	}

	seq, err := nextNumber(ctx, tx, tenantID, "exam")
	if err != nil {
		return ExamRequest{}, err
	}
	requestNo := examRequestNoFormat(seq, time.Now())

	// 收费联动：显式选择的收费项目，或按项目名自动匹配「检查」类收费项目；
	// 命中则在同一事务内生成 pending 收费单（挂单），未命中则金额 0、不建单。
	charge, err := s.attachChargeToLabExamRequestTx(ctx, tx, tenantID, createdBy, customerID,
		req.EncounterID, req.ChargeItemID, req.ItemName, chargeCategoryExam)
	if err != nil {
		return ExamRequest{}, err
	}

	var item ExamRequest
	err = tx.QueryRow(ctx, `
		INSERT INTO his_exam_requests (
			tenant_id, encounter_id, customer_id, request_no, item_name, body_part,
			purpose, status, created_by, charge_item_id, charge_amount, order_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, 0), $10, $11, $12)
		RETURNING id, encounter_id, customer_id, request_no, item_name, body_part, purpose,
		          status, started_at, reported_at, created_by, created_at, updated_at,
		          charge_item_id, charge_amount, order_id`,
		tenantID, req.EncounterID, customerID, requestNo, req.ItemName, req.BodyPart,
		req.Purpose, examStatusPending, createdBy,
		charge.ChargeItemID, charge.ChargeAmount, charge.OrderID).
		Scan(&item.ID, &item.EncounterID, &item.CustomerID, &item.RequestNo, &item.ItemName,
			&item.BodyPart, &item.Purpose, &item.Status, &item.StartedAt, &item.ReportedAt,
			&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
			&item.ChargeItemID, &item.ChargeAmount, &item.OrderID)
	if err != nil {
		return ExamRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExamRequest{}, err
	}

	item.CustomerName = customerName
	item.MRN = mrn
	item.ReportCount = 0
	return item, nil
}

const examRequestSelectSQL = `
	SELECT e.id, e.encounter_id, e.customer_id, e.request_no, e.item_name, e.body_part,
	       e.purpose, e.status, e.started_at, e.reported_at, e.created_by, e.created_at, e.updated_at,
	       COALESCE(c.name, ''), COALESCE(pi.value, ''), COALESCE(rc.cnt, 0),
	       e.charge_item_id, e.charge_amount, e.order_id
	FROM his_exam_requests e
	LEFT JOIN customers c ON c.tenant_id = e.tenant_id AND c.id = e.customer_id
	LEFT JOIN LATERAL (
		SELECT p.value FROM patient_identifiers p
		WHERE p.tenant_id = e.tenant_id AND p.customer_id = e.customer_id
		  AND p.identifier_type = 'mrn' AND p.status = 'active'
		ORDER BY p.id DESC LIMIT 1
	) pi ON TRUE
	LEFT JOIN LATERAL (
		SELECT COUNT(*) AS cnt FROM his_exam_reports r WHERE r.request_id = e.id
	) rc ON TRUE`

func scanExamRequest(row pgx.Row) (ExamRequest, error) {
	var item ExamRequest
	err := row.Scan(&item.ID, &item.EncounterID, &item.CustomerID, &item.RequestNo, &item.ItemName,
		&item.BodyPart, &item.Purpose, &item.Status, &item.StartedAt, &item.ReportedAt,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &item.CustomerName, &item.MRN,
		&item.ReportCount, &item.ChargeItemID, &item.ChargeAmount, &item.OrderID)
	return item, err
}

// ListExamRequests 检查工作站列表：状态集合、开单日期、关键字（患者/病案号/项目/部位）筛选。
func (s *Store) ListExamRequests(ctx context.Context, tenantID int64, statuses []string, date, keyword string) ([]ExamRequest, error) {
	if statuses == nil {
		statuses = []string{}
	}
	rows, err := s.pool.Query(ctx, examRequestSelectSQL+`
		WHERE e.tenant_id = $1
		  AND (cardinality($2::text[]) = 0 OR e.status = ANY($2::text[]))
		  AND (NULLIF($3, '') IS NULL OR e.created_at::date = NULLIF($3, '')::date)
		  AND (NULLIF($4, '') IS NULL
		       OR e.request_no ILIKE '%' || $4 || '%'
		       OR e.item_name ILIKE '%' || $4 || '%'
		       OR e.body_part ILIKE '%' || $4 || '%'
		       OR COALESCE(c.name, '') ILIKE '%' || $4 || '%'
		       OR COALESCE(pi.value, '') ILIKE '%' || $4 || '%')
		ORDER BY e.created_at DESC, e.id DESC`, tenantID, statuses, date, keyword)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ExamRequest, 0)
	for rows.Next() {
		item, err := scanExamRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetExamRequest 申请详情（含报告；无报告时 report 为空）。
func (s *Store) GetExamRequest(ctx context.Context, tenantID, id int64) (ExamRequest, error) {
	item, err := scanExamRequest(s.pool.QueryRow(ctx, examRequestSelectSQL+`
		WHERE e.tenant_id = $1 AND e.id = $2`, tenantID, id))
	if err == pgx.ErrNoRows {
		return ExamRequest{}, ErrNotFound
	}
	if err != nil {
		return ExamRequest{}, err
	}

	var report ExamReport
	var refsRaw []byte
	err = s.pool.QueryRow(ctx, `
		SELECT id, request_id, findings, impression, suggestion, image_refs, reported_by, reported_at
		FROM his_exam_reports WHERE request_id = $1 ORDER BY id DESC LIMIT 1`, id).
		Scan(&report.ID, &report.RequestID, &report.Findings, &report.Impression, &report.Suggestion,
			&refsRaw, &report.ReportedBy, &report.ReportedAt)
	switch {
	case err == pgx.ErrNoRows:
		// 无报告
	case err != nil:
		return ExamRequest{}, err
	default:
		if len(refsRaw) > 0 {
			if err := json.Unmarshal(refsRaw, &report.ImageRefs); err != nil {
				return ExamRequest{}, err
			}
		}
		if report.ImageRefs == nil {
			report.ImageRefs = []string{}
		}
		item.Report = &report
		item.ReportCount = 1
	}
	return item, nil
}

// transitionExamRequest 状态机变更公共实现：锁行 → 校验状态 → 更新状态与时间戳。
func (s *Store) transitionExamRequest(ctx context.Context, tenantID, id int64, action, timestampColumn string) (ExamRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ExamRequest{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	var orderID *int64
	err = tx.QueryRow(ctx, `
		SELECT status, order_id FROM his_exam_requests
		WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, id).Scan(&status, &orderID)
	if err == pgx.ErrNoRows {
		return ExamRequest{}, ErrNotFound
	}
	if err != nil {
		return ExamRequest{}, err
	}
	if err := examTransitionAllowed(status, action); err != nil {
		return ExamRequest{}, err
	}
	nextStatus, err := nextExamStatus(action)
	if err != nil {
		return ExamRequest{}, err
	}

	query := `UPDATE his_exam_requests SET status = $3, updated_at = NOW() WHERE tenant_id = $1 AND id = $2`
	if timestampColumn != "" {
		query = `UPDATE his_exam_requests SET status = $3, ` + timestampColumn + ` = NOW(), updated_at = NOW()
			WHERE tenant_id = $1 AND id = $2`
	}
	if action == examActionCancel && orderID != nil {
		if err := s.voidPendingOrderTx(ctx, tx, tenantID, *orderID); err != nil {
			return ExamRequest{}, err
		}
	}
	if _, err := tx.Exec(ctx, query, tenantID, id, nextStatus); err != nil {
		return ExamRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExamRequest{}, err
	}
	return s.GetExamRequest(ctx, tenantID, id)
}

// StartExamRequest 开始检查：pending → in_progress，写 started_at。
func (s *Store) StartExamRequest(ctx context.Context, tenantID, id int64) (ExamRequest, error) {
	return s.transitionExamRequest(ctx, tenantID, id, examActionStart, "started_at")
}

// CancelExamRequest 取消：仅 pending（不发新号、不删除记录，保留审计）。
func (s *Store) CancelExamRequest(ctx context.Context, tenantID, id int64) (ExamRequest, error) {
	return s.transitionExamRequest(ctx, tenantID, id, examActionCancel, "")
}

// ReportExamRequest 录入报告：in_progress → reported，事务内 upsert 报告并写 reported_at。
func (s *Store) ReportExamRequest(ctx context.Context, tenantID, reportedBy, id int64, req ReportExamRequestRequest) (ExamRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ExamRequest{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	var orderID *int64
	err = tx.QueryRow(ctx, `
		SELECT status, order_id FROM his_exam_requests
		WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, id).Scan(&status, &orderID)
	if err == pgx.ErrNoRows {
		return ExamRequest{}, ErrNotFound
	}
	if err != nil {
		return ExamRequest{}, err
	}
	if err := examTransitionAllowed(status, examActionReport); err != nil {
		return ExamRequest{}, err
	}

	refsJSON, err := json.Marshal(req.ImageRefs)
	if err != nil {
		return ExamRequest{}, err
	}

	// upsert：同一申请单只保留一份报告（表未加唯一约束，用更新/插入两步实现）。
	tag, err := tx.Exec(ctx, `
		UPDATE his_exam_reports
		SET findings = $2, impression = $3, suggestion = $4, image_refs = $5::jsonb,
		    reported_by = NULLIF($6, 0), reported_at = NOW()
		WHERE request_id = $1`,
		id, req.Findings, req.Impression, req.Suggestion, string(refsJSON), reportedBy)
	if err != nil {
		return ExamRequest{}, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_exam_reports (
				request_id, findings, impression, suggestion, image_refs, reported_by, reported_at
			)
			VALUES ($1, $2, $3, $4, $5::jsonb, NULLIF($6, 0), NOW())`,
			id, req.Findings, req.Impression, req.Suggestion, string(refsJSON), reportedBy); err != nil {
			return ExamRequest{}, err
		}
	}

	nextStatus, err := nextExamStatus(examActionReport)
	if err != nil {
		return ExamRequest{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_exam_requests SET status = $3, reported_at = NOW(), updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, nextStatus); err != nil {
		return ExamRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExamRequest{}, err
	}
	return s.GetExamRequest(ctx, tenantID, id)
}
