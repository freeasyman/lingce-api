// his-price-import 把「派特定价体系价目表」原始 CSV（列顺序不规整）清洗后导入 HIS 收费项目，
// 作为开发库的初始收费项目（只做一次性初始数据，不做定价模板、不做倍率×城市系数）。
//
// 源表结构（表头不规整，实际每行字段数可变）：
//
//	项目,医保价,品类倍率,北京,深圳,成都,昆明,其他
//
// 有效行的通用形态是「项目名, 医保价(含项目全名与￥价), 倍率(Nx), 北京(￥价), 深圳…」，
// 但有的行带一个分区标题前缀（如 `诊费类<br>,妇科/中医初诊—知名专家,￥80,4x,￥300,…`），
// 有的行没有项目名列，把「项目全名 ￥价」整体写在医保价列里，还有纯说明行（组套子项）。
// 因此这里按「倍率列」定位：倍率所在第 m 列 → 医保价列 = m-1，项目名列 = m-2，北京列 = m+1。
// 找不到倍率列时退回宽松模式：项目名取首列，北京价取「第 2 个含￥的列」（优先带 Nx 的列）。
//
// 用法：
//
//	go run ./cmd/his-price-import \
//	  -csv ../../lingce-data/医保收费字典/解析/20261029_定价体系_价目表_raw.csv \
//	  -tenant-id 1 -dry-run -report ../tmp/price-import-report.csv
//
// 幂等：按「规范化名称」跳过已存在项目（忽略空格/全半角），并按 (tenant_id, code) ON CONFLICT DO NOTHING；
// 重复执行不会重复插入、也不会覆盖既有项目价格。
package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ---------------- 清洗规则 ----------------

var (
	// <br> / <br/> / <BR /> 等标签
	brRe = regexp.MustCompile(`(?i)<\s*/?\s*br\s*/?\s*>`)
	// 全部 markdown 粗体标记
	boldRe = regexp.MustCompile(`\*{2,}`)
	// 倍率列：4x / 3.5x / 2x
	marginRe = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?x$`)
	// ￥价，支持「￥1,200」与区间「￥50-80」（区间取低价）
	yuanRe = regexp.MustCompile(`[￥¥]\s*([0-9][0-9,]*(?:\.[0-9]+)?)(?:\s*[-–~—]\s*([0-9][0-9,]*(?:\.[0-9]+)?))?`)
	// 裸数字（无 ￥ 时兜底）
	numRe = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
	// 名称末尾的「/次」「/人次」计量后缀
	tailUnitRe = regexp.MustCompile(`\s*/\s*(?:次|人次)$`)
	// 括号里带「价」的说明片段，如「（迪安价￥140）」
	priceNoteRe = regexp.MustCompile(`[（(][^（）()]*价[^（）()]*[）)]`)
	// 项目名末尾残留的标点
	tailPunctRe = regexp.MustCompile(`[\s，,、;；:：]+$`)
)

// cleanText 去标签、去粗体、反转义 \. \- \( 等、统一空白。
func cleanText(s string) string {
	s = brRe.ReplaceAllString(s, "")
	s = boldRe.ReplaceAllString(s, "")
	// 反转义：\x → x（仅限标点，避免误伤中文）
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '.', '-', '+', '*', '(', ')', '[', ']', ',', '/', '~', '_', '^', '$':
				b.WriteByte(s[i+1])
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	s = b.String()
	s = strings.ReplaceAll(s, "\u00a0", " ") // nbsp
	return strings.Join(strings.Fields(s), " ")
}

// stripPrice 去掉描述里的价格片段，得到「医保项目全名」。
func stripPrice(desc string) string {
	d := yuanRe.ReplaceAllString(desc, "")
	d = priceNoteRe.ReplaceAllString(d, "")
	d = strings.TrimSpace(strings.Trim(d, "/·-—"))
	return strings.Join(strings.Fields(d), " ")
}

// parseYuan 从字段里取价格；优先 ￥，无 ￥ 时退化为首个数字。
// 返回 price、是否区间取低价（range）、是否解析成功。
func parseYuan(s string) (float64, bool, bool) {
	if m := yuanRe.FindStringSubmatch(s); m != nil {
		low := strings.ReplaceAll(m[1], ",", "")
		v, err := strconv.ParseFloat(low, 64)
		if err != nil {
			return 0, false, false
		}
		return v, m[2] != "", true
	}
	if m := numRe.FindString(s); m != "" {
		v, err := strconv.ParseFloat(m, 64)
		if err != nil {
			return 0, false, false
		}
		return v, false, true
	}
	return 0, false, false
}

// 分区/组套标题（不是收费项目本身）。出现在「项目名」位时，名称改从医保价描述里取。
var headingSet = map[string]bool{
	"举例":       true,
	"诊费类":      true,
	"检验类":      true,
	"检查类":      true,
	"治疗类":      true,
	"影像检查类":    true,
	"中医针灸":     true,
	"中医艾灸":     true,
	"中医推拿":     true,
	"中医拔罐":     true,
	"中医刮痧":     true,
	"耳豆压穴":     true,
	"中药热奄包":    true,
	"中药热奄包/封包": true,
	"中药熏蒸":     true,
	"中药贴敷":     true,
	"物理治疗":     true,
	"护理治疗":     true,
	"盆底康复":     true,
	"盆底肌评估":    true,
	"穴位埋线":     true,
	"中药腐蚀治疗":   true,
	"膏摩":       true,
	"微生态":      true,
	"微生态检测":    true,
	"TCT+HPV":  true,
	"药敏+培养":    true,
}

// 源表笔误/异写修正：项目名 → 规范名。
var nameAlias = map[string]string{
	"CPP": "CRP", // 源表「C反应蛋白(CPP)测定」应为 CRP
}

// isPackageName 判断是否为组套/打包项目（如「血脂四项」「传染四项」「TCT+HPV」）。
// 组套价格与其中单项医保价不是同一收费项目，因此组套不做「医保全名」去重。
func isPackageName(name string) bool {
	return strings.Contains(name, "项") || strings.Contains(name, "+") || strings.Contains(name, "套")
}

// manualSkip 人工复核后确认与既有项目重复的条目（源表项目名 → 理由）。
// 精确名称去重无法覆盖「名称不同但同一收费项目」的情况，这里显式列出，避免静默漏判。
var manualSkip = map[string]string{
	"性激素六项":     "与既有「性激素六项检查套餐」(210000037) 同一组套、同价",
	"经腹部妇科B超检查": "与既有「妇科B超」(C010) 同一检查（经腹部路径）",
}

// isHeading 判断字段是否为分区标题（标题不是收费项目）。
func isHeading(s string) bool {
	if s == "" {
		return true
	}
	if headingSet[s] {
		return true
	}
	return strings.HasSuffix(s, "类")
}

// ---------------- 分类映射 ----------------

// classify 按任务给定规则映射五大类：诊查/检验/检查/治疗/其他。
// 规则按「项目名关键词 → 分区上下文 → 其他」的优先级判定；检验先于检查，
// 保证「阴道分泌物检查」归检验、「电子阴道镜检查」归检查。
func classify(name, section string) string {
	if hasAny(name, "诊费", "初诊", "复诊", "联诊") {
		return "诊查"
	}
	if hasAny(name, "HCG", "孕酮", "血常规", "尿常规", "CRP", "血糖", "葡萄糖",
		"分泌物", "BV", "微生态", "传染", "抗体", "测定", "培养", "药敏",
		"TCT", "HPV", "细胞学", "激素", "AMH", "甲功", "血脂", "肝功", "肾功",
		"染色", "检测") {
		return "检验"
	}
	if hasAny(name, "超声", "彩超", "B超", "阴道镜", "肌电图", "盆底肌电", "POP-Q", "影像") {
		return "检查"
	}
	if hasAny(name, "针", "灸", "罐", "推拿", "埋线", "灌肠", "熏洗", "熏蒸", "热敷",
		"热奄包", "封包", "离子导入", "贴敷", "盆底修复", "生物反馈", "电刺激",
		"红光", "灌洗", "冲洗", "上药", "注射", "采血", "煎药", "刮痧", "膏摩",
		"腐蚀", "治疗", "理疗", "磁刺激") {
		return "治疗"
	}
	// 分区上下文兜底：诊费类/检验类/检查类/治疗类（含护理治疗、物理治疗、中医/中药分区…）
	switch section {
	case "诊费类":
		return "诊查"
	case "检验类", "微生态检测", "微生态":
		return "检验"
	case "检查类", "影像检查类":
		return "检查"
	case "":
		return "其他"
	}
	return "治疗"
}

func hasAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ---------------- 归一化（去重用） ----------------

// normKey 归一化用于名称比对：去空白、全角转半角、拉丁小写。
func normKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E: // 全角 ASCII
			r -= 0xFEE0
		case r == 0x3000: // 全角空格
			continue
		}
		if r == ' ' || r == '\t' || r == '\u00a0' || r == '-' || r == '－' {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// descKeys 返回医保项目全名的多个比对键：
// 全名、去括号补充（低密度脂蛋白胆固醇(LDL-C)测定 → 低密度脂蛋白胆固醇测定）、
// 再去尾缀「测定/试验/检查」（→ 低密度脂蛋白胆固醇），用于识别既有项目里的简写名。
func descKeys(desc string) []string {
	keys := []string{desc}
	bare := strings.TrimSpace(parenRe.ReplaceAllString(desc, ""))
	if bare != "" && bare != desc {
		keys = append(keys, bare)
	}
	for _, suf := range []string{"测定", "试验", "检查"} {
		if strings.HasSuffix(bare, suf) {
			if trimmed := strings.TrimSuffix(bare, suf); trimmed != "" {
				keys = append(keys, trimmed)
			}
			break
		}
	}
	return keys
}

var parenRe = regexp.MustCompile(`[（(][^（）()]*[）)]`)

// ---------------- 行解析 ----------------

type parsedItem struct {
	line     int
	name     string // 收费项目名（清洗后）
	descName string // 医保项目全名（清洗后，去价）
	category string
	unit     string
	price    float64
	priceSrc string // 北京 / 医保价
	rangeLow bool   // 源价是区间，已取低价
	section  string
	note     string
}

type skipRow struct {
	line   int
	reason string
	raw    string
}

// rawRow 是一行 CSV 的清洗后字段。
type rawRow struct {
	line   int
	fields []string
}

// readRawRows 读取并清洗每一行字段（保留原始行文本用于无法解析行清单）。
func readRawRows(path string) ([]rawRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	rows := make([]rawRow, 0, len(records))
	for i, rec := range records {
		fields := make([]string, 0, len(rec))
		for _, c := range rec {
			fields = append(fields, cleanText(c))
		}
		rows = append(rows, rawRow{line: i + 1, fields: fields})
	}
	return rows, nil
}

// parseRows 逐行解析成待导入项目；无法解析的行进入 skipped 清单（带原因）。
func parseRows(rows []rawRow) (items []parsedItem, skipped []skipRow) {
	section := ""
	for idx, row := range rows {
		if idx == 0 {
			continue // 表头
		}
		fields := row.fields
		if isEmptyRow(fields) {
			skipped = append(skipped, skipRow{row.line, "空行", joinRaw(fields)})
			continue
		}

		// 1. 先更新分区上下文（分区标题可能出现在项目名列或前置列）。
		for i := 0; i < len(fields) && i < 2; i++ {
			if headingSet[fields[i]] && fields[i] != "传染四项" {
				section = fields[i]
			}
		}

		it, reason, note := parseOne(row, section)
		if reason != "" {
			skipped = append(skipped, skipRow{row.line, reason, joinRaw(fields)})
			continue
		}
		it.note = note
		items = append(items, it)
	}
	return items, skipped
}

// parseOne 解析单行。
func parseOne(row rawRow, section string) (parsedItem, string, string) {
	fields := row.fields
	it := parsedItem{line: row.line, section: section, unit: "次"}

	// 定位倍率列 m：项目名 = m-2，医保价描述 = m-1，北京价 = m+1。
	m := -1
	for i, f := range fields {
		if marginRe.MatchString(strings.ToLower(f)) {
			m = i
			break
		}
	}

	var nameIdx int
	if m >= 2 {
		nameIdx = m - 2
		it.descName = strings.TrimSpace(stripPrice(fields[m-1]))
		it.name = fields[nameIdx]
		if m+1 < len(fields) {
			if v, isRange, ok := parseYuan(fields[m+1]); ok && v > 0 {
				it.price, it.priceSrc, it.rangeLow = v, "北京", isRange
			}
		}
	} else if m == 1 {
		// 项目名列缺失：整行只有「项目全名 ￥价, 倍率, 北京价」
		nameIdx = 0
		it.descName = stripPrice(fields[0])
		it.name = ""
		if len(fields) > 2 {
			if v, isRange, ok := parseYuan(fields[2]); ok && v > 0 {
				it.price, it.priceSrc, it.rangeLow = v, "北京", isRange
			}
		}
	} else {
		// 宽松模式（心理/陪诊等无倍率区块）：项目名取首列，北京价取第 2 个含￥的列（优先带 Nx）。
		// 首列是「心理咨询师(资深)」这类角色标签时，真实项目名在下一列。
		nameIdx = 0
		if len(fields) > 1 && strings.Contains(fields[0], "师") && !strings.Contains(fields[1], "￥") && fields[1] != "" {
			nameIdx = 1
			it.descName = stripPrice(fields[1])
			it.name = it.descName
			if v, src, isRange, ok := loosePrice(fields, nameIdx); ok {
				it.price, it.priceSrc, it.rangeLow = v, src, isRange
			}
		} else if strings.Contains(fields[0], "￥") || strings.Contains(fields[0], "¥") {
			it.descName = stripPrice(fields[0])
			it.name = it.descName
		} else {
			it.descName = fields[0]
			it.name = fields[0]
		}
		if v, src, isRange, ok := loosePrice(fields, nameIdx); ok {
			it.price, it.priceSrc, it.rangeLow = v, src, isRange
		}
	}

	// 2. 名称：为空或是分区标题时，从医保价描述里取项目全名。
	if strings.TrimSpace(it.name) == "" || isHeading(it.name) {
		if it.descName != "" {
			it.name = it.descName
			it.note = appendNote(it.note, "名称取自医保价列")
		}
	}
	it.name = sanitizeName(it.name)
	if it.descName == "" {
		it.descName = sanitizeName(it.name)
	}
	for bad, good := range nameAlias {
		if strings.Contains(it.descName, bad) {
			it.descName = strings.ReplaceAll(it.descName, bad, good)
		}
	}

	// 3. 说明性长文本（多段报价）直接跳过。
	if strings.Count(strings.Join(fields, ","), "￥") >= 3 && len([]rune(it.name)) > 40 {
		return it, "说明性长文本（多段报价，无单一项目/价格）", ""
	}

	// 4. 价格：北京价缺失时回落医保价列。
	if it.price <= 0 {
		idx := 0
		if m >= 2 {
			idx = m - 1
		}
		if v, isRange, ok := parseYuan(fields[idx]); ok && v > 0 {
			it.price, it.priceSrc, it.rangeLow = v, "医保价", isRange
			it.note = appendNote(it.note, "无北京价，回落医保价")
		}
	}
	if it.price <= 0 {
		return it, "无价（北京列与医保价列均无可解析价格）", ""
	}
	if it.rangeLow {
		it.note = appendNote(it.note, "源价为区间，取低价")
	}
	if it.priceSrc == "医保价" {
		it.note = appendNote(it.note, "医保价初始化（无北京价）")
	}

	// 5. 名称最终校验。
	if it.name == "" || len([]rune(it.name)) < 2 {
		return it, "项目名缺失", ""
	}

	if alias, ok := nameAlias[it.name]; ok {
		it.note = appendNote(it.note, "源表笔误修正 "+it.name+"→"+alias)
		it.name = alias
	}

	// 6. 单位与分类。
	if strings.Contains(it.name, "代煎") || strings.Contains(it.name, "煎药") {
		it.unit = "剂"
	}
	sec := section
	if m < 1 {
		sec = "" // 心理/陪诊等无倍率区块不继承上一分区上下文
	}
	it.category = classify(it.name, sec)
	return it, "", it.note
}

// loosePrice 宽松模式的北京价：优先带 Nx 倍率标记的列，其次第 2 个含￥列，再次第 1 个。
func loosePrice(fields []string, nameIdx int) (float64, string, bool, bool) {
	type cand struct {
		idx      int
		hasX     bool
		v        float64
		isRange  bool
		hasValue bool
	}
	var cands []cand
	for i := nameIdx + 1; i < len(fields); i++ {
		if !strings.Contains(fields[i], "￥") && !strings.Contains(fields[i], "¥") {
			continue
		}
		v, isRange, ok := parseYuan(fields[i])
		if !ok || v <= 0 {
			continue
		}
		cands = append(cands, cand{idx: i, hasX: strings.Contains(strings.ToLower(fields[i]), "x"), v: v, isRange: isRange, hasValue: true})
	}
	if len(cands) == 0 {
		return 0, "", false, false
	}
	for _, c := range cands {
		if c.hasX {
			return c.v, "北京", c.isRange, true
		}
	}
	if len(cands) >= 2 {
		return cands[1].v, "北京", cands[1].isRange, true
	}
	return cands[0].v, "医保价", cands[0].isRange, true
}

// priceFieldIndex 返回医保价列下标（价格回落时只看这一列）。
func priceFieldIndex(fields []string, m int) int {
	if m >= 2 {
		return m - 1
	}
	return 0
}

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	s = tailUnitRe.ReplaceAllString(s, "")
	s = tailPunctRe.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	// 源表存在「(每部位）」这类全半角混用，左右括号数不一致时统一为半角
	if strings.Count(s, "（") != strings.Count(s, "）") {
		s = strings.ReplaceAll(s, "（", "(")
		s = strings.ReplaceAll(s, "）", ")")
	}
	return s
}

func appendNote(old, add string) string {
	if old == "" {
		return add
	}
	return old + "；" + add
}

func isEmptyRow(fields []string) bool {
	for _, f := range fields {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

func joinRaw(fields []string) string {
	return strings.TrimSpace(strings.Join(fields, ","))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------------- 主流程 ----------------

type stats struct {
	total       int
	imported    int
	dupExisting int // 与既有项目重名（含医保全名）
	dupBatch    int // 源表内重复
	unparsed    int
}

func main() {
	csvPath := flag.String("csv", "/Users/yiliiang/Documents/lingce-data/医保收费字典/解析/20261029_定价体系_价目表_raw.csv", "派特价目表原始 CSV")
	tenantID := flag.Int64("tenant-id", 1, "租户 ID")
	dbURL := flag.String("db-url", os.Getenv("LINGCE_DEV_DB_URL"), "PostgreSQL DSN")
	dryRun := flag.Bool("dry-run", false, "只解析与比对，不写库")
	reportPath := flag.String("report", "", "把逐行解析结果写成 CSV 报告（可选）")
	codePrefix := flag.String("code-prefix", "PRC", "新项目编码前缀，编码形如 PRC0001")
	pinyinTSV := flag.String("pinyin-tsv", "", "可选：name<TAB>pinyin<TAB>initials 的拼音表，用于填充拼音检索列")
	flag.Parse()
	if *csvPath == "" || *dbURL == "" {
		fmt.Fprintln(os.Stderr, "需要 -csv 与 -db-url（或 LINGCE_DEV_DB_URL）")
		os.Exit(1)
	}

	rows, err := readRawRows(*csvPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 CSV 失败: %v\n", err)
		os.Exit(1)
	}
	items, skipped := parseRows(rows)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, *dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "连接数据库失败:", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	existingNames, existingCodes, err := loadExisting(ctx, conn, *tenantID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取既有收费项目失败:", err)
		os.Exit(1)
	}

	pinyinMap := map[string][2]string{}
	if *pinyinTSV != "" {
		pinyinMap, err = loadPinyinTSV(*pinyinTSV)
		if err != nil {
			fmt.Fprintln(os.Stderr, "读取拼音表失败:", err)
			os.Exit(1)
		}
	}

	// 去重 + 编码分配
	st := stats{total: len(items), unparsed: len(skipped)}
	seen := map[string]string{} // normKey → 项目名
	nextCode := nextCodeSeq(existingCodes, *codePrefix)
	accepted := make([]parsedItem, 0, len(items))
	codes := make([]string, 0, len(items))
	report := make([][]string, 0, len(rows))
	report = append(report, []string{"line", "name", "category", "unit", "price", "price_source", "status", "detail"})

	// 逐行给出状态，报告里能对上源文件行号。
	skippedByLine := map[int]skipRow{}
	for _, s := range skipped {
		skippedByLine[s.line] = s
	}
	for i := range items {
		it := items[i]
		key := normKey(it.name)
		descKey := normKey(it.descName)
		descKeysNorm := make([]string, 0, 3)
		for _, dk := range descKeys(it.descName) {
			if nk := normKey(dk); nk != "" && nk != key {
				descKeysNorm = append(descKeysNorm, nk)
			}
		}

		if reason, ok := manualSkip[it.name]; ok {
			st.dupExisting++
			report = append(report, reportRow(it, "skip", "人工复核跳过："+reason))
			continue
		}
		if code, ok := existingNames[key]; ok {
			st.dupExisting++
			report = append(report, reportRow(it, "skip", fmt.Sprintf("与既有项目重名（%s）", code)))
			continue
		}
		if !isPackageName(it.name) {
			matched := ""
			for _, dk := range descKeysNorm {
				if code, ok := existingNames[dk]; ok {
					matched = code
					break
				}
			}
			if matched != "" {
				st.dupExisting++
				report = append(report, reportRow(it, "skip", fmt.Sprintf("与既有项目同名（医保全名匹配 %s）", matched)))
				continue
			}
		}
		if other, ok := seen[key]; ok {
			st.dupBatch++
			report = append(report, reportRow(it, "skip", fmt.Sprintf("源表内重复（保留首条 %s）", other)))
			continue
		}
		if descKey != "" && descKey != key && !isPackageName(it.name) {
			if other, ok := seen[descKey]; ok {
				st.dupBatch++
				report = append(report, reportRow(it, "skip", fmt.Sprintf("源表内与 %s 同一医保项目", other)))
				continue
			}
		}

		code := fmt.Sprintf("%s%04d", *codePrefix, nextCode)
		nextCode++
		seen[key] = it.name
		if descKey != "" {
			seen[descKey] = it.name
		}
		accepted = append(accepted, it)
		codes = append(codes, code)
		report = append(report, reportRow(it, "import", code))
	}

	// 已跳过（无价/无法解析）的行也进报告
	skippedLines := make([]int, 0, len(skipped))
	for ln := range skippedByLine {
		skippedLines = append(skippedLines, ln)
	}
	sort.Ints(skippedLines)
	for _, ln := range skippedLines {
		s := skippedByLine[ln]
		report = append(report, []string{strconv.Itoa(s.line), "", "", "", "", "", "unparsed", s.reason + "｜" + truncate(s.raw, 80)})
	}

	// 导入
	if !*dryRun && len(accepted) > 0 {
		if err := insertItems(ctx, conn, *tenantID, accepted, codes, pinyinMap); err != nil {
			fmt.Fprintln(os.Stderr, "写入失败:", err)
			os.Exit(1)
		}
		st.imported = len(accepted)
	} else if *dryRun {
		st.imported = len(accepted)
	}

	printSummary(*dryRun, *tenantID, st, accepted, codes)
	if *reportPath != "" {
		if err := writeReport(*reportPath, report); err != nil {
			fmt.Fprintln(os.Stderr, "写报告失败:", err)
		} else {
			fmt.Printf("逐行报告：%s\n", *reportPath)
		}
	}

	// 验证：导入后租户项目总数
	var total int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM his_charge_items WHERE tenant_id=$1`, *tenantID).Scan(&total); err == nil {
		fmt.Printf("租户 %d 收费项目总数：%d\n", *tenantID, total)
	}
}

func reportRow(it parsedItem, status, detail string) []string {
	if it.note != "" {
		detail = strings.TrimSpace(detail + " " + it.note)
	}
	return []string{
		strconv.Itoa(it.line), it.name, it.category, it.unit,
		strconv.FormatFloat(it.price, 'f', 2, 64), it.priceSrc, status, detail,
	}
}

func noteInfo(it parsedItem) string {
	if it.note == "" {
		return ""
	}
	return "(" + it.note + ")"
}

func printSummary(dryRun bool, tenantID int64, st stats, accepted []parsedItem, codes []string) {
	mode := "已写入"
	if dryRun {
		mode = "dry-run（未写入）"
	}
	fmt.Printf("模式：%s  租户：%d\n", mode, tenantID)
	fmt.Printf("解析出可导入项目：%d 条（源表内重复 %d，与既有项目重复 %d，无法解析/无价 %d）\n",
		st.total, st.dupBatch, st.dupExisting, st.unparsed)
	fmt.Printf("本次新增：%d 条\n", len(accepted))

	if len(accepted) == 0 {
		return
	}
	// 分类分布
	byCat := map[string]int{}
	for _, it := range accepted {
		byCat[it.category]++
	}
	cats := make([]string, 0, len(byCat))
	for c := range byCat {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	fmt.Print("分类分布：")
	for _, c := range cats {
		fmt.Printf("%s=%d ", c, byCat[c])
	}
	fmt.Println()

	fmt.Println("抽查（前 5 条 + 后 2 条）：")
	show := append([]int{}, 0, 1, 2, 3, 4)
	if len(accepted) > 7 {
		show = append(show, len(accepted)-2, len(accepted)-1)
	}
	for _, i := range show {
		if i >= len(accepted) {
			continue
		}
		it := accepted[i]
		fmt.Printf("  %s  %-32s %-6s ¥%-9.2f 单位=%s  [%s%s]\n",
			codes[i], it.name, it.category, it.price, it.unit, it.priceSrc, noteInfo(it))
	}
}

func loadExisting(ctx context.Context, conn *pgx.Conn, tenantID int64) (map[string]string, map[string]bool, error) {
	names := map[string]string{}
	codes := map[string]bool{}
	rows, err := conn.Query(ctx, `SELECT code, name FROM his_charge_items WHERE tenant_id=$1`, tenantID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code, name string
		if err := rows.Scan(&code, &name); err != nil {
			return nil, nil, err
		}
		names[normKey(name)] = code
		codes[code] = true
	}
	return names, codes, rows.Err()
}

// nextCodeSeq 找 PRC 前缀编码已有最大序号 +1，避免重复执行时撞码。
func nextCodeSeq(existingCodes map[string]bool, prefix string) int {
	maxSeq := 0
	for code := range existingCodes {
		if !strings.HasPrefix(code, prefix) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(code, prefix))
		if err == nil && n > maxSeq {
			maxSeq = n
		}
	}
	return maxSeq + 1
}

func insertItems(ctx context.Context, conn *pgx.Conn, tenantID int64, items []parsedItem, codes []string, pinyinMap map[string][2]string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	inserted := 0
	for i, it := range items {
		py, ini := "", ""
		if v, ok := pinyinMap[it.name]; ok {
			py, ini = v[0], v[1]
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO his_charge_items (tenant_id, code, name, category, unit, price, status, pinyin, initials)
			VALUES ($1,$2,$3,$4,$5,$6,'active',$7,$8)
			ON CONFLICT (tenant_id, code) DO NOTHING`,
			tenantID, codes[i], it.name, it.category, it.unit, it.price, py, ini)
		if err != nil {
			return fmt.Errorf("%s %s: %w", codes[i], it.name, err)
		}
		inserted += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	fmt.Printf("INSERT 实际写入 %d 行\n", inserted)
	return nil
}

// loadPinyinTSV 读取 name<TAB>pinyin<TAB>initials 拼音表（由 pypinyin 预生成，可选）。
func loadPinyinTSV(path string) (map[string][2]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := map[string][2]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		m[strings.TrimSpace(parts[0])] = [2]string{strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])}
	}
	return m, sc.Err()
}

func writeReport(path string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
