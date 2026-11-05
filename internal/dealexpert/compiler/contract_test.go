package compiler

import (
	"bytes"
	"testing"
)

func TestSelectedFrozenContract(t *testing.T) {
	contract, err := SelectContract("deal-compiler-v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if contract.Version() != RulesVersion {
		t.Fatal("wrong rule selection")
	}
	i, m, f := contract.Contracts()
	ai, am, af := Contracts()
	if !bytes.Equal(i, ai) || !bytes.Equal(m, am) || !bytes.Equal(f, af) {
		t.Fatal("selected schemas changed")
	}
	i[0] = '!'
	i, _, _ = contract.Contracts()
	if i[0] != '{' {
		t.Fatal("schema copies share storage")
	}
	e, model := fixture(t, "example")
	seal(t, e, model)
	snapshot, output := encode(t, e), encode(t, model)
	if err := contract.ValidateSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	selected, err := contract.Compile(snapshot, output)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := Compile(snapshot, output, RulesVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(selected.Card, legacy.Card) || !bytes.Equal(encode(t, selected.Diagnostics), encode(t, legacy.Diagnostics)) {
		t.Fatal("version selection changed compiled output")
	}
	if err := contract.ValidateCard(selected.Card); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownFrozenContractNeverFallsBack(t *testing.T) {
	for _, version := range []string{"", "deal-compiler-v2.0.1", "future", " deal-compiler-v2.0.0"} {
		if _, err := SelectContract(version); err == nil {
			t.Fatalf("unknown version %q accepted", version)
		}
	}
	var zero Contract
	if _, err := zero.Compile(nil, nil); err == nil {
		t.Fatal("zero contract compiled")
	}
	if err := zero.ValidateSnapshot(nil); err == nil {
		t.Fatal("zero contract validated snapshot")
	}
	if err := zero.ValidateCard(nil); err == nil {
		t.Fatal("zero contract validated card")
	}
}
