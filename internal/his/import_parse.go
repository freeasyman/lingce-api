package his

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// 灵活 Excel/CSV 导入：解析、表头识别、中文别名映射、行级校验。
// 设计目标：客户拿自己的表格（表头/列顺序任意、可能有标题行或合并单元格）也能导入，
// 系统只提供空白模板，不强制客户使用固定模板。

const (
	importTargetChargeItem = "charge_item"
	importTargetDrug       = "drug"
	importTargetPurchase   = "purchase"
	importTargetMaterial   = "material"
)

const (
	importMaxFileBytes = 10 << 20 // 10MB
	importMaxRows      = 5000
	importSampleRows   = 10
	importHeaderScan   = 10
)

// ImportError 是可直接展示给用户的导入错误（文件格式/表头/行数等）。
type ImportError struct {
	Message string
}

func (e *ImportError) Error() string { return e.Message }

func importErrorf(format string, args ...any) error {
	return &ImportError{Message: fmt.Sprintf(format, args...)}
}

// ImportField 是导入目标的一个可映射字段。
type ImportField struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Required bool     `json:"required"`
	Type     string   `json:"type"` // text/number/date/status/bool
	Aliases  []string `json:"-"`    // 中文表头别名，用于自动建议列映射
}

// ImportPreview 是预览接口返回体。
type ImportPreview struct {
	Target     string         `json:"target"`
	Sheet      string         `json:"sheet"`
	HeaderRow  int            `json:"header_row"` // 1-based，便于前端提示
	Headers    []string       `json:"headers"`
	SampleRows [][]string     `json:"sample_rows"`
	Mapping    map[string]int `json:"mapping"` // 字段 key → 列索引（0-based）
	Fields     []ImportField  `json:"fields"`
	TotalRows  int            `json:"total_rows"`
	FileToken  string         `json:"file_token,omitempty"`
}

// importTargetSpec 描述一个导入目标及其字段。
type importTargetSpec struct {
	Key    string
	Label  string
	Fields []ImportField
}

var importChargeItemFields = []ImportField{
	{Key: "code", Label: "编码", Required: true, Type: "text", Aliases: []string{
		"编码", "项目编码", "收费编码", "收费项目编码", "项目代码", "代码", "编号", "院内编码", "项目编号",
	}},
	{Key: "name", Label: "名称", Required: true, Type: "text", Aliases: []string{
		"名称", "项目名称", "收费项目", "收费项目名称", "项目", "品名", "收费名称", "诊疗项目名称",
	}},
	{Key: "category", Label: "分类", Type: "text", Aliases: []string{
		"分类", "类别", "项目类别", "项目分类", "收费类别", "收费分类", "类目", "项目类型",
	}},
	{Key: "unit", Label: "单位", Type: "text", Aliases: []string{
		"单位", "计量单位", "计价单位", "规格单位",
	}},
	{Key: "price", Label: "价格", Type: "number", Aliases: []string{
		"价格", "单价", "收费价格", "收费标准", "项目单价", "金额", "售价", "零售价", "费用",
	}},
	{Key: "status", Label: "状态", Type: "status", Aliases: []string{
		"状态", "启用状态", "是否启用", "使用状态", "项目状态",
	}},
	{Key: "insurance_category", Label: "医保类别", Type: "text", Aliases: []string{
		"医保类别", "医保分类", "医保类型", "医保项目类别", "医保等级",
	}},
}

var importMaterialFields = []ImportField{
	{Key: "code", Label: "编码", Required: true, Type: "text", Aliases: []string{
		"编码", "材料编码", "项目编码", "耗材编码", "代码", "编号", "院内编码",
	}},
	{Key: "name", Label: "名称", Required: true, Type: "text", Aliases: []string{
		"名称", "材料名称", "耗材名称", "项目名称", "品名", "项目", "材料",
	}},
	{Key: "unit", Label: "单位", Type: "text", Aliases: []string{
		"单位", "计量单位", "规格单位",
	}},
	{Key: "price", Label: "价格", Type: "number", Aliases: []string{
		"价格", "单价", "金额", "售价", "零售价", "费用",
	}},
	{Key: "status", Label: "状态", Type: "status", Aliases: []string{
		"状态", "启用状态", "是否启用", "使用状态",
	}},
	{Key: "insurance_category", Label: "医保类别", Type: "text", Aliases: []string{
		"医保类别", "医保分类", "医保类型", "医保项目类别",
	}},
}

var importDrugFields = []ImportField{
	{Key: "code", Label: "编码", Required: true, Type: "text", Aliases: []string{
		"编码", "药品编码", "药品代码", "项目编码", "代码", "编号", "院内编码", "货号",
	}},
	{Key: "name", Label: "名称", Required: true, Type: "text", Aliases: []string{
		"名称", "药品名称", "药品", "品名", "商品名称", "通用名", "项目名称",
	}},
	{Key: "spec", Label: "规格", Type: "text", Aliases: []string{
		"规格", "规格型号", "型号", "包装规格", "制剂规格",
	}},
	{Key: "unit", Label: "单位", Type: "text", Aliases: []string{
		"单位", "计量单位", "包装单位", "规格单位",
	}},
	{Key: "manufacturer", Label: "生产厂家", Type: "text", Aliases: []string{
		"生产厂家", "厂家", "生产企业", "生产厂商", "厂商", "生产单位",
	}},
	{Key: "retail_price", Label: "零售价", Type: "number", Aliases: []string{
		"零售价", "零售价格", "售价", "销售价", "单价", "价格", "收费价",
	}},
	{Key: "purchase_price", Label: "进价", Type: "number", Aliases: []string{
		"进价", "采购价", "采购价格", "成本价", "成本", "进货价", "购进价",
	}},
	{Key: "drug_type", Label: "药品类型", Type: "text", Aliases: []string{
		"药品类型", "药品类别", "类型", "中西药", "药品分类", "剂型类别",
	}},
	{Key: "is_rx", Label: "是否处方药", Type: "bool", Aliases: []string{
		"是否处方药", "处方药", "处方类型", "处方标志", "rx", "otc",
	}},
}

var importPurchaseFields = []ImportField{
	{Key: "supplier", Label: "供应商", Required: true, Type: "text", Aliases: []string{
		"供应商", "供货商", "供应单位", "配送企业", "厂家", "生产企业", "供应商名称",
	}},
	{Key: "drug", Label: "药品名称/编码", Required: true, Type: "text", Aliases: []string{
		"药品名称", "药品编码", "药品", "名称", "品名", "项目名称", "商品名称", "编码", "货号",
	}},
	{Key: "quantity", Label: "数量", Required: true, Type: "number", Aliases: []string{
		"数量", "采购数量", "进货数量", "订购数量", "件数", "数量合计",
	}},
	{Key: "purchase_price", Label: "进价", Required: true, Type: "number", Aliases: []string{
		"进价", "采购价", "采购价格", "进货价", "单价", "价格", "成本价",
	}},
	{Key: "batch_no", Label: "批号", Type: "text", Aliases: []string{
		"批号", "生产批号", "批次", "批次号", "批号/序列号",
	}},
	{Key: "expiry_date", Label: "效期", Type: "date", Aliases: []string{
		"效期", "有效期", "有效期至", "效期至", "到期日期", "失效日期", "截止日期",
	}},
}

var importTargetSpecs = map[string]importTargetSpec{
	importTargetChargeItem: {Key: importTargetChargeItem, Label: "收费项目", Fields: importChargeItemFields},
	importTargetMaterial:   {Key: importTargetMaterial, Label: "材料", Fields: importMaterialFields},
	importTargetDrug:       {Key: importTargetDrug, Label: "药品", Fields: importDrugFields},
	importTargetPurchase:   {Key: importTargetPurchase, Label: "采购单", Fields: importPurchaseFields},
}

func importSpec(target string) (importTargetSpec, bool) {
	spec, ok := importTargetSpecs[strings.TrimSpace(target)]
	return spec, ok
}

// ImportTargetFields 返回目标的字段定义（模板下载与测试复用）。
func ImportTargetFields(target string) ([]ImportField, bool) {
	spec, ok := importSpec(target)
	if !ok {
		return nil, false
	}
	fields := make([]ImportField, len(spec.Fields))
	copy(fields, spec.Fields)
	return fields, true
}

// importSheet 是一个已读取的工作表。
type importSheet struct {
	Name string
	Rows [][]string
}

// importRow 保留原始行号（1-based，含表头偏移），用于错误定位。
type importRow struct {
	Number int
	Cells  []string
}

// parseImportFile 解析 .xlsx / .csv；.xls 明确提示另存为 xlsx。
func parseImportFile(filename string, data []byte) ([]importSheet, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx":
		return parseXLSX(data)
	case ".csv":
		return parseCSV(data)
	case ".xls":
		return nil, importErrorf("暂不支持旧版 .xls 格式，请在 Excel 中另存为 .xlsx 后重新上传")
	case "":
		return nil, importErrorf("无法识别文件类型，请上传 .xlsx 或 .csv 文件")
	default:
		return nil, importErrorf("不支持的文件类型 %s，请上传 .xlsx 或 .csv 文件", strings.ToLower(filepath.Ext(filename)))
	}
}

func parseXLSX(data []byte) ([]importSheet, error) {
	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, importErrorf("Excel 文件无法打开，请确认文件未加密且格式正确")
	}
	defer file.Close()

	sheets := make([]importSheet, 0)
	for _, name := range file.GetSheetList() {
		rows, err := file.GetRows(name)
		if err != nil {
			return nil, importErrorf("读取工作表「%s」失败，请确认文件格式正确", name)
		}
		if !hasAnyCell(rows) {
			continue
		}
		sheets = append(sheets, importSheet{Name: name, Rows: trimSheetRows(rows)})
	}
	if len(sheets) == 0 {
		return nil, importErrorf("文件中没有可读取的数据")
	}
	return sheets, nil
}

func parseCSV(data []byte) ([]importSheet, error) {
	content, err := decodeCSVBytes(data)
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(bytes.NewReader(content))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true
	reader.Comma = sniffDelimiter(content)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, importErrorf("CSV 解析失败，请检查引号与分隔符：%v", err)
	}
	if !hasAnyCell(rows) {
		return nil, importErrorf("文件中没有可读取的数据")
	}
	return []importSheet{{Name: "CSV", Rows: trimSheetRows(rows)}}, nil
}

// decodeCSVBytes 兼容 Excel 导出的 UTF-8 BOM 与中文 Windows 常见的 GB18030 编码。
func decodeCSVBytes(data []byte) ([]byte, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(data) {
		return data, nil
	}
	decoded, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), data)
	if err != nil {
		return nil, importErrorf("CSV 编码无法识别，请另存为 UTF-8 或 GBK 编码的 CSV")
	}
	return decoded, nil
}

// sniffDelimiter 按首行出现频率判断逗号/制表符/分号。
func sniffDelimiter(content []byte) rune {
	line := content
	if index := bytes.IndexByte(content, '\n'); index >= 0 {
		line = content[:index]
	}
	counts := map[rune]int{',': 0, '\t': 0, ';': 0}
	for _, r := range string(line) {
		if _, ok := counts[r]; ok {
			counts[r]++
		}
	}
	best := ','
	bestCount := 0
	for _, r := range []rune{',', '\t', ';'} {
		if counts[r] > bestCount {
			best = r
			bestCount = counts[r]
		}
	}
	return best
}

func hasAnyCell(rows [][]string) bool {
	for _, row := range rows {
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				return true
			}
		}
	}
	return false
}

// trimSheetRows 去掉尾部全空行/列，保留中间空行（行号必须与原始文件一致）。
func trimSheetRows(rows [][]string) [][]string {
	last := len(rows) - 1
	for last >= 0 && rowEmpty(rows[last]) {
		last--
	}
	if last < 0 {
		return nil
	}
	trimmed := rows[:last+1]
	width := 0
	for _, row := range trimmed {
		end := len(row) - 1
		for end >= 0 && strings.TrimSpace(row[end]) == "" {
			end--
		}
		if end+1 > width {
			width = end + 1
		}
	}
	out := make([][]string, len(trimmed))
	for index, row := range trimmed {
		cells := make([]string, width)
		copy(cells, row[:minInt(len(row), width)])
		out[index] = cells
	}
	return out
}

func rowEmpty(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// buildImportPreview 从工作表中选择数据最多的一张，识别表头并生成建议映射。
func buildImportPreview(sheets []importSheet, target string) (ImportPreview, []importRow, error) {
	spec, ok := importSpec(target)
	if !ok {
		return ImportPreview{}, nil, importErrorf("不支持的导入目标：%s", target)
	}

	var (
		chosen      *importSheet
		headerIndex int
		dataCount   int
	)
	for index := range sheets {
		sheet := &sheets[index]
		header := detectHeaderRow(sheet.Rows, spec)
		if header < 0 {
			continue
		}
		count := 0
		for _, row := range sheet.Rows[header+1:] {
			if !rowEmpty(row) {
				count++
			}
		}
		if count > importMaxRows {
			return ImportPreview{}, nil, importErrorf("工作表「%s」有 %d 行数据，超过单次导入上限 %d 行，请拆分后导入", sheet.Name, count, importMaxRows)
		}
		// 选择数据行最多的工作表；同分时保留靠前的工作表。
		if chosen == nil || count > dataCount {
			chosen = sheet
			headerIndex = header
			dataCount = count
		}
	}
	if chosen == nil {
		return ImportPreview{}, nil, importErrorf("未识别到表头行，请确认文件包含表头（前 10 行内），或下载空白模板参考列名")
	}

	header := chosen.Rows[headerIndex]
	headers := buildHeaders(header, chosen.Rows[headerIndex+1:])
	mapping := suggestMapping(spec, headers)

	dataRows := make([]importRow, 0, dataCount)
	sample := make([][]string, 0, importSampleRows)
	for offset, row := range chosen.Rows[headerIndex+1:] {
		if rowEmpty(row) {
			continue
		}
		cells := make([]string, len(headers))
		for column := range headers {
			if column < len(row) {
				cells[column] = strings.TrimSpace(row[column])
			}
		}
		dataRows = append(dataRows, importRow{Number: headerIndex + offset + 2, Cells: cells})
		if len(sample) < importSampleRows {
			sample = append(sample, cells)
		}
	}

	fields := make([]ImportField, len(spec.Fields))
	copy(fields, spec.Fields)
	return ImportPreview{
		Target:     spec.Key,
		Sheet:      chosen.Name,
		HeaderRow:  headerIndex + 1,
		Headers:    headers,
		SampleRows: sample,
		Mapping:    mapping,
		Fields:     fields,
		TotalRows:  len(dataRows),
	}, dataRows, nil
}

// buildHeaders 按本表最大列宽补齐表头；空表头命名为「列N」，重复表头加序号。
func buildHeaders(header []string, dataRows [][]string) []string {
	width := len(header)
	for _, row := range dataRows {
		if len(row) > width {
			width = len(row)
		}
	}
	headers := make([]string, width)
	seen := make(map[string]int, width)
	for index := 0; index < width; index++ {
		raw := ""
		if index < len(header) {
			raw = strings.TrimSpace(header[index])
		}
		if raw == "" {
			raw = fmt.Sprintf("列%d", index+1)
		}
		seen[raw]++
		if seen[raw] > 1 {
			raw = fmt.Sprintf("%s(%d)", raw, seen[raw])
		}
		headers[index] = raw
	}
	return headers
}

// detectHeaderRow 在前 10 行中选择最像表头的一行：
// 非空单元格多、含文字、命中字段别名、且数字单元格较少。数据行通常数字更多，因此不会被误选。
func detectHeaderRow(rows [][]string, spec importTargetSpec) int {
	best := -1
	bestScore := math.MinInt
	limit := minInt(len(rows), importHeaderScan)
	for index := 0; index < limit; index++ {
		row := rows[index]
		nonEmpty, textCells, numericCells := 0, 0, 0
		for _, cell := range row {
			value := strings.TrimSpace(cell)
			if value == "" {
				continue
			}
			nonEmpty++
			if containsLetter(value) {
				textCells++
			}
			if looksNumeric(value) {
				numericCells++
			}
		}
		if nonEmpty < 2 || textCells == 0 {
			continue
		}
		aliasHits := aliasHitCount(spec, row)
		score := nonEmpty*2 + textCells*2 + aliasHits*15 - numericCells*2
		if score > bestScore {
			best = index
			bestScore = score
		}
	}
	return best
}

func containsLetter(value string) bool {
	for _, r := range value {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func looksNumeric(value string) bool {
	cleaned := strings.NewReplacer(",", "", "¥", "", "￥", "", "元", "").Replace(strings.TrimSpace(value))
	if cleaned == "" {
		return false
	}
	_, err := strconv.ParseFloat(cleaned, 64)
	return err == nil
}

// aliasHitCount 统计一行中有多少个单元格命中字段别名（精确或包含）。
func aliasHitCount(spec importTargetSpec, row []string) int {
	hits := 0
	for _, cell := range row {
		normalized := normalizeHeaderText(cell)
		if normalized == "" {
			continue
		}
		matched := false
		for _, field := range spec.Fields {
			for _, alias := range field.Aliases {
				if headerAliasMatch(normalized, normalizeHeaderText(alias)) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			hits++
		}
	}
	return hits
}

func headerAliasMatch(header, alias string) bool {
	if header == "" || alias == "" {
		return false
	}
	return header == alias || strings.Contains(header, alias) || strings.Contains(alias, header)
}

// normalizeHeaderText 统一大小写并去掉空白与常见标点，便于中文别名匹配。
func normalizeHeaderText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsSpace(r) {
			continue
		}
		switch r {
		case '(', ')', '（', '）', '_', '-', '/', '\\', ':', '：', '.', '。', '*', '【', '】', '[', ']', '、', '，', ',', '"', '\'', '“', '”':
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

// suggestMapping 用别名给每个字段推荐一列；字段与列都是一对一，避免重复占用。
func suggestMapping(spec importTargetSpec, headers []string) map[string]int {
	type candidate struct {
		field string
		index int
		score int
	}
	candidates := make([]candidate, 0)
	for _, field := range spec.Fields {
		normalizedAliases := make([]string, 0, len(field.Aliases))
		for _, alias := range field.Aliases {
			if normalized := normalizeHeaderText(alias); normalized != "" {
				normalizedAliases = append(normalizedAliases, normalized)
			}
		}
		for index, header := range headers {
			normalized := normalizeHeaderText(header)
			if normalized == "" {
				continue
			}
			best := 0
			for _, alias := range normalizedAliases {
				switch {
				case normalized == alias:
					best = maxInt(best, 1000+utf8.RuneCountInString(alias))
				case strings.Contains(normalized, alias):
					best = maxInt(best, 500+utf8.RuneCountInString(alias))
				case strings.Contains(alias, normalized) && utf8.RuneCountInString(normalized) >= 2:
					best = maxInt(best, 200+utf8.RuneCountInString(normalized))
				}
			}
			if best > 0 {
				candidates = append(candidates, candidate{field: field.Key, index: index, score: best})
			}
		}
	}
	// 高分优先；同分时按字段声明顺序、列顺序稳定选择。
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	mapping := make(map[string]int)
	usedColumns := make(map[int]struct{})
	for _, item := range candidates {
		if _, exists := mapping[item.field]; exists {
			continue
		}
		if _, used := usedColumns[item.index]; used {
			continue
		}
		mapping[item.field] = item.index
		usedColumns[item.index] = struct{}{}
	}
	return mapping
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// mappedValues 按 mapping 把一行单元格转换为「字段 → 去空格文本」。
func mappedValues(mapping map[string]int, cells []string) map[string]string {
	values := make(map[string]string, len(mapping))
	for field, index := range mapping {
		if index < 0 || index >= len(cells) {
			values[field] = ""
			continue
		}
		values[field] = strings.TrimSpace(cells[index])
	}
	return values
}

// importChargeItemValues 是校验后的收费项目/材料行。
type importChargeItemValues struct {
	Row               int
	Code              string
	Name              string
	Category          string
	Unit              string
	Price             float64
	Status            string
	InsuranceCategory string
}

// parseChargeItemValues 校验收费项目/材料行；材料分类固定为「材料」。
func parseChargeItemValues(values map[string]string, row int, target string) (importChargeItemValues, error) {
	item := importChargeItemValues{
		Row:               row,
		Code:              strings.TrimSpace(values["code"]),
		Name:              strings.TrimSpace(values["name"]),
		Category:          strings.TrimSpace(values["category"]),
		Unit:              strings.TrimSpace(values["unit"]),
		Status:            "active",
		InsuranceCategory: strings.TrimSpace(values["insurance_category"]),
	}
	if item.Code == "" {
		return item, fmt.Errorf("编码不能为空")
	}
	if item.Name == "" {
		return item, fmt.Errorf("名称不能为空")
	}
	price, err := parseImportPrice(values["price"])
	if err != nil {
		return item, fmt.Errorf("价格格式不正确：%s", values["price"])
	}
	item.Price = price
	status, err := normalizeImportStatus(values["status"])
	if err != nil {
		return item, err
	}
	item.Status = status
	if target == importTargetMaterial {
		item.Category = "材料"
		if item.Unit == "" {
			item.Unit = "个"
		}
	} else {
		if item.Category == "" {
			item.Category = "其他"
		}
		if item.Unit == "" {
			if item.Category == "材料" {
				item.Unit = "个"
			} else {
				item.Unit = "次"
			}
		}
	}
	return item, nil
}

// importDrugValues 是校验后的药品行。
type importDrugValues struct {
	Row           int
	Req           CreateDrugRequest
	DrugTypeLabel string
}

// parseDrugValues 校验药品行，并把中文类型/处方标识转换为后端枚举。
func parseDrugValues(values map[string]string, row int) (importDrugValues, error) {
	code := strings.TrimSpace(values["code"])
	name := strings.TrimSpace(values["name"])
	if code == "" {
		return importDrugValues{}, fmt.Errorf("编码不能为空")
	}
	if name == "" {
		return importDrugValues{}, fmt.Errorf("名称不能为空")
	}
	retail, err := parseImportPrice(values["retail_price"])
	if err != nil {
		return importDrugValues{}, fmt.Errorf("零售价格式不正确：%s", values["retail_price"])
	}
	purchase, err := parseImportPrice(values["purchase_price"])
	if err != nil {
		return importDrugValues{}, fmt.Errorf("进价格式不正确：%s", values["purchase_price"])
	}
	drugType, label, err := normalizeImportDrugType(values["drug_type"])
	if err != nil {
		return importDrugValues{}, err
	}
	isRx, err := parseImportBool(values["is_rx"], true)
	if err != nil {
		return importDrugValues{}, fmt.Errorf("是否处方药无法识别：%s", values["is_rx"])
	}
	return importDrugValues{
		Row: row,
		Req: CreateDrugRequest{
			Code:          code,
			Name:          name,
			Spec:          strings.TrimSpace(values["spec"]),
			Unit:          strings.TrimSpace(values["unit"]),
			Manufacturer:  strings.TrimSpace(values["manufacturer"]),
			DrugType:      drugType,
			RetailPrice:   retail,
			PurchasePrice: purchase,
			IsRx:          &isRx,
		},
		DrugTypeLabel: label,
	}, nil
}

// importPurchaseValues 是校验后的采购单行。
type importPurchaseValues struct {
	Row           int
	Supplier      string
	DrugKey       string
	Quantity      float64
	PurchasePrice float64
	BatchNo       string
	ExpiryDate    *string
}

func parsePurchaseValues(values map[string]string, row int) (importPurchaseValues, error) {
	item := importPurchaseValues{
		Row:      row,
		Supplier: strings.TrimSpace(values["supplier"]),
		DrugKey:  strings.TrimSpace(values["drug"]),
		BatchNo:  strings.TrimSpace(values["batch_no"]),
	}
	if item.Supplier == "" {
		return item, fmt.Errorf("供应商不能为空")
	}
	if item.DrugKey == "" {
		return item, fmt.Errorf("药品名称/编码不能为空")
	}
	quantity, err := parseImportPrice(values["quantity"])
	if err != nil || quantity <= 0 {
		return item, fmt.Errorf("数量必须是大于 0 的数字：%s", values["quantity"])
	}
	item.Quantity = quantity
	if strings.TrimSpace(values["purchase_price"]) == "" {
		return item, fmt.Errorf("进价不能为空")
	}
	price, err := parseImportPrice(values["purchase_price"])
	if err != nil {
		return item, fmt.Errorf("进价格式不正确：%s", values["purchase_price"])
	}
	item.PurchasePrice = price
	if raw := strings.TrimSpace(values["expiry_date"]); raw != "" {
		date, err := parseImportDate(raw)
		if err != nil {
			return item, err
		}
		item.ExpiryDate = &date
	}
	return item, nil
}

func parseImportPrice(raw string) (float64, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return 0, nil
	}
	cleaned = strings.NewReplacer(",", "", "，", "", "¥", "", "￥", "", "元", "", " ", "").Replace(cleaned)
	value, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, fmt.Errorf("invalid price")
	}
	return math.Round(value*100) / 100, nil
}

func normalizeImportStatus(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", "active", "启用", "正常", "可用", "是", "有效", "1", "true", "on":
		return "active", nil
	case "inactive", "停用", "禁用", "停止", "否", "无效", "0", "false", "off":
		return "inactive", nil
	default:
		return "", fmt.Errorf("状态只能是「启用」或「停用」：%s", raw)
	}
}

func normalizeImportDrugType(raw string) (string, string, error) {
	value := strings.TrimSpace(raw)
	switch strings.ToLower(value) {
	case "", "western", "西药", "化学药", "化学药品":
		return "western", "西药", nil
	case "chinese_patent", "中成药", "中成":
		return "chinese_patent", "中成药", nil
	case "chinese_herb", "中药饮片", "中草药", "中药":
		return "chinese_herb", "中药饮片", nil
	case "supplies", "耗材", "材料", "卫生材料":
		return "supplies", "耗材", nil
	default:
		return "", "", fmt.Errorf("药品类型无法识别（支持西药/中成药/中药饮片/耗材）：%s", raw)
	}
}

func parseImportBool(raw string, fallback bool) (bool, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "":
		return fallback, nil
	case "是", "处方药", "处方", "true", "1", "yes", "y", "rx":
		return true, nil
	case "否", "非处方药", "非处方", "otc", "false", "0", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool")
	}
}

// parseImportDate 支持常见日期格式与 Excel 序列号（1900 日期系统）。
func parseImportDate(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("效期不能为空")
	}
	layouts := []string{
		"2006-01-02", "2006/01/02", "2006.01.02", "2006年1月2日",
		"20060102", "2006-1-2", "2006/1/2", "01/02/2006", "1/2/2006",
		"2006-01-02 15:04:05", "2006/01/02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed.Format("2006-01-02"), nil
		}
	}
	if serial, err := strconv.ParseFloat(value, 64); err == nil {
		if serial > 20000 && serial < 80000 {
			base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.Local)
			return base.AddDate(0, 0, int(serial)).Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("效期格式不正确（示例：2027-12-31 或 2027/12/31）：%s", raw)
}

// checkDuplicateCode 统一编码查重（文件内 + 系统中已存在）。
func checkDuplicateCode(code string, existing map[string]struct{}) error {
	key := strings.TrimSpace(code)
	if _, ok := existing[key]; ok {
		return fmt.Errorf("编码 %s 已存在（系统或本次文件中重复）", key)
	}
	existing[key] = struct{}{}
	return nil
}
