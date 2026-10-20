package dealexpert

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	coverageComplete = "complete"
	coveragePartial  = "partial"
	coverageUnknown  = "unknown"

	// 产品机会开关：漏推按产品要求默认打开（受目录完整性与转写覆盖约束）；
	// 升级/替代在完成反例评测前保持关闭。
	allowMissedOpportunity   = true
	allowExtendedOpportunity = false
)

type transcriptSegment struct {
	ID      string `json:"id"`
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
	StartMS *int   `json:"start_ms"`
}

type businessFact struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Text       string   `json:"text"`
	Amount     *float64 `json:"amount"`
	Source     string   `json:"source"`
	OccurredAt string   `json:"occurred_at"`
}

type recordingMeta struct {
	Duration                int
	TranscriptionStatus     string
	EncounterRecordingCount int
	RawFirstStart           float64
	RawLastEnd              float64
	HasRawTiming            bool
}

type analysisInput struct {
	Recording       map[string]any `json:"recording"`
	BusinessFacts   []businessFact `json:"business_facts"`
	AnalysisContext map[string]any `json:"analysis_context"`
}

func buildAnalysisInput(
	item *pendingAnalysis,
	segments []transcriptSegment,
	facts []businessFact,
	catalog []map[string]any,
	coverage string,
	catalogComplete bool,
) analysisInput {
	allowedContact := any(nil)
	if item.CustomerID != nil {
		allowedContact = true
	}
	return analysisInput{
		Recording: map[string]any{
			"recording_id":       item.RecordingID,
			"encounter_id":       item.EncounterID,
			"tenant_id":          item.TenantID,
			"employee_id":        item.EmployeeID,
			"consultant_name":    item.EmployeeName,
			"customer_id":        item.CustomerID,
			"role_code":          item.RoleCode,
			"recorded_at":        formatTimePtr(item.RecordedAt),
			"coverage":           coverage,
			"cleaned_transcript": segments,
		},
		BusinessFacts: facts,
		AnalysisContext: map[string]any{
			"identity_confirmed":           item.CustomerID != nil,
			"contact_authorized":           allowedContact,
			"analysis_at":                  time.Now().Format(time.RFC3339),
			"timezone":                     "Asia/Shanghai",
			"product_catalog_available":    len(catalog) > 0,
			"catalog_complete":             catalogComplete,
			"product_catalog":              catalog,
			"allow_missed_opportunity":     allowMissedOpportunity,
			"allow_extended_opportunities": allowExtendedOpportunity,
		},
	}
}

func renderAnalysisInput(template string, input analysisInput) string {
	payload, _ := json.MarshalIndent(input, "", "  ")
	if !strings.Contains(template, "{{input_json}}") {
		return strings.TrimSpace(template) + "\n\n" + string(payload)
	}
	return strings.ReplaceAll(template, "{{input_json}}", string(payload))
}

func segmentsTranscript(segments []transcriptSegment) string {
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		if strings.TrimSpace(segment.Text) == "" {
			continue
		}
		if segment.Speaker != "" {
			parts = append(parts, segment.Speaker+"："+segment.Text)
			continue
		}
		parts = append(parts, segment.Text)
	}
	return strings.Join(parts, "\n")
}

// loadTranscriptSegments 使用 cleaned_transcription 的分段（说话人），
// 再用 transcription_segments 的 start_time 对齐时间点，并返回覆盖判定所需的元数据。
func (a *Analyzer) loadTranscriptSegments(ctx context.Context, item *pendingAnalysis) ([]transcriptSegment, recordingMeta) {
	meta := recordingMeta{}
	var cleanedRaw, rawSegments []byte
	err := a.pool.QueryRow(ctx, `
		SELECT COALESCE(cleaned_transcription::text, '[]'),
		       COALESCE(transcription_segments::text, '[]'),
		       COALESCE(duration, 0),
		       COALESCE(transcription_status, ''),
		       (SELECT COUNT(*) FROM recordings r2
		        WHERE r2.tenant_id = $2 AND r2.encounter_id = $3 AND r2.deleted_at IS NULL)
		FROM recordings WHERE id = $1
	`, item.RecordingID, item.TenantID, item.EncounterID).Scan(
		&cleanedRaw, &rawSegments, &meta.Duration, &meta.TranscriptionStatus, &meta.EncounterRecordingCount)
	if err != nil {
		meta.TranscriptionStatus = ""
	}

	segments := make([]transcriptSegment, 0, 16)
	if len(cleanedRaw) > 0 {
		var payload any
		if json.Unmarshal(cleanedRaw, &payload) == nil {
			segments = decodeTranscriptSegments(payload, 0)
		}
	}
	raw := decodeRawSegments(rawSegments)
	if len(raw) > 0 {
		meta.HasRawTiming = true
		meta.RawFirstStart = raw[0].Start
		meta.RawLastEnd = raw[0].End
		for _, segment := range raw {
			if segment.End > meta.RawLastEnd {
				meta.RawLastEnd = segment.End
			}
		}
		if len(segments) > 0 {
			alignSegmentTimestamps(segments, raw)
		}
	}
	if len(segments) == 0 && strings.TrimSpace(item.CleanedTranscript) != "" {
		segments = append(segments, transcriptSegment{
			Speaker: "未知",
			Text:    truncateRunes(item.CleanedTranscript, maxTranscriptRunes),
		})
	}
	for index := range segments {
		segments[index].ID = fmt.Sprintf("s%d", index+1)
	}
	return segments, meta
}

func computeCoverage(meta recordingMeta, segmentCount int) string {
	if meta.TranscriptionStatus != "completed" || segmentCount == 0 || meta.Duration <= 0 {
		return coverageUnknown
	}
	if meta.EncounterRecordingCount > 1 {
		return coveragePartial
	}
	if !meta.HasRawTiming {
		return coverageUnknown
	}
	if meta.RawFirstStart > 60 || float64(meta.Duration)-meta.RawLastEnd > 60 {
		return coverageUnknown
	}
	return coverageComplete
}

type rawSegment struct {
	Text  string
	Start float64
	End   float64
}

func decodeRawSegments(raw []byte) []rawSegment {
	if len(raw) == 0 {
		return nil
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil
	}
	out := make([]rawSegment, 0, len(rows))
	for _, row := range rows {
		text := pickText(row["text"], row["content"])
		if text == "" {
			continue
		}
		out = append(out, rawSegment{Text: text, Start: pickFloat(row["start_time"]), End: pickFloat(row["end_time"])})
	}
	return out
}

func alignSegmentTimestamps(segments []transcriptSegment, raw []rawSegment) {
	if len(raw) == 0 {
		return
	}
	cursor := 0
	for index := range segments {
		if segments[index].StartMS != nil {
			continue
		}
		best := -1
		bestScore := 0.0
		limit := cursor + 12
		if limit > len(raw) {
			limit = len(raw)
		}
		for j := cursor; j < limit; j++ {
			score := bigramSimilarity(segments[index].Text, raw[j].Text)
			if score > bestScore {
				bestScore, best = score, j
			}
			if score >= 0.85 {
				break
			}
		}
		if best >= 0 && bestScore >= 0.3 {
			value := int(raw[best].Start * 1000)
			segments[index].StartMS = &value
			cursor = best + 1
		}
	}
}

func bigramSimilarity(left, right string) float64 {
	a := bigramSet(left)
	b := bigramSet(right)
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for gram := range a {
		if b[gram] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func bigramSet(value string) map[string]bool {
	runes := []rune(normalizeForQuoteMatch(value))
	set := make(map[string]bool, len(runes))
	for i := 0; i+1 < len(runes); i++ {
		set[string(runes[i:i+2])] = true
	}
	return set
}

func decodeTranscriptSegments(payload any, depth int) []transcriptSegment {
	if depth > 3 || payload == nil {
		return nil
	}
	switch typed := payload.(type) {
	case []any:
		segments := make([]transcriptSegment, 0, len(typed))
		for _, row := range typed {
			item, ok := row.(map[string]any)
			if !ok {
				continue
			}
			text := pickText(item["text"], item["content"])
			if text == "" {
				continue
			}
			segments = append(segments, transcriptSegment{
				Speaker: mapSpeaker(pickText(item["speaker_role"], item["speaker"], item["role"])),
				Text:    text,
				StartMS: pickInt(item["start_ms"], item["start_time"], item["startMs"]),
			})
		}
		return segments
	case map[string]any:
		for _, key := range []string{"segments", "items", "data"} {
			if nested, ok := typed[key]; ok {
				if segments := decodeTranscriptSegments(nested, depth+1); len(segments) > 0 {
					return segments
				}
			}
		}
	}
	return nil
}

// loadBusinessFacts 把录音上的人工确认值与结果事件组装成可引用的业务事实。
func (a *Analyzer) loadBusinessFacts(ctx context.Context, item *pendingAnalysis) ([]businessFact, error) {
	facts := make([]businessFact, 0, 4)
	var status *string
	var converted, refund *float64
	if err := a.pool.QueryRow(ctx, `
		SELECT NULLIF(confirmed_deal_status, ''), converted_amount::float8, refund_amount::float8
		FROM recordings WHERE id = $1
	`, item.RecordingID).Scan(&status, &converted, &refund); err != nil {
		return nil, fmt.Errorf("load recording facts: %w", err)
	}
	_ = converted
	_ = refund

	rows, err := a.pool.Query(ctx, `
		SELECT type, amount::float8, occurred_at, source
		FROM deal_outcome_events
		WHERE tenant_id = $1 AND encounter_id = $2
		ORDER BY occurred_at ASC, id ASC
	`, item.TenantID, item.EncounterID)
	if err != nil {
		return nil, fmt.Errorf("load outcome facts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var eventType, source string
		var amount *float64
		var occurredAt time.Time
		if err := rows.Scan(&eventType, &amount, &occurredAt, &source); err != nil {
			return nil, err
		}
		text := fmt.Sprintf("已确认%s", eventType)
		if amount != nil && *amount > 0 {
			text = fmt.Sprintf("已确认%s %0.0f 元", eventType, *amount)
		}
		facts = append(facts, businessFact{
			ID:         fmt.Sprintf("f%d", len(facts)+1),
			Type:       eventType,
			Text:       text,
			Amount:     amount,
			Source:     source,
			OccurredAt: occurredAt.Format(time.RFC3339),
		})
	}
	return facts, rows.Err()
}

func pickText(values ...any) string {
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

func pickInt(values ...any) *int {
	for _, value := range values {
		switch typed := value.(type) {
		case float64:
			result := int(typed)
			return &result
		case int:
			result := typed
			return &result
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				result := int(parsed)
				return &result
			}
		}
	}
	return nil
}

func pickFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	}
	return 0
}

func formatTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "\n[转写过长，已截断]"
}

// mapSpeaker 把库里的英文说话人标识映射成中文角色；未知值原样保留，由模型判为无法判断。
func mapSpeaker(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "unknown":
		return "未知"
	case "consultant", "advisor", "sales", "doctor":
		return "咨询师"
	case "customer", "patient", "client":
		return "客户"
	}
	if strings.Contains(raw, "咨询") {
		return "咨询师"
	}
	if strings.Contains(raw, "客户") || strings.Contains(raw, "患者") {
		return "客户"
	}
	return raw
}
