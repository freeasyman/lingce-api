package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultCatalogCode    = "icd10_medical_insurance_v2"
	defaultCatalogName    = "ICD-10医保2.0版"
	defaultCatalogVersion = "2.0"
)

var diagnosisCodePattern = regexp.MustCompile(`^[A-Z][0-9]{2}[.][0-9A-Z]{3,}(?:x[0-9A-Z]+)?(?:[+*])?(?:[A-Z][0-9]{2}[.][0-9A-Z]+(?:x[0-9A-Z]+)?[*+]*)*$`)
var codeFragmentPattern = regexp.MustCompile(`^[A-Z0-9.+*]+$`)

type word struct {
	Page int
	Top  float64
	Left float64
	Text string
}

type line struct {
	Page  int
	Top   float64
	Words []word
}

type icdRow struct {
	CatalogCode      string
	CatalogName      string
	CatalogVersion   string
	ChapterNo        string
	ChapterCodeRange string
	ChapterName      string
	SectionCodeRange string
	SectionName      string
	CategoryCode     string
	CategoryName     string
	SubcategoryCode  string
	SubcategoryName  string
	DiagnosisCode    string
	DiagnosisName    string
	NormalizedName   string
	SourceFile       string
	SourcePage       int
}

func main() {
	pdfPath := flag.String("pdf", "", "ICD PDF path")
	tsvPath := flag.String("tsv", "", "existing pdftotext -tsv output; optional")
	csvOut := flag.String("csv", "", "write parsed rows to CSV; optional")
	dryRun := flag.Bool("dry-run", true, "parse and print summary without importing")
	importDB := flag.Bool("import", false, "import parsed rows into DATABASE_URL")
	sample := flag.Int("sample", 20, "number of sample rows to print")
	truncate := flag.Bool("truncate", false, "delete existing rows for catalog before import")
	flag.Parse()

	if strings.TrimSpace(*pdfPath) == "" && strings.TrimSpace(*tsvPath) == "" {
		exitf("must provide -pdf or -tsv")
	}
	if *importDB {
		*dryRun = false
	}

	source, cleanup, err := ensureTSV(*pdfPath, *tsvPath)
	if err != nil {
		exitf("prepare tsv: %v", err)
	}
	defer cleanup()

	rows, err := parseTSV(source, sourceFile(*pdfPath, *tsvPath))
	if err != nil {
		exitf("parse tsv: %v", err)
	}
	if len(rows) == 0 {
		exitf("no ICD rows parsed")
	}
	if err := validateRows(rows); err != nil {
		exitf("validate rows: %v", err)
	}
	printSummary(rows, *sample)

	if *csvOut != "" {
		if err := writeCSV(*csvOut, rows); err != nil {
			exitf("write csv: %v", err)
		}
		fmt.Printf("csv_written=%s rows=%d\n", *csvOut, len(rows))
	}
	if *dryRun {
		return
	}
	if !*importDB {
		return
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		exitf("DATABASE_URL is required for -import")
	}
	if err := importRows(context.Background(), databaseURL, rows, *truncate); err != nil {
		exitf("import rows: %v", err)
	}
}

func ensureTSV(pdfPath, existingTSV string) (string, func(), error) {
	if strings.TrimSpace(existingTSV) != "" {
		return existingTSV, func() {}, nil
	}
	tmp, err := os.CreateTemp("", "emr-icd10-*.tsv")
	if err != nil {
		return "", nil, err
	}
	path := tmp.Name()
	_ = tmp.Close()
	cmd := exec.Command("pdftotext", "-tsv", pdfPath, path)
	if output, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("pdftotext -tsv failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func parseTSV(path, source string) ([]icdRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(bufio.NewReader(file))
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}
	index := map[string]int{}
	for i, name := range header {
		index[name] = i
	}
	required := []string{"level", "page_num", "left", "top", "text"}
	for _, name := range required {
		if _, ok := index[name]; !ok {
			return nil, fmt.Errorf("missing tsv column %s", name)
		}
	}

	wordsByPage := map[int][]word{}
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if field(record, index["level"]) != "5" {
			continue
		}
		text := strings.TrimSpace(field(record, index["text"]))
		if text == "" || strings.HasPrefix(text, "###") {
			continue
		}
		page, _ := strconv.Atoi(field(record, index["page_num"]))
		left, _ := strconv.ParseFloat(field(record, index["left"]), 64)
		top, _ := strconv.ParseFloat(field(record, index["top"]), 64)
		if top < 100 || top > 535 {
			continue
		}
		w := word{Page: page, Left: left, Top: top, Text: text}
		wordsByPage[page] = append(wordsByPage[page], w)
	}

	pages := make([]int, 0, len(wordsByPage))
	for page := range wordsByPage {
		pages = append(pages, page)
	}
	sort.Ints(pages)

	rows := make([]icdRow, 0, 80000)
	for _, page := range pages {
		rows = append(rows, parsePage(wordsByPage[page], page, source)...)
	}
	return rows, nil
}

func parsePage(words []word, page int, source string) []icdRow {
	sort.Slice(words, func(i, j int) bool {
		if words[i].Top == words[j].Top {
			return words[i].Left < words[j].Left
		}
		return words[i].Top < words[j].Top
	})
	codes := make([]word, 0)
	for _, item := range words {
		if item.Left < 610 || item.Left >= 690 {
			continue
		}
		code := normalizeCode(item.Text)
		if codeFragmentPattern.MatchString(code) {
			codes = append(codes, word{Page: page, Top: item.Top, Left: item.Left, Text: code})
		}
	}
	codes = mergeWrappedCodes(codes, words)
	rows := make([]icdRow, 0, len(codes))
	for _, code := range codes {
		if !diagnosisCodePattern.MatchString(code.Text) {
			continue
		}
		name := diagnosisNameForCode(words, code, codes)
		name = normalizeName(name)
		if name == "" {
			continue
		}
		rows = append(rows, icdRow{
			CatalogCode:    defaultCatalogCode,
			CatalogName:    defaultCatalogName,
			CatalogVersion: defaultCatalogVersion,
			DiagnosisCode:  code.Text,
			DiagnosisName:  name,
			NormalizedName: normalizeDiagnosisName(name),
			SourceFile:     source,
			SourcePage:     page,
		})
	}
	return rows
}

func mergeWrappedCodes(codes []word, words []word) []word {
	sort.Slice(codes, func(i, j int) bool {
		if codes[i].Top == codes[j].Top {
			return codes[i].Left < codes[j].Left
		}
		return codes[i].Top < codes[j].Top
	})
	merged := make([]word, 0, len(codes))
	for _, fragment := range codes {
		if len(merged) == 0 {
			merged = append(merged, fragment)
			continue
		}
		last := &merged[len(merged)-1]
		if fragment.Top-last.Top <= 20 && fragment.Left >= last.Left &&
			(strings.HasSuffix(last.Text, "+") || strings.HasSuffix(last.Text, "*") ||
				strings.HasSuffix(last.Text, ".")) {
			combined := last.Text + fragment.Text
			last.Text = combined
			continue
		}
		if diagnosisCodePattern.MatchString(last.Text) {
			merged = append(merged, fragment)
		} else {
			// Keep an incomplete fragment only long enough to try joining it
			// with the next wrapped fragment.
			merged[len(merged)-1] = fragment
		}
	}
	return merged
}

func diagnosisNameForCode(words []word, code word, codes []word) string {
	nameParts := make([]word, 0)
	for _, item := range words {
		if item.Left < 690 {
			continue
		}
		if strings.Contains(item.Text, "诊断名称") || (strings.Contains(item.Text, "第") && strings.Contains(item.Text, "页")) {
			continue
		}
		nearestDistance := 9999.0
		nearestTop := 0.0
		for _, candidate := range codes {
			distance := abs(item.Top - candidate.Top)
			if distance < nearestDistance {
				nearestDistance = distance
				nearestTop = candidate.Top
			}
		}
		if nearestDistance > 21.0 || nearestTop != code.Top {
			continue
		}
		nameParts = append(nameParts, item)
	}
	sort.Slice(nameParts, func(a, b int) bool {
		if nameParts[a].Top == nameParts[b].Top {
			return nameParts[a].Left < nameParts[b].Left
		}
		return nameParts[a].Top < nameParts[b].Top
	})
	var name strings.Builder
	for _, part := range nameParts {
		name.WriteString(part.Text)
	}
	return name.String()
}

func hasRowContextAt(words []word, top float64) bool {
	hasChapter := false
	hasSection := false
	for _, item := range words {
		if abs(item.Top-top) > 2.0 {
			continue
		}
		if item.Left >= 70 && item.Left < 155 {
			hasChapter = true
		}
		if item.Left >= 225 && item.Left < 405 {
			hasSection = true
		}
	}
	return hasChapter && hasSection
}

func appendWord(lines []line, w word) []line {
	for i := range lines {
		if abs(lines[i].Top-w.Top) <= 2.0 {
			lines[i].Words = append(lines[i].Words, w)
			return lines
		}
	}
	return append(lines, line{Page: w.Page, Top: w.Top, Words: []word{w}})
}

func lineCells(ln line) map[string]string {
	sort.Slice(ln.Words, func(i, j int) bool { return ln.Words[i].Left < ln.Words[j].Left })
	cells := map[string][]string{}
	for _, w := range ln.Words {
		key := columnFor(w.Left)
		if key == "" {
			continue
		}
		cells[key] = append(cells[key], w.Text)
	}
	out := map[string]string{}
	for key, parts := range cells {
		out[key] = strings.Join(parts, "")
	}
	return out
}

func columnFor(left float64) string {
	switch {
	case left >= 70 && left < 100:
		return "chapter_no"
	case left >= 100 && left < 155:
		return "chapter_code_range"
	case left >= 155 && left < 225:
		return "chapter_name"
	case left >= 225 && left < 275:
		return "section_code_range"
	case left >= 275 && left < 350:
		return "section_name"
	case left >= 350 && left < 405:
		return "category_code"
	case left >= 405 && left < 465:
		return "category_name"
	case left >= 465 && left < 520:
		return "subcategory_code"
	case left >= 520 && left < 610:
		return "subcategory_name"
	case left >= 610 && left < 690:
		return "diagnosis_code"
	case left >= 690 && left < 820:
		return "diagnosis_name"
	default:
		return ""
	}
}

func isHeaderOrFooter(cells map[string]string) bool {
	joined := strings.Join([]string{
		cells["chapter_no"], cells["chapter_code_range"], cells["chapter_name"],
		cells["diagnosis_code"], cells["diagnosis_name"],
	}, "")
	return strings.Contains(joined, "章") || strings.Contains(joined, "诊断代码") ||
		strings.Contains(joined, "诊断名称") || strings.Contains(joined, "第") && strings.Contains(joined, "页")
}

func finishRow(row *icdRow, rows *[]icdRow) {
	row.DiagnosisCode = normalizeCode(row.DiagnosisCode)
	row.DiagnosisName = normalizeName(row.DiagnosisName)
	row.NormalizedName = normalizeDiagnosisName(row.DiagnosisName)
	if row.DiagnosisCode == "" || row.DiagnosisName == "" || row.NormalizedName == "" {
		return
	}
	*rows = append(*rows, *row)
}

func validateRows(rows []icdRow) error {
	seenCode := map[string]icdRow{}
	for i, row := range rows {
		if !diagnosisCodePattern.MatchString(row.DiagnosisCode) {
			return fmt.Errorf("row %d invalid diagnosis code %q name=%q page=%d", i+1, row.DiagnosisCode, row.DiagnosisName, row.SourcePage)
		}
		if previous, ok := seenCode[row.DiagnosisCode]; ok {
			return fmt.Errorf("duplicate diagnosis code %q: first page=%d name=%q, second page=%d name=%q", row.DiagnosisCode, previous.SourcePage, previous.DiagnosisName, row.SourcePage, row.DiagnosisName)
		}
		seenCode[row.DiagnosisCode] = row
	}
	return nil
}

func printSummary(rows []icdRow, sample int) {
	names := map[string]int{}
	shortNames := make([]icdRow, 0)
	for _, row := range rows {
		names[row.NormalizedName]++
		if len([]rune(row.DiagnosisName)) <= 1 {
			shortNames = append(shortNames, row)
		}
	}
	fmt.Printf("parsed_rows=%d unique_names=%d catalog=%s\n", len(rows), len(names), defaultCatalogName)
	fmt.Printf("suspicious_short_names=%d\n", len(shortNames))
	hash := sha256.New()
	for _, row := range rows {
		_, _ = hash.Write([]byte(row.DiagnosisCode + "\t" + row.DiagnosisName + "\n"))
	}
	fmt.Printf("content_sha256=%s\n", hex.EncodeToString(hash.Sum(nil)))
	limit := sample
	if limit > len(rows) {
		limit = len(rows)
	}
	for i := 0; i < limit; i++ {
		row := rows[i]
		fmt.Printf("sample[%d] page=%d code=%s name=%s\n", i+1, row.SourcePage, row.DiagnosisCode, row.DiagnosisName)
	}
	shortLimit := sample
	if shortLimit > len(shortNames) {
		shortLimit = len(shortNames)
	}
	for i := 0; i < shortLimit; i++ {
		row := shortNames[i]
		fmt.Printf("suspicious_short[%d] page=%d code=%s name=%s\n", i+1, row.SourcePage, row.DiagnosisCode, row.DiagnosisName)
	}
}

func writeCSV(path string, rows []icdRow) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	header := []string{"catalog_code", "catalog_name", "catalog_version", "chapter_no", "chapter_code_range", "chapter_name", "section_code_range", "section_name", "category_code", "category_name", "subcategory_code", "subcategory_name", "diagnosis_code", "diagnosis_name", "normalized_name", "source_file", "source_page"}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writer.Write([]string{
			row.CatalogCode, row.CatalogName, row.CatalogVersion, row.ChapterNo, row.ChapterCodeRange, row.ChapterName,
			row.SectionCodeRange, row.SectionName, row.CategoryCode, row.CategoryName, row.SubcategoryCode, row.SubcategoryName,
			row.DiagnosisCode, row.DiagnosisName, row.NormalizedName, row.SourceFile, strconv.Itoa(row.SourcePage),
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func importRows(ctx context.Context, databaseURL string, rows []icdRow, truncate bool) error {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if truncate {
		if _, err := tx.Exec(ctx, `DELETE FROM public.emr_icd10_diagnosis_catalog WHERE catalog_code=$1`, defaultCatalogCode); err != nil {
			return err
		}
	}
	_, err = tx.CopyFrom(ctx,
		pgx.Identifier{"public", "emr_icd10_diagnosis_catalog"},
		[]string{"catalog_code", "catalog_name", "catalog_version", "chapter_no", "chapter_code_range", "chapter_name", "section_code_range", "section_name", "category_code", "category_name", "subcategory_code", "subcategory_name", "diagnosis_code", "diagnosis_name", "normalized_name", "source_file", "source_page", "imported_at"},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			row := rows[i]
			return []any{
				row.CatalogCode, row.CatalogName, row.CatalogVersion, row.ChapterNo, row.ChapterCodeRange, row.ChapterName,
				row.SectionCodeRange, row.SectionName, row.CategoryCode, row.CategoryName, row.SubcategoryCode, row.SubcategoryName,
				row.DiagnosisCode, row.DiagnosisName, row.NormalizedName, row.SourceFile, row.SourcePage, time.Now(),
			}, nil
		}),
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func normalizeCode(value string) string {
	replacer := strings.NewReplacer(" ", "", "\t", "", "\n", "", "（", "", "）", "", "，", "", ",", "")
	return strings.ToUpper(replacer.Replace(strings.TrimSpace(value)))
}

func normalizeName(value string) string {
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "\t", "")
	return strings.TrimSpace(value)
}

func normalizeDiagnosisName(value string) string {
	value = normalizeName(value)
	replacer := strings.NewReplacer("（", "(", "）", ")", "，", ",", "；", ";", "：", ":", "　", "")
	return replacer.Replace(value)
}

func sourceFile(pdfPath, tsvPath string) string {
	if pdfPath != "" {
		return pdfPath
	}
	return tsvPath
}

func field(record []string, index int) string {
	if index < 0 || index >= len(record) {
		return ""
	}
	return record[index]
}

func firstLeft(ln line) float64 {
	left := 9999.0
	for _, w := range ln.Words {
		if w.Left < left {
			left = w.Left
		}
	}
	return left
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
