package content

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// GEO Optimization Handlers

// AnalyzeGEO handles analyzing content for GEO optimization
func (h *Handler) AnalyzeGEO(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	content := strings.TrimSpace(toString(req["content"]))
	if content == "" {
		content = strings.TrimSpace(toString(req["text"]))
	}
	if content == "" {
		httputil.WriteBadRequest(w, "content is required")
		return
	}

	keywords := extractKeywordsFromText(content, 10)
	wordCount := len(extractKeywordsFromText(content, 300))
	score := 60.0
	if wordCount >= 80 {
		score += 10
	}
	if wordCount >= 200 {
		score += 10
	}
	if len(keywords) >= 5 {
		score += 10
	}
	readabilityLevel := "medium"
	if wordCount < 60 {
		readabilityLevel = "easy"
	} else if wordCount > 220 {
		readabilityLevel = "hard"
	}
	suggestions := []string{
		"在标题和前两段自然包含核心关键词",
		"增加小标题分段，提升可读性与抓取结构",
		"补充结尾行动建议，强化转化导向",
	}
	seoScore := score - 5

	httputil.WriteSuccess(w, map[string]interface{}{
		"score":    score,
		"keywords": keywords,
		"readability": map[string]interface{}{
			"score": score - 8,
			"level": readabilityLevel,
		},
		"suggestions": suggestions,
		"seo_score":   seoScore,
	})
}

// AnalyzeGEOByID handles analyzing content by ID for GEO optimization
func (h *Handler) AnalyzeGEOByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	contentID, err := parseContentID(r)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid content ID")
		return
	}

	content, err := h.service.GetContentByID(r.Context(), contentID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	req := map[string]interface{}{"content": content.Content}
	r2, _ := http.NewRequest(http.MethodPost, "/", nil)
	r2 = r2.WithContext(r.Context())
	_ = req
	keywords := extractKeywordsFromText(content.Content, 10)
	wordCount := len(extractKeywordsFromText(content.Content, 300))
	score := 60.0
	if wordCount >= 80 {
		score += 10
	}
	if wordCount >= 200 {
		score += 10
	}
	if len(keywords) >= 5 {
		score += 10
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"content_id": contentID,
		"score":      score,
		"keywords":   keywords,
		"readability": map[string]interface{}{
			"score": score - 8,
			"level": "medium",
		},
		"suggestions": []string{
			"在首段引入目标关键词",
			"增加场景化案例和步骤化表达",
			"补齐FAQ段落覆盖长尾搜索意图",
		},
		"seo_score": score - 5,
	})
}

// OptimizeGEO handles deep GEO optimization
func (h *Handler) OptimizeGEO(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	content := strings.TrimSpace(toString(req["content"]))
	if content == "" {
		httputil.WriteBadRequest(w, "content is required")
		return
	}
	keywords := extractKeywordsFromText(content, 6)
	if len(keywords) == 0 {
		keywords = []string{"医疗运营", "患者沟通", "复诊转化"}
	}
	optimized := "【优化标题】" + keywords[0] + "提升指南\n\n" + content
	optimized += "\n\n【结构化补充】\n1. 关键问题\n2. 执行步骤\n3. 指标复盘"

	httputil.WriteSuccess(w, map[string]interface{}{
		"optimized_content": optimized,
		"improvements": []string{
			"强化关键词前置与标题匹配",
			"补充段落层次，便于搜索抓取",
			"增加可执行清单，提高内容价值密度",
		},
		"before_score": 62.0,
		"after_score":  82.0,
	})
}

// GetGEOPromptInjection handles getting GEO prompt injection strategy
func (h *Handler) GetGEOPromptInjection(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"strategies": []string{
			"关键词分层注入（核心词+长尾词）",
			"段落意图标注（问题/方案/行动）",
			"结构提示（H2/H3+要点列表）",
		},
		"prompts": map[string]interface{}{
			"keyword_optimization":    "请将关键词{{keywords}}自然融入标题、首段与小标题。",
			"readability_improvement": "请把内容改写为短句+要点列表，降低阅读门槛。",
			"structure_optimization":  "请按问题-方案-步骤-复盘结构重排内容。",
			"seo_enhancement":         "请补充FAQ段落并覆盖长尾搜索问题。",
		},
	})
}
