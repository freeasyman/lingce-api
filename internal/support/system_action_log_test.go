package support

import "testing"

func TestNormalizeSystemActionLogWriteRequest(t *testing.T) {
	req := SystemActionLogWriteRequest{
		LogType:    "  ops  ",
		ActionCode: "  employee_update  ",
		ActionName: "   ",
		Result:     "  FAILURE  ",
	}

	normalizeSystemActionLogWriteRequest(&req)

	if req.LogType != "ops" {
		t.Fatalf("expected log type to be trimmed, got %q", req.LogType)
	}
	if req.ActionCode != "employee_update" {
		t.Fatalf("expected action code to be trimmed, got %q", req.ActionCode)
	}
	if req.ActionName != "" {
		t.Fatalf("expected empty action name to stay empty, got %q", req.ActionName)
	}
	if req.Result != "failure" {
		t.Fatalf("expected result to be normalized, got %q", req.Result)
	}
}

func TestIsValidSystemActionLogCode(t *testing.T) {
	okValues := []string{"ops", "employee_update", "recording:write", "audit-log"}
	for _, value := range okValues {
		if !isValidSystemActionLogCode(value) {
			t.Fatalf("expected %q to be valid", value)
		}
	}

	badValues := []string{"", " with space ", "UPPER", "slash/value", "中文"}
	for _, value := range badValues {
		if isValidSystemActionLogCode(value) {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}

func TestSanitizeJSONObject(t *testing.T) {
	input := JSONObject{
		"username": "alice",
		"password": "secret",
		"nested": map[string]interface{}{
			"token": "abc",
			"items": []interface{}{
				map[string]interface{}{"secret": "x"},
				"keep",
			},
		},
	}

	out := sanitizeJSONObject(input)
	if out["password"] != "[redacted]" {
		t.Fatalf("expected password to be redacted, got %#v", out["password"])
	}

	nested, ok := out["nested"].(JSONObject)
	if !ok {
		t.Fatalf("expected nested object to remain JSONObject, got %T", out["nested"])
	}
	if nested["token"] != "[redacted]" {
		t.Fatalf("expected nested token to be redacted, got %#v", nested["token"])
	}

	items, ok := nested["items"].([]interface{})
	if !ok {
		t.Fatalf("expected nested items to be []interface{}, got %T", nested["items"])
	}
	first, ok := items[0].(JSONObject)
	if !ok {
		t.Fatalf("expected first item to be JSONObject, got %T", items[0])
	}
	if first["secret"] != "[redacted]" {
		t.Fatalf("expected nested secret to be redacted, got %#v", first["secret"])
	}
}
