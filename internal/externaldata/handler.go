package externaldata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrrecord"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	pool     *pgxpool.Pool
	tenantID int64
	emr      *emrrecord.Service
}

var errEMRDeferred = errors.New("EMR暂不能创建")

func NewHandler(pool *pgxpool.Pool, tenantID int64, emr ...*emrrecord.Service) *Handler {
	h := &Handler{pool: pool, tenantID: tenantID}
	if len(emr) > 0 {
		h.emr = emr[0]
	}
	return h
}
func (h *Handler) RegisterRoutes(mux *http.ServeMux, token string) {
	for _, k := range []string{"patients", "appointments", "queues", "visits"} {
		path := "/internal/v1/external-data/" + k
		router.Register(mux, []router.Route{{Method: http.MethodPost, Path: path, Handler: h.receive(k), AuthMode: "internal"}}, router.RouteDeps{InternalToken: strings.TrimSpace(token)})
	}
	router.Register(mux, []router.Route{{Method: http.MethodGet, Path: "/internal/v1/external-data/deliveries/{id}", Handler: h.getDelivery, AuthMode: "internal"}}, router.RouteDeps{InternalToken: strings.TrimSpace(token)})
	router.Register(mux, []router.Route{{Method: http.MethodGet, Path: "/internal/v1/external-data/queues/waiting", Handler: h.waitingQueues, AuthMode: "internal"}, {Method: http.MethodPost, Path: "/internal/v1/external-data/queues/{id}/claim", Handler: h.claimQueue, AuthMode: "internal"}}, router.RouteDeps{InternalToken: strings.TrimSpace(token)})
}
func (h *Handler) receive(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var e Envelope
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
		if err := dec.Decode(&e); err != nil {
			write(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		if h.tenantID <= 0 {
			write(w, 500, map[string]string{"error": "external data tenant is not configured"})
			return
		}
		if strings.TrimSpace(e.SourceSystem) == "" || strings.TrimSpace(e.SourceRecordID) == "" || strings.TrimSpace(e.IdempotencyKey) == "" || strings.TrimSpace(e.EventType) == "" || e.UpdatedAt.IsZero() {
			write(w, 400, map[string]string{"error": "source_system, source_record_id, event_type, updated_at and idempotency_key are required"})
			return
		}
		if kind != "patients" && strings.TrimSpace(fmt.Sprint(e.Patient["source_patient_id"])) == "" {
			write(w, 400, map[string]string{"error": "patient.source_patient_id is required"})
			return
		}
		if err := validateEnvelope(kind, e); err != nil {
			write(w, 400, map[string]string{"error": err.Error()})
			return
		}
		payload, _ := json.Marshal(e)
		tx, err := h.pool.Begin(r.Context())
		if err != nil {
			write(w, 500, map[string]string{"error": "begin transaction failed"})
			return
		}
		defer tx.Rollback(r.Context())
		var existingStatus, existingResource string
		var existingID string
		var existingUpdated time.Time
		var did string
		dupErr := tx.QueryRow(r.Context(), `SELECT id,status,COALESCE(resource_id,''),COALESCE(source_updated_at,'epoch'::timestamptz) FROM external_data_deliveries WHERE tenant_id=$1 AND object_type=$2 AND source_system=$3 AND source_record_id=$4`, h.tenantID, kind, e.SourceSystem, e.SourceRecordID).Scan(&existingID, &existingStatus, &existingResource, &existingUpdated)
		if dupErr == nil {
			if !e.UpdatedAt.After(existingUpdated) && !isRetryableStatus(existingStatus) {
				_ = tx.Rollback(r.Context())
				write(w, 200, DeliveryResponse{DeliveryID: existingID, Status: "duplicate", TenantID: h.tenantID, ObjectType: kind, SourceRecordID: e.SourceRecordID, ResourceID: existingResource})
				return
			}
			if _, err := tx.Exec(r.Context(), `UPDATE external_data_deliveries SET idempotency_key=$1,payload=$2::jsonb,source_updated_at=$3,status='pending',error_reason=NULL,updated_at=NOW() WHERE id=$4`, e.IdempotencyKey, payload, e.UpdatedAt, existingID); err != nil {
				write(w, 500, map[string]string{"error": "update delivery failed"})
				return
			}
			did = existingID
		} else if dupErr != pgx.ErrNoRows {
			write(w, 500, map[string]string{"error": "query delivery failed"})
			return
		}
		if did == "" {
			var insertErr error
			insertErr = tx.QueryRow(r.Context(), `INSERT INTO external_data_deliveries(tenant_id,object_type,source_system,source_record_id,idempotency_key,payload,source_updated_at,status,attempts) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,'pending',0) RETURNING id`, h.tenantID, kind, e.SourceSystem, e.SourceRecordID, e.IdempotencyKey, payload, e.UpdatedAt).Scan(&did)
			if insertErr != nil {
				write(w, 500, map[string]string{"error": "persist delivery failed"})
				return
			}
		}
		if _, err := tx.Exec(r.Context(), `UPDATE external_data_deliveries SET attempts=attempts+1,status='delivering',updated_at=NOW() WHERE id=$1`, did); err != nil {
			write(w, 500, map[string]string{"error": "update delivery attempt failed"})
			return
		}
		status := "accepted"
		var resource string
		failBusiness := func(cause error) {
			if _, updateErr := tx.Exec(r.Context(), `UPDATE external_data_deliveries SET status='failed',error_reason=$1,updated_at=NOW() WHERE id=$2`, cause.Error(), did); updateErr == nil {
				if commitErr := tx.Commit(r.Context()); commitErr == nil {
					write(w, 422, map[string]string{"error": cause.Error()})
					return
				}
			}
			_ = tx.Rollback(r.Context())
			write(w, 422, map[string]string{"error": cause.Error()})
		}
		if kind == "patients" {
			resource, err = h.upsertPatient(r.Context(), tx, e)
			if err != nil {
				failBusiness(err)
				return
			}
		} else if kind == "appointments" {
			err = h.persistAppointment(r.Context(), tx, did, e)
		} else if kind == "queues" {
			err = h.persistQueue(r.Context(), tx, did, e)
		} else if kind == "visits" {
			resource, err = h.ensureVisitEncounter(r.Context(), tx, e)
		}
		if err != nil {
			failBusiness(err)
			return
		}
		if resource != "" {
			deliveryStatus := "delivered"
			if kind == "visits" {
				deliveryStatus = "pending_emr"
				status = "pending_emr"
			}
			if _, err := tx.Exec(r.Context(), `UPDATE external_data_deliveries SET resource_id=$1, status=$2, updated_at=NOW() WHERE id=$3`, resource, deliveryStatus, did); err != nil {
				write(w, 500, map[string]string{"error": "update delivery resource failed"})
				return
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			write(w, 500, map[string]string{"error": "commit failed"})
			return
		}
		if kind == "visits" && h.emr != nil && resource != "" {
			if err := h.createEMR(r.Context(), e, resource); err != nil {
				if errors.Is(err, errEMRDeferred) {
					write(w, 200, DeliveryResponse{DeliveryID: did, Status: "pending_emr", TenantID: h.tenantID, ObjectType: kind, SourceRecordID: e.SourceRecordID, ResourceID: resource})
					return
				}
				_, _ = h.pool.Exec(r.Context(), `UPDATE external_data_deliveries SET status='failed', error_reason=$1, updated_at=NOW() WHERE id=$2 AND tenant_id=$3`, err.Error(), did, h.tenantID)
				write(w, 422, map[string]string{"error": err.Error()})
				return
			}
			_, _ = h.pool.Exec(r.Context(), `UPDATE external_data_deliveries SET status='delivered', error_reason=NULL, updated_at=NOW() WHERE id=$1 AND tenant_id=$2`, did, h.tenantID)
			status = "delivered"
		}
		write(w, 200, DeliveryResponse{DeliveryID: did, Status: status, TenantID: h.tenantID, ObjectType: kind, SourceRecordID: e.SourceRecordID, ResourceID: resource})
	}
}
func (h *Handler) upsertPatient(ctx context.Context, tx pgx.Tx, e Envelope) (string, error) {
	sid := strings.TrimSpace(fmt.Sprint(e.Patient["source_patient_id"]))
	var cid int64
	err := tx.QueryRow(ctx, `SELECT customer_id FROM external_data_source_identities WHERE tenant_id=$1 AND source_system=$2 AND source_patient_id=$3`, h.tenantID, e.SourceSystem, sid).Scan(&cid)
	if err == pgx.ErrNoRows {
		// Patient identity lives in the envelope; patient master data lives in
		// data according to the external contract. Keep the envelope values as
		// a compatibility fallback for older senders.
		name := strings.TrimSpace(fmt.Sprint(e.Data["name"]))
		if name == "" {
			name = strings.TrimSpace(fmt.Sprint(e.Patient["name"]))
		}
		if name == "" {
			return "", fmt.Errorf("patient.name is required")
		}
		phone := nullString(e.Data["phone"])
		if phone == nil {
			phone = nullString(e.Patient["phone"])
		}
		gender := nullString(e.Data["gender"])
		if gender == nil {
			gender = nullString(e.Patient["gender"])
		}
		age := nullInt(e.Data["age"])
		if age == nil {
			age = nullInt(e.Patient["age"])
		}
		err = tx.QueryRow(ctx, `INSERT INTO customers(tenant_id,name,phone,gender,age,status,momentum,source,lifecycle_stage,extra_data,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'lead',50,$6,'unknown',$7::jsonb,0,NOW(),NOW()) RETURNING id`, h.tenantID, name, phone, gender, age, e.SourceSystem, mustJSON(map[string]any{"external_source_system": e.SourceSystem, "external_source_patient_id": sid, "external_updated_at": e.UpdatedAt.Format(time.RFC3339), "address": e.Data["address"]})).Scan(&cid)
		if err != nil {
			return "", fmt.Errorf("create patient: %w", err)
		}
		evidence, _ := json.Marshal(map[string]any{"source_patient_id": sid, "source_system": e.SourceSystem})
		_, err = tx.Exec(ctx, `INSERT INTO external_data_source_identities(tenant_id,source_system,source_patient_id,customer_id,match_method,match_evidence) VALUES($1,$2,$3,$4,'source_identity',$5::jsonb)`, h.tenantID, e.SourceSystem, sid, cid, evidence)
		if err != nil {
			return "", fmt.Errorf("save source identity: %w", err)
		}
	} else if err != nil {
		return "", err
	} else {
		// Preserve the source identity and only update fields that are present;
		// external data never clears existing manual values.
		_, err = tx.Exec(ctx, `UPDATE customers SET name=COALESCE(NULLIF($1,''),name),phone=COALESCE($2,phone),gender=COALESCE($3,gender),age=COALESCE($4,age),extra_data=COALESCE(extra_data,'{}'::jsonb)||$5::jsonb,updated_at=NOW() WHERE id=$6 AND tenant_id=$7`, strings.TrimSpace(fmt.Sprint(e.Data["name"])), nullString(e.Data["phone"]), nullString(e.Data["gender"]), nullInt(e.Data["age"]), mustJSON(map[string]any{"external_updated_at": e.UpdatedAt.Format(time.RFC3339), "address": e.Data["address"]}), cid, h.tenantID)
		if err != nil {
			return "", fmt.Errorf("update patient: %w", err)
		}
	}
	return fmt.Sprint(cid), nil
}
func (h *Handler) persistAppointment(ctx context.Context, tx pgx.Tx, did string, e Envelope) error {
	sid := strings.TrimSpace(fmt.Sprint(e.Data["source_appointment_id"]))
	if sid == "" {
		return fmt.Errorf("data.source_appointment_id is required")
	}
	patient := strings.TrimSpace(fmt.Sprint(e.Patient["source_patient_id"]))
	var customerID int64
	if err := tx.QueryRow(ctx, `SELECT customer_id FROM external_data_source_identities WHERE tenant_id=$1 AND source_system=$2 AND source_patient_id=$3`, h.tenantID, e.SourceSystem, patient).Scan(&customerID); err != nil {
		return fmt.Errorf("patient identity not found: %s", patient)
	}
	status := strings.TrimSpace(fmt.Sprint(e.Data["status"]))
	updated := e.UpdatedAt
	_, err := tx.Exec(ctx, `INSERT INTO external_data_appointments(delivery_id,tenant_id,source_system,source_appointment_id,customer_id,payload,status,source_updated_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8) ON CONFLICT(tenant_id,source_system,source_appointment_id) DO UPDATE SET delivery_id=EXCLUDED.delivery_id,customer_id=EXCLUDED.customer_id,payload=EXCLUDED.payload,status=EXCLUDED.status,source_updated_at=EXCLUDED.source_updated_at`, did, h.tenantID, e.SourceSystem, sid, customerID, mustJSON(e.Data), status, updated)
	return err
}
func (h *Handler) persistQueue(ctx context.Context, tx pgx.Tx, did string, e Envelope) error {
	sid := strings.TrimSpace(fmt.Sprint(e.Data["source_queue_ticket_id"]))
	if sid == "" {
		return fmt.Errorf("data.source_queue_ticket_id is required")
	}
	patient := strings.TrimSpace(fmt.Sprint(e.Patient["source_patient_id"]))
	var customerID int64
	if err := tx.QueryRow(ctx, `SELECT customer_id FROM external_data_source_identities WHERE tenant_id=$1 AND source_system=$2 AND source_patient_id=$3`, h.tenantID, e.SourceSystem, patient).Scan(&customerID); err != nil {
		return fmt.Errorf("patient identity not found: %s", patient)
	}
	status := strings.TrimSpace(fmt.Sprint(e.Data["status"]))
	updated := e.UpdatedAt
	_, err := tx.Exec(ctx, `INSERT INTO external_data_queues(delivery_id,tenant_id,source_system,source_queue_ticket_id,customer_id,payload,status,source_updated_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8) ON CONFLICT(tenant_id,source_system,source_queue_ticket_id) DO UPDATE SET delivery_id=EXCLUDED.delivery_id,customer_id=EXCLUDED.customer_id,payload=EXCLUDED.payload,status=EXCLUDED.status,source_updated_at=EXCLUDED.source_updated_at`, did, h.tenantID, e.SourceSystem, sid, customerID, mustJSON(e.Data), status, updated)
	return err
}

func (h *Handler) ensureVisitEncounter(ctx context.Context, tx pgx.Tx, e Envelope) (string, error) {
	sid := strings.TrimSpace(fmt.Sprint(e.Patient["source_patient_id"]))
	var patientID int64
	if err := tx.QueryRow(ctx, `SELECT customer_id FROM external_data_source_identities WHERE tenant_id=$1 AND source_system=$2 AND source_patient_id=$3`, h.tenantID, e.SourceSystem, sid).Scan(&patientID); err != nil {
		if err == pgx.ErrNoRows {
			return "", fmt.Errorf("patient identity not found: %s", sid)
		}
		return "", err
	}
	sum := sha256.Sum256([]byte(e.SourceSystem + "\x00" + e.SourceRecordID))
	sourceID := int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
	if sourceID == 0 {
		sourceID = 1
	}
	var encounterID int64
	doctorName := strings.TrimSpace(fmt.Sprint(e.Data["doctor"]))
	department := strings.TrimSpace(fmt.Sprint(e.Data["department"]))
	var doctorID int64
	var departmentID *int64
	// HIS doctor names are source facts. The local employee directory may not
	// contain every historical or external provider, so keep the name and let
	// provider_id remain unset when no tenant employee matches.
	var matchedDepartmentID int64
	matchErr := tx.QueryRow(ctx, `SELECT id, COALESCE(department_id,0) FROM employees WHERE tenant_id=$1 AND deleted_at IS NULL AND is_active=1 AND (full_name=$2 OR name=$2) LIMIT 1`, h.tenantID, doctorName).Scan(&doctorID, &matchedDepartmentID)
	if matchErr == nil {
		if matchedDepartmentID > 0 {
			departmentID = &matchedDepartmentID
		}
	} else if matchErr != pgx.ErrNoRows {
		return "", fmt.Errorf("resolve external doctor: %w", matchErr)
	}
	var patientName string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(name,'') FROM customers WHERE tenant_id=$1 AND id=$2`, h.tenantID, patientID).Scan(&patientName); err != nil {
		return "", fmt.Errorf("load patient customer: %w", err)
	}
	visitType, err := mapEncounterVisitType(fmt.Sprint(e.Data["visit_type"]))
	if err != nil {
		return "", err
	}
	started := e.OccurredAt
	if started.IsZero() {
		started = e.UpdatedAt
	}
	err = tx.QueryRow(ctx, `INSERT INTO encounters(tenant_id,source_type,source_id,patient_id,patient_name,provider_name,department_id,channel,visit_type,started_at,status,created_at,updated_at) VALUES($1,'external_data',$2,$3,NULLIF($4,''),$5,$6,'store',$7,$8,'new',NOW(),NOW()) ON CONFLICT(tenant_id,source_type,source_id) DO UPDATE SET patient_id=EXCLUDED.patient_id,patient_name=EXCLUDED.patient_name,provider_name=EXCLUDED.provider_name,visit_type=EXCLUDED.visit_type,started_at=EXCLUDED.started_at,updated_at=NOW() RETURNING id`, h.tenantID, sourceID, patientID, patientName, doctorName, departmentID, visitType, started).Scan(&encounterID)
	if err != nil {
		return "", fmt.Errorf("ensure encounter: %w", err)
	}
	_ = department
	return fmt.Sprint(encounterID), nil
}

func (h *Handler) waitingQueues(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `SELECT id,source_system,source_queue_ticket_id,customer_id,payload,status FROM external_data_queues WHERE tenant_id=$1 AND status IN ('waiting','called','pending') ORDER BY id`, h.tenantID)
	if err != nil {
		write(w, 500, map[string]string{"error": "query queues failed"})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, source, ticket string
		var customer int64
		var payload []byte
		var status string
		if err := rows.Scan(&id, &source, &ticket, &customer, &payload, &status); err != nil {
			write(w, 500, map[string]string{"error": "scan queue failed"})
			return
		}
		out = append(out, map[string]any{"id": id, "source_system": source, "source_queue_ticket_id": ticket, "customer_id": customer, "payload": json.RawMessage(payload), "status": status})
	}
	write(w, 200, map[string]any{"items": out})
}
func (h *Handler) claimQueue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var claim struct {
		DoctorID int64 `json:"doctor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&claim); err != nil || claim.DoctorID <= 0 {
		write(w, 400, map[string]string{"error": "doctor_id is required"})
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		write(w, 500, map[string]string{"error": "begin transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())
	var doctorName string
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(NULLIF(full_name,''),NULLIF(name,''),'' ) FROM employees WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL AND is_active=1`, claim.DoctorID, h.tenantID).Scan(&doctorName); err != nil {
		write(w, 403, map[string]string{"error": "doctor is not active or not in tenant"})
		return
	}
	var source, ticket string
	var customer int64
	var payload []byte
	var status string
	err = tx.QueryRow(r.Context(), `SELECT source_system,source_queue_ticket_id,customer_id,payload,status FROM external_data_queues WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, h.tenantID, id).Scan(&source, &ticket, &customer, &payload, &status)
	if err == pgx.ErrNoRows {
		write(w, 404, map[string]string{"error": "queue not found"})
		return
	}
	if err != nil {
		write(w, 500, map[string]string{"error": "load queue failed"})
		return
	}
	if status == "completed" || status == "cancelled" {
		write(w, 409, map[string]string{"error": "queue is not claimable"})
		return
	}
	sum := sha256.Sum256([]byte(source + "\x00" + ticket))
	sid := int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
	if sid == 0 {
		sid = 1
	}
	var encounter int64
	err = tx.QueryRow(r.Context(), `INSERT INTO encounters(tenant_id,source_type,source_id,patient_id,patient_name,provider_id,provider_name,channel,visit_type,status,created_at,updated_at) SELECT $1,'external_queue',$2,$3,COALESCE(c.name,''),$4,$5,'store','consultation','new',NOW(),NOW() FROM customers c WHERE c.id=$3 AND c.tenant_id=$1 ON CONFLICT(tenant_id,source_type,source_id) DO UPDATE SET patient_id=EXCLUDED.patient_id,updated_at=NOW() RETURNING id`, h.tenantID, sid, customer, claim.DoctorID, doctorName).Scan(&encounter)
	if err != nil {
		write(w, 422, map[string]string{"error": "create encounter failed"})
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE external_data_queues SET status='called' WHERE tenant_id=$1 AND id=$2`, h.tenantID, id)
	if err != nil {
		write(w, 500, map[string]string{"error": "update queue failed"})
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		write(w, 500, map[string]string{"error": "commit failed"})
		return
	}
	write(w, 200, map[string]any{"status": "claimed", "queue_id": id, "encounter_id": encounter, "doctor_id": claim.DoctorID})
}

func (h *Handler) createEMR(ctx context.Context, e Envelope, encounter string) error {
	var eid int64
	if _, err := fmt.Sscan(encounter, &eid); err != nil {
		return fmt.Errorf("invalid encounter id")
	}
	doctorName := strings.TrimSpace(fmt.Sprint(e.Data["doctor"]))
	var doctorID int64
	var departmentID *int64
	var dept int64
	matchErr := h.pool.QueryRow(ctx, `SELECT id, COALESCE(department_id,0) FROM employees WHERE tenant_id=$1 AND deleted_at IS NULL AND is_active=1 AND (full_name=$2 OR name=$2) LIMIT 1`, h.tenantID, doctorName).Scan(&doctorID, &dept)
	if matchErr == pgx.ErrNoRows {
		// An external visit is still valid when its historical provider is not
		// present in the current employee directory. Encounter creation already
		// preserves provider_name; EMR creation waits for a local doctor mapping.
		return nil
	}
	if matchErr != nil {
		return fmt.Errorf("resolve external doctor: %w", matchErr)
	}
	if dept > 0 {
		departmentID = &dept
	}
	var patientID int64
	if err := h.pool.QueryRow(ctx, `SELECT customer_id FROM external_data_source_identities WHERE tenant_id=$1 AND source_system=$2 AND source_patient_id=$3`, h.tenantID, e.SourceSystem, strings.TrimSpace(fmt.Sprint(e.Patient["source_patient_id"]))).Scan(&patientID); err != nil {
		return fmt.Errorf("patient identity not found")
	}
	var templateID string
	if err := h.pool.QueryRow(ctx, `SELECT id FROM emr_templates WHERE tenant_id IN ($1,0) AND code='standard_outpatient' AND status='published' AND is_current=true ORDER BY CASE WHEN tenant_id=$1 THEN 0 ELSE 1 END LIMIT 1`, h.tenantID).Scan(&templateID); err != nil {
		return fmt.Errorf("load outpatient template: %w", err)
	}
	var existing string
	if err := h.pool.QueryRow(ctx, `SELECT id FROM emr_records WHERE tenant_id=$1 AND encounter_id=$2 LIMIT 1`, h.tenantID, eid).Scan(&existing); err == nil {
		return nil
	}
	content := map[string]any{"chief_complaint": fmt.Sprint(e.Data["chief_complaint"]), "auxiliary_exam": fmt.Sprint(e.Data["examination"]), "diagnosis": map[string]any{"diagnosis_name": fmt.Sprint(e.Data["diagnosis"])}, "disposition": fmt.Sprint(e.Data["treatment_plan"]), "external_source_system": e.SourceSystem, "external_updated_at": e.UpdatedAt.Format(time.RFC3339)}
	if p, ok := e.Data["prescription"]; ok {
		content["prescription"] = p
	}
	started := e.OccurredAt
	_, err := h.emr.Create(ctx, h.tenantID, 0, emrrecord.CreateRequest{EncounterID: eid, PatientID: &patientID, PatientSnapshot: e.Patient, TemplateVersionID: templateID, DocumentType: "门诊病历", VisitType: fmt.Sprint(e.Data["visit_type"]), StartedAt: &started, SourceReferences: map[string]any{"source_system": e.SourceSystem, "source_record_id": e.SourceRecordID, "updated_at": e.UpdatedAt.Format(time.RFC3339)}, DoctorID: &doctorID, DepartmentID: departmentID, Content: content})
	return err
}

func (h *Handler) getDelivery(w http.ResponseWriter, r *http.Request) {
	var d DeliveryResponse
	var errReason sql.NullString
	err := h.pool.QueryRow(r.Context(), `SELECT id,status,tenant_id,object_type,source_record_id,COALESCE(resource_id,''),error_reason FROM external_data_deliveries WHERE id=$1 AND tenant_id=$2`, r.PathValue("id"), h.tenantID).Scan(&d.DeliveryID, &d.Status, &d.TenantID, &d.ObjectType, &d.SourceRecordID, &d.ResourceID, &errReason)
	if err == pgx.ErrNoRows {
		write(w, 404, map[string]string{"error": "delivery not found"})
		return
	}
	if err != nil {
		write(w, 500, map[string]string{"error": "query failed"})
		return
	}
	if errReason.Valid {
		d.Error = errReason.String
	}
	write(w, 200, d)
}
func nullString(v any) *string {
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || s == "<nil>" {
		return nil
	}
	return &s
}
func nullInt(v any) *int {
	switch x := v.(type) {
	case float64:
		i := int(x)
		return &i
	case int:
		i := x
		return &i
	}
	return nil
}
func mapEncounterVisitType(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "初诊", "first_visit":
		return "first_visit", nil
	case "复诊", "return_visit":
		return "return_visit", nil
	case "咨询", "consultation":
		return "consultation", nil
	case "随访", "follow_up":
		return "follow_up", nil
	default:
		return "", fmt.Errorf("unsupported visit_type: %s", value)
	}
}
func isRetryableStatus(status string) bool {
	switch status {
	case "failed", "pending", "delivering", "pending_emr", "retrying":
		return true
	default:
		return false
	}
}
func validateEnvelope(kind string, e Envelope) error {
	if !strings.HasPrefix(strings.TrimSpace(e.EventType), strings.TrimSuffix(kind, "s")+".") {
		return fmt.Errorf("event_type does not match object type")
	}
	if e.Data == nil {
		return fmt.Errorf("data is required")
	}
	if kind == "patients" {
		if strings.TrimSpace(fmt.Sprint(e.Data["name"])) == "" {
			return fmt.Errorf("data.name is required")
		}
		return nil
	}
	if strings.TrimSpace(fmt.Sprint(e.Patient["source_patient_id"])) == "" {
		return fmt.Errorf("patient.source_patient_id is required")
	}
	switch kind {
	case "visits":
		for _, key := range []string{"doctor", "department", "chief_complaint", "diagnosis"} {
			if strings.TrimSpace(fmt.Sprint(e.Data[key])) == "" {
				return fmt.Errorf("data.%s is required", key)
			}
		}
	case "appointments":
		for _, key := range []string{"source_appointment_id", "appointment_at", "department", "doctor", "project", "status"} {
			if strings.TrimSpace(fmt.Sprint(e.Data[key])) == "" {
				return fmt.Errorf("data.%s is required", key)
			}
		}
	case "queues":
		for _, key := range []string{"source_queue_ticket_id", "visit_date", "queue_number", "department", "doctor", "status"} {
			if strings.TrimSpace(fmt.Sprint(e.Data[key])) == "" {
				return fmt.Errorf("data.%s is required", key)
			}
		}
	}
	return nil
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
