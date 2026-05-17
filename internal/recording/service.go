package recording

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/employee"
	"github.com/jackc/pgx/v5"
)

type Service struct {
	store             *Store
	employeeStore     *employee.Store
	workerURL         string
	workerToken       string
	lingceWorkerURL   string
	lingceWorkerToken string
	httpClient        *http.Client
}

type workerUnavailableError struct {
	cause error
}

func (e *workerUnavailableError) Error() string {
	if e == nil || e.cause == nil {
		return "recording worker unavailable"
	}
	return fmt.Sprintf("recording worker unavailable: %v", e.cause)
}

func (e *workerUnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

type recordingValidationError struct {
	code    string
	message string
}

func (e *recordingValidationError) Error() string {
	if e == nil {
		return "recording validation failed"
	}
	return e.message
}

func (e *recordingValidationError) Code() string {
	if e == nil || strings.TrimSpace(e.code) == "" {
		return "BAD_REQUEST"
	}
	return e.code
}

func IsRecordingValidationError(err error) bool {
	var validationErr *recordingValidationError
	return errors.As(err, &validationErr)
}

// IsWorkerUnavailable indicates whether an error is caused by recording-worker unavailability.
func IsWorkerUnavailable(err error) bool {
	var unavailable *workerUnavailableError
	return errors.As(err, &unavailable)
}

func NewService(store *Store, employeeStore *employee.Store, workerURL, workerToken, lingceWorkerURL, lingceWorkerToken string) *Service {
	return &Service{
		store:             store,
		employeeStore:     employeeStore,
		workerURL:         strings.TrimRight(workerURL, "/"),
		workerToken:       workerToken,
		lingceWorkerURL:   strings.TrimRight(lingceWorkerURL, "/"),
		lingceWorkerToken: lingceWorkerToken,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// ListRecordings retrieves a paginated list of medical recordings
func (s *Service) ListRecordings(ctx context.Context, req RecordingListRequest) ([]*RecordingResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	recordings, total, err := s.store.ListRecordings(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*RecordingResponse, len(recordings))
	for i, r := range recordings {
		resp := toRecordingResponse(r)
		responses[i] = resp
	}
	if err := s.backfillListChiefComplaintFromEMR(ctx, responses); err != nil {
		slog.Warn("failed to backfill chief complaint for recording list", "error", err)
	}
	for i := range responses {
		compactRecordingListItem(responses[i])
	}

	return responses, total, nil
}

func (s *Service) backfillListChiefComplaintFromEMR(ctx context.Context, responses []*RecordingResponse) error {
	if len(responses) == 0 {
		return nil
	}
	needIDs := make([]int64, 0, len(responses))
	for _, resp := range responses {
		if resp == nil || resp.ID <= 0 {
			continue
		}
		if resp.ChiefComplaint != nil && strings.TrimSpace(*resp.ChiefComplaint) != "" {
			continue
		}
		needIDs = append(needIDs, resp.ID)
	}
	if len(needIDs) == 0 {
		return nil
	}

	rows, err := s.store.pool.Query(ctx, `
		SELECT recording_id, COALESCE(emr_content::jsonb, '{}'::jsonb)
		FROM recording_emr_drafts
		WHERE recording_id = ANY($1::bigint[])
	`, needIDs)
	if err != nil {
		return err
	}
	defer rows.Close()

	byID := make(map[int64]map[string]interface{}, len(needIDs))
	for rows.Next() {
		var recordingID int64
		var emrContent JSONObject
		if scanErr := rows.Scan(&recordingID, &emrContent); scanErr != nil {
			return scanErr
		}
		byID[recordingID] = map[string]interface{}(emrContent)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, resp := range responses {
		if resp == nil || resp.ID <= 0 {
			continue
		}
		if resp.ChiefComplaint != nil && strings.TrimSpace(*resp.ChiefComplaint) != "" {
			continue
		}
		content, ok := byID[resp.ID]
		if !ok || content == nil {
			continue
		}
		chiefComplaint := pickString(content, "chief_complaint")
		if chiefComplaint == "" {
			chiefComplaint = pickString(content, "chiefComplaint")
		}
		if chiefComplaint != "" {
			resp.ChiefComplaint = literalStringPtr(chiefComplaint)
		}
	}

	return nil
}

func compactRecordingListItem(resp *RecordingResponse) {
	if resp == nil {
		return
	}
	// Preserve summary from analysis_result before clearing it.
	if resp.AnalysisResult != nil {
		if s, ok := resp.AnalysisResult["summary"].(string); ok && s != "" && resp.ConversationSummary == nil {
			resp.ConversationSummary = &s
		}
	}
	resp.TranscriptText = nil
	resp.DoctorSummary = nil
	resp.TherapistSummary = nil
	resp.ConsultantSummary = nil
	resp.AnalysisResult = nil
	resp.AnalysisSummary = nil
	resp.AnalysisDisplay = nil
	resp.StructuredTranscript = nil
	resp.TimelineTranscript = nil
	resp.ContentSeeds = nil
	resp.RouteReview = nil
	resp.EMRDraft = nil
	resp.ConsultationRecord = nil
	resp.DealOutcome = nil
	resp.SuggestedTask = nil
}

// GetRecording retrieves a medical recording by ID
func (s *Service) GetRecording(ctx context.Context, id int64) (*RecordingResponse, error) {
	recording, err := s.store.GetRecordingByID(ctx, id)
	if err != nil {
		return nil, err
	}

	resp := toRecordingResponse(recording)
	if err := s.enrichRecordingResponse(ctx, id, resp); err != nil {
		return nil, err
	}
	if err := s.rebuildRecordingAnalysisPayload(ctx, id, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *Service) GetTherapistReset(ctx context.Context, id int64) (*TherapistResetResponse, error) {
	resp, err := s.GetRecording(ctx, id)
	if err != nil {
		return nil, err
	}
	analysis := resp.AnalysisResult
	if analysis == nil {
		analysis = map[string]interface{}{}
	}
	raw := pickMap(analysis, "raw")
	tra := pickMap(raw, "therapist_reset_analysis")
	resetNode := pickMap(tra, "reset")
	items := toMapSlice(firstNonEmptyArray(
		pickArray(resetNode, "items"),
		pickArray(analysis, "reset_items"),
		pickArray(pickMap(analysis, "reset"), "items"),
	))
	return &TherapistResetResponse{
		RecordingID:           id,
		DimensionScores:       toScoreMap(firstNonEmptyMap(pickMap(analysis, "dimension_scores"), pickMap(tra, "dimension_scores"))),
		ResetPercent:          valueOrZeroFloat(pickFloat(analysis, "reset_percent")),
		CriticalGap:           valueOrFalseBool(pickBool(analysis, "critical_gap")),
		CriticalMissingItems:  pickStringSlice(firstNonEmptyArray(pickArray(analysis, "critical_missing_items"), pickArray(tra, "critical_missing_items"))),
		Highlights:            pickStringSlice(firstNonEmptyArray(pickArray(analysis, "highlights"), pickArray(tra, "highlights"))),
		ImprovementPriorities: pickStringSlice(firstNonEmptyArray(pickArray(analysis, "improvement_priorities"), pickArray(tra, "improvement_priorities"))),
		RecommendedActions:    pickStringSlice(firstNonEmptyArray(pickArray(analysis, "recommended_actions"), pickArray(tra, "recommended_actions"))),
		Items:                 items,
	}, nil
}

func valueOrZeroFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func valueOrFalseBool(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}

// CreateRecording creates a new medical recording
func (s *Service) CreateRecording(ctx context.Context, req CreateRecordingRequest) (*RecordingResponse, error) {
	// Validate request
	if req.TenantID == 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if req.EmployeeID == 0 {
		return nil, fmt.Errorf("employee_id is required")
	}
	if req.PatientName == "" {
		return nil, fmt.Errorf("patient_name is required")
	}
	if req.RecordingURL == "" {
		return nil, fmt.Errorf("recording_url is required")
	}

	recording, err := s.store.CreateRecording(ctx, req)
	if err != nil {
		return nil, err
	}

	return toRecordingResponse(recording), nil
}

// UpdateRecording updates a medical recording
func (s *Service) UpdateRecording(ctx context.Context, id int64, req UpdateRecordingRequest) (*RecordingResponse, error) {
	before, err := s.store.GetRecordingByID(ctx, id)
	if err != nil {
		return nil, err
	}

	recording, err := s.store.UpdateRecording(ctx, id, req)
	if err != nil {
		return nil, err
	}

	if req.CustomerID != nil && recording.CustomerID != nil && customerChanged(before.CustomerID, recording.CustomerID) {
		_ = s.backfillEMRDraftCustomerID(ctx, id, *recording.CustomerID)
		_ = s.rebindOpenTasksCustomer(ctx, id, *recording.CustomerID)
		_ = s.dispatchMedicalFollowUpTasksIfPossible(ctx, id, "manual_link")
		if refreshed, refreshErr := s.store.GetRecordingByID(ctx, id); refreshErr == nil {
			recording = refreshed
		}
	}

	return toRecordingResponse(recording), nil
}

func customerChanged(before *int64, after *int64) bool {
	if before == nil && after == nil {
		return false
	}
	if before == nil || after == nil {
		return true
	}
	return *before != *after
}

func (s *Service) backfillEMRDraftCustomerID(ctx context.Context, recordingID, customerID int64) error {
	_, err := s.store.pool.Exec(ctx, `
		UPDATE recording_emr_drafts
		SET customer_id = $1
		WHERE recording_id = $2 AND customer_id IS NULL
	`, customerID, recordingID)
	return err
}

func (s *Service) rebindOpenTasksCustomer(ctx context.Context, recordingID, customerID int64) error {
	var customerName string
	var customerPhone string
	if err := s.store.pool.QueryRow(ctx, `
		SELECT COALESCE(name, ''), COALESCE(phone, '')
		FROM customers
		WHERE id = $1
	`, customerID).Scan(&customerName, &customerPhone); err != nil {
		return err
	}

	_, err := s.store.pool.Exec(ctx, `
		UPDATE recording_tasks
		SET customer_id = $1,
			customer_name = $2,
			customer_phone = $3,
			updated_at = NOW()
		WHERE recording_id = $4
		  AND status IN ('pending', 'assigned')
		  AND COALESCE(source_type, '') IN ('follow_up', 'ai')
	`, customerID, customerName, customerPhone, recordingID)
	return err
}

func (s *Service) dispatchMedicalFollowUpTasksIfPossible(ctx context.Context, recordingID int64, triggerSource string) error {
	rec, err := s.store.GetRecordingByID(ctx, recordingID)
	if err != nil {
		return err
	}

	if !isMedicalRecording(rec.AnalysisResult) {
		return s.persistFollowupDispatch(ctx, rec, map[string]interface{}{
			"eligible":       false,
			"reasons":        []string{"NON_MEDICAL_RECORDING"},
			"created_count":  0,
			"trigger_source": triggerSource,
			"dispatched_at":  time.Now().UTC().Format(time.RFC3339),
		})
	}

	followUpTasks := extractFollowUpTasks(rec.AnalysisResult)
	if len(followUpTasks) == 0 {
		return s.persistFollowupDispatch(ctx, rec, map[string]interface{}{
			"eligible":       false,
			"reasons":        []string{"NO_FOLLOWUP_TASKS"},
			"created_count":  0,
			"trigger_source": triggerSource,
			"dispatched_at":  time.Now().UTC().Format(time.RFC3339),
		})
	}

	if rec.CustomerID == nil || *rec.CustomerID <= 0 {
		return s.persistFollowupDispatch(ctx, rec, map[string]interface{}{
			"eligible":       false,
			"reasons":        []string{"MISSING_CUSTOMER"},
			"created_count":  0,
			"trigger_source": triggerSource,
			"dispatched_at":  time.Now().UTC().Format(time.RFC3339),
		})
	}

	var customerName string
	var customerPhone string
	customerQueryErr := s.store.pool.QueryRow(ctx, `
		SELECT COALESCE(name, ''), COALESCE(phone, '')
		FROM customers
		WHERE id = $1
	`, *rec.CustomerID).Scan(&customerName, &customerPhone)
	if customerQueryErr != nil {
		return s.persistFollowupDispatch(ctx, rec, map[string]interface{}{
			"eligible":       false,
			"reasons":        []string{"MISSING_CUSTOMER"},
			"created_count":  0,
			"trigger_source": triggerSource,
			"dispatched_at":  time.Now().UTC().Format(time.RFC3339),
		})
	}

	var existingCount int
	if err := s.store.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM recording_tasks
		WHERE tenant_id = $1 AND recording_id = $2
	`, rec.TenantID, rec.ID).Scan(&existingCount); err != nil {
		return err
	}

	createdCount := 0
	assigneeID := s.resolveRecordingTaskAssignee(ctx, rec.TenantID, rec.EmployeeID)
	for _, task := range followUpTasks {
		title := firstNonEmptyText(task["title"], task["name"], task["action"])
		if title == "" {
			continue
		}

		var duplicateCount int
		if err := s.store.pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM recording_tasks
			WHERE tenant_id = $1
			  AND recording_id = $2
			  AND source_type = 'follow_up'
			  AND title = $3
		`, rec.TenantID, rec.ID, title).Scan(&duplicateCount); err != nil {
			return err
		}
		if duplicateCount > 0 {
			continue
		}

		description := firstNonEmptyText(task["description"], task["reason"], task["note"])
		script := firstNonEmptyText(task["script"], task["call_script"])
		channel := firstNonEmptyText(task["channel"])
		priority := normalizeTaskPriority(firstNonEmptyText(task["priority"]))
		contactReason := description
		if contactReason == "" {
			contactReason = title
		}
		timingHours := toPositiveInt(task["timing_hours"])
		if timingHours <= 0 {
			timingHours = 24
		}
		dueAt := time.Now().Add(time.Duration(timingHours) * time.Hour)

		_, err := s.store.pool.Exec(ctx, `
			INSERT INTO recording_tasks (
				tenant_id,
				recording_id,
				customer_id,
				customer_name,
				customer_phone,
				title,
				description,
				priority,
				script,
				contact_reason,
				assigned_to,
				status,
				source_type,
				source_detail,
				due_at,
				created_at,
				updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, NULLIF($9, ''), NULLIF($10, ''), $11, 'pending', 'follow_up', NULLIF($12, ''), $13, NOW(), NOW())
		`, rec.TenantID, rec.ID, *rec.CustomerID, customerName, customerPhone, title, description, priority, script, contactReason, assigneeID, channel, dueAt)
		if err != nil {
			return err
		}
		createdCount++
	}

	var totalCount int
	if err := s.store.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM recording_tasks
		WHERE tenant_id = $1 AND recording_id = $2
	`, rec.TenantID, rec.ID).Scan(&totalCount); err != nil {
		return err
	}

	alreadyDispatched := createdCount == 0 && existingCount > 0 && totalCount >= existingCount
	eligible := createdCount > 0 || alreadyDispatched
	reasons := []string{}
	if !eligible {
		reasons = []string{"NO_TASKS_CREATED"}
	}

	return s.persistFollowupDispatch(ctx, rec, map[string]interface{}{
		"eligible":       eligible,
		"reasons":        reasons,
		"created_count":  createdCount,
		"existing_count": totalCount,
		"trigger_source": triggerSource,
		"dispatched_at":  time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Service) resolveRecordingTaskAssignee(ctx context.Context, tenantID, ownerEmployeeID int64) *int64 {
	if tenantID <= 0 || ownerEmployeeID <= 0 {
		return nil
	}

	var assignee int64
	err := s.store.pool.QueryRow(ctx, `
		SELECT ep.partner_employee_id
		FROM employee_partnerships ep
		INNER JOIN employees e
		  ON e.id = ep.partner_employee_id
		 AND e.tenant_id = ep.tenant_id
		 AND COALESCE(e.is_active, true) = true
		WHERE ep.tenant_id = $1
		  AND ep.primary_employee_id = $2
		  AND COALESCE(ep.is_active, true) = true
		ORDER BY COALESCE(ep.is_primary, false) DESC, ep.id DESC
		LIMIT 1
	`, tenantID, ownerEmployeeID).Scan(&assignee)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return nil
	}
	if assignee <= 0 {
		return nil
	}
	return &assignee
}

func isMedicalRecording(analysisResult map[string]interface{}) bool {
	if analysisResult == nil {
		return false
	}
	if b := pickBool(analysisResult, "is_medical_consultation"); b != nil && *b {
		return true
	}
	if b := pickBool(pickMap(analysisResult, "doctor_routing"), "is_medical_consultation"); b != nil && *b {
		return true
	}
	role := strings.ToLower(firstNonEmptyText(analysisResult["role"]))
	return role == "doctor" || role == "medical"
}

func extractFollowUpTasks(analysisResult map[string]interface{}) []map[string]interface{} {
	if analysisResult == nil {
		return nil
	}
	candidates := []interface{}{
		analysisResult["follow_up_tasks"],
		pickMap(analysisResult, "analysis_summary")["follow_up_tasks"],
		pickMap(pickMap(analysisResult, "doctor_segue_narrative"), "final")["follow_up_tasks"],
	}
	for _, candidate := range candidates {
		items := toMapSliceFromUnknown(candidate)
		if len(items) > 0 {
			return items
		}
	}
	return nil
}

func toMapSliceFromUnknown(raw interface{}) []map[string]interface{} {
	switch v := raw.(type) {
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]interface{}:
		return v
	default:
		return nil
	}
}

func normalizeTaskPriority(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "high", "urgent":
		return "high"
	case "low":
		return "low"
	default:
		return "medium"
	}
}

func toPositiveInt(raw interface{}) int {
	switch v := raw.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case float32:
		if v > 0 {
			return int(v)
		}
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0
		}
		if parsed, err := strconv.Atoi(trimmed); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func (s *Service) persistFollowupDispatch(ctx context.Context, rec *MedicalRecording, dispatch map[string]interface{}) error {
	analysis := map[string]interface{}{}
	if rec.AnalysisResult != nil {
		analysis = map[string]interface{}(rec.AnalysisResult)
	}
	analysis["followup_dispatch"] = dispatch
	payload, err := json.Marshal(analysis)
	if err != nil {
		return err
	}
	_, err = s.store.pool.Exec(ctx, `
		UPDATE recordings
		SET analysis_result = $1::jsonb, updated_at = NOW()
		WHERE id = $2
	`, payload, rec.ID)
	return err
}

// DeleteRecording deletes a medical recording
func (s *Service) DeleteRecording(ctx context.Context, id int64) error {
	return s.store.DeleteRecording(ctx, id)
}

// toRecordingResponse converts a MedicalRecording to RecordingResponse
func toRecordingResponse(r *MedicalRecording) *RecordingResponse {
	analysisStatus := strings.TrimSpace(string(r.Status))
	if r.AnalysisStatus != nil && strings.TrimSpace(*r.AnalysisStatus) != "" {
		analysisStatus = strings.TrimSpace(*r.AnalysisStatus)
	}
	resp := &RecordingResponse{
		ID:                r.ID,
		TenantID:          r.TenantID,
		TenantName:        r.TenantName,
		EmployeeID:        r.EmployeeID,
		EmployeeName:      r.EmployeeName,
		DepartmentName:    r.DepartmentName,
		DeviceNo:          r.DeviceNo,
		CustomerID:        r.CustomerID,
		CustomerName:      r.CustomerName,
		PatientName:       r.PatientName,
		PatientAge:        r.PatientAge,
		PatientGender:     r.PatientGender,
		PatientPhone:      r.PatientPhone,
		RecordingURL:      r.RecordingURL,
		RecordingDuration: r.RecordingDuration,
		Duration:          r.RecordingDuration,
		TranscriptText:    r.TranscriptText,
		DoctorSummary:     r.DoctorSummary,
		TherapistSummary:  r.TherapistSummary,
		ConsultantSummary: r.ConsultantSummary,
		BusinessScope:     strings.TrimSpace(r.BusinessScope),
		AnalysisStatus:    pickStringPtr(analysisStatus),
		ContentSeedsTypes: []string{},
		Status:            string(r.Status),
		ProcessingError:   r.ProcessingError,
		CreatedAt:         formatDBLocalTime(r.CreatedAt),
		UpdatedAt:         formatDBLocalTime(r.UpdatedAt),
	}
	if len(r.AnalysisResult) > 0 {
		resp.AnalysisResult = map[string]interface{}(r.AnalysisResult)
	}
	if len(r.AnalysisDisplay) > 0 {
		resp.AnalysisDisplay = map[string]interface{}(r.AnalysisDisplay)
	}

	if len(r.AnalysisDisplay) > 0 {
		analysisDisplay := map[string]interface{}(r.AnalysisDisplay)
		if nested := pickMap(analysisDisplay, "analysis_result"); nested != nil {
			if resp.AnalysisResult == nil {
				resp.AnalysisResult = nested
			} else {
				// Keep fields already persisted in recordings.analysis_result
				// (e.g. therapist RESET aggregate), and only fill missing keys from display payload.
				mergeMap(resp.AnalysisResult, nested)
			}
		}
		if resp.AnalysisResult == nil && looksLikeAnalysisResult(analysisDisplay) {
			resp.AnalysisResult = analysisDisplay
		}
		resp.AnalysisSummary = pickMap(resp.AnalysisResult, "analysis_summary")
		if resp.AnalysisSummary == nil {
			resp.AnalysisSummary = pickMap(analysisDisplay, "analysis_summary")
		}
		resp.RouteReview = pickMap(analysisDisplay, "route_review")
		resp.EMRDraft = pickMap(analysisDisplay, "emr_draft")
		resp.SceneType = pickStringPtr(
			pickString(resp.AnalysisResult, "scene_type"),
			pickString(resp.AnalysisResult, "scene"),
			pickString(analysisDisplay, "scene_type"),
		)
		resp.VisitOutcome = pickStringPtr(
			pickString(resp.AnalysisResult, "visit_outcome"),
			pickString(resp.AnalysisResult, "decision_status"),
			pickString(analysisDisplay, "visit_outcome"),
		)
		resp.SubjectiveSummary = pickStringPtr(
			pickString(resp.AnalysisResult, "subjective_summary"),
			pickString(resp.AnalysisResult, "conversation_summary"),
			pickString(resp.AnalysisResult, "summary"),
			pickString(analysisDisplay, "subjective_summary"),
		)
		resp.ConversationSummary = pickStringPtr(
			pickString(resp.AnalysisResult, "conversation_summary"),
			pickString(analysisDisplay, "conversation_summary"),
			pickString(resp.AnalysisResult, "subjective_summary"),
			pickString(analysisDisplay, "subjective_summary"),
			pickString(resp.AnalysisResult, "summary"),
		)
		resp.KeyQuotes = pickStringSlice(
			pickArray(resp.AnalysisResult, "key_quotes"),
			pickArray(resp.AnalysisResult, "highlights"),
			pickArray(analysisDisplay, "key_quotes"),
			pickArray(analysisDisplay, "highlights"),
			pickArray(resp.AnalysisSummary, "highlights"),
		)
		resp.QualityScore = pickFloatPtr(
			pickFloat(resp.AnalysisResult, "quality_score"),
			pickFloat(resp.AnalysisResult, "quality"),
			pickFloat(analysisDisplay, "quality_score"),
		)
		resp.SegueScore = pickFloatPtr(
			pickFloat(resp.AnalysisResult, "segue_percent"),
			pickFloat(resp.AnalysisResult, "segue_score"),
			pickFloat(resp.AnalysisResult, "communication_score"),
			pickFloat(pickMap(resp.AnalysisResult, "segue"), "overall_score"),
			pickFloat(analysisDisplay, "segue_score"),
		)
		resp.CriticalGap = pickBoolPtr(
			pickBool(resp.AnalysisResult, "critical_gap"),
			pickBool(analysisDisplay, "critical_gap"),
		)
		resp.AnalysisSummary = normalizeInsightSummary(resp.AnalysisSummary, resp.AnalysisResult)
	}

	if r.RecordingStartedAt != nil {
		formatted := formatDBLocalTime(*r.RecordingStartedAt)
		resp.RecordingStartedAt = &formatted
		resp.RecordedAt = &formatted
	}

	if r.RecordingEndedAt != nil {
		formatted := formatDBLocalTime(*r.RecordingEndedAt)
		resp.RecordingEndedAt = &formatted
	}

	if r.ProcessedAt != nil {
		formatted := formatDBLocalTime(*r.ProcessedAt)
		resp.ProcessedAt = &formatted
	}

	populateCompatibilityFields(resp)
	return resp
}

func formatDBLocalTime(t time.Time) string {
	// DB uses timestamp without timezone; keep wall-clock semantics in API output.
	localWallClock := time.Date(
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond(),
		time.Local,
	)
	return localWallClock.Format("2006-01-02T15:04:05Z07:00")
}

func (s *Service) enrichRecordingResponse(ctx context.Context, recordingID int64, resp *RecordingResponse) error {
	if resp == nil {
		return nil
	}

	// Expose raw source payloads as-is for all roles so every page reads the same
	// authoritative transcript inputs and avoids per-page reconstruction drift.
	if cleaned, err := s.loadCleanedTranscriptionSegments(ctx, recordingID); err == nil && len(cleaned) > 0 {
		resp.CleanedTranscription = cleaned
	}
	if segments, err := s.loadRawTranscriptionSegments(ctx, recordingID); err == nil && len(segments) > 0 {
		resp.TranscriptionSegments = segments
	}

	// Frontdesk recordings: the worker pipeline saves a complete aggregate
	// to recordings.analysis_result with role="frontdesk". Use it directly
	// instead of overriding with individual prompt-step results.
	if resp.AnalysisResult != nil {
		if role, _ := resp.AnalysisResult["role"].(string); role == "frontdesk" {
			if resp.AnalysisSummary == nil {
				resp.AnalysisSummary = pickMap(resp.AnalysisResult, "analysis_summary")
			}
			if resp.ConversationSummary == nil {
				resp.ConversationSummary = pickStringPtr(pickString(resp.AnalysisResult, "summary"))
			}
			// Load cleaned transcription segments for structured display
			if cleaned, err := s.loadCleanedTranscriptionSegments(ctx, recordingID); err == nil && len(cleaned) > 0 {
				resp.StructuredTranscript = cleaned
			}
			return nil
		}
	}

	type analysisRow struct {
		PromptCode string
		ResultData map[string]interface{}
	}
	rows, err := s.store.pool.Query(ctx, `
		SELECT prompt_code, COALESCE(result_data, '{}'::json)
		FROM recording_analysis_results
		WHERE recording_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 12
	`, recordingID)
	if err != nil {
		return fmt.Errorf("failed to query analysis result: %w", err)
	}
	defer rows.Close()

	analysisRows := make([]analysisRow, 0, 12)
	for rows.Next() {
		var row analysisRow
		if scanErr := rows.Scan(&row.PromptCode, &row.ResultData); scanErr != nil {
			return fmt.Errorf("failed to scan analysis result: %w", scanErr)
		}
		analysisRows = append(analysisRows, row)
	}

	var (
		latestAny          map[string]interface{}
		latestConsultation map[string]interface{}
		latestStructured   map[string]interface{}
		latestNarrative    map[string]interface{}
		latestContentGen   map[string]interface{}
		latestOpsPlan      map[string]interface{}
	)
	for _, row := range analysisRows {
		code := strings.ToLower(strings.TrimSpace(row.PromptCode))
		data := row.ResultData
		if data == nil {
			continue
		}
		if latestAny == nil {
			latestAny = data
		}
		switch {
		case strings.Contains(code, "consultation_analysis"):
			if latestConsultation == nil {
				latestConsultation = data
			}
		case strings.Contains(code, "structured"):
			if latestStructured == nil {
				latestStructured = data
			}
		case strings.Contains(code, "narrative"):
			if latestNarrative == nil {
				latestNarrative = data
			}
		case strings.Contains(code, "content_gen"):
			if latestContentGen == nil {
				latestContentGen = data
			}
		case strings.Contains(code, "customer_operations_plan"):
			if latestOpsPlan == nil {
				latestOpsPlan = data
			}
		}
	}

	var analysisResult map[string]interface{}
	var analysisSummary map[string]interface{}
	switch {
	case latestConsultation != nil:
		analysisResult = latestConsultation
	case latestStructured != nil:
		analysisResult = latestStructured
	case latestNarrative != nil:
		analysisResult = latestNarrative
	default:
		analysisResult = latestAny
	}
	if summary := pickMap(analysisResult, "analysis_summary"); summary != nil {
		analysisSummary = summary
	}
	if summary := pickMap(latestConsultation, "analysis_summary"); summary != nil {
		analysisSummary = summary
	}
	if final := pickMap(latestNarrative, "final"); final != nil {
		if analysisSummary == nil {
			analysisSummary = map[string]interface{}{}
		}
		mergeMap(analysisSummary, final)
	}
	if summary := pickMap(latestOpsPlan, "analysis_summary"); summary != nil {
		if analysisSummary == nil {
			analysisSummary = map[string]interface{}{}
		}
		mergeMap(analysisSummary, summary)
	}

	if latestContentGen != nil && resp.EMRDraft == nil {
		if emr := pickMap(latestContentGen, "emr"); emr != nil {
			resp.EMRDraft = map[string]interface{}{
				"emr_content": emr,
			}
			if conf := pickString(emr, "confidence"); conf != "" {
				resp.EMRDraft["confidence"] = conf
			}
			if missing, ok := emr["missing_fields"]; ok {
				resp.EMRDraft["missing_fields"] = missing
			}
		}
	}
	if latestContentGen != nil && len(resp.ContentSeeds) == 0 {
		resp.ContentSeeds = toMapSlice(pickArray(latestContentGen, "content_seeds"))
	}
	if len(resp.ContentSeeds) == 0 {
		resp.ContentSeeds = toMapSlice(firstNonEmptyArray(
			pickArray(latestConsultation, "content_seeds"),
			pickArray(analysisResult, "content_seeds"),
			pickArray(analysisSummary, "content_seeds"),
		))
	}

	if analysisResult != nil {
		if resp.AnalysisResult == nil {
			resp.AnalysisResult = analysisResult
		} else {
			// Do not overwrite already persisted aggregate fields with step-level rows.
			mergeMap(resp.AnalysisResult, analysisResult)
		}
	}
	if analysisSummary != nil {
		resp.AnalysisSummary = analysisSummary
	}
	resp.AnalysisSummary = normalizeInsightSummary(resp.AnalysisSummary, resp.AnalysisResult)

	if resp.SceneType == nil {
		resp.SceneType = pickStringPtr(
			pickString(resp.AnalysisResult, "scene_type"),
			pickString(resp.AnalysisResult, "scene"),
			pickString(resp.AnalysisSummary, "scene_type"),
		)
	}
	if resp.VisitOutcome == nil {
		resp.VisitOutcome = pickStringPtr(
			pickString(resp.AnalysisResult, "visit_outcome"),
			pickString(resp.AnalysisResult, "decision_status"),
			pickNestedStatus(resp.AnalysisSummary, "visit_outcome"),
			pickString(resp.AnalysisSummary, "visit_outcome"),
			pickString(resp.AnalysisSummary, "status"),
		)
	}
	if resp.SubjectiveSummary == nil {
		resp.SubjectiveSummary = pickStringPtr(
			pickString(resp.AnalysisResult, "subjective_summary"),
			pickString(resp.AnalysisResult, "conversation_summary"),
			pickString(resp.AnalysisSummary, "summary"),
			pickString(resp.AnalysisSummary, "critical_summary"),
		)
	}
	if resp.ConversationSummary == nil {
		resp.ConversationSummary = pickStringPtr(
			pickString(resp.AnalysisResult, "conversation_summary"),
			pickString(resp.AnalysisDisplay, "conversation_summary"),
			pickString(resp.AnalysisResult, "subjective_summary"),
			pickString(resp.AnalysisSummary, "summary"),
			pickString(resp.AnalysisSummary, "critical_summary"),
		)
	}
	if resp.StatusSummary == nil {
		resp.StatusSummary = pickStringPtr(
			pickString(resp.AnalysisResult, "status_summary"),
			pickString(resp.AnalysisSummary, "current_state"),
			pickString(resp.AnalysisSummary, "critical_summary"),
		)
	}
	if len(resp.KeyQuotes) == 0 {
		resp.KeyQuotes = pickStringSlice(
			pickArray(resp.AnalysisResult, "key_quotes"),
			pickArray(resp.AnalysisResult, "highlights"),
			pickArray(resp.AnalysisDisplay, "key_quotes"),
			pickArray(resp.AnalysisDisplay, "highlights"),
			pickArray(resp.AnalysisSummary, "highlights"),
		)
	}
	if resp.DealOutcome == nil {
		resp.DealOutcome = pickMap(resp.AnalysisResult, "deal_outcome")
	}
	if resp.SuggestedTask == nil {
		resp.SuggestedTask = pickMap(resp.AnalysisResult, "suggested_task")
	}
	if resp.ConsultationRecord == nil {
		resp.ConsultationRecord = pickMap(resp.AnalysisResult, "consultation_record")
	}
	if resp.Report == nil {
		resp.Report = pickStringPtr(
			pickString(resp.AnalysisResult, "report"),
			pickString(resp.AnalysisResult, "feedback_report"),
			pickString(resp.AnalysisSummary, "feedback_report"),
			pickString(resp.AnalysisSummary, "critical_summary"),
		)
	}
	if resp.QualityScore == nil {
		resp.QualityScore = pickFloatPtr(
			pickFloat(resp.AnalysisResult, "quality_score"),
			pickFloat(resp.AnalysisResult, "quality"),
		)
	}
	if resp.SegueScore == nil {
		resp.SegueScore = pickFloatPtr(
			pickFloat(resp.AnalysisResult, "segue_score"),
			pickFloat(resp.AnalysisResult, "overall_score"),
			pickFloat(pickMap(resp.AnalysisResult, "segue"), "overall_score"),
		)
	}
	if resp.CriticalGap == nil {
		resp.CriticalGap = pickBoolPtr(
			pickBool(resp.AnalysisSummary, "critical_gap"),
			pickBool(resp.AnalysisResult, "critical_gap"),
		)
	}
	if resp.RouteReview == nil {
		resp.RouteReview = pickMap(resp.AnalysisResult, "route_review")
		if resp.RouteReview == nil {
			resp.RouteReview = pickMap(resp.AnalysisSummary, "route_review")
		}
	}
	if len(resp.ContentSeeds) == 0 {
		contentSeedsRaw := pickArray(resp.AnalysisResult, "content_seeds")
		if len(contentSeedsRaw) == 0 {
			contentSeedsRaw = pickArray(resp.AnalysisSummary, "content_seeds")
		}
		resp.ContentSeeds = toMapSlice(contentSeedsRaw)
	}
	if len(resp.StructuredTranscript) == 0 {
		resp.StructuredTranscript = toMapSlice(
			firstNonEmptyArray(
				pickArray(resp.AnalysisResult, "annotated_transcription"),
				pickArray(resp.AnalysisResult, "structured_transcript"),
				pickArray(resp.AnalysisResult, "structured_transcription"),
				pickArray(resp.AnalysisResult, "transcript_structured"),
				pickArray(latestConsultation, "annotated_transcription"),
				pickArray(resp.AnalysisSummary, "structured_transcript"),
				pickArray(resp.AnalysisSummary, "structured_transcription"),
			),
		)
	}
	if len(resp.TimelineTranscript) == 0 {
		resp.TimelineTranscript = toMapSlice(
			firstNonEmptyArray(
				pickArray(resp.AnalysisResult, "timeline_transcript"),
				pickArray(resp.AnalysisResult, "timeline_transcription"),
				pickArray(resp.AnalysisResult, "transcript_timeline"),
				pickArray(resp.AnalysisSummary, "timeline_transcript"),
				pickArray(resp.AnalysisSummary, "timeline_transcription"),
			),
		)
	}
	if len(resp.StructuredTranscript) == 0 || len(resp.TimelineTranscript) == 0 {
		if cleaned := resp.CleanedTranscription; len(cleaned) > 0 {
			structured, timeline := buildTranscriptFromSegments(cleaned)
			if len(resp.StructuredTranscript) == 0 {
				resp.StructuredTranscript = structured
			}
			if len(resp.TimelineTranscript) == 0 {
				resp.TimelineTranscript = timeline
			}
		}
	}
	if len(resp.StructuredTranscript) == 0 || len(resp.TimelineTranscript) == 0 {
		if segments := resp.TranscriptionSegments; len(segments) > 0 {
			structured, timeline := buildTranscriptFromSegments(segments)
			if len(resp.StructuredTranscript) == 0 {
				resp.StructuredTranscript = structured
			}
			if len(resp.TimelineTranscript) == 0 {
				resp.TimelineTranscript = timeline
			}
		}
	}
	if len(resp.StructuredTranscript) == 0 && len(resp.TimelineTranscript) == 0 && resp.TranscriptText != nil {
		resp.StructuredTranscript, resp.TimelineTranscript = buildTranscriptFallback(*resp.TranscriptText)
	}

	if resp.EMRDraft == nil {
		var (
			patientID            *int64
			patientNameExtracted *string
			patientMatchSource   *string
			emrContent           map[string]interface{}
			confidence           *string
			missingFields        interface{}
			isConfirmed          *bool
			confirmedAt          interface{}
		)
		err = s.store.pool.QueryRow(ctx, `
			SELECT
				patient_id,
				NULLIF(patient_name_extracted, ''),
				NULLIF(patient_match_source, ''),
				COALESCE(emr_content::jsonb, '{}'::jsonb),
				NULLIF(confidence, ''),
				COALESCE(missing_fields::jsonb, '[]'::jsonb),
				is_confirmed,
				confirmed_at
			FROM recording_emr_drafts
			WHERE recording_id = $1
			ORDER BY generated_at DESC NULLS LAST, id DESC
			LIMIT 1
		`, recordingID).Scan(
			&patientID,
			&patientNameExtracted,
			&patientMatchSource,
			&emrContent,
			&confidence,
			&missingFields,
			&isConfirmed,
			&confirmedAt,
		)
		if err == nil {
			draft := map[string]interface{}{
				"emr_content": emrContent,
			}
			if patientID != nil {
				draft["patient_id"] = *patientID
			}
			if patientNameExtracted != nil {
				draft["patient_name_extracted"] = *patientNameExtracted
			}
			if patientMatchSource != nil {
				draft["patient_match_source"] = *patientMatchSource
			}
			if confidence != nil {
				draft["confidence"] = *confidence
			}
			if missingFields != nil {
				draft["missing_fields"] = missingFields
			}
			if isConfirmed != nil {
				draft["is_confirmed"] = *isConfirmed
			}
			if confirmedAt != nil {
				draft["confirmed_at"] = confirmedAt
			}
			resp.EMRDraft = draft
		}
	}
	if resp.AnalysisDisplay == nil {
		resp.AnalysisDisplay = map[string]interface{}{}
	}
	if resp.AnalysisResult != nil {
		resp.AnalysisDisplay["analysis_result"] = resp.AnalysisResult
	}
	if resp.AnalysisSummary != nil {
		resp.AnalysisDisplay["analysis_summary"] = resp.AnalysisSummary
	}
	if resp.RouteReview != nil {
		resp.AnalysisDisplay["route_review"] = resp.RouteReview
	}
	if resp.EMRDraft != nil {
		resp.AnalysisDisplay["emr_draft"] = resp.EMRDraft
	}
	if len(resp.ContentSeeds) > 0 {
		resp.AnalysisDisplay["content_seeds"] = resp.ContentSeeds
		if resp.AnalysisResult != nil {
			if _, exists := resp.AnalysisResult["content_seeds"]; !exists {
				resp.AnalysisResult["content_seeds"] = resp.ContentSeeds
			}
		}
	}
	if len(resp.StructuredTranscript) > 0 {
		resp.AnalysisDisplay["structured_transcript"] = resp.StructuredTranscript
		resp.AnalysisDisplay["structured_transcription"] = resp.StructuredTranscript
		if resp.AnalysisResult != nil {
			if _, exists := resp.AnalysisResult["structured_transcript"]; !exists {
				resp.AnalysisResult["structured_transcript"] = resp.StructuredTranscript
			}
			if _, exists := resp.AnalysisResult["structured_transcription"]; !exists {
				resp.AnalysisResult["structured_transcription"] = resp.StructuredTranscript
			}
		}
	}
	if len(resp.TimelineTranscript) > 0 {
		resp.AnalysisDisplay["timeline_transcript"] = resp.TimelineTranscript
		resp.AnalysisDisplay["timeline_transcription"] = resp.TimelineTranscript
		if resp.AnalysisResult != nil {
			if _, exists := resp.AnalysisResult["timeline_transcript"]; !exists {
				resp.AnalysisResult["timeline_transcript"] = resp.TimelineTranscript
			}
			if _, exists := resp.AnalysisResult["timeline_transcription"]; !exists {
				resp.AnalysisResult["timeline_transcription"] = resp.TimelineTranscript
			}
		}
	}

	populateCompatibilityFields(resp)
	return nil
}

func mergeMap(dst map[string]interface{}, src map[string]interface{}) {
	if dst == nil || src == nil {
		return
	}
	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

func cloneMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (s *Service) rebuildRecordingAnalysisPayload(ctx context.Context, recordingID int64, resp *RecordingResponse) error {
	if resp == nil {
		return nil
	}
	rows, err := s.store.pool.Query(ctx, `
		SELECT prompt_code, COALESCE(result_data, '{}'::json), COALESCE(is_active, false), created_at, id
		FROM recording_analysis_results
		WHERE recording_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 32
	`, recordingID)
	if err != nil {
		return fmt.Errorf("failed to query analysis rows: %w", err)
	}
	defer rows.Close()

	type analysisRow struct {
		promptCode string
		resultData map[string]interface{}
		isActive   bool
		createdAt  time.Time
		id         int64
	}
	collected := make([]analysisRow, 0, 32)
	for rows.Next() {
		var row analysisRow
		if scanErr := rows.Scan(&row.promptCode, &row.resultData, &row.isActive, &row.createdAt, &row.id); scanErr != nil {
			return fmt.Errorf("failed to scan analysis row: %w", scanErr)
		}
		collected = append(collected, row)
	}
	if rows.Err() != nil {
		return fmt.Errorf("failed to iterate analysis rows: %w", rows.Err())
	}

	analysisResults := make([]map[string]interface{}, 0, len(collected))
	rebuilt := cloneMap(resp.AnalysisResult)
	for _, row := range collected {
		item := map[string]interface{}{
			"prompt_code":   row.promptCode,
			"is_active":     row.isActive,
			"created_at":    row.createdAt.Format(time.RFC3339),
			"id":            row.id,
			"analysis_json": row.resultData,
		}
		analysisResults = append(analysisResults, item)
		if rebuilt == nil {
			rebuilt = cloneMap(row.resultData)
			continue
		}
		mergeMap(rebuilt, row.resultData)
	}
	if rebuilt != nil {
		hydrateTherapistResetFields(rebuilt, analysisResults)
	}

	if rebuilt != nil {
		resp.AnalysisResult = rebuilt
		if resp.AnalysisDisplay == nil {
			resp.AnalysisDisplay = map[string]interface{}{}
		}
		resp.AnalysisDisplay["analysis_result"] = rebuilt
	}
	if len(analysisResults) > 0 {
		resp.AnalysisResults = analysisResults
		if resp.AnalysisDisplay == nil {
			resp.AnalysisDisplay = map[string]interface{}{}
		}
		resp.AnalysisDisplay["analysis_results"] = analysisResults
	}
	return nil
}

func hydrateTherapistResetFields(rebuilt map[string]interface{}, analysisResults []map[string]interface{}) {
	if rebuilt == nil {
		return
	}
	ensureRaw := func() map[string]interface{} {
		raw := pickMap(rebuilt, "raw")
		if raw == nil {
			raw = map[string]interface{}{}
			rebuilt["raw"] = raw
		}
		return raw
	}
	ensureTherapistReset := func(raw map[string]interface{}) map[string]interface{} {
		tra := pickMap(raw, "therapist_reset_analysis")
		if tra == nil {
			tra = map[string]interface{}{}
			raw["therapist_reset_analysis"] = tra
		}
		return tra
	}
	setIfMissing := func(key string, value interface{}) {
		if value == nil {
			return
		}
		if _, ok := rebuilt[key]; !ok {
			rebuilt[key] = value
		}
	}

	for _, row := range analysisResults {
		code := strings.ToLower(strings.TrimSpace(pickString(row, "prompt_code")))
		if !strings.Contains(code, "therapist_reset") && !strings.Contains(code, "reset_analysis") {
			continue
		}
		payload := pickMap(row, "analysis_json")
		if payload == nil {
			continue
		}
		normalized := payload
		if nested := pickMap(payload, "analysis_result"); nested != nil {
			normalized = nested
		}
		if nested := pickMap(payload, "data"); nested != nil && len(normalized) == 0 {
			normalized = nested
		}

		raw := ensureRaw()
		tra := ensureTherapistReset(raw)
		mergeMap(tra, normalized)

		setIfMissing("dimension_scores", pickMap(tra, "dimension_scores"))
		if rp, ok := tra["reset_percent"]; ok {
			setIfMissing("reset_percent", rp)
		}
		if cg, ok := tra["critical_gap"]; ok {
			setIfMissing("critical_gap", cg)
		}
		if cmi, ok := tra["critical_missing_items"]; ok {
			setIfMissing("critical_missing_items", cmi)
		}
		if hl, ok := tra["highlights"]; ok {
			setIfMissing("highlights", hl)
		}
		if ip, ok := tra["improvement_priorities"]; ok {
			setIfMissing("improvement_priorities", ip)
		}
		if ra, ok := tra["recommended_actions"]; ok {
			setIfMissing("recommended_actions", ra)
		}

		resetNode := pickMap(tra, "reset")
		if resetNode != nil {
			if items, ok := resetNode["items"]; ok {
				setIfMissing("reset_items", items)
			}
			if _, ok := rebuilt["reset"]; !ok {
				rebuilt["reset"] = resetNode
			}
		}
	}
}

func normalizeInsightSummary(summary map[string]interface{}, result map[string]interface{}) map[string]interface{} {
	if result == nil {
		return summary
	}
	out := summary
	if out == nil {
		out = map[string]interface{}{}
	}

	if _, exists := out["patient_mindset"]; !exists {
		if patientMindset := pickMap(result, "patient_mindset"); patientMindset != nil {
			out["patient_mindset"] = patientMindset
		}
	}

	business := pickMap(result, "business")
	if business != nil {
		if _, exists := out["care_progress"]; !exists {
			if careProgress := pickMap(business, "care_progress"); careProgress != nil {
				out["care_progress"] = careProgress
			}
		}
		if _, exists := out["visit_outcome"]; !exists {
			if visitOutcome := pickMap(business, "visit_outcome"); visitOutcome != nil {
				out["visit_outcome"] = visitOutcome
			}
		}
		for _, key := range []string{"core_blockers", "conversion_opportunity", "current_state"} {
			if _, exists := out[key]; exists {
				continue
			}
			if value, ok := business[key]; ok && value != nil {
				out[key] = value
			}
		}
	}

	return out
}

func looksLikeAnalysisResult(source map[string]interface{}) bool {
	if source == nil {
		return false
	}
	for _, key := range []string{
		"scene_type", "scene", "segue_percent", "segue_score", "critical_gap",
		"business", "patient_mindset", "analysis_summary", "visit_outcome",
		"chief_complaint", "diagnosis", "consultation_record", "decision_status",
	} {
		if _, ok := source[key]; ok {
			return true
		}
	}
	return false
}

func populateCompatibilityFields(resp *RecordingResponse) {
	if resp == nil {
		return
	}
	if resp.Duration == nil {
		resp.Duration = resp.RecordingDuration
	}
	if resp.AnalysisStatus == nil || strings.TrimSpace(*resp.AnalysisStatus) == "" {
		resp.AnalysisStatus = pickStringPtr(resp.Status)
	}
	if resp.RecordedAt == nil {
		resp.RecordedAt = resp.RecordingStartedAt
	}

	analysisDisplay := resp.AnalysisDisplay
	analysisResult := resp.AnalysisResult
	analysisSummary := resp.AnalysisSummary
	if analysisSummary == nil {
		analysisSummary = pickMap(analysisResult, "analysis_summary")
	}
	analysisResultBusiness := pickMap(analysisResult, "business")
	analysisSummaryBusiness := pickMap(analysisSummary, "business")
	analysisDisplayBusiness := pickMap(analysisDisplay, "business")
	analysisResultDoctorContentGen := pickMap(analysisResult, "doctor_content_gen")
	analysisResultRawDoctorContentGen := pickMap(pickMap(analysisResult, "raw"), "doctor_content_gen")
	emrDraft := firstNonNilMap(resp.EMRDraft, pickMap(analysisDisplay, "emr_draft"), pickMap(analysisResult, "emr_draft"))
	emrContent := firstNonNilMap(
		pickMap(analysisResult, "emr"),
		pickMap(analysisResultDoctorContentGen, "emr"),
		pickMap(analysisResultRawDoctorContentGen, "emr"),
		pickMap(emrDraft, "emr_content"),
		pickMap(pickMap(analysisResult, "emr_draft"), "emr_content"),
		pickMap(pickMap(analysisDisplay, "emr_draft"), "emr_content"),
	)

	if resp.ConversationSummary == nil {
		resp.ConversationSummary = pickStringPtr(
			pickString(analysisResult, "conversation_summary"),
			pickString(analysisDisplay, "conversation_summary"),
			pickString(analysisResult, "subjective_summary"),
			pickString(analysisSummary, "summary"),
			pickString(analysisSummary, "critical_summary"),
			pickString(analysisResult, "status_summary"),
			pickString(analysisSummary, "current_state"),
			pickString(analysisDisplay, "status_summary"),
			pickString(analysisDisplay, "chief_complaint"),
		)
	}
	if resp.StatusSummary == nil {
		resp.StatusSummary = pickStringPtr(
			pickString(analysisResult, "status_summary"),
			pickString(analysisSummary, "current_state"),
			pickString(analysisSummary, "critical_summary"),
			pickString(analysisDisplay, "status_summary"),
		)
	}
	if len(resp.KeyQuotes) == 0 {
		resp.KeyQuotes = pickStringSlice(
			pickArray(analysisResult, "key_quotes"),
			pickArray(analysisResult, "highlights"),
			pickArray(analysisDisplay, "key_quotes"),
			pickArray(analysisDisplay, "highlights"),
			pickArray(analysisSummary, "highlights"),
		)
	}

	if resp.SeguePercent == nil {
		resp.SeguePercent = pickFloatPtr(
			pickFloat(analysisResult, "segue_percent"),
			pickFloat(analysisResult, "segue_score"),
			pickFloat(analysisResult, "communication_score"),
			pickFloat(pickMap(analysisResult, "segue"), "overall_score"),
			pickFloat(pickMap(analysisResult, "segue"), "segue_percent"),
			pickFloat(analysisDisplay, "segue_percent"),
			pickFloat(analysisDisplay, "segue_score"),
			pickFloat(pickMap(analysisDisplay, "segue_scores"), "overall"),
		)
	}
	if resp.SegueScore == nil {
		resp.SegueScore = resp.SeguePercent
	}
	if resp.CriticalGap == nil {
		resp.CriticalGap = pickBoolPtr(
			pickBool(analysisResult, "critical_gap"),
			pickBool(analysisSummary, "critical_gap"),
			pickBool(analysisDisplay, "critical_gap"),
		)
	}

	visitOutcomeStatus := pickStringPtr(
		pickNestedStatus(analysisResult, "visit_outcome"),
		pickNestedStatus(analysisResultBusiness, "visit_outcome"),
		pickNestedStatus(analysisSummary, "visit_outcome"),
		pickNestedStatus(analysisSummaryBusiness, "visit_outcome"),
		pickNestedStatus(analysisDisplay, "visit_outcome"),
		pickNestedStatus(analysisDisplayBusiness, "visit_outcome"),
		pickString(analysisResult, "visit_outcome_status"),
		pickString(analysisResult, "visit_outcome"),
		pickString(analysisResult, "decision_status"),
		pickString(analysisSummary, "visit_outcome_status"),
		pickString(analysisSummary, "visit_outcome"),
		pickString(analysisDisplay, "visit_outcome_status"),
		pickString(analysisDisplay, "visit_outcome"),
	)
	resp.VisitOutcomeStatus = visitOutcomeStatus
	if resp.VisitOutcome == nil {
		resp.VisitOutcome = visitOutcomeStatus
	}

	resp.DecisionStatus = pickStringPtr(
		pickString(analysisResult, "decision_status"),
		pickString(analysisSummary, "decision_status"),
		pickString(analysisDisplay, "decision_status"),
	)
	resp.SoapSubjectiveSummary = pickStringPtr(
		pickString(analysisResult, "subjective_summary"),
		pickString(analysisResult, "soap_subjective_summary"),
		pickString(analysisResult, "summary"),
		pickString(analysisSummary, "subjective_summary"),
		pickString(analysisDisplay, "subjective_summary"),
	)
	resp.ChiefComplaint = pickStringPtr(
		pickString(analysisResult, "chief_complaint"),
		pickString(emrContent, "chief_complaint"),
		pickString(pickMap(analysisResult, "consultation_record"), "chief_complaint"),
		pickString(pickMap(analysisResult, "emr_draft"), "chief_complaint"),
		pickString(pickMap(pickMap(analysisResult, "emr_draft"), "emr_content"), "chief_complaint"),
		pickString(pickMap(pickMap(analysisDisplay, "emr_draft"), "emr_content"), "chief_complaint"),
		pickString(pickMap(resp.EMRDraft, "emr_content"), "chief_complaint"),
	)
	resp.Diagnosis = pickStringPtr(
		pickString(analysisResult, "diagnosis"),
		pickString(emrContent, "diagnosis"),
		pickString(pickMap(analysisResult, "consultation_record"), "diagnosis"),
	)
	resp.CurrentState = pickStringPtr(
		pickString(analysisSummary, "current_state"),
		pickString(analysisResult, "current_state"),
		pickString(pickMap(analysisResult, "business"), "current_state"),
	)

	if resp.RouteReview == nil {
		resp.RouteReview = pickMap(analysisResult, "route_review")
		if resp.RouteReview == nil {
			resp.RouteReview = pickMap(analysisDisplay, "route_review")
		}
	}
	resp.RouteReviewRequired = pickBoolPtr(
		pickBool(resp.RouteReview, "required"),
		pickBool(resp.RouteReview, "route_review_required"),
		pickBool(analysisResult, "route_review_required"),
		pickBool(analysisSummary, "route_review_required"),
	)
	resp.RouteReviewReason = pickStringPtr(
		pickString(resp.RouteReview, "reason"),
		pickString(resp.RouteReview, "route_review_reason"),
		pickString(analysisResult, "route_review_reason"),
		pickString(analysisSummary, "route_review_reason"),
	)
	resp.RouteAutoDecision = pickStringPtr(
		pickString(resp.RouteReview, "auto_decision"),
		pickString(resp.RouteReview, "route_auto_decision"),
		pickString(analysisResult, "route_auto_decision"),
		pickString(analysisSummary, "route_auto_decision"),
	)
	resp.RouteReviewStatus = pickStringPtr(
		pickString(resp.RouteReview, "status"),
		pickString(resp.RouteReview, "route_review_status"),
		pickString(analysisResult, "route_review_status"),
		pickString(analysisSummary, "route_review_status"),
	)
	if resp.RouteReviewRequired == nil {
		resp.RouteReviewRequired = boolPtr(false)
	}
	if resp.RouteReviewReason == nil {
		resp.RouteReviewReason = literalStringPtr("")
	}
	if resp.RouteAutoDecision == nil {
		resp.RouteAutoDecision = literalStringPtr("")
	}
	if resp.RouteReviewStatus == nil {
		resp.RouteReviewStatus = literalStringPtr("pending")
	}

	if len(resp.ContentSeeds) == 0 {
		resp.ContentSeeds = toMapSlice(firstNonEmptyArray(
			pickArray(analysisResult, "content_seeds"),
			pickArray(analysisResultDoctorContentGen, "content_seeds"),
			pickArray(analysisResultRawDoctorContentGen, "content_seeds"),
			pickArray(analysisSummary, "content_seeds"),
			pickArray(analysisDisplay, "content_seeds"),
		))
	}

	if len(resp.ContentSeeds) > 0 {
		types := make([]string, 0, len(resp.ContentSeeds))
		seen := map[string]struct{}{}
		for _, seed := range resp.ContentSeeds {
			if seedType := strings.TrimSpace(pickString(seed, "seed_type")); seedType != "" {
				if _, ok := seen[seedType]; !ok {
					seen[seedType] = struct{}{}
					types = append(types, seedType)
				}
			}
		}
		resp.ContentSeedsCount = len(resp.ContentSeeds)
		resp.ContentSeedsTypes = types
	} else if resp.ContentSeedsTypes == nil {
		resp.ContentSeedsTypes = []string{}
	}

	if resp.EMRStatus == nil {
		if draft := firstNonNilMap(resp.EMRDraft, pickMap(analysisDisplay, "emr_draft"), pickMap(analysisResult, "emr_draft")); draft != nil {
			if confirmed := pickBool(draft, "is_confirmed"); confirmed != nil {
				if *confirmed {
					resp.EMRStatus = pickStringPtr("confirmed")
				} else {
					resp.EMRStatus = pickStringPtr("draft")
				}
			}
		}
	}
	if resp.EMRStatus == nil {
		resp.EMRStatus = pickStringPtr(
			pickString(analysisDisplay, "emr_status"),
			pickString(analysisResult, "emr_status"),
		)
	}
}

func firstNonNilMap(candidates ...map[string]interface{}) map[string]interface{} {
	for _, candidate := range candidates {
		if candidate != nil {
			return candidate
		}
	}
	return nil
}

func pickMap(source map[string]interface{}, key string) map[string]interface{} {
	if source == nil {
		return nil
	}
	raw, ok := source[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case map[string]interface{}:
		return v
	case JSONObject:
		return map[string]interface{}(v)
	default:
		return nil
	}
}

func pickNestedStatus(source map[string]interface{}, key string) string {
	m := pickMap(source, key)
	if m == nil {
		return ""
	}
	return pickString(m, "status")
}

func pickArray(source map[string]interface{}, key string) []interface{} {
	if source == nil {
		return nil
	}
	raw, ok := source[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []interface{}:
		return v
	case JSONArray:
		return []interface{}(v)
	default:
		return nil
	}
}

func pickStringSlice(candidates ...[]interface{}) []string {
	for _, candidate := range candidates {
		if len(candidate) == 0 {
			continue
		}
		result := make([]string, 0, len(candidate))
		for _, raw := range candidate {
			text := strings.TrimSpace(fmt.Sprintf("%v", raw))
			if text == "" || text == "<nil>" {
				continue
			}
			result = append(result, text)
		}
		if len(result) > 0 {
			return result
		}
	}
	return nil
}

func toMapSlice(items []interface{}) []map[string]interface{} {
	if len(items) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(items))
	for _, raw := range items {
		switch v := raw.(type) {
		case map[string]interface{}:
			result = append(result, v)
		case JSONObject:
			result = append(result, map[string]interface{}(v))
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func firstNonEmptyArray(candidates ...[]interface{}) []interface{} {
	for _, candidate := range candidates {
		if len(candidate) > 0 {
			return candidate
		}
	}
	return nil
}

func firstNonEmptyMap(candidates ...map[string]interface{}) map[string]interface{} {
	for _, candidate := range candidates {
		if len(candidate) > 0 {
			return candidate
		}
	}
	return nil
}

func toScoreMap(source map[string]interface{}) map[string]float64 {
	if len(source) == 0 {
		return map[string]float64{}
	}
	out := map[string]float64{}
	for k, v := range source {
		switch n := v.(type) {
		case float64:
			out[k] = n
		case float32:
			out[k] = float64(n)
		case int:
			out[k] = float64(n)
		case int64:
			out[k] = float64(n)
		case json.Number:
			if f, err := n.Float64(); err == nil {
				out[k] = f
			}
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				out[k] = f
			}
		}
	}
	return out
}

func buildTranscriptFallback(text string) ([]map[string]interface{}, []map[string]interface{}) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}
	lines := strings.Split(trimmed, "\n")
	structured := make([]map[string]interface{}, 0, len(lines))
	timeline := make([]map[string]interface{}, 0, len(lines))
	cursor := 0
	for _, line := range lines {
		content := strings.TrimSpace(line)
		if content == "" {
			continue
		}
		speaker := "对话"
		if idx := strings.Index(content, ":"); idx > 0 && idx < 12 {
			candidate := strings.TrimSpace(content[:idx])
			if candidate != "" {
				speaker = candidate
				content = strings.TrimSpace(content[idx+1:])
			}
		}
		if content == "" {
			continue
		}
		duration := estimateDurationSeconds(content)
		start := cursor
		end := start + duration
		structured = append(structured, map[string]interface{}{
			"speaker": speaker,
			"content": content,
		})
		timeline = append(timeline, map[string]interface{}{
			"speaker":       speaker,
			"text":          content,
			"start_seconds": start,
			"end_seconds":   end,
		})
		cursor = end
	}
	if len(structured) == 0 {
		structured = []map[string]interface{}{{"speaker": "对话", "content": trimmed}}
		timeline = []map[string]interface{}{{"speaker": "对话", "text": trimmed, "start_seconds": 0, "end_seconds": estimateDurationSeconds(trimmed)}}
	}
	return structured, timeline
}

func buildTranscriptFromSegments(segments []map[string]interface{}) ([]map[string]interface{}, []map[string]interface{}) {
	if len(segments) == 0 {
		return nil, nil
	}
	structured := make([]map[string]interface{}, 0, len(segments))
	timeline := make([]map[string]interface{}, 0, len(segments))
	cursor := 0
	for _, segment := range segments {
		content := firstNonEmptyText(
			segment["text"],
			segment["content"],
			segment["transcript"],
			segment["utterance"],
		)
		if content == "" {
			continue
		}
		speaker := firstNonEmptyText(segment["speaker_role"], segment["speaker"], segment["role"])
		if speaker == "" {
			speaker = "unknown"
		}
		start := pickInt(segment["start_seconds"], segment["start_time"], segment["start"], segment["offset"])
		if start == nil {
			if startMs := pickInt(segment["start_ms"]); startMs != nil {
				value := *startMs / 1000
				start = &value
			}
		}
		end := pickInt(segment["end_seconds"], segment["end_time"], segment["end"])
		if end == nil {
			if endMs := pickInt(segment["end_ms"]); endMs != nil {
				value := *endMs / 1000
				end = &value
			}
		}
		startSec := cursor
		if start != nil && *start >= 0 {
			startSec = *start
		}
		endSec := startSec + estimateDurationSeconds(content)
		if end != nil && *end >= startSec {
			endSec = *end
		}
		if endSec < startSec {
			endSec = startSec
		}
		structured = append(structured, map[string]interface{}{
			"speaker": speaker,
			"content": content,
		})
		timeline = append(timeline, map[string]interface{}{
			"speaker":       speaker,
			"text":          content,
			"start_seconds": startSec,
			"end_seconds":   endSec,
		})
		cursor = endSec
	}
	if len(structured) == 0 {
		return nil, nil
	}
	return structured, timeline
}

func (s *Service) loadRawTranscriptionSegments(ctx context.Context, recordingID int64) ([]map[string]interface{}, error) {
	var raw interface{}
	err := s.store.pool.QueryRow(ctx, `SELECT transcription_segments FROM recordings WHERE id = $1`, recordingID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	return decodeSegmentsJSON(raw), nil
}

func (s *Service) loadCleanedTranscriptionSegments(ctx context.Context, recordingID int64) ([]map[string]interface{}, error) {
	var raw interface{}
	err := s.store.pool.QueryRow(ctx, `SELECT cleaned_transcription FROM recordings WHERE id = $1`, recordingID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	return decodeSegmentsJSON(raw), nil
}

func decodeSegmentsJSON(raw interface{}) []map[string]interface{} {
	decoded := decodeNestedJSONValue(raw, 0)
	switch value := decoded.(type) {
	case []interface{}:
		return toMapSlice(value)
	case []map[string]interface{}:
		return value
	case map[string]interface{}:
		for _, key := range []string{"segments", "items", "data"} {
			if nested, ok := value[key]; ok {
				return decodeSegmentsJSON(nested)
			}
		}
	}
	return nil
}

func decodeNestedJSONValue(raw interface{}, depth int) interface{} {
	if depth > 3 || raw == nil {
		return raw
	}
	switch value := raw.(type) {
	case []byte:
		trimmed := strings.TrimSpace(string(value))
		if trimmed == "" {
			return nil
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return raw
		}
		return decodeNestedJSONValue(parsed, depth+1)
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		if !(strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
			return value
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return value
		}
		return decodeNestedJSONValue(parsed, depth+1)
	default:
		return raw
	}
}

func firstNonEmptyText(values ...interface{}) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func pickInt(values ...interface{}) *int {
	for _, value := range values {
		if value == nil {
			continue
		}
		switch v := value.(type) {
		case int:
			out := v
			return &out
		case int32:
			out := int(v)
			return &out
		case int64:
			out := int(v)
			return &out
		case float32:
			out := int(v)
			return &out
		case float64:
			out := int(v)
			return &out
		case json.Number:
			if i64, err := v.Int64(); err == nil {
				out := int(i64)
				return &out
			}
			if f64, err := v.Float64(); err == nil {
				out := int(f64)
				return &out
			}
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				continue
			}
			if i64, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
				out := int(i64)
				return &out
			}
			if f64, err := strconv.ParseFloat(trimmed, 64); err == nil {
				out := int(f64)
				return &out
			}
		}
	}
	return nil
}

func estimateDurationSeconds(text string) int {
	runes := len([]rune(strings.TrimSpace(text)))
	if runes <= 0 {
		return 5
	}
	seconds := runes / 6
	if seconds < 5 {
		return 5
	}
	if seconds > 90 {
		return 90
	}
	return seconds
}

func pickString(source map[string]interface{}, key string) string {
	if source == nil {
		return ""
	}
	raw, ok := source[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return strings.TrimSpace(v.String())
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, bool:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	default:
		return ""
	}
}

func literalStringPtr(v string) *string {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}

func pickStringPtr(values ...string) *string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			s := strings.TrimSpace(v)
			return &s
		}
	}
	return nil
}

func pickFloat(source map[string]interface{}, key string) *float64 {
	if source == nil {
		return nil
	}
	raw, ok := source[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case float64:
		return &v
	case float32:
		x := float64(v)
		return &x
	case int:
		x := float64(v)
		return &x
	case int64:
		x := float64(v)
		return &x
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		if x, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return &x
		}
	}
	return nil
}

func pickFloatPtr(values ...*float64) *float64 {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func pickBool(source map[string]interface{}, key string) *bool {
	if source == nil {
		return nil
	}
	raw, ok := source[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case bool:
		return &v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		if s == "true" || s == "1" || s == "yes" || s == "y" {
			b := true
			return &b
		}
		if s == "false" || s == "0" || s == "no" || s == "n" {
			b := false
			return &b
		}
	}
	return nil
}

func pickBoolPtr(values ...*bool) *bool {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

// Recording Statistics Services

// GetStatsOverview retrieves overview statistics
func (s *Service) GetStatsOverview(ctx context.Context, tenantID int64, startDate, endDate *string) (*RecordingStatsOverviewResponse, error) {
	return s.store.GetStatsOverview(ctx, tenantID, startDate, endDate)
}

// GetStatsByScene retrieves statistics by scene
func (s *Service) GetStatsByScene(ctx context.Context, tenantID int64) ([]RecordingStatsBySceneResponse, error) {
	return s.store.GetStatsByScene(ctx, tenantID)
}

// GetStatsBySource retrieves statistics by source
func (s *Service) GetStatsBySource(ctx context.Context, tenantID int64) ([]RecordingStatsBySourceResponse, error) {
	return s.store.GetStatsBySource(ctx, tenantID)
}

// Recording Task Services

// ListRecordingTasks retrieves a paginated list of recording tasks
func (s *Service) ListRecordingTasks(ctx context.Context, req TaskListRequest) ([]*TaskResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	tasks, total, err := s.store.ListRecordingTasks(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*TaskResponse, len(tasks))
	for i, t := range tasks {
		responses[i] = toTaskResponse(t)
	}
	if err := s.enrichTaskListResponse(ctx, responses); err != nil {
		return nil, 0, err
	}
	shouldCompact := req.RecordingID == nil
	if shouldCompact {
		for _, item := range responses {
			compactTaskListItem(item)
		}
	}

	return responses, total, nil
}

// GetTask retrieves a recording task by ID
func (s *Service) GetTask(ctx context.Context, id int64) (*TaskResponse, error) {
	task, err := s.store.GetTaskByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toTaskResponse(task), nil
}

// CompleteTask marks a task as completed
func (s *Service) CompleteTask(ctx context.Context, id int64, completedBy int64, req CompleteTaskRequest) error {
	return s.store.CompleteTask(ctx, id, completedBy)
}

// CancelTask marks a task as cancelled
func (s *Service) CancelTask(ctx context.Context, id int64, req CancelTaskRequest) error {
	if req.Reason == "" {
		return fmt.Errorf("cancel reason is required")
	}
	return s.store.CancelTask(ctx, id, req.Reason)
}

// GetTaskStats retrieves task statistics
func (s *Service) GetTaskStats(ctx context.Context, tenantID int64, tenantIDs []int64, assignedTo *int64) (*RecordingTaskStatsResponse, error) {
	return s.store.GetTaskStats(ctx, tenantID, tenantIDs, assignedTo)
}

// GetDailyBriefing retrieves a daily briefing
func (s *Service) GetDailyBriefing(ctx context.Context, tenantID int64, assignedTo *int64, date time.Time) (*DailyBriefingResponse, error) {
	return s.store.GetDailyBriefing(ctx, tenantID, assignedTo, date)
}

// Recording Prompt Services

// ListRecordingPrompts retrieves a paginated list of recording prompts
func (s *Service) ListRecordingPrompts(ctx context.Context, req RecordingPromptListRequest) ([]*RecordingPrompt, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	return s.store.ListRecordingPrompts(ctx, req)
}

// GetRecordingPrompt retrieves a recording prompt by code
func (s *Service) GetRecordingPrompt(ctx context.Context, code string) (*RecordingPrompt, error) {
	return s.store.GetRecordingPromptByCode(ctx, code)
}

// CreateRecordingPrompt creates a new recording prompt
func (s *Service) CreateRecordingPrompt(ctx context.Context, req CreateRecordingPromptRequest) (*RecordingPrompt, error) {
	// Validate request
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.PromptText == "" {
		return nil, fmt.Errorf("prompt_text is required")
	}
	if req.SystemPrompt == "" {
		return nil, fmt.Errorf("system_prompt is required")
	}

	return s.store.CreateRecordingPrompt(ctx, req)
}

// UpdateRecordingPrompt updates a recording prompt
func (s *Service) UpdateRecordingPrompt(ctx context.Context, code string, req UpdateRecordingPromptRequest) (*RecordingPrompt, error) {
	return s.store.UpdateRecordingPrompt(ctx, code, req)
}

// DeleteRecordingPrompt deletes a recording prompt
func (s *Service) DeleteRecordingPrompt(ctx context.Context, code string) error {
	return s.store.DeleteRecordingPrompt(ctx, code)
}

// Best Practice Services

// ListBestPractices retrieves a list of best practices
func (s *Service) ListBestPractices(ctx context.Context, tenantID int64) ([]*RecordingBestPractice, error) {
	return s.store.ListBestPractices(ctx, tenantID)
}

// AddBestPractice adds a recording to best practices
func (s *Service) AddBestPractice(ctx context.Context, tenantID, recordingID, createdBy int64, req AddBestPracticeRequest) (*RecordingBestPractice, error) {
	// Validate request
	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}

	return s.store.AddBestPractice(ctx, tenantID, recordingID, createdBy, req)
}

// DeleteBestPractice removes a recording from best practices
func (s *Service) DeleteBestPractice(ctx context.Context, recordingID int64) error {
	return s.store.DeleteBestPractice(ctx, recordingID)
}

// Placeholder methods for remaining endpoints (to be implemented)

type workerJobRequest struct {
	RecordingID       int64  `json:"recording_id"`
	TenantID          int64  `json:"tenant_id"`
	JobType           string `json:"job_type"`
	Force             bool   `json:"force,omitempty"`
	TraceID           string `json:"trace_id,omitempty"`
	TriggerSource     string `json:"trigger_source,omitempty"`
	VendorRecordingID string `json:"vendor_recording_id,omitempty"`
}

type workerJobResponse struct {
	JobID string `json:"job_id"`
}

func (s *Service) submitLingceWorkerJob(ctx context.Context, recordingID int64, jobType, triggerSource string) (string, error) {
	if s.lingceWorkerURL == "" {
		return "", &recordingValidationError{code: "LINGCE_WORKER_NOT_CONFIGURED", message: "lingce-worker url is not configured"}
	}
	if strings.TrimSpace(triggerSource) == "" {
		triggerSource = "manual_replay"
	}
	url := fmt.Sprintf("%s/internal/jobs/enqueue?recording_id=%d&job_type=%s&trigger_source=%s", s.lingceWorkerURL, recordingID, jobType, triggerSource)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create lingce-worker request: %w", err)
	}
	if s.lingceWorkerToken != "" {
		httpReq.Header.Set("X-Internal-Token", s.lingceWorkerToken)
	}
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
			return "", &workerUnavailableError{cause: err}
		}
		return "", &workerUnavailableError{cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		message := strings.TrimSpace(errResp.Error)
		if message == "" {
			message = fmt.Sprintf("lingce-worker returned status %d", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusBadRequest {
			return "", &recordingValidationError{code: "JOB_REJECTED", message: message}
		}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			return "", &workerUnavailableError{cause: fmt.Errorf("lingce-worker returned status %d", resp.StatusCode)}
		}
		return "", errors.New(message)
	}

	var result struct {
		Action string         `json:"action"`
		Status string         `json:"status"`
		Data   map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode lingce-worker response: %w", err)
	}
	if status, ok := result.Data["status"].(string); ok && strings.TrimSpace(status) != "" {
		return strings.TrimSpace(status), nil
	}
	return "queued", nil
}

func (s *Service) submitWorkerJob(ctx context.Context, req workerJobRequest) (string, error) {
	if s.workerURL == "" {
		return "", fmt.Errorf("recording worker url is not configured")
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal worker job request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.workerURL+"/v1/jobs", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create worker request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.workerToken != "" {
		httpReq.Header.Set("X-Internal-Token", s.workerToken)
		httpReq.Header.Set("Authorization", "Bearer "+s.workerToken)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
			return "", &workerUnavailableError{cause: err}
		}
		return "", &workerUnavailableError{cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			return "", &workerUnavailableError{cause: fmt.Errorf("recording worker returned status %d", resp.StatusCode)}
		}
		return "", fmt.Errorf("recording worker returned status %d", resp.StatusCode)
	}

	var result workerJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode worker response: %w", err)
	}

	return result.JobID, nil
}

func (s *Service) submitLingceWorkerReplay(ctx context.Context, recordingID int64) (string, error) {
	if s.lingceWorkerURL == "" {
		return "", &recordingValidationError{code: "LINGCE_WORKER_NOT_CONFIGURED", message: "lingce-worker url is not configured"}
	}
	url := fmt.Sprintf("%s/internal/analysis/replay?recording_id=%d&trigger_source=%s", s.lingceWorkerURL, recordingID, "manual_replay")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create lingce-worker request: %w", err)
	}
	if s.lingceWorkerToken != "" {
		httpReq.Header.Set("X-Internal-Token", s.lingceWorkerToken)
	}
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
			return "", &workerUnavailableError{cause: err}
		}
		return "", &workerUnavailableError{cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		message := strings.TrimSpace(errResp.Error)
		if message == "" {
			message = fmt.Sprintf("lingce-worker returned status %d", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusBadRequest {
			return "", &recordingValidationError{code: "REPLAY_REJECTED", message: message}
		}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			return "", &workerUnavailableError{cause: fmt.Errorf("lingce-worker returned status %d", resp.StatusCode)}
		}
		return "", errors.New(message)
	}

	var result struct {
		Action string         `json:"action"`
		Status string         `json:"status"`
		Data   map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode lingce-worker response: %w", err)
	}
	if runID, ok := result.Data["run_id"].(string); ok && strings.TrimSpace(runID) != "" {
		return strings.TrimSpace(runID), nil
	}
	return "", fmt.Errorf("lingce-worker response missing run_id")
}

func (s *Service) ReanalyzeRecording(ctx context.Context, id int64) (string, error) {
	if s.lingceWorkerURL == "" {
		return s.TriggerAnalyze(ctx, id, TriggerAnalyzeRequest{Force: true})
	}
	if _, err := s.store.GetRecordingByID(ctx, id); err != nil {
		return "", err
	}
	return s.submitLingceWorkerReplay(ctx, id)
}

func (s *Service) triggerWorkerJob(ctx context.Context, id int64, jobType string, force bool) (string, error) {
	recording, err := s.store.GetRecordingByID(ctx, id)
	if err != nil {
		return "", err
	}

	if s.lingceWorkerURL != "" {
		return s.submitLingceWorkerJob(ctx, id, jobType, "manual_replay")
	}

	traceID := fmt.Sprintf("trace-manual-%d-%d", id, time.Now().UnixNano())
	vendorRecordingID := ""
	if recording.DeviceNo != "" {
		vendorRecordingID = recording.DeviceNo
	}
	jobID, err := s.submitWorkerJob(ctx, workerJobRequest{
		RecordingID:       id,
		TenantID:          recording.TenantID,
		JobType:           jobType,
		Force:             force,
		TraceID:           traceID,
		TriggerSource:     "manual_replay",
		VendorRecordingID: vendorRecordingID,
	})
	if err != nil {
		return "", err
	}

	return jobID, nil
}

// TriggerTranscribe triggers transcription for a recording
func (s *Service) TriggerTranscribe(ctx context.Context, id int64, req TriggerTranscribeRequest) (string, error) {
	return s.triggerWorkerJob(ctx, id, "transcribe", req.Force)
}

// TriggerAnalyze triggers analysis for a recording
func (s *Service) TriggerAnalyze(ctx context.Context, id int64, req TriggerAnalyzeRequest) (string, error) {
	return s.triggerWorkerJob(ctx, id, "analyze", req.Force)
}

// TriggerClean triggers cleaning for a recording
func (s *Service) TriggerClean(ctx context.Context, id int64, req TriggerCleanRequest) (string, error) {
	return s.triggerWorkerJob(ctx, id, "clean", req.Force)
}

// GetAnalysisResult retrieves analysis result for a recording
func (s *Service) GetAnalysisResult(ctx context.Context, id int64) (*RecordingAnalysisResult, error) {
	recording, err := s.store.GetRecordingByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if recording.TranscriptText == nil &&
		recording.DoctorSummary == nil &&
		recording.TherapistSummary == nil &&
		recording.ConsultantSummary == nil {
		return nil, fmt.Errorf("analysis result not found")
	}

	return &RecordingAnalysisResult{
		ID:                recording.ID,
		RecordingID:       recording.ID,
		TenantID:          recording.TenantID,
		TranscriptText:    recording.TranscriptText,
		DoctorSummary:     recording.DoctorSummary,
		TherapistSummary:  recording.TherapistSummary,
		ConsultantSummary: recording.ConsultantSummary,
		CreatedAt:         recording.CreatedAt,
		UpdatedAt:         recording.UpdatedAt,
	}, nil
}

// GetQualityControlDashboard retrieves quality control dashboard
func (s *Service) GetQualityControlDashboard(ctx context.Context, tenantID int64) (*QualityControlDashboardResponse, error) {
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}

	total := stats.TotalRecordings
	if total == 0 {
		return &QualityControlDashboardResponse{
			TopIssues: []IssueCount{},
		}, nil
	}

	qualified := stats.CompletedRecordings
	unqualified := total - qualified
	avgScore := float64(qualified) / float64(total) * 100

	topIssues := []IssueCount{}
	if stats.PendingRecordings > 0 {
		topIssues = append(topIssues, IssueCount{Issue: "待处理录音较多", Count: stats.PendingRecordings})
	}
	if stats.FailedRecordings > 0 {
		topIssues = append(topIssues, IssueCount{Issue: "处理失败录音", Count: stats.FailedRecordings})
	}
	if len(topIssues) == 0 {
		topIssues = append(topIssues, IssueCount{Issue: "当前无明显质量异常", Count: 0})
	}

	return &QualityControlDashboardResponse{
		TotalRecordings:  total,
		QualifiedCount:   qualified,
		UnqualifiedCount: unqualified,
		AvgScore:         avgScore,
		TopIssues:        topIssues,
	}, nil
}

// GetDoctorAbilityRanking retrieves doctor list style ranking for medical recordings.
func (s *Service) GetDoctorAbilityRanking(ctx context.Context, tenantID int64, period string) ([]DoctorAbilityRankingResponse, error) {
	windowDays := 30
	switch strings.TrimSpace(strings.ToLower(period)) {
	case "3m", "quarter":
		windowDays = 90
	case "6m", "half_year":
		windowDays = 180
	case "1m", "month", "":
		windowDays = 30
	}

	now := time.Now()
	currentStart := now.AddDate(0, 0, -(windowDays - 1))
	prevStart := currentStart.AddDate(0, 0, -windowDays)

	type doctorAbilityRow struct {
		EmployeeID int64
		Name       string
		TS         time.Time
		Analysis   map[string]interface{}
	}
	rows, err := s.store.pool.Query(ctx, `
		SELECT r.employee_id,
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), NULLIF(e.phone, ''), '未知医生') AS employee_name,
		       COALESCE(r.recorded_at, r.created_at) AS ts,
		       COALESCE(r.analysis_result, '{}'::json) AS analysis_result
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) <= $3
		  AND (
			EXISTS (
				SELECT 1
				FROM inst_employee_roles ier
				WHERE ier.tenant_id = r.tenant_id
				  AND ier.employee_id = r.employee_id
				  AND lower(ier.role_code) = ANY(ARRAY['doctor', 'therapist', 'doctor_assistant'])
			)
			OR NOT EXISTS (
				SELECT 1
				FROM inst_employee_roles ier_any
				WHERE ier_any.tenant_id = r.tenant_id
				  AND ier_any.employee_id = r.employee_id
			)
		  )
	`, tenantID, prevStart, now)
	if err != nil {
		return nil, fmt.Errorf("failed to query doctor ability rows: %w", err)
	}
	defer rows.Close()

	type doctorAbilityAgg struct {
		EmployeeID          int64
		Name                string
		CurrentCount        int64
		CurrentSegueScores  []float64
		PreviousSegueScores []float64
		StageScores         map[string][]float64
		PatientStatusTotal  int64
		PatientAcceptCount  int64
		EmotionTotal        int64
		EmotionImproveCount int64
		CriticalGapCount    int64
	}
	aggByEmployee := map[int64]*doctorAbilityAgg{}

	for rows.Next() {
		var row doctorAbilityRow
		if scanErr := rows.Scan(&row.EmployeeID, &row.Name, &row.TS, &row.Analysis); scanErr != nil {
			return nil, fmt.Errorf("failed to scan doctor ability row: %w", scanErr)
		}
		if row.EmployeeID <= 0 {
			continue
		}
		agg := aggByEmployee[row.EmployeeID]
		if agg == nil {
			agg = &doctorAbilityAgg{
				EmployeeID:  row.EmployeeID,
				Name:        row.Name,
				StageScores: map[string][]float64{"G1": {}, "G2": {}, "G3": {}, "G4": {}, "G5": {}, "G6": {}},
			}
			aggByEmployee[row.EmployeeID] = agg
		}

		score := resolveWeeklyDisplayScore(row.Analysis)
		if row.TS.Before(currentStart) {
			if score > 0 {
				agg.PreviousSegueScores = append(agg.PreviousSegueScores, score)
			}
			continue
		}

		agg.CurrentCount++
		if score > 0 {
			agg.CurrentSegueScores = append(agg.CurrentSegueScores, score)
		}

		quality := pickMap(row.Analysis, "quality_score")
		if stagesAny, ok := quality["stages"]; ok {
			if stages, ok := stagesAny.([]interface{}); ok {
				for _, item := range stages {
					stage, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					code := toDimensionCode(firstNonEmptyText(stage["group"], stage["code"], stage["key"], stage["name"]))
					if code == "" {
						continue
					}
					if stageScore, ok := toFloat(stage["score"]); ok {
						agg.StageScores[code] = append(agg.StageScores[code], stageScore)
					}
				}
			}
		}

		statusRaw := firstNonEmptyText(
			pickNestedStatus(row.Analysis, "visit_outcome"),
			pickString(row.Analysis, "visit_outcome_status"),
			pickString(row.Analysis, "visit_outcome"),
			pickNestedStatus(pickMap(row.Analysis, "analysis_summary"), "visit_outcome"),
			pickString(pickMap(row.Analysis, "analysis_summary"), "visit_outcome_status"),
			pickString(pickMap(row.Analysis, "analysis_summary"), "visit_outcome"),
		)
		if normalized := normalizePatientStatus(statusRaw); normalized != "" {
			agg.PatientStatusTotal++
			if normalized == "顺利接受" {
				agg.PatientAcceptCount++
			}
		}

		if startEmotion, endEmotion, ok := readEmotionPair(row.Analysis); ok {
			agg.EmotionTotal++
			if isEmotionImproved(startEmotion, endEmotion) {
				agg.EmotionImproveCount++
			}
		}

		if critical := pickBool(row.Analysis, "critical_gap"); critical != nil && *critical {
			agg.CriticalGapCount++
		}
	}

	out := make([]DoctorAbilityRankingResponse, 0, len(aggByEmployee))
	for _, agg := range aggByEmployee {
		if agg.CurrentCount == 0 {
			continue
		}
		curAvg := avgFloat(agg.CurrentSegueScores)
		prevAvg := avgFloat(agg.PreviousSegueScores)
		strongest, weakest := findStrongestWeakestDimension(agg.StageScores)
		acceptRate := 0.0
		if agg.PatientStatusTotal > 0 {
			acceptRate = roundFloat((float64(agg.PatientAcceptCount)/float64(agg.PatientStatusTotal))*100, 1)
		}
		emotionRate := 0.0
		if agg.EmotionTotal > 0 {
			emotionRate = roundFloat((float64(agg.EmotionImproveCount)/float64(agg.EmotionTotal))*100, 1)
		}

		out = append(out, DoctorAbilityRankingResponse{
			EmployeeID:     agg.EmployeeID,
			EmployeeName:   agg.Name,
			RecordingCount: agg.CurrentCount,
			AvgScore:       roundFloat(curAvg, 1),
			SegueAvg:       roundFloat(curAvg, 1),
			SegueTrend:     roundFloat(curAvg-prevAvg, 1),
			StrongestDim:   strongest,
			WeakestDim:     weakest,
			AcceptanceRate: acceptRate,
			EmotionRate:    emotionRate,
			CriticalGaps:   agg.CriticalGapCount,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].SegueAvg == out[j].SegueAvg {
			return out[i].RecordingCount > out[j].RecordingCount
		}
		return out[i].SegueAvg > out[j].SegueAvg
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	return out, nil
}

func avgFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func findStrongestWeakestDimension(stageScores map[string][]float64) (string, string) {
	type dimAvg struct {
		code string
		avg  float64
	}
	labels := map[string]string{
		"G1": "建立接诊环境",
		"G2": "引出信息",
		"G3": "给予信息",
		"G4": "理解患者视角",
		"G5": "结束接诊",
		"G6": "治疗/预防计划",
	}
	all := make([]dimAvg, 0, 6)
	for _, code := range []string{"G1", "G2", "G3", "G4", "G5", "G6"} {
		avg := avgFloat(stageScores[code])
		if avg <= 0 {
			continue
		}
		all = append(all, dimAvg{code: code, avg: avg})
	}
	if len(all) == 0 {
		return "", ""
	}
	sort.Slice(all, func(i, j int) bool { return all[i].avg > all[j].avg })
	strongest := all[0].code + " " + labels[all[0].code]
	weakest := all[len(all)-1].code + " " + labels[all[len(all)-1].code]
	return strongest, weakest
}

// GetDoctorAbilityDetail retrieves detailed doctor ability
func (s *Service) GetDoctorAbilityDetail(ctx context.Context, tenantID, employeeID int64) (*DoctorAbilityDetailResponse, error) {
	name, err := s.employeeStore.GetEmployeeNameByID(ctx, employeeID, tenantID)
	if err != nil {
		return nil, err
	}

	var total, completed int64
	if err := s.store.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(CASE WHEN analysis_status = 'completed' THEN 1 END)
		FROM recordings
		WHERE tenant_id = $1 AND employee_id = $2
	`, tenantID, employeeID).Scan(&total, &completed); err != nil {
		return nil, fmt.Errorf("failed to query doctor detail: %w", err)
	}

	score := 0.0
	if total > 0 {
		score = float64(completed) / float64(total) * 100
	}

	return &DoctorAbilityDetailResponse{
		EmployeeID:           employeeID,
		EmployeeName:         name,
		CommunicationScore:   score,
		ProfessionalismScore: score,
		EmpathyScore:         score,
		EfficiencyScore:      score,
		RecentTrend:          "stable",
		Strengths:            []string{"按计划完成录音处理"},
		Weaknesses:           []string{"建议提升高峰期处理效率"},
	}, nil
}

// GetCommunicationAnalysis retrieves communication analysis
func (s *Service) GetCommunicationAnalysis(ctx context.Context, tenantID int64) (*CommunicationAnalysisResponse, error) {
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}
	top, err := s.GetDoctorAbilityRanking(ctx, tenantID, "")
	if err != nil {
		return nil, err
	}
	if len(top) > 5 {
		top = top[:5]
	}
	avg := 0.0
	if stats.TotalRecordings > 0 {
		avg = float64(stats.CompletedRecordings) / float64(stats.TotalRecordings) * 100
	}
	common := []IssueCount{}
	if stats.PendingRecordings > 0 {
		common = append(common, IssueCount{Issue: "转写待完成", Count: stats.PendingRecordings})
	}
	if stats.FailedRecordings > 0 {
		common = append(common, IssueCount{Issue: "处理失败需复核", Count: stats.FailedRecordings})
	}
	return &CommunicationAnalysisResponse{
		TotalRecordings:       stats.TotalRecordings,
		AvgCommunicationScore: avg,
		TopCommunicators:      top,
		CommonIssues:          common,
	}, nil
}

// GetWeeklyMeetingMaterial retrieves weekly meeting material
func (s *Service) GetWeeklyMeetingMaterial(ctx context.Context, tenantID int64) (*WeeklyMeetingMaterialResponse, error) {
	now := time.Now()
	weekStart := now.AddDate(0, 0, -6)
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}
	practices, err := s.store.ListBestPractices(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	best := make([]BestPracticeItem, 0, len(practices))
	for i, p := range practices {
		if i >= 5 {
			break
		}
		best = append(best, BestPracticeItem{
			RecordingID: p.RecordingID,
			Title:       p.Title,
			Description: strOrDefault(p.Description, ""),
		})
	}
	return &WeeklyMeetingMaterialResponse{
		WeekStart: weekStart.Format("2006-01-02"),
		WeekEnd:   now.Format("2006-01-02"),
		Highlights: []string{
			fmt.Sprintf("本周累计处理录音 %d 条", stats.TotalRecordings),
			fmt.Sprintf("本周完成率 %.1f%%", safeRate(stats.CompletedRecordings, stats.TotalRecordings)),
		},
		BestPractices:    best,
		ImprovementAreas: []string{"关注失败和待处理录音", "持续优化跟进动作闭环"},
	}, nil
}

type weeklySummaryRow struct {
	ID          int64
	EmployeeID  int64
	Employee    string
	PatientName string
	Analysis    map[string]interface{}
	TS          time.Time
}

type weeklySummaryHighlightEntry struct {
	Dimension string
	Text      string
}

type medicalTrendRow struct {
	Analysis map[string]interface{}
	TS       time.Time
}

// GetWeeklySummary retrieves weekly summary
func (s *Service) GetWeeklySummary(ctx context.Context, tenantID int64, weekOffset int) (*WeeklySummaryResponse, error) {
	now := time.Now()
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -int(now.Weekday())+1)
	if now.Weekday() == time.Sunday {
		monday = monday.AddDate(0, 0, -6)
	}
	weekStart := monday.AddDate(0, 0, weekOffset*7)
	weekEnd := weekStart.AddDate(0, 0, 7)
	prevStart := weekStart.AddDate(0, 0, -7)
	prev2Start := weekStart.AddDate(0, 0, -14)

	rows, err := s.store.pool.Query(ctx, `
		SELECT r.id,
		       r.employee_id,
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), NULLIF(e.phone, ''), '未知医生') AS employee_name,
		       COALESCE(c.name, '') AS patient_name,
		       COALESCE(r.analysis_result, '{}'::json) AS analysis_result,
		       COALESCE(r.recorded_at, r.created_at) AS ts
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) < $3
		  AND (
			EXISTS (
				SELECT 1
				FROM inst_employee_roles ier
				WHERE ier.tenant_id = r.tenant_id
				  AND ier.employee_id = r.employee_id
				  AND lower(ier.role_code) = ANY(ARRAY['doctor', 'therapist', 'doctor_assistant'])
			)
			OR NOT EXISTS (
				SELECT 1
				FROM inst_employee_roles ier_any
				WHERE ier_any.tenant_id = r.tenant_id
				  AND ier_any.employee_id = r.employee_id
			)
		  )
		ORDER BY ts DESC
	`, tenantID, prev2Start, weekEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to query weekly summary rows: %w", err)
	}
	defer rows.Close()

	allRows := make([]weeklySummaryRow, 0, 512)
	for rows.Next() {
		var item weeklySummaryRow
		if scanErr := rows.Scan(&item.ID, &item.EmployeeID, &item.Employee, &item.PatientName, &item.Analysis, &item.TS); scanErr != nil {
			return nil, fmt.Errorf("failed to scan weekly summary row: %w", scanErr)
		}
		allRows = append(allRows, item)
	}

	currentRows := make([]weeklySummaryRow, 0, len(allRows))
	prevRows := make([]weeklySummaryRow, 0, len(allRows))
	prev2Rows := make([]weeklySummaryRow, 0, len(allRows))
	for _, row := range allRows {
		switch {
		case !row.TS.Before(weekStart) && row.TS.Before(weekEnd):
			currentRows = append(currentRows, row)
		case !row.TS.Before(prevStart) && row.TS.Before(weekStart):
			prevRows = append(prevRows, row)
		case !row.TS.Before(prev2Start) && row.TS.Before(prevStart):
			prev2Rows = append(prev2Rows, row)
		}
	}

	patientStatusCounter := map[string]int64{}
	blockerCounter := map[string]int64{}
	highlights := make([]WeeklySummaryHighlightItem, 0, 64)
	criticalAttention := make([]WeeklySummaryAttentionItem, 0, 64)
	doctorIDMap := map[string]int64{}

	for _, row := range currentRows {
		doctorIDMap[row.Employee] = row.EmployeeID

		rawStatus := firstNonEmptyText(
			pickNestedStatus(row.Analysis, "visit_outcome"),
			pickString(row.Analysis, "visit_outcome_status"),
			pickString(row.Analysis, "visit_outcome"),
			pickNestedStatus(pickMap(row.Analysis, "analysis_summary"), "visit_outcome"),
			pickString(pickMap(row.Analysis, "analysis_summary"), "visit_outcome_status"),
			pickString(pickMap(row.Analysis, "analysis_summary"), "visit_outcome"),
		)
		if normalized := normalizePatientStatus(rawStatus); normalized != "" {
			patientStatusCounter[normalized]++
		}

		blockers := extractCoreBlockers(row.Analysis)
		for _, blocker := range blockers {
			blockerCounter[blocker]++
		}

		score := resolveWeeklyDisplayScore(row.Analysis)
		for _, entry := range extractWeeklyHighlightEntries(row.Analysis) {
			recID := row.ID
			item := WeeklySummaryHighlightItem{
				EmployeeName:  row.Employee,
				Type:          "highlight",
				Dimension:     formatWeeklyDimension(entry.Dimension),
				HighlightText: entry.Text,
				RecordingIDs:  []int64{recID},
			}
			if row.EmployeeID > 0 {
				eid := row.EmployeeID
				item.EmployeeID = &eid
			}
			if score > 0 {
				v := roundFloat(score, 1)
				item.Value = &v
			}
			highlights = append(highlights, item)
		}

		if isCritical := pickBool(row.Analysis, "critical_gap"); isCritical != nil && *isCritical {
			summary := strings.TrimSpace(firstNonEmptyText(pickString(row.Analysis, "critical_summary")))
			if summary == "" && len(blockers) > 0 {
				summary = blockers[0]
			}
			if summary == "" {
				summary = "本周出现关键缺口"
			}
			recID := row.ID
			item := WeeklySummaryAttentionItem{
				EmployeeName:    row.Employee,
				Type:            "attention",
				Dimension:       "关键缺口",
				CriticalSummary: summary,
				RecordingIDs:    []int64{recID},
			}
			if row.EmployeeID > 0 {
				eid := row.EmployeeID
				item.EmployeeID = &eid
			}
			criticalAttention = append(criticalAttention, item)
		}
	}

	curAvg := weeklyDoctorScoreAverage(currentRows)
	prevAvg := weeklyDoctorScoreAverage(prevRows)
	prev2Avg := weeklyDoctorScoreAverage(prev2Rows)

	trendAttention := make([]WeeklySummaryAttentionItem, 0, 16)
	for doctor, curVal := range curAvg {
		prevVal, ok1 := prevAvg[doctor]
		prev2Val, ok2 := prev2Avg[doctor]
		if !ok1 || !ok2 {
			continue
		}
		if prev2Val > prevVal && prevVal > curVal {
			item := WeeklySummaryAttentionItem{
				EmployeeName: doctor,
				Type:         "attention",
				Dimension:    "SEGUE 沟通分",
				Trend: []float64{
					roundFloat(prev2Val, 1),
					roundFloat(prevVal, 1),
					roundFloat(curVal, 1),
				},
			}
			if eid := doctorIDMap[doctor]; eid > 0 {
				id := eid
				item.EmployeeID = &id
			}
			trendAttention = append(trendAttention, item)
		}
	}

	sort.Slice(highlights, func(i, j int) bool {
		iv := 0.0
		jv := 0.0
		if highlights[i].Value != nil {
			iv = *highlights[i].Value
		}
		if highlights[j].Value != nil {
			jv = *highlights[j].Value
		}
		return iv > jv
	})
	if len(highlights) > 10 {
		highlights = highlights[:10]
	}

	attention := append(trendAttention, criticalAttention...)
	if len(attention) > 20 {
		attention = attention[:20]
	}

	coreBlockers := make([]WeeklySummaryBlockerItem, 0, len(blockerCounter))
	for blocker, count := range blockerCounter {
		coreBlockers = append(coreBlockers, WeeklySummaryBlockerItem{
			Blocker: blocker,
			Count:   count,
		})
	}
	sort.Slice(coreBlockers, func(i, j int) bool { return coreBlockers[i].Count > coreBlockers[j].Count })
	if len(coreBlockers) > 10 {
		coreBlockers = coreBlockers[:10]
	}

	highlightCandidates, candidateTotal, candidateErr := s.loadWeeklyHighlightCandidates(ctx, tenantID)
	if candidateErr != nil {
		return nil, candidateErr
	}

	top, err := s.GetDoctorAbilityRanking(ctx, tenantID, "")
	if err != nil {
		return nil, err
	}
	if len(top) > 3 {
		top = top[:3]
	}

	keyInsights := []string{}
	if len(coreBlockers) > 0 {
		keyInsights = append(keyInsights, "主要阻塞："+coreBlockers[0].Blocker)
	}
	if len(attention) > 0 {
		keyInsights = append(keyInsights, "需重点关注人员："+attention[0].EmployeeName)
	}
	if len(keyInsights) == 0 {
		keyInsights = append(keyInsights, "本周整体稳定")
	}

	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}

	return &WeeklySummaryResponse{
		WeekStart:       weekStart.Format("2006-01-02"),
		WeekEnd:         weekEnd.AddDate(0, 0, -1).Format("2006-01-02"),
		TotalRecordings: stats.TotalRecordings,
		AvgScore:        safeRate(stats.CompletedRecordings, stats.TotalRecordings),
		TopPerformers:   top,
		KeyInsights:     keyInsights,
		Week: &WeeklySummaryWeek{
			Start:  weekStart.Format("2006-01-02"),
			End:    weekEnd.AddDate(0, 0, -1).Format("2006-01-02"),
			Offset: weekOffset,
		},
		RecordingCount:           int64(len(currentRows)),
		RecordingCountPrev:       int64(len(prevRows)),
		Attention:                attention,
		Highlights:               highlights,
		PatientStatus:            patientStatusCounter,
		CoreBlockers:             coreBlockers,
		HighlightCandidatesTotal: candidateTotal,
		HighlightCandidates:      highlightCandidates,
	}, nil
}

func normalizePatientStatus(raw string) string {
	text := strings.TrimSpace(raw)
	switch text {
	case "顺利接受", "已接受治疗", "已决定治疗", "已成交", "已预约":
		return "顺利接受"
	case "接受但有疑虑", "倾向治疗", "疗程随访中", "继续观察":
		return "接受但有疑虑"
	case "待作决定", "犹豫中":
		return "待作决定"
	case "倾向拒绝", "倾向不做", "明确拒绝":
		return "倾向拒绝"
	default:
		return ""
	}
}

func extractCoreBlockers(analysis map[string]interface{}) []string {
	raw, ok := analysis["core_blockers"]
	if !ok {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case map[string]interface{}:
			text := strings.TrimSpace(firstNonEmptyText(v["blocker"], v["name"], v["text"]))
			if text != "" {
				out = append(out, text)
			}
		default:
			text := strings.TrimSpace(firstNonEmptyText(v))
			if text != "" {
				out = append(out, text)
			}
		}
	}
	return out
}

func extractWeeklyHighlightEntries(analysis map[string]interface{}) []weeklySummaryHighlightEntry {
	readEntries := func(src map[string]interface{}) []weeklySummaryHighlightEntry {
		if src == nil {
			return nil
		}
		candidates := []interface{}{}
		for _, key := range []string{"best_practices", "highlight_candidates", "highlights"} {
			if raw, exists := src[key]; exists {
				if arr, ok := raw.([]interface{}); ok {
					candidates = append(candidates, arr...)
				}
			}
		}
		out := make([]weeklySummaryHighlightEntry, 0, len(candidates))
		for _, candidate := range candidates {
			m, ok := candidate.(map[string]interface{})
			if !ok {
				continue
			}
			text := strings.TrimSpace(firstNonEmptyText(m["highlight_text"], m["text"], m["summary"], m["detail"]))
			if text == "" {
				continue
			}
			dim := strings.TrimSpace(strings.ToUpper(firstNonEmptyText(m["dimension"], m["stage"], m["category"])))
			out = append(out, weeklySummaryHighlightEntry{
				Dimension: dim,
				Text:      text,
			})
		}
		return out
	}

	result := readEntries(analysis)
	if len(result) > 0 {
		return result
	}
	return readEntries(pickMap(analysis, "analysis_summary"))
}

func formatWeeklyDimension(raw string) string {
	dim := strings.ToUpper(strings.TrimSpace(raw))
	labels := map[string]string{
		"G1": "建立接诊环境",
		"G2": "引出信息",
		"G3": "给予信息",
		"G4": "理解患者视角",
		"G5": "结束接诊",
		"G6": "治疗/预防计划",
	}
	if label, ok := labels[dim]; ok {
		return dim + " " + label
	}
	if dim == "" {
		return "高光片段"
	}
	return dim
}

func resolveWeeklyDisplayScore(analysis map[string]interface{}) float64 {
	candidates := []*float64{
		pickFloat(analysis, "segue_percent"),
		pickFloat(analysis, "segue_score"),
		pickFloat(analysis, "communication_score"),
		pickFloat(pickMap(analysis, "segue"), "overall_score"),
		pickFloat(pickMap(analysis, "segue"), "overall"),
		pickFloat(pickMap(analysis, "analysis_summary"), "segue_percent"),
		pickFloat(pickMap(analysis, "analysis_summary"), "segue_score"),
		pickFloat(pickMap(analysis, "quality_score"), "overall_score"),
	}
	for _, candidate := range candidates {
		if candidate != nil && *candidate > 0 {
			return *candidate
		}
	}
	return 0
}

func weeklyDoctorScoreAverage(rows []weeklySummaryRow) map[string]float64 {
	values := map[string][]float64{}
	for _, row := range rows {
		score := resolveWeeklyDisplayScore(row.Analysis)
		if score <= 0 {
			continue
		}
		values[row.Employee] = append(values[row.Employee], score)
	}
	out := map[string]float64{}
	for doctor, scores := range values {
		var total float64
		for _, score := range scores {
			total += score
		}
		out[doctor] = total / float64(len(scores))
	}
	return out
}

func (s *Service) loadWeeklyHighlightCandidates(ctx context.Context, tenantID int64) ([]WeeklySummaryCandidateItem, int64, error) {
	since := time.Now().AddDate(0, 0, -21)
	rows, err := s.store.pool.Query(ctx, `
		SELECT r.id,
		       COALESCE(r.recorded_at, r.created_at) AS ts,
		       r.employee_id,
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), NULLIF(e.phone, ''), '-') AS employee_name,
		       COALESCE(c.name, '') AS patient_name,
		       COALESCE(r.analysis_result, '{}'::json) AS analysis_result
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE r.tenant_id = $1
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND (
			EXISTS (
				SELECT 1
				FROM inst_employee_roles ier
				WHERE ier.tenant_id = r.tenant_id
				  AND ier.employee_id = r.employee_id
				  AND lower(ier.role_code) = ANY(ARRAY['doctor', 'therapist', 'doctor_assistant'])
			)
			OR NOT EXISTS (
				SELECT 1
				FROM inst_employee_roles ier_any
				WHERE ier_any.tenant_id = r.tenant_id
				  AND ier_any.employee_id = r.employee_id
			)
		  )
		  AND r.id NOT IN (
		      SELECT recording_id
		      FROM recording_best_practices
		      WHERE tenant_id = $1
		  )
		ORDER BY ts DESC
		LIMIT 100
	`, tenantID, since)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query weekly highlight candidates: %w", err)
	}
	defer rows.Close()

	out := make([]WeeklySummaryCandidateItem, 0, 20)
	for rows.Next() {
		var (
			recordingID int64
			ts          time.Time
			employeeID  int64
			employee    string
			patientName string
			analysis    map[string]interface{}
		)
		if scanErr := rows.Scan(&recordingID, &ts, &employeeID, &employee, &patientName, &analysis); scanErr != nil {
			return nil, 0, fmt.Errorf("failed to scan weekly candidate row: %w", scanErr)
		}
		entries := extractWeeklyHighlightEntries(analysis)
		if len(entries) == 0 {
			continue
		}
		entry := entries[0]
		recordedAt := ts.Format("2006-01-02 15:04:05")
		item := WeeklySummaryCandidateItem{
			RecordingID:   recordingID,
			RecordedAt:    &recordedAt,
			EmployeeName:  employee,
			HighlightText: entry.Text,
		}
		if employeeID > 0 {
			eid := employeeID
			item.EmployeeID = &eid
		}
		if strings.TrimSpace(patientName) != "" {
			name := strings.TrimSpace(patientName)
			item.PatientName = &name
		}
		if strings.TrimSpace(entry.Dimension) != "" {
			dim := strings.ToUpper(strings.TrimSpace(entry.Dimension))
			item.SuggestedDimension = &dim
		}
		out = append(out, item)
		if len(out) >= 20 {
			break
		}
	}
	return out, int64(len(out)), nil
}

// GetTeamTrends retrieves team trends for medical recordings.
func (s *Service) GetTeamTrends(ctx context.Context, tenantID int64, dateFrom, dateTo, specialtyGroup string) (*TeamTrendsResponse, error) {
	_ = specialtyGroup // 兼容保留，当前后端暂无专科分组字段，先不做过滤。

	now := time.Now()
	start := now.AddDate(0, 0, -29)
	end := now
	if strings.TrimSpace(dateFrom) != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateFrom), now.Location()); err == nil {
			start = parsed
		}
	}
	if strings.TrimSpace(dateTo) != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateTo), now.Location()); err == nil {
			end = parsed.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		}
	}
	if end.Before(start) {
		start, end = end, start
	}

	rows, err := s.store.pool.Query(ctx, `
		SELECT COALESCE(r.analysis_result, '{}'::json) AS analysis_result,
		       COALESCE(r.recorded_at, r.created_at) AS ts
		FROM recordings r
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) <= $3
		  AND (
			EXISTS (
				SELECT 1
				FROM inst_employee_roles ier
				WHERE ier.tenant_id = r.tenant_id
				  AND ier.employee_id = r.employee_id
				  AND lower(ier.role_code) = ANY(ARRAY['doctor', 'therapist', 'doctor_assistant'])
			)
			OR NOT EXISTS (
				SELECT 1
				FROM inst_employee_roles ier_any
				WHERE ier_any.tenant_id = r.tenant_id
				  AND ier_any.employee_id = r.employee_id
			)
		  )
		ORDER BY ts ASC
	`, tenantID, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query medical team trends rows: %w", err)
	}
	defer rows.Close()

	allRows := make([]medicalTrendRow, 0, 512)
	for rows.Next() {
		var item medicalTrendRow
		if scanErr := rows.Scan(&item.Analysis, &item.TS); scanErr != nil {
			return nil, fmt.Errorf("failed to scan medical trend row: %w", scanErr)
		}
		allRows = append(allRows, item)
	}

	dateSpanDays := int(end.Sub(start).Hours()/24) + 1
	if dateSpanDays < 1 {
		dateSpanDays = 1
	}
	windowDays := 7
	if dateSpanDays < 14 {
		windowDays = maxInt(1, dateSpanDays/2)
	}
	startWindowEnd := start.AddDate(0, 0, windowDays)
	endWindowStart := end.AddDate(0, 0, -(windowDays - 1))

	startRows := make([]medicalTrendRow, 0, len(allRows))
	endRows := make([]medicalTrendRow, 0, len(allRows))
	for _, row := range allRows {
		if !row.TS.Before(start) && row.TS.Before(startWindowEnd) {
			startRows = append(startRows, row)
		}
		if !row.TS.Before(endWindowStart) && !row.TS.After(end) {
			endRows = append(endRows, row)
		}
	}

	dimOrder := []string{"G1", "G2", "G3", "G4", "G5", "G6"}
	dimLabels := map[string]string{
		"G1": "建立接诊环境",
		"G2": "引出信息",
		"G3": "给予信息",
		"G4": "理解患者视角",
		"G5": "结束接诊",
		"G6": "治疗/预防计划",
	}
	startDim := collectDimensionAvg(startRows)
	endDim := collectDimensionAvg(endRows)
	dimensionTrends := make([]TeamTrendDimension, 0, len(dimOrder))
	for _, group := range dimOrder {
		sv := roundFloat(startDim[group], 1)
		ev := roundFloat(endDim[group], 1)
		dimensionTrends = append(dimensionTrends, TeamTrendDimension{
			Group:  group,
			Label:  dimLabels[group],
			Start:  sv,
			End:    ev,
			Change: roundFloat(ev-sv, 1),
		})
	}

	startAccept, startEmotion := collectPatientTrend(startRows)
	endAccept, endEmotion := collectPatientTrend(endRows)

	blockerCounter := map[string]int64{}
	for _, row := range allRows {
		for _, blocker := range extractCoreBlockers(row.Analysis) {
			blockerCounter[blocker]++
		}
	}
	coreBlockers := make([]TeamTrendBlocker, 0, len(blockerCounter))
	for blocker, count := range blockerCounter {
		coreBlockers = append(coreBlockers, TeamTrendBlocker{
			Blocker: blocker,
			Count:   count,
		})
	}
	sort.Slice(coreBlockers, func(i, j int) bool { return coreBlockers[i].Count > coreBlockers[j].Count })
	if len(coreBlockers) > 10 {
		coreBlockers = coreBlockers[:10]
	}

	return &TeamTrendsResponse{
		Period:          "custom",
		DateFrom:        start.Format("2006-01-02"),
		DateTo:          end.Format("2006-01-02"),
		DimensionTrends: dimensionTrends,
		PatientStatusTrend: TeamTrendPatientStatus{
			StartAcceptanceRate:         startAccept,
			EndAcceptanceRate:           endAccept,
			StartEmotionImprovementRate: startEmotion,
			EndEmotionImprovementRate:   endEmotion,
		},
		CoreBlockers: coreBlockers,
	}, nil
}

func collectDimensionAvg(rows []medicalTrendRow) map[string]float64 {
	values := map[string][]float64{
		"G1": {}, "G2": {}, "G3": {}, "G4": {}, "G5": {}, "G6": {},
	}
	for _, row := range rows {
		quality := pickMap(row.Analysis, "quality_score")
		stagesAny, ok := quality["stages"]
		if !ok {
			continue
		}
		stages, ok := stagesAny.([]interface{})
		if !ok {
			continue
		}
		for _, item := range stages {
			stage, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			name := strings.TrimSpace(firstNonEmptyText(stage["group"], stage["code"], stage["key"], stage["name"]))
			code := toDimensionCode(name)
			if code == "" {
				continue
			}
			score, ok := toFloat(stage["score"])
			if !ok {
				continue
			}
			values[code] = append(values[code], score)
		}
	}
	out := map[string]float64{"G1": 0, "G2": 0, "G3": 0, "G4": 0, "G5": 0, "G6": 0}
	for code, arr := range values {
		if len(arr) == 0 {
			continue
		}
		var sum float64
		for _, v := range arr {
			sum += v
		}
		out[code] = sum / float64(len(arr))
	}
	return out
}

func toDimensionCode(raw string) string {
	text := strings.ToUpper(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(text, "G1") || strings.Contains(text, "建立接诊环境"):
		return "G1"
	case strings.HasPrefix(text, "G2") || strings.Contains(text, "引出信息"):
		return "G2"
	case strings.HasPrefix(text, "G3") || strings.Contains(text, "给予信息"):
		return "G3"
	case strings.HasPrefix(text, "G4") || strings.Contains(text, "理解患者视角"):
		return "G4"
	case strings.HasPrefix(text, "G5") || strings.Contains(text, "结束接诊"):
		return "G5"
	case strings.HasPrefix(text, "G6") || strings.Contains(text, "治疗/预防计划"):
		return "G6"
	default:
		return ""
	}
}

func collectPatientTrend(rows []medicalTrendRow) (acceptanceRate float64, emotionImproveRate float64) {
	total := 0
	acceptCount := 0
	emotionTotal := 0
	emotionImprove := 0
	for _, row := range rows {
		statusRaw := firstNonEmptyText(
			pickNestedStatus(row.Analysis, "visit_outcome"),
			pickString(row.Analysis, "visit_outcome_status"),
			pickString(row.Analysis, "visit_outcome"),
			pickNestedStatus(pickMap(row.Analysis, "analysis_summary"), "visit_outcome"),
			pickString(pickMap(row.Analysis, "analysis_summary"), "visit_outcome_status"),
			pickString(pickMap(row.Analysis, "analysis_summary"), "visit_outcome"),
		)
		if normalized := normalizePatientStatus(statusRaw); normalized != "" {
			total++
			if normalized == "顺利接受" {
				acceptCount++
			}
		}

		if startEmotion, endEmotion, ok := readEmotionPair(row.Analysis); ok {
			emotionTotal++
			if isEmotionImproved(startEmotion, endEmotion) {
				emotionImprove++
			}
		}
	}
	if total > 0 {
		acceptanceRate = roundFloat((float64(acceptCount)/float64(total))*100, 1)
	}
	if emotionTotal > 0 {
		emotionImproveRate = roundFloat((float64(emotionImprove)/float64(emotionTotal))*100, 1)
	}
	return acceptanceRate, emotionImproveRate
}

func readEmotionPair(analysis map[string]interface{}) (string, string, bool) {
	candidates := []map[string]interface{}{
		pickMap(analysis, "patient_mindset"),
		pickMap(pickMap(analysis, "analysis_summary"), "patient_mindset"),
	}
	for _, m := range candidates {
		if m == nil {
			continue
		}
		start := strings.TrimSpace(firstNonEmptyText(m["start_emotion"], m["initial_emotion"], m["before"]))
		end := strings.TrimSpace(firstNonEmptyText(m["end_emotion"], m["final_emotion"], m["after"]))
		if start != "" && end != "" {
			return start, end, true
		}
	}
	return "", "", false
}

func isEmotionImproved(start, end string) bool {
	startText := strings.TrimSpace(start)
	endText := strings.TrimSpace(end)
	negativeSet := map[string]struct{}{"焦虑": {}, "迷茫": {}, "抵触": {}, "恐惧": {}, "担忧": {}}
	positiveSet := map[string]struct{}{"缓解": {}, "稳定": {}, "满意": {}, "放心": {}, "积极": {}}
	startNegative := false
	for k := range negativeSet {
		if strings.Contains(startText, k) {
			startNegative = true
			break
		}
	}
	if !startNegative {
		return false
	}
	for k := range positiveSet {
		if strings.Contains(endText, k) {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Service) GetTeamAbility(ctx context.Context, tenantID int64, months int) (*TeamAbilityResponse, error) {
	if months <= 0 {
		months = 3
	}
	if months > 12 {
		months = 12
	}

	now := time.Now()
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	currentPeriodStart := addMonths(currentMonthStart, -(months - 1))
	currentPeriodEnd := addMonths(currentMonthStart, 1)
	prevPeriodStart := addMonths(currentPeriodStart, -months)
	prevPeriodEnd := currentPeriodStart

	rows, err := s.store.pool.Query(ctx, `
		SELECT r.employee_id,
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), NULLIF(e.phone, ''), '未知员工') AS employee_name,
		       r.analysis_result,
		       COALESCE(r.recorded_at, r.created_at) AS ts
		FROM recordings r
		JOIN employees e ON e.id = r.employee_id
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) < $3
		ORDER BY ts ASC
	`, tenantID, prevPeriodStart, currentPeriodEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to query team ability rows: %w", err)
	}
	defer rows.Close()

	allRows := make([]teamAbilityRow, 0, 512)
	for rows.Next() {
		var item teamAbilityRow
		var analysisRaw interface{}
		if scanErr := rows.Scan(&item.EmployeeID, &item.EmployeeName, &analysisRaw, &item.TS); scanErr != nil {
			return nil, fmt.Errorf("failed to scan team ability row: %w", scanErr)
		}
		if decoded, ok := decodeNestedJSONValue(analysisRaw, 0).(map[string]interface{}); ok && decoded != nil {
			item.Analysis = decoded
		} else {
			item.Analysis = map[string]interface{}{}
		}
		allRows = append(allRows, item)
	}

	currentRows := make([]teamAbilityRow, 0, len(allRows))
	previousRows := make([]teamAbilityRow, 0, len(allRows))
	for _, r := range allRows {
		if !r.TS.Before(currentPeriodStart) {
			currentRows = append(currentRows, r)
		} else if !r.TS.Before(prevPeriodStart) && r.TS.Before(prevPeriodEnd) {
			previousRows = append(previousRows, r)
		}
	}

	currentStageAvg := avgStageMap(currentRows)
	previousStageAvg := avgStageMap(previousRows)

	teamRadar := TeamAbilityRadar{
		Current:  make([]TeamAbilityRadarScore, 0, len(stageOrder)),
		Previous: make([]TeamAbilityRadarScore, 0, len(stageOrder)),
		Weakest:  weakestStages(currentStageAvg, 2),
	}
	for _, name := range stageOrder {
		teamRadar.Current = append(teamRadar.Current, TeamAbilityRadarScore{Name: name, Score: currentStageAvg[name]})
		teamRadar.Previous = append(teamRadar.Previous, TeamAbilityRadarScore{Name: name, Score: previousStageAvg[name]})
	}

	employeeGroup := map[int64][]teamAbilityRow{}
	employeeNames := map[int64]string{}
	for _, r := range currentRows {
		employeeGroup[r.EmployeeID] = append(employeeGroup[r.EmployeeID], r)
		employeeNames[r.EmployeeID] = r.EmployeeName
	}
	employeeMatrix := make([]TeamAbilityEmployeeMatrixItem, 0, len(employeeGroup))
	for eid, group := range employeeGroup {
		stageAvg := avgStageMap(group)
		overall := calcOverall(stageAvg)
		employeeMatrix = append(employeeMatrix, TeamAbilityEmployeeMatrixItem{
			EmployeeID:  eid,
			Name:        employeeNames[eid],
			Overall:     overall,
			Stages:      stageAvg,
			SampleCount: int64(len(group)),
		})
	}
	sort.Slice(employeeMatrix, func(i, j int) bool { return employeeMatrix[i].Overall > employeeMatrix[j].Overall })

	growthTrend := make([]TeamAbilityGrowthPoint, 0, months)
	monthPoints := make([]time.Time, 0, months)
	for i := 0; i < months; i++ {
		monthPoints = append(monthPoints, addMonths(currentPeriodStart, i))
	}
	for _, mStart := range monthPoints {
		mEnd := addMonths(mStart, 1)
		monthRows := make([]teamAbilityRow, 0, len(currentRows))
		for _, r := range currentRows {
			if !r.TS.Before(mStart) && r.TS.Before(mEnd) {
				monthRows = append(monthRows, r)
			}
		}
		monthAvg := avgStageMap(monthRows)
		growthTrend = append(growthTrend, TeamAbilityGrowthPoint{
			Month: fmt.Sprintf("%d月", int(mStart.Month())),
			Score: calcOverall(monthAvg),
		})
	}

	prevByEmployee := map[int64][]teamAbilityRow{}
	for _, r := range previousRows {
		prevByEmployee[r.EmployeeID] = append(prevByEmployee[r.EmployeeID], r)
	}

	fastestGrowth := TeamAbilityFastestGrowth{Name: "-", Change: 0}
	mostStable := TeamAbilityMostStable{Name: "-", Variance: 0}

	growthCandidates := make([]TeamAbilityFastestGrowth, 0, len(employeeMatrix))
	stableCandidates := make([]TeamAbilityMostStable, 0, len(employeeMatrix))
	for _, m := range employeeMatrix {
		prevOverall := calcOverall(avgStageMap(prevByEmployee[m.EmployeeID]))
		growthCandidates = append(growthCandidates, TeamAbilityFastestGrowth{
			Name:   m.Name,
			Change: roundFloat(m.Overall-prevOverall, 2),
		})

		monthlyScores := make([]float64, 0, months)
		for _, mStart := range monthPoints {
			mEnd := addMonths(mStart, 1)
			empRows := make([]teamAbilityRow, 0, 32)
			for _, r := range currentRows {
				if r.EmployeeID == m.EmployeeID && !r.TS.Before(mStart) && r.TS.Before(mEnd) {
					empRows = append(empRows, r)
				}
			}
			if len(empRows) == 0 {
				continue
			}
			monthlyScores = append(monthlyScores, calcOverall(avgStageMap(empRows)))
		}
		if len(monthlyScores) > 0 {
			stableCandidates = append(stableCandidates, TeamAbilityMostStable{
				Name:     m.Name,
				Variance: variance(monthlyScores),
			})
		}
	}
	if len(growthCandidates) > 0 {
		sort.Slice(growthCandidates, func(i, j int) bool { return growthCandidates[i].Change > growthCandidates[j].Change })
		fastestGrowth = growthCandidates[0]
	}
	if len(stableCandidates) > 0 {
		sort.Slice(stableCandidates, func(i, j int) bool { return stableCandidates[i].Variance < stableCandidates[j].Variance })
		mostStable = stableCandidates[0]
	}

	return &TeamAbilityResponse{
		TeamRadar:      teamRadar,
		EmployeeMatrix: employeeMatrix,
		GrowthTrend:    growthTrend,
		Highlights: TeamAbilityHighlights{
			FastestGrowth: fastestGrowth,
			MostStable:    mostStable,
		},
	}, nil
}

var stageWeights = map[string]float64{
	"开场建立权威": 0.10,
	"需求探索":   0.20,
	"问题放大":   0.15,
	"专业呈现":   0.15,
	"方案定制":   0.10,
	"异议化解":   0.20,
	"促成与收尾":  0.10,
}

var stageOrder = []string{"开场建立权威", "需求探索", "问题放大", "专业呈现", "方案定制", "异议化解", "促成与收尾"}

type teamAbilityRow struct {
	EmployeeID   int64
	EmployeeName string
	Analysis     map[string]interface{}
	TS           time.Time
}

func extractStageScores(analysisResult map[string]interface{}) map[string]float64 {
	out := map[string]float64{}
	quality := pickMap(analysisResult, "quality_score")
	stagesAny, ok := quality["stages"]
	if !ok {
		return out
	}
	stages, ok := stagesAny.([]interface{})
	if !ok {
		return out
	}
	for _, item := range stages {
		stage, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name := strings.TrimSpace(fmt.Sprintf("%v", stage["name"]))
		if _, exists := stageWeights[name]; !exists {
			continue
		}
		score, ok := toFloat(stage["score"])
		if !ok {
			continue
		}
		out[name] = score
	}
	return out
}

func avgStageMap(rows []teamAbilityRow) map[string]float64 {
	stageValues := map[string][]float64{}
	for _, name := range stageOrder {
		stageValues[name] = []float64{}
	}
	for _, row := range rows {
		stageScores := extractStageScores(row.Analysis)
		for name, score := range stageScores {
			stageValues[name] = append(stageValues[name], score)
		}
	}
	out := map[string]float64{}
	for _, name := range stageOrder {
		values := stageValues[name]
		if len(values) == 0 {
			out[name] = 0
			continue
		}
		var sum float64
		for _, v := range values {
			sum += v
		}
		out[name] = roundFloat(sum/float64(len(values)), 2)
	}
	return out
}

func calcOverall(stageAvg map[string]float64) float64 {
	var weightedSum float64
	var weightTotal float64
	for _, name := range stageOrder {
		score := stageAvg[name]
		if score <= 0 {
			continue
		}
		w := stageWeights[name]
		weightedSum += score * w
		weightTotal += w
	}
	if weightTotal <= 0 {
		return 0
	}
	return roundFloat(weightedSum/weightTotal, 2)
}

func weakestStages(stageAvg map[string]float64, n int) []string {
	type pair struct {
		Name  string
		Score float64
	}
	items := make([]pair, 0, len(stageOrder))
	for _, name := range stageOrder {
		items = append(items, pair{Name: name, Score: stageAvg[name]})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Score < items[j].Score })
	if n > len(items) {
		n = len(items)
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, items[i].Name)
	}
	return out
}

func addMonths(dt time.Time, months int) time.Time {
	shifted := dt.AddDate(0, months, 0)
	return time.Date(shifted.Year(), shifted.Month(), 1, 0, 0, 0, 0, dt.Location())
}

func variance(values []float64) float64 {
	if len(values) == 0 {
		return 999
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	var sq float64
	for _, v := range values {
		d := v - mean
		sq += d * d
	}
	return roundFloat(sq/float64(len(values)), 4)
}

// GetDailyReport retrieves daily report
func (s *Service) GetDailyReport(ctx context.Context, tenantID int64, dateFrom, dateTo, date string) (*DailyReportResponse, error) {
	rangeClause, rangeArgs := buildDashboardDateRangeSQL(dateFrom, dateTo, 2, "")
	rangeClauseR, rangeArgsR := buildDashboardDateRangeSQL(dateFrom, dateTo, 2, "r")
	args := append([]interface{}{tenantID}, rangeArgs...)

	reportDate := strings.TrimSpace(date)
	if reportDate == "" {
		reportDate = time.Now().Format("2006-01-02")
	}
	if strings.TrimSpace(dateTo) != "" {
		reportDate = strings.TrimSpace(dateTo)
	}

	var (
		confirmedCount  int64
		dealCount       int64
		totalAmount     float64
		totalRecordings int64
		activeEmployees int64
	)
	overviewQuery := fmt.Sprintf(`
		SELECT
			COUNT(*) FILTER (WHERE confirmed_deal_status IS NOT NULL) AS confirmed_count,
			COUNT(*) FILTER (WHERE confirmed_deal_status = '成交了') AS deal_count,
			COALESCE(SUM(converted_amount) FILTER (WHERE confirmed_deal_status = '成交了'), 0) AS total_amount,
			COUNT(*) AS total_recordings,
			COUNT(DISTINCT employee_id) AS active_employees
		FROM recordings
		WHERE tenant_id = $1 %s
	`, rangeClause)
	if err := s.store.pool.QueryRow(ctx, overviewQuery, args...).Scan(
		&confirmedCount, &dealCount, &totalAmount, &totalRecordings, &activeEmployees,
	); err != nil {
		return nil, fmt.Errorf("failed to query dashboard overview: %w", err)
	}

	dealRate := safeRate(dealCount, confirmedCount)
	avgDealAmount := 0.0
	if dealCount > 0 {
		avgDealAmount = roundFloat(totalAmount/float64(dealCount), 2)
	}

	// 跟进回收率（30天）
	recoveryQuery := fmt.Sprintf(`
		WITH follow_customers AS (
			SELECT DISTINCT r.customer_id, MIN(rt.created_at) AS first_task_at
			FROM recordings r
			JOIN recording_tasks rt ON rt.recording_id = r.id
			WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
			  AND r.customer_id IS NOT NULL
			  AND r.tenant_id = $1 %s
			GROUP BY r.customer_id
		),
		recovered_customers AS (
			SELECT DISTINCT fc.customer_id
			FROM follow_customers fc
			JOIN recordings r2 ON r2.customer_id = fc.customer_id
			WHERE r2.confirmed_deal_status = '成交了'
			  AND COALESCE(r2.recorded_at, r2.created_at)
			      BETWEEN fc.first_task_at AND fc.first_task_at + INTERVAL '30 days'
		)
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE rc.customer_id IS NOT NULL) AS recovered
		FROM follow_customers fc
		LEFT JOIN recovered_customers rc ON rc.customer_id = fc.customer_id
	`, rangeClauseR)
	var followTotal int64
	var followRecovered int64
	if err := s.store.pool.QueryRow(ctx, recoveryQuery, append([]interface{}{tenantID}, rangeArgsR...)...).Scan(&followTotal, &followRecovered); err != nil {
		return nil, fmt.Errorf("failed to query dashboard recovery: %w", err)
	}
	var recoveryRate *float64
	if followTotal > 0 {
		v := roundFloat(float64(followRecovered)/float64(followTotal)*100, 1)
		recoveryRate = &v
	}

	// 跟进管线（当前）
	var followingCount, active7dCount, overdueCount int64
	pipelineQuery := `
		WITH task_latest AS (
			SELECT recording_id, MAX(updated_at) AS last_task_action_at
			FROM recording_tasks
			WHERE tenant_id = $1
			GROUP BY recording_id
		),
		follow_pipeline AS (
			SELECT r.customer_id,
			       MAX(COALESCE(r.recorded_at, r.created_at)) AS last_recording_at,
			       MAX(tl.last_task_action_at) AS last_task_action_at
			FROM recordings r
			JOIN task_latest tl ON tl.recording_id = r.id
			WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
			  AND r.customer_id IS NOT NULL
			  AND r.tenant_id = $1
			GROUP BY r.customer_id
		)
		SELECT
			COUNT(*) AS following_count,
			COUNT(*) FILTER (
				WHERE GREATEST(last_recording_at, last_task_action_at) >= NOW() - INTERVAL '7 days'
			) AS active_7d_count,
			COUNT(*) FILTER (
				WHERE GREATEST(last_recording_at, last_task_action_at) < NOW() - INTERVAL '7 days'
			) AS overdue_count
		FROM follow_pipeline
	`
	if err := s.store.pool.QueryRow(ctx, pipelineQuery, tenantID).Scan(&followingCount, &active7dCount, &overdueCount); err != nil {
		return nil, fmt.Errorf("failed to query dashboard pipeline: %w", err)
	}

	// 月度目标
	var targetNullable *float64
	targetQuery := `SELECT monthly_revenue_target::float8 FROM tenants WHERE id = $1`
	if err := s.store.pool.QueryRow(ctx, targetQuery, tenantID).Scan(&targetNullable); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed to query dashboard target: %w", err)
	}
	var targetProgress *float64
	if targetNullable != nil && *targetNullable > 0 {
		v := roundFloat(totalAmount/(*targetNullable)*100, 1)
		targetProgress = &v
	}

	// 环比
	var momAmount, momDealRate, momAvgAmount *float64
	if p := buildPrevRange(dateFrom, dateTo); p != nil {
		prevQuery := `
			SELECT
				COUNT(*) FILTER (WHERE confirmed_deal_status IS NOT NULL) AS confirmed,
				COUNT(*) FILTER (WHERE confirmed_deal_status = '成交了') AS deals,
				COALESCE(SUM(converted_amount) FILTER (WHERE confirmed_deal_status = '成交了'), 0)::float8 AS amount
			FROM recordings
			WHERE tenant_id = $1
			  AND COALESCE(recorded_at, created_at) >= $2
			  AND COALESCE(recorded_at, created_at) < $3
		`
		var pvConfirmed, pvDeals int64
		var pvAmount float64
		if err := s.store.pool.QueryRow(ctx, prevQuery, tenantID, p.From, p.ToEx).Scan(&pvConfirmed, &pvDeals, &pvAmount); err != nil {
			return nil, fmt.Errorf("failed to query dashboard mom: %w", err)
		}
		pvRate := safeRate(pvDeals, pvConfirmed)
		pvAvg := 0.0
		if pvDeals > 0 {
			pvAvg = roundFloat(pvAmount/float64(pvDeals), 2)
		}
		a := roundFloat(totalAmount-pvAmount, 2)
		r := roundFloat(dealRate-pvRate, 1)
		av := roundFloat(avgDealAmount-pvAvg, 2)
		momAmount = &a
		momDealRate = &r
		momAvgAmount = &av
	}

	// 员工排行
	empQuery := fmt.Sprintf(`
		WITH task_latest AS (
			SELECT recording_id
			FROM recording_tasks
			WHERE tenant_id = $1
			GROUP BY recording_id
		)
		SELECT
			e.id AS eid,
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未知员工') AS name,
			COUNT(r.id) FILTER (WHERE r.confirmed_deal_status IS NOT NULL) AS consultations,
			COUNT(r.id) FILTER (WHERE r.confirmed_deal_status = '成交了') AS deals,
			COALESCE(SUM(r.converted_amount) FILTER (WHERE r.confirmed_deal_status = '成交了'), 0)::float8 AS amount,
			COUNT(DISTINCT r.customer_id) FILTER (
				WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
				  AND r.customer_id IS NOT NULL
				  AND tl.recording_id IS NOT NULL
			) AS following_count
		FROM recordings r
		JOIN employees e ON e.id = r.employee_id
		LEFT JOIN task_latest tl ON tl.recording_id = r.id
		WHERE r.tenant_id = $1 %s
		GROUP BY e.id, COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未知员工')
		ORDER BY amount DESC
		LIMIT 20
	`, rangeClauseR)
	empRows, err := s.store.pool.Query(ctx, empQuery, append([]interface{}{tenantID}, rangeArgsR...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to query dashboard employee ranking: %w", err)
	}
	defer empRows.Close()

	overdueByEmployee := map[int64]int64{}
	overdueQuery := `
		WITH task_latest AS (
			SELECT recording_id, MAX(updated_at) AS last_task_action_at
			FROM recording_tasks
			WHERE tenant_id = $1
			GROUP BY recording_id
		),
		emp_customer_last AS (
			SELECT r.employee_id, r.customer_id,
			       GREATEST(MAX(COALESCE(r.recorded_at, r.created_at)), MAX(tl.last_task_action_at)) AS last_action_at
			FROM recordings r
			JOIN task_latest tl ON tl.recording_id = r.id
			WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
			  AND r.customer_id IS NOT NULL
			  AND r.tenant_id = $1
			GROUP BY r.employee_id, r.customer_id
		)
		SELECT employee_id, COUNT(*) AS overdue_count
		FROM emp_customer_last
		WHERE last_action_at < NOW() - INTERVAL '7 days'
		GROUP BY employee_id
	`
	overdueRows, err := s.store.pool.Query(ctx, overdueQuery, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query dashboard overdue by employee: %w", err)
	}
	for overdueRows.Next() {
		var eid, cnt int64
		if scanErr := overdueRows.Scan(&eid, &cnt); scanErr != nil {
			overdueRows.Close()
			return nil, fmt.Errorf("failed to scan dashboard overdue by employee: %w", scanErr)
		}
		overdueByEmployee[eid] = cnt
	}
	overdueRows.Close()

	abilityMap, err := s.calcEmployeeAbilityScores(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	employeeRanking := make([]DashboardEmployeeRankingItem, 0, 20)
	topPerformers := make([]DoctorAbilityRankingResponse, 0, 5)
	rank := 1
	for empRows.Next() {
		var (
			eid           int64
			name          string
			consultations int64
			deals         int64
			amount        float64
			following     int64
		)
		if scanErr := empRows.Scan(&eid, &name, &consultations, &deals, &amount, &following); scanErr != nil {
			return nil, fmt.Errorf("failed to scan dashboard employee ranking: %w", scanErr)
		}
		ability := abilityMap[eid]
		item := DashboardEmployeeRankingItem{
			EmployeeID:         eid,
			Name:               name,
			DealAmount:         amount,
			DealRate:           safeRate(deals, consultations),
			Consultations:      consultations,
			AvgDealAmount:      0,
			FollowingCount:     following,
			OverdueCount:       overdueByEmployee[eid],
			AbilityScore:       ability.Score,
			AbilitySampleCount: ability.SampleCount,
		}
		if deals > 0 {
			item.AvgDealAmount = roundFloat(amount/float64(deals), 2)
		}
		employeeRanking = append(employeeRanking, item)
		if len(topPerformers) < 5 {
			topPerformers = append(topPerformers, DoctorAbilityRankingResponse{
				EmployeeID:     eid,
				EmployeeName:   name,
				RecordingCount: consultations,
				AvgScore:       item.DealRate,
				Rank:           rank,
			})
		}
		rank++
	}

	// 趋势
	trendQuery := fmt.Sprintf(`
		SELECT
			DATE(COALESCE(recorded_at, created_at)) AS day,
			COALESCE(SUM(converted_amount) FILTER (WHERE confirmed_deal_status = '成交了'), 0)::float8 AS amount,
			COUNT(*) FILTER (WHERE confirmed_deal_status IS NOT NULL) AS total,
			COUNT(*) FILTER (WHERE confirmed_deal_status = '成交了') AS deals
		FROM recordings
		WHERE tenant_id = $1 %s
		GROUP BY day
		ORDER BY day
	`, rangeClause)
	trendRows, err := s.store.pool.Query(ctx, trendQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query dashboard trend: %w", err)
	}
	amountTrend := make([]DashboardAmountTrendItem, 0, 32)
	for trendRows.Next() {
		var day time.Time
		var amount float64
		var total int64
		var deals int64
		if scanErr := trendRows.Scan(&day, &amount, &total, &deals); scanErr != nil {
			trendRows.Close()
			return nil, fmt.Errorf("failed to scan dashboard trend: %w", scanErr)
		}
		amountTrend = append(amountTrend, DashboardAmountTrendItem{
			Date:     day.Format("2006-01-02"),
			Amount:   amount,
			DealRate: safeRate(deals, total),
		})
	}
	trendRows.Close()

	var insight *string
	if len(employeeRanking) > 0 {
		s := fmt.Sprintf("本期录音 %d 条，成交率 %.1f%%，建议优先关注低转化员工与超期跟进客户。", totalRecordings, dealRate)
		insight = &s
	}

	resp := &DailyReportResponse{
		Date:            reportDate,
		TotalRecordings: totalRecordings,
		TotalDuration:   0,
		AvgScore:        dealRate,
		TopPerformers:   topPerformers,
		KeyMetrics: JSONObject{
			"pending_tasks":            followingCount,
			"failed_tasks":             overdueCount,
			"deal_count":               dealCount,
			"total_amount":             totalAmount,
			"deal_rate":                dealRate,
			"avg_deal_amount":          avgDealAmount,
			"confirmed_count":          confirmedCount,
			"active_employees":         activeEmployees,
			"following_count":          followingCount,
			"active_7d_count":          active7dCount,
			"overdue_count":            overdueCount,
			"historical_recovery_rate": recoveryRate,
			"target":                   targetNullable,
		},
		Revenue: &DashboardRevenue{
			TotalAmount:     totalAmount,
			DealCount:       dealCount,
			DealRate:        dealRate,
			AvgDealAmount:   avgDealAmount,
			ConfirmedCount:  confirmedCount,
			TotalRecordings: totalRecordings,
			ActiveEmployees: activeEmployees,
			Target:          targetNullable,
			TargetProgress:  targetProgress,
			MomAmount:       momAmount,
			MomDealRate:     momDealRate,
			MomAvgAmount:    momAvgAmount,
		},
		Pipeline: &DashboardPipeline{
			FollowingCount:         followingCount,
			Active7DCount:          active7dCount,
			OverdueCount:           overdueCount,
			AvgDealAmount:          avgDealAmount,
			HistoricalRecoveryRate: recoveryRate,
		},
		EmployeeRanking: employeeRanking,
		AmountTrend:     amountTrend,
		Insight:         insight,
	}
	return resp, nil
}

// GetDiagnosis retrieves diagnosis
func (s *Service) GetDiagnosis(ctx context.Context, tenantID int64, dateFrom, dateTo string) (*DiagnosisResponse, error) {
	rangeClause, rangeArgs := buildDashboardDateRangeSQL(dateFrom, dateTo, 2, "")
	rangeClauseR, rangeArgsR := buildDashboardDateRangeSQL(dateFrom, dateTo, 2, "r")
	args := append([]interface{}{tenantID}, rangeArgs...)

	var recCount, analyzed int64
	qualityQuery := fmt.Sprintf(`
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE analysis_status = 'completed') AS analyzed
		FROM recordings
		WHERE tenant_id = $1 %s
	`, rangeClause)
	if err := s.store.pool.QueryRow(ctx, qualityQuery, args...).Scan(&recCount, &analyzed); err != nil {
		return nil, fmt.Errorf("failed to query diagnosis quality: %w", err)
	}
	analysisRate := safeRate(analyzed, recCount)

	var fc, fd, fn int64
	var dealAmount float64
	funnelQuery := fmt.Sprintf(`
		SELECT
			COUNT(*) FILTER (WHERE confirmed_deal_status IS NOT NULL) AS confirmed,
			COUNT(*) FILTER (WHERE confirmed_deal_status = '成交了') AS deal_count,
			COALESCE(SUM(converted_amount) FILTER (WHERE confirmed_deal_status = '成交了'), 0)::float8 AS deal_amount,
			COUNT(*) FILTER (WHERE confirmed_deal_status IN ('没成交','还在跟进')) AS not_closed
		FROM recordings r
		WHERE r.tenant_id = $1 AND r.confirmed_deal_status IS NOT NULL %s
	`, rangeClauseR)
	if err := s.store.pool.QueryRow(ctx, funnelQuery, append([]interface{}{tenantID}, rangeArgsR...)...).Scan(&fc, &fd, &dealAmount, &fn); err != nil {
		return nil, fmt.Errorf("failed to query diagnosis funnel: %w", err)
	}

	var followingCount int64
	followingQuery := fmt.Sprintf(`
		SELECT COUNT(DISTINCT r.customer_id) AS following_count
		FROM recordings r
		JOIN recording_tasks rt ON rt.recording_id = r.id
		WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
		  AND r.customer_id IS NOT NULL
		  AND r.tenant_id = $1 %s
	`, rangeClauseR)
	if err := s.store.pool.QueryRow(ctx, followingQuery, append([]interface{}{tenantID}, rangeArgsR...)...).Scan(&followingCount); err != nil {
		return nil, fmt.Errorf("failed to query diagnosis following count: %w", err)
	}

	rate := func(n, d int64) *float64 {
		if d <= 0 {
			return nil
		}
		v := roundFloat(float64(n)/float64(d)*100, 1)
		return &v
	}
	dealAmountCopy := dealAmount
	funnel := []DashboardFunnelStage{
		{Key: "confirmed", Label: "已确认咨询", Count: fc},
		{Key: "deal", Label: "成交", Count: fd, Amount: &dealAmountCopy, Rate: rate(fd, fc)},
		{Key: "not_closed", Label: "未成交", Count: fn, Rate: rate(fn, fc)},
		{Key: "following", Label: "正在跟进", Count: followingCount, Rate: rate(followingCount, fn)},
	}

	concernQuery := fmt.Sprintf(`
		SELECT COALESCE(not_closed_reason, ai_not_closed_reason) AS reason, COUNT(*) AS cnt
		FROM recordings
		WHERE tenant_id = $1
		  AND COALESCE(not_closed_reason, ai_not_closed_reason) IS NOT NULL
		  AND confirmed_deal_status IN ('没成交','还在跟进') %s
		GROUP BY COALESCE(not_closed_reason, ai_not_closed_reason)
		ORDER BY cnt DESC
	`, rangeClause)
	concernRows, err := s.store.pool.Query(ctx, concernQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query diagnosis concern distribution: %w", err)
	}
	labelMap := map[string]string{
		"price": "价格顾虑", "need_think": "还要考虑", "family": "家人意见",
		"trust": "信任不足", "plan_mismatch": "方案不满意", "timing": "时间安排",
		"competitor": "在比较", "other": "其他",
	}
	type concernTmp struct {
		reason string
		cnt    int64
	}
	tmpConcerns := make([]concernTmp, 0, 16)
	var totalConcern int64
	for concernRows.Next() {
		var reason string
		var cnt int64
		if scanErr := concernRows.Scan(&reason, &cnt); scanErr != nil {
			concernRows.Close()
			return nil, fmt.Errorf("failed to scan concern distribution: %w", scanErr)
		}
		tmpConcerns = append(tmpConcerns, concernTmp{reason: reason, cnt: cnt})
		totalConcern += cnt
	}
	concernRows.Close()
	concernDist := make([]DashboardConcernDistribution, 0, len(tmpConcerns))
	issues := make([]IssueCount, 0, len(tmpConcerns))
	for _, c := range tmpConcerns {
		pct := 0.0
		if totalConcern > 0 {
			pct = roundFloat(float64(c.cnt)/float64(totalConcern)*100, 1)
		}
		label := c.reason
		if mapped, ok := labelMap[c.reason]; ok {
			label = mapped
		}
		concernDist = append(concernDist, DashboardConcernDistribution{
			Reason:     c.reason,
			Label:      label,
			Count:      c.cnt,
			Percentage: pct,
		})
		issues = append(issues, IssueCount{Issue: label, Count: c.cnt})
	}

	empQuery := fmt.Sprintf(`
		WITH task_latest AS (
			SELECT recording_id
			FROM recording_tasks
			WHERE tenant_id = $1
			GROUP BY recording_id
		)
		SELECT
			e.id AS eid,
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未知员工') AS name,
			COUNT(r.id) FILTER (WHERE r.confirmed_deal_status IS NOT NULL) AS consultations,
			COUNT(r.id) FILTER (WHERE r.confirmed_deal_status = '成交了') AS deal_count,
			COALESCE(SUM(r.converted_amount) FILTER (WHERE r.confirmed_deal_status = '成交了'), 0)::float8 AS deal_amount,
			COUNT(DISTINCT r.customer_id) FILTER (
				WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
				  AND r.customer_id IS NOT NULL
				  AND tl.recording_id IS NOT NULL
			) AS following_count
		FROM recordings r
		JOIN employees e ON e.id = r.employee_id
		LEFT JOIN task_latest tl ON tl.recording_id = r.id
		WHERE r.tenant_id = $1 %s
		GROUP BY e.id, COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未知员工')
		ORDER BY deal_amount DESC
		LIMIT 20
	`, rangeClauseR)
	empRows, err := s.store.pool.Query(ctx, empQuery, append([]interface{}{tenantID}, rangeArgsR...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to query diagnosis employee details: %w", err)
	}
	defer empRows.Close()

	overdueByEmployee := map[int64]int64{}
	overdueQuery := `
		SELECT employee_id, COUNT(*) AS overdue_count
		FROM (
			SELECT r.employee_id, r.customer_id
			FROM recordings r
			JOIN recording_tasks rt ON rt.recording_id = r.id
			WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
			  AND r.customer_id IS NOT NULL
			  AND r.tenant_id = $1
			GROUP BY r.employee_id, r.customer_id
			HAVING GREATEST(MAX(COALESCE(r.recorded_at, r.created_at)), MAX(rt.updated_at)) < NOW() - INTERVAL '7 days'
		) sub
		GROUP BY employee_id
	`
	overdueRows, err := s.store.pool.Query(ctx, overdueQuery, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query diagnosis overdue employee: %w", err)
	}
	for overdueRows.Next() {
		var eid, cnt int64
		if scanErr := overdueRows.Scan(&eid, &cnt); scanErr != nil {
			overdueRows.Close()
			return nil, fmt.Errorf("failed to scan diagnosis overdue employee: %w", scanErr)
		}
		overdueByEmployee[eid] = cnt
	}
	overdueRows.Close()

	employeeDiagnosis := make([]DashboardEmployeeDiagnosisRow, 0, 20)
	for empRows.Next() {
		var eid int64
		var name string
		var consultations, dealCount, following int64
		var dealAmountRow float64
		if scanErr := empRows.Scan(&eid, &name, &consultations, &dealCount, &dealAmountRow, &following); scanErr != nil {
			return nil, fmt.Errorf("failed to scan diagnosis employee detail: %w", scanErr)
		}
		avg := 0.0
		if dealCount > 0 {
			avg = roundFloat(dealAmountRow/float64(dealCount), 2)
		}
		employeeDiagnosis = append(employeeDiagnosis, DashboardEmployeeDiagnosisRow{
			EmployeeID:     eid,
			Name:           name,
			Consultations:  consultations,
			DealCount:      dealCount,
			DealRate:       safeRate(dealCount, consultations),
			DealAmount:     dealAmountRow,
			AvgDealAmount:  avg,
			FollowingCount: following,
			OverdueCount:   overdueByEmployee[eid],
		})
	}

	// 兼容旧字段
	health := "poor"
	switch {
	case analysisRate >= 90:
		health = "excellent"
	case analysisRate >= 75:
		health = "good"
	case analysisRate >= 60:
		health = "fair"
	}
	trends := make([]TrendDataPoint, 0, 30)
	for _, item := range employeeDiagnosis {
		_ = item
	}
	trendRows, err := s.store.pool.Query(ctx, `
		SELECT DATE(COALESCE(created_at, recorded_at)) AS dt,
		       COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE analysis_status='completed') AS completed
		FROM recordings
		WHERE tenant_id = $1
		  AND COALESCE(created_at, recorded_at) >= NOW() - INTERVAL '30 days'
		GROUP BY dt
		ORDER BY dt ASC
	`, tenantID)
	if err == nil {
		for trendRows.Next() {
			var dt time.Time
			var total, completed int64
			if scanErr := trendRows.Scan(&dt, &total, &completed); scanErr == nil {
				trends = append(trends, TrendDataPoint{
					Date:           dt.Format("2006-01-02"),
					AvgScore:       safeRate(completed, total),
					RecordingCount: total,
				})
			}
		}
		trendRows.Close()
	}

	return &DiagnosisResponse{
		OverallHealth:   health,
		Issues:          issues,
		Recommendations: []string{"优先处理超7天未跟进客户", "重点复盘未成交原因Top项"},
		Trends:          trends,
		DataQuality: &DashboardDataQuality{
			RecordingCount:      recCount,
			AnalysisSuccessRate: analysisRate,
		},
		Funnel:          funnel,
		ConcernDist:     concernDist,
		EmployeeDetails: employeeDiagnosis,
	}, nil
}

func (s *Service) GetDashboardFunnelDetail(ctx context.Context, tenantID int64, dateFrom, dateTo, stageKey string) (*DashboardFunnelDetailResponse, error) {
	stageLabelMap := map[string]string{
		"deal":       "成交",
		"not_closed": "未成交",
		"following":  "正在跟进",
	}
	stageConditionMap := map[string]string{
		"deal":       "r.confirmed_deal_status = '成交了'",
		"not_closed": "r.confirmed_deal_status IN ('没成交','还在跟进')",
		"following":  "r.confirmed_deal_status IN ('没成交','还在跟进') AND EXISTS (SELECT 1 FROM recording_tasks rt WHERE rt.recording_id = r.id)",
	}
	cond, ok := stageConditionMap[stageKey]
	if !ok {
		return nil, fmt.Errorf("invalid stage_key")
	}

	rangeClauseR, rangeArgsR := buildDashboardDateRangeSQL(dateFrom, dateTo, 2, "r")
	query := fmt.Sprintf(`
		SELECT e.id AS eid, COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name,''), '未知员工') AS name, COUNT(DISTINCT r.id) AS cnt
		FROM recordings r
		JOIN employees e ON e.id = r.employee_id
		WHERE r.tenant_id = $1
		  AND %s %s
		GROUP BY e.id, COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name,''), '未知员工')
		ORDER BY cnt DESC
	`, cond, rangeClauseR)
	rows, err := s.store.pool.Query(ctx, query, append([]interface{}{tenantID}, rangeArgsR...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to query funnel detail: %w", err)
	}
	defer rows.Close()

	overdueByEmployee := map[int64]int64{}
	if stageKey == "following" {
		rangeClause, rangeArgs := buildDashboardDateRangeSQL(dateFrom, dateTo, 2, "r")
		overdueQuery := fmt.Sprintf(`
			SELECT employee_id, COUNT(*) AS overdue_count
			FROM (
				SELECT r.employee_id, r.customer_id
				FROM recordings r
				JOIN recording_tasks rt ON rt.recording_id = r.id
				WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
				  AND r.customer_id IS NOT NULL
				  AND r.tenant_id = $1 %s
				GROUP BY r.employee_id, r.customer_id
				HAVING GREATEST(MAX(COALESCE(r.recorded_at, r.created_at)), MAX(rt.updated_at)) < NOW() - INTERVAL '7 days'
			) sub
			GROUP BY employee_id
		`, rangeClause)
		overdueRows, qerr := s.store.pool.Query(ctx, overdueQuery, append([]interface{}{tenantID}, rangeArgs...)...)
		if qerr == nil {
			for overdueRows.Next() {
				var eid, cnt int64
				if scanErr := overdueRows.Scan(&eid, &cnt); scanErr == nil {
					overdueByEmployee[eid] = cnt
				}
			}
			overdueRows.Close()
		}
	}

	byEmployee := make([]DashboardFunnelDetailByEmployee, 0, 32)
	var totalCount int64
	for rows.Next() {
		var eid int64
		var name string
		var cnt int64
		if scanErr := rows.Scan(&eid, &name, &cnt); scanErr != nil {
			return nil, fmt.Errorf("failed to scan funnel detail: %w", scanErr)
		}
		byEmployee = append(byEmployee, DashboardFunnelDetailByEmployee{
			EmployeeID:   eid,
			Name:         name,
			Count:        cnt,
			OverdueCount: overdueByEmployee[eid],
		})
		totalCount += cnt
	}

	return &DashboardFunnelDetailResponse{
		StageKey:   stageKey,
		StageLabel: stageLabelMap[stageKey],
		TotalCount: totalCount,
		ByEmployee: byEmployee,
	}, nil
}

type abilityScoreAggregate struct {
	Score       *float64
	SampleCount int64
}

func (s *Service) calcEmployeeAbilityScores(ctx context.Context, tenantID int64) (map[int64]abilityScoreAggregate, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT employee_id, analysis_result
		FROM recordings
		WHERE tenant_id = $1
		  AND analysis_status = 'completed'
		  AND analysis_result IS NOT NULL
		  AND COALESCE(recorded_at, created_at) >= NOW() - INTERVAL '30 days'
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query ability scores: %w", err)
	}
	defer rows.Close()

	weights := map[string]float64{
		"开场建立权威": 0.10,
		"需求探索":   0.20,
		"问题放大":   0.15,
		"专业呈现":   0.15,
		"方案定制":   0.10,
		"异议化解":   0.20,
		"促成与收尾":  0.10,
	}
	stageByEmployee := map[int64]map[string][]float64{}
	sampleByEmployee := map[int64]int64{}

	for rows.Next() {
		var employeeID int64
		var resultData JSONObject
		if scanErr := rows.Scan(&employeeID, &resultData); scanErr != nil {
			return nil, fmt.Errorf("failed to scan ability score row: %w", scanErr)
		}
		qualityScore := pickMap(resultData, "quality_score")
		stagesRaw := qualityScore["stages"]
		stageList, ok := stagesRaw.([]interface{})
		if !ok || len(stageList) == 0 {
			continue
		}
		hasValid := false
		for _, stageAny := range stageList {
			stageMap, ok := stageAny.(map[string]interface{})
			if !ok {
				continue
			}
			stageName := strings.TrimSpace(fmt.Sprintf("%v", stageMap["name"]))
			if _, exists := weights[stageName]; !exists {
				continue
			}
			score, ok := toFloat(stageMap["score"])
			if !ok {
				continue
			}
			if _, exists := stageByEmployee[employeeID]; !exists {
				stageByEmployee[employeeID] = map[string][]float64{}
			}
			stageByEmployee[employeeID][stageName] = append(stageByEmployee[employeeID][stageName], score)
			hasValid = true
		}
		if hasValid {
			sampleByEmployee[employeeID]++
		}
	}

	out := map[int64]abilityScoreAggregate{}
	for employeeID, stageMap := range stageByEmployee {
		var weightedSum float64
		var weightTotal float64
		for stageName, values := range stageMap {
			if len(values) == 0 {
				continue
			}
			var sum float64
			for _, v := range values {
				sum += v
			}
			avg := sum / float64(len(values))
			w := weights[stageName]
			weightedSum += avg * w
			weightTotal += w
		}
		var scorePtr *float64
		if weightTotal > 0 {
			score := roundFloat(weightedSum/weightTotal, 2)
			scorePtr = &score
		}
		out[employeeID] = abilityScoreAggregate{
			Score:       scorePtr,
			SampleCount: sampleByEmployee[employeeID],
		}
	}
	return out, nil
}

type dashboardPrevRange struct {
	From string
	ToEx string
}

func buildPrevRange(dateFrom, dateTo string) *dashboardPrevRange {
	if strings.TrimSpace(dateFrom) == "" || strings.TrimSpace(dateTo) == "" {
		return nil
	}
	start, err := time.Parse("2006-01-02", dateFrom)
	if err != nil {
		return nil
	}
	end, err := time.Parse("2006-01-02", dateTo)
	if err != nil {
		return nil
	}
	delta := end.Sub(start) + 24*time.Hour
	prevFrom := start.Add(-delta)
	prevTo := end.Add(-delta).Add(24 * time.Hour)
	return &dashboardPrevRange{
		From: prevFrom.Format("2006-01-02"),
		ToEx: prevTo.Format("2006-01-02"),
	}
}

func buildDashboardDateRangeSQL(dateFrom, dateTo string, startArg int, alias string) (string, []interface{}) {
	col := "COALESCE(recorded_at, created_at)"
	if strings.TrimSpace(alias) != "" {
		col = fmt.Sprintf("COALESCE(%s.recorded_at, %s.created_at)", alias, alias)
	}
	parts := make([]string, 0, 2)
	args := make([]interface{}, 0, 2)
	arg := startArg
	if strings.TrimSpace(dateFrom) != "" {
		parts = append(parts, fmt.Sprintf("%s >= $%d", col, arg))
		args = append(args, strings.TrimSpace(dateFrom))
		arg++
	}
	if strings.TrimSpace(dateTo) != "" {
		parts = append(parts, fmt.Sprintf("%s < $%d", col, arg))
		if dt, err := time.Parse("2006-01-02", strings.TrimSpace(dateTo)); err == nil {
			args = append(args, dt.Add(24*time.Hour).Format("2006-01-02"))
		} else {
			args = append(args, strings.TrimSpace(dateTo))
		}
		arg++
	}
	if len(parts) == 0 {
		return "", args
	}
	return " AND " + strings.Join(parts, " AND "), args
}

func roundFloat(v float64, precision int) float64 {
	if precision < 0 {
		return v
	}
	p := math.Pow(10, float64(precision))
	return math.Round(v*p) / p
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case json.Number:
		parsed, err := n.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func safeRate(numerator, denominator int64) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}

func strOrDefault(v *string, def string) string {
	if v == nil {
		return def
	}
	return *v
}

// Helper functions

// toTaskResponse converts a RecordingTask to TaskResponse
func toTaskResponse(t *RecordingTask) *TaskResponse {
	resp := &TaskResponse{
		ID:            t.ID,
		TenantID:      t.TenantID,
		RecordingID:   t.RecordingID,
		TaskType:      string(t.TaskType),
		Title:         t.Title,
		Description:   t.Description,
		CustomerName:  t.CustomerName,
		Priority:      t.Priority,
		Script:        t.Script,
		ContactReason: t.ContactReason,
		SourceType:    t.SourceType,
		SourceDetail:  t.SourceDetail,
		AssignedTo:    t.AssignedTo,
		AssignedBy:    t.AssignedBy,
		Status:        string(t.Status),
		CompletedBy:   t.CompletedBy,
		CancelReason:  t.CancelReason,
		CreatedAt:     t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if t.DueDate != nil {
		formatted := t.DueDate.Format("2006-01-02T15:04:05Z07:00")
		resp.DueDate = &formatted
		resp.DueAt = &formatted
	}

	if t.CompletedAt != nil {
		formatted := t.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.CompletedAt = &formatted
	}

	if t.CancelledAt != nil {
		formatted := t.CancelledAt.Format("2006-01-02T15:04:05Z07:00")
		resp.CancelledAt = &formatted
	}
	if resp.SourceType != nil {
		if label := sourceTypeLabel(*resp.SourceType); label != "" {
			resp.SourceTypeLabel = &label
		}
	}
	if resp.SourceDetail != nil {
		if label := sourceDetailLabel(*resp.SourceDetail); label != "" {
			resp.SourceDetailLabel = &label
		}
	}

	return resp
}

func compactTaskListItem(item *TaskResponse) {
	if item == nil {
		return
	}
	item.Description = nil
	item.Script = nil
	item.ContactReason = nil
	item.SourceDetail = nil
	item.SourceDetailLabel = nil
}

func (s *Service) enrichTaskListResponse(ctx context.Context, items []*TaskResponse) error {
	if len(items) == 0 {
		return nil
	}
	assignedIDs := make([]int64, 0, len(items))
	recordingIDs := make([]int64, 0, len(items))
	assignedSeen := map[int64]struct{}{}
	recordingSeen := map[int64]struct{}{}
	for _, item := range items {
		if item == nil {
			continue
		}
		if item.AssignedTo != nil && *item.AssignedTo > 0 {
			if _, ok := assignedSeen[*item.AssignedTo]; !ok {
				assignedSeen[*item.AssignedTo] = struct{}{}
				assignedIDs = append(assignedIDs, *item.AssignedTo)
			}
		}
		if item.RecordingID > 0 {
			if _, ok := recordingSeen[item.RecordingID]; !ok {
				recordingSeen[item.RecordingID] = struct{}{}
				recordingIDs = append(recordingIDs, item.RecordingID)
			}
		}
	}

	assignedNameMap := map[int64]string{}
	if len(assignedIDs) > 0 {
		rows, err := s.store.pool.Query(ctx, `
			SELECT id, COALESCE(name, '')
			FROM employees
			WHERE id = ANY($1)
		`, assignedIDs)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				return err
			}
			assignedNameMap[id] = strings.TrimSpace(name)
		}
	}

	type recordingMeta struct {
		OwnerName    string
		RoleCategory string
	}
	recordingMetaMap := map[int64]recordingMeta{}
	if len(recordingIDs) > 0 {
		rows, err := s.store.pool.Query(ctx, `
			SELECT
				r.id AS recording_id,
				COALESCE(NULLIF(e.full_name, ''), COALESCE(e.name, '')) AS owner_name,
				CASE
					WHEN EXISTS (
						SELECT 1 FROM inst_employee_roles ier
						WHERE ier.tenant_id = r.tenant_id
						  AND ier.employee_id = r.employee_id
						  AND lower(ier.role_code) = ANY($2)
					) THEN 'doctor'
					WHEN EXISTS (
						SELECT 1 FROM inst_employee_roles ier
						WHERE ier.tenant_id = r.tenant_id
						  AND ier.employee_id = r.employee_id
						  AND lower(ier.role_code) = ANY($3)
					) THEN 'consultant'
					ELSE 'other'
				END AS role_category
			FROM recordings r
			LEFT JOIN employees e ON e.id = r.employee_id
			WHERE r.id = ANY($1)
		`, recordingIDs, []string{"doctor", "therapist", "doctor_assistant"}, []string{"consultant"})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var recordingID int64
			var ownerName string
			var roleCategory string
			if err := rows.Scan(&recordingID, &ownerName, &roleCategory); err != nil {
				return err
			}
			recordingMetaMap[recordingID] = recordingMeta{
				OwnerName:    strings.TrimSpace(ownerName),
				RoleCategory: strings.TrimSpace(roleCategory),
			}
		}
	}

	for _, item := range items {
		if item == nil {
			continue
		}
		if item.AssignedTo != nil {
			if name := strings.TrimSpace(assignedNameMap[*item.AssignedTo]); name != "" {
				item.AssignedToName = &name
			}
		}
		if meta, ok := recordingMetaMap[item.RecordingID]; ok {
			if meta.OwnerName != "" {
				item.RecordingOwnerName = &meta.OwnerName
			}
			if meta.RoleCategory != "" {
				category := meta.RoleCategory
				label := recordingRoleLabel(category)
				item.RecordingRoleCategory = &category
				item.RecordingRoleLabel = &label
			}
		}
	}
	return nil
}

func recordingRoleLabel(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "consultant":
		return "咨询师录音"
	case "customer_service":
		return "客服录音"
	case "doctor":
		return "医生录音"
	case "therapist":
		return "康复师录音"
	case "doctor_assistant":
		return "医助录音"
	default:
		return "其他录音"
	}
}

func sourceTypeLabel(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "ai":
		return "AI任务"
	case "follow_up":
		return "跟进任务"
	case "manual":
		return "手动任务"
	default:
		return ""
	}
}

func sourceDetailLabel(sourceDetail string) string {
	switch strings.ToLower(strings.TrimSpace(sourceDetail)) {
	case "phone":
		return "电话"
	case "wechat":
		return "微信"
	case "sms":
		return "短信"
	default:
		return ""
	}
}

// Front-desk analysis methods

// ListShiftAnalyses lists shift analyses for a tenant
func (s *Service) ListShiftAnalyses(ctx context.Context, tenantID int64, page, pageSize int, employeeID *int64, dateStr string) ([]map[string]interface{}, int64, error) {
	return s.store.ListShiftAnalyses(ctx, tenantID, page, pageSize, employeeID, dateStr)
}

// GetShiftAnalysis gets a single shift analysis
func (s *Service) GetShiftAnalysis(ctx context.Context, tenantID, id int64) (map[string]interface{}, error) {
	return s.store.GetShiftAnalysis(ctx, tenantID, id)
}

// CreateShiftAnalysis creates a new shift analysis
func (s *Service) CreateShiftAnalysis(ctx context.Context, tenantID int64, req *ShiftAnalysisCreateReq) (map[string]interface{}, error) {
	return s.store.CreateShiftAnalysis(ctx, tenantID, req)
}

// ListDailyReports lists daily reports for a tenant
func (s *Service) ListDailyReports(ctx context.Context, tenantID int64, page, pageSize int) ([]map[string]interface{}, int64, error) {
	return s.store.ListDailyReports(ctx, tenantID, page, pageSize)
}

// GetFrontdeskDailyReport gets a frontdesk daily report by date
func (s *Service) GetFrontdeskDailyReport(ctx context.Context, tenantID int64, dateStr string) (map[string]interface{}, error) {
	return s.store.GetFrontdeskDailyReport(ctx, tenantID, dateStr)
}

// ListKnowledgeBases lists knowledge bases for a tenant
func (s *Service) ListKnowledgeBases(ctx context.Context, tenantID int64, kbType string) ([]map[string]interface{}, error) {
	return s.store.ListKnowledgeBases(ctx, tenantID, kbType)
}

// GetKnowledgeBase gets a knowledge base by type
func (s *Service) GetKnowledgeBase(ctx context.Context, tenantID int64, kbType string) (map[string]interface{}, error) {
	return s.store.GetKnowledgeBase(ctx, tenantID, kbType)
}

// CreateKnowledgeBase creates a new knowledge base
func (s *Service) CreateKnowledgeBase(ctx context.Context, tenantID int64, kbType string, content map[string]interface{}) (map[string]interface{}, error) {
	return s.store.CreateKnowledgeBase(ctx, tenantID, kbType, content)
}

// UpdateKnowledgeBase updates a knowledge base
func (s *Service) UpdateKnowledgeBase(ctx context.Context, tenantID int64, kbType string, content map[string]interface{}) (map[string]interface{}, error) {
	return s.store.UpdateKnowledgeBase(ctx, tenantID, kbType, content)
}

// ListWeeklyReports lists weekly reports for a tenant
func (s *Service) ListWeeklyReports(ctx context.Context, tenantID int64, page, pageSize int) ([]map[string]interface{}, int64, error) {
	return s.store.ListWeeklyReports(ctx, tenantID, page, pageSize)
}

// GetWeeklyReport gets a weekly report by date
func (s *Service) GetWeeklyReport(ctx context.Context, tenantID int64, dateStr string) (map[string]interface{}, error) {
	return s.store.GetWeeklyReport(ctx, tenantID, dateStr)
}

// GenerateWeeklyReport generates a weekly report
func (s *Service) GenerateWeeklyReport(ctx context.Context, tenantID int64, dateStr string) (map[string]interface{}, error) {
	weekEndDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid date format: %w", err)
	}

	weeklyService := NewWeeklyReportService(s.store, "", "", "", "", "", slog.Default())
	reportData, err := weeklyService.GenerateWeeklyReport(ctx, tenantID, weekEndDate)
	if err != nil {
		return nil, fmt.Errorf("generate weekly report data: %w", err)
	}

	analysis := map[string]interface{}{
		"summary":                 buildFrontdeskWeeklySummary(reportData),
		"total_interactions":      reportData.TotalInteractions,
		"total_appointments":      reportData.TotalAppointments,
		"total_walk_ins":          reportData.TotalWalkIns,
		"risk_event_count":        reportData.RiskEventCount,
		"top_questions":           reportData.TopQuestions,
		"competitor_mentions":     reportData.CompetitorMentions,
		"doctor_inquiries":        reportData.DoctorInquiries,
		"channel_feedback":        reportData.ChannelFeedback,
		"key_insights":            reportData.KeyInsights,
		"key_changes":             reportData.KeyInsights,
		"next_actions":            reportData.ImprovementSuggestions,
		"priority_actions":        reportData.ImprovementSuggestions,
		"question_structure":      reportData.TopQuestions,
		"response_quality":        buildWeeklyResponseQuality(reportData),
		"issue_summary":           reportData.IssueSummary,
		"staff_highlights":        buildWeeklyStaffHighlights(reportData),
		"staff_variances":         buildWeeklyStaffVariances(reportData),
		"staff_suggestions":       buildWeeklyStaffSuggestions(reportData),
		"improvement_suggestions": reportData.ImprovementSuggestions,
		"trend_analysis":          reportData.TrendAnalysis,
		"evidence_count":          len(reportData.TopQuestions),
		"evidence_items":          buildWeeklyEvidenceItems(reportData.TopQuestions),
		"action_owners":           buildWeeklyActionOwners(reportData),
		"action_effects":          buildWeeklyActionEffects(reportData),
		"week_start_date":         reportData.WeekStartDate,
		"week_end_date":           reportData.WeekEndDate,
		"generated_at":            reportData.GeneratedAt.Format(time.RFC3339),
		"status":                  "draft",
	}

	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("marshal weekly analysis: %w", err)
	}
	topQuestionsJSON, err := json.Marshal(reportData.TopQuestions)
	if err != nil {
		return nil, fmt.Errorf("marshal top_questions: %w", err)
	}
	competitorMentionsJSON, err := json.Marshal(reportData.CompetitorMentions)
	if err != nil {
		return nil, fmt.Errorf("marshal competitor_mentions: %w", err)
	}
	doctorInquiriesJSON, err := json.Marshal(reportData.DoctorInquiries)
	if err != nil {
		return nil, fmt.Errorf("marshal doctor_inquiries: %w", err)
	}
	channelFeedbackJSON, err := json.Marshal(reportData.ChannelFeedback)
	if err != nil {
		return nil, fmt.Errorf("marshal channel_feedback: %w", err)
	}

	if _, err := s.store.pool.Exec(ctx, `
		INSERT INTO frontdesk_daily_reports (
			tenant_id, report_date,
			total_estimated_interactions, estimated_appointment_count, estimated_walkin_count,
			top_questions, competitor_mentions, doctor_inquiries, channel_feedback, risk_event_count, testimonial_materials, created_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
		ON CONFLICT (tenant_id, report_date) DO UPDATE
		SET total_estimated_interactions = EXCLUDED.total_estimated_interactions,
		    estimated_appointment_count = EXCLUDED.estimated_appointment_count,
		    estimated_walkin_count = EXCLUDED.estimated_walkin_count,
		    top_questions = EXCLUDED.top_questions,
		    competitor_mentions = EXCLUDED.competitor_mentions,
		    doctor_inquiries = EXCLUDED.doctor_inquiries,
		    channel_feedback = EXCLUDED.channel_feedback,
		    risk_event_count = EXCLUDED.risk_event_count,
		    testimonial_materials = EXCLUDED.testimonial_materials
	`,
		tenantID, dateStr,
		reportData.TotalInteractions, reportData.TotalAppointments, reportData.TotalWalkIns,
		topQuestionsJSON, competitorMentionsJSON, doctorInquiriesJSON, channelFeedbackJSON, reportData.RiskEventCount, analysisJSON,
	); err != nil {
		return nil, fmt.Errorf("save weekly report: %w", err)
	}

	return s.store.GetWeeklyReport(ctx, tenantID, dateStr)
}

func (s *Service) PublishWeeklyReport(ctx context.Context, tenantID int64, dateStr, managerComment, publisherName string) (map[string]interface{}, error) {
	report, err := s.store.GetWeeklyReport(ctx, tenantID, dateStr)
	if err != nil {
		return nil, err
	}
	analysis, _ := report["analysis"].(map[string]interface{})
	if analysis == nil {
		analysis = map[string]interface{}{}
	}
	analysis["status"] = "published"
	analysis["manager_comment"] = strings.TrimSpace(managerComment)
	analysis["published_at"] = time.Now().Format(time.RFC3339)
	if strings.TrimSpace(publisherName) != "" {
		analysis["published_by_name"] = strings.TrimSpace(publisherName)
	}

	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("marshal published analysis: %w", err)
	}
	if _, err := s.store.pool.Exec(ctx, `
		UPDATE frontdesk_daily_reports
		SET testimonial_materials = $3
		WHERE tenant_id = $1 AND report_date = $2
	`, tenantID, dateStr, analysisJSON); err != nil {
		return nil, fmt.Errorf("update weekly report publish status: %w", err)
	}

	return s.store.GetWeeklyReport(ctx, tenantID, dateStr)
}

func (s *Service) AddWeeklyReportEvidence(ctx context.Context, tenantID int64, reportDate string, recordingID int64, shiftDate, summary string) (map[string]interface{}, error) {
	targetDate := strings.TrimSpace(reportDate)
	if targetDate == "" {
		targetDate = strings.TrimSpace(shiftDate)
	}
	if targetDate == "" {
		targetDate = time.Now().Format("2006-01-02")
	}

	report, err := s.store.GetWeeklyReport(ctx, tenantID, targetDate)
	if err != nil {
		if _, genErr := s.GenerateWeeklyReport(ctx, tenantID, targetDate); genErr != nil {
			return nil, fmt.Errorf("prepare weekly report: %w", genErr)
		}
		report, err = s.store.GetWeeklyReport(ctx, tenantID, targetDate)
		if err != nil {
			return nil, err
		}
	}

	analysis, _ := report["analysis"].(map[string]interface{})
	if analysis == nil {
		analysis = map[string]interface{}{}
	}

	items := make([]interface{}, 0, 8)
	if raw, ok := analysis["evidence_items"].([]interface{}); ok {
		items = append(items, raw...)
	}
	items = append(items, map[string]interface{}{
		"recording_id": recordingID,
		"shift_date":   strings.TrimSpace(shiftDate),
		"summary":      strings.TrimSpace(summary),
		"added_at":     time.Now().Format(time.RFC3339),
	})
	analysis["evidence_items"] = items
	analysis["evidence_count"] = len(items)

	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("marshal evidence analysis: %w", err)
	}
	if _, err := s.store.pool.Exec(ctx, `
		UPDATE frontdesk_daily_reports
		SET testimonial_materials = $3
		WHERE tenant_id = $1 AND report_date = $2
	`, tenantID, targetDate, analysisJSON); err != nil {
		return nil, fmt.Errorf("update weekly report evidence: %w", err)
	}
	return s.store.GetWeeklyReport(ctx, tenantID, targetDate)
}

func buildFrontdeskWeeklySummary(data *WeeklyReportData) string {
	if data == nil {
		return "本周暂无可用数据，建议优先补齐录音分析样本。"
	}
	return fmt.Sprintf(
		"本周累计交互 %d 次（预约 %d / walk-in %d），风险事件 %d 起，建议围绕高频问题与风险场景优先优化应答。",
		data.TotalInteractions,
		data.TotalAppointments,
		data.TotalWalkIns,
		data.RiskEventCount,
	)
}

func buildWeeklyResponseQuality(data *WeeklyReportData) []string {
	if data == nil {
		return []string{}
	}
	out := make([]string, 0, 3)
	if data.TotalInteractions > 0 {
		appointmentRate := float64(data.TotalAppointments) / float64(data.TotalInteractions) * 100
		walkInRate := float64(data.TotalWalkIns) / float64(data.TotalInteractions) * 100
		out = append(out, fmt.Sprintf("预约承接占比 %.1f%%（%d/%d）", appointmentRate, data.TotalAppointments, data.TotalInteractions))
		out = append(out, fmt.Sprintf("walk-in 占比 %.1f%%（%d/%d）", walkInRate, data.TotalWalkIns, data.TotalInteractions))
	}
	if data.RiskEventCount <= 0 {
		out = append(out, "合规风险本周未检出明显异常")
	} else {
		out = append(out, fmt.Sprintf("合规风险共 %d 起，建议对高风险应答逐条复盘", data.RiskEventCount))
	}
	return out
}

func buildWeeklyStaffHighlights(data *WeeklyReportData) []string {
	if data == nil {
		return []string{}
	}
	out := make([]string, 0, 2)
	if data.TotalAppointments > 0 {
		out = append(out, fmt.Sprintf("预约承接完成 %d 次，建议复用高转化应答片段", data.TotalAppointments))
	}
	if len(data.ChannelFeedback) > 0 {
		out = append(out, fmt.Sprintf("渠道反馈覆盖 %d 类问题，形成跨渠道标准答复基础", len(data.ChannelFeedback)))
	}
	return out
}

func buildWeeklyStaffVariances(data *WeeklyReportData) []string {
	if data == nil {
		return []string{}
	}
	out := make([]string, 0, 2)
	if data.TotalInteractions > 0 && data.TotalWalkIns > data.TotalInteractions/2 {
		out = append(out, "walk-in 占比偏高，班次承接稳定性波动风险上升")
	}
	if data.RiskEventCount > 0 {
		out = append(out, fmt.Sprintf("风险事件 %d 起，个别场景应答一致性不足", data.RiskEventCount))
	}
	return out
}

func buildWeeklyStaffSuggestions(data *WeeklyReportData) []string {
	if data == nil {
		return []string{}
	}
	out := make([]string, 0, 3)
	out = append(out, "按高频问题组织班次演练，统一标准应答")
	if data.RiskEventCount > 0 {
		out = append(out, "高风险录音建立日复盘机制，次日追踪改进结果")
	}
	if data.TotalWalkIns > 0 {
		out = append(out, "针对 walk-in 场景补齐即时安排与恢复期说明话术")
	}
	return out
}

func buildWeeklyEvidenceItems(questions []string) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(questions))
	for i, q := range questions {
		topic := strings.TrimSpace(q)
		if topic == "" {
			continue
		}
		items = append(items, map[string]interface{}{
			"recording_id": 0,
			"shift_date":   "",
			"summary":      fmt.Sprintf("高频问题主题：%s", topic),
			"source":       "weekly_aggregation",
			"rank":         i + 1,
		})
	}
	return items
}

func buildWeeklyActionOwners(data *WeeklyReportData) []string {
	if data == nil {
		return []string{}
	}
	owners := []string{"前台主管"}
	if data.RiskEventCount > 0 {
		owners = append(owners, "合规负责人")
	}
	if len(data.TopQuestions) > 0 {
		owners = append(owners, "培训负责人")
	}
	return owners
}

func buildWeeklyActionEffects(data *WeeklyReportData) []string {
	if data == nil {
		return []string{}
	}
	effects := make([]string, 0, 3)
	if data.TotalInteractions > 0 {
		effects = append(effects, "观察下周预约承接率与 walk-in 承接率变化")
	}
	if data.RiskEventCount > 0 {
		effects = append(effects, "跟踪高风险场景复盘后复发率是否下降")
	}
	if len(data.TopQuestions) > 0 {
		effects = append(effects, "验证高频问题命中后回答完整率是否提升")
	}
	return effects
}
