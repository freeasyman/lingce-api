package emrrealtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// This payload deliberately matches the internal HTTP contract without importing
// followup implementation: the two modules can be developed independently.
type realtimeFollowupRequest struct {
	TenantID          int64  `json:"tenant_id"`
	RecordingID       int64  `json:"recording_id"`
	EncounterID       int64  `json:"encounter_id"`
	EmployeeID        int64  `json:"employee_id"`
	DoctorName        string `json:"doctor_name"`
	CustomerID        int64  `json:"customer_id"`
	CustomerName      string `json:"customer_name"`
	RecordedAt        string `json:"recorded_at"`
	CleanedTranscript string `json:"cleaned_transcript"`
}

func (r realtimeFollowupRequest) validate() error {
	if r.TenantID <= 0 || r.RecordingID <= 0 || r.EncounterID <= 0 || r.EmployeeID <= 0 || r.CustomerID <= 0 {
		return fmt.Errorf("followup delivery requires valid tenant, recording, encounter, doctor and customer IDs")
	}
	if strings.TrimSpace(r.DoctorName) == "" || strings.TrimSpace(r.CustomerName) == "" || strings.TrimSpace(r.CleanedTranscript) == "" {
		return fmt.Errorf("followup delivery requires doctor name, customer name and cleaned transcript")
	}
	if _, err := time.Parse(time.RFC3339, r.RecordedAt); err != nil {
		return fmt.Errorf("followup recorded_at is invalid")
	}
	return nil
}

// Persist transcript and outbox atomically BEFORE medical record generation.
// Source and owner checks prevent badge/Worker recordings entering this producer.
func (s *Service) persistRealtimeFollowup(ctx context.Context, tenantID, employeeID, encounterID, recordingID int64, transcript string) error {
	if strings.TrimSpace(transcript) == "" {
		return nil
	}
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE recordings SET transcription_text=$5,
        transcription_status='completed', cleaned_transcription_status='completed',
        cleaned_transcription=jsonb_build_object('text',$5::text,'source','realtime'), updated_at=NOW()
        WHERE tenant_id=$1 AND id=$2 AND employee_id=$3 AND encounter_id=$4
          AND source='realtime' AND business_scope='doctor'`, tenantID, recordingID, employeeID, encounterID, strings.TrimSpace(transcript))
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("realtime followup recording ownership mismatch")
	}
	_, err = tx.Exec(ctx, `INSERT INTO emr_followup_deliveries(tenant_id,recording_id)
        VALUES($1,$2) ON CONFLICT(tenant_id,recording_id) DO NOTHING`, tenantID, recordingID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) startFollowupRetryLoop() {
	if s.store == nil || s.store.pool == nil || s.followupAPIURL == "" || s.internalToken == "" {
		slog.Warn("realtime followup delivery is not configured")
		return
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			s.processFollowupDeliveries(ctx)
			cancel()
		}
	}()
}

// Lock a small batch with SKIP LOCKED, keeping ownership through HTTP reception.
// After a timeout the receiver may already have accepted: its tenant+recording
// idempotency is REQUIRED for safe retries, including crashes before local commit.
func (s *Service) processFollowupDeliveries(ctx context.Context) {
	for i := 0; i < 4 && ctx.Err() == nil; i++ {
		tx, err := s.store.pool.Begin(ctx)
		if err != nil {
			slog.Error("begin realtime followup delivery", "error", err)
			return
		}
		var tenantID, recordingID int64
		err = tx.QueryRow(ctx, `SELECT tenant_id,recording_id FROM emr_followup_deliveries
            WHERE status='pending' AND next_attempt_at<=NOW()
            ORDER BY next_attempt_at LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&tenantID, &recordingID)
		if err != nil {
			_ = tx.Rollback(ctx)
			if err != pgx.ErrNoRows {
				slog.Error("claim realtime followup delivery", "error", err)
			}
			return
		}
	request, loadErr := loadRealtimeFollowup(ctx, tx, tenantID, recordingID)
		requestID := ""
		if loadErr == nil {
			requestID, loadErr = sendRealtimeFollowup(ctx, &http.Client{Timeout: 10 * time.Second}, s.followupAPIURL, s.internalToken, request)
		}
		if loadErr == nil {
			_, err = tx.Exec(ctx, `UPDATE emr_followup_deliveries SET status='accepted',request_id=$3,
                attempt_count=attempt_count+1,last_error=NULL,updated_at=NOW()
                WHERE tenant_id=$1 AND recording_id=$2`, tenantID, recordingID, requestID)
		} else {
			// Missing references/auth/422 also stay observable and retry after
			// correction. Retry only delivery; never wait for task generation.
			_, err = tx.Exec(ctx, `UPDATE emr_followup_deliveries SET attempt_count=attempt_count+1,
                last_error=$3,next_attempt_at=NOW()+INTERVAL '5 minutes',updated_at=NOW()
                WHERE tenant_id=$1 AND recording_id=$2`, tenantID, recordingID, loadErr.Error())
			slog.Warn("realtime followup delivery pending", "tenant_id", tenantID, "recording_id", recordingID, "error", loadErr)
		}
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if err != nil {
			slog.Error("persist realtime followup delivery result", "recording_id", recordingID, "error", err)
			return
		}
	}
}

func loadRealtimeFollowup(ctx context.Context, tx pgx.Tx, tenantID, recordingID int64) (realtimeFollowupRequest, error) {
	r := realtimeFollowupRequest{}
	var recordedAt time.Time
	// Database is the authority: no frontend names, role claims or latest-record guesses.
	err := tx.QueryRow(ctx, `SELECT r.tenant_id,r.id,r.encounter_id,r.employee_id,
        COALESCE(NULLIF(emp.full_name,''),NULLIF(emp.name,''),''),r.customer_id,c.name,
        r.recorded_at,COALESCE(r.cleaned_transcription->>'text','')
        FROM recordings r
        JOIN encounters e ON e.id=r.encounter_id AND e.tenant_id=r.tenant_id AND e.source_type='realtime' AND e.provider_id=r.employee_id
        JOIN employees emp ON emp.id=r.employee_id AND emp.tenant_id=r.tenant_id AND emp.deleted_at IS NULL
        JOIN customers c ON c.id=r.customer_id AND c.tenant_id=r.tenant_id AND c.deleted_at IS NULL
        WHERE r.tenant_id=$1 AND r.id=$2 AND r.source='realtime' AND r.business_scope='doctor'
          AND r.cleaned_transcription_status='completed'
          AND (SELECT lower(trim(er.role_code)) FROM institution_employee_roles er
               WHERE er.tenant_id=r.tenant_id AND er.employee_id=r.employee_id
               ORDER BY er.updated_at DESC NULLS LAST,er.created_at DESC,er.id DESC LIMIT 1)='doctor'`, tenantID, recordingID).
		Scan(&r.TenantID, &r.RecordingID, &r.EncounterID, &r.EmployeeID, &r.DoctorName, &r.CustomerID, &r.CustomerName, &recordedAt, &r.CleanedTranscript)
	if err != nil {
		return r, fmt.Errorf("load eligible realtime followup references: %w", err)
	}
	r.RecordedAt = recordedAt.UTC().Format(time.RFC3339)
	return r, r.validate()
}

func sendRealtimeFollowup(ctx context.Context, client *http.Client, baseURL, token string, r realtimeFollowupRequest) (string, error) {
	if err := r.validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("followup internal authentication is not configured")
	}
	body, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/followup/tasks/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", token)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("followup reception unconfirmed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("followup reception HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	var result struct {
		Code        string `json:"code"`
		Status      string `json:"status"`
		RequestID   string `json:"request_id"`
		TenantID    int64  `json:"tenant_id"`
		RecordingID int64  `json:"recording_id"`
		EncounterID int64  `json:"encounter_id"`
		EmployeeID  int64  `json:"employee_id"`
		CustomerID  int64  `json:"customer_id"`
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(data, &envelope)
	if len(envelope.Data) > 0 {
		data = envelope.Data
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("invalid followup reception response")
	}
	if result.Code != "ACCEPTED" || result.Status != "accepted" || strings.TrimSpace(result.RequestID) == "" || result.TenantID != r.TenantID || result.RecordingID != r.RecordingID || result.EncounterID != r.EncounterID || result.EmployeeID != r.EmployeeID || result.CustomerID != r.CustomerID {
		return "", fmt.Errorf("followup reception not confirmed for this recording")
	}
	return result.RequestID, nil
}
