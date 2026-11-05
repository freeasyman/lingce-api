package compiler

import (
	"fmt"
	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler/v200"
	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler/v210"
)

// Contract selects a frozen executable rule set. Add a separately preserved
// implementation when publishing a new version; unknown versions never fall
// back to the current compiler. Its zero value cannot execute.
type Contract struct{ version string }

// Exported aliases keep rule strings in one place for explicit selection. rc8,
// rc9, rc10, rc11 and rc12 are independent candidates, never the default.
const (
	RulesVersionV210RC7  = v210.RulesVersion
	RulesVersionV210RC8  = v210.RulesVersionRC8
	RulesVersionV210RC9  = v210.RulesVersionRC9
	RulesVersionV210RC10 = v210.RulesVersionRC10
	RulesVersionV210RC11 = v210.RulesVersionRC11
	RulesVersionV210RC12   = v210.RulesVersionRC12
	RulesVersionV210RC12P1 = v210.RulesVersionRC12P1
)

// IsV210Rules reports whether a rules version uses the v2.1 input assembly and
// compiler implementation. It does not imply acceptance or default routing.
func IsV210Rules(version string) bool { return v210.IsRulesVersion(version) }

func SelectContract(version string) (Contract, error) {
	switch version {
	case "deal-compiler-v2.0.0", RulesVersionV210RC7, RulesVersionV210RC8, RulesVersionV210RC9, RulesVersionV210RC10, RulesVersionV210RC11, RulesVersionV210RC12, RulesVersionV210RC12P1:
		return Contract{version: version}, nil
	default:
		return Contract{}, fmt.Errorf("unsupported compiler contract %q", version)
	}
}

func (c Contract) Version() string { return c.version }

func (c Contract) Contracts() (input, intermediate, final []byte) {
	if c.version == "deal-compiler-v2.0.0" {
		return v200.Contracts()
	}
	if IsV210Rules(c.version) {
		return v210.ContractsForVersion(c.version)
	}
	return nil, nil, nil
}

func (c Contract) Compile(snapshot, intermediate []byte) (Result, error) {
	if c.version != "deal-compiler-v2.0.0" && !IsV210Rules(c.version) {
		return Result{}, fmt.Errorf("unselected compiler contract")
	}
	if IsV210Rules(c.version) {
		r, err := v210.Compile(snapshot, intermediate, c.version)
		if ce, ok := err.(*v210.Error); ok {
			err = &Error{Stage: ce.Stage, Err: ce.Err}
		}
		ds := make([]Diagnostic, len(r.Diagnostics))
		for i, d := range r.Diagnostics {
			ds[i] = Diagnostic{Path: d.Path, Code: d.Code, Severity: d.Severity}
		}
		return Result{Status: r.Status, Card: r.Card, Diagnostics: ds}, err
	}
	return v200.Compile(snapshot, intermediate, c.version)
}

func (c Contract) ValidateSnapshot(snapshot []byte) error {
	if c.version != "deal-compiler-v2.0.0" && !IsV210Rules(c.version) {
		return fmt.Errorf("unselected compiler contract")
	}
	if IsV210Rules(c.version) {
		return v210.ValidateSnapshot(snapshot)
	}
	return v200.ValidateSnapshot(snapshot)
}

func (c Contract) ValidateCard(card []byte) error {
	if c.version != "deal-compiler-v2.0.0" && !IsV210Rules(c.version) {
		return fmt.Errorf("unselected compiler contract")
	}
	if IsV210Rules(c.version) {
		return v210.ValidateCardForVersion(card, c.version)
	}
	return v200.ValidateCard(card)
}
