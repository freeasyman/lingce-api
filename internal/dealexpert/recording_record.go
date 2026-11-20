package dealexpert

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type recordSegment struct {
	Role string
	Text string
	At   *time.Duration
}

type recordingRecordData struct {
	Customer   string
	Consultant string
	Tenant     string
	RecordedAt time.Time
	Duration   time.Duration
	Summary    string
	Segments   []recordSegment
}

func recordingRole(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(value, "consultant"), strings.Contains(value, "advisor"), strings.Contains(value, "咨询"):
		return "咨询师"
	case strings.Contains(value, "patient"), strings.Contains(value, "customer"), strings.Contains(value, "client"), strings.Contains(value, "客户"), strings.Contains(value, "患者"):
		return "客户"
	case strings.Contains(value, "doctor"), strings.Contains(value, "医生"):
		return "医生"
	case strings.Contains(value, "family"), strings.Contains(value, "relative"), strings.Contains(value, "家属"):
		return "家属"
	case strings.Contains(value, "nurse"), strings.Contains(value, "护士"):
		return "护士"
	case strings.Contains(value, "therapist"), strings.Contains(value, "治疗师"):
		return "治疗师"
	case strings.Contains(value, "frontdesk"), strings.Contains(value, "reception"), strings.Contains(value, "前台"):
		return "前台"
	case value == "":
		return "未识别说话人"
	}
	if !strings.ContainsAny(raw, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return strings.TrimSpace(raw)
	}
	if strings.HasPrefix(value, "speaker_") {
		return "说话人 " + strings.TrimPrefix(value, "speaker_")
	}
	return "未识别说话人"
}

func numberValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func parseRecordSegments(cleaned, raw, plain []byte) []recordSegment {
	parse := func(payload []byte) []recordSegment {
		var rows []map[string]any
		if err := json.Unmarshal(payload, &rows); err != nil {
			return nil
		}
		result := make([]recordSegment, 0, len(rows))
		for _, row := range rows {
			text, _ := row["text"].(string)
			if strings.TrimSpace(text) == "" {
				continue
			}
			roleRaw, _ := row["speaker_role"].(string)
			if strings.TrimSpace(roleRaw) == "" {
				roleRaw, _ = row["speaker"].(string)
			}
			seg := recordSegment{Role: recordingRole(roleRaw), Text: strings.TrimSpace(text)}
			if ms, ok := numberValue(row["start_ms"]); ok && ms >= 0 {
				d := time.Duration(ms) * time.Millisecond
				seg.At = &d
			} else if seconds, ok := numberValue(row["start_time"]); ok && seconds >= 0 {
				d := time.Duration(seconds * float64(time.Second))
				seg.At = &d
			}
			result = append(result, seg)
		}
		return result
	}
	if segments := parse(cleaned); len(segments) > 0 {
		return segments
	}
	if segments := parse(raw); len(segments) > 0 {
		return segments
	}
	if text := strings.TrimSpace(string(plain)); text != "" {
		return []recordSegment{{Role: "未识别说话人", Text: text}}
	}
	return nil
}

func (h *Handler) ExportRecordingRecord(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	recordingID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || recordingID <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	if accessErr := h.checkRecordingAccess(r.Context(), scope, recordingID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}

	var data recordingRecordData
	var cleaned, raw, plain []byte
	err = h.pool.QueryRow(r.Context(), `
		SELECT COALESCE(c.name, ''), COALESCE(cons.name, ''), COALESCE(t.name, ''),
		       COALESCE(r.recorded_at, r.created_at), COALESCE(r.duration, 0) * INTERVAL '1 second',
		       COALESCE(r.cleaned_transcription::text, 'null'), COALESCE(r.transcription_segments::text, 'null'),
		       COALESCE(r.transcription_text, ''), COALESCE(a.analysis->>'headline', '')
		FROM recordings r
		JOIN tenants t ON t.id = r.tenant_id
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN employees cons ON cons.id = r.employee_id AND cons.tenant_id = r.tenant_id
		LEFT JOIN LATERAL (
			SELECT analysis FROM consultation_analyses
			WHERE tenant_id = r.tenant_id AND recording_id = r.id AND status = 'analyzed'
			ORDER BY updated_at DESC, id DESC LIMIT 1
		) a ON TRUE
		WHERE r.tenant_id = $1 AND r.id = $2 AND r.deleted_at IS NULL
	`, scope.TenantID, recordingID).Scan(&data.Customer, &data.Consultant, &data.Tenant, &data.RecordedAt, &data.Duration, &cleaned, &raw, &plain, &data.Summary)
	if err != nil {
		httputil.WriteNotFound(w, "recording not found")
		return
	}
	if data.Customer == "" {
		data.Customer = "客户"
	}
	if data.Consultant == "" {
		data.Consultant = "未识别咨询师"
	}
	data.Segments = parseRecordSegments(cleaned, raw, plain)
	if len(data.Segments) == 0 {
		httputil.WriteError(w, http.StatusUnprocessableEntity, "TRANSCRIPT_EMPTY", "这条录音还没有可导出的录音记录", nil)
		return
	}
	doc, err := buildRecordingDocx(data)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	filename := fmt.Sprintf("接待录音记录-%s-%s-%s.docx", safeFilename(data.Tenant), safeFilename(data.Customer), data.RecordedAt.Format("20060102"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", "attachment; filename=\"recording-record.docx\"; filename*=UTF-8''"+urlEncode(filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(doc)))
	_, _ = w.Write(doc)
}

func safeFilename(value string) string {
	value = strings.TrimSpace(value)
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-", "\n", "", "\r", "")
	value = replacer.Replace(value)
	if value == "" {
		return "未命名"
	}
	return value
}

func urlEncode(value string) string {
	var b strings.Builder
	for _, r := range []byte(value) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == '~' {
			b.WriteByte(r)
		} else {
			b.WriteString(fmt.Sprintf("%%%02X", r))
		}
	}
	return b.String()
}

func xmlText(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func docxParagraph(text string, bold bool, size int, align string) string {
	if text == "" {
		return `<w:p/>`
	}
	alignXML := ""
	if align != "" {
		alignXML = `<w:jc w:val="` + align + `"/>`
	}
	boldXML := ""
	if bold {
		boldXML = `<w:b/>`
	}
	return `<w:p><w:pPr><w:spacing w:after="120"/>` + alignXML + `</w:pPr><w:r><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="宋体"/>` + boldXML + `<w:sz w:val="` + strconv.Itoa(size) + `"/><w:szCs w:val="` + strconv.Itoa(size) + `"/></w:rPr><w:t xml:space="preserve">` + xmlText(text) + `</w:t></w:r></w:p>`
}

func formatRecordTime(value *time.Duration) string {
	if value == nil {
		return ""
	}
	seconds := int(value.Seconds())
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func buildRecordingDocx(data recordingRecordData) ([]byte, error) {
	var body strings.Builder
	body.WriteString(docxParagraph(data.Tenant+" · 接待录音记录", true, 36, "center"))
	body.WriteString(docxParagraph("客户："+data.Customer, false, 22, ""))
	body.WriteString(docxParagraph("接待人："+data.Consultant, false, 22, ""))
	body.WriteString(docxParagraph("接待时间："+data.RecordedAt.Format("2006-01-02 15:04"), false, 22, ""))
	if data.Duration > 0 {
		body.WriteString(docxParagraph("录音时长："+formatRecordTime(&data.Duration), false, 22, ""))
	}
	if strings.TrimSpace(data.Summary) != "" {
		body.WriteString(docxParagraph("摘要："+strings.TrimSpace(data.Summary), false, 22, ""))
	}
	body.WriteString(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="4" w:space="1" w:color="D9D9D9"/></w:pBdr><w:spacing w:after="240"/></w:pPr></w:p>`)
	for _, segment := range data.Segments {
		label := segment.Role
		if at := formatRecordTime(segment.At); at != "" {
			label += "  " + at
		}
		body.WriteString(docxParagraph(label, true, 22, ""))
		body.WriteString(docxParagraph(segment.Text, false, 22, ""))
	}
	document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() + `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:body></w:document>`
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   document,
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		entry, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
