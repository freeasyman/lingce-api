package v210

import (
	"bytes"
	"encoding/json"
	"testing"
)

// rc9 changes prompt guidance only. This test proves the compiler output for the
// same input/intermediate is rc8's card with only the rules_version literal
// changed, so an rc9 card cannot be confused with an rc8 or rc7 record.
func TestRC9CompilesUnderIndependentRulesVersion(t *testing.T) {
	in, mid := v210Fixture(t)
	rc8, err := Compile(in, mid, RulesVersionRC8)
	if err != nil {
		t.Fatal(err)
	}
	rc9, err := Compile(in, mid, RulesVersionRC9)
	if err != nil {
		t.Fatal(err)
	}
	var c8, c9 map[string]any
	if err = json.Unmarshal(rc8.Card, &c8); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(rc9.Card, &c9); err != nil {
		t.Fatal(err)
	}
	if c9["rules_version"] != RulesVersionRC9 {
		t.Fatalf("rc9 card version missing: %v", c9["rules_version"])
	}
	if c8["rules_version"] != RulesVersionRC8 {
		t.Fatalf("rc8 card version changed: %v", c8["rules_version"])
	}

	// Compiler behavior is identical: only the frozen version literal differs.
	normalized := bytes.ReplaceAll(rc9.Card, []byte(RulesVersionRC9), []byte(RulesVersionRC8))
	if !bytes.Equal(rc8.Card, normalized) {
		t.Fatal("rc9 compiler changed output beyond rules_version")
	}

	// Validation is version-specific in both directions.
	if err = ValidateCardForVersion(rc9.Card, RulesVersionRC9); err != nil {
		t.Fatalf("rc9 card rejected: %v", err)
	}
	if ValidateCardForVersion(rc9.Card, RulesVersionRC8) == nil {
		t.Fatal("rc9 card accepted as rc8")
	}
	if ValidateCardForVersion(rc8.Card, RulesVersionRC9) == nil {
		t.Fatal("rc8 card accepted as rc9")
	}
	if ValidateCard(rc9.Card) == nil {
		t.Fatal("rc9 card accepted as rc7")
	}

	// rc9 reuses rc8's input/intermediate schema bytes and has its own final
	// card schema whose only difference from rc8 is the rules_version const.
	_, m8, f8 := ContractsForVersion(RulesVersionRC8)
	_, m9, f9 := ContractsForVersion(RulesVersionRC9)
	i9, _, _ := ContractsForVersion(RulesVersionRC9)
	if len(i9) == 0 || len(m9) == 0 || len(f9) == 0 {
		t.Fatal("rc9 contracts incomplete")
	}
	if !bytes.Equal(m8, m9) {
		t.Fatal("rc9 intermediate contract drifted from rc8")
	}
	if bytes.Equal(f8, f9) {
		t.Fatal("rc9 reused rc8 final card schema")
	}
	if !bytes.Equal(bytes.ReplaceAll(f9, []byte(RulesVersionRC9), []byte(RulesVersionRC8)), f8) {
		t.Fatal("rc9 final schema differs from rc8 beyond rules_version")
	}
}

func TestRC9RejectsUnknownRulesVersion(t *testing.T) {
	in, mid := v210Fixture(t)
	// rc10 and rc12 are now registered versions; the unknown probe moves to rc13
	// with the same expectation (unregistered versions are rejected, never executed).
	for _, version := range []string{"", "deal-compiler-v2.1.0-rc8x", "deal-compiler-v2.1.0-rc13", "deal-compiler-v2.1.1"} {
		if _, err := Compile(in, mid, version); err == nil {
			t.Fatalf("unknown version %q accepted", version)
		}
	}
	if i, m, f := ContractsForVersion("future"); i != nil || m != nil || f != nil {
		t.Fatal("unknown version returned schemas")
	}
}
