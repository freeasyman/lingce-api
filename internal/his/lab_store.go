package his

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// 检验（LIS）数据访问：申请单 + 结果项（20261113_003_his_lab）。

// CreateLabRequest 开检验申请：校验 encounter 属于本租户，按 scope='lab' 发号后落库。
func (s *Store) CreateLabRequest(ctx context.Context, tenantID, createdBy int64, req CreateLabRequestRequest) (LabRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return LabRequest{}, err
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
		return LabRequest{}, ErrNotFound
	}
	if err != nil {
		return LabRequest{}, err
	}
	if customerID <= 0 {
		return LabRequest{}, ErrNotFound
	}

	seq, err := nextNumber(ctx, tx, tenantID, "lab")
	if err != nil {
		return LabRequest{}, err
	}
	requestNo := labRequestNoFormat(seq, time.Now())

	var item LabRequest
	err = tx.QueryRow(ctx, `
		INSERT INTO his_lab_requests (
			tenant_id, encounter_id, customer_id, request_no, item_name, specimen_type,
			purpose, status, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, 0))
		RETURNING id, encounter_id, customer_id, request_no, item_name, specimen_type, purpose,
		          status, sampled_at, reported_at, created_by, created_at, updated_at`,
		tenantID, req.EncounterID, customerID, requestNo, req.ItemName, req.SpecimenType,
		req.Purpose, labStatusPending, createdBy).
		Scan(&item.ID, &item.EncounterID, &item.CustomerID, &item.RequestNo, &item.ItemName,
			&item.SpecimenType, &item.Purpose, &item.Status, &item.SampledAt, &item.ReportedAt,
			&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return LabRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return LabRequest{}, err
	}

	item.CustomerName = customerName
	item.MRN = mrn
	item.ResultItemCount = 0
	return item, nil
}

const labRequestSelectSQL = `
	SELECT l.id, l.encounter_id, l.customer_id, l.request_no, l.item_name, l.specimen_type,
	       l.purpose, l.status, l.sampled_at, l.reported_at, l.created_by, l.created_at, l.updated_at,
	       COALESCE(c.name, ''), COALESCE(pi.value, ''), COALESCE(rc.cnt, 0)
	FROM his_lab_requests l
	LEFT JOIN customers c ON c.tenant_id = l.tenant_id AND c.id = l.customer_id
	LEFT JOIN LATERAL (
		SELECT p.value FROM patient_identifiers p
		WHERE p.tenant_id = l.tenant_id AND p.customer_id = l.customer_id
		  AND p.identifier_type = 'mrn' AND p.status = 'active'
		ORDER BY p.id DESC LIMIT 1
	) pi ON TRUE
	LEFT JOIN LATERAL (
		SELECT COUNT(*) AS cnt FROM his_lab_result_items ri WHERE ri.request_id = l.id
	) rc ON TRUE`

func scanLabRequest(row pgx.Row) (LabRequest, error) {
	var item LabRequest
	err := row.Scan(&item.ID, &item.EncounterID, &item.CustomerID, &item.RequestNo, &item.ItemName,
		&item.SpecimenType, &item.Purpose, &item.Status, &item.SampledAt, &item.ReportedAt,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &item.CustomerName, &item.MRN,
		&item.ResultItemCount)
	return item, err
}

// ListLabRequests 检验工作站列表：状态集合、开单日期、关键字（患者/病案号/项目）筛选。
func (s *Store) ListLabRequests(ctx context.Context, tenantID int64, statuses []string, date, keyword string) ([]LabRequest, error) {
	if statuses == nil {
		statuses = []string{}
	}
	rows, err := s.pool.Query(ctx, labRequestSelectSQL+`
		WHERE l.tenant_id = $1
		  AND (cardinality($2::text[]) = 0 OR l.status = ANY($2::text[]))
		  AND (NULLIF($3, '') IS NULL OR l.created_at::date = NULLIF($3, '')::date)
		  AND (NULLIF($4, '') IS NULL
		       OR l.request_no ILIKE '%' || $4 || '%'
		       OR l.item_name ILIKE '%' || $4 || '%'
		       OR COALESCE(c.name, '') ILIKE '%' || $4 || '%'
		       OR COALESCE(pi.value, '') ILIKE '%' || $4 || '%')
		ORDER BY l.created_at DESC, l.id DESC`, tenantID, statuses, date, keyword)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]LabRequest, 0)
	for rows.Next() {
		item, err := scanLabRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetLabRequest 申请详情（含结果项，按 id 升序）。
func (s *Store) GetLabRequest(ctx context.Context, tenantID, id int64) (LabRequest, error) {
	item, err := scanLabRequest(s.pool.QueryRow(ctx, labRequestSelectSQL+`
		WHERE l.tenant_id = $1 AND l.id = $2`, tenantID, id))
	if err == pgx.ErrNoRows {
		return LabRequest{}, ErrNotFound
	}
	if err != nil {
		return LabRequest{}, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, request_id, item_code, item_name, value, unit, reference_range, flag
		FROM his_lab_result_items WHERE request_id = $1 ORDER BY id ASC`, id)
	if err != nil {
		return LabRequest{}, err
	}
	defer rows.Close()

	resultItems := make([]LabResultItem, 0)
	for rows.Next() {
		var result LabResultItem
		if err := rows.Scan(&result.ID, &result.RequestID, &result.ItemCode, &result.ItemName,
			&result.Value, &result.Unit, &result.ReferenceRange, &result.Flag); err != nil {
			return LabRequest{}, err
		}
		resultItems = append(resultItems, result)
	}
	if err := rows.Err(); err != nil {
		return LabRequest{}, err
	}
	item.ResultItems = resultItems
	item.ResultItemCount = len(resultItems)
	return item, nil
}

// transitionLabRequest 状态机变更公共实现：锁行 → 校验状态 → 更新状态与时间戳。
func (s *Store) transitionLabRequest(ctx context.Context, tenantID, id int64, action string, timestampColumn string) (LabRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return LabRequest{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_lab_requests
		WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, id).Scan(&status)
	if err == pgx.ErrNoRows {
		return LabRequest{}, ErrNotFound
	}
	if err != nil {
		return LabRequest{}, err
	}
	if err := labTransitionAllowed(status, action); err != nil {
		return LabRequest{}, err
	}
	nextStatus, err := nextLabStatus(action)
	if err != nil {
		return LabRequest{}, err
	}

	query := `UPDATE his_lab_requests SET status = $3, updated_at = NOW() WHERE tenant_id = $1 AND id = $2`
	if timestampColumn != "" {
		query = `UPDATE his_lab_requests SET status = $3, ` + timestampColumn + ` = NOW(), updated_at = NOW()
			WHERE tenant_id = $1 AND id = $2`
	}
	if _, err := tx.Exec(ctx, query, tenantID, id, nextStatus); err != nil {
		return LabRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return LabRequest{}, err
	}
	return s.GetLabRequest(ctx, tenantID, id)
}

// SampleLabRequest 采样：pending → sampled，写 sampled_at。
func (s *Store) SampleLabRequest(ctx context.Context, tenantID, id int64) (LabRequest, error) {
	return s.transitionLabRequest(ctx, tenantID, id, labActionSample, "sampled_at")
}

// CancelLabRequest 取消：仅 pending（不发新号、不删除记录，保留审计）。
func (s *Store) CancelLabRequest(ctx context.Context, tenantID, id int64) (LabRequest, error) {
	return s.transitionLabRequest(ctx, tenantID, id, labActionCancel, "")
}

// ReportLabRequest 录入结果：sampled → reported，事务内替换全部结果项并写 reported_at。
func (s *Store) ReportLabRequest(ctx context.Context, tenantID, id int64, items []LabResultItemInput) (LabRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return LabRequest{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_lab_requests
		WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, id).Scan(&status)
	if err == pgx.ErrNoRows {
		return LabRequest{}, ErrNotFound
	}
	if err != nil {
		return LabRequest{}, err
	}
	if err := labTransitionAllowed(status, labActionReport); err != nil {
		return LabRequest{}, err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM his_lab_result_items WHERE request_id = $1`, id); err != nil {
		return LabRequest{}, err
	}
	for _, item := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_lab_result_items (
				request_id, item_code, item_name, value, unit, reference_range, flag
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id, item.ItemCode, item.ItemName, item.Value, item.Unit, item.ReferenceRange, item.Flag); err != nil {
			return LabRequest{}, err
		}
	}

	nextStatus, err := nextLabStatus(labActionReport)
	if err != nil {
		return LabRequest{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_lab_requests SET status = $3, reported_at = NOW(), updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, nextStatus); err != nil {
		return LabRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return LabRequest{}, err
	}
	return s.GetLabRequest(ctx, tenantID, id)
}
