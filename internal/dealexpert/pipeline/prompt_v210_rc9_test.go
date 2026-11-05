package pipeline

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
)

// rc8 is published evidence for the real 4-recording batch; publishing rc9 must
// not change it. The hash is hard coded so an accidental rc8 edit fails loudly.
func TestV210RC9LeavesRC8AndRC7Frozen(t *testing.T) {
	rc7, err := V210Bundle()
	if err != nil {
		t.Fatal(err)
	}
	rc8, err := V210BundleRC8()
	if err != nil {
		t.Fatal(err)
	}
	const wantRC8ID = "sha256:5645f6b4534e983e9b5d1ad336319dc3a95cf9b2c704780a33e7e920e088e2ba"
	const wantRC8Contract = "sha256:a36938d2c7e890b99238bd8677a37ccdd6a11fb01bccc03f50f18d213886bc7c"
	if rc8.ID() != wantRC8ID {
		t.Fatalf("rc8 bundle changed: %s", rc8.ID())
	}
	if rc8.ContractHash() != wantRC8Contract {
		t.Fatalf("rc8 contract changed: %s", rc8.ContractHash())
	}
	if rc7.ID() != "sha256:8fe3272408f8ba6f56b12d2f2d0d2a35f39ba75d345ea91919a614517cc7d4ce" {
		t.Fatalf("rc7 bundle changed: %s", rc7.ID())
	}
	if rc7.Spec().SystemPrompt != v210SystemPrompt {
		t.Fatal("rc7 prompt changed")
	}
	if rc8.Spec().SystemPrompt != v210RC8SystemPrompt {
		t.Fatal("rc8 prompt changed")
	}
	if rc8.Spec().UserTemplate != v210RC8UserTemplate {
		t.Fatal("rc8 user template changed")
	}
}

func TestV210RC9BundleIsExplicitAndLoadable(t *testing.T) {
	rc8, err := V210BundleRC8()
	if err != nil {
		t.Fatal(err)
	}
	rc9, err := V210BundleRC9()
	if err != nil {
		t.Fatal(err)
	}
	if rc9.RulesVersion() != compiler.RulesVersionV210RC9 {
		t.Fatalf("rules: %s", rc9.RulesVersion())
	}
	if rc9.Spec().Version != "deal-bundle-v2.1.0-rc9" || rc9.Spec().PromptVersion != "deal-analysis-prompt-v2.1.0-rc9" {
		t.Fatalf("rc9 versions: %+v", rc9.Spec())
	}
	if rc9.ID() == rc8.ID() || rc9.ContractHash() == rc8.ContractHash() {
		t.Fatal("rc9 reused rc8 identity")
	}
	t.Logf("rc9 bundle=%s contract=%s prompt_chars=%d user_template_chars=%d", rc9.ID(), rc9.ContractHash(), len(rc9.Spec().SystemPrompt), len(rc9.Spec().UserTemplate))

	// rc9 = rc8 prompt + exactly the two additions; nothing else moved.
	if !strings.HasPrefix(rc9.Spec().SystemPrompt, v210RC8SystemPrompt) {
		t.Fatal("rc9 prompt no longer extends the frozen rc8 prompt")
	}
	if !strings.HasPrefix(rc9.Spec().UserTemplate, v210RC8UserTemplate) {
		t.Fatal("rc9 user template no longer extends the frozen rc8 template")
	}
	if strings.Count(rc9.Spec().UserTemplate, "{{input_json}}") != 1 {
		t.Fatal("rc9 user template lost its single input placeholder")
	}
	loaded, err := ReadBundle(rc9.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID() != rc9.ID() || loaded.RulesVersion() != compiler.RulesVersionV210RC9 {
		t.Fatal("rc9 bundle did not load immutably")
	}
	contract, err := compiler.SelectContract(compiler.RulesVersionV210RC9)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Version() != compiler.RulesVersionV210RC9 || string(loaded.Contract().Version()) != contract.Version() {
		t.Fatal("rc9 not recognized by version selection")
	}

	// Explicit selection only: the legacy default is still v2.0.0.
	def, err := DefaultBundle()
	if err != nil {
		t.Fatal(err)
	}
	if def.RulesVersion() != "deal-compiler-v2.0.0" {
		t.Fatal("default bundle changed")
	}
	// rc8 stays selectable and unchanged after rc9 publication.
	if again, err := V210BundleRC8(); err != nil || again.ID() != rc8.ID() {
		t.Fatal("publishing rc9 changed rc8")
	}
}

// TestV210RC9CoachEnumMatchesProtocolSchema proves the prompt's coach enum list
// is copied from the shipped intermediate schema instead of invented.
func TestV210RC9CoachEnumMatchesProtocolSchema(t *testing.T) {
	b, err := V210BundleRC9()
	if err != nil {
		t.Fatal(err)
	}
	_, intermediate, _ := b.Contract().Contracts()
	var schema map[string]any
	if err = json.Unmarshal(intermediate, &schema); err != nil {
		t.Fatal(err)
	}
	props := schema["properties"].(map[string]any)
	coach := props["coach"].(map[string]any)["properties"].(map[string]any)
	behavior := coach["observations"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["behavior"].(map[string]any)["enum"].([]any)
	template := coach["improvement"].(map[string]any)["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["template"].(map[string]any)["enum"].([]any)

	schemaValues := make([]string, 0, len(behavior))
	for _, raw := range behavior {
		schemaValues = append(schemaValues, raw.(string))
	}
	sort.Strings(schemaValues)
	templateValues := make([]string, 0, len(template))
	for _, raw := range template {
		templateValues = append(templateValues, raw.(string))
	}
	sort.Strings(templateValues)
	if strings.Join(schemaValues, ",") != strings.Join(templateValues, ",") {
		t.Fatalf("schema coach enums diverge: behavior=%v template=%v", schemaValues, templateValues)
	}

	// The enum sentence in the prompt must list exactly the schema values.
	firstLine := strings.SplitN(v210RC9CoachEnumPrompt, "\n", 2)[0]
	parts := strings.SplitN(firstLine, "）：", 2)
	if len(parts) != 2 {
		t.Fatalf("coach enum prompt is not a single explicit list: %q", firstLine)
	}
	promptValues := strings.Split(strings.SplitN(parts[1], "。", 2)[0], "、")
	sort.Strings(promptValues)
	if strings.Join(promptValues, ",") != strings.Join(schemaValues, ",") {
		t.Fatalf("prompt coach enum = %v, schema = %v", promptValues, schemaValues)
	}
	for _, v := range schemaValues {
		if !strings.Contains(b.Spec().SystemPrompt, v) || !strings.Contains(b.Spec().UserTemplate, v) {
			t.Fatalf("coach enum %q missing from system prompt or user template", v)
		}
	}
	// The rc8 failure was borrowing proposed_action template names; rc9 must say
	// they are not coach values, without ever listing them as valid coach values.
	if !strings.Contains(v210RC9CoachEnumPrompt, "confirm_next_step") || !strings.Contains(v210RC9CoachEnumPrompt, "review_sources") {
		t.Fatal("coach prompt does not name the cross-field template names it forbids")
	}
	if !strings.Contains(v210RC9CoachEnumPrompt, "不是 coach 枚举值") {
		t.Fatal("coach prompt does not mark the borrowed template names as invalid")
	}
}

func TestV210RC9TimingHintIsRecallOnly(t *testing.T) {
	b, err := V210BundleRC9()
	if err != nil {
		t.Fatal(err)
	}
	prompt := b.Spec().SystemPrompt
	for _, want := range []string{"时机建议", "当天", "隔天", "术前", "术后", "customer_window_source_refs", "decision.condition", "时间安排不等于已预约"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("rc9 timing hint missing %q", want)
		}
	}
	// It must stay a recall hint, not a new mandatory rule or a keyword gate.
	if !strings.Contains(prompt, "只提醒召回") || !strings.Contains(prompt, "不要求每条录音都产出时机") || !strings.Contains(prompt, "不得编造时间") {
		t.Fatal("rc9 timing hint is not scoped as a recall-only reminder")
	}
	reminder := `时机建议复述`
	if !strings.Contains(b.Spec().UserTemplate, reminder) {
		t.Fatal("rc9 user template missing post-input timing reminder")
	}
	// The hint must not introduce any new protocol field.
	for _, forbidden := range []string{"timing_source_refs", "time_window", "appointment_time"} {
		if strings.Contains(prompt, forbidden) || strings.Contains(b.Spec().UserTemplate, forbidden) {
			t.Fatalf("rc9 prompt invented protocol field %q", forbidden)
		}
	}
}
