package encounter

import "testing"

func TestRealtimeSourceIDIsStableAndPositive(t *testing.T) {
	first := realtimeSourceID("request-1")
	if first <= 0 {
		t.Fatalf("realtimeSourceID must be positive, got %d", first)
	}
	if got := realtimeSourceID("request-1"); got != first {
		t.Fatalf("realtimeSourceID must be stable, got %d want %d", got, first)
	}
}
