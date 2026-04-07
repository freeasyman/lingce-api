package content

// Topic DTOs

// TopicListRequest represents the request for listing topics
type TopicListRequest struct {
	TenantID   *int64  `json:"tenant_id,omitempty"`
	Status     *string `json:"status,omitempty"`
	Category   *string `json:"category,omitempty"`
	Source     *string `json:"source,omitempty"`
	CreatedBy  *int64  `json:"created_by,omitempty"`
	StartDate  *string `json:"start_date,omitempty"`
	EndDate    *string `json:"end_date,omitempty"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
}

// TopicResponse represents topic response
type TopicResponse struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	Title       string     `json:"title"`
	Description *string    `json:"description,omitempty"`
	Category    *string    `json:"category,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Source      string     `json:"source"`
	Status      string     `json:"status"`
	Priority    *int       `json:"priority,omitempty"`
	TargetDate  *string    `json:"target_date,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
	CreatedBy   int64      `json:"created_by"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
}

// CreateTopicRequest represents create topic request
type CreateTopicRequest struct {
	Title       string     `json:"title"`
	Description *string    `json:"description,omitempty"`
	Category    *string    `json:"category,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Source      string     `json:"source"`
	Priority    *int       `json:"priority,omitempty"`
	TargetDate  *string    `json:"target_date,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
}

// UpdateTopicRequest represents update topic request
type UpdateTopicRequest struct {
	Title       *string    `json:"title,omitempty"`
	Description *string    `json:"description,omitempty"`
	Category    *string    `json:"category,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Status      *string    `json:"status,omitempty"`
	Priority    *int       `json:"priority,omitempty"`
	TargetDate  *string    `json:"target_date,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
}

// GenerateTopicsRequest represents AI topic generation request
type GenerateTopicsRequest struct {
	Context     string     `json:"context"`
	Count       int        `json:"count"`
	Category    *string    `json:"category,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
}

// Content DTOs

// ContentListRequest represents the request for listing contents
type ContentListRequest struct {
	TenantID   *int64  `json:"tenant_id,omitempty"`
	TopicID    *int64  `json:"topic_id,omitempty"`
	Status     *string `json:"status,omitempty"`
	Category   *string `json:"category,omitempty"`
	CreatedBy  *int64  `json:"created_by,omitempty"`
	StartDate  *string `json:"start_date,omitempty"`
	EndDate    *string `json:"end_date,omitempty"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
}

// ContentResponse represents content response
type ContentResponse struct {
	ID            int64      `json:"id"`
	TenantID      int64      `json:"tenant_id"`
	TopicID       *int64     `json:"topic_id,omitempty"`
	Title         string     `json:"title"`
	Content       string     `json:"content"`
	Summary       *string    `json:"summary,omitempty"`
	Category      *string    `json:"category,omitempty"`
	Tags          []string   `json:"tags,omitempty"`
	Status        string     `json:"status"`
	PublishedAt   *string    `json:"published_at,omitempty"`
	UnpublishedAt *string    `json:"unpublished_at,omitempty"`
	ViewCount     int        `json:"view_count"`
	LikeCount     int        `json:"like_count"`
	ShareCount    int        `json:"share_count"`
	Images        []string   `json:"images,omitempty"`
	ExtraData     JSONObject `json:"extra_data,omitempty"`
	CreatedBy     int64      `json:"created_by"`
	CreatedAt     string     `json:"created_at"`
	UpdatedAt     string     `json:"updated_at"`
}

// CreateContentRequest represents create content request
type CreateContentRequest struct {
	TopicID   *int64     `json:"topic_id,omitempty"`
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	Summary   *string    `json:"summary,omitempty"`
	Category  *string    `json:"category,omitempty"`
	Tags      []string   `json:"tags,omitempty"`
	Images    []string   `json:"images,omitempty"`
	ExtraData JSONObject `json:"extra_data,omitempty"`
}

// UpdateContentRequest represents update content request
type UpdateContentRequest struct {
	Title     *string    `json:"title,omitempty"`
	Content   *string    `json:"content,omitempty"`
	Summary   *string    `json:"summary,omitempty"`
	Category  *string    `json:"category,omitempty"`
	Tags      []string   `json:"tags,omitempty"`
	Status    *string    `json:"status,omitempty"`
	Images    []string   `json:"images,omitempty"`
	ExtraData JSONObject `json:"extra_data,omitempty"`
}

// GenerateContentRequest represents AI content generation request
type GenerateContentRequest struct {
	TopicID   *int64     `json:"topic_id,omitempty"`
	Title     string     `json:"title"`
	Context   *string    `json:"context,omitempty"`
	Style     *string    `json:"style,omitempty"`
	Length    *int       `json:"length,omitempty"`
	ExtraData JSONObject `json:"extra_data,omitempty"`
}

// Publish Task DTOs

// PublishTaskListRequest represents the request for listing publish tasks
type PublishTaskListRequest struct {
	TenantID   *int64  `json:"tenant_id,omitempty"`
	ContentID  *int64  `json:"content_id,omitempty"`
	Platform   *string `json:"platform,omitempty"`
	Status     *string `json:"status,omitempty"`
	CreatedBy  *int64  `json:"created_by,omitempty"`
	StartDate  *string `json:"start_date,omitempty"`
	EndDate    *string `json:"end_date,omitempty"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
}

// PublishTaskResponse represents publish task response
type PublishTaskResponse struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	ContentID   int64      `json:"content_id"`
	ContentTitle string    `json:"content_title"`
	Platform    string     `json:"platform"`
	Status      string     `json:"status"`
	ScheduledAt *string    `json:"scheduled_at,omitempty"`
	PublishedAt *string    `json:"published_at,omitempty"`
	ErrorMsg    *string    `json:"error_msg,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
	CreatedBy   int64      `json:"created_by"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
}

// CreatePublishTaskRequest represents create publish task request
type CreatePublishTaskRequest struct {
	ContentID   int64      `json:"content_id"`
	Platform    string     `json:"platform"`
	ScheduledAt *string    `json:"scheduled_at,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
}

// UpdateTaskStatusRequest represents update task status request
type UpdateTaskStatusRequest struct {
	Status string `json:"status"`
}

// Content Seed DTOs

// SeedListRequest represents the request for listing seeds
type SeedListRequest struct {
	TenantID    *int64  `json:"tenant_id,omitempty"`
	Status      *string `json:"status,omitempty"`
	Category    *string `json:"category,omitempty"`
	ClusterID   *int64  `json:"cluster_id,omitempty"`
	RecordingID *int64  `json:"recording_id,omitempty"`
	Page        int     `json:"page"`
	PageSize    int     `json:"page_size"`
}

// SeedResponse represents seed response
type SeedResponse struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	RecordingID *int64     `json:"recording_id,omitempty"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	Category    *string    `json:"category,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Status      string     `json:"status"`
	ClusterID   *int64     `json:"cluster_id,omitempty"`
	AdoptedBy   *int64     `json:"adopted_by,omitempty"`
	AdoptedAt   *string    `json:"adopted_at,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
}

// SeedStatsResponse represents seed statistics
type SeedStatsResponse struct {
	TotalSeeds     int64 `json:"total_seeds"`
	PendingSeeds   int64 `json:"pending_seeds"`
	AdoptedSeeds   int64 `json:"adopted_seeds"`
	DismissedSeeds int64 `json:"dismissed_seeds"`
	DraftGenerated int64 `json:"draft_generated"`
}

// Prompt Template DTOs

// TemplateListRequest represents the request for listing templates
type TemplateListRequest struct {
	TenantID *int64  `json:"tenant_id,omitempty"`
	Category *string `json:"category,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

// TemplateResponse represents template response
type TemplateResponse struct {
	ID               int64      `json:"id"`
	TenantID         *int64     `json:"tenant_id,omitempty"`
	Code             string     `json:"code"`
	Name             string     `json:"name"`
	Description      *string    `json:"description,omitempty"`
	Category         *string    `json:"category,omitempty"`
	Template         string     `json:"template"`
	Variables        []string   `json:"variables,omitempty"`
	CurrentVersion   *int       `json:"current_version,omitempty"`
	PublishedVersion *int       `json:"published_version,omitempty"`
	IsActive         bool       `json:"is_active"`
	ExtraData        JSONObject `json:"extra_data,omitempty"`
	CreatedBy        int64      `json:"created_by"`
	CreatedAt        string     `json:"created_at"`
	UpdatedAt        string     `json:"updated_at"`
}

// CreateTemplateRequest represents create template request
type CreateTemplateRequest struct {
	Code        string     `json:"code"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Category    *string    `json:"category,omitempty"`
	Template    string     `json:"template"`
	Variables   []string   `json:"variables,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
}

// UpdateTemplateRequest represents update template request
type UpdateTemplateRequest struct {
	Name        *string    `json:"name,omitempty"`
	Description *string    `json:"description,omitempty"`
	Category    *string    `json:"category,omitempty"`
	Template    *string    `json:"template,omitempty"`
	Variables   []string   `json:"variables,omitempty"`
	IsActive    *bool      `json:"is_active,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
}

// CreateVersionRequest represents create version request
type CreateVersionRequest struct {
	Template  string   `json:"template"`
	Variables []string `json:"variables,omitempty"`
	ChangeLog *string  `json:"change_log,omitempty"`
}

// VersionResponse represents version response
type VersionResponse struct {
	ID          int64    `json:"id"`
	TemplateID  int64    `json:"template_id"`
	Version     int      `json:"version"`
	Template    string   `json:"template"`
	Variables   []string `json:"variables,omitempty"`
	ChangeLog   *string  `json:"change_log,omitempty"`
	IsPublished bool     `json:"is_published"`
	PublishedAt *string  `json:"published_at,omitempty"`
	CreatedBy   int64    `json:"created_by"`
	CreatedAt   string   `json:"created_at"`
}
