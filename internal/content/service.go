package content

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/freeasyman/lingce-api/pkg/oss"
)

type Service struct {
	store            *Store
	llmClient        *llmgateway.Client
	ossClient        *oss.Client
	tenantID         int64
	ideaMu           sync.RWMutex
	ideaTopics       map[string]*ideaTopicSession
	templateMu       sync.RWMutex
	promptTemplates  map[int64]*runtimeTemplateEntry
	contentTemplates map[int64]*runtimeTemplateEntry
	nextTemplateID   int64
	nextVersionID    int64
}

func NewService(store *Store, llmClient *llmgateway.Client, ossClient *oss.Client) *Service {
	return &Service{
		store:            store,
		llmClient:        llmClient,
		ossClient:        ossClient,
		ideaTopics:       make(map[string]*ideaTopicSession),
		promptTemplates:  make(map[int64]*runtimeTemplateEntry),
		contentTemplates: make(map[int64]*runtimeTemplateEntry),
		nextTemplateID:   1,
		nextVersionID:    1,
	}
}

type ideaTopicSession struct {
	SessionID     string
	UserID        int64
	TenantID      int64
	Description   string
	ParsedContent string
	Keywords      []string
	CreatedAt     time.Time
	UpdatedAt     time.Time
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

	if s.llmClient == nil {
		return s.generateTopicsFallback(ctx, tenantID, createdBy, req)
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

// GetHotTopics retrieves scored hot topics from recent topic records.
func (s *Service) GetHotTopics(ctx context.Context, tenantID *int64) (*HotTopicsResponse, error) {
	topics, _, err := s.ListTopics(ctx, TopicListRequest{
		TenantID: tenantID,
		Page:     1,
		PageSize: 100,
	})
	if err != nil {
		return nil, err
	}

	type scoredTopic struct {
		topic *TopicResponse
		score float64
	}
	scored := make([]scoredTopic, 0, len(topics))
	for _, t := range topics {
		scored = append(scored, scoredTopic{
			topic: t,
			score: scoreTopic(t),
		})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	if len(scored) > 10 {
		scored = scored[:10]
	}

	hotTopics := make([]HotTopic, 0, len(scored))
	for _, item := range scored {
		desc := ""
		if item.topic.Description != nil {
			desc = *item.topic.Description
		}
		keywords := item.topic.Tags
		if len(keywords) == 0 {
			keywords = extractKeywordsFromText(item.topic.Title+" "+desc, 5)
		}
		hotTopics = append(hotTopics, HotTopic{
			Title:       item.topic.Title,
			Description: desc,
			Keywords:    keywords,
			Trend:       calcTrend(item.topic),
			Score:       item.score,
		})
	}

	return &HotTopicsResponse{
		Topics:    hotTopics,
		UpdatedAt: time.Now().Format(time.RFC3339),
	}, nil
}

// RefreshHotTopics triggers a topic generation pass from current hot topics context.
func (s *Service) RefreshHotTopics(ctx context.Context, tenantID, createdBy int64) (int, error) {
	hot, err := s.GetHotTopics(ctx, &tenantID)
	if err != nil {
		return 0, err
	}

	contextParts := []string{"现有热点话题："}
	for _, t := range hot.Topics {
		contextParts = append(contextParts, fmt.Sprintf("- %s：%s", t.Title, t.Description))
	}

	generated, err := s.GenerateTopics(ctx, tenantID, createdBy, GenerateTopicsRequest{
		Context: strings.Join(contextParts, "\n"),
		Count:   5,
	})
	if err != nil {
		return 0, err
	}

	return len(generated), nil
}

// StartIdeaTopicSession initializes an idea-topic session.
func (s *Service) StartIdeaTopicSession(ctx context.Context, userID, tenantID int64, req IdeaTopicStartRequest) (*IdeaTopicStartResponse, error) {
	_ = ctx
	sessionID := fmt.Sprintf("idea_%d_%d", userID, time.Now().UnixNano())

	s.ideaMu.Lock()
	s.ideaTopics[sessionID] = &ideaTopicSession{
		SessionID:   sessionID,
		UserID:      userID,
		TenantID:    tenantID,
		Description: strings.TrimSpace(req.Description),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	s.ideaMu.Unlock()

	return &IdeaTopicStartResponse{
		SessionID: sessionID,
		Message:   "Session initialized successfully",
	}, nil
}

// ParseIdeaFiles stores parsed content and keywords to the session.
func (s *Service) ParseIdeaFiles(ctx context.Context, userID int64, req ParseFilesRequest) (*ParseFilesResponse, error) {
	_ = ctx
	s.ideaMu.Lock()
	defer s.ideaMu.Unlock()

	session, ok := s.ideaTopics[req.SessionID]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	if session.UserID != userID {
		return nil, fmt.Errorf("session access denied")
	}

	parsedContent := strings.TrimSpace(session.Description)
	if len(req.FileURLs) > 0 {
		parsedContent = strings.TrimSpace(parsedContent + "\n" + strings.Join(req.FileURLs, "\n"))
	}
	if parsedContent == "" {
		parsedContent = "未提供可解析的内容，使用默认主题上下文"
	}

	keywords := extractKeywordsFromText(parsedContent, 12)
	session.ParsedContent = parsedContent
	session.Keywords = keywords
	session.UpdatedAt = time.Now()

	return &ParseFilesResponse{
		SessionID:     session.SessionID,
		ParsedContent: parsedContent,
		Keywords:      keywords,
	}, nil
}

// GenerateIdeaTopics generates topics from idea session and stores them.
func (s *Service) GenerateIdeaTopics(ctx context.Context, userID int64, req IdeaGenerateTopicsRequest) ([]*TopicResponse, error) {
	s.ideaMu.RLock()
	session, ok := s.ideaTopics[req.SessionID]
	s.ideaMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	if session.UserID != userID {
		return nil, fmt.Errorf("session access denied")
	}

	count := req.Count
	if count <= 0 || count > 10 {
		count = 5
	}

	contextParts := []string{}
	if session.Description != "" {
		contextParts = append(contextParts, "初始想法："+session.Description)
	}
	if session.ParsedContent != "" {
		contextParts = append(contextParts, "解析内容："+session.ParsedContent)
	}
	if len(session.Keywords) > 0 {
		contextParts = append(contextParts, "关键词："+strings.Join(session.Keywords, "、"))
	}

	generated, err := s.GenerateTopics(ctx, session.TenantID, userID, GenerateTopicsRequest{
		Context: strings.Join(contextParts, "\n"),
		Count:   count,
	})
	if err != nil {
		return nil, err
	}

	return generated, nil
}

// SaveIdeaTopics marks selected topic IDs as selected.
func (s *Service) SaveIdeaTopics(ctx context.Context, userID int64, req SaveIdeaTopicsRequest) (int, error) {
	s.ideaMu.RLock()
	session, ok := s.ideaTopics[req.SessionID]
	s.ideaMu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("session not found")
	}
	if session.UserID != userID {
		return 0, fmt.Errorf("session access denied")
	}

	selectedStatus := "selected"
	updatedCount := 0
	for _, id := range req.TopicIDs {
		topic, err := s.GetTopicByID(ctx, id)
		if err != nil {
			continue
		}
		if session.TenantID != 0 && topic.TenantID != session.TenantID {
			continue
		}
		if _, err := s.UpdateTopic(ctx, id, UpdateTopicRequest{Status: &selectedStatus}); err != nil {
			continue
		}
		updatedCount++
	}

	return updatedCount, nil
}

// Content Services

// ListContents retrieves a paginated list of content items.
func (s *Service) ListContents(ctx context.Context, req ContentListRequest) ([]*ContentResponse, int, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	items, total, err := s.store.ListContents(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	resp := make([]*ContentResponse, len(items))
	for i, item := range items {
		resp[i] = toContentResponse(item)
	}
	return resp, total, nil
}

// GetContentByID retrieves a content item by ID.
func (s *Service) GetContentByID(ctx context.Context, id int64) (*ContentResponse, error) {
	item, err := s.store.GetContentByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return toContentResponse(item), nil
}

// CreateContent creates a content item.
func (s *Service) CreateContent(ctx context.Context, tenantID, createdBy int64, req CreateContentRequest) (*ContentResponse, error) {
	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if req.Content == "" {
		return nil, fmt.Errorf("content is required")
	}

	item, err := s.store.CreateContent(ctx, tenantID, createdBy, req)
	if err != nil {
		return nil, err
	}
	return toContentResponse(item), nil
}

// UpdateContent updates a content item.
func (s *Service) UpdateContent(ctx context.Context, id int64, req UpdateContentRequest) (*ContentResponse, error) {
	item, err := s.store.UpdateContent(ctx, id, req)
	if err != nil {
		return nil, err
	}
	return toContentResponse(item), nil
}

// DeleteContent deletes a content item.
func (s *Service) DeleteContent(ctx context.Context, id int64) error {
	return s.store.DeleteContent(ctx, id)
}

// GenerateContent generates content using LLM and persists it.
func (s *Service) GenerateContent(ctx context.Context, tenantID, createdBy int64, req GenerateContentRequest) (*ContentResponse, error) {
	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}

	var generatedText string
	if s.llmClient == nil {
		generatedText = fallbackGeneratedContent(req.Title, req.Context)
	} else {
		systemPrompt := "你是医疗运营内容写作助手，请输出结构清晰、可执行的中文内容。"
		contextText := ""
		if req.Context != nil {
			contextText = *req.Context
		}
		styleText := "专业"
		if req.Style != nil && *req.Style != "" {
			styleText = *req.Style
		}
		length := 600
		if req.Length != nil && *req.Length > 0 {
			length = *req.Length
		}

		userPrompt := fmt.Sprintf("标题：%s\n背景：%s\n风格：%s\n目标字数：%d\n请直接输出正文内容。", req.Title, contextText, styleText, length)
		llmResp, err := s.llmClient.TextInference(ctx, llmgateway.TextInferenceRequest{
			TenantID:      tenantID,
			CallerService: "lingce-api",
			CallerModule:  "content",
			FunctionType:  "content_generation",
			Messages: []llmgateway.Message{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: userPrompt},
			},
			Params: &llmgateway.Params{
				Temperature: 0.7,
				MaxTokens:   3000,
			},
		})
		if err != nil {
			generatedText = fallbackGeneratedContent(req.Title, req.Context)
		} else {
			generatedText = strings.TrimSpace(llmResp.Content)
		}
	}

	createReq := CreateContentRequest{
		TopicID:   req.TopicID,
		Title:     req.Title,
		Content:   generatedText,
		Category:  nil,
		Tags:      nil,
		Images:    nil,
		ExtraData: req.ExtraData,
	}
	item, err := s.store.CreateContent(ctx, tenantID, createdBy, createReq)
	if err != nil {
		return nil, err
	}
	return toContentResponse(item), nil
}

// PublishContent publishes a content item.
func (s *Service) PublishContent(ctx context.Context, id int64) (*ContentResponse, error) {
	item, err := s.store.PublishContent(ctx, id)
	if err != nil {
		return nil, err
	}
	return toContentResponse(item), nil
}

// UnpublishContent unpublishes a content item.
func (s *Service) UnpublishContent(ctx context.Context, id int64) (*ContentResponse, error) {
	item, err := s.store.UnpublishContent(ctx, id)
	if err != nil {
		return nil, err
	}
	return toContentResponse(item), nil
}

// GenerateImages generates multiple image candidates for a content item.
func (s *Service) GenerateImages(ctx context.Context, contentID int64, req GenerateImagesRequest) (*ImageGenerationResponse, error) {
	if len(req.Prompts) == 0 {
		return nil, fmt.Errorf("prompts is required")
	}

	content, err := s.store.GetContentByID(ctx, contentID)
	if err != nil {
		return nil, err
	}

	images := make([]GeneratedImage, 0, len(req.Prompts))
	for _, prompt := range req.Prompts {
		url, err := s.generateImageURL(ctx, content.TenantID, contentID, prompt)
		if err != nil {
			return nil, err
		}
		images = append(images, GeneratedImage{
			URL:    url,
			Prompt: prompt,
		})
	}

	return &ImageGenerationResponse{Images: images}, nil
}

// GenerateSingleImage generates one image candidate for a content item.
func (s *Service) GenerateSingleImage(ctx context.Context, contentID int64, req GenerateSingleImageRequest) (*ImageGenerationResponse, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	return s.GenerateImages(ctx, contentID, GenerateImagesRequest{
		Prompts: []string{req.Prompt},
		Style:   req.Style,
	})
}

// SaveComposedImages saves selected images to content.
func (s *Service) SaveComposedImages(ctx context.Context, contentID int64, req SaveComposedImagesRequest) (*ContentResponse, error) {
	if len(req.Images) == 0 {
		return nil, fmt.Errorf("images is required")
	}
	item, err := s.store.UpdateContent(ctx, contentID, UpdateContentRequest{
		Images: req.Images,
	})
	if err != nil {
		return nil, err
	}
	return toContentResponse(item), nil
}

// Publish Task Services

// GetPublishDashboard returns publish task dashboard data.
func (s *Service) GetPublishDashboard(ctx context.Context, tenantID *int64) (*PublishDashboardResponse, error) {
	counts, err := s.store.CountPublishTasksByStatus(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	list, _, err := s.ListPublishTasks(ctx, PublishTaskListRequest{
		TenantID: tenantID,
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		return nil, err
	}

	total := int64(0)
	for _, c := range counts {
		total += c
	}

	return &PublishDashboardResponse{
		TotalTasks:     total,
		PendingTasks:   counts["pending"],
		RunningTasks:   counts["processing"],
		CompletedTasks: counts["completed"],
		FailedTasks:    counts["failed"],
		RecentTasks:    list,
	}, nil
}

// ListPublishTasks retrieves publish tasks.
func (s *Service) ListPublishTasks(ctx context.Context, req PublishTaskListRequest) ([]*PublishTaskResponse, int, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	tasks, total, err := s.store.ListPublishTasks(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	resp := make([]*PublishTaskResponse, len(tasks))
	for i, task := range tasks {
		title := ""
		content, err := s.store.GetContentByID(ctx, task.ContentID)
		if err == nil {
			title = content.Title
		}
		resp[i] = toPublishTaskResponse(task, title)
	}
	return resp, total, nil
}

// GetPublishTaskByID retrieves a publish task by ID.
func (s *Service) GetPublishTaskByID(ctx context.Context, id int64) (*PublishTaskResponse, error) {
	task, err := s.store.GetPublishTaskByID(ctx, id)
	if err != nil {
		return nil, err
	}
	title := ""
	content, err := s.store.GetContentByID(ctx, task.ContentID)
	if err == nil {
		title = content.Title
	}
	return toPublishTaskResponse(task, title), nil
}

// CreatePublishTask creates a publish task.
func (s *Service) CreatePublishTask(ctx context.Context, tenantID, createdBy int64, req CreatePublishTaskRequest) (*PublishTaskResponse, error) {
	if req.ContentID == 0 {
		return nil, fmt.Errorf("content_id is required")
	}
	if strings.TrimSpace(req.Platform) == "" {
		return nil, fmt.Errorf("platform is required")
	}

	content, err := s.store.GetContentByID(ctx, req.ContentID)
	if err != nil {
		return nil, fmt.Errorf("content not found")
	}
	if tenantID != 0 && content.TenantID != tenantID {
		return nil, fmt.Errorf("content tenant mismatch")
	}

	task, err := s.store.CreatePublishTask(ctx, content.TenantID, createdBy, req)
	if err != nil {
		return nil, err
	}
	return toPublishTaskResponse(task, content.Title), nil
}

// BatchCreatePublishTasks creates tasks in batch.
func (s *Service) BatchCreatePublishTasks(ctx context.Context, tenantID, createdBy int64, req BatchCreatePublishTasksRequest) ([]*PublishTaskResponse, error) {
	result := make([]*PublishTaskResponse, 0, len(req.Tasks))
	for _, taskReq := range req.Tasks {
		task, err := s.CreatePublishTask(ctx, tenantID, createdBy, taskReq)
		if err != nil {
			continue
		}
		result = append(result, task)
	}
	return result, nil
}

// UpdatePublishTaskStatus updates a publish task status.
func (s *Service) UpdatePublishTaskStatus(ctx context.Context, id int64, status string) (*PublishTaskResponse, error) {
	if strings.TrimSpace(status) == "" {
		return nil, fmt.Errorf("status is required")
	}
	task, err := s.store.UpdatePublishTaskStatus(ctx, id, status)
	if err != nil {
		return nil, err
	}

	title := ""
	content, err := s.store.GetContentByID(ctx, task.ContentID)
	if err == nil {
		title = content.Title
	}
	return toPublishTaskResponse(task, title), nil
}

// DeletePublishTask deletes a publish task.
func (s *Service) DeletePublishTask(ctx context.Context, id int64) error {
	return s.store.DeletePublishTask(ctx, id)
}

func (s *Service) generateTopicsFallback(ctx context.Context, tenantID, createdBy int64, req GenerateTopicsRequest) ([]*TopicResponse, error) {
	keywords := extractKeywordsFromText(req.Context, req.Count)
	if len(keywords) == 0 {
		keywords = []string{"运营", "患者沟通", "门店服务", "复诊转化", "内容传播"}
	}

	responses := make([]*TopicResponse, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		kw := keywords[i%len(keywords)]
		title := fmt.Sprintf("%s场景下的实操指南 %d", kw, i+1)
		desc := fmt.Sprintf("围绕“%s”输出可直接落地的内容结构与执行建议", kw)
		topic, err := s.store.CreateTopic(ctx, tenantID, createdBy, CreateTopicRequest{
			Title:       title,
			Description: &desc,
			Category:    req.Category,
			Source:      "ai_generated",
			Tags:        []string{kw},
		})
		if err != nil {
			continue
		}
		responses = append(responses, toTopicResponse(topic))
	}

	return responses, nil
}

func scoreTopic(t *TopicResponse) float64 {
	score := 50.0
	if t.Priority != nil {
		score += float64(*t.Priority) * 5
	}

	switch t.Status {
	case "selected":
		score += 20
	case "in_progress":
		score += 15
	case "completed":
		score += 10
	case "draft":
		score += 5
	case "archived":
		score -= 10
	}

	switch t.Source {
	case "conversation_insight":
		score += 10
	case "idea":
		score += 8
	case "ai_generated":
		score += 5
	}

	if createdAt, err := time.Parse(time.RFC3339, t.CreatedAt); err == nil {
		age := time.Since(createdAt)
		switch {
		case age <= 24*time.Hour:
			score += 20
		case age <= 7*24*time.Hour:
			score += 10
		case age <= 30*24*time.Hour:
			score += 5
		}
	}

	return score
}

func calcTrend(t *TopicResponse) string {
	if createdAt, err := time.Parse(time.RFC3339, t.CreatedAt); err == nil {
		if time.Since(createdAt) <= 7*24*time.Hour {
			return "rising"
		}
	}
	switch t.Status {
	case "archived":
		return "declining"
	default:
		return "stable"
	}
}

func extractKeywordsFromText(text string, limit int) []string {
	re := regexp.MustCompile(`[\p{Han}A-Za-z0-9_]{2,}`)
	raw := re.FindAllString(strings.ToLower(text), -1)
	if len(raw) == 0 {
		return nil
	}

	stopwords := map[string]struct{}{
		"this": {}, "that": {}, "with": {}, "from": {}, "have": {}, "will": {},
		"and": {}, "for": {}, "the": {}, "you": {}, "your": {}, "are": {},
		"idea": {}, "session": {}, "content": {}, "topics": {}, "topic": {},
	}

	freq := make(map[string]int)
	for _, w := range raw {
		if _, ok := stopwords[w]; ok {
			continue
		}
		freq[w]++
	}

	type kv struct {
		key string
		val int
	}
	items := make([]kv, 0, len(freq))
	for k, v := range freq {
		items = append(items, kv{key: k, val: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].val == items[j].val {
			return items[i].key < items[j].key
		}
		return items[i].val > items[j].val
	})

	if limit <= 0 {
		limit = 10
	}
	if len(items) > limit {
		items = items[:limit]
	}

	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.key)
	}
	return result
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

func toContentResponse(item *ContentItem) *ContentResponse {
	return &ContentResponse{
		ID:            item.ID,
		TenantID:      item.TenantID,
		TopicID:       item.TopicID,
		Title:         item.Title,
		Content:       item.Content,
		Summary:       item.Summary,
		Category:      item.Category,
		Tags:          item.Tags,
		Status:        item.Status,
		PublishedAt:   formatTimePtr(item.PublishedAt),
		UnpublishedAt: formatTimePtr(item.UnpublishedAt),
		ViewCount:     item.ViewCount,
		LikeCount:     item.LikeCount,
		ShareCount:    item.ShareCount,
		Images:        item.Images,
		ExtraData:     item.ExtraData,
		CreatedBy:     item.CreatedBy,
		CreatedAt:     item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     item.UpdatedAt.Format(time.RFC3339),
	}
}

func toPublishTaskResponse(task *ContentPublishTask, contentTitle string) *PublishTaskResponse {
	return &PublishTaskResponse{
		ID:           task.ID,
		TenantID:     task.TenantID,
		ContentID:    task.ContentID,
		ContentTitle: contentTitle,
		Platform:     task.Platform,
		Status:       task.Status,
		ScheduledAt:  formatTimePtr(task.ScheduledAt),
		PublishedAt:  formatTimePtr(task.PublishedAt),
		ErrorMsg:     task.ErrorMsg,
		ExtraData:    task.ExtraData,
		CreatedBy:    task.CreatedBy,
		CreatedAt:    task.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    task.UpdatedAt.Format(time.RFC3339),
	}
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := t.Format(time.RFC3339)
	return &formatted
}

func fallbackGeneratedContent(title string, context *string) string {
	bg := ""
	if context != nil {
		bg = strings.TrimSpace(*context)
	}
	if bg == "" {
		bg = "围绕医疗运营场景，突出可执行建议。"
	}
	return fmt.Sprintf("【%s】\n\n一、背景\n%s\n\n二、核心问题\n1. 目标人群不清晰\n2. 内容价值表达不足\n\n三、执行建议\n1. 用具体场景开头，增强代入感\n2. 以问题-方法-案例结构组织内容\n3. 给出可直接执行的清单\n\n四、落地动作\n- 今日完成内容初稿\n- 明日完成渠道适配\n- 本周完成数据复盘", title, bg)
}

func (s *Service) generateImageURL(ctx context.Context, tenantID, contentID int64, prompt string) (string, error) {
	if s.ossClient == nil {
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(prompt))
		return fmt.Sprintf("mock://content/%d/%d/%d.png", tenantID, contentID, hash.Sum64()), nil
	}

	key := oss.GenerateObjectKey(fmt.Sprintf("content/%d/%d", tenantID, contentID), ".txt")
	data := []byte("generated_image_prompt: " + prompt)
	url, err := s.ossClient.UploadBytes(ctx, key, data, &oss.UploadOptions{
		ContentType: "text/plain; charset=utf-8",
		Metadata: map[string]string{
			"prompt": prompt,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload generated image payload: %w", err)
	}
	return url, nil
}
