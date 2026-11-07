package his

import (
	"testing"
	"time"
)

func TestSlotTimes(t *testing.T) {
	start, _ := time.Parse("15:04", "09:00")
	end, _ := time.Parse("15:04", "09:35")

	pairs := slotTimes(start, end, 15)
	if len(pairs) != 3 {
		t.Fatalf("expected 3 slots, got %d: %v", len(pairs), pairs)
	}
	want := [][2]string{{"09:00", "09:15"}, {"09:15", "09:30"}, {"09:30", "09:35"}}
	for i, pair := range pairs {
		if pair != want[i] {
			t.Fatalf("slot %d = %v, want %v", i, pair, want[i])
		}
	}
}

func TestSlotTimesExactFit(t *testing.T) {
	start, _ := time.Parse("15:04", "08:00")
	end, _ := time.Parse("15:04", "08:30")
	pairs := slotTimes(start, end, 10)
	if len(pairs) != 3 {
		t.Fatalf("expected 3 slots for exact fit, got %d", len(pairs))
	}
}

func TestMRNFormat(t *testing.T) {
	cases := map[int64]string{1: "M00000001", 999: "M00000999", 12345678: "M12345678"}
	for seq, want := range cases {
		if got := mrnFormat(seq); got != want {
			t.Fatalf("mrnFormat(%d) = %s, want %s", seq, got, want)
		}
	}
}
