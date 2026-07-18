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
