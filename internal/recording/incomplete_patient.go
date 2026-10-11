package recording

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

type IncompletePatient struct {
	ID            int64      `json:"id"`
	TenantID      int64      `json:"tenant_id"`
	Name          string     `json:"customer_name"`
	Phone         string     `json:"customer_phone"`
	Anonymous     bool       `json:"anonymous_patient"`
	Age           *int       `json:"patient_age,omitempty"`
	Gender        *string    `json:"patient_gender,omitempty"`
	Title         string     `json:"title"`
	Background    string     `json:"background"`
	Evidence      string     `json:"evidence"`
	PhoneScript   string     `json:"phone_script"`
	WechatContent string     `json:"wechat_content"`
	ContactMethod string     `json:"contact_method"`
	SourceType    string     `json:"source_type"`
	SourceDetail  string     `json:"source_detail"`
	DoctorName    string     `json:"doctor_name"`
	RecordedAt    *time.Time `json:"recorded_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (s *Store) ListIncompletePatients(ctx context.Context, tenants []int64, viewerID *int64) ([]IncompletePatient, error) {
	query := `SELECT rt.id,rt.tenant_id,COALESCE(rt.customer_name,''),COALESCE(rt.customer_phone,''),rt.anonymous_patient,rt.patient_age,rt.patient_gender,COALESCE(rt.title,''),COALESCE(rt.background,''),COALESCE(rt.evidence,''),COALESCE(rt.phone_script,''),COALESCE(rt.wechat_content,''),COALESCE(rt.contact_method,''),COALESCE(rt.source_type,''),COALESCE(rt.source_detail,''),COALESCE(e.full_name,e.name,''),COALESCE(r.recorded_at,r.created_at),rt.created_at FROM recording_tasks rt LEFT JOIN recordings r ON r.id=rt.recording_id AND r.tenant_id=rt.tenant_id LEFT JOIN employees e ON e.id=r.employee_id WHERE rt.tenant_id=ANY($1) AND rt.source_type IN ('follow_up','followup') AND (rt.anonymous_patient=true OR NULLIF(trim(rt.customer_name),'') IS NULL OR NULLIF(trim(COALESCE(rt.customer_phone,'')),'') IS NULL) AND rt.status NOT IN ('completed','cancelled')`
	args := []any{tenants}
	if viewerID != nil {
		query += ` AND (rt.assigned_to=$2 OR EXISTS (SELECT 1 FROM recordings vr JOIN followup_data_scopes vs ON vs.tenant_id=rt.tenant_id AND vs.target_employee_id=vr.employee_id AND vs.viewer_employee_id=$2 WHERE vr.id=rt.recording_id))`
		args = append(args, *viewerID)
	}
	query += ` ORDER BY rt.created_at DESC LIMIT 100`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IncompletePatient{}
	for rows.Next() {
		var v IncompletePatient
		if err := rows.Scan(&v.ID, &v.TenantID, &v.Name, &v.Phone, &v.Anonymous, &v.Age, &v.Gender, &v.Title, &v.Background, &v.Evidence, &v.PhoneScript, &v.WechatContent, &v.ContactMethod, &v.SourceType, &v.SourceDetail, &v.DoctorName, &v.RecordedAt, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func validatePatientCompletion(req CompletePatientRequest) error {
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Phone) == "" {
		return fmt.Errorf("name and phone are required")
	}
	if req.Age != nil && (*req.Age < 0 || *req.Age > 130) {
		return fmt.Errorf("age must be between 0 and 130")
	}
	if req.Gender != nil && !strings.Contains("|男|女|其他|未知|", "|"+strings.TrimSpace(*req.Gender)+"|") {
		return fmt.Errorf("invalid gender")
	}
	return nil
}

func (s *Store) CompletePatient(ctx context.Context, tenantID, id int64, req CompletePatientRequest, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var customerID int64
	if err = tx.QueryRow(ctx, `SELECT customer_id FROM recording_tasks WHERE id=$1 AND tenant_id=$2 AND source_type IN ('follow_up','followup') AND status NOT IN ('completed','cancelled') AND (anonymous_patient=true OR NULLIF(trim(customer_name),'') IS NULL OR NULLIF(trim(COALESCE(customer_phone,'')),'') IS NULL) FOR UPDATE`, id, tenantID).Scan(&customerID); err != nil {
		return fmt.Errorf("task not found: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE customers SET name=NULLIF($1,''),phone=NULLIF($2,''),age=$3,gender=$4,extra_data=COALESCE(extra_data,'{}'::jsonb)-'anonymous' WHERE id=$5 AND tenant_id=$6`, strings.TrimSpace(req.Name), strings.TrimSpace(req.Phone), req.Age, req.Gender, customerID, tenantID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE recording_tasks SET customer_name=$1,customer_phone=$2,patient_age=$3,patient_gender=$4,anonymous_patient=false,updated_at=NOW() WHERE id=$5 AND tenant_id=$6`, strings.TrimSpace(req.Name), strings.TrimSpace(req.Phone), req.Age, req.Gender, id, tenantID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO followup_task_audits(tenant_id,task_id,action,detail) VALUES($1,$2,'patient_promoted',jsonb_build_object('customer_id',$3,'age',$4,'gender',$5,'actor_id',$6))`, tenantID, id, customerID, req.Age, req.Gender, actorID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AbandonIncompletePatient(ctx context.Context, tenantID, id, actorID int64, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	if err = tx.QueryRow(ctx, `UPDATE recording_tasks SET status='cancelled',feedback=$1,updated_at=NOW() WHERE id=$2 AND tenant_id=$3 AND source_type IN ('follow_up','followup') AND status NOT IN ('completed','cancelled') AND (anonymous_patient=true OR NULLIF(trim(customer_name),'') IS NULL OR NULLIF(trim(COALESCE(customer_phone,'')),'') IS NULL) RETURNING status`, reason, id, tenantID).Scan(&status); err == pgx.ErrNoRows {
		return fmt.Errorf("incomplete patient lead not found")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO followup_task_audits(tenant_id,task_id,action,detail) VALUES($1,$2,'incomplete_abandoned',jsonb_build_object('reason',$3,'actor_id',$4))`, tenantID, id, reason, actorID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
