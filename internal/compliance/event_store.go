package compliance

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) ListEvents(ctx context.Context, tenantID int64) ([]*ComplianceEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, encounter_id, rule_code, quote_hash, source, severity, type, status, timestamp,
		       employee_name, employee_role, department, patient_name, patient_meta,
		       content_title, content_type, quote, summary, advice, legal_basis,
		       evidence_at, transcript, related_actions, created_at, updated_at
		FROM compliance_events
		WHERE deleted_at IS NULL
		  AND tenant_id = $1
		ORDER BY timestamp DESC, created_at DESC, id DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list compliance events: %w", err)
	}
	defer rows.Close()

	var events []*ComplianceEvent
	for rows.Next() {
		event, err := scanComplianceEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan compliance events: %w", err)
	}
	return events, nil
}

func (s *Store) GetEvent(ctx context.Context, tenantID int64, id string) (*ComplianceEvent, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, encounter_id, rule_code, quote_hash, source, severity, type, status, timestamp,
		       employee_name, employee_role, department, patient_name, patient_meta,
		       content_title, content_type, quote, summary, advice, legal_basis,
		       evidence_at, transcript, related_actions, created_at, updated_at
		FROM compliance_events
		WHERE deleted_at IS NULL
		  AND id = $1
		  AND tenant_id = $2
	`, id, tenantID)
	event, err := scanComplianceEvent(row)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("event not found")
	}
	if err != nil {
		return nil, fmt.Errorf("get compliance event: %w", err)
	}
	return event, nil
}

func scanComplianceEvent(row rowScanner) (*ComplianceEvent, error) {
	var (
		event          ComplianceEvent
		timestamp      time.Time
		transcriptJSON []byte
		actionsJSON    []byte
		patientName    *string
		patientMeta    *string
		contentTitle   *string
		contentType    *string
		evidenceAt     *string
	)
	if err := row.Scan(
		&event.ID,
		&event.TenantID,
		&event.EncounterID,
		&event.RuleCode,
		&event.QuoteHash,
		&event.Source,
		&event.Severity,
		&event.Type,
		&event.Status,
		&timestamp,
		&event.EmployeeName,
		&event.EmployeeRole,
		&event.Department,
		&patientName,
		&patientMeta,
		&contentTitle,
		&contentType,
		&event.Quote,
		&event.Summary,
		&event.Advice,
		&event.LegalBasis,
		&evidenceAt,
		&transcriptJSON,
		&actionsJSON,
		&event.CreatedAt,
		&event.UpdatedAt,
	); err != nil {
		return nil, err
	}
	event.Timestamp = timestamp
	if patientName != nil {
		event.PatientName = *patientName
	}
	if patientMeta != nil {
		event.PatientMeta = *patientMeta
	}
	if contentTitle != nil {
		event.ContentTitle = *contentTitle
	}
	if contentType != nil {
		event.ContentType = *contentType
	}
	if evidenceAt != nil {
		event.EvidenceAt = *evidenceAt
	}
	if len(transcriptJSON) > 0 {
		event.Transcript = json.RawMessage(transcriptJSON)
	}
	event.RelatedActions = []string{}
	if len(actionsJSON) > 0 {
		if err := json.Unmarshal(actionsJSON, &event.RelatedActions); err != nil {
			return nil, fmt.Errorf("unmarshal compliance event related actions: %w", err)
		}
	}
	return &event, nil
}
