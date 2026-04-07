package content

import (
	"context"
	"fmt"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

// Topic Services

// ListTopics retrieves a paginated list of topics
func (s *Service) ListTopics(ctx context.Context, req TopicListRequest) ([]*TopicResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	topics, total, err := s.store.ListTopics(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*TopicResponse, len(topics))
	for i, t := range topics {
		responses[i] = toTopicResponse(t)
	}

	return responses, total, nil
}

// GetTopicByID retrieves a topic by ID
func (s *Service) GetTopicByID(ctx context.Context, id int64) (*TopicResponse, error) {
	topic, err := s.store.GetTopicByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toTopicResponse(topic), nil
}

// CreateTopic creates a new topic
func (s *Service) CreateTopic(ctx context.Context, tenantID, createdBy int64, req CreateTopicRequest) (*TopicResponse, error) {
	// Validate request
	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if req.Source == "" {
		return nil, fmt.Errorf("source is required")
	}

	topic, err := s.store.CreateTopic(ctx, tenantID, createdBy, req)
	if err != nil {
		return nil, err
	}

	return toTopicResponse(topic), nil
}

// UpdateTopic updates a topic
func (s *Service) UpdateTopic(ctx context.Context, id int64, req UpdateTopicRequest) (*TopicResponse, error) {
	topic, err := s.store.UpdateTopic(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toTopicResponse(topic), nil
}

// DeleteTopic deletes a topic
func (s *Service) DeleteTopic(ctx context.Context, id int64) error {
	return s.store.DeleteTopic(ctx, id)
}

// GenerateTopics generates topics using AI
func (s *Service) GenerateTopics(ctx context.Context, tenantID, createdBy int64, req GenerateTopicsRequest) ([]*TopicResponse, error) {
	// TODO: Implement AI topic generation via llm-gateway
	// For now, return empty list
	return []*TopicResponse{}, nil
}

// Helper functions

// toTopicResponse converts a ContentTopic to TopicResponse
func toTopicResponse(t *ContentTopic) *TopicResponse {
	resp := &TopicResponse{
		ID:          t.ID,
		TenantID:    t.TenantID,
		Title:       t.Title,
		Description: t.Description,
		Category:    t.Category,
		Tags:        t.Tags,
		Source:      t.Source,
		Status:      t.Status,
		Priority:    t.Priority,
		ExtraData:   t.ExtraData,
		CreatedBy:   t.CreatedBy,
		CreatedAt:   t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if t.TargetDate != nil {
		formatted := t.TargetDate.Format("2006-01-02T15:04:05Z07:00")
		resp.TargetDate = &formatted
	}

	return resp
}

// TODO: Implement Content, PublishTask, Seed, and Template services
// These require integration with llm-gateway and OSS, which should be done in a separate phase
