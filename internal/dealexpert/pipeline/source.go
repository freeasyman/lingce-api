package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SourceSnapshot struct {
	AnalysisID int64
	Material   InputMaterial
	Audit      json.RawMessage
}

// LoadSourceSnapshot reads a consistent current source set. It does not claim
// to restore the unrecorded request used by a historical analysis.
func LoadSourceSnapshot(ctx context.Context, pool *pgxpool.Pool, tenantID, analysisID int64, at time.Time) (SourceSnapshot, error) {
	if pool == nil {
		return SourceSnapshot{}, fmt.Errorf("source pool required")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return SourceSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	s, err := loadSourceSnapshot(ctx, tx, tenantID, analysisID, at)
	if err != nil {
		return SourceSnapshot{}, err
	}
	return s, tx.Commit(ctx)
}

func loadSourceSnapshot(ctx context.Context, db auditDB, tenantID, analysisID int64, at time.Time) (SourceSnapshot, error) {
	if tenantID <= 0 || analysisID <= 0 || at.IsZero() {
		return SourceSnapshot{}, fmt.Errorf("positive source identity and explicit time required")
	}
	var recordingID, encounterID, employeeID int64
	var name string
	var recordedAt *time.Time
	var clean, asr []byte
	var status, cleanStatus, legacyText string
	var duration int
	err := db.QueryRow(ctx, `SELECT r.id,a.encounter_id,a.employee_id,
 COALESCE(NULLIF(e.full_name,''),e.name,''),r.recorded_at AT TIME ZONE 'Asia/Shanghai',
 COALESCE(r.cleaned_transcription,'null'::jsonb)::text,COALESCE(r.transcription_segments,'[]'::json)::text,
 COALESCE(r.transcription_status,''),COALESCE(r.cleaned_transcription_status,''),COALESCE(r.duration,0),COALESCE(a.cleaned_transcript,'')
 FROM deal_expert_analyses a JOIN recordings r ON r.id=a.recording_id AND r.tenant_id=a.tenant_id AND r.encounter_id=a.encounter_id AND r.employee_id=a.employee_id AND r.deleted_at IS NULL
 JOIN encounters n ON n.id=a.encounter_id AND n.tenant_id=a.tenant_id
 JOIN employees e ON e.id=a.employee_id AND e.tenant_id=a.tenant_id
 WHERE a.id=$1 AND a.tenant_id=$2`, analysisID, tenantID).Scan(&recordingID, &encounterID, &employeeID, &name, &recordedAt, &clean, &asr, &status, &cleanStatus, &duration, &legacyText)
	if err != nil {
		return SourceSnapshot{}, fmt.Errorf("load tenant-scoped recording source: %w", err)
	}
	transcript, trace, dropped, truncated, err := decodeSourceTranscript(clean, asr)
	if err != nil {
		return SourceSnapshot{}, err
	}
	// The old request may have truncated text even when the current source is full.
	legacyTruncated := strings.Contains(legacyText, "[转写过长，已截断]")
	m := InputMaterial{Recording: RecordingInput{RecordingID: recordingID, TenantID: tenantID, EncounterID: &encounterID, EmployeeID: employeeID, ConsultantName: name, RecordedAt: recordedAt}, Context: ContextInput{AnalysisAt: at, Timezone: "Asia/Shanghai", Coverage: "unknown", Truncated: truncated, CatalogComplete: true, AllowMissedOpportunity: true}, Transcript: transcript, DroppedSourceIDs: dropped}
	if truncated {
		m.Context.Coverage = "partial"
	}
	// Existing customer/patient links do not establish verified identity or consent.
	m.Context.IdentityConfirmed = false
	m.Context.ContactAuthorized = nil
	rows, err := db.Query(ctx, `SELECT id,type,amount::text,occurred_at,source,source_ref,note,confirmed_by FROM deal_outcome_events WHERE tenant_id=$1 AND encounter_id=$2 ORDER BY occurred_at,id`, tenantID, encounterID)
	if err != nil {
		return SourceSnapshot{}, err
	}
	factAudit := []any{}
	for rows.Next() {
		var id int64
		var kind, source, ref, note string
		var amount *string
		var occurred time.Time
		var confirmed *int64
		if err = rows.Scan(&id, &kind, &amount, &occurred, &source, &ref, &note, &confirmed); err != nil {
			rows.Close()
			return SourceSnapshot{}, err
		}
		if kind != "预约" && kind != "定金" && kind != "付款" && kind != "退款" {
			rows.Close()
			return SourceSnapshot{}, fmt.Errorf("unsupported business event type for record %d", id)
		}
		text := "系统已记录" + kind
		if amount != nil {
			text += "，金额 " + *amount + " 元"
		}
		fID := fmt.Sprintf("f%d", len(m.Facts)+1)
		m.Facts = append(m.Facts, BusinessFactInput{ID: fID, Type: kind, Text: text, SourceRecordID: id, OccurredAt: &occurred})
		factAudit = append(factAudit, map[string]any{"id": fID, "table": "deal_outcome_events", "source_record_id": id, "type": kind, "amount": amount, "occurred_at": occurred, "source": source, "source_ref": ref, "note": note, "confirmed_by": confirmed})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return SourceSnapshot{}, err
	}
	rows, err = db.Query(ctx, `SELECT id,name,version::text,COALESCE(applicable,'') FROM products WHERE tenant_id=$1 AND deleted_at IS NULL AND status='active' AND is_template=false AND review_status='approved' ORDER BY updated_at DESC,id DESC`, tenantID)
	if err != nil {
		return SourceSnapshot{}, err
	}
	for rows.Next() {
		var p ProductInput
		if err = rows.Scan(&p.ID, &p.Name, &p.Version, &p.Description); err != nil {
			rows.Close()
			return SourceSnapshot{}, err
		}
		p.RequiresClinicalConfirmation = true
		m.Products = append(m.Products, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return SourceSnapshot{}, err
	}
	env, err := AssembleInput(m)
	if err != nil {
		return SourceSnapshot{}, err
	}
	var preprocessing map[string]any
	_ = json.Unmarshal(env.PreprocessingAudit, &preprocessing)
	preprocessing["source_loader_version"] = "deal-source-v2.0.2"
	preprocessing["recorded_at_basis"] = "recordings.recorded_at wall-clock Asia/Shanghai"
	preprocessing["analysis_id"] = analysisID
	preprocessing["source_read_at"] = at
	preprocessing["source_mode"] = "current_sources_shadow"
	preprocessing["recording_id"] = recordingID
	preprocessing["cleaned_raw"] = string(clean)
	preprocessing["cleaned_hash"] = hashBytes(clean)
	preprocessing["asr_raw"] = string(asr)
	preprocessing["asr_hash"] = hashBytes(asr)
	preprocessing["transcript_mapping"] = trace
	preprocessing["business_event_records"] = factAudit
	preprocessing["transcription_status"] = status
	preprocessing["cleaned_transcription_status"] = cleanStatus
	preprocessing["duration_seconds"] = duration
	preprocessing["legacy_request_was_truncated"] = legacyTruncated
	preprocessing["coverage_reason"] = "no source proving complete encounter coverage"
	preprocessing["identity_basis"] = "no explicit identity verification source"
	preprocessing["contact_authorization_basis"] = "no explicit consent source; association is not authorization"
	audit, _ := json.Marshal(preprocessing)
	return SourceSnapshot{AnalysisID: analysisID, Material: m, Audit: audit}, nil
}

type transcriptMapping struct {
	ID                  string `json:"id"`
	Path                string `json:"cleaned_json_path"`
	RawRole             string `json:"raw_role"`
	ASRIndex            *int   `json:"asr_index"`
	Reason              string `json:"timing_reason"`
	OriginalTextHash    string `json:"original_text_hash"`
	StartByte           int    `json:"start_byte"`
	EndByte             int    `json:"end_byte"`
	Disposition         string `json:"disposition"`
	PreprocessingReason string `json:"preprocessing_reason"`
}

// Only an unambiguous exact text match can transfer an ASR timestamp. Cleaned
// text keeps its original bytes. No similarity, positional or zero-time fallback.
func decodeSourceTranscript(clean, asr []byte) ([]TranscriptInput, []transcriptMapping, []string, bool, error) {
	var payload any
	d := json.NewDecoder(bytes.NewReader(clean))
	d.UseNumber()
	if err := d.Decode(&payload); err != nil {
		return nil, nil, nil, false, fmt.Errorf("decode cleaned source: %w", err)
	}
	if err := requireSourceEOF(d); err != nil {
		return nil, nil, nil, false, err
	}
	path := "$"
	for depth := 0; depth < 5; depth++ {
		obj, ok := payload.(map[string]any)
		if !ok {
			break
		}
		found := false
		for _, key := range []string{"segments", "items", "data"} {
			if value, ok := obj[key]; ok {
				payload = value
				path += "." + key
				found = true
				break
			}
		}
		if !found {
			return nil, nil, nil, false, fmt.Errorf("unrecognized cleaned source wrapper")
		}
	}
	var items []any
	switch v := payload.(type) {
	case []any:
		items = v
	case string:
		items = []any{map[string]any{"text": v}}
	default:
		return nil, nil, nil, false, fmt.Errorf("cleaned source must contain segments")
	}
	raw, err := decodeASRSegments(asr)
	if err != nil {
		return nil, nil, nil, false, err
	}
	result := []TranscriptInput{}
	mapping := []transcriptMapping{}
	dropped := []string{}
	truncated := false
	for index, value := range items {
		location := fmt.Sprintf("%s[%d]", path, index)
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, nil, nil, false, fmt.Errorf("segment %s must be an object", location)
		}
		text, ok := obj["text"].(string)
		if !ok {
			return nil, nil, nil, false, fmt.Errorf("segment %s text must be a string", location)
		}
		role := ""
		for _, key := range []string{"speaker_role", "role", "speaker"} {
			if s, ok := obj[key].(string); ok && s != "" {
				role = s
				break
			}
		}
		speaker := sourceRole(role)
		basis := "cleaned_transcription"
		if speaker == "未知" {
			basis = "unknown"
		}
		pieces, hasControl := splitSourceText(text)
		truncated = truncated || hasControl
		matches := []int{}
		for i, a := range raw {
			if a["text"] == text {
				matches = append(matches, i)
			}
		}
		for _, piece := range pieces {
			trace := transcriptMapping{Path: location, RawRole: role, OriginalTextHash: hashBytes([]byte(text)), StartByte: piece.start, EndByte: piece.end, Disposition: piece.disposition, PreprocessingReason: piece.reason, Reason: "not a dialogue source"}
			if piece.disposition != "kept" {
				dropped = append(dropped, location)
				mapping = append(mapping, trace)
				continue
			}
			s := TranscriptInput{ID: fmt.Sprintf("s%d", len(result)+1), Text: text[piece.start:piece.end], Speaker: speaker, RoleBasis: basis, TimingBasis: "unknown"}
			trace.ID = s.ID
			trace.Reason = "no unique exact ASR text match"
			// A substring has no independently known timing. Even an exact match of
			// the original marked segment must not lend it a guessed start time.
			if hasControl {
				trace.Reason = "control-separated substring has no independent timing"
			} else if len(matches) == 1 {
				i := matches[0]
				if ms, ok := asrStartMS(raw[i]); ok {
					s.StartMS = &ms
					s.TimingBasis = "aligned"
					trace.ASRIndex = &i
					trace.Reason = "unique exact ASR text match"
				}
			}
			result = append(result, s)
			mapping = append(mapping, trace)
		}
	}
	return result, mapping, dropped, truncated, nil
}

func requireSourceEOF(d *json.Decoder) error {
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing source JSON")
	}
	return nil
}

func sourceRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "consultant", "advisor", "sales", "咨询师":
		return "咨询师"
	case "doctor", "医生":
		return "医生"
	case "customer", "patient", "client", "客户", "患者":
		return "客户"
	case "family", "家属":
		return "家属"
	default:
		return "未知"
	}
}

func asrStartMS(row map[string]any) (int, bool) {
	for _, key := range []string{"start_ms", "start_time"} {
		if number, ok := row[key].(json.Number); ok {
			x, err := strconv.ParseFloat(string(number), 64)
			if err != nil || math.IsInf(x, 0) || math.IsNaN(x) || x < 0 {
				return 0, false
			}
			if key == "start_time" {
				x *= 1000
			}
			rounded := math.Round(x)
			if rounded > float64(math.MaxInt32) || math.Abs(x-rounded) > 0.000001 {
				return 0, false
			}
			return int(rounded), true
		}
	}
	return 0, false
}

// decodeASRSegments 解析 ASR 原始分段。历史数据里 transcription_segments 可能是
// 双编码的 JSON 字符串（内容才是数组），这里显式兼容该形态；两种形态都必须通过
// 严格的 EOF 检查，不放宽对拼接/重复 JSON 的拒绝。
func decodeASRSegments(asr []byte) ([]map[string]any, error) {
	var segments []map[string]any
	d := json.NewDecoder(bytes.NewReader(asr))
	d.UseNumber()
	if err := d.Decode(&segments); err == nil {
		if err := requireSourceEOF(d); err != nil {
			return nil, err
		}
		return segments, nil
	}
	var encoded string
	if err := json.Unmarshal(asr, &encoded); err != nil {
		return nil, fmt.Errorf("decode ASR source: %w", err)
	}
	segments = nil
	inner := json.NewDecoder(strings.NewReader(encoded))
	inner.UseNumber()
	if err := inner.Decode(&segments); err != nil {
		return nil, fmt.Errorf("decode ASR source: %w", err)
	}
	if err := requireSourceEOF(inner); err != nil {
		return nil, err
	}
	return segments, nil
}
