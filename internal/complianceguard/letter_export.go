package complianceguard

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/compliancepermission"
)

// 本文件提供三类迎检/整改文书导出：
//  1. 合规改进意见书（按核查事项）
//  2. 核查事项闭环清单
//  3. 迎检资质清单
// 三份文书均复用 report_export.go 的段落/表格/打包能力，输出真正的 docx（A4、宋体、页眉、页码域、落款）。

// FlexStrings 允许 JSON 里传字符串或字符串数组（法条依据两种写法都常见）。
type FlexStrings []string

func (f *FlexStrings) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*f = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = FlexStrings(splitBasis(s))
		return nil
	}
	var arr []string
	if err := json.Unmarshal(b, &arr); err == nil {
		out := make([]string, 0, len(arr))
		for _, line := range arr {
			if v := strings.TrimSpace(line); v != "" {
				out = append(out, v)
			}
		}
		*f = FlexStrings(out)
		return nil
	}
	*f = nil
	return nil
}

// FlexText 允许 JSON 里传字符串或数字（数量列既有 "4 / 4" 也有 12）。
type FlexText string

func (t *FlexText) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*t = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = FlexText(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		*t = FlexText(n.String())
		return nil
	}
	*t = ""
	return nil
}

func splitBasis(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '；' || r == ';' })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if v := strings.TrimSpace(f); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		if v := strings.TrimSpace(s); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// 一、合规改进意见书
// ---------------------------------------------------------------------------

// LetterInput 改进意见书入参。
type LetterInput struct {
	ID            string      `json:"-"`
	Person        string      `json:"person"`
	Department    string      `json:"department"`
	Question      string      `json:"question"`
	Quote         string      `json:"quote"`
	Basis         FlexStrings `json:"basis"`
	Hypothesis    string      `json:"hypothesis"`
	RewriteBefore string      `json:"rewrite_before"`
	RewriteAfter  string      `json:"rewrite_after"`
	VisitDate     string      `json:"visit_date"`
	ReplyDays     int         `json:"reply_days"`
	Tenant        string      `json:"tenant"`
	Org           string      `json:"org"`
	DateLine      string      `json:"date_line"`
	DocNo         string      `json:"doc_no"`
}

func (in LetterInput) docNo() string {
	if v := strings.TrimSpace(in.DocNo); v != "" {
		return v
	}
	id := strings.ToUpper(strings.TrimSpace(in.ID))
	if id == "" {
		id = fmt.Sprintf("%d", time.Now().Unix())
	}
	return "灵医合规意〔2026〕" + id + "号"
}

func (in LetterInput) replyDays() int {
	if in.ReplyDays > 0 {
		return in.ReplyDays
	}
	return 7
}

// BuildLetterDocx 渲染《合规改进意见书》。
func BuildLetterDocx(in LetterInput) ([]byte, error) {
	tenant := dash(strings.TrimSpace(in.Tenant), "灵策医美医院")
	org := dash(strings.TrimSpace(in.Org), "医务科")
	dateLine := dash(strings.TrimSpace(in.DateLine), time.Now().Format("2006年01月02日"))

	var doc strings.Builder
	doc.WriteString(docxDocumentOpen())
	doc.WriteString(reportPara("合规改进意见书", true, 44, "center", false))
	doc.WriteString(reportPara("编号："+in.docNo(), false, 21, "center", false))
	doc.WriteString(reportPara("（本意见书依据合规卫士核查结果出具，交当事人确认后归档）", false, 20, "center", false))
	doc.WriteString(`<w:p><w:pPr><w:spacing w:before="120"/></w:pPr></w:p>`)

	// 基本信息表：人员、科室、就诊时间、问题类型。
	doc.WriteString(keyValueTable([][2]string{
		{"就诊时间", dash(strings.TrimSpace(in.VisitDate), "—")},
		{"人员 / 科室", strings.TrimSpace(dash(in.Person, "—") + " · " + dash(in.Department, "—"))},
		{"问题类型", dash(strings.TrimSpace(in.Question), "—")},
		{"法条依据", dash(strings.Join(in.Basis, "；"), "—")},
	}))

	// 相关原文：大引号 + 浅黄底，保持与页面展示一致的引用样式。
	doc.WriteString(reportPara("一、相关原文", true, 26, "", false))
	if q := strings.TrimSpace(in.Quote); q != "" {
		doc.WriteString(quotePara(q))
	} else {
		doc.WriteString(reportPara("—", false, 24, "", true))
	}

	doc.WriteString(reportPara("二、问题与风险说明", true, 26, "", false))
	doc.WriteString(reportPara(dash(strings.TrimSpace(in.Hypothesis), "经核查，上述表述不符合合规要求，存在被认定为违规宣传的风险。"), false, 24, "", true))

	doc.WriteString(reportPara("三、建议说法", true, 26, "", false))
	doc.WriteString(reportPara("原表述：", true, 24, "", false))
	doc.WriteString(quotePara(dash(strings.TrimSpace(in.RewriteBefore), "—")))
	doc.WriteString(reportPara("建议改为：", true, 24, "", false))
	doc.WriteString(quotePara(dash(strings.TrimSpace(in.RewriteAfter), "—")))
	doc.WriteString(reportPara("上述建议保留必要信息、去掉保证性结果与绝对化表述，请在后续接诊中统一使用。", false, 24, "", true))

	doc.WriteString(reportPara("四、回复要求", true, 26, "", false))
	doc.WriteString(reportPara(fmt.Sprintf("请于收到本意见书后 %d 个工作日内书面回复改进情况；%s将在到期后抽查接诊记录复查，确认同类表述已消除。", in.replyDays(), org), false, 24, "", true))

	// 签收栏。
	doc.WriteString(`<w:p><w:pPr><w:spacing w:before="240"/></w:pPr></w:p>`)
	doc.WriteString(reportPara("五、签收", true, 26, "", false))
	doc.WriteString(keyValueTable([][2]string{
		{"本人签收（签字）", ""},
		{"签收日期", ""},
		{"送达方式", "□ 纸质　□ 企业微信　□ 邮件"},
	}))

	doc.WriteString(`<w:p><w:pPr><w:spacing w:before="480"/></w:pPr></w:p>`)
	doc.WriteString(reportPara(tenant+"　"+org, false, 24, "right", false))
	doc.WriteString(reportPara(dateLine, false, 24, "right", false))
	doc.WriteString(reportPara("（盖章）", false, 24, "right", false))
	doc.WriteString(docxSectPr())

	header := docxHeader(tenant + " · 合规改进意见书（" + in.docNo() + "）")
	return packDocx(doc.String(), header, docxFooter())
}

// ExportCheckItemLetter 处理 POST /compliance/check-items/{id}/letter/export。
func (h *Handler) ExportCheckItemLetter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// P2-2：导出前校验合规只读能力与租户。
	if _, _, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingRead); !ok {
		return
	}
	var in LetterInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	in.ID = strings.TrimSpace(r.PathValue("id"))
	if in.ID == "" {
		in.ID = strings.TrimSpace(r.URL.Query().Get("id"))
	}
	if strings.TrimSpace(in.Question) == "" && strings.TrimSpace(in.Quote) == "" {
		http.Error(w, "question or quote is required", http.StatusBadRequest)
		return
	}
	b, err := BuildLetterDocx(in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeDocx(w, "compliance-improvement-letter.docx", "合规改进意见书-"+in.ID+".docx", b)
}

// ---------------------------------------------------------------------------
// 二、核查事项闭环清单
// ---------------------------------------------------------------------------

// LoopRow 闭环清单一行。
type LoopRow struct {
	No               string `json:"no"`
	Person           string `json:"person"`
	Department       string `json:"department"`
	VisitDate        string `json:"visit_date"`
	Question         string `json:"question"`
	Decision         string `json:"decision"`
	ReviewDate       string `json:"review_date"`
	ReviewConclusion string `json:"review_conclusion"`
	ClosedDate       string `json:"closed_date"`
}

// LoopInput 闭环清单入参。
type LoopInput struct {
	Tenant   string    `json:"tenant"`
	Period   string    `json:"period"`
	Range    string    `json:"period_range"`
	DocNo    string    `json:"doc_no"`
	Org      string    `json:"org"`
	DateLine string    `json:"date_line"`
	Rows     []LoopRow `json:"rows"`
}

// BuildLoopDocx 渲染《核查事项闭环清单》。
func BuildLoopDocx(in LoopInput) ([]byte, error) {
	tenant := dash(strings.TrimSpace(in.Tenant), "灵策医美医院")
	org := dash(strings.TrimSpace(in.Org), "医务科")
	period := dash(strings.TrimSpace(in.Period), time.Now().Format("2006年01月"))
	dateLine := dash(strings.TrimSpace(in.DateLine), time.Now().Format("2006年01月02日"))
	docNo := dash(strings.TrimSpace(in.DocNo), "灵医合规闭〔2026〕01号")

	closed := 0
	for _, row := range in.Rows {
		if strings.TrimSpace(row.ClosedDate) != "" {
			closed++
		}
	}

	var doc strings.Builder
	doc.WriteString(docxDocumentOpen())
	doc.WriteString(reportPara("核查事项闭环清单", true, 44, "center", false))
	doc.WriteString(reportPara(docNo, false, 21, "center", false))
	doc.WriteString(reportPara(fmt.Sprintf("统计周期：%s。本清单共 %d 条核查事项，其中已关闭 %d 条，逐条记录处理决定、复查结论与关闭日期，作为依法执业自查材料。", period, len(in.Rows), closed), false, 24, "", true))
	if r := strings.TrimSpace(in.Range); r != "" {
		doc.WriteString(reportPara("统计区间："+r, false, 21, "", false))
	}

	rows := make([][]string, 0, len(in.Rows))
	for i, row := range in.Rows {
		no := strings.TrimSpace(row.No)
		if no == "" {
			no = strconv.Itoa(i + 1)
		}
		rows = append(rows, []string{
			no,
			dash(strings.TrimSpace(row.Person), "—"),
			dash(strings.TrimSpace(row.Department), "—"),
			dash(strings.TrimSpace(row.VisitDate), "—"),
			dash(strings.TrimSpace(row.Question), "—"),
			dash(strings.TrimSpace(row.Decision), "待核实"),
			dash(strings.TrimSpace(row.ReviewDate), "—"),
			dash(strings.TrimSpace(row.ReviewConclusion), "未复查"),
			dash(strings.TrimSpace(row.ClosedDate), "—"),
		})
	}
	cols := []string{"编号", "人员", "科室", "就诊时间", "问题", "处理决定", "复查日期", "复查结论", "关闭日期"}
	doc.WriteString(sizedTable(cols, rows, []int{820, 760, 780, 960, 2100, 880, 900, 820, 820}))

	doc.WriteString(reportPara("说明：处理决定分为“发改进意见书”“立项整改”“关闭（误报）”三类；复查结论分为“通过”“未通过”“继续观察”；复查未通过的事项重新进入待复查状态。", false, 21, "", true))
	if len(in.Rows) == 0 {
		doc.WriteString(reportPara("本期无核查事项。", false, 24, "", true))
	}

	doc.WriteString(`<w:p><w:pPr><w:spacing w:before="360"/></w:pPr></w:p>`)
	doc.WriteString(reportPara(tenant+"　"+org, false, 24, "right", false))
	doc.WriteString(reportPara(dateLine, false, 24, "right", false))
	doc.WriteString(reportPara("（盖章）", false, 24, "right", false))
	doc.WriteString(docxSectPr())

	header := docxHeader(tenant + " · 核查事项闭环清单（" + period + "）")
	return packDocx(doc.String(), header, docxFooter())
}

// ExportLoopList 处理 POST /compliance/reports/loop/export。
func (h *Handler) ExportLoopList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// P2-2：导出前校验合规只读能力与租户。
	if _, _, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingRead); !ok {
		return
	}
	var in LoopInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&in); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	b, err := BuildLoopDocx(in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeDocx(w, "compliance-loop-list.docx", "核查事项闭环清单.docx", b)
}

// ---------------------------------------------------------------------------
// 三、迎检资质清单
// ---------------------------------------------------------------------------

// QualRow 资质清单一行。
type QualRow struct {
	Item   string   `json:"item"`
	Count  FlexText `json:"count"`
	Status string   `json:"status"`
}

// QualInput 资质清单入参。
type QualInput struct {
	Tenant   string    `json:"tenant"`
	Period   string    `json:"period"`
	DocNo    string    `json:"doc_no"`
	Org      string    `json:"org"`
	DateLine string    `json:"date_line"`
	Rows     []QualRow `json:"rows"`
}

func qualStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "ok", "normal", "pass", "done", "已具备", "正常":
		return "已具备"
	case "expiring", "soon", "即将到期":
		return "即将到期"
	case "missing", "pending", "warning", "todo", "待补", "缺失":
		return "待补"
	default:
		return strings.TrimSpace(status)
	}
}

// BuildQualificationDocx 渲染《迎检资质清单》。
func BuildQualificationDocx(in QualInput) ([]byte, error) {
	tenant := dash(strings.TrimSpace(in.Tenant), "灵策医美医院")
	org := dash(strings.TrimSpace(in.Org), "医务科")
	period := dash(strings.TrimSpace(in.Period), time.Now().Format("2006年01月"))
	dateLine := dash(strings.TrimSpace(in.DateLine), time.Now().Format("2006年01月02日"))
	docNo := dash(strings.TrimSpace(in.DocNo), "灵医合规资〔2026〕01号")

	missing := 0
	for _, row := range in.Rows {
		if qualStatusLabel(row.Status) != "已具备" {
			missing++
		}
	}

	var doc strings.Builder
	doc.WriteString(docxDocumentOpen())
	doc.WriteString(reportPara("迎检资质清单", true, 44, "center", false))
	doc.WriteString(reportPara(docNo, false, 21, "center", false))
	doc.WriteString(reportPara(fmt.Sprintf("核查周期：%s。本清单依据资质台账自动生成，共 %d 项，其中已具备 %d 项、待补 %d 项，用于卫生健康行政部门检查时快速出示。", period, len(in.Rows), len(in.Rows)-missing, missing), false, 24, "", true))

	rows := make([][]string, 0, len(in.Rows))
	for i, row := range in.Rows {
		item := strings.TrimSpace(row.Item)
		if item == "" {
			item = "—"
		}
		rows = append(rows, []string{
			strconv.Itoa(i + 1),
			item,
			dash(strings.TrimSpace(string(row.Count)), "—"),
			qualStatusLabel(row.Status),
		})
	}
	doc.WriteString(sizedTable([]string{"序号", "项目", "数量", "状态"}, rows, []int{900, 5200, 1500, 1500}))

	doc.WriteString(reportPara("说明：状态为“待补”的项目，由医务科在迎检前完成补办与归档；设备/技术授权过期的，暂停相应诊疗项目直至复审通过。", false, 21, "", true))

	doc.WriteString(`<w:p><w:pPr><w:spacing w:before="360"/></w:pPr></w:p>`)
	doc.WriteString(reportPara(tenant+"　"+org, false, 24, "right", false))
	doc.WriteString(reportPara(dateLine, false, 24, "right", false))
	doc.WriteString(reportPara("（盖章）", false, 24, "right", false))
	doc.WriteString(docxSectPr())

	header := docxHeader(tenant + " · 迎检资质清单（" + period + "）")
	return packDocx(doc.String(), header, docxFooter())
}

// ExportQualificationChecklist 处理 POST /compliance/qualifications/checklist/export。
func (h *Handler) ExportQualificationChecklist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// P2-2：导出前校验合规只读能力与租户。
	if _, _, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingRead); !ok {
		return
	}
	var in QualInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	b, err := BuildQualificationDocx(in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeDocx(w, "compliance-qualification-checklist.docx", "迎检资质清单.docx", b)
}

// ---------------------------------------------------------------------------
// 公共 docx 片段
// ---------------------------------------------------------------------------

func docxDocumentOpen() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>`
}

// docxSectPr A4 纵向 + 页眉页脚引用。
func docxSectPr() string {
	return `<w:sectPr><w:headerReference w:type="default" r:id="rId2"/><w:footerReference w:type="default" r:id="rId3"/><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1474" w:bottom="1440" w:left="1587" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:body></w:document>`
}

func docxHeader(text string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:jc w:val="right"/><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="999999"/></w:pBdr></w:pPr><w:r><w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体"/><w:sz w:val="18"/></w:rPr><w:t>` + xmlEsc(text) + `</w:t></w:r></w:p></w:hdr>`
}

// docxFooter 页脚居中页码域（PAGE）。
func docxFooter() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体"/><w:sz w:val="20"/></w:rPr><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve">PAGE</w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p></w:ftr>`
}

// quotePara 引用段落：大引号 + 浅黄底（对齐页面上的引用样式）。
func quotePara(text string) string {
	body := strings.TrimSpace(text)
	body = strings.TrimPrefix(body, "“")
	body = strings.TrimSuffix(body, "”")
	return `<w:p><w:pPr><w:spacing w:line="360" w:lineRule="auto" w:before="80" w:after="120"/><w:ind w:left="240" w:right="240"/><w:shd w:val="clear" w:color="auto" w:fill="FFF7E0"/><w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体"/><w:sz w:val="24"/></w:rPr></w:pPr><w:r><w:rPr><w:rFonts w:ascii="宋体" w:eastAsia="宋体"/><w:sz w:val="24"/></w:rPr><w:t xml:space="preserve">“` + xmlEsc(body) + `”</w:t></w:r></w:p>`
}

// keyValueTable 两列「标签 / 内容」表，无表头底色。
func keyValueTable(rows [][2]string) string {
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="8845" w:type="dxa"/><w:tblBorders>`)
	for _, edge := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		b.WriteString(`<w:` + edge + ` w:val="single" w:sz="4" w:space="0" w:color="666666"/>`)
	}
	b.WriteString(`</w:tblBorders></w:tblPr><w:tblGrid><w:gridCol w:w="1800"/><w:gridCol w:w="7045"/></w:tblGrid>`)
	for _, row := range rows {
		b.WriteString(`<w:tr><w:tc><w:tcPr><w:tcW w:w="1800" w:type="dxa"/><w:shd w:val="clear" w:color="auto" w:fill="F7F7F7"/></w:tcPr>` + reportPara(row[0], true, 21, "", false) + `</w:tc>`)
		b.WriteString(`<w:tc><w:tcPr><w:tcW w:w="7045" w:type="dxa"/></w:tcPr>` + reportPara(row[1], false, 21, "", false) + `</w:tc></w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	return b.String()
}

// sizedTable 带列宽的表格（列宽单位 twips）。
func sizedTable(cols []string, rows [][]string, widths []int) string {
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/><w:tblBorders>`)
	for _, edge := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		b.WriteString(`<w:` + edge + ` w:val="single" w:sz="4" w:space="0" w:color="666666"/>`)
	}
	b.WriteString(`</w:tblBorders></w:tblPr><w:tblGrid>`)
	for i := range cols {
		width := 1600
		if i < len(widths) {
			width = widths[i]
		}
		b.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, width))
	}
	b.WriteString(`</w:tblGrid><w:tr>`)
	for i, c := range cols {
		width := 1600
		if i < len(widths) {
			width = widths[i]
		}
		b.WriteString(fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/><w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/></w:tcPr>`, width) + reportPara(c, true, 18, "center", false) + `</w:tc>`)
	}
	b.WriteString(`</w:tr>`)
	for _, row := range rows {
		b.WriteString(`<w:tr>`)
		for i := range cols {
			width := 1600
			if i < len(widths) {
				width = widths[i]
			}
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			b.WriteString(fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/></w:tcPr>`, width) + reportPara(cell, false, 18, "", false) + `</w:tc>`)
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	return b.String()
}

// writeDocx 输出 docx 响应，附带 ASCII 与 UTF-8 两种文件名。
func writeDocx(w http.ResponseWriter, asciiName, cnName string, b []byte) {
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiName, url.PathEscape(cnName)))
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
