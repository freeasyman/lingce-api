package dashboard

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// GetAdminDashboardData retrieves admin dashboard statistics
func (s *Store) GetAdminDashboardData(ctx context.Context, tenantID int64) (*AdminDashboardData, error) {
	now := time.Now()
	currentMonth := now.Format("2006-01")
	lastMonth := now.AddDate(0, -1, 0).Format("2006-01")

	// TODO: 实现真实的数据库查询
	// 这里先返回模拟数据，后续可以根据实际数据库表结构实现

	data := &AdminDashboardData{
		Metrics: AdminDashboardMetrics{},
		Alerts: []AdminDashboardAlert{
			{
				ID:          1,
				Priority:    "high",
				Title:       "系统提示",
				Description: "暂无重要提醒",
			},
		},
		TopPerformers: []AdminDashboardTopPerformer{
			{Name: "员工A", Revenue: 123000, Rank: 1},
			{Name: "员工B", Revenue: 108000, Rank: 2},
			{Name: "员工C", Revenue: 92000, Rank: 3},
		},
	}

	// Set default metrics
	data.Metrics.Revenue.Value = 1285600
	data.Metrics.Revenue.Trend = 12.3
	data.Metrics.DealRate.Value = 68.2
	data.Metrics.DealRate.Trend = 3.1
	data.Metrics.Patients.Value = 342
	data.Metrics.Patients.Trend = 8.7
	data.Metrics.TaskCompletion.Value = 92.3
	data.Metrics.TaskCompletion.Trend = -2.1
	data.Metrics.TargetAchievement.Value = 85.6
	data.Metrics.TargetAchievement.Trend = 5.2
	data.Metrics.ServiceQuality.Value = 4.8
	data.Metrics.ServiceQuality.Trend = 0.3

	_ = currentMonth
	_ = lastMonth

	return data, nil
}

// GetConsultantDashboardData retrieves consultant dashboard statistics
func (s *Store) GetConsultantDashboardData(ctx context.Context, tenantID int64, employeeID *int64) (*ConsultantDashboardData, error) {
	// TODO: 实现真实的数据库查询
	// 这里先返回模拟数据

	data := &ConsultantDashboardData{
		Metrics: ConsultantDashboardMetrics{},
		HighPriorityCustomers: []ConsultantDashboardCustomer{
			{
				ID:          1,
				Priority:    "high",
				Title:       "重点客户跟进",
				Description: "暂无高优先级客户",
			},
		},
	}

	data.Metrics.TodayDeals.Count = 2
	data.Metrics.TodayDeals.Amount = 28000
	data.Metrics.Following = 8
	data.Metrics.Pending = 3
	data.Metrics.DealRate.Value = 65.2
	data.Metrics.DealRate.Trend = 2.3
	data.Metrics.DealRate.Rank = 2

	data.AbilityScore.Overall = 4.6
	data.AbilityScore.Rank = 2
	data.AbilityScore.Total = 15

	_ = employeeID

	return data, nil
}

// GetDoctorDashboardData retrieves doctor dashboard statistics
func (s *Store) GetDoctorDashboardData(ctx context.Context, tenantID int64, employeeID *int64) (*DoctorDashboardData, error) {
	// TODO: 实现真实的数据库查询
	// 这里先返回模拟数据

	data := &DoctorDashboardData{
		Metrics: DoctorDashboardMetrics{},
		ReviewRecordings: []DoctorDashboardRecording{
			{
				ID:          1,
				Priority:    "medium",
				Title:       "待复盘录音",
				Description: "暂无需要复盘的录音",
				Score:       4.5,
			},
		},
	}

	data.Metrics.TodayRecordings.Count = 8
	data.Metrics.TodayRecordings.AvgDuration = 18
	data.Metrics.Quality.Score = 5
	data.Metrics.Quality.Rate = 98.5
	data.Metrics.WeeklyService.Count = 42
	data.Metrics.WeeklyService.AvgDuration = 16
	data.Metrics.QualityTrend.Value = 4.8
	data.Metrics.QualityTrend.Trend = 0.2

	data.AbilityScores.Professionalism = 4.9
	data.AbilityScores.Empathy = 4.6
	data.AbilityScores.Efficiency = 4.8
	data.AbilityScores.Compliance = 5.0
	data.AbilityScores.Rank = 1
	data.AbilityScores.Total = 8

	_ = employeeID

	return data, nil
}
