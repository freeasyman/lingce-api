package recording

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// ScheduledTaskManager manages scheduled tasks for weekly reports
type ScheduledTaskManager struct {
	weeklyReportService *WeeklyReportService
	store               *Store
	logger              *slog.Logger
	ticker              *time.Ticker
	done                chan bool
}

// NewScheduledTaskManager creates a new scheduled task manager
func NewScheduledTaskManager(
	weeklyReportService *WeeklyReportService,
	store *Store,
	logger *slog.Logger,
) *ScheduledTaskManager {
	return &ScheduledTaskManager{
		weeklyReportService: weeklyReportService,
		store:               store,
		logger:              logger,
		done:                make(chan bool),
	}
}

// Start starts the scheduled task manager
// It runs weekly report generation every Monday at 9:00 AM
func (m *ScheduledTaskManager) Start(ctx context.Context) {
	go func() {
		for {
			// Calculate time until next Monday 9:00 AM
			now := time.Now()
			nextRun := m.getNextMondayMorning(now)
			duration := nextRun.Sub(now)

			m.logger.Info("next weekly report generation scheduled", "time", nextRun)

			// Wait until next run time
			select {
			case <-time.After(duration):
				m.logger.Info("starting weekly report generation")
				m.generateAndSendWeeklyReports(ctx)
			case <-m.done:
				m.logger.Info("scheduled task manager stopped")
				return
			}
		}
	}()
}

// Stop stops the scheduled task manager
func (m *ScheduledTaskManager) Stop() {
	m.done <- true
}

// getNextMondayMorning calculates the next Monday at 9:00 AM
func (m *ScheduledTaskManager) getNextMondayMorning(now time.Time) time.Time {
	// Monday is 1 in Go's time.Weekday
	daysUntilMonday := (1 - int(now.Weekday()) + 7) % 7
	if daysUntilMonday == 0 && now.Hour() >= 9 {
		daysUntilMonday = 7
	}

	nextMonday := now.AddDate(0, 0, daysUntilMonday)
	return time.Date(nextMonday.Year(), nextMonday.Month(), nextMonday.Day(), 9, 0, 0, 0, nextMonday.Location())
}

// generateAndSendWeeklyReports generates and sends weekly reports for all tenants
func (m *ScheduledTaskManager) generateAndSendWeeklyReports(ctx context.Context) {
	// Get all active tenants
	query := `
		SELECT DISTINCT tenant_id
		FROM frontdesk_shift_analyses
		WHERE created_at >= NOW() - INTERVAL '7 days'
		ORDER BY tenant_id
	`

	rows, err := m.store.pool.Query(ctx, query)
	if err != nil {
		m.logger.Error("failed to query tenants", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var tenantID int64
		if err := rows.Scan(&tenantID); err != nil {
			m.logger.Error("failed to scan tenant id", "error", err)
			continue
		}

		// Generate weekly report for this tenant
		weekEndDate := time.Now().AddDate(0, 0, -1) // Yesterday
		reportData, err := m.weeklyReportService.GenerateWeeklyReport(ctx, tenantID, weekEndDate)
		if err != nil {
			m.logger.Error("failed to generate weekly report", "error", err, "tenant_id", tenantID)
			continue
		}

		// Save report to database
		if err := m.saveWeeklyReport(ctx, reportData); err != nil {
			m.logger.Error("failed to save weekly report", "error", err, "tenant_id", tenantID)
			continue
		}

		// Get tenant admin email and send report
		adminEmail, err := m.getTenantAdminEmail(ctx, tenantID)
		if err != nil {
			m.logger.Error("failed to get tenant admin email", "error", err, "tenant_id", tenantID)
			continue
		}

		if adminEmail != "" {
			if err := m.weeklyReportService.SendWeeklyReport(ctx, reportData, adminEmail); err != nil {
				m.logger.Error("failed to send weekly report", "error", err, "tenant_id", tenantID)
				continue
			}
		}
	}

	m.logger.Info("weekly report generation completed")
}

// saveWeeklyReport saves the weekly report to database
func (m *ScheduledTaskManager) saveWeeklyReport(ctx context.Context, reportData *WeeklyReportData) error {
	analysis := map[string]interface{}{
		"summary":                 buildFrontdeskWeeklySummary(reportData),
		"total_interactions":      reportData.TotalInteractions,
		"total_appointments":      reportData.TotalAppointments,
		"total_walk_ins":          reportData.TotalWalkIns,
		"risk_event_count":        reportData.RiskEventCount,
		"top_questions":           reportData.TopQuestions,
		"competitor_mentions":     reportData.CompetitorMentions,
		"doctor_inquiries":        reportData.DoctorInquiries,
		"channel_feedback":        reportData.ChannelFeedback,
		"key_insights":            reportData.KeyInsights,
		"key_changes":             reportData.KeyInsights,
		"next_actions":            reportData.ImprovementSuggestions,
		"priority_actions":        reportData.ImprovementSuggestions,
		"question_structure":      reportData.TopQuestions,
		"response_quality":        buildWeeklyResponseQuality(reportData),
		"issue_summary":           reportData.IssueSummary,
		"staff_highlights":        buildWeeklyStaffHighlights(reportData),
		"staff_variances":         buildWeeklyStaffVariances(reportData),
		"staff_suggestions":       buildWeeklyStaffSuggestions(reportData),
		"improvement_suggestions": reportData.ImprovementSuggestions,
		"trend_analysis":          reportData.TrendAnalysis,
		"evidence_count":          len(reportData.TopQuestions),
		"evidence_items":          buildWeeklyEvidenceItems(reportData.TopQuestions),
		"action_owners":           buildWeeklyActionOwners(reportData),
		"action_effects":          buildWeeklyActionEffects(reportData),
		"week_start_date":         reportData.WeekStartDate,
		"week_end_date":           reportData.WeekEndDate,
		"generated_at":            reportData.GeneratedAt.Format(time.RFC3339),
		"status":                  "draft",
	}

	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO frontdesk_daily_reports (
			tenant_id, report_date,
			total_estimated_interactions, estimated_appointment_count, estimated_walkin_count,
			top_questions, competitor_mentions, doctor_inquiries, channel_feedback, risk_event_count, testimonial_materials, created_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
		ON CONFLICT (tenant_id, report_date) DO UPDATE
		SET total_estimated_interactions = EXCLUDED.total_estimated_interactions,
		    estimated_appointment_count = EXCLUDED.estimated_appointment_count,
		    estimated_walkin_count = EXCLUDED.estimated_walkin_count,
		    top_questions = EXCLUDED.top_questions,
		    competitor_mentions = EXCLUDED.competitor_mentions,
		    doctor_inquiries = EXCLUDED.doctor_inquiries,
		    channel_feedback = EXCLUDED.channel_feedback,
		    risk_event_count = EXCLUDED.risk_event_count,
		    testimonial_materials = EXCLUDED.testimonial_materials
	`

	_, err = m.store.pool.Exec(
		ctx,
		query,
		reportData.TenantID,
		reportData.WeekEndDate,
		reportData.TotalInteractions,
		reportData.TotalAppointments,
		reportData.TotalWalkIns,
		reportData.TopQuestions,
		reportData.CompetitorMentions,
		reportData.DoctorInquiries,
		reportData.ChannelFeedback,
		reportData.RiskEventCount,
		analysisJSON,
	)
	return err
}

// getTenantAdminEmail gets the admin email for a tenant
func (m *ScheduledTaskManager) getTenantAdminEmail(ctx context.Context, tenantID int64) (string, error) {
	// Query tenant admin email from tenants table
	// This assumes there's a tenants table with admin_email field
	query := `
		SELECT admin_email
		FROM tenants
		WHERE id = $1
		LIMIT 1
	`

	var adminEmail string
	err := m.store.pool.QueryRow(ctx, query, tenantID).Scan(&adminEmail)
	if err != nil {
		return "", err
	}

	return adminEmail, nil
}
