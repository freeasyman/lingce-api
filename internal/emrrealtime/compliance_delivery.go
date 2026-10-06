package emrrealtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrrecord"
)

type realtimeComplianceRequest struct {
	TenantID          int64  `json:"tenant_id"`
	RecordingID       int64  `json:"recording_id"`
	EncounterID       int64  `json:"encounter_id"`
	EmployeeID        int64  `json:"employee_id"`
	CustomerID        int64  `json:"customer_id"`
	CustomerName      string `json:"customer_name"`
	EmployeeName      string `json:"employee_name"`
	DoctorName        string `json:"doctor_name"`
	BusinessScope     string `json:"business_scope"`
	RoleCode          string `json:"role_code"`
	RecordedAt        string `json:"recorded_at"`
	CleanedTranscript string `json:"cleaned_transcript"`
}

func (s *Service) submitRealtimeComplianceCheck(ctx context.Context, tenantID, employeeID, encounterID, recordingID int64, record *emrrecord.Record, recordedAt time.Time, transcript string) error {
	if strings.TrimSpace(s.complianceAPIURL) == "" || strings.TrimSpace(s.internalToken) == "" {
		return fmt.Errorf("compliance delivery is not configured")
	}
	doctorName, _, err := s.store.DoctorContext(ctx, tenantID, employeeID)
	if err != nil {
		return err
	}
	customerID := derefInt64(record.PatientID)
	if customerID <= 0 {
		switch value := record.PatientSnapshot["customer_id"].(type) {
		case float64:
			customerID = int64(value)
		case int64:
			customerID = value
		case int:
			customerID = int64(value)
		case string:
			_, _ = fmt.Sscan(strings.TrimSpace(value), &customerID)
		}
	}
	customerName := patientNameFromSnapshot(record.PatientSnapshot)
	reqBody := realtimeComplianceRequest{
		TenantID: tenantID, RecordingID: recordingID, EncounterID: encounterID, EmployeeID: employeeID,
		CustomerID: customerID, CustomerName: customerName, EmployeeName: doctorName, DoctorName: doctorName,
		BusinessScope: "doctor", RoleCode: "doctor", RecordedAt: recordedAt.UTC().Format(time.RFC3339), CleanedTranscript: strings.TrimSpace(transcript),
	}
	if reqBody.RecordingID <= 0 || reqBody.EncounterID <= 0 || reqBody.EmployeeID <= 0 || reqBody.CleanedTranscript == "" {
		return fmt.Errorf("invalid realtime compliance submission")
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.complianceAPIURL+"/api/v1/internal/compliance/communication-check", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", s.internalToken)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("compliance HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}
