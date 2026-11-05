package pipeline

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// The rc7 release is immutable evidence for the real 32-recording batch. Hard
// coding its identity makes an accidental prompt/schema edit fail loudly.
func TestV210RC7BundleIdentityFrozen(t *testing.T) {
	b, err := V210Bundle()
	if err != nil {
		t.Fatal(err)
	}
	const wantID = "sha256:8fe3272408f8ba6f56b12d2f2d0d2a35f39ba75d345ea91919a614517cc7d4ce"
	const wantContract = "sha256:495890ff34d2b2e5d24fc81b08e91555d677ec4b2d7547824b2251e802f3b2cf"
	if b.ID() != wantID {
		t.Fatalf("rc7 bundle changed: %s", b.ID())
	}
	if b.ContractHash() != wantContract {
		t.Fatalf("rc7 contract changed: %s", b.ContractHash())
	}
	if b.RulesVersion() != "deal-compiler-v2.1.0-rc7" {
		t.Fatal(b.RulesVersion())
	}
	if b.Spec().SystemPrompt != v210SystemPrompt {
		t.Fatal("rc7 prompt changed")
	}
	loaded, err := ReadBundle(b.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID() != b.ID() || loaded.RulesVersion() != b.RulesVersion() {
		t.Fatal("rc7 bundle did not load immutably")
	}
}

func TestV210RC8BundleIsExplicitAndLoadable(t *testing.T) {
	rc7, err := V210Bundle()
	if err != nil {
		t.Fatal(err)
	}
	rc8, err := V210BundleRC8()
	if err != nil {
		t.Fatal(err)
	}
	if rc8.RulesVersion() != compiler.RulesVersionV210RC8 {
		t.Fatalf("rules: %s", rc8.RulesVersion())
	}
	if rc8.Spec().Version != "deal-bundle-v2.1.0-rc8" || rc8.Spec().PromptVersion != "deal-analysis-prompt-v2.1.0-rc8" {
		t.Fatalf("rc8 versions: %+v", rc8.Spec())
	}
	if rc8.ID() == rc7.ID() || rc8.ContractHash() == rc7.ContractHash() {
		t.Fatal("rc8 reused rc7 identity")
	}
	t.Logf("rc8 bundle=%s contract=%s prompt_chars=%d", rc8.ID(), rc8.ContractHash(), len(rc8.Spec().SystemPrompt))
	if !strings.Contains(rc8.Spec().SystemPrompt, "输出形状") || !strings.Contains(rc8.Spec().SystemPrompt, "最小合法示例") {
		t.Fatal("rc8 prompt missing flat output skeleton")
	}
	if !strings.Contains(rc8.Spec().SystemPrompt, "禁止把根层的 clinical_risk") || !strings.Contains(rc8.Spec().SystemPrompt, "禁止把 concerns") {
		t.Fatal("rc8 prompt missing explicit nesting prohibitions")
	}
	if strings.Count(rc8.Spec().UserTemplate, "{{input_json}}") != 1 || !strings.Contains(rc8.Spec().UserTemplate, "输出形状复述") {
		t.Fatal("rc8 user template missing one placeholder or post-input reminder")
	}
	loaded, err := ReadBundle(rc8.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID() != rc8.ID() || loaded.RulesVersion() != compiler.RulesVersionV210RC8 {
		t.Fatal("rc8 bundle did not load immutably")
	}
	contract, err := compiler.SelectContract(compiler.RulesVersionV210RC8)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Version() != compiler.RulesVersionV210RC8 || string(loaded.Contract().Version()) != contract.Version() {
		t.Fatal("rc8 not recognized by version selection")
	}

	// Explicit selection only: the legacy default is still v2.0.0.
	def, err := DefaultBundle()
	if err != nil {
		t.Fatal(err)
	}
	if def.RulesVersion() == rc8.RulesVersion() || def.RulesVersion() != "deal-compiler-v2.0.0" {
		t.Fatal("default bundle changed")
	}
	// rc7 stays selectable and unchanged by rc8 publication.
	if again, err := V210Bundle(); err != nil || again.ID() != rc7.ID() {
		t.Fatal("publishing rc8 changed rc7")
	}
}

func TestV210RC8NewRunFromInputUsesV210Assembly(t *testing.T) {
	b, err := V210BundleRC8()
	if err != nil {
		t.Fatal(err)
	}
	original, _ := sampleRun(t)
	spec := original.Spec()
	run, err := NewRunFromInput(spec.ID, material(), b, spec.Model, nil)
	if err != nil {
		t.Fatal(err)
	}
	if run.Spec().BundleID != b.ID() {
		t.Fatal("rc8 run lost bundle binding")
	}
	var env struct {
		Input struct {
			Context map[string]any `json:"context"`
			Sources []any          `json:"transcript_sources"`
		} `json:"input"`
	}
	if err = json.Unmarshal(run.Spec().Snapshot, &env); err != nil {
		t.Fatal(err)
	}
	if _, ok := env.Input.Context["target_binding"]; !ok {
		t.Fatal("rc8 run did not use v2.1 input assembly")
	}
	if len(env.Input.Sources) == 0 {
		t.Fatal("rc8 run lost transcript sources")
	}
}

func TestV210RC8PromptExampleShapeAndSchema(t *testing.T) {
	b, err := V210BundleRC8()
	if err != nil {
		t.Fatal(err)
	}
	_, intermediate, _ := b.Contract().Contracts()
	if len(intermediate) == 0 {
		t.Fatal("missing rc8 intermediate schema")
	}
	example := strings.Replace(v210RC8OutputExample, "sha256:<逐字复制输入的 input_snapshot_id>", "sha256:"+strings.Repeat("0", 64), 1)
	var doc map[string]any
	if err = json.Unmarshal([]byte(example), &doc); err != nil {
		t.Fatal(err)
	}
	assertKeys(t, doc, []string{
		"protocol_version", "input_snapshot_id", "conclusion", "decision", "clinical_risk",
		"contact_request", "product_findings", "proposed_action", "secondary_findings",
		"quality", "coach", "confidence", "unknowns", "event_reports", "additional_decisions",
	}, "root")
	decision := doc["decision"].(map[string]any)
	assertKeys(t, decision, []string{
		"state", "target", "source_refs", "condition", "trust", "need", "concerns", "outcome_signals", "confidence",
	}, "decision")
	for _, forbidden := range []string{"clinical_risk", "contact_request", "product_findings", "proposed_action", "secondary_findings", "quality", "coach", "unknowns", "event_reports", "additional_decisions"} {
		if _, ok := decision[forbidden]; ok {
			t.Fatalf("top-level %s nested in decision", forbidden)
		}
	}
	need := decision["need"].(map[string]any)
	assertKeys(t, need, []string{"state", "summary", "source_refs", "support"}, "need")
	for _, forbidden := range []string{"concerns", "outcome_signals", "confidence", "target"} {
		if _, ok := need[forbidden]; ok {
			t.Fatalf("%s nested in need", forbidden)
		}
	}

	var schemaDoc map[string]any
	if err = json.Unmarshal(intermediate, &schemaDoc); err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	id := schemaDoc["$id"].(string)
	if err = c.AddResource(id, schemaDoc); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile(id)
	if err != nil {
		t.Fatal(err)
	}
	if err = schema.Validate(doc); err != nil {
		t.Fatalf("rc8 prompt example is not valid against the rc8 intermediate schema: %v", err)
	}
}

func assertKeys(t *testing.T, obj map[string]any, want []string, where string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for key := range obj {
		got = append(got, key)
	}
	sort.Strings(got)
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	if strings.Join(got, ",") != strings.Join(sorted, ",") {
		t.Fatalf("%s keys = %v, want %v", where, got, sorted)
	}
}
