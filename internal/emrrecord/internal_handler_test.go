package emrrecord

import (
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/emrtemplate"
)

func TestParseAndValidateEMRResult(t *testing.T) {
	template := &emrtemplate.PublishedVersionDetail{
		Sections: []*emrtemplate.Section{
			{Code: "chief_complaint"},
			{Code: "diagnosis"},
		},
	}

	t.Run("accepts complete fenced JSON", func(t *testing.T) {
		content, unresolved, err := parseAndValidateEMRResult("```json\n{\n  \"working_content\": {\"chief_complaint\": \"咳嗽\", \"diagnosis\": \"待明确\"},\n  \"unresolved_items\": [{\"field_code\": \"diagnosis\", \"reason\": \"诊断需要医生核对\"}]\n}\n```", template)
		if err != nil {
			t.Fatalf("parse result: %v", err)
		}
		if content["chief_complaint"] != "咳嗽" {
			t.Fatalf("unexpected chief complaint: %#v", content["chief_complaint"])
		}
		if len(unresolved) != 1 {
			t.Fatalf("unexpected unresolved item count: %d", len(unresolved))
		}
	})

	t.Run("rejects missing template field", func(t *testing.T) {
		_, _, err := parseAndValidateEMRResult(`{"working_content":{"chief_complaint":"咳嗽"},"unresolved_items":[]}`, template)
		if err == nil || !strings.Contains(err.Error(), "diagnosis") {
			t.Fatalf("expected missing diagnosis error, got %v", err)
		}
	})

	t.Run("rejects unknown template field", func(t *testing.T) {
		_, _, err := parseAndValidateEMRResult(`{"working_content":{"chief_complaint":"咳嗽","diagnosis":"","unknown":"不允许"},"unresolved_items":[]}`, template)
		if err == nil || !strings.Contains(err.Error(), "模板之外") {
			t.Fatalf("expected unknown field error, got %v", err)
		}
	})

	t.Run("rejects string unresolved items", func(t *testing.T) {
		_, _, err := parseAndValidateEMRResult(`{"working_content":{"chief_complaint":"咳嗽","diagnosis":""},"unresolved_items":["诊断需要核对"]}`, template)
		if err == nil || !strings.Contains(err.Error(), "对象数组") {
			t.Fatalf("expected unresolved item shape error, got %v", err)
		}
	})

	t.Run("rejects unresolved item without reason", func(t *testing.T) {
		_, _, err := parseAndValidateEMRResult(`{"working_content":{"chief_complaint":"咳嗽","diagnosis":""},"unresolved_items":[{"field_code":"diagnosis"}]}`, template)
		if err == nil || !strings.Contains(err.Error(), "缺少 reason") {
			t.Fatalf("expected unresolved item reason error, got %v", err)
		}
	})
}
