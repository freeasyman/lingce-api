package content

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/freeasyman/lingce-api/pkg/oss"
)

type Service struct {
	store      *Store
	llmClient  *llmgateway.Client
	ossClient  *oss.Client
	tenantID   int64
}

func NewService(store *Store, llmClient *llmgateway.Client, ossClient *oss.Client) *Service {
	return &Service{
		store:     store,
		llmClient: llmClient,
		ossClient: ossClient,
	}
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
	// Validate request
	if req.Context == "" {
		return nil, fmt.Errorf("context is required")
	}
	if req.Count <= 0 || req.Count > 10 {
		req.Count = 5
	}

	// Prepare prompt for LLM
	systemPrompt := "你是一个专业的内容策划专家，擅长根据用户需求生成有价值的内容选题。"
	userPrompt := fmt.Sprintf(`请根据以下背景信息，生成%d个内容选题：

背景：%s

要求：
1. 每个选题要有明确的标题和描述
2. 选题要有实用价值和吸引力
3. 返回JSON格式，包含title和description字段

返回格式示例：
[
  {"title": "选题标题1", "description": "选题描述1"},
  {"title": "选题标题2", "description": "选题描述2"}
]`, req.Count, req.Context)

	// Call LLM gateway
	llmReq := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  "content",
		FunctionType:  "topic_generation",
		Messages: []llmgateway.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{
			Temperature:    0.7,
			MaxTokens:      2000,
			ResponseFormat: "json",
		},
	}

	llmResp, err := s.llmClient.TextInference(ctx, llmReq)
	if err != nil {
		return nil, fmt.Errorf("failed to generate topics via LLM: %w", err)
	}

	// Parse LLM response
	var generatedTopics []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(llmResp.Content), &generatedTopics); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	// Create topics in database
	var responses []*TopicResponse
	for _, gt := range generatedTopics {
		createReq := CreateTopicRequest{
			Title:       gt.Title,
			Description: &gt.Description,
			Category:    req.Category,
			Source:      "ai_generated",
		}

		topic, err := s.store.CreateTopic(ctx, tenantID, createdBy, createReq)
		if err != nil {
			// Log error but continue with other topics
			continue
		}

		responses = append(responses, toTopicResponse(topic))
	}

	return responses, nil
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
