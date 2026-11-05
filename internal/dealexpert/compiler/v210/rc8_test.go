package v210

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRC8CompilesUnderIndependentRulesVersion(t *testing.T) {
	in, mid := v210Fixture(t)
	rc7, err := Compile(in, mid, RulesVersion)
	if err != nil {
		t.Fatal(err)
	}
	rc8, err := Compile(in, mid, RulesVersionRC8)
	if err != nil {
		t.Fatal(err)
	}
	var c7, c8 map[string]any
	if err = json.Unmarshal(rc7.Card, &c7); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(rc8.Card, &c8); err != nil {
		t.Fatal(err)
	}
	if c7["rules_version"] != RulesVersion {
		t.Fatalf("rc7 card version changed: %v", c7["rules_version"])
	}
	if c8["rules_version"] != RulesVersionRC8 {
		t.Fatalf("rc8 card version missing: %v", c8["rules_version"])
	}

	// Card validation is version-specific in both directions.
	if err = ValidateCardForVersion(rc8.Card, RulesVersionRC8); err != nil {
		t.Fatalf("rc8 card rejected: %v", err)
	}
	if ValidateCard(rc8.Card) == nil {
		t.Fatal("rc8 card accepted as rc7")
	}
	if ValidateCardForVersion(rc7.Card, RulesVersionRC8) == nil {
		t.Fatal("rc7 card accepted as rc8")
	}

	// Selector adds no behavior change: only the rules_version literal differs.
	normalized := bytes.ReplaceAll(rc8.Card, []byte(RulesVersionRC8), []byte(RulesVersion))
	if !bytes.Equal(rc7.Card, normalized) {
		t.Fatal("rc8 compiler changed output beyond rules_version")
	}

	i7, m7, f7 := Contracts()
	i8, m8, f8 := ContractsForVersion(RulesVersionRC8)
	if !bytes.Equal(i7, i8) || !bytes.Equal(m7, m8) {
		t.Fatal("rc8 input/intermediate contracts drifted")
	}
	if bytes.Equal(f7, f8) {
		t.Fatal("rc8 reused rc7 final card schema")
	}
	if len(i8) == 0 || len(m8) == 0 || len(f8) == 0 {
		t.Fatal("rc8 contracts incomplete")
	}
}

func TestRC8RejectsUnknownRulesVersion(t *testing.T) {
	in, mid := v210Fixture(t)
	// rc9, rc10 and rc12 became registered versions when each was published as
	// an independent candidate, so the "future/unknown" probe moves to rc13. The
	// expectation is unchanged: an unregistered version is still rejected, never
	// executed as the current implementation.
	for _, version := range []string{"", "deal-compiler-v2.1.0-rc7x", "deal-compiler-v2.1.0-rc13", "deal-compiler-v2.1.1"} {
		if _, err := Compile(in, mid, version); err == nil {
			t.Fatalf("unknown version %q accepted", version)
		}
	}
	if i, m, f := ContractsForVersion("future"); i != nil || m != nil || f != nil {
		t.Fatal("unknown version returned schemas")
	}
}
