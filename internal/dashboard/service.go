package dashboard

import (
	"context"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

// GetAdminDashboard retrieves admin dashboard data
func (s *Service) GetAdminDashboard(ctx context.Context, tenantID int64) (*AdminDashboardData, error) {
	return s.store.GetAdminDashboardData(ctx, tenantID)
}

// GetConsultantDashboard retrieves consultant dashboard data
func (s *Service) GetConsultantDashboard(ctx context.Context, tenantID int64, employeeID *int64) (*ConsultantDashboardData, error) {
	return s.store.GetConsultantDashboardData(ctx, tenantID, employeeID)
}

// GetDoctorDashboard retrieves doctor dashboard data
func (s *Service) GetDoctorDashboard(ctx context.Context, tenantID int64, employeeID *int64) (*DoctorDashboardData, error) {
	return s.store.GetDoctorDashboardData(ctx, tenantID, employeeID)
}
