package his

import (
	"errors"
	"testing"
)

// 检验/检查开单收费联动的纯逻辑测试：项目选择、名称匹配、金额快照、无匹配不建单。

func chargeSnapshotFixture() []chargeItemSnapshot {
	return []chargeItemSnapshot{
		{ID: 8, Name: "白带常规", Price: 30, Category: "检验"},
		{ID: 9, Name: "血常规", Price: 25, Category: "检验"},
		{ID: 10, Name: "妇科B超", Price: 80, Category: "检查"},
	}
}

func TestNormalizeChargeItemID(t *testing.T) {
	for _, id := range []int64{0, 9} {
		if err := normalizeChargeItemID(id); err != nil {
			t.Fatalf("id %d should be valid: %v", id, err)
		}
	}
	for _, id := range []int64{-1, -9} {
		if err := normalizeChargeItemID(id); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("id %d err = %v, want ErrInvalidRequest", id, err)
		}
	}
}

func TestSelectChargeItemByID(t *testing.T) {
	item, matched, err := selectChargeItem(chargeSnapshotFixture(), 9, "白带常规")
	if err != nil || !matched {
		t.Fatalf("select by id = %+v, %v, %v; want matched 血常规", item, matched, err)
	}
	if item.ID != 9 || item.Name != "血常规" || item.Price != 25 {
		t.Fatalf("selected item = %+v, want id 9 血常规 25", item)
	}

	// 显式 id 但不在本租户启用字典里（含跨租户、已停用）→ 拒绝开单，而不是静默降级。
	if _, _, err := selectChargeItem(chargeSnapshotFixture(), 999, "血常规"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown charge item err = %v, want ErrInvalidRequest", err)
	}
}

func TestMatchChargeItemByNameExact(t *testing.T) {
	items := chargeSnapshotFixture()

	matches := matchChargeItemsByName(items, " 血常规 ")
	if len(matches) != 1 || matches[0].ID != 9 || matches[0].Price != 25 {
		t.Fatalf("trimmed exact match = %+v; want 血常规", matches)
	}

	// 只做精确匹配：不做模糊/前缀匹配，避免把「血常规（五分类）」错配成「血常规」。
	for _, name := range []string{"血常规（五分类）", "血", "白带", "", "   "} {
		if got := matchChargeItemsByName(items, name); len(got) != 0 {
			t.Fatalf("name %q should not match, got %+v", name, got)
		}
	}

	// 同名不同价：歧义时不自动匹配（交由医生显式选择），避免挂错价格。
	ambiguous := append(chargeSnapshotFixture(), chargeItemSnapshot{ID: 99, Name: "血常规", Price: 120, Category: "检验"})
	if _, matched, _ := selectChargeItem(ambiguous, 0, "血常规"); matched {
		t.Fatal("ambiguous name should not auto match")
	}
}

func TestSelectChargeItemAutoMatchByName(t *testing.T) {
	item, matched, err := selectChargeItem(chargeSnapshotFixture(), 0, "妇科B超")
	if err != nil || !matched || item.ID != 10 || item.Price != 80 {
		t.Fatalf("auto match = %+v, %v, %v; want 妇科B超 80", item, matched, err)
	}
}

func TestSelectChargeItemNoMatchBuildsNoOrder(t *testing.T) {
	// 未命中：matched=false 且 item 为零值，调用方据此跳过建单（charge_amount=0、order_id=NULL）。
	item, matched, err := selectChargeItem(chargeSnapshotFixture(), 0, "胸部DR")
	if err != nil {
		t.Fatalf("no match should not fail: %v", err)
	}
	if matched {
		t.Fatalf("no match should return matched=false, got %+v", item)
	}
	if _, err := newPendingChargeLine(item); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero charge item must not build line, err = %v", err)
	}
}

func TestNewPendingChargeLineSnapshotsPrice(t *testing.T) {
	item := chargeItemSnapshot{ID: 9, Name: "血常规", Price: 25, Category: "检验"}
	line, err := newPendingChargeLine(item)
	if err != nil {
		t.Fatalf("line: %v", err)
	}
	if line.ItemType != "charge_item" || line.RefID != 9 || line.Name != "血常规" {
		t.Fatalf("line identity = %+v", line)
	}
	if line.UnitPrice != 25 || line.Quantity != 1 || line.LineTotal != 25 || line.Payable != 25 {
		t.Fatalf("line amounts = %+v; want 25 x1 no discount", line)
	}

	// 字典改价后快照不受影响（收费行记录下单时价格）。
	item.Price = 99
	if line.UnitPrice != 25 || line.LineTotal != 25 {
		t.Fatalf("snapshot changed after dictionary update: %+v", line)
	}

	if _, err := newPendingChargeLine(chargeItemSnapshot{ID: 0, Name: "无", Price: 1}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing id err = %v, want ErrInvalidRequest", err)
	}
	if _, err := newPendingChargeLine(chargeItemSnapshot{ID: 1, Name: "负数", Price: -1}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("negative price err = %v, want ErrInvalidRequest", err)
	}
}

func TestSelectChargeItemIgnoresOtherCategoryCandidates(t *testing.T) {
	// 候选集合由分类决定：检验开单不会命中「检查」类项目（妇科B超不在检验候选里）。
	labCandidates := make([]chargeItemSnapshot, 0)
	for _, item := range chargeSnapshotFixture() {
		if item.Category == chargeCategoryLab {
			labCandidates = append(labCandidates, item)
		}
	}
	if _, matched, err := selectChargeItem(labCandidates, 0, "妇科B超"); err != nil || matched {
		t.Fatalf("lab auto match on exam item = %v, %v; want no match", matched, err)
	}
	if item, matched, err := selectChargeItem(labCandidates, 0, "白带常规"); err != nil || !matched || item.Price != 30 {
		t.Fatalf("lab auto match 白带常规 = %+v, %v, %v; want 30", item, matched, err)
	}
}
