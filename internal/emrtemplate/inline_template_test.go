package emrtemplate

import "testing"

func TestInlineTemplateSegmentsAndAssemble(t *testing.T) {
	template := "双眼视力差（　）余年，近2年每年增加（<）0.5D，要求手术"
	segments, defaults := InlineTemplateSegments(template)
	if len(segments) != 3 || len(defaults) != 2 {
		t.Fatalf("segments=%d defaults=%d, want 3/2", len(segments), len(defaults))
	}
	if got, want := AssembleInlineTemplate(segments, defaults, []string{"20", "<"}), "双眼视力差20余年，近2年每年增加<0.5D，要求手术"; got != want {
		t.Fatalf("assemble = %q, want %q", got, want)
	}
	// 空槽位回退默认值：括号是编辑标记，拼回的是去括号的临床句子。
	if got, want := AssembleInlineTemplate(segments, defaults, nil), "双眼视力差　余年，近2年每年增加<0.5D，要求手术"; got != want {
		t.Fatalf("default assemble = %q, want %q", got, want)
	}
}

func TestInlineTemplateEmptyParentheses(t *testing.T) {
	template := "晶状体（清），散瞳后晶状体（），视盘色可"
	segments, defaults := InlineTemplateSegments(template)
	if len(defaults) != 2 || defaults[1] != "" {
		t.Fatalf("defaults=%q, want 2 slots with second empty", defaults)
	}
	if got, want := AssembleInlineTemplate(segments, defaults, []string{"清", ""}), "晶状体清，散瞳后晶状体，视盘色可"; got != want {
		t.Fatalf("assemble = %q, want %q", got, want)
	}
}

func TestInlineTemplateSlotHints(t *testing.T) {
	cases := []struct {
		template string
		want     []string
	}{
		{
			template: "双眼视力差（　）余年，近2年每年增加（<）0.5D，要求手术",
			want:     []string{"数值（单位：余年）", "填写符号或数值（如 <、≤、>）"},
		},
		{
			template: "曾戴角膜接触镜（　）年，摘镜（　）周（　）天。眼干涩（有/无）。",
			want:     []string{"数值（单位：年）", "数值（单位：周）", "数值（单位：天）", "从默认值中选择一项（有/无）"},
		},
		{
			template: "睑缘形态（正常/肥厚），鳞屑（+/-），瞳孔直径约（3）mm。",
			want:     []string{"从默认值中选择一项（正常/肥厚）", "从默认值中选择一项（+/-）", "数值（单位：mm）"},
		},
		{
			template: "眼外伤手术史（/），视网膜脱离病史（/）。",
			want:     []string{"填写有/无/不详或具体描述", "填写有/无/不详或具体描述"},
		},
	}
	for _, tc := range cases {
		got := InlineTemplateSlotHints(tc.template)
		if len(got) != len(tc.want) {
			t.Fatalf("hints=%v, want %v", got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("hint[%d]=%q, want %q (all=%v)", i, got[i], tc.want[i], got)
			}
		}
	}
}
