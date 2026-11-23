package his

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTemplateFallbackMessage(t *testing.T) {
	cases := []struct {
		name string
		res  *templateResolution
		want string
	}{
		{name: "matched 无提示", res: &templateResolution{Matched: true}, want: ""},
		{name: "无科室", res: &templateResolution{Reason: templateReasonNoDepartment}, want: "本次就诊未关联科室，已使用标准门急诊病历；请补科室（排班/挂号）"},
		{name: "科室无专属模板", res: &templateResolution{Reason: templateReasonNoTemplateCode}, want: "科室未匹配模板（未配置 template_code），已使用标准门急诊病历"},
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

// 模板不可用必须是 400 + 明确 message（不能落成 500）。
func TestWriteServiceErrorTemplateUnavailable(t *testing.T) {
	rec := httptest.NewRecorder()
	writeServiceError(rec, &TemplateUnavailableError{
		Message: "未找到可用病历模板（科室模板与标准门急诊病历均不可用），请联系管理员检查模板发布状态",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "BAD_REQUEST" || body.Message == "" || !strings.Contains(body.Message, "未找到可用病历模板") {
		t.Fatalf("body = %+v", body)
	}
}
