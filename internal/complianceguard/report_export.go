package complianceguard

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/compliancepermission"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// ReportStats 月度报告聚合数据。
//
// compliance_rate 现在是 overview 聚合出的两位小数（如 99.95），
// 用 float64 接收，避免前端传小数时 JSON 解码失败导致 400。
type ReportStats struct {
	Recordings   int      `json:"recordings"`
	Findings     int      `json:"findings"`
	Fatal        int      `json:"fatal"`
	Talk         int      `json:"talk"`
	Train        int      `json:"train"`
	WithFindings int      `json:"recordings_with_findings"`
	Compliance   float64  `json:"compliance_rate"`
	RuleRows     [][2]any `json:"rule_rows"`
	DeptRows     [][2]any `json:"dept_rows"`
	FatalRows    [][4]any `json:"fatal_rows"`
}

// formatComplianceRate 输出不带多余小数位的合规率（99.95 / 100）。
func formatComplianceRate(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// ReportInput 导出接口入参。
type ReportInput struct {
	TenantID int64  `json:"tenant_id"`
	Tenant   string `json:"tenant"`
	Period   string `json:"period"`
	Range    string `json:"period_range"`
	DocNo    string `json:"doc_no"`
	Org      string `json:"org"`
	DateLine string `json:"date_line"`
	// Masked 为 true 时输出脱敏示例报告：数值一律 X，人名/科室/日期按示例口径渲染。
	Masked bool        `json:"masked"`
	Stats  ReportStats `json:"stats"`
}

// ReportTable 表格。
type ReportTable struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// ReportSection 报告一节。
type ReportSection struct {
	Heading    string       `json:"heading"`
	Paragraphs []string     `json:"paragraphs"`
	Table      *ReportTable `json:"table"`
}

type llmReport struct {
	Title      string          `json:"title"`
	DocNo      string          `json:"doc_no"`
	PeriodLine string          `json:"period_line"`
	Sections   []ReportSection `json:"sections"`
}

const reportPromptCode = "compliance_monthly_report_v1"

var reportDigitPattern = regexp.MustCompile(`\d+(?:\.\d+)?`)

func maskReportDigits(value string) string {
	return reportDigitPattern.ReplaceAllString(value, "X")
}

func maskReportDepartment(value string) string {
	if strings.HasSuffix(strings.TrimSpace(value), "部") {
		return "某某部"
	}
	return "某某科"
}

// deterministicMaskedSections 是脱敏示例报告的固定模板：不读真实数值，也不交给生成式模型。
func deterministicMaskedSections(in ReportInput) []ReportSection {
	s := in.Stats
	ruleRows := make([][]string, 0, len(s.RuleRows))
	for _, r := range s.RuleRows {
		ruleRows = append(ruleRows, []string{maskReportDigits(fmt.Sprint(r[0])), "X"})
	}
	deptRows := make([][]string, 0, len(s.DeptRows))
	for _, r := range s.DeptRows {
		deptRows = append(deptRows, []string{maskReportDepartment(fmt.Sprint(r[0])), "X"})
	}
	fatalRows := make([][]string, 0, len(s.FatalRows))
	for _, r := range s.FatalRows {
		status := maskReportDigits(strings.TrimSpace(fmt.Sprint(r[3])))
		if status == "" {
			status = "待核查"
		}
		fatalRows = append(fatalRows, []string{"某某某", maskReportDepartment(fmt.Sprint(r[1])), maskReportDigits(fmt.Sprint(r[2])), status})
	}
	return []ReportSection{
		{Heading: "一、总体情况", Paragraphs: []string{"本月共核查接诊 X 次，发现合规问题 X 项，其中重大风险 X 项、一般风险 X 项、培训建议 X 项；存在问题的接诊 X 次，重点行为合规率 X%。总体看，问题以话术类为主，重大风险数量不多但性质更敏感，需要优先处理。"},
			Table: &ReportTable{Columns: []string{"指标", "数值"}, Rows: [][]string{{"核查接诊", "X 次"}, {"发现合规问题", "X 项"}, {"其中重大风险", "X 项"}, {"重点行为合规率", "X%"}}}},
		{Heading: "二、主要问题", Paragraphs: []string{"问题结构呈“一头集中、长尾分散”的形态：无法核验的数据引用与绝对化表述构成主要风险，多发生在咨询与术后说明环节，实质是缺少可核验依据的疗效背书。"}, Table: &ReportTable{Columns: []string{"规则", "条数"}, Rows: ruleRows}},
		{Heading: "三、科室分布", Paragraphs: []string{"问题集中在某某科与某某部：科室承担咨询与成交环节，话术密度最高；术后说明与效果表述类问题性质以“说得过满”为主。"}, Table: &ReportTable{Columns: []string{"科室", "条数"}, Rows: deptRows}},
		{Heading: "四、重大风险处置", Paragraphs: []string{"本月重大风险 X 项，均已生成核查事项并逐条核实；处理结果将在下月报告中体现。"}, Table: &ReportTable{Columns: []string{"人员", "科室", "问题", "状态"}, Rows: fatalRows}},
		{Heading: "五、原因分析", Paragraphs: []string{"一是话术口径不统一，缺少统一表述模板；二是引用本院数据时缺少出处与口径意识；三是一线对隐私边界缺少明确规则。"}},
		{Heading: "六、整改建议", Paragraphs: []string{"统一话术口径、以典型原话做专项培训、明确隐私边界并纳入日常检查。"}},
		{Heading: "七、下一步工作", Paragraphs: []string{"完成本月重大风险事项的核实与处理，开展专项话术培训，持续完善资质台账。"}},
	}
}

func deterministicSections(in ReportInput) []ReportSection {
	if in.Masked {
		return deterministicMaskedSections(in)
	}
	s := in.Stats
	ruleRows := make([][]string, 0, len(s.RuleRows))
	for _, r := range s.RuleRows {
		ruleRows = append(ruleRows, []string{fmt.Sprint(r[0]), fmt.Sprint(r[1])})
	}
	deptRows := make([][]string, 0, len(s.DeptRows))
	for _, r := range s.DeptRows {
		deptRows = append(deptRows, []string{fmt.Sprint(r[0]), fmt.Sprint(r[1])})
	}
	fatalRows := make([][]string, 0, len(s.FatalRows))
	for _, r := range s.FatalRows {
		fatalRows = append(fatalRows, []string{fmt.Sprint(r[0]), fmt.Sprint(r[1]), fmt.Sprint(r[2]), fmt.Sprint(r[3])})
	}
	return []ReportSection{
		{Heading: "一、总体情况", Paragraphs: []string{fmt.Sprintf("本月共核查接诊 %d 次，发现合规问题 %d 项，其中重大风险 %d 项、一般风险 %d 项、培训建议 %d 项；存在问题的接诊 %d 次，重点行为合规率 %s%%。总体看，问题以话术类为主，重大风险数量不多但性质更敏感，需要优先处理。", s.Recordings, s.Findings, s.Fatal, s.Talk, s.Train, s.WithFindings, formatComplianceRate(s.Compliance))},
			Table: &ReportTable{Columns: []string{"指标", "数值"}, Rows: [][]string{{"核查接诊", fmt.Sprintf("%d 次", s.Recordings)}, {"发现合规问题", fmt.Sprintf("%d 项", s.Findings)}, {"其中重大风险", fmt.Sprintf("%d 项", s.Fatal)}, {"重点行为合规率", fmt.Sprintf("%s%%", formatComplianceRate(s.Compliance))}}}},
		{Heading: "二、主要问题", Paragraphs: []string{"问题结构呈“一头集中、长尾分散”的形态：无法核验的数据引用与绝对化表述构成主要风险，多发生在咨询与术后说明环节，实质是缺少可核验依据的疗效背书。"}, Table: &ReportTable{Columns: []string{"规则", "条数"}, Rows: ruleRows}},
		{Heading: "三、科室分布", Paragraphs: []string{"问题集中在接待中心与医疗部：接待中心承担咨询与成交环节，话术密度最高；医疗部的问题集中在术后说明与效果表述。"}, Table: &ReportTable{Columns: []string{"科室", "条数"}, Rows: deptRows}},
		{Heading: "四、重大风险处置", Paragraphs: []string{fmt.Sprintf("本月重大风险 %d 项，均已生成核查事项并逐条核实；处理结果将在下月报告中体现。", s.Fatal)}, Table: &ReportTable{Columns: []string{"人员", "科室", "问题", "状态"}, Rows: fatalRows}},
		{Heading: "五、原因分析", Paragraphs: []string{"一是话术口径不统一，缺少统一表述模板；二是引用本院数据时缺少出处与口径意识；三是一线对隐私边界缺少明确规则。"}},
		{Heading: "六、整改建议", Paragraphs: []string{"统一话术口径、以真实原话做专项培训、明确隐私边界并纳入日常检查。"}},
		{Heading: "七、下一步工作", Paragraphs: []string{"完成本月重大风险事项的核实与处理，开展专项话术培训，持续完善资质台账。"}},
	}
}

// BuildReportDocx 用固定分析模板渲染 docx。
func BuildReportDocx(in ReportInput) ([]byte, error) {
	return buildDoc(in, in.Tenant+"合规月度报告", in.DocNo, fmt.Sprintf("统计周期：%s（%s）。本报告依据合规卫士核查结果编制，抄送院长办公室。", in.Period, in.Range), deterministicSections(in))
}

func buildDoc(in ReportInput, title, docNo, periodLine string, sections []ReportSection) ([]byte, error) {
	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>`)
	doc.WriteString(reportPara(title, true, 40, "center", false))
	doc.WriteString(reportPara(dash(docNo, "灵医合规〔〕号"), false, 24, "center", false))
	doc.WriteString(reportPara(dash(normalizeReportWording(periodLine), ""), false, 21, "", true))
	for _, sec := range sections {
		doc.WriteString(reportPara(normalizeReportWording(sec.Heading), true, 26, "", false))
		for _, p := range sec.Paragraphs {
			doc.WriteString(reportPara(normalizeReportWording(p), false, 24, "", true))
		}
		if sec.Table != nil && len(sec.Table.Columns) > 0 {
			doc.WriteString(reportTable(sec.Table.Columns, sec.Table.Rows))
		}
	}
	doc.WriteString(`<w:p><w:pPr><w:spacing w:before="720"/></w:pPr></w:p>`)
	doc.WriteString(reportPara(dash(in.Org, "医务科"), false, 24, "right", false))
	doc.WriteString(reportPara(dash(in.DateLine, ""), false, 24, "right", false))
	doc.WriteString(reportPara("（盖章）", false, 24, "right", false))
	doc.WriteString(`<w:sectPr><w:headerReference w:type="default" r:id="rId2"/><w:footerReference w:type="default" r:id="rId3"/><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1474" w:bottom="1440" w:left="1587" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:body></w:document>`)

	header := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:jc w:val="right"/><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="999999"/></w:pBdr></w:pPr><w:r><w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体"/><w:sz w:val="18"/></w:rPr><w:t>` + xmlEsc(in.Tenant+" · 合规月度报告（"+in.Period+"）") + `</w:t></w:r></w:p></w:hdr>`
	footer := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体"/><w:sz w:val="20"/></w:rPr><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve">PAGE</w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p></w:ftr>`

	return packDocx(doc.String(), header, footer)
}

// packDocx 把 document/header/footer 三段 XML 打成最小可用的 docx 包。
// 月度报告、改进意见书、闭环清单、迎检清单共用这一套打包逻辑。
func packDocx(documentXML, headerXML, footerXML string) ([]byte, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	add := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			return
		}
		_, _ = w.Write([]byte(content))
	}
	add("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/><Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/></Types>`)
	add("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`)
	add("word/document.xml", documentXML)
	add("word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/></Relationships>`)
	add("word/header1.xml", headerXML)
	add("word/footer1.xml", footerXML)
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// normalizeReportWording 统一报告口径：Demo 为单一机构，统一讲“接诊/Encounter”，不出现“录音”。
func normalizeReportWording(s string) string {
	s = strings.ReplaceAll(s, "录音位置", "")
	s = strings.ReplaceAll(s, "录音", "接诊")
	s = regexp.MustCompile(`接诊核查(\d+)\s*条`).ReplaceAllString(s, "接诊核查$1次")
	s = regexp.MustCompile(`(\d+)\s*条接诊`).ReplaceAllString(s, "$1次接诊")
	s = regexp.MustCompile(`(\d+)\s*条录音`).ReplaceAllString(s, "$1次接诊")
	return s
}

// dashUTF8 与 dash 相同，单独命名以表明用于中文文件名。
func dashUTF8(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func dash(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func xmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func reportPara(text string, bold bool, size int, align string, indent bool) string {
	jc := ""
	if align != "" {
		jc = `<w:jc w:val="` + align + `"/>`
	}
	ind := ""
	if indent {
		ind = `<w:ind w:firstLine="480"/>`
	}
	rpr := fmt.Sprintf(`<w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体"/><w:sz w:val="%d"/>`, size)
	if bold {
		rpr += `<w:b/>`
	}
	rpr += `</w:rPr>`
	return `<w:p><w:pPr><w:spacing w:line="360" w:lineRule="auto" w:after="80"/>` + jc + ind + rpr + `</w:pPr><w:r>` + rpr + `<w:t xml:space="preserve">` + xmlEsc(text) + `</w:t></w:r></w:p>`
}

func reportTable(cols []string, rows [][]string) string {
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/><w:tblBorders>`)
	for _, edge := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		b.WriteString(`<w:` + edge + ` w:val="single" w:sz="4" w:space="0" w:color="666666"/>`)
	}
	b.WriteString(`</w:tblBorders></w:tblPr><w:tblGrid>`)
	for range cols {
		b.WriteString(`<w:gridCol w:w="2400"/>`)
	}
	b.WriteString(`</w:tblGrid><w:tr>`)
	for _, c := range cols {
		b.WriteString(`<w:tc><w:tcPr><w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/></w:tcPr>` + reportPara(c, true, 21, "", false) + `</w:tc>`)
	}
	b.WriteString(`</w:tr>`)
	for _, row := range rows {
		b.WriteString(`<w:tr>`)
		for _, c := range row {
			b.WriteString(`<w:tc>` + reportPara(c, false, 21, "", false) + `</w:tc>`)
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	return b.String()
}

// generateWithPrompt 调提示词生成报告；失败返回错误，由调用方回退模板。
func (s *Service) generateWithPrompt(ctx context.Context, in ReportInput) (*llmReport, string, error) {
	var system, userTpl, schemaJSON, version string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(system_prompt,''), COALESCE(user_prompt_template,''), COALESCE(output_schema::text,'{}'), version
		FROM recording_analysis_prompts WHERE code = $1 AND is_active = true LIMIT 1`, reportPromptCode).Scan(&system, &userTpl, &schemaJSON, &version)
	if err != nil {
		return nil, "", err
	}
	inputJSON, _ := json.Marshal(map[string]any{"tenant": in.Tenant, "period": in.Period, "period_range": in.Range, "stats": in.Stats})
	user := strings.ReplaceAll(userTpl, "{{report_input_json}}", string(inputJSON))
	user = strings.ReplaceAll(user, "{{ report_input_json }}", string(inputJSON))

	var schema map[string]any
	_ = json.Unmarshal([]byte(schemaJSON), &schema)
	tenantID := in.TenantID
	if tenantID <= 0 {
		tenantID = 5001
	}
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  "compliance-report",
		TraceID:       fmt.Sprintf("report-%d", time.Now().UnixNano()),
		FunctionType:  "compliance_check",
		Messages:      []llmgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Params:        &llmgateway.Params{Temperature: 0, MaxTokens: 8000, ResponseFormat: "json", OutputSchema: &llmgateway.OutputSchema{Name: "monthly_report", Strict: true, Schema: schema}},
	}
	resp, err := s.llm.TextInference(ctx, req)
	if err != nil {
		return nil, version, err
	}
	var out llmReport
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil || len(out.Sections) == 0 {
		return nil, version, fmt.Errorf("report json invalid")
	}
	return &out, version, nil
}

// ExportMonthlyReportDocx 优先生成式撰写，失败回退模板式。
func (s *Service) ExportMonthlyReportDocx(ctx context.Context, in ReportInput) ([]byte, string, string, error) {
	// 脱敏示例报告固定走模板渲染：生成式模型可能把真实数值/人名写回来，不能用于脱敏口径。
	if in.Masked {
		b, err := BuildReportDocx(in)
		return b, "", "masked", err
	}
	if r, version, err := s.generateWithPrompt(ctx, in); err == nil {
		title := r.Title
		if strings.TrimSpace(title) == "" {
			title = in.Tenant + "合规月度报告"
		}
		docNo := in.DocNo
		if strings.TrimSpace(docNo) == "" {
			docNo = r.DocNo
		}
		periodLine := fmt.Sprintf("统计周期：%s（%s）。本报告依据合规卫士核查结果编制，抄送院长办公室。", in.Period, in.Range)
		det := deterministicSections(in)
		sections := make([]ReportSection, 0, len(r.Sections))
		for i, sec := range r.Sections {
			if sec.Table == nil && i < len(det) && det[i].Table != nil {
				sec.Table = det[i].Table
			}
			sections = append(sections, sec)
		}
		b, err := buildDoc(in, title, docNo, periodLine, sections)
		if err == nil {
			return b, version, "llm", nil
		}
	}
	b, err := BuildReportDocx(in)
	mode := "template"
	if _, verr := s.promptExists(ctx); verr != nil {
		mode = "template:prompt-missing"
	} else if _, _, lerr := s.generateWithPrompt(ctx, in); lerr != nil {
		m := lerr.Error()
		if len(m) > 80 {
			m = m[:80]
		}
		mode = "template:llm-" + m
	}
	return b, "", mode, err

}

// ExportMonthlyReport 处理月度报告导出（POST JSON → docx）。
func (s *Service) promptExists(ctx context.Context) (string, error) {
	var v string
	err := s.pool.QueryRow(ctx, `SELECT version FROM recording_analysis_prompts WHERE code = $1 AND is_active = true LIMIT 1`, reportPromptCode).Scan(&v)
	return v, err
}

func (h *Handler) ExportMonthlyReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// P2-2：导出前校验合规只读能力与租户；body 里的 tenant_id 一律忽略。
	_, tenantID, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingRead)
	if !ok {
		return
	}
	var in ReportInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	in.TenantID = tenantID
	b, version, mode, err := h.service.ExportMonthlyReportDocx(r.Context(), in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", `attachment; filename="compliance-monthly-report.docx"; filename*=UTF-8''`+url.PathEscape("合规月度报告-"+dashUTF8(in.Period, "本期")+".docx"))
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")
	w.Header().Set("X-Report-Prompt", reportPromptCode)
	w.Header().Set("X-Report-Prompt-Version", version)
	w.Header().Set("X-Report-Mode", mode)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
