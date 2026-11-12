package his

import (
	"errors"
	"math"
	"testing"
	"time"
)

func purchaseTestString(value string) *string { return &value }

func TestPurchaseOrderNo(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 30, 0, 0, time.Local)
	cases := map[int64]string{
		1:     "PO202611120001",
		999:   "PO202611120999",
		10000: "PO2026111210000",
	}
	for seq, want := range cases {
		if got := purchaseOrderNo(seq, now); got != want {
			t.Fatalf("purchaseOrderNo(%d) = %s, want %s", seq, got, want)
		}
	}
}

func TestNormalizePurchaseOrderInput(t *testing.T) {
	req := CreatePurchaseOrderRequest{
		Supplier: "  华润医药  ",
		Items: []CreatePurchaseOrderItem{
			{DrugID: 1, Quantity: 10, PurchasePrice: 4.2, BatchNo: " B20261112 ", ExpiryDate: purchaseTestString(" 2028-12-31 ")},
			{DrugID: 2, Quantity: 2, PurchasePrice: 0, BatchNo: "", ExpiryDate: purchaseTestString("")},
		},
	}
	normalized, err := normalizePurchaseOrderInput(req)
	if err != nil {
		t.Fatalf("normalize valid request: %v", err)
	}
	if normalized.Supplier != "华润医药" {
		t.Fatalf("supplier not trimmed: %q", normalized.Supplier)
	}
	if normalized.Items[0].BatchNo != "B20261112" {
		t.Fatalf("batch no not trimmed: %q", normalized.Items[0].BatchNo)
	}
	if normalized.Items[0].ExpiryDate == nil || *normalized.Items[0].ExpiryDate != "2028-12-31" {
		t.Fatalf("expiry not normalized: %+v", normalized.Items[0].ExpiryDate)
	}
	if normalized.Items[1].ExpiryDate != nil {
		t.Fatalf("empty expiry should become nil: %+v", normalized.Items[1].ExpiryDate)
	}
}

func TestNormalizePurchaseOrderInputRejectsInvalid(t *testing.T) {
	base := CreatePurchaseOrderRequest{
		Supplier: "国药控股",
		Items:    []CreatePurchaseOrderItem{{DrugID: 1, Quantity: 1, PurchasePrice: 1}},
	}
	cases := map[string]CreatePurchaseOrderRequest{
		"empty supplier": func() CreatePurchaseOrderRequest {
			req := base
			req.Supplier = "   "
			return req
		}(),
		"no items": func() CreatePurchaseOrderRequest {
			req := base
			req.Items = nil
			return req
		}(),
		"invalid drug": func() CreatePurchaseOrderRequest {
			req := base
			req.Items = []CreatePurchaseOrderItem{{DrugID: 0, Quantity: 1, PurchasePrice: 1}}
			return req
		}(),
		"zero quantity": func() CreatePurchaseOrderRequest {
			req := base
			req.Items = []CreatePurchaseOrderItem{{DrugID: 1, Quantity: 0, PurchasePrice: 1}}
			return req
		}(),
		"negative quantity": func() CreatePurchaseOrderRequest {
			req := base
			req.Items = []CreatePurchaseOrderItem{{DrugID: 1, Quantity: -1, PurchasePrice: 1}}
			return req
		}(),
		"negative price": func() CreatePurchaseOrderRequest {
			req := base
			req.Items = []CreatePurchaseOrderItem{{DrugID: 1, Quantity: 1, PurchasePrice: -0.01}}
			return req
		}(),
		"invalid expiry": func() CreatePurchaseOrderRequest {
			req := base
			req.Items = []CreatePurchaseOrderItem{{DrugID: 1, Quantity: 1, PurchasePrice: 1, ExpiryDate: purchaseTestString("2026-13-01")}}
			return req
		}(),
	}
	for name, req := range cases {
		if _, err := normalizePurchaseOrderInput(req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("%s: expected ErrInvalidRequest, got %v", name, err)
		}
	}
}

func TestPurchaseOrderAmounts(t *testing.T) {
	if got := purchaseLineAmount(10, 4.2); got != 42 {
		t.Fatalf("line amount = %v, want 42", got)
	}
	if got := purchaseLineAmount(1, 2.345); got != 2.35 {
		t.Fatalf("line amount should round to cents, got %v", got)
	}
	items := []CreatePurchaseOrderItem{
		{Quantity: 10, PurchasePrice: 4.2},
		{Quantity: 3, PurchasePrice: 1.1},
	}
	if got := purchaseOrderTotal(items); math.Abs(got-45.3) > 1e-9 {
		t.Fatalf("total = %v, want 45.3", got)
	}
	if got := purchaseOrderTotal(nil); got != 0 {
		t.Fatalf("empty total = %v, want 0", got)
	}
}

func TestPurchaseStatusTransitions(t *testing.T) {
	for _, status := range []string{purchaseStatusDraft, purchaseStatusOrdered} {
		if err := purchaseReceiveStatusError(status); err != nil {
			t.Fatalf("receive from %s should be allowed: %v", status, err)
		}
		if err := purchaseCancelStatusError(status); err != nil {
			t.Fatalf("cancel from %s should be allowed: %v", status, err)
		}
	}
	if err := purchaseReceiveStatusError(purchaseStatusReceived); !errors.Is(err, ErrPurchaseAlreadyReceived) {
		t.Fatalf("duplicate receive should return ErrPurchaseAlreadyReceived, got %v", err)
	}
	for _, status := range []string{purchaseStatusCancelled, "unknown"} {
		if err := purchaseReceiveStatusError(status); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("receive from %s should be invalid state, got %v", status, err)
		}
	}
	for _, status := range []string{purchaseStatusReceived, purchaseStatusCancelled} {
		if err := purchaseCancelStatusError(status); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("cancel from %s should be invalid state, got %v", status, err)
		}
	}
}

func TestValidatePurchaseStatusFilter(t *testing.T) {
	for _, status := range []string{"", purchaseStatusDraft, purchaseStatusOrdered, purchaseStatusReceived, purchaseStatusCancelled} {
		if err := validatePurchaseStatusFilter(status); err != nil {
			t.Fatalf("status %q should be accepted: %v", status, err)
		}
	}
	if err := validatePurchaseStatusFilter("paid"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown status should be rejected, got %v", err)
	}
}
