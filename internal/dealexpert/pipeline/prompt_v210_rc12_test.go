package pipeline

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
)

// TestV210RC12LeavesRC11Frozen pins the rc11 bundle identity so publishing rc12
// cannot silently change an already executed real batch.
func TestV210RC12LeavesRC11Frozen(t *testing.T) {
	rc11, err := V210BundleRC11()
	if err != nil {
		t.Fatal(err)
	}
	if rc11.ID() != "sha256:6746ceea5ac09d2af66c06a5a3a18cbabab9646ccfbc7822602929f7c7d808a2" {
		t.Fatalf("rc11 bundle changed: %s", rc11.ID())
	}
	if rc11.ContractHash() != "sha256:afffb5b3d1fc00a05bdcfc4fdd17da7f8c79c9f86299babe24f8e6e05423bd15" {
		t.Fatalf("rc11 contract changed: %s", rc11.ContractHash())
	}
	if rc11.Spec().SystemPrompt != v210RC11SystemPrompt || rc11.Spec().UserTemplate != v210RC11UserTemplate {
		t.Fatal("rc11 prompt changed")
	}
}

func TestV210RC12BundleIsExplicitAndLoadable(t *testing.T) {
	rc11, err := V210BundleRC11()
	if err != nil {
		t.Fatal(err)
	}
	rc12, err := V210BundleRC12()
	if err != nil {
		t.Fatal(err)
	}
	if rc12.RulesVersion() != compiler.RulesVersionV210RC12 {
		t.Fatalf("rules: %s", rc12.RulesVersion())
	}
	if rc12.Spec().Version != "deal-bundle-v2.1.0-rc12" || rc12.Spec().PromptVersion != "deal-analysis-prompt-v2.1.0-rc12" {
		t.Fatalf("rc12 versions: %+v", rc12.Spec())
	}
	if rc12.ID() == rc11.ID() || rc12.ContractHash() == rc11.ContractHash() {
		t.Fatal("rc12 reused rc11 identity")
	}
	t.Logf("rc12 bundle=%s contract=%s prompt_chars=%d user_template_chars=%d", rc12.ID(), rc12.ContractHash(), len(rc12.Spec().SystemPrompt), len(rc12.Spec().UserTemplate))
	if !strings.HasPrefix(rc12.Spec().SystemPrompt, v210RC11SystemPrompt) {
		t.Fatal("rc12 prompt no longer extends the frozen rc11 prompt")
	}
	if !strings.HasPrefix(rc12.Spec().UserTemplate, v210RC11UserTemplate) {
		t.Fatal("rc12 user template no longer extends the frozen rc11 template")
	}
	if strings.Count(rc12.Spec().UserTemplate, "{{input_json}}") != 1 {
		t.Fatal("rc12 user template lost its single input placeholder")
	}
	loaded, err := ReadBundle(rc12.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID() != rc12.ID() || loaded.RulesVersion() != compiler.RulesVersionV210RC12 {
		t.Fatal("rc12 bundle did not load immutably")
	}
	def, err := DefaultBundle()
	if err != nil {
		t.Fatal(err)
	}
	if def.RulesVersion() != "deal-compiler-v2.0.0" {
		t.Fatal("default bundle changed")
	}
	if again, err := V210BundleRC11(); err != nil || again.ID() != rc11.ID() {
		t.Fatal("publishing rc12 changed rc11")
	}
}

// TestV210RC12LocalObservationConstraint checks the prompt restates the channel
// and that the requirement is a verbatim source channel, not a paraphrase or a
// date conversion rule.
func TestV210RC12LocalObservationConstraint(t *testing.T) {
	b, err := V210BundleRC12()
	if err != nil {
		t.Fatal(err)
	}
	prompt := b.Spec().SystemPrompt + "\n" + b.Spec().UserTemplate
	for _, want := range []string{
		"local_observations",
		"summary 必须逐字复制所引来源中的一个连续片段",
		"不得改写、概括、换近义词",
		"timing_kind",
		"relative_window",
		"不得把口语日期改写成数字日期",
		"意愿必须单列 kind=willingness 并用原词",
		"查体与现场观察写 kind=exam",
		"不得因此删除已经出现的时机、意愿、诉求和查体观察",
		"同一段原话只发布一次",
		"不超过240字",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("rc12 local-observation constraint missing %q", want)
		}
	}
	// The new channel must not weaken rc11's intent/refusal constraint.
	if !strings.Contains(prompt, "不得生成 kind=intent 的客户意向观察") {
		t.Fatal("rc12 prompt lost the rc11 attribution constraint")
	}
}

// TestV210RC12PromptDoesNotLeakSampleWording guards against overfitting the fix
// to the evaluated recordings.
func TestV210RC12PromptDoesNotLeakSampleWording(t *testing.T) {
	b, err := V210BundleRC12()
	if err != nil {
		t.Fatal(err)
	}
	prompt := b.Spec().SystemPrompt + "\n" + b.Spec().UserTemplate
	for _, leak := range []string{
		"不想做", "没想好", "不想查", "8月1", "两周", "按这个位置", "抬起来", "抬高",
		"怀孕", "散瞳", "干眼", "腰疼", "王医生", "孩子眼睛疼", "八周", "漏尿",
	} {
		if strings.Contains(prompt, leak) {
			t.Fatalf("rc12 prompt leaks sample wording %q", leak)
		}
	}
}

// TestV210RC12ContractIsAdditiveOnly checks the schemas actually shipped by the
// bundle: rc11's are untouched, rc12 adds the optional channel and the timing
// slot, and the earlier bundles keep their exact bytes.
func TestV210RC12ContractIsAdditiveOnly(t *testing.T) {
	rc11, err := V210BundleRC11()
	if err != nil {
		t.Fatal(err)
	}
	rc12, err := V210BundleRC12()
	if err != nil {
		t.Fatal(err)
	}
	i11, m11, f11 := rc11.Contract().Contracts()
	i12, m12, f12 := rc12.Contract().Contracts()
	if string(i11) != string(i12) {
		t.Fatal("rc12 changed the frozen input schema")
	}
	if string(m11) == string(m12) || string(f11) == string(f12) {
		t.Fatal("rc12 reused rc11 schema bytes")
	}
	var mid map[string]any
	if err := json.Unmarshal(m12, &mid); err != nil {
		t.Fatal(err)
	}
	channel, ok := mid["properties"].(map[string]any)["local_observations"].(map[string]any)
	if !ok {
		t.Fatal("rc12 intermediate schema lacks local_observations")
	}
	for _, raw := range mid["required"].([]any) {
		if raw == "local_observations" {
			t.Fatal("local_observations must stay optional so rc11 responses recompile")
		}
	}
	kinds := channel["items"].(map[string]any)["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]any)
	seen := map[string]bool{}
	for _, raw := range kinds {
		seen[raw.(string)] = true
	}
	for _, want := range []string{"timing", "willingness", "exam", "symptom", "question", "staff_statement", "other"} {
		if !seen[want] {
			t.Fatalf("rc12 local observation kinds missing %q: %+v", want, kinds)
		}
	}
}

// TestV210RC12P1KeepsRC12Frozen pins the rc12 bundle produced by real batch 09 and
// checks the repair package is additive.
func TestV210RC12P1KeepsRC12Frozen(t *testing.T) {
	rc12, err := V210BundleRC12()
	if err != nil {
		t.Fatal(err)
	}
	if rc12.ID() != "sha256:b309c39a7b0a2b394e020f4255e276b98693d506f60730ae6046088682f44edb" {
		t.Fatalf("rc12 bundle changed: %s", rc12.ID())
	}
	if !strings.HasPrefix(v210RC12P1SystemPrompt, v210RC12SystemPrompt) {
		t.Fatal("rc12.1 prompt no longer extends rc12")
	}
	if !strings.HasPrefix(v210RC12P1UserTemplate, v210RC12UserTemplate) {
		t.Fatal("rc12.1 user template no longer extends rc12")
	}
	rc12p1, err := V210BundleRC12P1()
	if err != nil {
		t.Fatal(err)
	}
	if rc12p1.RulesVersion() != compiler.RulesVersionV210RC12P1 {
		t.Fatalf("rc12.1 rules: %s", rc12p1.RulesVersion())
	}
	if rc12p1.Spec().Version != "deal-bundle-v2.1.0-rc12.1" || rc12p1.Spec().PromptVersion != "deal-analysis-prompt-v2.1.0-rc12.1" {
		t.Fatalf("rc12.1 versions: %+v", rc12p1.Spec())
	}
	if rc12p1.ID() == rc12.ID() || rc12p1.ContractHash() == rc12.ContractHash() {
		t.Fatal("rc12.1 reused rc12 identity")
	}
	if strings.Count(rc12p1.Spec().UserTemplate, "{{input_json}}") != 1 {
		t.Fatal("rc12.1 lost the input placeholder")
	}
	loaded, err := ReadBundle(rc12p1.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID() != rc12p1.ID() {
		t.Fatal("rc12.1 did not load immutably")
	}
	t.Logf("rc12.1 bundle=%s contract=%s", rc12p1.ID(), rc12p1.ContractHash())
	combined := rc12p1.Spec().SystemPrompt + "\n" + rc12p1.Spec().UserTemplate
	for _, want := range []string{
		"意愿说法即使出自未知说话人、或由员工提问/转述，也要单列 kind=willingness",
		"相对时间范围（几天、几周、一周、半个月、下个月等）无论出现在哪一句里",
		"timing_kind 写为 relative_window",
		"说到某个具体日期时 timing_kind 写 absolute_date",
		"每项必须是扁平对象",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("rc12.1 routing prompt missing %q", want)
		}
	}
	for _, leak := range []string{"不想做", "不想查", "两周", "8月1", "按这个位置"} {
		if strings.Contains(rc12p1.Spec().SystemPrompt+rc12p1.Spec().UserTemplate, leak) {
			t.Fatalf("rc12.1 prompt leaks sample wording %q", leak)
		}
	}
}
