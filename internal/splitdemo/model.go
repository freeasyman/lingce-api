package splitdemo

import "time"

type RecordingListItem struct {
	ID                      int64   `json:"id"`
	TenantID                int64   `json:"tenant_id"`
	EmployeeName            string  `json:"employee_name"`
	RecordingURL            string  `json:"recording_url,omitempty"`
	RecordingDuration       *int    `json:"recording_duration,omitempty"`
	RecordedAt              *string `json:"recorded_at,omitempty"`
	HasTranscript           bool    `json:"has_transcript"`
	HasStructuredInput      bool    `json:"has_structured_input"`
	TranscriptChars         int     `json:"transcript_chars"`
	TranscriptSegmentsCount int     `json:"transcript_segments_count"`
	HasSavedRun             bool    `json:"has_saved_run"`
	SavedRunAt              *string `json:"saved_run_at,omitempty"`
	HasSavedAnnotation      bool    `json:"has_saved_annotation"`
	SavedAnnotationAt       *string `json:"saved_annotation_at,omitempty"`
}

type RecordingDetail struct {
	ID                   int64                    `json:"id"`
	TenantID             int64                    `json:"tenant_id"`
	TenantName           string                   `json:"tenant_name"`
	EmployeeID           int64                    `json:"employee_id"`
	EmployeeName         string                   `json:"employee_name"`
	RecordingURL         string                   `json:"recording_url,omitempty"`
	RecordingDuration    *int                     `json:"recording_duration,omitempty"`
	RecordedAt           *string                  `json:"recorded_at,omitempty"`
	TranscriptText       *string                  `json:"transcript_text,omitempty"`
	CleanedTranscription []map[string]interface{} `json:"cleaned_transcription,omitempty"`
	TranscriptionSegs    []map[string]interface{} `json:"transcription_segments,omitempty"`
	AnalysisResult       map[string]interface{}   `json:"analysis_result,omitempty"`
	StructuredTranscript []map[string]interface{} `json:"structured_transcript,omitempty"`
	TimelineTranscript   []map[string]interface{} `json:"timeline_transcript,omitempty"`
	TranscriptSource     string                   `json:"transcript_source"`
	LatestRun            *SplitRunRecord          `json:"latest_run,omitempty"`
	LatestAnnotation     *AnnotationRecord        `json:"latest_annotation,omitempty"`
	CreatedAt            time.Time                `json:"created_at"`
	UpdatedAt            time.Time                `json:"updated_at"`
}

type SplitRequest struct {
	RecordingID        int64    `json:"recording_id"`
	Model              string   `json:"model"`
	SystemPrompt       string   `json:"system_prompt"`
	UserPrompt         string   `json:"user_prompt"`
	ScanSystemPrompt   string   `json:"scan_system_prompt,omitempty"`
	ScanUserPrompt     string   `json:"scan_user_prompt,omitempty"`
	JudgeSystemPrompt  string   `json:"judge_system_prompt,omitempty"`
	JudgeUserPrompt    string   `json:"judge_user_prompt,omitempty"`
	PromptVersion      string   `json:"prompt_version"`
	UseTwoPass         *bool    `json:"use_two_pass,omitempty"`
	Temperature        *float64 `json:"temperature,omitempty"`
	MaxChunkChars      *int     `json:"max_chunk_chars,omitempty"`
}

type SplitRunRecord struct {
	ID            int64          `json:"id"`
	RunID         string         `json:"run_id,omitempty"`
	RecordingID   int64          `json:"recording_id"`
	CaseID        string         `json:"case_id,omitempty"`
	Model         string         `json:"model"`
	PromptVersion string         `json:"prompt_version"`
	Status        string         `json:"status"`
	Result        *SplitResponse `json:"result,omitempty"`
	Eval          *EvalReport    `json:"eval,omitempty"`
	MatchReport   *MatchReport   `json:"match_report,omitempty"`
	RawOutput     string         `json:"raw_output,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type SplitSegment struct {
	Index                int         `json:"index"`
	SegmentType          string      `json:"segment_type"`
	StartSeconds         int         `json:"start_seconds"`
	EndSeconds           int         `json:"end_seconds"`
	BoundaryConfidence   float64     `json:"boundary_confidence"`
	BoundaryReasons      []string    `json:"boundary_reasons,omitempty"`
	SilenceGapSeconds    *int        `json:"silence_gap_seconds,omitempty"`
	NeedsReview          bool        `json:"needs_review"`
	MultiPatient         bool        `json:"multi_patient"`
	ContainsClinicalInfo *bool       `json:"contains_clinical_info,omitempty"`
	TextPreview          string      `json:"text_preview,omitempty"`
	FullText             string      `json:"full_text,omitempty"`
	DurationSeconds      int         `json:"duration_seconds"`
	ConfidenceLabel      string      `json:"confidence_label,omitempty"`
	ReasonLabels         []string    `json:"reason_labels,omitempty"`
	Utterances           []Utterance `json:"utterances,omitempty"`
}

type SplitResponse struct {
	RecordingID        int64            `json:"recording_id"`
	RecordingDuration  int              `json:"recording_duration_seconds"`
	SplitModel         string           `json:"split_model"`
	SplitPromptVersion string           `json:"split_prompt_version"`
	InputKind          string           `json:"input_kind"`
	ChunkCount         int              `json:"chunk_count"`
	ElapsedMS          int64            `json:"elapsed_ms"`
	Usage              Usage            `json:"usage"`
	Segments           []SplitSegment   `json:"segments"`
	Boundaries         []BoundaryReview `json:"boundaries,omitempty"`
	Summary            SplitSummary     `json:"summary"`
	Encounters         []EncounterView  `json:"encounters,omitempty"`
	Validation         ValidationResult `json:"validation"`
	RawOutput          string           `json:"raw_output,omitempty"`
}

type Utterance struct {
	StartSeconds int    `json:"start_seconds"`
	EndSeconds   int    `json:"end_seconds"`
	Speaker      string `json:"speaker"`
	SpeakerRole  string `json:"speaker_role,omitempty"`
	Text         string `json:"text"`
}

type BoundaryReview struct {
	Index             int         `json:"index"`
	BoundarySeconds   int         `json:"boundary_seconds"`
	BoundaryTimeLabel string      `json:"boundary_time_label"`
	SilenceGapSeconds *int        `json:"silence_gap_seconds,omitempty"`
	SilenceGapLabel   string      `json:"silence_gap_label,omitempty"`
	LeftSegmentIndex  int         `json:"left_segment_index"`
	RightSegmentIndex int         `json:"right_segment_index"`
	LeftSegmentType   string      `json:"left_segment_type"`
	RightSegmentType  string      `json:"right_segment_type"`
	LeftUtterances    []Utterance `json:"left_utterances,omitempty"`
	RightUtterances   []Utterance `json:"right_utterances,omitempty"`
	ReasonLabels      []string    `json:"reason_labels,omitempty"`
	ConfidenceLabel   string      `json:"confidence_label,omitempty"`
	NeedsReview       bool        `json:"needs_review"`
}

type SplitSummary struct {
	EncounterCount        int `json:"encounter_count"`
	NonEncounterTalkCount int `json:"non_encounter_talk_count"`
	IdleCount             int `json:"idle_count"`
	UncertainCount        int `json:"uncertain_count"`
	TotalSegmentCount     int `json:"total_segment_count"`
}

type EncounterView struct {
	EncounterNumber    int                    `json:"encounter_number"`
	SegmentIndex       int                    `json:"segment_index"`
	StartSeconds       int                    `json:"start_seconds"`
	EndSeconds         int                    `json:"end_seconds"`
	DurationSeconds    int                    `json:"duration_seconds"`
	PatientClue        string                 `json:"patient_clue"`
	PatientHint        string                 `json:"patient_hint,omitempty"`
	ChiefComplaint     string                 `json:"chief_complaint,omitempty"`
	Disposition        string                 `json:"disposition,omitempty"`
	OpeningLine        string                 `json:"opening_line,omitempty"`
	ClosingLine        string                 `json:"closing_line,omitempty"`
	FullText           string                 `json:"full_text,omitempty"`
	BoundaryConfidence float64                `json:"boundary_confidence"`
	ConfidenceLabel    string                 `json:"confidence_label,omitempty"`
	ReasonLabels       []string               `json:"reason_labels,omitempty"`
	NeedsReview        bool                   `json:"needs_review"`
	MultiPatient       bool                   `json:"multi_patient"`
	Utterances         []Utterance            `json:"utterances,omitempty"`
	BoundaryFromPrev   *EncounterBoundaryView `json:"boundary_from_prev,omitempty"`
}

type EncounterBoundaryView struct {
	PreviousEncounterNumber int                `json:"previous_encounter_number"`
	NextEncounterNumber     int                `json:"next_encounter_number"`
	StartSeconds            int                `json:"start_seconds"`
	EndSeconds              int                `json:"end_seconds"`
	DurationSeconds         int                `json:"duration_seconds"`
	LeftUtterances          []Utterance        `json:"left_utterances,omitempty"`
	RightUtterances         []Utterance        `json:"right_utterances,omitempty"`
	GapItems                []EncounterGapItem `json:"gap_items,omitempty"`
	GapSummary              string             `json:"gap_summary,omitempty"`
}

type EncounterGapItem struct {
	SegmentIndex    int    `json:"segment_index"`
	SegmentType     string `json:"segment_type"`
	StartSeconds    int    `json:"start_seconds"`
	EndSeconds      int    `json:"end_seconds"`
	DurationSeconds int    `json:"duration_seconds"`
	Label           string `json:"label"`
	TextPreview     string `json:"text_preview,omitempty"`
}

type ValidationResult struct {
	IsValid        bool     `json:"is_valid"`
	StartsAtZero   bool     `json:"starts_at_zero"`
	EndsAtDuration bool     `json:"ends_at_duration"`
	Continuous     bool     `json:"continuous"`
	OverlapCount   int      `json:"overlap_count"`
	GapCount       int      `json:"gap_count"`
	Messages       []string `json:"messages,omitempty"`
}

type SplitJobStatus string

const (
	SplitJobPending   SplitJobStatus = "pending"
	SplitJobRunning   SplitJobStatus = "running"
	SplitJobCompleted SplitJobStatus = "completed"
	SplitJobFailed    SplitJobStatus = "failed"
)

type SplitJob struct {
	ID              string         `json:"id"`
	RecordingID     int64          `json:"recording_id"`
	CaseID          string         `json:"case_id,omitempty"`
	Model           string         `json:"model"`
	PromptVersion   string         `json:"prompt_version"`
	Status          SplitJobStatus `json:"status"`
	Stage           string         `json:"stage,omitempty"`
	Message         string         `json:"message"`
	TotalChunks     int            `json:"total_chunks"`
	CompletedChunks int            `json:"completed_chunks"`
	CurrentChunk    int            `json:"current_chunk"`
	SummaryTotal    int            `json:"summary_total"`
	SummaryDone     int            `json:"summary_done"`
	CurrentSummary  int            `json:"current_summary"`
	TotalCandidates int            `json:"total_candidates"`
	JudgedCandidates int           `json:"judged_candidates"`
	PartialSegments int            `json:"partial_segments"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
	Result          *SplitResponse `json:"result,omitempty"`
	Eval            *EvalReport    `json:"eval,omitempty"`
	MatchReport     *MatchReport   `json:"match_report,omitempty"`
	ErrorMessage    string         `json:"error_message,omitempty"`
}

type SplitProgress struct {
	Stage            string
	Message          string
	TotalChunks      int
	CompletedChunks  int
	CurrentChunk     int
	SummaryTotal     int
	SummaryDone      int
	CurrentSummary   int
	TotalCandidates  int
	JudgedCandidates int
	PartialSegments  int
}

type CandidateBoundary struct {
	AtSeconds  int    `json:"at_seconds"`
	SignalType string `json:"signal_type"`
	SignalText string `json:"signal_text"`
}

type BoundaryJudgment struct {
	AtSeconds  int     `json:"at_seconds"`
	IsBoundary bool    `json:"is_boundary"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
	SignalType string  `json:"signal_type"`
	SignalText string  `json:"signal_text"`
}

type EncounterSummary struct {
	PatientHint    string `json:"patient_hint"`
	ChiefComplaint string `json:"chief_complaint"`
	Disposition    string `json:"disposition"`
}

type AnnotationRecord struct {
	RecordingID     int64                  `json:"recording_id"`
	DurationSeconds int                    `json:"duration_seconds"`
	AnnotatedAt     string                 `json:"annotated_at"`
	AnnotatedBy     string                 `json:"annotated_by,omitempty"`
	SourceRunID     string                 `json:"source_run_id,omitempty"`
	Encounters      []AnnotationEncounter  `json:"encounters"`
	Corrections     []AnnotationCorrection `json:"corrections"`
	History         []*AnnotationRecord    `json:"history,omitempty"`
}

type AnnotationEncounter struct {
	Seq            int    `json:"seq"`
	StartSeconds   int    `json:"start_seconds"`
	EndSeconds     int    `json:"end_seconds"`
	PatientHint    string `json:"patient_hint,omitempty"`
	Source         string `json:"source"`
	SourceLabel    string `json:"source_label,omitempty"`
	Confirmed      bool   `json:"confirmed,omitempty"`
	MergedFromSeqs []int  `json:"merged_from_seqs,omitempty"`
	ChiefComplaint string `json:"chief_complaint,omitempty"`
	Disposition    string `json:"disposition,omitempty"`
	OpeningLine    string `json:"opening_line,omitempty"`
	ClosingLine    string `json:"closing_line,omitempty"`
}

func cloneAnnotationRecord(record *AnnotationRecord) *AnnotationRecord {
	if record == nil {
		return nil
	}
	clone := *record
	clone.Encounters = append([]AnnotationEncounter(nil), record.Encounters...)
	clone.Corrections = append([]AnnotationCorrection(nil), record.Corrections...)
	clone.History = nil
	return &clone
}

type AnnotationCorrection struct {
	Type                 string `json:"type"`
	OriginalEncounterSeq int    `json:"original_encounter_seq,omitempty"`
	MergedSeqs           []int  `json:"merged_seqs,omitempty"`
	SplitAtSeconds       int    `json:"split_at_seconds,omitempty"`
	FromSegmentIndex     int    `json:"from_segment_index,omitempty"`
	EncounterSeq         int    `json:"encounter_seq,omitempty"`
	TargetStartSeconds   int    `json:"target_start_seconds,omitempty"`
	Notes                string `json:"notes,omitempty"`
	ReasonCode           string `json:"reason_code,omitempty"`
	ReasonNote           string `json:"reason_note,omitempty"`
}

type CorrectionReasonCode string

const (
	CorrectionReasonPatientReturned  CorrectionReasonCode = "patient_returned"
	CorrectionReasonColleagueTalk    CorrectionReasonCode = "colleague_talk"
	CorrectionReasonMultiPatient     CorrectionReasonCode = "multi_patient"
	CorrectionReasonFamilyProxy      CorrectionReasonCode = "family_proxy"
	CorrectionReasonTopicShift       CorrectionReasonCode = "topic_shift"
	CorrectionReasonFalseBoundary    CorrectionReasonCode = "false_boundary"
	CorrectionReasonMentionedPatient CorrectionReasonCode = "mentioned_patient"
	CorrectionReasonMissedBoundary   CorrectionReasonCode = "missed_boundary"
	CorrectionReasonBoundaryOffset   CorrectionReasonCode = "boundary_offset"
	CorrectionReasonNotEncounter     CorrectionReasonCode = "not_encounter"
	CorrectionReasonMissingEncounter CorrectionReasonCode = "missing_encounter"
	CorrectionReasonOther            CorrectionReasonCode = "other"
)

type CorrectionReasonOption struct {
	Code        string   `json:"code"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Actions     []string `json:"actions,omitempty"`
}

type CorrectionReasonStat struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type AnnotationStats struct {
	TotalCorrections int                    `json:"total_corrections"`
	ByReason         []CorrectionReasonStat `json:"by_reason"`
}

type AnnotationSummary struct {
	RecordingID      int64                  `json:"recording_id,omitempty"`
	TotalCorrections int                    `json:"total_corrections"`
	ByReason         []CorrectionReasonStat `json:"by_reason"`
}

type AnnotationOverview struct {
	TotalRecordings       int                    `json:"total_recordings"`
	TotalCorrections      int                    `json:"total_corrections"`
	ByReason              []CorrectionReasonStat `json:"by_reason"`
	AnnotatedRecordingIDs []int64                `json:"annotated_recording_ids,omitempty"`
}

type AnnotationOverviewResponse struct {
	ReasonOptions []CorrectionReasonOption `json:"reason_options"`
	Recording     *AnnotationSummary       `json:"recording,omitempty"`
	Overall       AnnotationOverview       `json:"overall"`
}

type SyntheticCandidate struct {
	ID                int64   `json:"id"`
	DurationSeconds   int     `json:"duration_seconds"`
	EmployeeName      string  `json:"employee_name"`
	RecordedAt        *string `json:"recorded_at,omitempty"`
	TranscriptChars   int     `json:"transcript_chars"`
	TranscriptPreview string  `json:"transcript_preview,omitempty"`
	TranscriptText    string  `json:"transcript_text,omitempty"`
}

type SyntheticGapType string

const (
	SyntheticGapSilence   SyntheticGapType = "silence"
	SyntheticGapCallNext  SyntheticGapType = "call_next"
	SyntheticGapClosing   SyntheticGapType = "closing"
	SyntheticGapAbrupt    SyntheticGapType = "abrupt"
	SyntheticGapColleague SyntheticGapType = "colleague"
)

type SyntheticSource struct {
	RecordingID  int64             `json:"recording_id"`
	GapSeconds   int               `json:"gap_seconds"`
	GapType      SyntheticGapType  `json:"gap_type"`
	StartSeconds int               `json:"start_seconds,omitempty"`
	EndSeconds   int               `json:"end_seconds,omitempty"`
	Summary      *EncounterSummary `json:"summary,omitempty"`
}

type GroundTruthMark struct {
	AtSeconds   int    `json:"at_seconds"`
	MarkType    string `json:"mark_type"`
	GapType     string `json:"gap_type"`
	Description string `json:"description,omitempty"`
}

type SyntheticCase struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Segments        []SyntheticSource `json:"segments"`
	DurationSeconds int               `json:"duration_seconds"`
	Transcript      string            `json:"transcript"`
	GroundTruth     []GroundTruthMark `json:"ground_truth"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type MaterialMatch struct {
	MaterialIndex     int               `json:"material_index"`
	RecordingID       int64             `json:"recording_id"`
	StartSeconds      int               `json:"start_seconds"`
	EndSeconds        int               `json:"end_seconds"`
	Summary           *EncounterSummary `json:"summary,omitempty"`
	Verdict           string            `json:"verdict"`
	MatchedEncounters []int             `json:"matched_encounters"`
	MergedWith        []int             `json:"merged_with,omitempty"`
	// CoveringTypes 记录该素材时间段被哪些 segment_type 覆盖(按覆盖秒数降序)。
	// verdict 为 missing 时用它说明 AI 到底把这段判成了什么,而不是笼统说"不是就诊"。
	CoveringTypes []CoverageByType `json:"covering_types,omitempty"`
}

type CoverageByType struct {
	SegmentType    string `json:"segment_type"`
	OverlapSeconds int    `json:"overlap_seconds"`
}

type EncounterMatch struct {
	Seq              int               `json:"seq"`
	StartSeconds     int               `json:"start_seconds"`
	EndSeconds       int               `json:"end_seconds"`
	Summary          *EncounterSummary `json:"summary,omitempty"`
	Verdict          string            `json:"verdict"`
	CoveredMaterials []int             `json:"covered_materials"`
}

type SeamDetail struct {
	AfterMaterial int     `json:"after_material"`
	AtSeconds     int     `json:"at_seconds"`
	GapType       string  `json:"gap_type"`
	GapSeconds    int     `json:"gap_seconds"`
	Detected      bool    `json:"detected"`
	BeforeText    string  `json:"before_text"`
	AfterText     string  `json:"after_text"`
	AIConfidence  float64 `json:"ai_confidence,omitempty"`
}

type MatchReport struct {
	MaterialCount  int `json:"material_count"`
	EncounterCount int `json:"encounter_count"`
	// MissedCount 完全没有 encounter 覆盖到的素材数
	MissedCount int `json:"missed_count"`
	// MergedCount 被合并进同一个 encounter 的素材数(漏切,和 MissedCount 同样严重)
	MergedCount    int              `json:"merged_count"`
	OversplitCount int              `json:"oversplit_count"`
	Materials      []MaterialMatch  `json:"materials"`
	Encounters     []EncounterMatch `json:"encounters"`
	Seams          []SeamDetail     `json:"seams"`
}

type SyntheticCaseSummary struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	Segments        int       `json:"segments"`
	DurationSeconds int       `json:"duration_seconds"`
	GroundTruth     int       `json:"ground_truth"`
	LastPrecision   float64   `json:"last_precision,omitempty"`
	LastRecall      float64   `json:"last_recall,omitempty"`
	LastRunAt       string    `json:"last_run_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SyntheticPreviewResult struct {
	Case SyntheticCase `json:"case"`
}

type SyntheticCaseDetail struct {
	Case         SyntheticCase     `json:"case"`
	LatestRun    *SplitRunRecord   `json:"latest_run,omitempty"`
	PreviousRuns []*SplitRunRecord `json:"previous_runs,omitempty"`
}

type SyntheticRunRequest struct {
	CaseID           string   `json:"case_id"`
	Model            string   `json:"model"`
	SystemPrompt     string   `json:"system_prompt"`
	UserPrompt       string   `json:"user_prompt"`
	ScanSystemPrompt string   `json:"scan_system_prompt,omitempty"`
	ScanUserPrompt   string   `json:"scan_user_prompt,omitempty"`
	JudgeSystemPrompt string  `json:"judge_system_prompt,omitempty"`
	JudgeUserPrompt   string  `json:"judge_user_prompt,omitempty"`
	PromptVersion    string   `json:"prompt_version"`
	UseTwoPass       *bool    `json:"use_two_pass,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	MaxChunkChars    *int     `json:"max_chunk_chars,omitempty"`
	ToleranceSeconds *int     `json:"tolerance_seconds,omitempty"`
}

type EvalDetail struct {
	Kind          string `json:"kind"`
	GroundTruthAt int    `json:"ground_truth_at,omitempty"`
	PredictedAt   int    `json:"predicted_at,omitempty"`
	OffsetSeconds int    `json:"offset_seconds,omitempty"`
	GapType       string `json:"gap_type,omitempty"`
	ContextText   string `json:"context_text"`
}

type GapTypeStat struct {
	Total  int `json:"total"`
	Hits   int `json:"hits"`
	Missed int `json:"missed"`
	Extra  int `json:"extra"`
}

type EvalReport struct {
	CaseID           string                  `json:"case_id"`
	ToleranceSeconds int                     `json:"tolerance_seconds"`
	TotalGroundTruth int                     `json:"total_ground_truth"`
	Hits             int                     `json:"hits"`
	Missed           int                     `json:"missed"`
	Extra            int                     `json:"extra"`
	Precision        float64                 `json:"precision"`
	Recall           float64                 `json:"recall"`
	MeanOffset       float64                 `json:"mean_offset"`
	MaxOffset        int                     `json:"max_offset"`
	Details          []EvalDetail            `json:"details"`
	ByGapType        map[string]*GapTypeStat `json:"by_gap_type"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llmChatRequest struct {
	Model       string       `json:"model"`
	Messages    []llmMessage `json:"messages"`
	Temperature *float64     `json:"temperature,omitempty"`
	Stream      bool         `json:"stream,omitempty"`
}

type llmChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}
