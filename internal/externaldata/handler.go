package externaldata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	pool     *pgxpool.Pool
	tenantID int64
}

func NewHandler(pool *pgxpool.Pool, tenantID int64) *Handler {
	return &Handler{pool: pool, tenantID: tenantID}
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
		payload, _ := json.Marshal(e)
		tx, err := h.pool.Begin(r.Context())
		if err != nil {
			write(w, 500, map[string]string{"error": "begin transaction failed"})
			return
		}
		defer tx.Rollback(r.Context())
		var did string
		err = tx.QueryRow(r.Context(), `INSERT INTO external_data_deliveries(tenant_id,object_type,source_system,source_record_id,idempotency_key,payload,status,attempts) VALUES($1,$2,$3,$4,$5,$6::jsonb,'delivered',1) ON CONFLICT(tenant_id,object_type,source_system,source_record_id) DO UPDATE SET updated_at=NOW() RETURNING id`, h.tenantID, kind, e.SourceSystem, e.SourceRecordID, e.IdempotencyKey, payload).Scan(&did)
		if err != nil {
			write(w, 500, map[string]string{"error": "persist delivery failed"})
			return
		}
		status := "accepted"
		var resource string
		if kind == "patients" {
			resource, err = h.upsertPatient(r.Context(), tx, e)
			if err != nil {
				write(w, 422, map[string]string{"error": err.Error()})
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
			write(w, 422, map[string]string{"error": err.Error()})
			return
		}
		if resource != "" {
			if _, err := tx.Exec(r.Context(), `UPDATE external_data_deliveries SET resource_id=$1, status='delivered', updated_at=NOW() WHERE id=$2`, resource, did); err != nil {
				write(w, 500, map[string]string{"error": "update delivery resource failed"})
				return
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			write(w, 500, map[string]string{"error": "commit failed"})
			return
		}
		write(w, 200, DeliveryResponse{DeliveryID: did, Status: status, TenantID: h.tenantID, ObjectType: kind, SourceRecordID: e.SourceRecordID, ResourceID: resource})
	}
}
func (h *Handler) upsertPatient(ctx context.Context, tx pgx.Tx, e Envelope) (string, error) {
	sid := strings.TrimSpace(fmt.Sprint(e.Patient["source_patient_id"]))
	var cid int64
	err := tx.QueryRow(ctx, `SELECT customer_id FROM external_data_source_identities WHERE tenant_id=$1 AND source_system=$2 AND source_patient_id=$3`, h.tenantID, e.SourceSystem, sid).Scan(&cid)
	if err == pgx.ErrNoRows {
		name := strings.TrimSpace(fmt.Sprint(e.Patient["name"]))
		if name == "" {
			return "", fmt.Errorf("patient.name is required")
		}
		phone := nullString(e.Patient["phone"])
		gender := nullString(e.Patient["gender"])
		age := nullInt(e.Patient["age"])
		err = tx.QueryRow(ctx, `INSERT INTO customers(tenant_id,name,phone,gender,age,status,momentum,source,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'lead',50,$6,0,NOW(),NOW()) RETURNING id`, h.tenantID, name, phone, gender, age, e.SourceSystem).Scan(&cid)
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
	}
	return fmt.Sprint(cid), nil
}
func (h *Handler) persistAppointment(ctx context.Context, tx pgx.Tx, did string, e Envelope) error {
	sid := strings.TrimSpace(fmt.Sprint(e.Data["source_appointment_id"]))
	if sid == "" {
		return fmt.Errorf("data.source_appointment_id is required")
	}
	_, err := tx.Exec(ctx, `INSERT INTO external_data_appointments(delivery_id,tenant_id,source_system,source_appointment_id,payload,status) VALUES($1,$2,$3,$4,$5::jsonb,$6) ON CONFLICT(tenant_id,source_system,source_appointment_id) DO UPDATE SET payload=EXCLUDED.payload,status=EXCLUDED.status`, did, h.tenantID, e.SourceSystem, sid, mustJSON(e.Data), fmt.Sprint(e.Data["status"]))
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
	_, err := tx.Exec(ctx, `INSERT INTO external_data_queues(delivery_id,tenant_id,source_system,source_queue_ticket_id,customer_id,payload,status) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7) ON CONFLICT(tenant_id,source_system,source_queue_ticket_id) DO UPDATE SET customer_id=EXCLUDED.customer_id,payload=EXCLUDED.payload,status=EXCLUDED.status`, did, h.tenantID, e.SourceSystem, sid, customerID, mustJSON(e.Data), fmt.Sprint(e.Data["status"]))
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
	patientName := strings.TrimSpace(fmt.Sprint(e.Patient["name"]))
	visitType := strings.TrimSpace(fmt.Sprint(e.Data["visit_type"]))
	if visitType == "" {
		visitType = "初诊"
	}
	started := e.OccurredAt
	if started.IsZero() {
		started = e.UpdatedAt
	}
	err := tx.QueryRow(ctx, `INSERT INTO encounters(tenant_id,source_type,source_id,patient_id,patient_name,provider_name,department_id,channel,visit_type,started_at,status,created_at,updated_at) VALUES($1,'external_data',$2,$3,NULLIF($4,''),$5,NULL,'external',$6,$7,'new',NOW(),NOW()) ON CONFLICT(tenant_id,source_type,source_id) DO UPDATE SET patient_id=EXCLUDED.patient_id,patient_name=EXCLUDED.patient_name,provider_name=EXCLUDED.provider_name,visit_type=EXCLUDED.visit_type,started_at=EXCLUDED.started_at,updated_at=NOW() RETURNING id`, h.tenantID, sourceID, patientID, patientName, doctorName, visitType, started).Scan(&encounterID)
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
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		write(w, 500, map[string]string{"error": "begin transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())
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
	err = tx.QueryRow(r.Context(), `INSERT INTO encounters(tenant_id,source_type,source_id,patient_id,patient_name,channel,visit_type,status,created_at,updated_at) SELECT $1,'external_queue',$2,$3,COALESCE(c.name,''),'external','consultation','new',NOW(),NOW() FROM customers c WHERE c.id=$3 AND c.tenant_id=$1 ON CONFLICT(tenant_id,source_type,source_id) DO UPDATE SET patient_id=EXCLUDED.patient_id,updated_at=NOW() RETURNING id`, h.tenantID, sid, customer).Scan(&encounter)
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
	write(w, 200, map[string]any{"status": "claimed", "queue_id": id, "encounter_id": encounter})
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
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
