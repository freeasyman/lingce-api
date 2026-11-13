package his

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteServiceErrorBirthDateRequired(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeServiceError(recorder, birthDateRequiredError())

	if recorder.Code != 400 {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Missing []MissingField `json:"missing_fields"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != "BAD_REQUEST" {
		t.Fatalf("code = %q, want BAD_REQUEST", body.Code)
	}
	if body.Message != "请填写出生日期" {
		t.Fatalf("message = %q, want 请填写出生日期", body.Message)
	}
	if len(body.Missing) != 1 || body.Missing[0].Field != "birth_date" || body.Missing[0].Label != "出生日期" {
		t.Fatalf("missing_fields = %+v, want birth_date/出生日期", body.Missing)
	}
}
