package his

import (
	"context"
	"errors"
	"testing"
)

func orderTestInt64(value int64) *int64 { return &value }

func orderTestFixture() []Prescription {
	return []Prescription{
		{
			ID: 11, EncounterID: 1001, Status: "issued",
			Items: []PrescriptionItem{
				{ID: 1, DrugID: orderTestInt64(501), Name: "阿莫西林胶囊", Price: 12.5, Quantity: 2},
				{ID: 2, DrugID: orderTestInt64(502), Name: "生理盐水", Price: 3, Quantity: 1},
			},
		},
		{
			ID: 12, EncounterID: 1001, Status: "draft",
			Items: []PrescriptionItem{
				{ID: 3, DrugID: orderTestInt64(503), Name: "布洛芬缓释胶囊", Price: 8, Quantity: 1},
			},
		},
		{
			ID: 13, EncounterID: 1001, Status: "charged",
			Items: []PrescriptionItem{
				{ID: 4, DrugID: orderTestInt64(504), Name: "已收费药品", Price: 10, Quantity: 1},
			},
		},
	}
}

func TestOrderLinesFromEncounterFeeAndPrescriptions(t *testing.T) {
	lines, err := buildEncounterOrderLines(12, orderTestInt64(900), true, orderTestFixture(), nil, nil)
	if err != nil {
		t.Fatalf("build lines: %v", err)
	}
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (fee + 3 drugs), got %d: %+v", len(lines), lines)
	}
	fee := lines[0]
	if fee.ItemType != "charge_item" || fee.Name != "挂号费" || fee.UnitPrice != 12 || fee.Quantity != 1 {
		t.Fatalf("fee line = %+v", fee)
	}
	if fee.RefID == nil || *fee.RefID != 900 {
		t.Fatalf("fee ref = %v, want 900", fee.RefID)
	}
	if lines[1].PrescriptionID == nil || *lines[1].PrescriptionID != 11 {
		t.Fatalf("drug line prescription_id = %v, want 11", lines[1].PrescriptionID)
	}
	if lines[1].UnitPrice != 12.5 || lines[1].Quantity != 2 {
		t.Fatalf("drug line = %+v", lines[1])
	}
	for _, line := range lines[1:] {
		if line.PrescriptionID != nil && *line.PrescriptionID == 13 {
			t.Fatalf("charged prescription must be skipped: %+v", line)
		}
	}
}

func TestOrderLinesFromEncounterSelectedPrescriptions(t *testing.T) {
	lines, err := buildEncounterOrderLines(0, nil, false, orderTestFixture(), []int64{12}, nil)
	if err != nil {
		t.Fatalf("build lines: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 selected drug line, got %d: %+v", len(lines), lines)
	}
	if lines[0].Name != "布洛芬缓释胶囊" || lines[0].PrescriptionID == nil || *lines[0].PrescriptionID != 12 {
		t.Fatalf("selected line = %+v", lines[0])
	}

	if _, err := buildEncounterOrderLines(0, nil, false, orderTestFixture(), []int64{13}, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("charged prescription should fail, got %v", err)
	}
	if _, err := buildEncounterOrderLines(0, nil, false, orderTestFixture(), []int64{999}, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown prescription should fail, got %v", err)
	}
}

func TestOrderLinesFromEncounterQuantityOverride(t *testing.T) {
	rxID := int64(11)
	lines, err := buildEncounterOrderLines(10, nil, true, orderTestFixture(), []int64{11}, []OrderLineInput{
		{ItemType: "drug", RefID: 501, Quantity: 5, DiscountAmount: 2.5, PrescriptionID: &rxID},
	})
	if err != nil {
		t.Fatalf("build lines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected fee + 1 override line, got %d: %+v", len(lines), lines)
	}
	if lines[0].RefID != nil {
		t.Fatalf("fee ref should be nil without charge item, got %v", *lines[0].RefID)
	}
	if lines[1].Quantity != 5 || lines[1].UnitPrice != 12.5 || lines[1].DiscountAmount != 2.5 {
		t.Fatalf("override line = %+v", lines[1])
	}

	if _, err := buildEncounterOrderLines(0, nil, false, orderTestFixture(), []int64{11}, []OrderLineInput{
		{ItemType: "drug", RefID: 999, Quantity: 1, PrescriptionID: &rxID},
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown drug should fail, got %v", err)
	}
	if _, err := buildEncounterOrderLines(0, nil, false, orderTestFixture(), []int64{11}, []OrderLineInput{
		{ItemType: "drug", RefID: 501, Quantity: 1, PrescriptionID: orderTestInt64(12)},
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unselected prescription should fail, got %v", err)
	}
}

func TestOrderFromEncounterValidation(t *testing.T) {
	service := NewService(nil)
	ctx := context.Background()
	cases := []CreateOrderFromEncounterRequest{
		{},
		{EncounterID: 1},
		{EncounterID: 1, IncludeRegistrationFee: true, DiscountTotal: -1},
		{EncounterID: 1, Items: []OrderLineInput{{ItemType: "bundle", RefID: 1, Quantity: 1}}},
		{EncounterID: 1, Items: []OrderLineInput{{ItemType: "drug", RefID: 1, Quantity: 0, PrescriptionID: orderTestInt64(2)}}},
	}
	for index, req := range cases {
		if _, err := service.CreateOrderFromEncounter(ctx, 1, 10, req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("case %d: err = %v, want ErrInvalidRequest", index, err)
		}
	}
}
