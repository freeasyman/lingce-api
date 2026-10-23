package content

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// 选题 → 创作的上下文组装：把"判断/洞察"沉淀下来的信息完整带进创作 prompt，
// 而不是只给大模型一个标题。

// contentGoalLabel 内容目标中文标签。
func contentGoalLabel(goal string) string {
	switch strings.TrimSpace(goal) {
	case "science":
		return "科普"
	case "brand":
		return "品牌"
	case "doctor_ip":
		return "医生IP"
	case "conversion":
		return "转化"
	case "geo":
		return "GEO"
	default:
		return goal
	}
}

// expectedHelp 选题"预期帮助"四维结构。
type expectedHelp struct {
	Search     []string `json:"search"`
	Cognition  string   `json:"cognition"`
	Conversion string   `json:"conversion"`
	AICitation string   `json:"ai_citation"`
}

// insightEvidence 洞察 evidence 结构。
type insightEvidence struct {
	Angle          string           `json:"angle"`
	Quotes         []insightQuote   `json:"quotes"`
	PatientConcern string           `json:"patient_concern"`
}

type insightQuote struct {
	Text    string `json:"text"`
	Time    string `json:"time"`
	Speaker string `json:"speaker"`
}

// topicEvidenceInsight 选题关联的证据洞察（evidence_ref → content_insights）。
type topicEvidenceInsight struct {
	Type             string          `json:"type"`
	Topic            string          `json:"topic"`
	Evidence         json.RawMessage `json:"evidence"`
	SuggestedTargets json.RawMessage `json:"suggested_targets"`
}

// getTopicEvidenceInsight 取一条证据洞察。
func (s *Store) getTopicEvidenceInsight(ctx context.Context, tenantID, insightID int64) (*topicEvidenceInsight, error) {
	var e topicEvidenceInsight
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(type,''), COALESCE(topic,''), COALESCE(evidence,'{}'::jsonb), COALESCE(suggested_targets,'[]'::jsonb)
		FROM content_insights WHERE id = $1 AND tenant_id = $2`, insightID, tenantID).
		Scan(&e.Type, &e.Topic, &e.Evidence, &e.SuggestedTargets)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// getQuestionClusterByTopic 尽力匹配反复问簇（按洞察/选题关键词 contains）。
func (s *Store) getQuestionClusterByTopic(ctx context.Context, tenantID int64, haystack string) (*QuestionCluster, error) {
	haystack = strings.ToLower(strings.TrimSpace(haystack))
	if haystack == "" {
		return nil, nil
	}
	clusters, err := s.listQuestionClusters(ctx, tenantID, 2)
	if err != nil {
		return nil, err
	}
	for i := range clusters {
		c := &clusters[i]
		key := strings.ToLower(strings.TrimSpace(c.Topic))
		if key == "" {
			continue
		}
		// 簇关键词是选题/洞察的子串，或反之（取更短的一方做包含判断）
		if strings.Contains(haystack, key) || strings.Contains(key, haystack) {
			return c, nil
		}
	}
	return nil, nil
}

// topicContextText 组装"选题依据"上下文文本。
func (s *Service) topicContextText(ctx context.Context, tenantID, topicID int64) string {
	if topicID <= 0 {
		return ""
	}
	t, err := s.store.getTopicV2(ctx, tenantID, topicID)
	if err != nil || t == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("## 选题依据（来自真实录音/洞察，创作时请充分用上这些信息，不要只围绕标题泛泛而谈）\n\n")

	if t.Recommendation != "" {
		b.WriteString("- 推荐理由（为什么发这篇）：" + strings.TrimSpace(t.Recommendation) + "\n")
	}
	if t.Goal != "" {
		b.WriteString("- 内容目标：" + contentGoalLabel(t.Goal) + "\n")
	}
	if t.Description != "" {
		b.WriteString("- 选题说明：" + strings.TrimSpace(t.Description) + "\n")
	}

	// 预期帮助 4 维
	if len(t.ExpectedHelp) > 0 && string(t.ExpectedHelp) != "{}" {
		var eh expectedHelp
		if json.Unmarshal(t.ExpectedHelp, &eh) == nil {
			b.WriteString("- 预期帮助：\n")
			if len(eh.Search) > 0 {
				b.WriteString("  · 患者搜索意图（会搜的词）：" + strings.Join(eh.Search, "、") + "\n")
			}
			if strings.TrimSpace(eh.Cognition) != "" {
				b.WriteString("  · 用户认知目标：" + strings.TrimSpace(eh.Cognition) + "\n")
			}
			if strings.TrimSpace(eh.Conversion) != "" {
				b.WriteString("  · 获客预期：" + strings.TrimSpace(eh.Conversion) + "\n")
			}
			if strings.TrimSpace(eh.AICitation) != "" {
				b.WriteString("  · AI 收录预期：" + strings.TrimSpace(eh.AICitation) + "\n")
			}
		}
	}

	// 证据洞察（角度 / 患者顾虑 / 原话）
	var insightTopic string
	if t.EvidenceRef != nil && *t.EvidenceRef > 0 {
		if e, err := s.store.getTopicEvidenceInsight(ctx, tenantID, *t.EvidenceRef); err == nil && e != nil {
			insightTopic = e.Topic
			var ev insightEvidence
			_ = json.Unmarshal(e.Evidence, &ev)
			b.WriteString("- 真实录音洞察（证据）：\n")
			if strings.TrimSpace(e.Topic) != "" {
				b.WriteString("  · 患者核心问题：" + strings.TrimSpace(e.Topic) + "\n")
			}
			if strings.TrimSpace(ev.PatientConcern) != "" {
				b.WriteString("  · 患者顾虑：" + strings.TrimSpace(ev.PatientConcern) + "\n")
			}
			if strings.TrimSpace(ev.Angle) != "" {
				b.WriteString("  · 内容切入角度：" + strings.TrimSpace(ev.Angle) + "\n")
			}
			if len(ev.Quotes) > 0 {
				b.WriteString("  · 患者/咨询师原话（真实录音，可直接引用做金句）：\n")
				for _, q := range ev.Quotes {
					if strings.TrimSpace(q.Text) == "" {
						continue
					}
					who := strings.TrimSpace(q.Speaker)
					when := strings.TrimSpace(q.Time)
					suffix := ""
					if who != "" || when != "" {
						suffix = "（" + who
						if when != "" {
							suffix += "，" + when
						}
						suffix += "）"
					}
					b.WriteString("    - 「" + strings.TrimSpace(q.Text) + "」" + suffix + "\n")
				}
			}
		}
	}

	// 反复问热度（尽力匹配）
	if hay := t.Title + " " + t.Description + " " + insightTopic; strings.TrimSpace(hay) != "" {
		if c, err := s.store.getQuestionClusterByTopic(ctx, tenantID, hay); err == nil && c != nil && c.Frequency >= 2 {
			b.WriteString("- 反复问热度：患者已反复问 " + fmt.Sprintf("%d", c.Frequency) + " 次，属高频真实需求\n")
			var quotes []string
			_ = json.Unmarshal(c.Quotes, &quotes)
			if len(quotes) > 0 {
				b.WriteString("  · 代表原话：")
				parts := make([]string, 0, len(quotes))
				for _, q := range quotes {
					if strings.TrimSpace(q) != "" {
						parts = append(parts, "「"+strings.TrimSpace(q)+"」")
					}
				}
				b.WriteString(strings.Join(parts, "、") + "\n")
			}
		}
	}

	out := b.String()
	if out != "" {
		slog.Info("topic creation context assembled", "topic_id", topicID, "len", len(out))
	}
	return out
}

// buildDraftUserPrompt 组装创作 user prompt：优先用选题依据上下文，否则回退到标题/说明/证据；最后拼知识库。
func (s *Service) buildDraftUserPrompt(ctx context.Context, tenantID int64, req GenerateGraphicNoteDraftRequest) string {
	tc := s.topicContextText(ctx, tenantID, valueOrZero(req.TopicID))
	var user strings.Builder
	if tc != "" {
		user.WriteString(tc + "\n")
		if strings.TrimSpace(req.Evidence) != "" && !strings.Contains(tc, req.Evidence) {
			user.WriteString("- 补充证据（金句/原话）：" + strings.TrimSpace(req.Evidence) + "\n")
		}
	} else {
		user.WriteString("选题标题：" + req.TopicTitle + "\n")
		if req.TopicDescription != "" {
			user.WriteString("选题说明：" + req.TopicDescription + "\n")
		}
		if req.Evidence != "" {
			user.WriteString("证据（金句/原话/依据）：" + req.Evidence + "\n")
		}
	}
	if kc := s.knowledgeContext(ctx, tenantID); kc != "" {
		user.WriteString("\n" + kc + "\n")
	}
	return user.String()
}
