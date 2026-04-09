package content

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// GetInsightsStats handles getting conversation insights statistics
func (h *Handler) GetInsightsStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	source := "conversation_insight"
	topics, total, err := h.service.ListTopics(r.Context(), TopicListRequest{
		TenantID: claims.TenantID,
		Source:   &source,
		Page:     1,
		PageSize: 200,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	today := time.Now().Format("2006-01-02")
	todayCount := 0
	unique := map[string]struct{}{}
	totalQuestions := int64(0)
	for _, t := range topics {
		unique[strings.ToLower(strings.TrimSpace(t.Title))] = struct{}{}
		totalQuestions += int64(len(t.Tags) + 1)
		if strings.HasPrefix(t.CreatedAt, today) {
			todayCount++
		}
	}
	avg := 0.0
	if todayCount > 0 {
		avg = float64(totalQuestions) / float64(todayCount)
	}
	response := &ConversationInsightsStatsResponse{
		TotalConversations: int64(total),
		TotalQuestions:     totalQuestions,
		UniqueTopics:       int64(len(unique)),
		AvgQuestionsPerDay: avg,
	}

	httputil.WriteSuccess(w, response)
}

// GetFrequentQuestions handles getting frequent questions
func (h *Handler) GetFrequentQuestions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	source := "conversation_insight"
	topics, _, err := h.service.ListTopics(r.Context(), TopicListRequest{
		TenantID: claims.TenantID,
		Source:   &source,
		Page:     1,
		PageSize: 200,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	type qa struct {
		Question string
		Count    int64
	}
	freq := map[string]int64{}
	for _, topic := range topics {
		q := strings.TrimSpace(topic.Title)
		if q != "" {
			freq[q]++
		}
	}
	items := make([]qa, 0, len(freq))
	for q, c := range freq {
		items = append(items, qa{Question: q, Count: c})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Question < items[j].Question
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > 20 {
		items = items[:20]
	}
	questions := make([]FrequentQuestion, 0, len(items))
	for _, item := range items {
		questions = append(questions, FrequentQuestion{
			Question:  item.Question,
			Count:     item.Count,
			Category:  "conversation",
			Sentiment: "neutral",
		})
	}
	response := &FrequentQuestionsResponse{
		Questions: questions,
		Total:     int64(len(questions)),
	}

	httputil.WriteSuccess(w, response)
}

// MineTopics handles mining topics from conversations
func (h *Handler) MineTopics(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req MineTopicsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	contents, _, err := h.service.ListContents(r.Context(), ContentListRequest{
		TenantID: claims.TenantID,
		Page:     1,
		PageSize: 200,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	minCount := 1
	if req.MinCount != nil && *req.MinCount > 1 {
		minCount = *req.MinCount
	}
	wordCount := map[string]int64{}
	for _, c := range contents {
		words := extractKeywordsFromText(c.Title+" "+c.Content, 12)
		for _, word := range words {
			wordCount[word]++
		}
	}

	topics := make([]MinedTopic, 0, len(wordCount))
	for word, count := range wordCount {
		if int(count) < minCount {
			continue
		}
		topics = append(topics, MinedTopic{
			Title:       fmt.Sprintf("%s运营策略", word),
			Description: fmt.Sprintf("围绕关键词“%s”的内容策略与执行建议", word),
			Keywords:    []string{word},
			Count:       count,
			Relevance:   float64(count) / float64(len(contents)+1),
		})
	}
	sort.Slice(topics, func(i, j int) bool { return topics[i].Count > topics[j].Count })
	if len(topics) > 30 {
		topics = topics[:30]
	}
	response := &MineTopicsResponse{
		Topics: topics,
		Total:  int64(len(topics)),
	}

	httputil.WriteSuccess(w, response)
}

// SaveMinedTopics handles saving mined topics
func (h *Handler) SaveMinedTopics(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req SaveMinedTopicsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	created := 0
	for _, topic := range req.Topics {
		if strings.TrimSpace(topic.Title) == "" {
			continue
		}
		source := "conversation_insight"
		_, err := h.service.CreateTopic(r.Context(), *claims.TenantID, claims.UserID, CreateTopicRequest{
			Title:       topic.Title,
			Description: strPtr(topic.Description),
			Category:    topic.Category,
			Tags:        topic.Keywords,
			Source:      source,
		})
		if err == nil {
			created++
		}
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"created": created,
		"message": "Topics saved successfully",
	})
}
