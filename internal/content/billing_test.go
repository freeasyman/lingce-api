package content

import "testing"

func TestNewBillingMetadataSetsBusinessObject(t *testing.T) {
	billing := newBillingMetadata("content", "recording", 101, "insight_extraction", "content_insight", 202)

	if billing.BusinessDomain != "content" || billing.BusinessObjectType != "recording" || billing.BusinessObjectID != 101 {
		t.Fatalf("unexpected business object attribution: %#v", billing)
	}
	if billing.RecordingID != 202 {
		t.Fatalf("recording id was not set: %#v", billing)
	}
	if billing.BillingSubject != "insight_extraction" || billing.BillingScene != "content_insight" || billing.BillingRuleVersion != "v1" || billing.CostLayer != "product" {
		t.Fatalf("unexpected billing fields: %#v", billing)
	}
}

func TestContentCallerModuleMapsSubjects(t *testing.T) {
	cases := map[string]string{
		"topic_generation":    "content.topic_generation",
		"content_generation":  "content.content_generation",
		"graphic_note_repair": "content.graphic_note_repair",
		"unexpected_subject":  "content",
	}

	for subject, want := range cases {
		if got := contentCallerModule(subject); got != want {
			t.Fatalf("contentCallerModule(%q) = %q, want %q", subject, got, want)
		}
	}
}
