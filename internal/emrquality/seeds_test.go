package emrquality

import "testing"

func TestBuiltinSeedsAreCompleteAndUnique(t *testing.T) {
	seeds := builtinSeeds()
	if len(seeds) != 181 {
		t.Fatalf("builtin seed count = %d, want 181", len(seeds))
	}

	seen := make(map[string]struct{}, len(seeds))
	for _, item := range seeds {
		if item.Code == "" {
			t.Fatal("builtin seed has empty code")
		}
		if _, exists := seen[item.Code]; exists {
			t.Fatalf("duplicate builtin seed code: %s", item.Code)
		}
		seen[item.Code] = struct{}{}
	}
}
