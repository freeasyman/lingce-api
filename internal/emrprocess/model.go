package emrprocess

import "time"

type ProcessRecord struct {
	ID               string         `json:"id"`
	TenantID         int64          `json:"tenant_id"`
	RecordID         *string        `json:"record_id,omitempty"`
	ActionType       string         `json:"action_type"`
	ActionResult     string         `json:"action_result"`
	OccurredAt       time.Time      `json:"occurred_at"`
	ActorType        string         `json:"actor_type"`
	ActorID          *int64         `json:"actor_id,omitempty"`
	Source           string         `json:"source"`
	BeforeStatus     *string        `json:"before_status,omitempty"`
	AfterStatus      *string        `json:"after_status,omitempty"`
	ActionSnapshotID *string        `json:"action_snapshot_id,omitempty"`
	BeforeSnapshotID *string        `json:"before_snapshot_id,omitempty"`
	AfterSnapshotID  *string        `json:"after_snapshot_id,omitempty"`
	CheckRunID       *string        `json:"check_run_id,omitempty"`
	OutputInfo       map[string]any `json:"output_info,omitempty"`
	FailureReason    string         `json:"failure_reason,omitempty"`
	ActionNote       string         `json:"action_note,omitempty"`
	ContentChanges   []any          `json:"content_changes,omitempty"`
}

type AppendRequest struct {
	TenantID         int64
	RecordID         *string
	ActionType       string
	ActionResult     string
	ActorType        string
	ActorID          *int64
	Source           string
	BeforeStatus     *string
	AfterStatus      *string
	ActionSnapshotID *string
	BeforeSnapshotID *string
	AfterSnapshotID  *string
	CheckRunID       *string
	OutputInfo       map[string]any
	FailureReason    string
	ActionNote       string
	ContentChanges   []any
}
