package content

import "testing"

func TestNewContentBillingMetadataSetsDedicatedIDs(t *testing.T) {
	contentID := int64(101)
	topicID := int64(202)
	req := GenerateContentRequest{
		ContentID: &contentID,
		TopicID:   &topicID,
	}

	billing := newContentBillingMetadata(req, "content_generation", "xiaohongshu_note")

	if billing.BusinessDomain != "content" || billing.BusinessObjectType != "content_item" || billing.BusinessObjectID != contentID {
		t.Fatalf("unexpected business object attribution: %#v", billing)
	}
	if billing.ContentID != contentID || billing.TopicID != topicID {
		t.Fatalf("dedicated content fields were not set: %#v", billing)
	}
	if billing.BillingSubject != "content_generation" || billing.BillingScene != "xiaohongshu_note" || billing.BillingRuleVersion != "v1" {
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
