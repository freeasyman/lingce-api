package recording

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/employee"
)

type Service struct {
	store         *Store
	employeeStore *employee.Store
	workerURL     string
	workerToken   string
	httpClient    *http.Client
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

// IsWorkerUnavailable indicates whether an error is caused by recording-worker unavailability.
func IsWorkerUnavailable(err error) bool {
	var unavailable *workerUnavailableError
	return errors.As(err, &unavailable)
}

func NewService(store *Store, employeeStore *employee.Store, workerURL, workerToken string) *Service {
	return &Service{
		store:         store,
		employeeStore: employeeStore,
		workerURL:     strings.TrimRight(workerURL, "/"),
		workerToken:   workerToken,
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
		compactRecordingListItem(resp)
		responses[i] = resp
	}

	return responses, total, nil
}

func compactRecordingListItem(resp *RecordingResponse) {
	if resp == nil {
		return
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
	return resp, nil
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
	recording, err := s.store.UpdateRecording(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toRecordingResponse(recording), nil
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
			resp.AnalysisResult = nested
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
			pickString(resp.AnalysisResult, "summary"),
			pickString(analysisDisplay, "subjective_summary"),
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
		resp.AnalysisResult = analysisResult
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
	if resp.StatusSummary == nil {
		resp.StatusSummary = pickStringPtr(
			pickString(resp.AnalysisResult, "status_summary"),
			pickString(resp.AnalysisSummary, "current_state"),
			pickString(resp.AnalysisSummary, "critical_summary"),
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
		if cleaned, err := s.loadCleanedTranscriptionSegments(ctx, recordingID); err == nil && len(cleaned) > 0 {
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
		if segments, err := s.loadRawTranscriptionSegments(ctx, recordingID); err == nil && len(segments) > 0 {
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
	RecordingID int64  `json:"recording_id"`
	TenantID    int64  `json:"tenant_id"`
	JobType     string `json:"job_type"`
	Force       bool   `json:"force,omitempty"`
}

type workerJobResponse struct {
	JobID string `json:"job_id"`
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

func (s *Service) triggerWorkerJob(ctx context.Context, id int64, jobType string, force bool) (string, error) {
	recording, err := s.store.GetRecordingByID(ctx, id)
	if err != nil {
		return "", err
	}

	jobID, err := s.submitWorkerJob(ctx, workerJobRequest{
		RecordingID: id,
		TenantID:    recording.TenantID,
		JobType:     jobType,
		Force:       force,
	})
	if err != nil {
		return "", err
	}

	status := StatusProcessing
	_, err = s.store.UpdateRecording(ctx, id, UpdateRecordingRequest{Status: &status})
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

// GetDoctorAbilityRanking retrieves doctor ability ranking
func (s *Service) GetDoctorAbilityRanking(ctx context.Context, tenantID int64) ([]DoctorAbilityRankingResponse, error) {
	rankingItems, err := s.employeeStore.GetAbilityRanking(ctx, tenantID, 20)
	if err != nil {
		return nil, err
	}

	ranking := make([]DoctorAbilityRankingResponse, 0, len(rankingItems))
	for i, item := range rankingItems {
		resp := DoctorAbilityRankingResponse{
			Rank:           i + 1,
			EmployeeID:     item.EmployeeID,
			EmployeeName:   item.EmployeeName,
			RecordingCount: item.RecordingCount,
		}
		if item.RecordingCount > 0 {
			resp.AvgScore = float64(item.CompletedCount) / float64(item.RecordingCount) * 100
		}
		ranking = append(ranking, resp)
	}
	return ranking, nil
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
	top, err := s.GetDoctorAbilityRanking(ctx, tenantID)
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

// GetWeeklySummary retrieves weekly summary
func (s *Service) GetWeeklySummary(ctx context.Context, tenantID int64) (*WeeklySummaryResponse, error) {
	now := time.Now()
	weekStart := now.AddDate(0, 0, -6)
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}
	top, err := s.GetDoctorAbilityRanking(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if len(top) > 3 {
		top = top[:3]
	}
	return &WeeklySummaryResponse{
		WeekStart:       weekStart.Format("2006-01-02"),
		WeekEnd:         now.Format("2006-01-02"),
		TotalRecordings: stats.TotalRecordings,
		AvgScore:        safeRate(stats.CompletedRecordings, stats.TotalRecordings),
		TopPerformers:   top,
		KeyInsights:     []string{"完成率稳定", "建议继续提升任务及时率"},
	}, nil
}

// GetTeamTrends retrieves team trends
func (s *Service) GetTeamTrends(ctx context.Context, tenantID int64, period string) (*TeamTrendsResponse, error) {
	if period == "" {
		period = "daily"
	}
	rows, err := s.store.pool.Query(ctx, `
		SELECT DATE(created_at) AS dt,
		       COUNT(*) AS total,
		       COUNT(CASE WHEN analysis_status = 'completed' THEN 1 END) AS completed
		FROM recordings
		WHERE tenant_id = $1 AND created_at >= NOW() - INTERVAL '30 days'
		GROUP BY dt
		ORDER BY dt ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query team trends: %w", err)
	}
	defer rows.Close()

	var points []TrendDataPoint
	for rows.Next() {
		var (
			dt        time.Time
			total     int64
			completed int64
		)
		if err := rows.Scan(&dt, &total, &completed); err != nil {
			return nil, fmt.Errorf("failed to scan trend row: %w", err)
		}
		points = append(points, TrendDataPoint{
			Date:           dt.Format("2006-01-02"),
			AvgScore:       safeRate(completed, total),
			RecordingCount: total,
		})
	}
	return &TeamTrendsResponse{Period: period, Data: points}, nil
}

// GetDailyReport retrieves daily report
func (s *Service) GetDailyReport(ctx context.Context, tenantID int64, date string) (*DailyReportResponse, error) {
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}
	top, err := s.GetDoctorAbilityRanking(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if len(top) > 5 {
		top = top[:5]
	}
	reportDate := date
	if reportDate == "" {
		reportDate = time.Now().Format("2006-01-02")
	}
	return &DailyReportResponse{
		Date:            reportDate,
		TotalRecordings: stats.TotalRecordings,
		TotalDuration:   stats.TotalDuration,
		AvgScore:        safeRate(stats.CompletedRecordings, stats.TotalRecordings),
		TopPerformers:   top,
		KeyMetrics: JSONObject{
			"pending_tasks": stats.PendingRecordings,
			"failed_tasks":  stats.FailedRecordings,
		},
	}, nil
}

// GetDiagnosis retrieves diagnosis
func (s *Service) GetDiagnosis(ctx context.Context, tenantID int64) (*DiagnosisResponse, error) {
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}
	score := safeRate(stats.CompletedRecordings, stats.TotalRecordings)
	health := "poor"
	switch {
	case score >= 90:
		health = "excellent"
	case score >= 75:
		health = "good"
	case score >= 60:
		health = "fair"
	}
	issues := []IssueCount{}
	if stats.PendingRecordings > 0 {
		issues = append(issues, IssueCount{Issue: "待处理录音", Count: stats.PendingRecordings})
	}
	if stats.FailedRecordings > 0 {
		issues = append(issues, IssueCount{Issue: "失败录音", Count: stats.FailedRecordings})
	}
	trends, err := s.GetTeamTrends(ctx, tenantID, "daily")
	if err != nil {
		return nil, err
	}
	data := trends.Data
	if len(data) > 7 {
		data = data[len(data)-7:]
	}
	sort.Slice(data, func(i, j int) bool { return data[i].Date < data[j].Date })

	return &DiagnosisResponse{
		OverallHealth: health,
		Issues:        issues,
		Recommendations: []string{
			"优先处理超时与失败任务",
			"提升录音分析完成率",
		},
		Trends: data,
	}, nil
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

	return resp
}
