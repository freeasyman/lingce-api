package splitdemo

import "time"

type RecordingListItem struct {
	ID                 int64   `json:"id"`
	TenantID           int64   `json:"tenant_id"`
	EmployeeName       string  `json:"employee_name"`
	RecordingDuration  *int    `json:"recording_duration,omitempty"`
	RecordedAt         *string `json:"recorded_at,omitempty"`
	HasTranscript      bool    `json:"has_transcript"`
	HasStructuredInput bool    `json:"has_structured_input"`
	TranscriptChars    int     `json:"transcript_chars"`
	HasSavedRun        bool    `json:"has_saved_run"`
	SavedRunAt         *string `json:"saved_run_at,omitempty"`
	HasSavedAnnotation bool    `json:"has_saved_annotation"`
	SavedAnnotationAt  *string `json:"saved_annotation_at,omitempty"`
}

type RecordingDetail struct {
	ID                   int64                    `json:"id"`
	TenantID             int64                    `json:"tenant_id"`
	TenantName           string                   `json:"tenant_name"`
	EmployeeID           int64                    `json:"employee_id"`
	EmployeeName         string                   `json:"employee_name"`
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
	RecordingID   int64    `json:"recording_id"`
	Model         string   `json:"model"`
	SystemPrompt  string   `json:"system_prompt"`
	UserPrompt    string   `json:"user_prompt"`
	PromptVersion string   `json:"prompt_version"`
	Temperature   *float64 `json:"temperature,omitempty"`
	MaxChunkChars *int     `json:"max_chunk_chars,omitempty"`
}

type SplitRunRecord struct {
	ID            int64          `json:"id"`
	RecordingID   int64          `json:"recording_id"`
	Model         string         `json:"model"`
	PromptVersion string         `json:"prompt_version"`
	Status        string         `json:"status"`
	Result        *SplitResponse `json:"result,omitempty"`
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
	PartialSegments int            `json:"partial_segments"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
	Result          *SplitResponse `json:"result,omitempty"`
	ErrorMessage    string         `json:"error_message,omitempty"`
}

type SplitProgress struct {
	Stage           string
	Message         string
	TotalChunks     int
	CompletedChunks int
	CurrentChunk    int
	SummaryTotal    int
	SummaryDone     int
	CurrentSummary  int
	PartialSegments int
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
}

type AnnotationEncounter struct {
	Seq            int    `json:"seq"`
	StartSeconds   int    `json:"start_seconds"`
	EndSeconds     int    `json:"end_seconds"`
	PatientHint    string `json:"patient_hint,omitempty"`
	Source         string `json:"source"`
	ChiefComplaint string `json:"chief_complaint,omitempty"`
	Disposition    string `json:"disposition,omitempty"`
	OpeningLine    string `json:"opening_line,omitempty"`
	ClosingLine    string `json:"closing_line,omitempty"`
}

type AnnotationCorrection struct {
	Type                 string `json:"type"`
	OriginalEncounterSeq int    `json:"original_encounter_seq,omitempty"`
	MergedSeqs           []int  `json:"merged_seqs,omitempty"`
	SplitAtSeconds       int    `json:"split_at_seconds,omitempty"`
	FromSegmentIndex     int    `json:"from_segment_index,omitempty"`
	EncounterSeq         int    `json:"encounter_seq,omitempty"`
	Notes                string `json:"notes,omitempty"`
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
