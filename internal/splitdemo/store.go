package splitdemo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool            *pgxpool.Pool
	runStore        *RunStore
	annotationStore *AnnotationStore
	syntheticStore  *SyntheticStore
}

func NewStore(pool *pgxpool.Pool, runStore *RunStore, annotationStore *AnnotationStore, syntheticStore *SyntheticStore) *Store {
	if runStore == nil {
		runStore = NewRunStore("")
	}
	if annotationStore == nil {
		annotationStore = NewAnnotationStore("")
	}
	if syntheticStore == nil {
		syntheticStore = NewSyntheticStore("")
	}
	return &Store{pool: pool, runStore: runStore, annotationStore: annotationStore, syntheticStore: syntheticStore}
}

func (s *Store) ListRecordings(ctx context.Context, tenantID int64, minDurationSeconds, page, pageSize int) ([]RecordingListItem, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if tenantID <= 0 {
		tenantID = 1
	}
	if minDurationSeconds < 0 {
		minDurationSeconds = 0
	}
	where := `
		r.tenant_id = $1
		AND r.deleted_at IS NULL
		AND LOWER(COALESCE(r.business_scope, '')) = 'doctor'
		AND COALESCE(r.duration, 0) >= $2
		AND r.transcription_text IS NOT NULL
		AND length(r.transcription_text) > 2000
	`
	var total int64
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM recordings r WHERE %s`, where), tenantID, minDurationSeconds).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count split demo recordings: %w", err)
	}
	offset := (page - 1) * pageSize
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT
			r.id,
			r.tenant_id,
			COALESCE(
				NULLIF(NULLIF(e.full_name, 'unknown'), ''),
				NULLIF(NULLIF(e.name, 'unknown'), ''),
				NULLIF(e.phone, ''),
				NULLIF(oa.username, ''),
				NULLIF(oa.email, ''),
				'未知员工'
			) AS employee_name,
			r.duration,
			r.recorded_at,
			COALESCE(length(COALESCE(r.transcription_text, '')), 0) AS transcript_chars,
			CASE WHEN COALESCE(length(trim(COALESCE(r.transcription_text, ''))), 0) > 0 THEN TRUE ELSE FALSE END AS has_transcript,
			CASE
				WHEN jsonb_typeof(COALESCE(r.transcription_segments::jsonb, '[]'::jsonb)) = 'array'
				 AND jsonb_array_length(COALESCE(r.transcription_segments::jsonb, '[]'::jsonb)) > 0 THEN TRUE
				WHEN jsonb_typeof(COALESCE(r.cleaned_transcription::jsonb, '[]'::jsonb)) = 'array'
				 AND jsonb_array_length(COALESCE(r.cleaned_transcription::jsonb, '[]'::jsonb)) > 0 THEN TRUE
				WHEN COALESCE(r.analysis_result::jsonb ? 'timeline_transcript', FALSE) THEN TRUE
				WHEN COALESCE(r.analysis_result::jsonb ? 'structured_transcript', FALSE) THEN TRUE
				ELSE FALSE
			END AS has_structured_input
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN operations_admins oa ON oa.id = r.employee_id
		WHERE %s
		ORDER BY COALESCE(r.duration, 0) DESC, r.id DESC
		LIMIT $3 OFFSET $4
	`, where), tenantID, minDurationSeconds, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list split demo recordings: %w", err)
	}
	defer rows.Close()

	items := make([]RecordingListItem, 0, pageSize)
	for rows.Next() {
		var (
			item       RecordingListItem
			recordedAt sql.NullTime
		)
		if err := rows.Scan(&item.ID, &item.TenantID, &item.EmployeeName, &item.RecordingDuration, &recordedAt, &item.TranscriptChars, &item.HasTranscript, &item.HasStructuredInput); err != nil {
			return nil, 0, fmt.Errorf("scan split demo recording: %w", err)
		}
		if recordedAt.Valid {
			v := recordedAt.Time.Format(time.RFC3339)
			item.RecordedAt = &v
		}
		if latest, err := s.runStore.LatestByRecording(ctx, item.ID); err == nil && latest != nil {
			item.HasSavedRun = true
			ts := latest.UpdatedAt.Format(time.RFC3339)
			item.SavedRunAt = &ts
		}
		if latest, err := s.annotationStore.Load(ctx, item.ID); err == nil && latest != nil {
			item.HasSavedAnnotation = true
			ts := latest.AnnotatedAt
			item.SavedAnnotationAt = &ts
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Store) GetRecording(ctx context.Context, recordingID int64) (*RecordingDetail, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			r.id,
			r.tenant_id,
			COALESCE(NULLIF(t.name, ''), CONCAT('租户#', r.tenant_id::text)) AS tenant_name,
			r.employee_id,
			COALESCE(
				NULLIF(NULLIF(e.full_name, 'unknown'), ''),
				NULLIF(NULLIF(e.name, 'unknown'), ''),
				NULLIF(e.phone, ''),
				NULLIF(oa.username, ''),
				NULLIF(oa.email, ''),
				'未知员工'
			) AS employee_name,
			r.duration,
			r.recorded_at,
			r.transcription_text,
			COALESCE(r.cleaned_transcription::jsonb, '[]'::jsonb)::text,
			COALESCE(r.transcription_segments::jsonb, '[]'::jsonb)::text,
			COALESCE(r.analysis_result::jsonb, '{}'::jsonb)::text,
			COALESCE(r.analysis_result::jsonb->'structured_transcript', '[]'::jsonb)::text,
			COALESCE(r.analysis_result::jsonb->'timeline_transcript', '[]'::jsonb)::text,
			r.created_at,
			COALESCE(r.updated_at, r.created_at, NOW())
		FROM recordings r
		LEFT JOIN tenants t ON t.id = r.tenant_id
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN operations_admins oa ON oa.id = r.employee_id
		WHERE r.id = $1
		  AND r.deleted_at IS NULL
	`, recordingID)

	var (
		item                                    RecordingDetail
		recordedAt                              sql.NullTime
		cleanedRaw, segmentsRaw                 string
		analysisRaw, structuredRaw, timelineRaw string
		transcriptText                          sql.NullString
	)
	if err := row.Scan(
		&item.ID,
		&item.TenantID,
		&item.TenantName,
		&item.EmployeeID,
		&item.EmployeeName,
		&item.RecordingDuration,
		&recordedAt,
		&transcriptText,
		&cleanedRaw,
		&segmentsRaw,
		&analysisRaw,
		&structuredRaw,
		&timelineRaw,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if recordedAt.Valid {
		v := recordedAt.Time.Format(time.RFC3339)
		item.RecordedAt = &v
	}
	if transcriptText.Valid && strings.TrimSpace(transcriptText.String) != "" {
		v := transcriptText.String
		item.TranscriptText = &v
	}
	item.CleanedTranscription = decodeSegmentsJSON([]byte(cleanedRaw))
	item.TranscriptionSegs = decodeSegmentsJSON([]byte(segmentsRaw))
	item.AnalysisResult = decodeJSONObject([]byte(analysisRaw))
	item.StructuredTranscript = decodeSegmentsJSON([]byte(structuredRaw))
	item.TimelineTranscript = decodeSegmentsJSON([]byte(timelineRaw))
	item.TranscriptSource = chooseTranscriptSource(item)
	if latest, err := s.runStore.LatestByRecording(ctx, recordingID); err == nil && latest != nil {
		item.LatestRun = latest
	}
	if latest, err := s.annotationStore.Load(ctx, recordingID); err == nil && latest != nil {
		item.LatestAnnotation = latest
	}
	return &item, nil
}

func (s *Store) SaveRun(ctx context.Context, record *SplitRunRecord) error {
	if s.runStore == nil {
		return nil
	}
	return s.runStore.Save(ctx, record)
}

func (s *Store) SaveAnnotation(ctx context.Context, record *AnnotationRecord) error {
	if s.annotationStore == nil {
		return nil
	}
	return s.annotationStore.Save(ctx, record)
}

func (s *Store) LoadAnnotation(ctx context.Context, recordingID int64) (*AnnotationRecord, error) {
	if s.annotationStore == nil {
		return nil, nil
	}
	return s.annotationStore.Load(ctx, recordingID)
}

func (s *Store) LoadAllAnnotations(ctx context.Context) ([]*AnnotationRecord, error) {
	if s.annotationStore == nil {
		return nil, nil
	}
	return s.annotationStore.LoadAll(ctx)
}

func (s *Store) SaveSyntheticCase(ctx context.Context, record *SyntheticCase) error {
	if s.syntheticStore == nil {
		return nil
	}
	return s.syntheticStore.Save(ctx, record)
}

func (s *Store) GetSyntheticCase(ctx context.Context, caseID string) (*SyntheticCase, error) {
	if s.syntheticStore == nil {
		return nil, nil
	}
	return s.syntheticStore.Load(ctx, caseID)
}

func (s *Store) ListSyntheticCases(ctx context.Context) ([]*SyntheticCase, error) {
	if s.syntheticStore == nil {
		return nil, nil
	}
	return s.syntheticStore.List(ctx)
}

func (s *Store) DeleteSyntheticCase(ctx context.Context, caseID string) error {
	if s.syntheticStore == nil {
		return nil
	}
	return s.syntheticStore.Delete(ctx, caseID)
}

func (s *Store) ListSyntheticCandidates(ctx context.Context, tenantID int64, minDurationSeconds, maxDurationSeconds, limit int) ([]SyntheticCandidate, error) {
	if tenantID <= 0 {
		tenantID = 1
	}
	if minDurationSeconds <= 0 {
		minDurationSeconds = 180
	}
	if maxDurationSeconds <= 0 {
		maxDurationSeconds = 900
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT
			r.id,
			COALESCE(r.duration, 0) AS duration,
			COALESCE(
				NULLIF(NULLIF(e.full_name, 'unknown'), ''),
				NULLIF(NULLIF(e.name, 'unknown'), ''),
				NULLIF(e.phone, ''),
				NULLIF(oa.username, ''),
				NULLIF(oa.email, ''),
				'未知员工'
			) AS employee_name,
			r.recorded_at,
			LENGTH(COALESCE(r.transcription_text, '')) AS transcript_length,
			COALESCE(r.transcription_text, '') AS transcript_text
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN operations_admins oa ON oa.id = r.employee_id
		WHERE r.tenant_id = $1
		  AND r.deleted_at IS NULL
		  AND LOWER(COALESCE(r.business_scope, '')) = 'doctor'
		  AND r.transcription_text IS NOT NULL
		  AND COALESCE(r.duration, 0) BETWEEN $2 AND $3
		ORDER BY COALESCE(r.duration, 0) ASC, r.id DESC
		LIMIT $4
	`, tenantID, minDurationSeconds, maxDurationSeconds, limit)
	if err != nil {
		return nil, fmt.Errorf("list synthetic candidates: %w", err)
	}
	defer rows.Close()

	items := make([]SyntheticCandidate, 0, limit)
	for rows.Next() {
		var (
			item       SyntheticCandidate
			recordedAt sql.NullTime
			fullText   string
		)
		if err := rows.Scan(&item.ID, &item.DurationSeconds, &item.EmployeeName, &recordedAt, &item.TranscriptChars, &fullText); err != nil {
			return nil, fmt.Errorf("scan synthetic candidate: %w", err)
		}
		if recordedAt.Valid {
			v := recordedAt.Time.Format(time.RFC3339)
			item.RecordedAt = &v
		}
		item.TranscriptText = strings.TrimSpace(fullText)
		item.TranscriptPreview = shortenPreviewText(item.TranscriptText, 180)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func chooseTranscriptSource(item RecordingDetail) string {
	switch {
	case len(item.TranscriptionSegs) > 0:
		return "recordings.transcription_segments"
	case len(item.CleanedTranscription) > 0:
		return "recordings.cleaned_transcription"
	case len(item.TimelineTranscript) > 0:
		return "analysis_result.timeline_transcript"
	case len(item.StructuredTranscript) > 0:
		return "analysis_result.structured_transcript"
	case item.TranscriptText != nil && strings.TrimSpace(*item.TranscriptText) != "":
		return "recordings.transcription_text"
	default:
		return "empty"
	}
}

func decodeSegmentsJSON(raw []byte) []map[string]interface{} {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	switch value := decoded.(type) {
	case []interface{}:
		return toMapSlice(value)
	case map[string]interface{}:
		for _, key := range []string{"segments", "items", "data"} {
			if nested, ok := value[key]; ok {
				return decodeSegmentsJSON(mustMarshal(nested))
			}
		}
	}
	return nil
}

func decodeJSONObject(raw []byte) map[string]interface{} {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	return decoded
}

func mustMarshal(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}

func toMapSlice(items []interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}
