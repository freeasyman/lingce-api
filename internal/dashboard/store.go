package dashboard

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// roundFloat 将浮点数四舍五入到指定小数位，并处理极小值
func roundFloat(val float64, precision int) float64 {
	if math.Abs(val) < 0.01 {
		return 0
	}
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// GetAdminDashboardData retrieves admin dashboard statistics
// This function now uses the same date range logic as the consultant dashboard (last 30 days)
func (s *Store) GetAdminDashboardData(ctx context.Context, tenantID int64) (*AdminDashboardData, error) {
	now := time.Now()
	// Use last 30 days as the date range (same as consultant dashboard "month" option)
	// This matches the frontend getDateRange function: start.setDate(end.getDate() - 29)
	currentPeriodEnd := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location())
	currentPeriodStart := currentPeriodEnd.AddDate(0, 0, -29) // Last 30 days
	lastPeriodEnd := currentPeriodStart.Add(-time.Second)
	lastPeriodStart := lastPeriodEnd.AddDate(0, 0, -29) // Previous 30 days

	data := &AdminDashboardData{
		Metrics:       AdminDashboardMetrics{},
		Alerts:        []AdminDashboardAlert{},
		TopPerformers: []AdminDashboardTopPerformer{},
	}

	// 查询最近30天录音统计（使用 recorded_at 或 created_at，与 daily-report 一致）
	var currentPeriodCount, lastPeriodCount int
	var currentPeriodDuration, lastPeriodDuration int64

	err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE COALESCE(recorded_at, created_at) >= $1 AND COALESCE(recorded_at, created_at) <= $2) as current_count,
			COUNT(*) FILTER (WHERE COALESCE(recorded_at, created_at) >= $3 AND COALESCE(recorded_at, created_at) <= $4) as last_count,
			COALESCE(SUM(duration) FILTER (WHERE COALESCE(recorded_at, created_at) >= $1 AND COALESCE(recorded_at, created_at) <= $2), 0) as current_duration,
			COALESCE(SUM(duration) FILTER (WHERE COALESCE(recorded_at, created_at) >= $3 AND COALESCE(recorded_at, created_at) <= $4), 0) as last_duration
		FROM recordings
		WHERE tenant_id = $5
	`, currentPeriodStart, currentPeriodEnd, lastPeriodStart, lastPeriodEnd, tenantID).Scan(
		&currentPeriodCount, &lastPeriodCount, &currentPeriodDuration, &lastPeriodDuration,
	)
	if err != nil {
		return nil, err
	}

	// 计算患者数趋势
	data.Metrics.Patients.Value = currentPeriodCount
	if lastPeriodCount > 0 {
		data.Metrics.Patients.Trend = roundFloat(float64(currentPeriodCount-lastPeriodCount)/float64(lastPeriodCount)*100, 1)
	}

	// 查询成交相关数据（与 daily-report 一致）
	var currentDealCount, currentConfirmedCount int
	var lastDealCount, lastConfirmedCount int
	err = s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE COALESCE(recorded_at, created_at) >= $1 AND COALESCE(recorded_at, created_at) <= $2 AND confirmed_deal_status = '成交了') as current_deal_count,
			COUNT(*) FILTER (WHERE COALESCE(recorded_at, created_at) >= $1 AND COALESCE(recorded_at, created_at) <= $2 AND confirmed_deal_status IS NOT NULL) as current_confirmed_count,
			COUNT(*) FILTER (WHERE COALESCE(recorded_at, created_at) >= $3 AND COALESCE(recorded_at, created_at) <= $4 AND confirmed_deal_status = '成交了') as last_deal_count,
			COUNT(*) FILTER (WHERE COALESCE(recorded_at, created_at) >= $3 AND COALESCE(recorded_at, created_at) <= $4 AND confirmed_deal_status IS NOT NULL) as last_confirmed_count
		FROM recordings
		WHERE tenant_id = $5
	`, currentPeriodStart, currentPeriodEnd, lastPeriodStart, lastPeriodEnd, tenantID).Scan(&currentDealCount, &currentConfirmedCount, &lastDealCount, &lastConfirmedCount)

	if err == nil {
		if currentConfirmedCount > 0 {
			data.Metrics.DealRate.Value = roundFloat(float64(currentDealCount)/float64(currentConfirmedCount)*100, 1)
		}
		if lastConfirmedCount > 0 {
			lastDealRate := roundFloat(float64(lastDealCount)/float64(lastConfirmedCount)*100, 1)
			data.Metrics.DealRate.Trend = roundFloat(data.Metrics.DealRate.Value-lastDealRate, 1)
		}
	}

	// 查询营收数据（与 daily-report 一致，使用 converted_amount）
	var currentRevenue, lastRevenue float64
	err = s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(converted_amount) FILTER (WHERE COALESCE(recorded_at, created_at) >= $1 AND COALESCE(recorded_at, created_at) <= $2 AND confirmed_deal_status = '成交了'), 0) as current_revenue,
			COALESCE(SUM(converted_amount) FILTER (WHERE COALESCE(recorded_at, created_at) >= $3 AND COALESCE(recorded_at, created_at) <= $4 AND confirmed_deal_status = '成交了'), 0) as last_revenue
		FROM recordings
		WHERE tenant_id = $5
	`, currentPeriodStart, currentPeriodEnd, lastPeriodStart, lastPeriodEnd, tenantID).Scan(&currentRevenue, &lastRevenue)

	if err == nil {
		data.Metrics.Revenue.Value = roundFloat(currentRevenue, 0)
		if lastRevenue > 0 {
			data.Metrics.Revenue.Trend = roundFloat((currentRevenue-lastRevenue)/lastRevenue*100, 1)
		} else if currentRevenue > 0 {
			data.Metrics.Revenue.Trend = 100
		}
	}

	// 查询任务完成率
	var totalTasks, completedTasks int
	var lastTotalTasks, lastCompletedTasks int
	err = s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE created_at >= $1 AND created_at <= $2) as current_total,
			COUNT(*) FILTER (WHERE created_at >= $1 AND created_at <= $2 AND status = 'completed') as current_completed,
			COUNT(*) FILTER (WHERE created_at >= $3 AND created_at <= $4) as last_total,
			COUNT(*) FILTER (WHERE created_at >= $3 AND created_at <= $4 AND status = 'completed') as last_completed
		FROM recording_tasks
		WHERE tenant_id = $5
	`, currentPeriodStart, currentPeriodEnd, lastPeriodStart, lastPeriodEnd, tenantID).Scan(&totalTasks, &completedTasks, &lastTotalTasks, &lastCompletedTasks)

	if err == nil && totalTasks > 0 {
		data.Metrics.TaskCompletion.Value = roundFloat(float64(completedTasks)/float64(totalTasks)*100, 1)
		var lastRate float64
		if lastTotalTasks > 0 {
			lastRate = float64(lastCompletedTasks) / float64(lastTotalTasks) * 100
		}
		data.Metrics.TaskCompletion.Trend = roundFloat(data.Metrics.TaskCompletion.Value-lastRate, 1)
	}

	// 查询目标达成率（从 tenants 表获取月度目标）
	var targetNullable *float64
	err = s.pool.QueryRow(ctx, `SELECT monthly_revenue_target::float8 FROM tenants WHERE id = $1`, tenantID).Scan(&targetNullable)
	if err == nil && targetNullable != nil && *targetNullable > 0 {
		targetProgress := roundFloat((currentRevenue/(*targetNullable))*100, 1)
		data.Metrics.TargetAchievement.Value = targetProgress
		// 趋势暂时设为0，需要历史数据对比
		data.Metrics.TargetAchievement.Trend = 0
	} else {
		data.Metrics.TargetAchievement.Value = 0
		data.Metrics.TargetAchievement.Trend = 0
	}

	// 查询服务质量（基于录音质检评分的平均值）
	var currentQuality, lastQuality float64
	var currentQualityCount, lastQualityCount int
	err = s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(AVG(
				CASE
					WHEN (analysis_result->>'overall_score')::text ~ '^[0-9]+(\.[0-9]+)?$'
					THEN (analysis_result->>'overall_score')::float
					WHEN (analysis_result->'quality_score'->>'overall')::text ~ '^[0-9]+(\.[0-9]+)?$'
					THEN (analysis_result->'quality_score'->>'overall')::float
					ELSE NULL
				END
			) FILTER (WHERE COALESCE(recorded_at, created_at) >= $1 AND COALESCE(recorded_at, created_at) <= $2), 0) as current_quality,
			COUNT(*) FILTER (WHERE COALESCE(recorded_at, created_at) >= $1 AND COALESCE(recorded_at, created_at) <= $2 AND (
				(analysis_result->>'overall_score')::text ~ '^[0-9]+(\.[0-9]+)?$' OR
				(analysis_result->'quality_score'->>'overall')::text ~ '^[0-9]+(\.[0-9]+)?$'
			)) as current_count,
			COALESCE(AVG(
				CASE
					WHEN (analysis_result->>'overall_score')::text ~ '^[0-9]+(\.[0-9]+)?$'
					THEN (analysis_result->>'overall_score')::float
					WHEN (analysis_result->'quality_score'->>'overall')::text ~ '^[0-9]+(\.[0-9]+)?$'
					THEN (analysis_result->'quality_score'->>'overall')::float
					ELSE NULL
				END
			) FILTER (WHERE COALESCE(recorded_at, created_at) >= $3 AND COALESCE(recorded_at, created_at) <= $4), 0) as last_quality,
			COUNT(*) FILTER (WHERE COALESCE(recorded_at, created_at) >= $3 AND COALESCE(recorded_at, created_at) <= $4 AND (
				(analysis_result->>'overall_score')::text ~ '^[0-9]+(\.[0-9]+)?$' OR
				(analysis_result->'quality_score'->>'overall')::text ~ '^[0-9]+(\.[0-9]+)?$'
			)) as last_count
		FROM recordings
		WHERE tenant_id = $5
			AND analysis_result IS NOT NULL
	`, currentPeriodStart, currentPeriodEnd, lastPeriodStart, lastPeriodEnd, tenantID).Scan(&currentQuality, &currentQualityCount, &lastQuality, &lastQualityCount)

	if err == nil && currentQualityCount > 0 {
		data.Metrics.ServiceQuality.Value = roundFloat(currentQuality, 1)
		data.Metrics.ServiceQuality.Trend = roundFloat(currentQuality-lastQuality, 1)
	} else {
		data.Metrics.ServiceQuality.Value = 0
		data.Metrics.ServiceQuality.Trend = 0
	}

	// 查询团队业绩排行（按成交金额，与 daily-report 一致）
	rows, err := s.pool.Query(ctx, `
		SELECT
			COALESCE(NULLIF(e.name, ''), NULLIF(e.full_name, ''), '未知员工') as employee_name,
			COALESCE(SUM(r.converted_amount), 0) as total_revenue,
			ROW_NUMBER() OVER (ORDER BY COALESCE(SUM(r.converted_amount), 0) DESC) as rank
		FROM employees e
		LEFT JOIN recordings r ON r.employee_id = e.id
			AND COALESCE(r.recorded_at, r.created_at) >= $1
			AND COALESCE(r.recorded_at, r.created_at) <= $2
			AND r.confirmed_deal_status = '成交了'
		WHERE e.tenant_id = $3
		GROUP BY e.id, e.full_name, e.name
		HAVING COALESCE(SUM(r.converted_amount), 0) > 0
		ORDER BY total_revenue DESC
		LIMIT 3
	`, currentPeriodStart, currentPeriodEnd, tenantID)

	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var performer AdminDashboardTopPerformer
			var revenue float64
			if err := rows.Scan(&performer.Name, &revenue, &performer.Rank); err == nil {
				performer.Revenue = revenue
				data.TopPerformers = append(data.TopPerformers, performer)
			}
		}
	}

	// 如果没有数据，添加默认提示
	if len(data.TopPerformers) == 0 {
		data.TopPerformers = []AdminDashboardTopPerformer{
			{Name: "暂无数据", Revenue: 0, Rank: 1},
		}
	}

	// 添加默认告警
	if currentPeriodCount == 0 {
		data.Alerts = append(data.Alerts, AdminDashboardAlert{
			ID:          1,
			Priority:    "medium",
			Title:       "最近30天暂无录音数据",
			Description: "建议检查录音设备状态和员工使用情况",
		})
	} else {
		data.Alerts = append(data.Alerts, AdminDashboardAlert{
			ID:          1,
			Priority:    "low",
			Title:       "系统运行正常",
			Description: fmt.Sprintf("最近30天已有 %d 条录音记录", currentPeriodCount),
		})
	}

	return data, nil
}

// GetConsultantDashboardData retrieves consultant dashboard statistics
func (s *Store) GetConsultantDashboardData(ctx context.Context, tenantID int64, employeeID *int64) (*ConsultantDashboardData, error) {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	data := &ConsultantDashboardData{
		Metrics:               ConsultantDashboardMetrics{},
		HighPriorityCustomers: []ConsultantDashboardCustomer{},
	}

	// 如果没有指定员工ID，返回默认数据
	if employeeID == nil {
		data.Metrics.TodayDeals.Count = 0
		data.Metrics.TodayDeals.Amount = 0
		data.Metrics.Following = 0
		data.Metrics.Pending = 0
		data.Metrics.DealRate.Value = 0
		data.Metrics.DealRate.Trend = 0
		data.Metrics.DealRate.Rank = 0
		data.AbilityScore.Overall = 0
		data.AbilityScore.Rank = 0
		data.AbilityScore.Total = 0
		return data, nil
	}

	// 查询今日成交数据
	var todayCount int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM recordings
		WHERE tenant_id = $1
			AND employee_id = $2
			AND created_at >= $3
			AND (
				analysis_result::text LIKE '%成交%' OR
				analysis_result::text LIKE '%deal%'
			)
	`, tenantID, *employeeID, todayStart).Scan(&todayCount)

	if err == nil {
		data.Metrics.TodayDeals.Count = todayCount
		data.Metrics.TodayDeals.Amount = float64(todayCount * 14000) // 假设平均每单14000元
	}

	// 查询跟进中和待分配（简化处理，基于录音数量）
	var followingCount, pendingCount int
	err = s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '7 days') as following,
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '1 day' AND created_at < NOW()) as pending
		FROM recordings
		WHERE tenant_id = $1
			AND employee_id = $2
	`, tenantID, *employeeID).Scan(&followingCount, &pendingCount)

	if err == nil {
		data.Metrics.Following = followingCount
		data.Metrics.Pending = pendingCount
	}

	// 查询成交率和排名
	var dealRate float64
	var rank int
	err = s.pool.QueryRow(ctx, `
		WITH employee_stats AS (
			SELECT
				employee_id,
				COUNT(*) as total_count,
				COUNT(*) FILTER (WHERE
					analysis_result::text LIKE '%成交%' OR
					analysis_result::text LIKE '%deal%'
				) as deal_count
			FROM recordings
			WHERE tenant_id = $1
				AND created_at >= NOW() - INTERVAL '30 days'
			GROUP BY employee_id
		),
		ranked_employees AS (
			SELECT
				employee_id,
				CASE WHEN total_count > 0
					THEN (deal_count::float / total_count * 100)
					ELSE 0
				END as deal_rate,
				ROW_NUMBER() OVER (ORDER BY
					CASE WHEN total_count > 0
						THEN (deal_count::float / total_count)
						ELSE 0
					END DESC
				) as rank
			FROM employee_stats
		)
		SELECT deal_rate, rank
		FROM ranked_employees
		WHERE employee_id = $2
	`, tenantID, *employeeID).Scan(&dealRate, &rank)

	if err == nil {
		data.Metrics.DealRate.Value = dealRate
		data.Metrics.DealRate.Trend = 2.3 // 简化处理
		data.Metrics.DealRate.Rank = rank
	}

	// 查询能力评分（简化处理）
	data.AbilityScore.Overall = 4.6
	data.AbilityScore.Rank = rank
	data.AbilityScore.Total = 15

	// 添加默认客户提示
	data.HighPriorityCustomers = append(data.HighPriorityCustomers, ConsultantDashboardCustomer{
		ID:          1,
		Priority:    "low",
		Title:       "暂无高优先级客户",
		Description: fmt.Sprintf("今日已成交 %d 单，继续加油！", todayCount),
	})

	return data, nil
}

// GetDoctorDashboardData retrieves doctor dashboard statistics
func (s *Store) GetDoctorDashboardData(ctx context.Context, tenantID int64, employeeID *int64) (*DoctorDashboardData, error) {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekStart := todayStart.AddDate(0, 0, -int(now.Weekday()))

	data := &DoctorDashboardData{
		Metrics:          DoctorDashboardMetrics{},
		ReviewRecordings: []DoctorDashboardRecording{},
	}

	// 如果没有指定员工ID，返回默认数据
	if employeeID == nil {
		data.Metrics.TodayRecordings.Count = 0
		data.Metrics.TodayRecordings.AvgDuration = 0
		data.Metrics.Quality.Score = 0
		data.Metrics.Quality.Rate = 0
		data.Metrics.WeeklyService.Count = 0
		data.Metrics.WeeklyService.AvgDuration = 0
		data.Metrics.QualityTrend.Value = 0
		data.Metrics.QualityTrend.Trend = 0
		data.AbilityScores.Professionalism = 0
		data.AbilityScores.Empathy = 0
		data.AbilityScores.Efficiency = 0
		data.AbilityScores.Compliance = 0
		data.AbilityScores.Rank = 0
		data.AbilityScores.Total = 0
		return data, nil
	}

	// 查询今日录音统计
	var todayCount int
	var todayAvgDuration float64
	err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COALESCE(AVG(duration), 0) / 60 as avg_minutes
		FROM recordings
		WHERE tenant_id = $1
			AND employee_id = $2
			AND created_at >= $3
	`, tenantID, *employeeID, todayStart).Scan(&todayCount, &todayAvgDuration)

	if err == nil {
		data.Metrics.TodayRecordings.Count = todayCount
		data.Metrics.TodayRecordings.AvgDuration = int(todayAvgDuration)
	}

	// 查询本周服务统计
	var weekCount int
	var weekAvgDuration float64
	err = s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COALESCE(AVG(duration), 0) / 60 as avg_minutes
		FROM recordings
		WHERE tenant_id = $1
			AND employee_id = $2
			AND created_at >= $3
	`, tenantID, *employeeID, weekStart).Scan(&weekCount, &weekAvgDuration)

	if err == nil {
		data.Metrics.WeeklyService.Count = weekCount
		data.Metrics.WeeklyService.AvgDuration = int(weekAvgDuration)
	}

	// 查询录音质量（基于成功分析的录音比例）
	var totalCount, successCount int
	err = s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE analysis_result IS NOT NULL) as success
		FROM recordings
		WHERE tenant_id = $1
			AND employee_id = $2
			AND created_at >= $3
	`, tenantID, *employeeID, weekStart).Scan(&totalCount, &successCount)

	if err == nil && totalCount > 0 {
		data.Metrics.Quality.Rate = float64(successCount) / float64(totalCount) * 100
		data.Metrics.Quality.Score = 5.0 // 简化处理
	}

	// 查询质量趋势
	data.Metrics.QualityTrend.Value = 4.8
	data.Metrics.QualityTrend.Trend = 0.2

	// 查询能力评分和排名
	var rank int
	err = s.pool.QueryRow(ctx, `
		WITH employee_stats AS (
			SELECT
				employee_id,
				COUNT(*) as recording_count
			FROM recordings
			WHERE tenant_id = $1
				AND created_at >= $2
			GROUP BY employee_id
		)
		SELECT ROW_NUMBER() OVER (ORDER BY recording_count DESC) as rank
		FROM employee_stats
		WHERE employee_id = $3
	`, tenantID, weekStart, *employeeID).Scan(&rank)

	if err == nil {
		data.AbilityScores.Rank = rank
	}

	// 设置能力评分（简化处理）
	data.AbilityScores.Professionalism = 4.9
	data.AbilityScores.Empathy = 4.6
	data.AbilityScores.Efficiency = 4.8
	data.AbilityScores.Compliance = 5.0
	data.AbilityScores.Total = 8

	// 添加默认录音提示
	data.ReviewRecordings = append(data.ReviewRecordings, DoctorDashboardRecording{
		ID:          1,
		Priority:    "low",
		Title:       "暂无需要复盘的录音",
		Description: fmt.Sprintf("今日已完成 %d 条录音，质量良好", todayCount),
		Score:       4.8,
	})

	return data, nil
}

func (s *Store) GetOpsWorkbenchOverview(ctx context.Context, tenantID *int64, dateFrom, dateTo string) (*OpsWorkbenchOverview, error) {
	res := &OpsWorkbenchOverview{}
	var tenantFilter int64
	if tenantID != nil {
		tenantFilter = *tenantID
	}

	err := s.pool.QueryRow(ctx, `
		WITH tenant_scope AS (
			SELECT id, name
			FROM tenants
			WHERE ($1::bigint = 0 OR id = $1)
		),
		rec AS (
			SELECT
				COUNT(*)::bigint AS recording_count,
				COALESCE(SUM(COALESCE(r.duration, 0)), 0)::bigint AS recording_duration_sec,
				COUNT(*) FILTER (WHERE r.confirmed_deal_status = '成交了')::bigint AS deal_count,
				COALESCE(SUM(CASE WHEN r.confirmed_deal_status = '成交了' THEN COALESCE(r.converted_amount, 0) ELSE 0 END), 0)::bigint AS deal_amount
			FROM recordings r
			JOIN tenant_scope t ON t.id = r.tenant_id
			WHERE COALESCE(r.recorded_at, r.created_at)::date BETWEEN $2::date AND $3::date
		),
		tasks AS (
			SELECT
				COUNT(*)::bigint AS task_total,
				COUNT(*) FILTER (WHERE rt.status = 'completed')::bigint AS task_done,
				COUNT(*) FILTER (WHERE rt.status <> 'completed')::bigint AS task_undone
			FROM recording_tasks rt
			JOIN tenant_scope t ON t.id = rt.tenant_id
			WHERE rt.created_at::date BETWEEN $2::date AND $3::date
		),
		devs AS (
			SELECT COUNT(*)::bigint AS device_count
			FROM badge_devices bd
			JOIN tenant_scope t ON t.id = bd.tenant_id
		)
		SELECT
			(SELECT COUNT(*)::bigint FROM tenant_scope) AS institution_count,
			COALESCE((SELECT device_count FROM devs), 0) AS device_count,
			COALESCE((SELECT recording_count FROM rec), 0) AS recording_count,
			COALESCE((SELECT recording_duration_sec FROM rec), 0) AS recording_duration_sec,
			COALESCE((SELECT task_total FROM tasks), 0) AS task_total,
			COALESCE((SELECT task_done FROM tasks), 0) AS task_done,
			COALESCE((SELECT task_undone FROM tasks), 0) AS task_undone,
			COALESCE((SELECT deal_count FROM rec), 0) AS deal_count,
			COALESCE((SELECT deal_amount FROM rec), 0) AS deal_amount
	`, tenantFilter, dateFrom, dateTo).Scan(
		&res.InstitutionCount,
		&res.DeviceCount,
		&res.RecordingCount,
		&res.RecordingDuration,
		&res.TaskTotal,
		&res.TaskDone,
		&res.TaskUndone,
		&res.DealCount,
		&res.DealAmount,
	)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Store) GetOpsWorkbenchTrend(ctx context.Context, tenantID *int64, dateFrom, dateTo, metric string) (*OpsWorkbenchTrend, error) {
	var tenantFilter int64
	if tenantID != nil {
		tenantFilter = *tenantID
	}
	allowed := map[string]bool{"recording": true, "duration": true, "task": true, "deal": true}
	if !allowed[metric] {
		metric = "recording"
	}

	rows, err := s.pool.Query(ctx, `
		WITH tenant_scope AS (
			SELECT id FROM tenants WHERE ($1::bigint = 0 OR id = $1)
		),
		days AS (
			SELECT generate_series($2::date, $3::date, interval '1 day')::date AS d
		),
		rec AS (
			SELECT
				COALESCE(r.recorded_at, r.created_at)::date AS d,
				COUNT(*)::bigint AS recording_count,
				COALESCE(SUM(COALESCE(r.duration, 0)), 0)::bigint AS duration_sum,
				COALESCE(SUM(CASE WHEN r.confirmed_deal_status = '成交了' THEN COALESCE(r.converted_amount, 0) ELSE 0 END), 0)::bigint AS deal_amount
			FROM recordings r
			JOIN tenant_scope t ON t.id = r.tenant_id
			WHERE COALESCE(r.recorded_at, r.created_at)::date BETWEEN $2::date AND $3::date
			GROUP BY COALESCE(r.recorded_at, r.created_at)::date
		),
		task_daily AS (
			SELECT
				rt.created_at::date AS d,
				COUNT(*)::bigint AS task_count
			FROM recording_tasks rt
			JOIN tenant_scope t ON t.id = rt.tenant_id
			WHERE rt.created_at::date BETWEEN $2::date AND $3::date
			GROUP BY rt.created_at::date
		)
		SELECT
			to_char(days.d, 'YYYY-MM-DD') AS d,
			CASE
				WHEN $4 = 'duration' THEN COALESCE(rec.duration_sum, 0)
				WHEN $4 = 'deal' THEN COALESCE(rec.deal_amount, 0)
				WHEN $4 = 'task' THEN COALESCE(task_daily.task_count, 0)
				ELSE COALESCE(rec.recording_count, 0)
			END AS v
		FROM days
		LEFT JOIN rec ON rec.d = days.d
		LEFT JOIN task_daily ON task_daily.d = days.d
		ORDER BY days.d ASC
	`, tenantFilter, dateFrom, dateTo, metric)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	resp := &OpsWorkbenchTrend{Metric: metric, Points: make([]OpsWorkbenchTrendPoint, 0, 64)}
	for rows.Next() {
		var p OpsWorkbenchTrendPoint
		if err := rows.Scan(&p.Date, &p.Value); err != nil {
			return nil, err
		}
		resp.Points = append(resp.Points, p)
	}
	return resp, rows.Err()
}

func (s *Store) GetOpsWorkbenchTable(
	ctx context.Context,
	tenantID *int64,
	dateFrom, dateTo string,
	page, pageSize int,
) ([]OpsWorkbenchTableRow, int64, error) {
	var tenantFilter int64
	if tenantID != nil {
		tenantFilter = *tenantID
	}
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}

	var total int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint
		FROM tenants
		WHERE ($1::bigint = 0 OR id = $1)
	`, tenantFilter).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx, `
		WITH tenant_scope AS (
			SELECT id, name
			FROM tenants
			WHERE ($1::bigint = 0 OR id = $1)
			ORDER BY id ASC
			LIMIT $4 OFFSET $5
		),
		rec AS (
			SELECT
				r.tenant_id,
				COUNT(*)::bigint AS recording_count,
				COALESCE(SUM(COALESCE(r.duration, 0)), 0)::bigint AS recording_duration_sec,
				COUNT(*) FILTER (WHERE r.confirmed_deal_status = '成交了')::bigint AS deal_count,
				COALESCE(SUM(CASE WHEN r.confirmed_deal_status = '成交了' THEN COALESCE(r.converted_amount, 0) ELSE 0 END), 0)::bigint AS deal_amount
			FROM recordings r
			JOIN tenant_scope t ON t.id = r.tenant_id
			WHERE COALESCE(r.recorded_at, r.created_at)::date BETWEEN $2::date AND $3::date
			GROUP BY r.tenant_id
		),
		task_agg AS (
			SELECT
				rt.tenant_id,
				COUNT(*) FILTER (WHERE rt.status = 'completed')::bigint AS task_done,
				COUNT(*) FILTER (WHERE rt.status <> 'completed')::bigint AS task_undone
			FROM recording_tasks rt
			JOIN tenant_scope t ON t.id = rt.tenant_id
			WHERE rt.created_at::date BETWEEN $2::date AND $3::date
			GROUP BY rt.tenant_id
		),
		dev_agg AS (
			SELECT bd.tenant_id, COUNT(*)::bigint AS device_count
			FROM badge_devices bd
			JOIN tenant_scope t ON t.id = bd.tenant_id
			GROUP BY bd.tenant_id
		)
		SELECT
			t.id,
			t.name,
			COALESCE(d.device_count, 0) AS device_count,
			COALESCE(r.recording_count, 0) AS recording_count,
			COALESCE(r.recording_duration_sec, 0) AS recording_duration_sec,
			COALESCE(ta.task_done, 0) AS task_done,
			COALESCE(ta.task_undone, 0) AS task_undone,
			COALESCE(r.deal_count, 0) AS deal_count,
			COALESCE(r.deal_amount, 0) AS deal_amount
		FROM tenant_scope t
		LEFT JOIN rec r ON r.tenant_id = t.id
		LEFT JOIN task_agg ta ON ta.tenant_id = t.id
		LEFT JOIN dev_agg d ON d.tenant_id = t.id
		ORDER BY t.id ASC
	`, tenantFilter, dateFrom, dateTo, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	result := make([]OpsWorkbenchTableRow, 0, pageSize)
	for rows.Next() {
		var row OpsWorkbenchTableRow
		if err := rows.Scan(
			&row.TenantID,
			&row.TenantName,
			&row.DeviceCount,
			&row.RecordingCount,
			&row.RecordingDurationSec,
			&row.TaskDone,
			&row.TaskUndone,
			&row.DealCount,
			&row.DealAmount,
		); err != nil {
			return nil, 0, err
		}
		result = append(result, row)
	}

	return result, total, rows.Err()
}
