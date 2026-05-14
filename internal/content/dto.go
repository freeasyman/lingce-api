package content

// Topic DTOs

// TopicListRequest represents the request for listing topics
type TopicListRequest struct {
	TenantID  *int64  `json:"tenant_id,omitempty"`
	TenantIDs []int64 `json:"tenant_ids,omitempty"`
	Status    *string `json:"status,omitempty"`
	Category  *string `json:"category,omitempty"`
	Source    *string `json:"source,omitempty"`
	CreatedBy *int64  `json:"created_by,omitempty"`
	StartDate *string `json:"start_date,omitempty"`
	EndDate   *string `json:"end_date,omitempty"`
	Page      int     `json:"page"`
	PageSize  int     `json:"page_size"`
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
	Context   string     `json:"context"`
	Count     int        `json:"count"`
	Category  *string    `json:"category,omitempty"`
	ExtraData JSONObject `json:"extra_data,omitempty"`
}

// HotTopicsResponse represents hot topics response
type HotTopicsResponse struct {
	Topics    []HotTopic `json:"topics"`
	UpdatedAt string     `json:"updated_at"`
}

// HotTopic represents a hot topic
type HotTopic struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`
	Trend       string   `json:"trend"` // rising, stable, declining
	Score       float64  `json:"score"`
}

// IdeaTopicStartRequest represents "我有想法" initialization request
type IdeaTopicStartRequest struct {
	Description string     `json:"description"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
}

// IdeaTopicStartResponse represents "我有想法" initialization response
type IdeaTopicStartResponse struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// ParseFilesRequest represents file parsing request
type ParseFilesRequest struct {
	SessionID string   `json:"session_id"`
	FileURLs  []string `json:"file_urls"`
}

// ParseFilesResponse represents file parsing response
type ParseFilesResponse struct {
	SessionID     string   `json:"session_id"`
	ParsedContent string   `json:"parsed_content"`
	Keywords      []string `json:"keywords"`
}

// IdeaGenerateTopicsRequest represents idea topic generation request
type IdeaGenerateTopicsRequest struct {
	SessionID string `json:"session_id"`
	Count     int    `json:"count"`
}

// SaveIdeaTopicsRequest represents save idea topics request
type SaveIdeaTopicsRequest struct {
	SessionID string  `json:"session_id"`
	TopicIDs  []int64 `json:"topic_ids"`
}

// Content DTOs

// ContentListRequest represents the request for listing contents
type ContentListRequest struct {
	TenantID  *int64  `json:"tenant_id,omitempty"`
	TenantIDs []int64 `json:"tenant_ids,omitempty"`
	TopicID   *int64  `json:"topic_id,omitempty"`
	Status    *string `json:"status,omitempty"`
	Category  *string `json:"category,omitempty"`
	CreatedBy *int64  `json:"created_by,omitempty"`
	StartDate *string `json:"start_date,omitempty"`
	EndDate   *string `json:"end_date,omitempty"`
	Page      int     `json:"page"`
	PageSize  int     `json:"page_size"`
}

// ContentResponse represents content response
type ContentResponse struct {
	ID            int64      `json:"id"`
	TenantID      int64      `json:"tenant_id"`
	TopicID       *int64     `json:"topic_id,omitempty"`
	ContentType   *string    `json:"content_type,omitempty"`
	Platform      *string    `json:"platform,omitempty"`
	CreatorName   *string    `json:"creator_name,omitempty"`
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
	TopicID         *int64     `json:"topic_id,omitempty"`
	ContentType     *string    `json:"content_type,omitempty"`
	Platform        *string    `json:"platform,omitempty"`
	Title           string     `json:"title"`
	Content         string     `json:"content"`
	Summary         *string    `json:"summary,omitempty"`
	Subtitle        *string    `json:"subtitle,omitempty"`
	ScriptStructure JSONObject `json:"script_structure,omitempty"`
	NoteStructure   JSONObject `json:"note_structure,omitempty"`
	Category        *string    `json:"category,omitempty"`
	Tags            []string   `json:"tags,omitempty"`
	Images          []string   `json:"images,omitempty"`
	ExtraData       JSONObject `json:"extra_data,omitempty"`
}

// UpdateContentRequest represents update content request
type UpdateContentRequest struct {
	Title           *string    `json:"title,omitempty"`
	Content         *string    `json:"content,omitempty"`
	Summary         *string    `json:"summary,omitempty"`
	Subtitle        *string    `json:"subtitle,omitempty"`
	ContentType     *string    `json:"content_type,omitempty"`
	Platform        *string    `json:"platform,omitempty"`
	ScriptStructure JSONObject `json:"script_structure,omitempty"`
	NoteStructure   JSONObject `json:"note_structure,omitempty"`
	Category        *string    `json:"category,omitempty"`
	Tags            []string   `json:"tags,omitempty"`
	Status          *string    `json:"status,omitempty"`
	Images          []string   `json:"images,omitempty"`
	ExtraData       JSONObject `json:"extra_data,omitempty"`
}

// GenerateContentRequest represents AI content generation request
type GenerateContentRequest struct {
	ContentID              *int64     `json:"content_id,omitempty"`
	TopicID                *int64     `json:"topic_id,omitempty"`
	SeedID                 *int64     `json:"seed_id,omitempty"`
	Title                  string     `json:"title"`
	ContentType            *string    `json:"content_type,omitempty"`
	Platform               *string    `json:"platform,omitempty"`
	Context                *string    `json:"context,omitempty"`
	Style                  *string    `json:"style,omitempty"`
	Length                 *int       `json:"length,omitempty"`
	WordCount              *int       `json:"word_count,omitempty"`
	Duration               *int       `json:"duration,omitempty"`
	Subtitle               *string    `json:"subtitle,omitempty"`
	ScriptType             *string    `json:"script_type,omitempty"`
	SlideCount             *int       `json:"slide_count,omitempty"`
	NoteStyle              *string    `json:"note_style,omitempty"`
	ContentGoal            *string    `json:"content_goal,omitempty"`
	IncludeExpert          *bool      `json:"include_expert,omitempty"`
	IncludeKnowledge       *bool      `json:"include_knowledge,omitempty"`
	AdditionalRequirements *string    `json:"additional_requirements,omitempty"`
	StrategyText           *string    `json:"strategy_text,omitempty"`
	SelectedTitle          *string    `json:"selected_headline,omitempty"`
	PromptTemplateID       *int64     `json:"prompt_template_id,omitempty"`
	ExtraData              JSONObject `json:"extra_data,omitempty"`
}

// Publish Task DTOs

// PublishTaskListRequest represents the request for listing publish tasks
type PublishTaskListRequest struct {
	TenantID  *int64  `json:"tenant_id,omitempty"`
	ContentID *int64  `json:"content_id,omitempty"`
	Platform  *string `json:"platform,omitempty"`
	Status    *string `json:"status,omitempty"`
	CreatedBy *int64  `json:"created_by,omitempty"`
	StartDate *string `json:"start_date,omitempty"`
	EndDate   *string `json:"end_date,omitempty"`
	Page      int     `json:"page"`
	PageSize  int     `json:"page_size"`
}

// PublishTaskResponse represents publish task response
type PublishTaskResponse struct {
	ID           int64      `json:"id"`
	TenantID     int64      `json:"tenant_id"`
	ContentID    int64      `json:"content_id"`
	ContentTitle string     `json:"content_title"`
	Platform     string     `json:"platform"`
	Status       string     `json:"status"`
	ScheduledAt  *string    `json:"scheduled_at,omitempty"`
	PublishedAt  *string    `json:"published_at,omitempty"`
	ErrorMsg     *string    `json:"error_msg,omitempty"`
	ExtraData    JSONObject `json:"extra_data,omitempty"`
	CreatedBy    int64      `json:"created_by"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
}

// CreatePublishTaskRequest represents create publish task request
type CreatePublishTaskRequest struct {
	ContentID   int64      `json:"content_id"`
	Platform    string     `json:"platform"`
	ScheduledAt *string    `json:"scheduled_at,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
}

// BatchCreatePublishTasksRequest represents batch create publish tasks request
type BatchCreatePublishTasksRequest struct {
	Tasks []CreatePublishTaskRequest `json:"tasks"`
}

// UpdateTaskStatusRequest represents update task status request
type UpdateTaskStatusRequest struct {
	Status string `json:"status"`
}

// PublishDashboardResponse represents publish dashboard statistics
type PublishDashboardResponse struct {
	TotalTasks     int64                  `json:"total_tasks"`
	PendingTasks   int64                  `json:"pending_tasks"`
	RunningTasks   int64                  `json:"running_tasks"`
	CompletedTasks int64                  `json:"completed_tasks"`
	FailedTasks    int64                  `json:"failed_tasks"`
	RecentTasks    []*PublishTaskResponse `json:"recent_tasks"`
}

// Content Seed DTOs

// SeedListRequest represents the request for listing seeds
type SeedListRequest struct {
	TenantID    *int64  `json:"tenant_id,omitempty"`
	TenantIDs   []int64 `json:"tenant_ids,omitempty"`
	Status      *string `json:"status,omitempty"`
	Category    *string `json:"category,omitempty"`
	ClusterID   *int64  `json:"cluster_id,omitempty"`
	RecordingID *int64  `json:"recording_id,omitempty"`
	Page        int     `json:"page"`
	PageSize    int     `json:"page_size"`
}

// SeedResponse represents seed response
type SeedResponse struct {
	ID                 int64      `json:"id"`
	TenantID           int64      `json:"tenant_id"`
	EmployeeID         *int64     `json:"employee_id,omitempty"`
	EmployeeName       *string    `json:"employee_name,omitempty"`
	RecordingID        *int64     `json:"recording_id,omitempty"`
	SeedType           *string    `json:"seed_type,omitempty"`
	Topic              string     `json:"topic,omitempty"`
	ContentAngle       *string    `json:"content_angle,omitempty"`
	SuggestedPlatforms []string   `json:"suggested_platforms,omitempty"`
	ViralPotential     *string    `json:"viral_potential,omitempty"`
	ConcernClusterID   *int64     `json:"concern_cluster_id,omitempty"`
	SeedData           JSONObject `json:"seed_data,omitempty"`
	Title              string     `json:"title"`
	Content            string     `json:"content"`
	Category           *string    `json:"category,omitempty"`
	Tags               []string   `json:"tags,omitempty"`
	Status             string     `json:"status"`
	ClusterID          *int64     `json:"cluster_id,omitempty"`
	AdoptedBy          *int64     `json:"adopted_by,omitempty"`
	AdoptedAt          *string    `json:"adopted_at,omitempty"`
	ExtraData          JSONObject `json:"extra_data,omitempty"`
	CreatedAt          string     `json:"created_at"`
	UpdatedAt          string     `json:"updated_at"`
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
	TenantID     *int64  `json:"tenant_id,omitempty"`
	TenantIDs    []int64 `json:"tenant_ids,omitempty"`
	Search       *string `json:"search,omitempty"`
	Category     *string `json:"category,omitempty"`
	FunctionType *string `json:"function_type,omitempty"`
	IsActive     *bool   `json:"is_active,omitempty"`
	Page         int     `json:"page"`
	PageSize     int     `json:"page_size"`
}

// TemplateResponse represents template response
type TemplateResponse struct {
	ID               int64      `json:"id"`
	TenantID         *int64     `json:"tenant_id,omitempty"`
	Code             string     `json:"code"`
	Name             string     `json:"name"`
	Description      *string    `json:"description,omitempty"`
	Category         *string    `json:"category,omitempty"`
	FunctionType     *string    `json:"function_type,omitempty"`
	Template         string     `json:"template"`
	PromptTemplate   string     `json:"prompt_template"`
	Variables        []string   `json:"variables,omitempty"`
	CurrentVersion   *int       `json:"current_version,omitempty"`
	PublishedVersion *int       `json:"published_version,omitempty"`
	IsActive         bool       `json:"is_active"`
	IsSystem         bool       `json:"is_system"`
	UsageCount       int        `json:"usage_count"`
	Version          *string    `json:"version,omitempty"`
	BusinessType     *string    `json:"business_type,omitempty"`
	SourceTable      *string    `json:"source_table,omitempty"`
	ExtraData        JSONObject `json:"extra_data,omitempty"`
	CreatedBy        int64      `json:"created_by"`
	CreatedAt        string     `json:"created_at"`
	UpdatedAt        string     `json:"updated_at"`
}

// CreateTemplateRequest represents create template request
type CreateTemplateRequest struct {
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	Description    *string    `json:"description,omitempty"`
	Category       *string    `json:"category,omitempty"`
	FunctionType   *string    `json:"function_type,omitempty"`
	Version        *string    `json:"version,omitempty"`
	Template       string     `json:"template"`
	PromptTemplate *string    `json:"prompt_template,omitempty"`
	Variables      []string   `json:"variables,omitempty"`
	ExtraData      JSONObject `json:"extra_data,omitempty"`
}

// UpdateTemplateRequest represents update template request
type UpdateTemplateRequest struct {
	Name           *string    `json:"name,omitempty"`
	Description    *string    `json:"description,omitempty"`
	Category       *string    `json:"category,omitempty"`
	FunctionType   *string    `json:"function_type,omitempty"`
	Version        *string    `json:"version,omitempty"`
	Template       *string    `json:"template,omitempty"`
	PromptTemplate *string    `json:"prompt_template,omitempty"`
	Variables      []string   `json:"variables,omitempty"`
	IsActive       *bool      `json:"is_active,omitempty"`
	ExtraData      JSONObject `json:"extra_data,omitempty"`
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

// GenerateImagesRequest represents batch image generation request
type GenerateImagesRequest struct {
	Prompts   []string   `json:"prompts"`
	Style     *string    `json:"style,omitempty"`
	ExtraData JSONObject `json:"extra_data,omitempty"`
}

// GenerateSingleImageRequest represents single image generation request
type GenerateSingleImageRequest struct {
	Prompt    string     `json:"prompt"`
	Style     *string    `json:"style,omitempty"`
	ExtraData JSONObject `json:"extra_data,omitempty"`
}

// ImageGenerationResponse represents image generation response
type ImageGenerationResponse struct {
	Images []GeneratedImage `json:"images"`
}

// GeneratedImage represents a generated image
type GeneratedImage struct {
	URL    string `json:"url"`
	Prompt string `json:"prompt"`
}

// SaveComposedImagesRequest represents save composed images request
type SaveComposedImagesRequest struct {
	Images []string `json:"images"` // Image URLs
}

// ConversationInsightsStatsResponse represents conversation insights statistics
type ConversationInsightsStatsResponse struct {
	TotalConversations int64   `json:"total_conversations"`
	TotalQuestions     int64   `json:"total_questions"`
	UniqueTopics       int64   `json:"unique_topics"`
	AvgQuestionsPerDay float64 `json:"avg_questions_per_day"`
}

// FrequentQuestion represents a frequent question
type FrequentQuestion struct {
	Question  string `json:"question"`
	Count     int64  `json:"count"`
	Category  string `json:"category"`
	Sentiment string `json:"sentiment"` // positive, neutral, negative
}

// FrequentQuestionsResponse represents frequent questions response
type FrequentQuestionsResponse struct {
	Questions []FrequentQuestion `json:"questions"`
	Total     int64              `json:"total"`
}

// MineTopicsRequest represents topic mining request
type MineTopicsRequest struct {
	StartDate *string `json:"start_date,omitempty"`
	EndDate   *string `json:"end_date,omitempty"`
	MinCount  *int    `json:"min_count,omitempty"`
	Category  *string `json:"category,omitempty"`
}

// MinedTopic represents a mined topic
type MinedTopic struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`
	Count       int64    `json:"count"`
	Relevance   float64  `json:"relevance"`
}

// MineTopicsResponse represents topic mining response
type MineTopicsResponse struct {
	Topics []MinedTopic `json:"topics"`
	Total  int64        `json:"total"`
}

// SaveMinedTopicsRequest represents save mined topics request
type SaveMinedTopicsRequest struct {
	Topics []struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Keywords    []string `json:"keywords"`
		Category    *string  `json:"category,omitempty"`
	} `json:"topics"`
}
