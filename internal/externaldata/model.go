package externaldata

import "time"

type Envelope struct {
	SourceSystem   string         `json:"source_system"`
	SourceRecordID string         `json:"source_record_id"`
	EventType      string         `json:"event_type"`
	OccurredAt     time.Time      `json:"occurred_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	IdempotencyKey string         `json:"idempotency_key"`
	Patient        map[string]any `json:"patient"`
	Data           map[string]any `json:"data"`
}
type DeliveryResponse struct {
	DeliveryID     string `json:"delivery_id"`
	Status         string `json:"status"`
	TenantID       int64  `json:"tenant_id"`
	ObjectType     string `json:"object_type"`
	SourceRecordID string `json:"source_record_id"`
	ResourceID     string `json:"resource_id,omitempty"`
	Error          string `json:"error,omitempty"`
}
