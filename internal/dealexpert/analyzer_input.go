package dealexpert

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type transcriptSegment struct {
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
	StartMS *int   `json:"start_ms"`
}

type businessFacts struct {
	ConfirmedDealStatus  *string  `json:"confirmed_deal_status"`
	ConvertedAmount      *float64 `json:"converted_amount"`
	RefundAmount         *float64 `json:"refund_amount"`
	AppointmentConfirmed bool     `json:"appointment_confirmed"`
	DepositConfirmed     bool     `json:"deposit_confirmed"`
	PaymentConfirmed     bool     `json:"payment_confirmed"`
	RefundConfirmed      bool     `json:"refund_confirmed"`
}

// buildAnalysisInput 按《分析结果生成提示词与评测方案》第 4 节组装模型输入。
// 模型只解释接待，事实和时间全部由这里注入。
func buildAnalysisInput(item *pendingAnalysis, segments []transcriptSegment, facts businessFacts, catalog []map[string]any) map[string]any {
	return map[string]any{
		"recording": map[string]any{
			"recording_id":       item.RecordingID,
			"encounter_id":       item.EncounterID,
			"tenant_id":          item.TenantID,
			"employee_id":        item.EmployeeID,
			"customer_id":        item.CustomerID,
			"customer_name":      item.CustomerName,
			"recorded_at":        formatTimePtr(item.RecordedAt),
			"cleaned_transcript": segments,
		},
		"business_facts": facts,
		"analysis_context": map[string]any{
			"product_catalog_available": len(catalog) > 0,
			"product_catalog":           catalog,
			"timezone":                  "Asia/Shanghai",
		},
	}
}

// renderAnalysisInput 支持两种模板：
//  1. 推荐：{{input_json}}，整段结构化输入；
//  2. 兼容：{{transcript}} / {{products}} / {{outcome_facts}} 占位符。
func renderAnalysisInput(template string, input map[string]any, transcript string, catalog, outcomes []map[string]any) string {
	if strings.Contains(template, "{{input_json}}") {
		payload, _ := json.MarshalIndent(input, "", "  ")
		return strings.ReplaceAll(template, "{{input_json}}", string(payload))
	}
	products, _ := json.Marshal(catalog)
	outcomeJSON, _ := json.Marshal(outcomes)
	replacements := map[string]string{
		"{{transcript}}":    transcript,
		"{{products}}":      string(products),
		"{{outcome_facts}}": string(outcomeJSON),
	}
	result := template
	for key, value := range replacements {
		result = strings.ReplaceAll(result, key, value)
	}
	return result
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

// loadTranscriptSegments 优先使用 recordings.cleaned_transcription 的分段（带说话人），
// 再用原始 transcription_segments 的 start_time 对齐出时间点（原始时间是秒）。
// 两者都拿不到时退回 Worker 传入的清洗文本，start_ms 留空，不编造时间。
func (a *Analyzer) loadTranscriptSegments(ctx context.Context, item *pendingAnalysis) []transcriptSegment {
	segments := make([]transcriptSegment, 0, 16)
	var cleanedRaw, rawSegments []byte
	err := a.pool.QueryRow(ctx, `
		SELECT COALESCE(cleaned_transcription::text, '[]'), COALESCE(transcription_segments::text, '[]')
		FROM recordings WHERE id = $1
	`, item.RecordingID).Scan(&cleanedRaw, &rawSegments)
	if err == nil {
		var payload any
		if json.Unmarshal(cleanedRaw, &payload) == nil {
			segments = decodeTranscriptSegments(payload, 0)
		}
		if len(segments) > 0 {
			alignSegmentTimestamps(segments, decodeRawSegments(rawSegments))
		}
	}
	if len(segments) == 0 && strings.TrimSpace(item.CleanedTranscript) != "" {
		segments = append(segments, transcriptSegment{
			Speaker: "未知",
			Text:    truncateRunes(item.CleanedTranscript, maxTranscriptRunes),
		})
	}
	return segments
}

type rawSegment struct {
	Text  string
	Start float64
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
		start := 0.0
		switch value := row["start_time"].(type) {
		case float64:
			start = value
		case json.Number:
			if parsed, err := value.Float64(); err == nil {
				start = parsed
			}
		}
		out = append(out, rawSegment{Text: text, Start: start})
	}
	return out
}

// alignSegmentTimestamps 用原始分段的时间点给清洗分段补 start_ms。
// 清洗会合并/改写原始分段，因此按顺序找最相近的原始分段，不保证逐字一致。
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

func (a *Analyzer) loadBusinessFacts(ctx context.Context, item *pendingAnalysis) (businessFacts, error) {
	facts := businessFacts{}
	var status *string
	if err := a.pool.QueryRow(ctx, `
		SELECT NULLIF(confirmed_deal_status, ''), converted_amount::float8, refund_amount::float8
		FROM recordings WHERE id = $1
	`, item.RecordingID).Scan(&status, &facts.ConvertedAmount, &facts.RefundAmount); err != nil {
		return facts, fmt.Errorf("load recording facts: %w", err)
	}
	facts.ConfirmedDealStatus = status

	rows, err := a.pool.Query(ctx, `
		SELECT type FROM deal_outcome_events
		WHERE tenant_id = $1 AND encounter_id = $2
	`, item.TenantID, item.EncounterID)
	if err != nil {
		return facts, fmt.Errorf("load outcome facts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			return facts, err
		}
		switch eventType {
		case "预约":
			facts.AppointmentConfirmed = true
		case "定金":
			facts.DepositConfirmed = true
		case "付款":
			facts.PaymentConfirmed = true
		case "退款":
			facts.RefundConfirmed = true
		}
	}
	return facts, rows.Err()
}

func (a *Analyzer) loadCatalogIDs(ctx context.Context, tenantID int64) (map[int64]bool, error) {
	catalog, err := a.loadCatalog(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	ids := make(map[int64]bool, len(catalog))
	for _, row := range catalog {
		if id, ok := row["id"].(int64); ok && id > 0 {
			ids[id] = true
		}
	}
	return ids, nil
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
