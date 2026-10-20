package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

type ContentVersion struct {
	ID          int64      `json:"id"`
	ContentID   int64      `json:"content_id"`
	TenantID    int64      `json:"tenant_id"`
	Version     int        `json:"version"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	ContentType *string    `json:"content_type,omitempty"`
	Platform    *string    `json:"platform,omitempty"`
	Status      string     `json:"status"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
	CreatedBy   int64      `json:"created_by"`
	CreatedAt   string     `json:"created_at"`
}

func (s *Service) SnapshotContentVersion(ctx context.Context, contentID, tenantID, createdBy int64) error {
	if contentID <= 0 || tenantID <= 0 {
		return nil
	}
	_, err := s.store.pool.Exec(ctx, `
		INSERT INTO content_item_versions
			(content_id, tenant_id, version, title, content, content_type, platform, status, extra_data, created_by)
		SELECT id, tenant_id, COALESCE((SELECT MAX(version) FROM content_item_versions WHERE content_id=$1), 0) + 1,
		       title, content, content_type, platform, status, COALESCE(extra_data, '{}'::jsonb), $3
		FROM content_items
		WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
	`, contentID, tenantID, createdBy)
	if err != nil {
		return fmt.Errorf("snapshot content version: %w", err)
	}
	return nil
}

func (s *Service) ListContentVersions(ctx context.Context, contentID, tenantID int64) ([]ContentVersion, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT id, content_id, tenant_id, version, title, content, content_type, platform, status,
		       COALESCE(extra_data, '{}'::jsonb), created_by, created_at
		FROM content_item_versions
		WHERE content_id=$1 AND tenant_id=$2
		ORDER BY version DESC
	`, contentID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list content versions: %w", err)
	}
	defer rows.Close()
	versions := make([]ContentVersion, 0, 4)
	for rows.Next() {
		var v ContentVersion
		var extraRaw []byte
		var createdAt time.Time
		if err := rows.Scan(&v.ID, &v.ContentID, &v.TenantID, &v.Version, &v.Title, &v.Content, &v.ContentType, &v.Platform, &v.Status, &extraRaw, &v.CreatedBy, &createdAt); err != nil {
			return nil, err
		}
		if len(extraRaw) > 0 {
			_ = jsonUnmarshalObject(extraRaw, &v.ExtraData)
		}
		v.CreatedAt = createdAt.Format(time.RFC3339)
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// RewriteSelection rewrites only the selected text through the DB-owned
// content_rewrite prompt and model config. It returns the rewritten text and
// does not mutate the content item.
func (s *Service) RewriteSelection(ctx context.Context, tenantID int64, selectedText, instruction, contextText string) (string, error) {
	selectedText = strings.TrimSpace(selectedText)
	instruction = strings.TrimSpace(instruction)
	if selectedText == "" {
		return "", fmt.Errorf("selected_text is required")
	}
	if instruction == "" {
		instruction = "保持事实和边界不变，改得更清楚、更口语。"
	}
	tpl, err := s.store.GetActiveContentPromptTemplateByFunctionType(ctx, tenantID, "content_rewrite")
	if err != nil {
		return "", fmt.Errorf("load content_rewrite prompt: %w", err)
	}
	systemPrompt := strings.TrimSpace(tpl.SystemPrompt)
	if systemPrompt == "" {
		return "", fmt.Errorf("content_rewrite prompt %s has empty system_prompt", tpl.Code)
	}
	userPrompt := tpl.PromptTemplate
	userPrompt = strings.ReplaceAll(userPrompt, "{{selected_text}}", selectedText)
	userPrompt = strings.ReplaceAll(userPrompt, "{{instruction}}", instruction)
	userPrompt = strings.ReplaceAll(userPrompt, "{{context}}", strings.TrimSpace(contextText))

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_rewrite", "content_generation", "chat"})
	if err != nil {
		return "", fmt.Errorf("resolve content_rewrite model: %w", err)
	}
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  "content.content_rewrite",
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Billing:       newBillingMetadata("content", "content_rewrite", 0, "content_rewrite", "content_rewrite"),
		Messages: []llmgateway.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{Temperature: 0.3, MaxTokens: 1500, TimeoutSeconds: 90},
	}
	applyModelParamsToLLMRequest(&req, selectedModel.ModelParams)
	ensureMinLLMTimeout(&req, 90)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return "", fmt.Errorf("rewrite selection via LLM: %w", err)
	}
	return strings.TrimSpace(resp.Content), nil
}

func jsonUnmarshalObject(raw []byte, target *JSONObject) error {
	if len(raw) == 0 {
		return nil
	}
	var obj JSONObject
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	*target = obj
	return nil
}

type ContentVersionCompare struct {
	From    ContentVersion `json:"from"`
	To      ContentVersion `json:"to"`
	Added   []string       `json:"added"`
	Removed []string       `json:"removed"`
}

func (s *Service) GetContentVersion(ctx context.Context, contentID, tenantID int64, version int) (*ContentVersion, error) {
	var v ContentVersion
	var extraRaw []byte
	var createdAt time.Time
	err := s.store.pool.QueryRow(ctx, `
		SELECT id, content_id, tenant_id, version, title, content, content_type, platform, status,
		       COALESCE(extra_data, '{}'::jsonb), created_by, created_at
		FROM content_item_versions
		WHERE content_id=$1 AND tenant_id=$2 AND version=$3
	`, contentID, tenantID, version).Scan(&v.ID, &v.ContentID, &v.TenantID, &v.Version, &v.Title, &v.Content, &v.ContentType, &v.Platform, &v.Status, &extraRaw, &v.CreatedBy, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("get content version: %w", err)
	}
	if len(extraRaw) > 0 {
		_ = jsonUnmarshalObject(extraRaw, &v.ExtraData)
	}
	v.CreatedAt = createdAt.Format(time.RFC3339)
	return &v, nil
}

func (s *Service) CompareContentVersions(ctx context.Context, contentID, tenantID int64, from, to int) (*ContentVersionCompare, error) {
	if from <= 0 || to <= 0 || from == to {
		return nil, fmt.Errorf("from and to must be different positive versions")
	}
	fromVersion, err := s.GetContentVersion(ctx, contentID, tenantID, from)
	if err != nil {
		return nil, err
	}
	toVersion, err := s.GetContentVersion(ctx, contentID, tenantID, to)
	if err != nil {
		return nil, err
	}
	added, removed := diffLines(fromVersion.Content, toVersion.Content)
	return &ContentVersionCompare{From: *fromVersion, To: *toVersion, Added: added, Removed: removed}, nil
}

func (s *Service) RestoreContentVersion(ctx context.Context, contentID, tenantID, createdBy int64, version int) (*ContentResponse, error) {
	selected, err := s.GetContentVersion(ctx, contentID, tenantID, version)
	if err != nil {
		return nil, err
	}
	title := selected.Title
	content := selected.Content
	update := UpdateContentRequest{
		Title:       &title,
		Content:     &content,
		ContentType: selected.ContentType,
		Platform:    selected.Platform,
		ExtraData:   selected.ExtraData,
	}
	return s.UpdateContent(ctx, contentID, update)
}

func diffLines(before, after string) ([]string, []string) {
	oldCount := map[string]int{}
	for _, line := range strings.Split(before, "\n") {
		oldCount[line]++
	}
	added := make([]string, 0)
	for _, line := range strings.Split(after, "\n") {
		if oldCount[line] > 0 {
			oldCount[line]--
		} else if strings.TrimSpace(line) != "" {
			added = append(added, line)
		}
	}
	newCount := map[string]int{}
	for _, line := range strings.Split(after, "\n") {
		newCount[line]++
	}
	removed := make([]string, 0)
	for _, line := range strings.Split(before, "\n") {
		if newCount[line] > 0 {
			newCount[line]--
		} else if strings.TrimSpace(line) != "" {
			removed = append(removed, line)
		}
	}
	return added, removed
}
