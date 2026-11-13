package his

import (
	"errors"
	"testing"
)

// 收费项目绑定的纯逻辑测试：非自身、正数量、去重。

func TestNormalizeChargeItemLinkInputs(t *testing.T) {
	valid := []ChargeItemLinkInput{
		{ChildChargeItemID: 11, Quantity: 2},
		{ChildChargeItemID: 12, Quantity: 0.5},
	}
	normalized, err := normalizeChargeItemLinkInputs(10, valid)
	if err != nil || len(normalized) != 2 {
		t.Fatalf("valid links = %+v, %v", normalized, err)
	}

	for name, inputs := range map[string][]ChargeItemLinkInput{
		"self":       {{ChildChargeItemID: 10, Quantity: 1}},
		"zero child": {{ChildChargeItemID: 0, Quantity: 1}},
		"zero qty":   {{ChildChargeItemID: 11, Quantity: 0}},
		"neg qty":    {{ChildChargeItemID: 11, Quantity: -1}},
		"duplicate":  {{ChildChargeItemID: 11, Quantity: 1}, {ChildChargeItemID: 11, Quantity: 2}},
	} {
		if _, err := normalizeChargeItemLinkInputs(10, inputs); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("%s: err = %v, want ErrInvalidRequest", name, err)
		}
	}

	if _, err := normalizeChargeItemLinkInputs(0, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid parent err = %v", err)
	}
}

func TestNormalizeChargeItemLinkInputsAllowsEmpty(t *testing.T) {
	normalized, err := normalizeChargeItemLinkInputs(10, nil)
	if err != nil || len(normalized) != 0 {
		t.Fatalf("empty links = %+v, %v; want empty and nil error", normalized, err)
	}
}
