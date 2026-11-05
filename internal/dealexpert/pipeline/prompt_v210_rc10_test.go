package pipeline

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
)

// rc7/rc8/rc9 are published evidence for earlier real/synthetic batches;
// publishing rc10 must not change any of them. The rc9 hashes are hard coded so
// an accidental rc9 prompt or schema edit fails loudly.
func TestV210RC10LeavesRC9RC8RC7Frozen(t *testing.T) {
	rc7, err := V210Bundle()
	if err != nil {
		t.Fatal(err)
	}
	rc8, err := V210BundleRC8()
	if err != nil {
		t.Fatal(err)
	}
	rc9, err := V210BundleRC9()
	if err != nil {
		t.Fatal(err)
	}
	if rc7.ID() != "sha256:8fe3272408f8ba6f56b12d2f2d0d2a35f39ba75d345ea91919a614517cc7d4ce" {
		t.Fatalf("rc7 bundle changed: %s", rc7.ID())
	}
	if rc8.ID() != "sha256:5645f6b4534e983e9b5d1ad336319dc3a95cf9b2c704780a33e7e920e088e2ba" {
		t.Fatalf("rc8 bundle changed: %s", rc8.ID())
	}
	if rc8.ContractHash() != "sha256:a36938d2c7e890b99238bd8677a37ccdd6a11fb01bccc03f50f18d213886bc7c" {
		t.Fatalf("rc8 contract changed: %s", rc8.ContractHash())
	}
	// rc9 was published and executed for phase4-v210-real-20261101-03.
	if rc9.ID() != "sha256:a299812a3db01ce5da545b27c23dfadc958841ec9caaaa28ccbf36b434f4433b" {
		t.Fatalf("rc9 bundle changed: %s", rc9.ID())
	}
	if rc9.ContractHash() != "sha256:4d0ad4b185a5dd5602d4b4678681d6d5027636b6537b89e7a7fd8ac99449171f" {
		t.Fatalf("rc9 contract changed: %s", rc9.ContractHash())
	}
	if rc9.Spec().SystemPrompt != v210RC9SystemPrompt || rc9.Spec().UserTemplate != v210RC9UserTemplate {
		t.Fatal("rc9 prompt changed")
	}
}

func TestV210RC10BundleIsExplicitAndLoadable(t *testing.T) {
	rc9, err := V210BundleRC9()
	if err != nil {
		t.Fatal(err)
	}
	rc10, err := V210BundleRC10()
	if err != nil {
		t.Fatal(err)
	}
	if rc10.RulesVersion() != compiler.RulesVersionV210RC10 {
		t.Fatalf("rules: %s", rc10.RulesVersion())
	}
	if rc10.Spec().Version != "deal-bundle-v2.1.0-rc10" || rc10.Spec().PromptVersion != "deal-analysis-prompt-v2.1.0-rc10" {
		t.Fatalf("rc10 versions: %+v", rc10.Spec())
	}
	if rc10.ID() == rc9.ID() || rc10.ContractHash() == rc9.ContractHash() {
		t.Fatal("rc10 reused rc9 identity")
	}
	t.Logf("rc10 bundle=%s contract=%s prompt_chars=%d user_template_chars=%d", rc10.ID(), rc10.ContractHash(), len(rc10.Spec().SystemPrompt), len(rc10.Spec().UserTemplate))

	// rc10 = rc9 prompt + exactly the two semantic additions; nothing moved.
	if !strings.HasPrefix(rc10.Spec().SystemPrompt, v210RC9SystemPrompt) {
		t.Fatal("rc10 prompt no longer extends the frozen rc9 prompt")
	}
	if !strings.HasPrefix(rc10.Spec().UserTemplate, v210RC9UserTemplate) {
		t.Fatal("rc10 user template no longer extends the frozen rc9 template")
	}
	if strings.Count(rc10.Spec().UserTemplate, "{{input_json}}") != 1 {
		t.Fatal("rc10 user template lost its single input placeholder")
	}
	loaded, err := ReadBundle(rc10.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID() != rc10.ID() || loaded.RulesVersion() != compiler.RulesVersionV210RC10 {
		t.Fatal("rc10 bundle did not load immutably")
	}
	contract, err := compiler.SelectContract(compiler.RulesVersionV210RC10)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Version() != compiler.RulesVersionV210RC10 || string(loaded.Contract().Version()) != contract.Version() {
		t.Fatal("rc10 not recognized by version selection")
	}

	// Explicit selection only: the legacy default is still v2.0.0.
	def, err := DefaultBundle()
	if err != nil {
		t.Fatal(err)
	}
	if def.RulesVersion() != "deal-compiler-v2.0.0" {
		t.Fatal("default bundle changed")
	}
	// rc9 stays selectable and unchanged after rc10 publication.
	if again, err := V210BundleRC9(); err != nil || again.ID() != rc9.ID() {
		t.Fatal("publishing rc10 changed rc9")
	}
}

// TestV210RC10WordingConstraint checks the P2-2 prompt constraint: a tentative
// or conditional customer statement may not be upgraded into an explicit
// refusal, and a constraint on one scheme may not be widened to all schemes.
func TestV210RC10WordingConstraint(t *testing.T) {
	b, err := V210BundleRC10()
	if err != nil {
		t.Fatal(err)
	}
	prompt := b.Spec().SystemPrompt + "\n" + b.Spec().UserTemplate
	for _, want := range []string{
		"不得强化客户原话",
		"保留原话的疑问、试探、犹豫、假设、条件和否定范围",
		"只能写“暂缓”或“有意向但未决定”",
		"不得写“明确不考虑”",
		"“明确不考虑”只用于客户对同一方案给出直接、无条件、无试探措辞的拒绝",
		"一个方案的现实限制或暂缓，不得扩大成对客户全部方案或对手术的明确拒绝",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("rc10 wording constraint missing %q", want)
		}
	}
	// It is a prompt constraint, not a new field or a keyword gate.
	for _, forbidden := range []string{"wording_source_refs", "refusal_strength", "statement_strength"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("rc10 wording prompt invented protocol field %q", forbidden)
		}
	}
	// The constraint must not silently require a new state enum value: rc7's
	// original enum sentence is still the only state definition in the prompt.
	if strings.Count(prompt, "意向state只能是了解比较中/有意向但未决定/暂缓/明确不考虑/无法判断") != 1 {
		t.Fatal("rc10 redefined or duplicated the decision state enum")
	}
	if strings.Contains(v210RC10WordingPrompt, "state只能是") {
		t.Fatal("rc10 wording addition redefines the decision state enum")
	}
}

// TestV210RC10NeedTypeConstraint checks the P2-3 prompt constraint: symptoms,
// findings and staff claims are facts, not customer needs, and the symptom must
// still be preserved elsewhere rather than deleted to pass the type check.
func TestV210RC10NeedTypeConstraint(t *testing.T) {
	b, err := V210BundleRC10()
	if err != nil {
		t.Fatal(err)
	}
	prompt := b.Spec().SystemPrompt + "\n" + b.Spec().UserTemplate
	for _, want := range []string{
		"症状、体征、检查或影像结果、诊断、病程和员工医学陈述都是事实或临床信息，不是 customer_need",
		"customer_need 只能是客户自己表达出的愿望、要求、期待或明确想达成的结果",
		"只有症状陈述、没有客户愿望或要求原话时",
		"need.summary 不写症状改写",
		"症状仍必须保留",
		"clinical_risk",
		"secondary_findings",
		"症状原文不能出现在 kind=need 的录音级观察里",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("rc10 need-type constraint missing %q", want)
		}
	}
	// The constraint must be a classification rule, not a keyword blacklist:
	// no symptom word is listed as a trigger.
	for _, symptom := range []string{"漏尿", "尿频", "小便多", "疼痛", "腰痛"} {
		if strings.Contains(prompt, symptom) {
			t.Fatalf("rc10 prompt embeds a case-specific symptom keyword %q", symptom)
		}
	}
}

// TestV210RC10PromptDoesNotLeakSampleWording proves the two constraints were
// written generically instead of pasting the four evaluation recordings into the
// prompt. It also guards against accidental overfitting to the known failures.
func TestV210RC10PromptDoesNotLeakSampleWording(t *testing.T) {
	b, err := V210BundleRC10()
	if err != nil {
		t.Fatal(err)
	}
	prompt := b.Spec().SystemPrompt + "\n" + b.Spec().UserTemplate
	for _, leak := range []string{"八周", "椎间孔镜", "算了吧？", "不动刀还是不动刀", "小便多，还有漏尿", "周教授"} {
		if strings.Contains(prompt, leak) {
			t.Fatalf("rc10 prompt leaks sample wording %q", leak)
		}
	}
}

// TestV210RC10ConstraintsArePromptOnly proves the compiler and intermediate
// schema are unchanged from rc9; the semantic fix must come from the model
// instruction, not from a keyword branch in the compiler.
func TestV210RC10ConstraintsArePromptOnly(t *testing.T) {
	rc9, err := V210BundleRC9()
	if err != nil {
		t.Fatal(err)
	}
	rc10, err := V210BundleRC10()
	if err != nil {
		t.Fatal(err)
	}
	i9, m9, _ := rc9.Contract().Contracts()
	i10, m10, _ := rc10.Contract().Contracts()
	if string(i9) != string(i10) || string(m9) != string(m10) {
		t.Fatal("rc10 changed the input or intermediate schema")
	}
	var schema map[string]any
	if err = json.Unmarshal(m10, &schema); err != nil {
		t.Fatal(err)
	}
	// The two fixes are prose constraints, so neither may add a compiler-visible
	// keyword property to the intermediate contract.
	raw := string(m10)
	for _, invented := range []string{"symptom", "wording_strength", "refusal"} {
		if strings.Contains(raw, invented) {
			t.Fatalf("rc10 intermediate schema contains new keyword field %q", invented)
		}
	}
}
