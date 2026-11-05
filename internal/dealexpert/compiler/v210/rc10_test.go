package v210

import (
	"bytes"
	"encoding/json"
	"testing"
)

// rc10 changes prompt guidance only (P2-2 wording strength, P2-3 symptom vs
// need). This test proves the compiler output for the same input/intermediate is
// rc9's card with only the rules_version literal changed, so an rc10 card cannot
// be confused with an rc9, rc8 or rc7 record.
func TestRC10CompilesUnderIndependentRulesVersion(t *testing.T) {
	in, mid := v210Fixture(t)
	rc9, err := Compile(in, mid, RulesVersionRC9)
	if err != nil {
		t.Fatal(err)
	}
	rc10, err := Compile(in, mid, RulesVersionRC10)
	if err != nil {
		t.Fatal(err)
	}
	var c9, c10 map[string]any
	if err = json.Unmarshal(rc9.Card, &c9); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(rc10.Card, &c10); err != nil {
		t.Fatal(err)
	}
	if c10["rules_version"] != RulesVersionRC10 {
		t.Fatalf("rc10 card version missing: %v", c10["rules_version"])
	}
	if c9["rules_version"] != RulesVersionRC9 {
		t.Fatalf("rc9 card version changed: %v", c9["rules_version"])
	}

	// Compiler behavior is identical: only the frozen version literal differs.
	normalized := bytes.ReplaceAll(rc10.Card, []byte(RulesVersionRC10), []byte(RulesVersionRC9))
	if !bytes.Equal(rc9.Card, normalized) {
		t.Fatal("rc10 compiler changed output beyond rules_version")
	}

	// Validation is version-specific in both directions.
	if err = ValidateCardForVersion(rc10.Card, RulesVersionRC10); err != nil {
		t.Fatalf("rc10 card rejected: %v", err)
	}
	if ValidateCardForVersion(rc10.Card, RulesVersionRC9) == nil {
		t.Fatal("rc10 card accepted as rc9")
	}
	if ValidateCardForVersion(rc9.Card, RulesVersionRC10) == nil {
		t.Fatal("rc9 card accepted as rc10")
	}
	if ValidateCard(rc10.Card) == nil {
		t.Fatal("rc10 card accepted as rc7")
	}

	// rc10 reuses rc9's input/intermediate schema bytes and has its own final
	// card schema whose only difference from rc9 is the rules_version const.
	_, m9, f9 := ContractsForVersion(RulesVersionRC9)
	i10, m10, f10 := ContractsForVersion(RulesVersionRC10)
	if len(i10) == 0 || len(m10) == 0 || len(f10) == 0 {
		t.Fatal("rc10 contracts incomplete")
	}
	if !bytes.Equal(m9, m10) {
		t.Fatal("rc10 intermediate contract drifted from rc9")
	}
	if bytes.Equal(f9, f10) {
		t.Fatal("rc10 reused rc9 final card schema")
	}
	if !bytes.Equal(bytes.ReplaceAll(f10, []byte(RulesVersionRC10), []byte(RulesVersionRC9)), f9) {
		t.Fatal("rc10 final schema differs from rc9 beyond rules_version")
	}

	// Publishing rc10 must not change rc9's executable contract.
	if !IsRulesVersion(RulesVersionRC9) {
		t.Fatal("rc9 no longer selectable after rc10 publication")
	}
	i9, m9Again, f9Again := ContractsForVersion(RulesVersionRC9)
	if len(i9) == 0 || !bytes.Equal(m9, m9Again) || !bytes.Equal(f9, f9Again) {
		t.Fatal("rc9 contracts changed")
	}
}

func TestRC10RejectsUnknownRulesVersion(t *testing.T) {
	in, mid := v210Fixture(t)
	for _, version := range []string{"", "deal-compiler-v2.1.0-rc9x", "deal-compiler-v2.1.0-rc13", "deal-compiler-v2.1.1"} {
		if _, err := Compile(in, mid, version); err == nil {
			t.Fatalf("unknown version %q accepted", version)
		}
	}
	if i, m, f := ContractsForVersion("future"); i != nil || m != nil || f != nil {
		t.Fatal("unknown version returned schemas")
	}
}
