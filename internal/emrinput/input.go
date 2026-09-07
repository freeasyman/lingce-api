package emrinput

import (
	"fmt"
	"strings"
	"time"
)

type SourceKind string

const (
	SourceKindRecording  SourceKind = "recording"
	SourceKindRealtime   SourceKind = "realtime"
	SourceKindHistorical SourceKind = "historical"
)

type Patient struct {
	CustomerID *int64 `json:"customer_id,omitempty"`
	PatientID  *int64 `json:"patient_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Phone      *string `json:"phone,omitempty"`
	Gender     *string `json:"gender,omitempty"`
	Age        *int    `json:"age,omitempty"`
}

type Segment struct {
	Sequence      int               `json:"sequence"`
	Speaker       string            `json:"speaker,omitempty"`
	StartTime     *float64          `json:"start_time,omitempty"`
	EndTime       *float64          `json:"end_time,omitempty"`
	RawText       string            `json:"raw_text,omitempty"`
	CorrectedText string            `json:"corrected_text,omitempty"`
	Final         bool              `json:"final"`
	Evidence      map[string]any    `json:"evidence,omitempty"`
	Metadata      map[string]any    `json:"metadata,omitempty"`
}

type Input struct {
	SourceKind     SourceKind       `json:"source_kind"`
	SourceID       string          `json:"source_id"`
	TenantID       int64           `json:"tenant_id"`
	EncounterID    *int64          `json:"encounter_id,omitempty"`
	RecordID       *string         `json:"record_id,omitempty"`
	CustomerID     *int64          `json:"customer_id,omitempty"`
	Patient        *Patient        `json:"patient,omitempty"`
	BatchID        string          `json:"batch_id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	FinalText      string          `json:"final_text,omitempty"`
	CorrectedText  string          `json:"corrected_text,omitempty"`
	Segments       []Segment       `json:"segments,omitempty"`
	SourceEvidence map[string]any   `json:"source_evidence,omitempty"`
	CompletedAt    time.Time       `json:"completed_at"`
}

func NewRecordingInput(sourceID string, tenantID int64, encounterID *int64, recordID *string, patient *Patient, finalText, correctedText string, segments []Segment, batchID, idempotencyKey string, evidence map[string]any, completedAt time.Time) Input {
	return newInput(SourceKindRecording, sourceID, tenantID, encounterID, recordID, patient, finalText, correctedText, segments, batchID, idempotencyKey, evidence, completedAt)
}

func NewRealtimeInput(sourceID string, tenantID int64, encounterID *int64, recordID *string, patient *Patient, finalText, correctedText string, segments []Segment, batchID, idempotencyKey string, evidence map[string]any, completedAt time.Time) Input {
	return newInput(SourceKindRealtime, sourceID, tenantID, encounterID, recordID, patient, finalText, correctedText, segments, batchID, idempotencyKey, evidence, completedAt)
}

func NewHistoricalInput(sourceID string, tenantID int64, encounterID *int64, recordID *string, patient *Patient, finalText, correctedText string, segments []Segment, batchID, idempotencyKey string, evidence map[string]any, completedAt time.Time) Input {
	return newInput(SourceKindHistorical, sourceID, tenantID, encounterID, recordID, patient, finalText, correctedText, segments, batchID, idempotencyKey, evidence, completedAt)
}

func newInput(kind SourceKind, sourceID string, tenantID int64, encounterID *int64, recordID *string, patient *Patient, finalText, correctedText string, segments []Segment, batchID, idempotencyKey string, evidence map[string]any, completedAt time.Time) Input {
	return Input{
		SourceKind:     kind,
		SourceID:       strings.TrimSpace(sourceID),
		TenantID:       tenantID,
		EncounterID:    positiveID(encounterID),
		RecordID:       stringPtr(recordID),
		CustomerID:     patientCustomerID(patient),
		Patient:        normalizePatient(patient),
		BatchID:        strings.TrimSpace(batchID),
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
		FinalText:      strings.TrimSpace(finalText),
		CorrectedText:  strings.TrimSpace(correctedText),
		Segments:       normalizeSegments(segments),
		SourceEvidence: normalizeEvidence(evidence),
		CompletedAt:    completedAt.UTC(),
	}
}

func (i Input) Validate() error {
	if i.TenantID <= 0 {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(string(i.SourceKind)) == "" {
		return fmt.Errorf("source_kind is required")
	}
	if strings.TrimSpace(i.SourceID) == "" {
		return fmt.Errorf("source_id is required")
	}
	if i.EncounterID != nil && *i.EncounterID <= 0 {
		return fmt.Errorf("encounter_id is invalid")
	}
	if i.RecordID != nil && strings.TrimSpace(*i.RecordID) == "" {
		return fmt.Errorf("record_id is invalid")
	}
	if strings.TrimSpace(i.FinalText) == "" && strings.TrimSpace(i.CorrectedText) == "" && len(i.Segments) == 0 {
		return fmt.Errorf("at least one transcript payload is required")
	}
	if strings.TrimSpace(i.IdempotencyKey) == "" {
		return fmt.Errorf("idempotency_key is required")
	}
	return nil
}

func (i Input) CanonicalKey() string {
	parts := []string{
		string(i.SourceKind),
		strings.TrimSpace(i.SourceID),
		itoa(i.TenantID),
	}
	if i.EncounterID != nil {
		parts = append(parts, itoa(*i.EncounterID))
	}
	if i.RecordID != nil {
		parts = append(parts, strings.TrimSpace(*i.RecordID))
	}
	if strings.TrimSpace(i.BatchID) != "" {
		parts = append(parts, strings.TrimSpace(i.BatchID))
	}
	if strings.TrimSpace(i.IdempotencyKey) != "" {
		parts = append(parts, strings.TrimSpace(i.IdempotencyKey))
	}
	return strings.Join(parts, ":")
}

func (i Input) SourceReferences() map[string]any {
	ref := map[string]any{
		"source_kind":     string(i.SourceKind),
		"source_id":       strings.TrimSpace(i.SourceID),
		"idempotency_key": strings.TrimSpace(i.IdempotencyKey),
	}
	if i.EncounterID != nil {
		ref["encounter_id"] = *i.EncounterID
	}
	if i.RecordID != nil {
		ref["record_id"] = strings.TrimSpace(*i.RecordID)
	}
	if strings.TrimSpace(i.BatchID) != "" {
		ref["batch_id"] = strings.TrimSpace(i.BatchID)
	}
	if i.Patient != nil {
		ref["customer_id"] = i.Patient.CustomerID
		ref["patient_id"] = i.Patient.PatientID
		ref["patient_name"] = i.Patient.Name
	}
	if strings.TrimSpace(i.FinalText) != "" {
		ref["final_text"] = i.FinalText
	}
	if strings.TrimSpace(i.CorrectedText) != "" {
		ref["corrected_text"] = i.CorrectedText
	}
	if len(i.Segments) > 0 {
		ref["segments"] = i.Segments
	}
	if len(i.SourceEvidence) > 0 {
		ref["source_evidence"] = i.SourceEvidence
	}
	return ref
}

func (i Input) StandardInput() map[string]any {
	input := map[string]any{
		"source_kind":     string(i.SourceKind),
		"source_id":       strings.TrimSpace(i.SourceID),
		"tenant_id":       i.TenantID,
		"idempotency_key": strings.TrimSpace(i.IdempotencyKey),
		"completed_at":    i.CompletedAt,
	}
	if i.EncounterID != nil {
		input["encounter_id"] = *i.EncounterID
	}
	if i.RecordID != nil {
		input["record_id"] = strings.TrimSpace(*i.RecordID)
	}
	if i.CustomerID != nil {
		input["customer_id"] = *i.CustomerID
	}
	if i.Patient != nil {
		patient := map[string]any{}
		if i.Patient.CustomerID != nil {
			patient["customer_id"] = *i.Patient.CustomerID
		}
		if i.Patient.PatientID != nil {
			patient["patient_id"] = *i.Patient.PatientID
		}
		if strings.TrimSpace(i.Patient.Name) != "" {
			patient["name"] = strings.TrimSpace(i.Patient.Name)
		}
		if i.Patient.Phone != nil {
			patient["phone"] = *i.Patient.Phone
		}
		if i.Patient.Gender != nil {
			patient["gender"] = *i.Patient.Gender
		}
		if i.Patient.Age != nil {
			patient["age"] = *i.Patient.Age
		}
		input["patient"] = patient
	}
	if strings.TrimSpace(i.BatchID) != "" {
		input["batch_id"] = strings.TrimSpace(i.BatchID)
	}
	if strings.TrimSpace(i.FinalText) != "" {
		input["final_text"] = strings.TrimSpace(i.FinalText)
	}
	if strings.TrimSpace(i.CorrectedText) != "" {
		input["corrected_text"] = strings.TrimSpace(i.CorrectedText)
	}
	if len(i.Segments) > 0 {
		input["segments"] = i.Segments
	}
	if len(i.SourceEvidence) > 0 {
		input["source_evidence"] = i.SourceEvidence
	}
	return input
}

func (i Input) EncounterContext() map[string]any {
	context := map[string]any{
		"source_kind": string(i.SourceKind),
		"source_id":   strings.TrimSpace(i.SourceID),
	}
	if strings.TrimSpace(i.FinalText) != "" {
		context["final_text"] = i.FinalText
	}
	if strings.TrimSpace(i.CorrectedText) != "" {
		context["corrected_text"] = i.CorrectedText
	}
	if len(i.Segments) > 0 {
		context["segments"] = i.Segments
	}
	return context
}

func normalizePatient(patient *Patient) *Patient {
	if patient == nil {
		return nil
	}
	return &Patient{
		CustomerID: positiveID(patient.CustomerID),
		PatientID:  positiveID(patient.PatientID),
		Name:       strings.TrimSpace(patient.Name),
		Phone:      stringPtr(patient.Phone),
		Gender:     stringPtr(patient.Gender),
		Age:        intPtr(patient.Age),
	}
}

func normalizeSegments(segments []Segment) []Segment {
	if len(segments) == 0 {
		return nil
	}
	result := make([]Segment, 0, len(segments))
	for _, segment := range segments {
		result = append(result, Segment{
			Sequence:      segment.Sequence,
			Speaker:       strings.TrimSpace(segment.Speaker),
			StartTime:     floatPtr(segment.StartTime),
			EndTime:       floatPtr(segment.EndTime),
			RawText:       strings.TrimSpace(segment.RawText),
			CorrectedText: strings.TrimSpace(segment.CorrectedText),
			Final:         segment.Final,
			Evidence:      normalizeEvidence(segment.Evidence),
			Metadata:      normalizeEvidence(segment.Metadata),
		})
	}
	return result
}

func normalizeEvidence(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func patientCustomerID(patient *Patient) *int64 {
	if patient == nil {
		return nil
	}
	return positiveID(patient.CustomerID)
}

func positiveID(value *int64) *int64 {
	if value == nil || *value <= 0 {
		return nil
	}
	result := *value
	return &result
}

func stringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func intPtr(value *int) *int {
	if value == nil || *value < 0 {
		return nil
	}
	result := *value
	return &result
}

func floatPtr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func itoa(value int64) string {
	return fmt.Sprintf("%d", value)
}
