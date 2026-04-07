package content

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// ContentTopic represents a content topic
type ContentTopic struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	Title       string     `json:"title"`
	Description *string    `json:"description,omitempty"`
	Category    *string    `json:"category,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Source      string     `json:"source"` // "ai_generated", "manual", "conversation_insight", "idea"
	Status      string     `json:"status"` // "draft", "selected", "in_progress", "completed", "archived"
	Priority    *int       `json:"priority,omitempty"`
	TargetDate  *time.Time `json:"target_date,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
	CreatedBy   int64      `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// ContentItem represents a content item
type ContentItem struct {
	ID            int64      `json:"id"`
	TenantID      int64      `json:"tenant_id"`
	TopicID       *int64     `json:"topic_id,omitempty"`
	Title         string     `json:"title"`
	Content       string     `json:"content"`
	Summary       *string    `json:"summary,omitempty"`
	Category      *string    `json:"category,omitempty"`
	Tags          []string   `json:"tags,omitempty"`
	Status        string     `json:"status"` // "draft", "pending_review", "approved", "rejected", "published", "unpublished"
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	UnpublishedAt *time.Time `json:"unpublished_at,omitempty"`
	ViewCount     int        `json:"view_count"`
	LikeCount     int        `json:"like_count"`
	ShareCount    int        `json:"share_count"`
	Images        []string   `json:"images,omitempty"`
	ExtraData     JSONObject `json:"extra_data,omitempty"`
	CreatedBy     int64      `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

// ContentPublishTask represents a content publish task
type ContentPublishTask struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	ContentID   int64      `json:"content_id"`
	Platform    string     `json:"platform"` // "wechat", "douyin", "xiaohongshu", etc.
	Status      string     `json:"status"` // "pending", "processing", "completed", "failed", "cancelled"
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	ErrorMsg    *string    `json:"error_msg,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
	CreatedBy   int64      `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// ContentSeed represents a content seed from recordings
type ContentSeed struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	RecordingID *int64     `json:"recording_id,omitempty"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	Category    *string    `json:"category,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Status      string     `json:"status"` // "pending", "adopted", "dismissed", "draft_generated"
	ClusterID   *int64     `json:"cluster_id,omitempty"`
	AdoptedBy   *int64     `json:"adopted_by,omitempty"`
	AdoptedAt   *time.Time `json:"adopted_at,omitempty"`
	DismissedBy *int64     `json:"dismissed_by,omitempty"`
	DismissedAt *time.Time `json:"dismissed_at,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// PromptTemplate represents a prompt template
type PromptTemplate struct {
	ID              int64      `json:"id"`
	TenantID        *int64     `json:"tenant_id,omitempty"` // NULL for global templates
	Code            string     `json:"code"`
	Name            string     `json:"name"`
	Description     *string    `json:"description,omitempty"`
	Category        *string    `json:"category,omitempty"`
	Template        string     `json:"template"`
	Variables       []string   `json:"variables,omitempty"`
	CurrentVersion  *int       `json:"current_version,omitempty"`
	PublishedVersion *int      `json:"published_version,omitempty"`
	IsActive        bool       `json:"is_active"`
	ExtraData       JSONObject `json:"extra_data,omitempty"`
	CreatedBy       int64      `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DeletedAt       *time.Time `json:"deleted_at,omitempty"`
}

// PromptTemplateVersion represents a prompt template version
type PromptTemplateVersion struct {
	ID          int64      `json:"id"`
	TemplateID  int64      `json:"template_id"`
	Version     int        `json:"version"`
	Template    string     `json:"template"`
	Variables   []string   `json:"variables,omitempty"`
	ChangeLog   *string    `json:"change_log,omitempty"`
	IsPublished bool       `json:"is_published"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	CreatedBy   int64      `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ContentPromptTemplate represents a content-specific prompt template
type ContentPromptTemplate struct {
	ID          int64      `json:"id"`
	TenantID    *int64     `json:"tenant_id,omitempty"`
	Type        string     `json:"type"` // "topic_generation", "content_generation", "geo_optimization", etc.
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Template    string     `json:"template"`
	Variables   []string   `json:"variables,omitempty"`
	IsActive    bool       `json:"is_active"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
	CreatedBy   int64      `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// Custom JSON type for database storage
type JSONObject map[string]interface{}

func (j *JSONObject) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, j)
}

func (j JSONObject) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}
