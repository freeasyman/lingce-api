package pipeline

import (
	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
	"testing"
)

func TestEffectivePromptRejectsLegacyAndAcceptsIntermediate(t *testing.T) {
	_, intermediate, _ := compiler.Contracts()
	base := EffectivePrompt{Version: "deal-analysis-prompt-v2.0.0", SystemPrompt: "system", UserTemplate: "{{input_json}}", OutputSchema: intermediate}
	b, err := NewBundleFromEffectivePrompt(0, base, Bundle{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Spec().PromptVersion != base.Version {
		t.Fatal("prompt version not bound")
	}
	legacy := base
	_, _, legacySchema := compiler.Contracts()
	legacy.OutputSchema = legacySchema
	if _, err := NewBundleFromEffectivePrompt(0, legacy, Bundle{}); err == nil {
		t.Fatal("legacy final schema accepted")
	}
	bad := base
	bad.UserTemplate = "old ev_1"
	if _, err := NewBundleFromEffectivePrompt(0, bad, Bundle{}); err == nil {
		t.Fatal("missing placeholder accepted")
	}
}
func TestEffectiveTenantRequiresGlobalParent(t *testing.T) {
	_, intermediate, _ := compiler.Contracts()
	p := EffectivePrompt{Version: "p2", SystemPrompt: "system", UserTemplate: "{{input_json}}", OutputSchema: intermediate}
	if _, err := NewBundleFromEffectivePrompt(17, p, Bundle{}); err == nil {
		t.Fatal("tenant parent not rejected")
	}
	parent, err := NewBundle(BundleSpec{Version: "global", PromptVersion: "global-prompt", SystemPrompt: "system", UserTemplate: "{{input_json}}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewBundleFromEffectivePrompt(17, p, parent); err != nil {
		t.Fatal(err)
	}
}
