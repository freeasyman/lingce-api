package his

import (
	"errors"
	"testing"
)

func orderLinesTestPtr(value int64) *int64 { return &value }

// TestValidateOrderLineInputsRegistrationFee registration_fee 行与其他行混合时校验通过。
func TestValidateOrderLineInputsRegistrationFee(t *testing.T) {
	valid := []OrderLineInput{
		{ItemType: "registration_fee", Quantity: 1},
		{ItemType: "registration_fee", Quantity: 2, RefID: 999}, // ref_id 对挂号费无意义，允许传但不参与定价
		{ItemType: "charge_item", RefID: 46, Quantity: 1},
		{ItemType: "bundle", RefID: 1, Quantity: 1},
		{ItemType: "drug", RefID: 2, Quantity: 1, PrescriptionID: orderLinesTestPtr(7)},
	}
	if err := validateOrderLineInputs(valid); err != nil {
		t.Fatalf("valid inputs rejected: %v", err)
	}
}

// TestValidateOrderLineInputsRejects 逐条断言非法输入（空集合/未知类型/数量为 0）。
func TestValidateOrderLineInputsRejects(t *testing.T) {
	cases := []struct {
		name   string
		inputs []OrderLineInput
	}{
		{"empty", nil},
		{"unknown type", []OrderLineInput{{ItemType: "lab", RefID: 1, Quantity: 1}}},
		{"registration fee zero qty", []OrderLineInput{{ItemType: "registration_fee", Quantity: 0}}},
		{"charge item without ref", []OrderLineInput{{ItemType: "charge_item", Quantity: 1}}},
		{"drug without ref", []OrderLineInput{{ItemType: "drug", Quantity: 1}}},
		{"negative discount", []OrderLineInput{{ItemType: "charge_item", RefID: 1, Quantity: 1, DiscountAmount: -1}}},
	}
	for _, testCase := range cases {
		if err := validateOrderLineInputs(testCase.inputs); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("%s: err = %v, want ErrInvalidRequest", testCase.name, err)
		}
	}
}

// TestBuildOrderItemRowsRegistrationFee 挂号费行按挂号记录定价，忽略前端 ref_id。
func TestBuildOrderItemRowsRegistrationFee(t *testing.T) {
	stub := func(input OrderLineInput) (resolvedLine, error) {
		if input.ItemType != "registration_fee" {
			return resolvedLine{}, ErrInvalidRequest
		}
		return resolvedLine{Name: "挂号费", UnitPrice: 12}, nil
	}
	rows, err := buildOrderItemRows([]OrderLineInput{
		{ItemType: "registration_fee", RefID: 999, Quantity: 1, PrescriptionID: orderLinesTestPtr(7)},
	}, stub)
	if err != nil {
		t.Fatalf("build rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1 row", rows)
	}
	row := rows[0]
	if row.Name != "挂号费" || row.UnitPrice != 12 || row.Quantity != 1 || row.LineTotal != 12 {
		t.Fatalf("fee row = %+v", row)
	}
	if row.RefID != nil {
		t.Fatalf("registration_fee ref_id should be ignored, got %v", *row.RefID)
	}
	if row.PrescriptionID != nil {
		t.Fatalf("registration_fee prescription_id should be ignored, got %v", *row.PrescriptionID)
	}
}

// TestBuildOrderItemRowsMixedTotals 挂号费 + 处方药品 + 手工收费项目混合订单金额。
func TestBuildOrderItemRowsMixedTotals(t *testing.T) {
	prices := map[string]resolvedLine{
		"registration_fee": {Name: "挂号费", UnitPrice: 12},
		"drug":             {Name: "阿莫西林胶囊", UnitPrice: 12.5, PrescriptionID: orderLinesTestPtr(11)},
		"charge_item":      {Name: "血常规", UnitPrice: 30},
	}
	stub := func(input OrderLineInput) (resolvedLine, error) {
		line, ok := prices[input.ItemType]
		if !ok {
			return resolvedLine{}, ErrInvalidRequest
		}
		return line, nil
	}
	inputs := []OrderLineInput{
		{ItemType: "registration_fee", Quantity: 1},
		{ItemType: "drug", RefID: 501, PrescriptionID: orderLinesTestPtr(11), Quantity: 2},
		{ItemType: "charge_item", RefID: 46, Quantity: 1},
	}
	rows, err := buildOrderItemRows(inputs, stub)
	if err != nil {
		t.Fatalf("build rows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want 3 rows", rows)
	}
	wantTotals := []float64{12, 25, 30}
	total := 0.0
	for index, row := range rows {
		if row.LineTotal != wantTotals[index] {
			t.Fatalf("row %d line total = %v, want %v", index, row.LineTotal, wantTotals[index])
		}
		total += row.LineTotal
	}
	if total != 67 {
		t.Fatalf("items total = %v, want 67", total)
	}
	payable, err := orderPayable(total, 7)
	if err != nil {
		t.Fatalf("payable: %v", err)
	}
	if payable != 60 {
		t.Fatalf("payable = %v, want 60", payable)
	}
	if rows[1].PrescriptionID == nil || *rows[1].PrescriptionID != 11 {
		t.Fatalf("drug row prescription_id = %v, want 11", rows[1].PrescriptionID)
	}
	if rows[2].RefID == nil || *rows[2].RefID != 46 {
		t.Fatalf("charge row ref_id = %v, want 46", rows[2].RefID)
	}
}

// TestOrderLineAmountAndPayable 行金额/应收的边界校验。
func TestOrderLineAmountAndPayable(t *testing.T) {
	amounts := []struct {
		unitPrice, quantity, discount, want float64
	}{
		{12.5, 2, 0, 25},
		{12, 1, 12, 0},
		{0, 1, 0, 0},
	}
	for _, testCase := range amounts {
		got, err := orderLineAmount(testCase.unitPrice, testCase.quantity, testCase.discount)
		if err != nil {
			t.Fatalf("amount(%v, %v, %v): %v", testCase.unitPrice, testCase.quantity, testCase.discount, err)
		}
		if got != testCase.want {
			t.Fatalf("amount(%v, %v, %v) = %v, want %v", testCase.unitPrice, testCase.quantity, testCase.discount, got, testCase.want)
		}
	}

	badAmounts := [][3]float64{{-1, 1, 0}, {10, 0, 0}, {10, 1, -1}, {10, 1, 11}}
	for _, testCase := range badAmounts {
		if _, err := orderLineAmount(testCase[0], testCase[1], testCase[2]); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("amount(%v, %v, %v) err = %v, want ErrInvalidRequest", testCase[0], testCase[1], testCase[2], err)
		}
	}

	if payable, err := orderPayable(67, 7); err != nil || payable != 60 {
		t.Fatalf("payable(67,7) = %v, %v; want 60, nil", payable, err)
	}
	for _, testCase := range [][2]float64{{10, 11}, {-1, 0}, {10, -1}} {
		if _, err := orderPayable(testCase[0], testCase[1]); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("payable(%v,%v) err = %v, want ErrInvalidRequest", testCase[0], testCase[1], err)
		}
	}
}

// TestRegistrationFeeLine 挂号费行定价：行名固定、金额取挂号记录 fee、负数拒绝。
func TestRegistrationFeeLine(t *testing.T) {
	line, err := registrationFeeLine(12)
	if err != nil {
		t.Fatalf("registrationFeeLine(12): %v", err)
	}
	if line.Name != "挂号费" || line.UnitPrice != 12 || line.PrescriptionID != nil {
		t.Fatalf("fee line = %+v", line)
	}
	if zero, err := registrationFeeLine(0); err != nil || zero.UnitPrice != 0 || zero.Name != "挂号费" {
		t.Fatalf("registrationFeeLine(0) = %+v, %v", zero, err)
	}
	if _, err := registrationFeeLine(-1); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("negative fee err = %v, want ErrInvalidRequest", err)
	}
}

// TestDrugLinePriceSnapshotPreferred 药品行定价：处方快照价优先，处方未含该药时回退字典价。
func TestDrugLinePriceSnapshotPreferred(t *testing.T) {
	dict := resolvedLine{Name: "阿奇霉素片", UnitPrice: 20}
	snapshot := resolvedLine{Name: "阿奇霉素片", UnitPrice: 18}

	line, err := drugLinePrice(dict, &snapshot)
	if err != nil {
		t.Fatalf("drugLinePrice snapshot: %v", err)
	}
	if line.UnitPrice != 18 {
		t.Fatalf("drug price = %v, want 处方快照价 18（字典价 20）", line.UnitPrice)
	}

	fallback, err := drugLinePrice(dict, nil)
	if err != nil {
		t.Fatalf("drugLinePrice fallback: %v", err)
	}
	if fallback.UnitPrice != 20 {
		t.Fatalf("fallback price = %v, want 字典价 20", fallback.UnitPrice)
	}

	if _, err := drugLinePrice(dict, &resolvedLine{Name: "bad", UnitPrice: -1}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("negative snapshot price err = %v, want ErrInvalidRequest", err)
	}
}
