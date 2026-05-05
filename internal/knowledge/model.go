package knowledge

import "time"

// KnowledgeItem represents a single knowledge entry
type KnowledgeItem struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	Scope       string     `json:"scope"`
	Category    string     `json:"category"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	Tags        []string   `json:"tags"`
	ProductName *string    `json:"product_name,omitempty"`
	SourceType  string     `json:"source_type"`
	SourceRef   *string    `json:"source_ref,omitempty"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Valid scopes
const (
	ScopeFrontdesk  = "frontdesk"
	ScopeDoctor     = "doctor"
	ScopeConsultant = "consultant"
	ScopeCommon     = "common"
)

// Valid categories
const (
	CategoryService    = "service"
	CategoryProduct    = "product"
	CategoryScript     = "script"
	CategoryCompliance = "compliance"
	CategoryTemporal   = "temporal"
	CategoryFAQ        = "faq"
)

// Valid statuses
const (
	StatusDraft    = "draft"
	StatusActive   = "active"
	StatusArchived = "archived"
)

// Valid source types
const (
	SourceManual    = "manual"
	SourceRecording = "recording"
	SourceDocument  = "document"
)
