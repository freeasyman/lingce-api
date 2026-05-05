package knowledge

import "time"

// ListRequest represents query parameters for listing knowledge items
type ListRequest struct {
	TenantID    int64  `json:"tenant_id"`
	Scope       string `json:"scope,omitempty"`
	Category    string `json:"category,omitempty"`
	Status      string `json:"status,omitempty"`
	ProductName string `json:"product_name,omitempty"`
	Keyword     string `json:"keyword,omitempty"`
	SourceType  string `json:"source_type,omitempty"`
	Page        int    `json:"page"`
	PageSize    int    `json:"page_size"`
}

// CreateRequest represents a request to create a knowledge item
type CreateRequest struct {
	TenantID    int64      `json:"tenant_id"`
	Scope       string     `json:"scope"`
	Category    string     `json:"category"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	Tags        []string   `json:"tags"`
	ProductName *string    `json:"product_name,omitempty"`
	SourceType  string     `json:"source_type"`
	SourceRef   *string    `json:"source_ref,omitempty"`
	Status      string     `json:"status,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// UpdateRequest represents a request to update a knowledge item
type UpdateRequest struct {
	Scope       *string    `json:"scope,omitempty"`
	Category    *string    `json:"category,omitempty"`
	Title       *string    `json:"title,omitempty"`
	Content     *string    `json:"content,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	ProductName *string    `json:"product_name,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// BatchStatusRequest represents a batch status change request
type BatchStatusRequest struct {
	IDs    []int64 `json:"ids"`
	Status string  `json:"status"`
}
