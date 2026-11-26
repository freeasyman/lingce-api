package content

import (
	"context"
	"time"
)

// FreeCheckupLead 是一条免费体检线索（独立产品，不挂真实机构）。
type FreeCheckupLead struct {
	ID         int64     `json:"id"`
	RunID      int64     `json:"run_id"`
	OrgName    string    `json:"org_name"`
	Region     string    `json:"region"`
	Question   string    `json:"question"`
	DoctorName string    `json:"doctor_name"`
	Phone      string    `json:"phone"`
	Scene      string    `json:"scene"`
	Status     string    `json:"status"`
	Note       string    `json:"note"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	// 关联 run 的摘要（列表页用）
	RunStatus string `json:"run_status,omitempty"`
	Progress  int    `json:"progress,omitempty"`
	Total     int    `json:"total,omitempty"`
}

const freeCheckupLeadSelect = `
	SELECT l.id, l.run_id, l.org_name, l.region, l.question, l.doctor_name, l.phone, l.scene, l.status, l.note, l.created_at, l.updated_at,
	       COALESCE(r.status,''), COALESCE(r.progress,0), COALESCE(r.total,0)
	FROM free_checkup_leads l
	LEFT JOIN content_ai_visibility_runs r ON r.id = l.run_id AND r.tenant_id = $1`

func (s *Store) InsertFreeCheckupLead(ctx context.Context, l *FreeCheckupLead) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO free_checkup_leads (run_id, org_name, region, question, doctor_name, phone, scene, status, note)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		l.RunID, l.OrgName, l.Region, l.Question, l.DoctorName, l.Phone, l.Scene, l.Status, l.Note).Scan(&id)
	return id, err
}

// ListFreeCheckupLeads 运营控制台列表：按状态筛选、分页，附带 run 摘要。
func (s *Store) ListFreeCheckupLeads(ctx context.Context, status string, limit, offset int) ([]FreeCheckupLead, int, error) {
	where := ""
	args := []any{PublicCheckupTenantID}
	if status != "" {
		where = " WHERE l.status = $2"
		args = append(args, status)
	}
	var total int
	countSQL := "SELECT count(*) FROM free_checkup_leads l" + where
	if status != "" {
		countSQL = "SELECT count(*) FROM free_checkup_leads l WHERE l.status = $1"
		if err := s.pool.QueryRow(ctx, countSQL, status).Scan(&total); err != nil {
			return nil, 0, err
		}
	} else if err := s.pool.QueryRow(ctx, countSQL).Scan(&total); err != nil {
		return nil, 0, err
	}

	q := freeCheckupLeadSelect + where + " ORDER BY l.created_at DESC LIMIT $2 OFFSET $3"
	args = append(args, limit, offset)
	rs, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rs.Close()
	var rows []FreeCheckupLead
	for rs.Next() {
		var l FreeCheckupLead
		if err := rs.Scan(&l.ID, &l.RunID, &l.OrgName, &l.Region, &l.Question, &l.DoctorName, &l.Phone, &l.Scene, &l.Status, &l.Note, &l.CreatedAt, &l.UpdatedAt, &l.RunStatus, &l.Progress, &l.Total); err != nil {
			return nil, 0, err
		}
		rows = append(rows, l)
	}
	return rows, total, rs.Err()
}

func (s *Store) GetFreeCheckupLead(ctx context.Context, id int64) (*FreeCheckupLead, error) {
	var l FreeCheckupLead
	err := s.pool.QueryRow(ctx, freeCheckupLeadSelect+` WHERE l.id = $2`, PublicCheckupTenantID, id).
		Scan(&l.ID, &l.RunID, &l.OrgName, &l.Region, &l.Question, &l.DoctorName, &l.Phone, &l.Scene, &l.Status, &l.Note, &l.CreatedAt, &l.UpdatedAt, &l.RunStatus, &l.Progress, &l.Total)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *Store) UpdateFreeCheckupLeadStatus(ctx context.Context, id int64, status, note string) error {
	_, err := s.pool.Exec(ctx, `UPDATE free_checkup_leads SET status=$1, note=$2, updated_at=NOW() WHERE id=$3`, status, note, id)
	return err
}

func (s *Store) FreeCheckupLeadStats(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	rows, err := s.pool.Query(ctx, `SELECT status, count(*) FROM free_checkup_leads GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	var today int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_leads WHERE created_at >= CURRENT_DATE`).Scan(&today); err == nil {
		out["today"] = today
	}
	return out, rows.Err()
}
