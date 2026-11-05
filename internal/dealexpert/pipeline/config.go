package pipeline

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
)

type EffectivePrompt struct {
	Version                string
	SystemPrompt           string
	UserTemplate           string
	OutputSchema           []byte
	AdditionalInstructions string
}

// NewBundleFromEffectivePrompt is the only adapter intended for DB-loaded
// prompt rows. It rejects legacy final-card schemas and tenant overrides that
// still let a model emit server-owned evidence fields.
func NewBundleFromEffectivePrompt(tenantID int64, prompt EffectivePrompt, parent Bundle) (Bundle, error) {
	if strings.TrimSpace(prompt.Version) == "" || strings.TrimSpace(prompt.SystemPrompt) == "" || strings.Count(prompt.UserTemplate, "{{input_json}}") != 1 {
		return Bundle{}, fmt.Errorf("effective prompt is incomplete")
	}
	selected, err := compiler.SelectContract(compiler.RulesVersion)
	if err != nil {
		return Bundle{}, err
	}
	if tenantID > 0 && parent.ID() != "" {
		selected = parent.Contract()
	}
	_, intermediate, _ := selected.Contracts()
	var got, expected map[string]any
	if json.Unmarshal(prompt.OutputSchema, &got) != nil || json.Unmarshal(intermediate, &expected) != nil {
		return Bundle{}, fmt.Errorf("effective output schema is invalid JSON")
	}
	if !reflect.DeepEqual(got, expected) {
		return Bundle{}, fmt.Errorf("effective prompt must use deal-analysis-v2 intermediate schema")
	}
	props, _ := got["properties"].(map[string]any)
	if _, ok := props["evidence_refs"]; ok {
		return Bundle{}, fmt.Errorf("intermediate schema unexpectedly exposes final evidence")
	}
	system := prompt.SystemPrompt
	if strings.TrimSpace(prompt.AdditionalInstructions) != "" {
		system += "\n\n" + strings.TrimSpace(prompt.AdditionalInstructions)
	}
	spec := BundleSpec{TenantID: tenantID, Version: "deal-bundle-" + prompt.Version, PromptVersion: prompt.Version, SystemPrompt: system, UserTemplate: prompt.UserTemplate}
	if tenantID > 0 {
		if parent.ID() == "" || parent.spec.TenantID != 0 {
			return Bundle{}, fmt.Errorf("tenant bundle requires global parent spec")
		}
		spec.ParentBundleID = parent.ID()
	}
	return NewBundleForRules(spec, selected.Version())
}
