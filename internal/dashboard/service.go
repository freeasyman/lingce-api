package dashboard

import (
	"context"
	"time"

	"github.com/freeasyman/lingce-api/internal/recording"
)

type Service struct {
	store            *Store
	dailyReportStore DailyReportProvider
}

type DailyReportProvider interface {
	GetDailyReport(ctx context.Context, tenantID int64, dateFrom, dateTo, date string) (*recording.DailyReportResponse, error)
}

func NewService(store *Store, dailyReportStore DailyReportProvider) *Service {
	return &Service{store: store, dailyReportStore: dailyReportStore}
}

// GetAdminDashboard retrieves admin dashboard data
func (s *Service) GetAdminDashboard(ctx context.Context, tenantID int64) (*AdminDashboardData, error) {
	// Keep existing admin-only metrics, then overwrite core business metrics with daily-report source.
	data, err := s.store.GetAdminDashboardData(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if s.dailyReportStore == nil {
		return data, nil
	}

	now := time.Now()
	dateTo := now.Format("2006-01-02")
	dateFrom := now.AddDate(0, 0, -29).Format("2006-01-02")
	report, err := s.dailyReportStore.GetDailyReport(ctx, tenantID, dateFrom, dateTo, dateTo)
	if err != nil || report == nil {
		return data, nil
	}

	data.Metrics.Patients.Value = int(report.TotalRecordings)

	if report.Revenue != nil {
		data.Metrics.Revenue.Value = roundFloat(report.Revenue.TotalAmount, 0)
		if report.Revenue.MomAmount != nil {
			prevRevenue := report.Revenue.TotalAmount - *report.Revenue.MomAmount
			if prevRevenue > 0 {
				data.Metrics.Revenue.Trend = roundFloat((*report.Revenue.MomAmount)/prevRevenue*100, 1)
			} else if report.Revenue.TotalAmount > 0 {
				data.Metrics.Revenue.Trend = 100
			}
		}

		data.Metrics.DealRate.Value = roundFloat(report.Revenue.DealRate, 1)
		if report.Revenue.MomDealRate != nil {
			data.Metrics.DealRate.Trend = roundFloat(*report.Revenue.MomDealRate, 1)
		}

		if report.Revenue.TargetProgress != nil {
			data.Metrics.TargetAchievement.Value = roundFloat(*report.Revenue.TargetProgress, 1)
		}
	}

	topPerformers := make([]AdminDashboardTopPerformer, 0, 3)
	for i, item := range report.EmployeeRanking {
		if i >= 3 {
			break
		}
		topPerformers = append(topPerformers, AdminDashboardTopPerformer{
			Name:    item.Name,
			Revenue: item.DealAmount,
			Rank:    i + 1,
		})
	}
	if len(topPerformers) > 0 {
		data.TopPerformers = topPerformers
	}

	return data, nil
}

// GetConsultantDashboard retrieves consultant dashboard data
func (s *Service) GetConsultantDashboard(ctx context.Context, tenantID int64, employeeID *int64) (*ConsultantDashboardData, error) {
	return s.store.GetConsultantDashboardData(ctx, tenantID, employeeID)
}

// GetDoctorDashboard retrieves doctor dashboard data
func (s *Service) GetDoctorDashboard(ctx context.Context, tenantID int64, employeeID *int64) (*DoctorDashboardData, error) {
	return s.store.GetDoctorDashboardData(ctx, tenantID, employeeID)
}

func (s *Service) GetOpsWorkbenchOverview(ctx context.Context, tenantID *int64, dateFrom, dateTo string) (*OpsWorkbenchOverview, error) {
	return s.store.GetOpsWorkbenchOverview(ctx, tenantID, dateFrom, dateTo)
}

func (s *Service) GetOpsWorkbenchTrend(ctx context.Context, tenantID *int64, dateFrom, dateTo, metric string) (*OpsWorkbenchTrend, error) {
	return s.store.GetOpsWorkbenchTrend(ctx, tenantID, dateFrom, dateTo, metric)
}

func (s *Service) GetOpsWorkbenchTable(ctx context.Context, tenantID *int64, dateFrom, dateTo string, page, pageSize int) ([]OpsWorkbenchTableRow, int64, error) {
	return s.store.GetOpsWorkbenchTable(ctx, tenantID, dateFrom, dateTo, page, pageSize)
}
