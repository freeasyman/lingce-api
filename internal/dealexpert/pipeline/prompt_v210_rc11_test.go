package pipeline

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
)

// rc7-rc10 are published evidence for earlier real/synthetic batches;
// publishing rc11 must not change them. Hashes are hard coded so an accidental
// prompt or schema edit fails loudly.
func TestV210RC11LeavesRC10Frozen(t *testing.T) {
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
	rc10, err := V210BundleRC10()
	if err != nil {
		t.Fatal(err)
	}
	if rc7.ID() != "sha256:8fe3272408f8ba6f56b12d2f2d0d2a35f39ba75d345ea91919a614517cc7d4ce" {
		t.Fatalf("rc7 bundle changed: %s", rc7.ID())
	}
	if rc8.ID() != "sha256:5645f6b4534e983e9b5d1ad336319dc3a95cf9b2c704780a33e7e920e088e2ba" {
		t.Fatalf("rc8 bundle changed: %s", rc8.ID())
	}
	if rc9.ID() != "sha256:a299812a3db01ce5da545b27c23dfadc958841ec9caaaa28ccbf36b434f4433b" {
		t.Fatalf("rc9 bundle changed: %s", rc9.ID())
	}
	if rc10.ID() != "sha256:635fc56335be0c86faf8ea9deb042d1e0b51bf66137ede84ba30a8b75c7ecdce" {
		t.Fatalf("rc10 bundle changed: %s", rc10.ID())
	}
	if rc10.ContractHash() != "sha256:73e0e1efa022b1b9535d4f4376830ad02f730f54a2a51c08024bba467bb1e2a2" {
		t.Fatalf("rc10 contract changed: %s", rc10.ContractHash())
	}
	if rc10.Spec().SystemPrompt != v210RC10SystemPrompt || rc10.Spec().UserTemplate != v210RC10UserTemplate {
		t.Fatal("rc10 prompt changed")
	}
}

func TestV210RC11BundleIsExplicitAndLoadable(t *testing.T) {
	rc10, err := V210BundleRC10()
	if err != nil {
		t.Fatal(err)
	}
	rc11, err := V210BundleRC11()
	if err != nil {
		t.Fatal(err)
	}
	if rc11.RulesVersion() != compiler.RulesVersionV210RC11 {
		t.Fatalf("rules: %s", rc11.RulesVersion())
	}
	if rc11.Spec().Version != "deal-bundle-v2.1.0-rc11" || rc11.Spec().PromptVersion != "deal-analysis-prompt-v2.1.0-rc11" {
		t.Fatalf("rc11 versions: %+v", rc11.Spec())
	}
	if rc11.ID() == rc10.ID() || rc11.ContractHash() == rc10.ContractHash() {
		t.Fatal("rc11 reused rc10 identity")
	}
	t.Logf("rc11 bundle=%s contract=%s prompt_chars=%d user_template_chars=%d", rc11.ID(), rc11.ContractHash(), len(rc11.Spec().SystemPrompt), len(rc11.Spec().UserTemplate))

	// rc11 = rc10 prompt + exactly the rc11 additions; nothing moved.
	if !strings.HasPrefix(rc11.Spec().SystemPrompt, v210RC10SystemPrompt) {
		t.Fatal("rc11 prompt no longer extends the frozen rc10 prompt")
	}
	if !strings.HasPrefix(rc11.Spec().UserTemplate, v210RC10UserTemplate) {
		t.Fatal("rc11 user template no longer extends the frozen rc10 template")
	}
	if strings.Count(rc11.Spec().UserTemplate, "{{input_json}}") != 1 {
		t.Fatal("rc11 user template lost its single input placeholder")
	}
	loaded, err := ReadBundle(rc11.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID() != rc11.ID() || loaded.RulesVersion() != compiler.RulesVersionV210RC11 {
		t.Fatal("rc11 bundle did not load immutably")
	}
	contract, err := compiler.SelectContract(compiler.RulesVersionV210RC11)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Version() != compiler.RulesVersionV210RC11 || string(loaded.Contract().Version()) != contract.Version() {
		t.Fatal("rc11 not recognized by version selection")
	}

	// Explicit selection only: the legacy default is still v2.0.0.
	def, err := DefaultBundle()
	if err != nil {
		t.Fatal(err)
	}
	if def.RulesVersion() != "deal-compiler-v2.0.0" {
		t.Fatal("default bundle changed")
	}
	if again, err := V210BundleRC10(); err != nil || again.ID() != rc10.ID() {
		t.Fatal("publishing rc11 changed rc10")
	}
}

func TestV210RC11AttributionConstraint(t *testing.T) {
	b, err := V210BundleRC11()
	if err != nil {
		t.Fatal(err)
	}
	prompt := b.Spec().SystemPrompt + "\n" + b.Spec().UserTemplate
	for _, want := range []string{
		"员工陈述、医学适配结论、其他人员说法和未知说话人内容都不是客户个人意向",
		"不得生成 kind=intent 的客户意向观察",
		"“明确不考虑/明确拒绝”只能用于客户本人对同一方案直接、无条件、无转述的拒绝",
		"只作为录音级待核实观察（员工/未知来源）保留",
		"一个员工结论只作用于它提到的方案",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("rc11 attribution constraint missing %q", want)
		}
	}
	// This must not become a keyword gate on a new decision state.
	if strings.Contains(v210RC11AttributionPrompt, "state只能是") {
		t.Fatal("rc11 addition redefines the decision state enum")
	}
}

func TestV210RC11ScopeAndSpanConstraint(t *testing.T) {
	b, err := V210BundleRC11()
	if err != nil {
		t.Fatal(err)
	}
	prompt := b.Spec().SystemPrompt + "\n" + b.Spec().UserTemplate
	for _, want := range []string{
		"analysis_scope",
		"non_business",
		"不得把界面意见写成医疗发现、客户需求、销售线索或成交信号",
		"support.spans 必须精确指向客户本人发言的 Unicode 字符区间",
		"不要把整段多人转写复制到 need/concern/trust 的 summary",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("rc11 scope/span constraint missing %q", want)
		}
	}
}

// TestV210RC11SchemaAddsOptionalScope checks the additive contract: old rc10
// intermediates (no analysis_scope) must still validate under rc11, while the
// rc10 schema bytes themselves do not move.
func TestV210RC11SchemaAddsOptionalScope(t *testing.T) {
	rc10, err := V210BundleRC10()
	if err != nil {
		t.Fatal(err)
	}
	rc11, err := V210BundleRC11()
	if err != nil {
		t.Fatal(err)
	}
	i10, m10, f10 := rc10.Contract().Contracts()
	i11, m11, f11 := rc11.Contract().Contracts()
	if string(i10) != string(i11) {
		t.Fatal("rc11 changed the frozen input schema")
	}
	if string(m10) == string(m11) {
		t.Fatal("rc11 did not add its own intermediate schema")
	}
	if string(f10) == string(f11) {
		t.Fatal("rc11 reused rc10 final card schema")
	}
	var mid map[string]any
	if err = json.Unmarshal(m11, &mid); err != nil {
		t.Fatal(err)
	}
	props := mid["properties"].(map[string]any)
	scope, ok := props["analysis_scope"].(map[string]any)
	if !ok {
		t.Fatal("rc11 intermediate schema lacks analysis_scope")
	}
	for _, raw := range mid["required"].([]any) {
		if raw == "analysis_scope" {
			t.Fatal("analysis_scope must stay optional so rc10 responses recompile")
		}
	}
	scopeProps := scope["properties"].(map[string]any)
	kind := scopeProps["kind"].(map[string]any)["enum"].([]any)
	if len(kind) != 3 {
		t.Fatalf("analysis_scope kind enum: %+v", kind)
	}
	var card map[string]any
	if err = json.Unmarshal(f11, &card); err != nil {
		t.Fatal(err)
	}
	cardProps := card["properties"].(map[string]any)
	if _, ok := cardProps["analysis_scope"]; !ok {
		t.Fatal("rc11 card schema lacks analysis_scope")
	}
	obsKind := cardProps["recording_observations"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]any)
	seen := map[string]bool{}
	for _, raw := range obsKind {
		seen[raw.(string)] = true
	}
	if !seen["staff_claim"] || !seen["non_business"] {
		t.Fatalf("rc11 observation kinds missing: %+v", obsKind)
	}
}

// TestV210RC11PromptDoesNotLeakSampleWording guards against overfitting the
// three fixes to the evaluation recordings.
func TestV210RC11PromptDoesNotLeakSampleWording(t *testing.T) {
	b, err := V210BundleRC11()
	if err != nil {
		t.Fatal(err)
	}
	prompt := b.Spec().SystemPrompt + "\n" + b.Spec().UserTemplate
	for _, leak := range []string{"晶体植入", "pass 掉了", "ICL", "900度", "漏尿", "周教授", "八周", "三焦点", "分隔线", "工牌"} {
		if strings.Contains(prompt, leak) {
			t.Fatalf("rc11 prompt leaks sample wording %q", leak)
		}
	}
}

func TestV210RC11P1KeepsRC11Frozen(t *testing.T) {
	rc11, err := V210BundleRC11()
	if err != nil {
		t.Fatal(err)
	}
	p1, err := V210BundleRC11P1()
	if err != nil {
		t.Fatal(err)
	}
	if p1.RulesVersion() != compiler.RulesVersionV210RC11 {
		t.Fatalf("rc11.1 rules: %s", p1.RulesVersion())
	}
	if p1.Spec().Version != "deal-bundle-v2.1.0-rc11.1" || p1.Spec().PromptVersion != "deal-analysis-prompt-v2.1.0-rc11.1" {
		t.Fatalf("rc11.1 versions: %+v", p1.Spec())
	}
	if p1.ID() == rc11.ID() {
		t.Fatal("rc11.1 reused rc11 bundle identity")
	}
	if !strings.HasPrefix(p1.Spec().SystemPrompt, v210RC11SystemPrompt) {
		t.Fatal("rc11.1 prompt no longer extends rc11")
	}
	if !strings.HasPrefix(p1.Spec().UserTemplate, v210RC11UserTemplate) {
		t.Fatal("rc11.1 user template no longer extends rc11")
	}
	if strings.Count(p1.Spec().UserTemplate, "{{input_json}}") != 1 {
		t.Fatal("rc11.1 lost the input placeholder")
	}
	loaded, err := ReadBundle(p1.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID() != p1.ID() || loaded.RulesVersion() != compiler.RulesVersionV210RC11 {
		t.Fatal("rc11.1 did not load immutably")
	}
	if again, err := V210BundleRC11(); err != nil || again.ID() != rc11.ID() {
		t.Fatal("publishing rc11.1 changed rc11")
	}
	t.Logf("rc11.1 bundle=%s contract=%s", p1.ID(), p1.ContractHash())
}
