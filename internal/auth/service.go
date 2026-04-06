package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/freeasyman/lingce-api/pkg/auth"
)

type Service struct {
	store         *Store
	jwtSecret     string
	jwtExpiryHours int
}

func NewService(store *Store, jwtSecret string, jwtExpiryHours int) *Service {
	return &Service{
		store:          store,
		jwtSecret:      jwtSecret,
		jwtExpiryHours: jwtExpiryHours,
	}
}

// LoginAdmin authenticates an operations admin
func (s *Service) LoginAdmin(ctx context.Context, username, password string) (*LoginResponse, error) {
	admin, err := s.store.GetAdminByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	if !admin.IsActive {
		return nil, fmt.Errorf("account is inactive")
	}

	// Verify password (try bcrypt first, fallback to SHA256 for old passwords)
	if !s.verifyPassword(password, admin.PasswordHash) {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Generate JWT token
	token, expiresAt, err := auth.GenerateToken(
		s.jwtSecret,
		admin.ID,
		auth.UserTypeAdmin,
		nil,
		admin.SessionVersion,
		s.jwtExpiryHours,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &LoginResponse{
		Token:     token,
		UserType:  string(auth.UserTypeAdmin),
		UserID:    admin.ID,
		Username:  admin.Username,
		ExpiresAt: expiresAt,
		UserInfo: map[string]interface{}{
			"real_name": admin.RealName,
			"email":     admin.Email,
			"phone":     admin.Phone,
		},
	}, nil
}

// LoginEmployee authenticates an institution employee
func (s *Service) LoginEmployee(ctx context.Context, username, password string, tenantID int64) (*LoginResponse, error) {
	// Verify tenant is active
	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("invalid tenant")
	}

	if !tenant.IsActive {
		return nil, fmt.Errorf("tenant is inactive")
	}

	// Check tenant validity period
	now := time.Now()
	if tenant.ValidTo != nil && tenant.ValidTo.Before(now) {
		return nil, fmt.Errorf("tenant subscription expired")
	}

	// Get employee
	employee, err := s.store.GetEmployeeByUsername(ctx, username, tenantID)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	if !employee.IsActive {
		return nil, fmt.Errorf("account is inactive")
	}

	// Verify password
	if !s.verifyPassword(password, employee.PasswordHash) {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Generate JWT token
	token, expiresAt, err := auth.GenerateToken(
		s.jwtSecret,
		employee.ID,
		auth.UserTypeEmployee,
		&employee.TenantID,
		employee.SessionVersion,
		s.jwtExpiryHours,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &LoginResponse{
		Token:     token,
		UserType:  string(auth.UserTypeEmployee),
		UserID:    employee.ID,
		Username:  employee.Username,
		TenantID:  &employee.TenantID,
		ExpiresAt: expiresAt,
		UserInfo: map[string]interface{}{
			"full_name":     employee.FullName,
			"phone":         employee.Phone,
			"email":         employee.Email,
			"department_id": employee.DepartmentID,
		},
	}, nil
}

// LoginMobile authenticates a mobile employee (session isolated)
func (s *Service) LoginMobile(ctx context.Context, username, password string, tenantID int64) (*LoginResponse, error) {
	// Same as LoginEmployee but with different user type for session isolation
	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("invalid tenant")
	}

	if !tenant.IsActive {
		return nil, fmt.Errorf("tenant is inactive")
	}

	now := time.Now()
	if tenant.ValidTo != nil && tenant.ValidTo.Before(now) {
		return nil, fmt.Errorf("tenant subscription expired")
	}

	employee, err := s.store.GetEmployeeByUsername(ctx, username, tenantID)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	if !employee.IsActive {
		return nil, fmt.Errorf("account is inactive")
	}

	if !s.verifyPassword(password, employee.PasswordHash) {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Use mobile user type for session isolation
	token, expiresAt, err := auth.GenerateToken(
		s.jwtSecret,
		employee.ID,
		auth.UserTypeMobile,
		&employee.TenantID,
		employee.SessionVersion,
		s.jwtExpiryHours,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &LoginResponse{
		Token:     token,
		UserType:  string(auth.UserTypeMobile),
		UserID:    employee.ID,
		Username:  employee.Username,
		TenantID:  &employee.TenantID,
		ExpiresAt: expiresAt,
		UserInfo: map[string]interface{}{
			"full_name": employee.FullName,
			"phone":     employee.Phone,
		},
	}, nil
}

// verifyPassword checks password against hash (bcrypt or legacy SHA256)
func (s *Service) verifyPassword(password, hash string) bool {
	// Try bcrypt first
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err == nil {
		return true
	}

	// Fallback to SHA256 for old passwords
	sha256Hash := sha256.Sum256([]byte(password))
	sha256Hex := hex.EncodeToString(sha256Hash[:])
	return sha256Hex == hash
}
