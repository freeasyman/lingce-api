package recording

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
	"time"
)

// WeeklyReportService handles weekly report generation and distribution
type WeeklyReportService struct {
	store     *Store
	smtpHost  string
	smtpPort  string
	smtpUser  string
	smtpPass  string
	fromEmail string
	logger    *slog.Logger
}

// NewWeeklyReportService creates a new weekly report service
func NewWeeklyReportService(
	store *Store,
	smtpHost, smtpPort, smtpUser, smtpPass, fromEmail string,
	logger *slog.Logger,
) *WeeklyReportService {
	return &WeeklyReportService{
		store:     store,
		smtpHost:  smtpHost,
		smtpPort:  smtpPort,
		smtpUser:  smtpUser,
		smtpPass:  smtpPass,
		fromEmail: fromEmail,
		logger:    logger,
	}
}

// WeeklyReportData represents the data for a weekly report
type WeeklyReportData struct {
	TenantID               int64                  `json:"tenant_id"`
	WeekStartDate          string                 `json:"week_start_date"`
	WeekEndDate            string                 `json:"week_end_date"`
	TotalInteractions      int                    `json:"total_interactions"`
	TotalAppointments      int                    `json:"total_appointments"`
	TotalWalkIns           int                    `json:"total_walk_ins"`
	RiskEventCount         int                    `json:"risk_event_count"`
	TopQuestions           []string               `json:"top_questions"`
	CompetitorMentions     []string               `json:"competitor_mentions"`
	DoctorInquiries        []string               `json:"doctor_inquiries"`
	ChannelFeedback        []string               `json:"channel_feedback"`
	KeyInsights            []string               `json:"key_insights"`
	TrendAnalysis          map[string]interface{} `json:"trend_analysis"`
	IssueSummary           []string               `json:"issue_summary"`
	ImprovementSuggestions []string               `json:"improvement_suggestions"`
	GeneratedAt            time.Time              `json:"generated_at"`
}

// GenerateWeeklyReport generates a weekly report for a tenant
func (s *WeeklyReportService) GenerateWeeklyReport(ctx context.Context, tenantID int64, weekEndDate time.Time) (*WeeklyReportData, error) {
	// Calculate week start and end dates
	weekStartDate := weekEndDate.AddDate(0, 0, -6) // 7 days back
	weekStartStr := weekStartDate.Format("2006-01-02")
	weekEndStr := weekEndDate.Format("2006-01-02")

	// Query daily reports for the week (legacy table schema fields)
	query := `
		SELECT total_estimated_interactions, estimated_appointment_count, estimated_walkin_count, risk_event_count,
		       top_questions, competitor_mentions, doctor_inquiries, channel_feedback
		FROM frontdesk_daily_reports
		WHERE tenant_id = $1
		  AND report_date >= $2
		  AND report_date <= $3
		ORDER BY report_date ASC
	`

	rows, err := s.store.pool.Query(ctx, query, tenantID, weekStartStr, weekEndStr)
	if err != nil {
		s.logger.Error("failed to query daily reports", "error", err, "tenant_id", tenantID)
		return nil, err
	}
	defer rows.Close()

	// Aggregate data from daily reports
	reportData := &WeeklyReportData{
		TenantID:      tenantID,
		WeekStartDate: weekStartStr,
		WeekEndDate:   weekEndStr,
		GeneratedAt:   time.Now(),
		TrendAnalysis: make(map[string]interface{}),
	}

	dailyMetrics := make([]map[string]interface{}, 0)

	for rows.Next() {
		var totalInteractions, totalAppointments, totalWalkIns, riskEventCount int
		var topQuestions, competitorMentions, doctorInquiries, channelFeedback []byte
		if err := rows.Scan(
			&totalInteractions, &totalAppointments, &totalWalkIns, &riskEventCount,
			&topQuestions, &competitorMentions, &doctorInquiries, &channelFeedback,
		); err != nil {
			s.logger.Error("failed to scan daily report", "error", err)
			continue
		}

		reportData.TotalInteractions += totalInteractions
		reportData.TotalAppointments += totalAppointments
		reportData.TotalWalkIns += totalWalkIns
		reportData.RiskEventCount += riskEventCount

		dailyData := map[string]interface{}{
			"total_interactions": totalInteractions,
			"total_appointments": totalAppointments,
			"total_walk_ins":     totalWalkIns,
			"risk_event_count":   riskEventCount,
			"top_questions":      parseJSONStringArray(topQuestions),
		}
		dailyMetrics = append(dailyMetrics, dailyData)

		for _, q := range parseJSONStringArray(topQuestions) {
			reportData.TopQuestions = appendUnique(reportData.TopQuestions, q)
		}
		for _, c := range parseJSONStringArray(competitorMentions) {
			reportData.CompetitorMentions = appendUnique(reportData.CompetitorMentions, c)
		}
		for _, d := range parseJSONStringArray(doctorInquiries) {
			reportData.DoctorInquiries = appendUnique(reportData.DoctorInquiries, d)
		}
		for _, ch := range parseJSONStringArray(channelFeedback) {
			reportData.ChannelFeedback = appendUnique(reportData.ChannelFeedback, ch)
		}
	}

	// Calculate trend analysis
	reportData.TrendAnalysis = s.calculateTrends(dailyMetrics)

	// Generate insights and suggestions
	reportData.KeyInsights = s.generateKeyInsights(reportData)
	reportData.IssueSummary = s.generateIssueSummary(reportData)
	reportData.ImprovementSuggestions = s.generateSuggestions(reportData)

	return reportData, nil
}

func parseJSONStringArray(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var source []interface{}
	if err := json.Unmarshal(raw, &source); err != nil {
		return []string{}
	}
	out := make([]string, 0, len(source))
	for _, item := range source {
		text := strings.TrimSpace(fmt.Sprintf("%v", item))
		if text != "" && text != "<nil>" {
			out = append(out, text)
		}
	}
	return out
}

// SendWeeklyReport sends the weekly report via email
func (s *WeeklyReportService) SendWeeklyReport(ctx context.Context, reportData *WeeklyReportData, recipientEmail string) error {
	// Build email content
	subject := fmt.Sprintf("前台分析周报 - %s 至 %s", reportData.WeekStartDate, reportData.WeekEndDate)
	body := s.buildEmailBody(reportData)

	// Send email
	auth := smtp.PlainAuth("", s.smtpUser, s.smtpPass, s.smtpHost)
	addr := fmt.Sprintf("%s:%s", s.smtpHost, s.smtpPort)

	message := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		s.fromEmail,
		recipientEmail,
		subject,
		body,
	)

	err := smtp.SendMail(addr, auth, s.fromEmail, []string{recipientEmail}, []byte(message))
	if err != nil {
		s.logger.Error("failed to send email", "error", err, "recipient", recipientEmail)
		return err
	}

	s.logger.Info("weekly report sent", "recipient", recipientEmail, "week", reportData.WeekStartDate)
	return nil
}

// buildEmailBody builds the HTML email body
func (s *WeeklyReportService) buildEmailBody(reportData *WeeklyReportData) string {
	html := `
<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<style>
		body { font-family: Arial, sans-serif; color: #333; }
		.container { max-width: 600px; margin: 0 auto; padding: 20px; }
		.header { background-color: #f5f5f5; padding: 20px; border-radius: 5px; margin-bottom: 20px; }
		.metric-card { background-color: #f9f9f9; padding: 15px; margin: 10px 0; border-left: 4px solid #007bff; }
		.metric-value { font-size: 24px; font-weight: bold; color: #007bff; }
		.metric-label { font-size: 14px; color: #666; }
		.section { margin: 20px 0; }
		.section-title { font-size: 16px; font-weight: bold; color: #333; margin-bottom: 10px; border-bottom: 2px solid #007bff; padding-bottom: 5px; }
		.item { padding: 8px 0; color: #555; }
		.footer { margin-top: 30px; padding-top: 20px; border-top: 1px solid #ddd; font-size: 12px; color: #999; }
	</style>
</head>
<body>
	<div class="container">
		<div class="header">
			<h2>前台分析周报</h2>
			<p>周期: %s 至 %s</p>
		</div>

		<div class="section">
			<div class="section-title">📊 关键指标</div>
			<div class="metric-card">
				<div class="metric-label">总交互数</div>
				<div class="metric-value">%d</div>
			</div>
			<div class="metric-card">
				<div class="metric-label">预约数</div>
				<div class="metric-value">%d</div>
			</div>
			<div class="metric-card">
				<div class="metric-label">Walk-in 数</div>
				<div class="metric-value">%d</div>
			</div>
			<div class="metric-card">
				<div class="metric-label">风险事件</div>
				<div class="metric-value">%d</div>
			</div>
		</div>

		<div class="section">
			<div class="section-title">💡 关键洞察</div>
			%s
		</div>

		<div class="section">
			<div class="section-title">⚠️ 问题汇总</div>
			%s
		</div>

		<div class="section">
			<div class="section-title">✅ 改进建议</div>
			%s
		</div>

		<div class="section">
			<div class="section-title">🔝 高频问题</div>
			%s
		</div>

		<div class="section">
			<div class="section-title">🏥 竞品提及</div>
			%s
		</div>

		<div class="footer">
			<p>本报告由前台分析系统自动生成，生成时间: %s</p>
		</div>
	</div>
</body>
</html>
`

	insightsHTML := s.buildListHTML(reportData.KeyInsights)
	issuesHTML := s.buildListHTML(reportData.IssueSummary)
	suggestionsHTML := s.buildListHTML(reportData.ImprovementSuggestions)
	questionsHTML := s.buildListHTML(reportData.TopQuestions)
	competitorsHTML := s.buildListHTML(reportData.CompetitorMentions)

	return fmt.Sprintf(
		html,
		reportData.WeekStartDate,
		reportData.WeekEndDate,
		reportData.TotalInteractions,
		reportData.TotalAppointments,
		reportData.TotalWalkIns,
		reportData.RiskEventCount,
		insightsHTML,
		issuesHTML,
		suggestionsHTML,
		questionsHTML,
		competitorsHTML,
		reportData.GeneratedAt.Format("2006-01-02 15:04:05"),
	)
}

// buildListHTML builds HTML list from string array
func (s *WeeklyReportService) buildListHTML(items []string) string {
	if len(items) == 0 {
		return `<div class="item">暂无数据</div>`
	}

	var html strings.Builder
	for _, item := range items {
		html.WriteString(fmt.Sprintf(`<div class="item">• %s</div>`, item))
	}
	return html.String()
}

// calculateTrends calculates trend analysis from daily metrics
func (s *WeeklyReportService) calculateTrends(dailyMetrics []map[string]interface{}) map[string]interface{} {
	trends := make(map[string]interface{})

	if len(dailyMetrics) == 0 {
		return trends
	}

	// Calculate average daily interactions
	var totalInteractions float64
	for _, daily := range dailyMetrics {
		totalInteractions += toFloat64Value(daily["total_interactions"])
	}
	trends["avg_daily_interactions"] = totalInteractions / float64(len(dailyMetrics))

	// Calculate trend direction (up/down/stable)
	if len(dailyMetrics) >= 2 {
		first := dailyMetrics[0]
		last := dailyMetrics[len(dailyMetrics)-1]

		firstInteractions := toFloat64Value(first["total_interactions"])
		lastInteractions := toFloat64Value(last["total_interactions"])

		if lastInteractions > firstInteractions*1.1 {
			trends["direction"] = "up"
		} else if lastInteractions < firstInteractions*0.9 {
			trends["direction"] = "down"
		} else {
			trends["direction"] = "stable"
		}
	}

	return trends
}

func toFloat64Value(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case uint:
		return float64(n)
	case uint64:
		return float64(n)
	case uint32:
		return float64(n)
	default:
		return 0
	}
}

// generateKeyInsights generates key insights from report data
func (s *WeeklyReportService) generateKeyInsights(reportData *WeeklyReportData) []string {
	insights := make([]string, 0)

	// Insight 1: Overall performance
	avgDaily := reportData.TotalInteractions / 7
	insights = append(insights, fmt.Sprintf("本周平均每日交互数为 %d，总计 %d 次交互", avgDaily, reportData.TotalInteractions))

	// Insight 2: Appointment conversion
	if reportData.TotalInteractions > 0 {
		conversionRate := float64(reportData.TotalAppointments) / float64(reportData.TotalInteractions) * 100
		insights = append(insights, fmt.Sprintf("预约转化率为 %.1f%%，共成功预约 %d 次", conversionRate, reportData.TotalAppointments))
	}

	// Insight 3: Risk events
	if reportData.RiskEventCount > 0 {
		insights = append(insights, fmt.Sprintf("本周检测到 %d 起风险事件，需要重点关注", reportData.RiskEventCount))
	}

	return insights
}

// generateIssueSummary generates issue summary
func (s *WeeklyReportService) generateIssueSummary(reportData *WeeklyReportData) []string {
	issues := make([]string, 0)

	if reportData.RiskEventCount > 5 {
		issues = append(issues, "风险事件频繁，建议加强员工培训")
	}

	if len(reportData.TopQuestions) > 0 {
		issues = append(issues, fmt.Sprintf("客户高频问题未得到有效解答，建议更新话术库"))
	}

	if reportData.TotalWalkIns > reportData.TotalAppointments {
		issues = append(issues, "Walk-in 客户比例较高，预约转化需要改进")
	}

	return issues
}

// generateSuggestions generates improvement suggestions
func (s *WeeklyReportService) generateSuggestions(reportData *WeeklyReportData) []string {
	suggestions := make([]string, 0)

	suggestions = append(suggestions, "根据高频问题更新知识库和话术库")
	suggestions = append(suggestions, "针对风险事件进行员工一对一辅导")
	suggestions = append(suggestions, "分析竞品提及情况，制定差异化竞争策略")

	return suggestions
}

// appendUnique appends item to slice if not already present
func appendUnique(slice []string, item string) []string {
	for _, v := range slice {
		if v == item {
			return slice
		}
	}
	return append(slice, item)
}
