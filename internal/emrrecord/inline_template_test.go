package emrrecord

import (
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/emrtemplate"
)

func TestNormalizeInlineTemplateValue(t *testing.T) {
	template := "双眼视力差（　）余年，近2年每年增加（<）0.5D，要求手术"

	// 模型按约定返回 slots：服务端拼回固定句式。
	got := normalizeInlineTemplateValue(map[string]any{"slots": []any{"20", "<"}}, template)
	if want := "双眼视力差20余年，近2年每年增加<0.5D，要求手术"; got != want {
		t.Fatalf("slots assemble = %q, want %q", got, want)
	}

	// 模型返回自由文本：不采信，回退到默认值拼出的句子，括号外文字保持原样。
	fallback := "双眼视力差　余年，近2年每年增加<0.5D，要求手术"
	if got := normalizeInlineTemplateValue("患者双眼视力差，要求手术", template); got != fallback {
		t.Fatalf("free text fallback = %q, want %q", got, fallback)
	}

	// 模型返回完整句式且包含全部固定文字：采信。
	full := "双眼视力差20余年，近2年每年增加<0.5D，要求手术"
	if got := normalizeInlineTemplateValue(full, template); got != full {
		t.Fatalf("full sentence = %q, want %q", got, full)
	}

	// slots 数量不匹配：回退默认值。
	if got := normalizeInlineTemplateValue(map[string]any{"slots": []any{"20"}}, template); got != fallback {
		t.Fatalf("slot count mismatch = %q, want %q", got, fallback)
	}
}

func TestInlineTemplateOf(t *testing.T) {
	section := &emrtemplate.Section{Structure: map[string]any{"template": "主诉（　）"}}
	if _, ok := inlineTemplateOf(section); !ok {
		t.Fatal("expected inline template")
	}
	if _, ok := inlineTemplateOf(&emrtemplate.Section{Structure: map[string]any{}}); ok {
		t.Fatal("expected no inline template")
	}
	if _, ok := inlineTemplateOf(nil); ok {
		t.Fatal("nil section must not have inline template")
	}
}

func TestRenderGenerationFieldSpecsIncludesInlineTemplate(t *testing.T) {
	sections := []*emrtemplate.Section{
		{Code: "chief_complaint", Name: "主诉", InputType: "长文本", Structure: map[string]any{"template": "双眼视力差（　）余年，要求手术"}},
	}
	got := renderGenerationFieldSpecs(sections)
	if !strings.Contains(got, "固定句式") || !strings.Contains(got, "slots") {
		t.Fatalf("spec must include fixed sentence and slots contract, got: %s", got)
	}
	if strings.Contains(got, "返回简洁连贯的自然语言字符串") {
		t.Fatalf("inline template section must not use free-text instruction, got: %s", got)
	}
}

func TestAppendInlineTemplateSystemRule(t *testing.T) {
	sections := []*emrtemplate.Section{
		{Code: "chief_complaint", InputType: "长文本", Structure: map[string]any{"template": "主诉（　）"}},
	}
	got := appendInlineTemplateSystemRule("base", sections)
	if !strings.Contains(got, "固定句式") || !strings.Contains(got, "slots") {
		t.Fatalf("system rule must mention fixed sentence and slots, got: %s", got)
	}
	if got := appendInlineTemplateSystemRule("base", []*emrtemplate.Section{{Code: "x"}}); got != "base" {
		t.Fatalf("no inline template should keep system prompt unchanged, got: %s", got)
	}
}

func TestRenderGenerationFieldSpecsIncludesSlotHints(t *testing.T) {
	sections := []*emrtemplate.Section{
		{Code: "present_illness", Name: "现病史", InputType: "长文本", Structure: map[string]any{"template": "曾戴角膜接触镜（　）年，摘镜（　）周。眼干涩（有/无）。"}},
	}
	got := renderGenerationFieldSpecs(sections)
	if !strings.Contains(got, "槽位填写要求") || !strings.Contains(got, "数值（单位：年）") {
		t.Fatalf("spec must include slot hints, got: %s", got)
	}
}
