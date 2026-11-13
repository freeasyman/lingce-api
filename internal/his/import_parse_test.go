package his

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func TestDetectHeaderRowSkipsTitleAndData(t *testing.T) {
	rows := [][]string{
		{"客户收费项目导入表"},
		{"项目名称", "单价（元）", "类别", "项目编码"},
		{"血常规", "25.00", "检验", "C001"},
		{"尿常规", "18.00", "检验", "C002"},
	}
	spec, _ := importSpec(importTargetChargeItem)
	if got := detectHeaderRow(rows, spec); got != 1 {
		t.Fatalf("header row = %d, want 1", got)
	}
}

func TestSuggestMappingChineseAliases(t *testing.T) {
	spec, _ := importSpec(importTargetChargeItem)
	headers := []string{"项目名称", "单价（元）", "类别", "项目编码", "备注"}
	mapping := suggestMapping(spec, headers)
	want := map[string]int{"name": 0, "price": 1, "category": 2, "code": 3}
	for field, index := range want {
		got, ok := mapping[field]
		if !ok || got != index {
			t.Fatalf("mapping[%s] = %d (ok=%v), want %d; full=%v", field, got, ok, index, mapping)
		}
	}
	if _, ok := mapping["unit"]; ok {
		t.Fatalf("unit should not be mapped: %v", mapping)
	}
}

func TestBuildPreviewArbitraryHeaders(t *testing.T) {
	sheets := []importSheet{{Name: "客户表", Rows: [][]string{
		{"客户自己的收费项目表"},
		{"项目名称", "单价（元）", "类别", "项目编码"},
		{"一次性窥器", "0", "材料", "MAT0001"},
		{"艾条", "12.5", "材料", "MAT0002"},
	}}}
	preview, rows, err := buildImportPreview(sheets, importTargetChargeItem)
	if err != nil {
		t.Fatal(err)
	}
	if preview.HeaderRow != 2 || preview.Sheet != "客户表" {
		t.Fatalf("preview header/sheet = %d/%s", preview.HeaderRow, preview.Sheet)
	}
	if len(rows) != 2 || rows[0].Number != 3 || rows[1].Number != 4 {
		t.Fatalf("rows = %+v", rows)
	}
	if preview.Mapping["name"] != 0 || preview.Mapping["price"] != 1 || preview.Mapping["category"] != 2 || preview.Mapping["code"] != 3 {
		t.Fatalf("mapping = %v", preview.Mapping)
	}
	if preview.Mapping["category"] != 2 {
		t.Fatalf("category mapping = %v", preview.Mapping)
	}
	if len(preview.Headers) != 4 || preview.Headers[0] != "项目名称" {
		t.Fatalf("headers = %v", preview.Headers)
	}
}

func TestParseCSVWithBOMAndTitleRow(t *testing.T) {
	content := "\ufeff客户导入表\n项目名称,单价,类别,编码\n一次性垫单,2.5,材料,MAT0100\n"
	sheets, err := parseImportFile("客户.csv", []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	preview, rows, err := buildImportPreview(sheets, importTargetMaterial)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TotalRows != 1 || len(rows) != 1 {
		t.Fatalf("total = %d rows=%d", preview.TotalRows, len(rows))
	}
	if preview.Mapping["code"] != 3 || preview.Mapping["name"] != 0 || preview.Mapping["price"] != 1 {
		t.Fatalf("mapping = %v", preview.Mapping)
	}
}

func TestParseCSVGB18030(t *testing.T) {
	utf8Content := "项目名称,单价,类别,编码\n一次性医用手套,1.2,材料,MAT0101\n"
	encoded, _, err := transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), []byte(utf8Content))
	if err != nil {
		t.Fatal(err)
	}
	sheets, err := parseImportFile("gbk.csv", encoded)
	if err != nil {
		t.Fatal(err)
	}
	preview, _, err := buildImportPreview(sheets, importTargetMaterial)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Headers[0] != "项目名称" || preview.TotalRows != 1 {
		t.Fatalf("gbk preview = %+v", preview)
	}
}

func TestParseXLSXMergedTitleAndSemicolonNotNeeded(t *testing.T) {
	file := excelize.NewFile()
	sheet := file.GetSheetName(0)
	_ = file.SetCellValue(sheet, "A1", "客户收费项目导入表")
	_ = file.MergeCell(sheet, "A1", "C1")
	_ = file.SetCellValue(sheet, "A2", "项目名称")
	_ = file.SetCellValue(sheet, "B2", "单价")
	_ = file.SetCellValue(sheet, "C2", "类别")
	_ = file.SetCellValue(sheet, "A3", "生理盐水")
	_ = file.SetCellValue(sheet, "B3", 3.5)
	_ = file.SetCellValue(sheet, "C3", "材料")
	buffer, err := file.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	sheets, err := parseImportFile("客户.xlsx", buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	preview, rows, err := buildImportPreview(sheets, importTargetChargeItem)
	if err != nil {
		t.Fatal(err)
	}
	if preview.HeaderRow != 2 {
		t.Fatalf("header row = %d, want 2; headers=%v", preview.HeaderRow, preview.Headers)
	}
	if len(rows) != 1 || rows[0].Cells[0] != "生理盐水" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestParseImportFileRejectsXLS(t *testing.T) {
	_, err := parseImportFile("old.xls", []byte("whatever"))
	if err == nil || !strings.Contains(err.Error(), "另存为 .xlsx") {
		t.Fatalf("xls error = %v", err)
	}
}

func TestParseImportFileRejectsUnknownExtension(t *testing.T) {
	_, err := parseImportFile("data.pdf", []byte("whatever"))
	if err == nil || !strings.Contains(err.Error(), "不支持的文件类型") {
		t.Fatalf("unknown extension error = %v", err)
	}
}

func TestParseChargeItemValues(t *testing.T) {
	item, err := parseChargeItemValues(map[string]string{
		"code": "C001", "name": "一次性窥器", "category": "妇科", "unit": "", "price": "¥12.50", "status": "停用",
		"insurance_category": "甲类",
	}, 3, importTargetMaterial)
	if err != nil {
		t.Fatal(err)
	}
	if item.Category != "材料" || item.Unit != "个" || item.Price != 12.5 || item.Status != "inactive" || item.InsuranceCategory != "甲类" {
		t.Fatalf("item = %+v", item)
	}

	cases := []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"missing name", map[string]string{"code": "C1"}, "名称不能为空"},
		{"missing code", map[string]string{"name": "X"}, "编码不能为空"},
		{"bad price", map[string]string{"code": "C1", "name": "X", "price": "abc"}, "价格格式不正确"},
		{"negative price", map[string]string{"code": "C1", "name": "X", "price": "-1"}, "价格格式不正确"},
		{"bad status", map[string]string{"code": "C1", "name": "X", "status": "未知"}, "状态只能是"},
	}
	for _, tc := range cases {
		_, err := parseChargeItemValues(tc.values, 2, importTargetChargeItem)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want contains %q", tc.name, err, tc.want)
		}
	}
}

func TestParseDrugValues(t *testing.T) {
	drug, err := parseDrugValues(map[string]string{
		"code": "D001", "name": "阿莫西林", "spec": "0.25g*24粒", "unit": "盒", "manufacturer": "某药厂",
		"retail_price": "18.5", "purchase_price": "12", "drug_type": "西药", "is_rx": "OTC",
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if drug.Req.DrugType != "western" || drug.Req.IsRx == nil || *drug.Req.IsRx {
		t.Fatalf("drug = %+v", drug)
	}
	if drug.Req.RetailPrice != 18.5 || drug.Req.PurchasePrice != 12 {
		t.Fatalf("prices = %+v", drug.Req)
	}
	if _, err := parseDrugValues(map[string]string{"code": "D002", "name": "X", "drug_type": "奇怪类型"}, 2); err == nil {
		t.Fatal("unknown drug type should fail")
	}
	if _, err := parseDrugValues(map[string]string{"code": "D003", "name": "X", "is_rx": "也许"}, 2); err == nil {
		t.Fatal("unknown is_rx should fail")
	}
}

func TestParsePurchaseValuesAndDate(t *testing.T) {
	item, err := parsePurchaseValues(map[string]string{
		"supplier": "某医药公司", "drug": "D001", "quantity": "10", "purchase_price": "12.5",
		"batch_no": "B2026", "expiry_date": "2027/12/31",
	}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if item.ExpiryDate == nil || *item.ExpiryDate != "2027-12-31" {
		t.Fatalf("expiry = %v", item.ExpiryDate)
	}
	if item.Quantity != 10 || item.PurchasePrice != 12.5 {
		t.Fatalf("item = %+v", item)
	}
	if _, err := parsePurchaseValues(map[string]string{"supplier": "S", "drug": "D", "quantity": "0", "purchase_price": "1"}, 2); err == nil {
		t.Fatal("zero quantity should fail")
	}
	if _, err := parsePurchaseValues(map[string]string{"supplier": "S", "drug": "D", "quantity": "1", "purchase_price": "1", "expiry_date": "not-a-date"}, 2); err == nil {
		t.Fatal("bad expiry should fail")
	}
	if _, err := parsePurchaseValues(map[string]string{"supplier": "S", "drug": "D", "quantity": "1"}, 2); err == nil {
		t.Fatal("missing purchase price should fail")
	}
	if got, err := parseImportDate("45992"); err != nil || got != "2025-12-01" {
		t.Fatalf("excel serial = %s, %v", got, err)
	}
}

func TestDuplicateCodeDetection(t *testing.T) {
	existing := map[string]struct{}{"C001": {}}
	if err := checkDuplicateCode("C001", existing); err == nil {
		t.Fatal("system duplicate should fail")
	}
	if err := checkDuplicateCode("C002", existing); err != nil {
		t.Fatal(err)
	}
	if err := checkDuplicateCode("C002", existing); err == nil {
		t.Fatal("in-file duplicate should fail")
	}
}

func TestValidateImportMapping(t *testing.T) {
	if err := ValidateImportMapping(importTargetChargeItem, map[string]int{"code": 0, "name": 1}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateImportMapping(importTargetChargeItem, map[string]int{"code": 0}); err == nil {
		t.Fatal("missing name should fail")
	}
	if err := ValidateImportMapping(importTargetChargeItem, map[string]int{"code": 0, "name": 1, "unknown": 2}); err == nil {
		t.Fatal("unknown field should fail")
	}
	if err := ValidateImportMapping(importTargetChargeItem, map[string]int{"code": 0, "name": 0}); err == nil {
		t.Fatal("duplicate column should fail")
	}
	if err := ValidateImportMapping("unknown", map[string]int{}); err == nil {
		t.Fatal("unknown target should fail")
	}
}

func TestImportTemplateCSV(t *testing.T) {
	data, err := ImportTemplateCSV(importTargetMaterial)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("template missing UTF-8 BOM")
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	if !strings.Contains(text, "编码") || !strings.Contains(text, "医保类别") || strings.Contains(text, "分类") {
		t.Fatalf("material template = %q", text)
	}
	if _, err := ImportTemplateCSV("unknown"); err == nil {
		t.Fatal("unknown template target should fail")
	}
}

func TestImportRowLimit(t *testing.T) {
	rows := make([][]string, 0, importMaxRows+2)
	rows = append(rows, []string{"编码", "名称"})
	for index := 0; index < importMaxRows+1; index++ {
		rows = append(rows, []string{"C" + strings.Repeat("0", 4), "项目"})
	}
	_, _, err := buildImportPreview([]importSheet{{Name: "S", Rows: rows}}, importTargetChargeItem)
	if err == nil || !strings.Contains(err.Error(), "超过单次导入上限") {
		t.Fatalf("row limit err = %v", err)
	}
}
