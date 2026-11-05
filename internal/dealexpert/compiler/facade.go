// Package compiler selects explicit immutable deal analysis contracts.
package compiler

import "github.com/freeasyman/lingce-api/internal/dealexpert/compiler/v200"

// RulesVersion remains the default legacy contract until a new release is accepted.
const RulesVersion = v200.RulesVersion

type Diagnostic = v200.Diagnostic
type Result = v200.Result
type Error = v200.Error

func Contracts() (input, intermediate, final []byte) { return v200.Contracts() }
func Compile(snapshot, intermediate []byte, rulesVersion string) (Result, error) {
	return v200.Compile(snapshot, intermediate, rulesVersion)
}
func ValidateSnapshot(snapshot []byte) error    { return v200.ValidateSnapshot(snapshot) }
func ValidateCard(card []byte) error            { return v200.ValidateCard(card) }
func SnapshotID(payload []byte) (string, error) { return v200.SnapshotID(payload) }
