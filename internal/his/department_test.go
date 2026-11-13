package his

import (
	"strings"
	"testing"
)

func TestTemplateSupportsVisitType(t *testing.T) {
	cases := []struct {
		name      string
		template  *EMRTemplate
		visitType string
		want      bool
	}{
		{name: "初诊匹配初诊", template: &EMRTemplate{VisitType: "初诊"}, visitType: "初诊", want: true},
		{name: "通用匹配初诊", template: &EMRTemplate{VisitType: "通用"}, visitType: "初诊", want: true},
		{name: "通用匹配复诊", template: &EMRTemplate{VisitType: "通用"}, visitType: "复诊", want: true},
		{name: "初诊不能直接用于复诊", template: &EMRTemplate{VisitType: "初诊"}, visitType: "复诊", want: false},
		{name: "复诊不能直接用于初诊", template: &EMRTemplate{VisitType: "复诊"}, visitType: "初诊", want: false},
		{name: "空模板", template: nil, visitType: "复诊", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := templateSupportsVisitType(tc.template, tc.visitType); got != tc.want {
				t.Fatalf("templateSupportsVisitType(%+v, %q) = %v, want %v", tc.template, tc.visitType, got, tc.want)
			}
		})
	}
}

func TestTemplateVisitFallbackNotice(t *testing.T) {
	template := &EMRTemplate{Code: "gyn_first", Name: "妇科初诊病历", VisitType: "初诊"}
	notice := templateVisitFallbackNotice("复诊", template)
	if notice != "复诊模板缺失，已使用妇科初诊病历" {
		t.Fatalf("notice = %q", notice)
	}
	if !strings.Contains(notice, "妇科初诊病历") {
		t.Fatalf("notice 应包含实际使用的模板名: %q", notice)
	}
	if got := templateVisitFallbackNotice("复诊", nil); got != "" {
		t.Fatalf("nil template notice = %q, want empty", got)
	}
}

func TestTemplateFallbackMessage(t *testing.T) {
	cases := []struct {
		name string
		res  *templateResolution
		want string
	}{
		{name: "matched 无提示", res: &templateResolution{Matched: true}, want: ""},
		{name: "无科室", res: &templateResolution{Reason: templateReasonNoDepartment}, want: "本次就诊未关联科室，已使用标准门急诊病历；请补科室（排班/挂号）"},
		{name: "无 template_code", res: &templateResolution{Reason: templateReasonNoTemplateCode}, want: "科室未匹配模板（未配置 template_code），已使用标准门急诊病历"},
		{name: "模板未发布", res: &templateResolution{Reason: templateReasonTemplateNotPublished}, want: "科室未匹配模板（对应模板未发布），已使用标准门急诊病历"},
		{name: "无兜底模板", res: &templateResolution{Reason: templateReasonNoFallback}, want: "未找到可用病历模板，请联系管理员"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := templateFallbackMessage(tc.res); got != tc.want {
				t.Fatalf("templateFallbackMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTemplateUnavailableErrorMessage(t *testing.T) {
	err := &TemplateUnavailableError{Message: "未找到可用病历模板"}
	if err.Error() != "未找到可用病历模板" {
		t.Fatalf("Error() = %q", err.Error())
	}
	var nilErr *TemplateUnavailableError
	if nilErr.Error() == "" {
		t.Fatal("nil TemplateUnavailableError 也应返回可读文案")
	}
}
