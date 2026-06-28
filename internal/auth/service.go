package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/sms"
)

type Service struct {
	store          *Store
	captchaStore   *CaptchaStore
	smsClient      *sms.AliyunClient
	jwtSecret      string
	jwtExpiryHours int
}

func NewService(store *Store, smsClient *sms.AliyunClient, jwtSecret string, jwtExpiryHours int) *Service {
	return &Service{
		store:          store,
		captchaStore:   NewCaptchaStore(),
		smsClient:      smsClient,
		jwtSecret:      jwtSecret,
		jwtExpiryHours: jwtExpiryHours,
	}
}

func (s *Service) resolveInstitutionRoleCode(ctx context.Context, tenantID, employeeID int64) string {
	roleCode, err := s.store.GetLatestEmployeeRoleCode(ctx, tenantID, employeeID)
	if err != nil {
		return ""
	}
	return roleCode
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
		Token:       token,
		AccessToken: token,
		TokenType:   "bearer",
		UserType:    string(auth.UserTypeAdmin),
		UserID:      admin.ID,
		Username:    admin.Username,
		ExpiresAt:   expiresAt,
		User: &LoginUser{
			ID:       admin.ID,
			Name:     admin.Username,
			Phone:    admin.Phone,
			Role:     string(auth.UserTypeAdmin),
			TenantID: nil,
		},
		UserInfo: map[string]interface{}{
			"real_name": admin.RealName,
			"email":     admin.Email,
			"phone":     admin.Phone,
		},
	}, nil
}

// LoginEmployee authenticates an institution employee
func (s *Service) LoginEmployee(ctx context.Context, username, password string, tenantID int64) (*LoginResponse, error) {
	if tenantID == 0 {
		options, optErr := s.store.ListEmployeeTenantOptionsByLoginID(ctx, username)
		if optErr != nil {
			return nil, fmt.Errorf("invalid credentials")
		}
		if len(options) == 0 {
			return nil, newUnauthorizedError("INVALID_CREDENTIALS", "invalid credentials")
		}

		activeOptions := make([]TenantOption, 0, len(options))
		for _, option := range options {
			if option.IsActive {
				activeOptions = append(activeOptions, option)
			}
		}

		if len(activeOptions) == 0 {
			return nil, newUnauthorizedError("TENANT_INACTIVE", "tenant is inactive")
		}
		if len(activeOptions) > 1 {
			return nil, newTenantSelectionRequiredError(activeOptions)
		}

		tenantID = activeOptions[0].TenantID
	}

	// Verify tenant is active
	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return nil, newUnauthorizedError("INVALID_TENANT", "invalid tenant")
	}

	if !tenant.IsActive {
		return nil, newUnauthorizedError("TENANT_INACTIVE", "tenant is inactive")
	}

	// Check tenant validity period
	now := time.Now()
	if tenant.ValidTo != nil && tenant.ValidTo.Before(now) {
		return nil, newUnauthorizedError("TENANT_INACTIVE", "tenant subscription expired")
	}

	// Get employee
	employee, err := s.store.GetEmployeeByUsername(ctx, username, tenantID)
	if err != nil {
		// Distinguish tenant mismatch from credential errors for better troubleshooting.
		anyTenantEmployee, anyErr := s.store.GetEmployeeByLoginAnyTenant(ctx, username)
		if anyErr == nil && anyTenantEmployee != nil && anyTenantEmployee.TenantID != tenantID {
			return nil, newUnauthorizedError("TENANT_MISMATCH", "tenant mismatch")
		}
		return nil, newUnauthorizedError("INVALID_CREDENTIALS", "invalid credentials")
	}

	if !employee.IsActive {
		return nil, newUnauthorizedError("ACCOUNT_INACTIVE", "account is inactive")
	}

	// Verify password
	if !s.verifyPassword(password, employee.PasswordHash) {
		return nil, newUnauthorizedError("INVALID_CREDENTIALS", "invalid credentials")
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

	roleCode := s.resolveInstitutionRoleCode(ctx, employee.TenantID, employee.ID)

	return &LoginResponse{
		Token:       token,
		AccessToken: token,
		TokenType:   "bearer",
		UserType:    string(auth.UserTypeEmployee),
		UserID:      employee.ID,
		Username:    employee.Username,
		TenantID:    &employee.TenantID,
		ExpiresAt:   expiresAt,
		User: &LoginUser{
			ID:         employee.ID,
			Name:       firstNonEmpty(employee.FullName, employee.Name, employee.Username, employee.Phone),
			Phone:      employee.Phone,
			Role:       firstNonEmpty(roleCode, string(auth.UserTypeEmployee)),
			TenantID:   &employee.TenantID,
			TenantName: &tenant.Name,
		},
		UserInfo: map[string]interface{}{
			"name":          firstNonEmpty(employee.FullName, employee.Name, employee.Username, employee.Phone),
			"real_name":     employee.FullName,
			"full_name":     employee.FullName,
			"phone":         employee.Phone,
			"email":         employee.Email,
			"department_id": employee.DepartmentID,
			"role_code":     roleCode,
		},
	}, nil
}

// LoginMobile authenticates a mobile employee (session isolated)
func (s *Service) LoginMobile(ctx context.Context, username, password string, tenantID int64) (*LoginResponse, error) {
	if tenantID == 0 {
		options, optErr := s.store.ListEmployeeTenantOptionsByLoginID(ctx, username)
		if optErr != nil {
			return nil, newUnauthorizedError("INVALID_CREDENTIALS", "invalid credentials")
		}
		if len(options) == 0 {
			return nil, newUnauthorizedError("INVALID_CREDENTIALS", "invalid credentials")
		}

		activeOptions := make([]TenantOption, 0, len(options))
		for _, option := range options {
			if option.IsActive {
				activeOptions = append(activeOptions, option)
			}
		}

		if len(activeOptions) == 0 {
			return nil, newUnauthorizedError("TENANT_INACTIVE", "tenant is inactive")
		}
		if len(activeOptions) > 1 {
			return nil, newTenantSelectionRequiredError(activeOptions)
		}

		tenantID = activeOptions[0].TenantID
	}

	tenant, err := s.store.GetTenantByID(ctx, tenantID)
	if err != nil {
		return nil, newUnauthorizedError("INVALID_TENANT", "invalid tenant")
	}

	if !tenant.IsActive {
		return nil, newUnauthorizedError("TENANT_INACTIVE", "tenant is inactive")
	}

	now := time.Now()
	if tenant.ValidTo != nil && tenant.ValidTo.Before(now) {
		return nil, newUnauthorizedError("TENANT_INACTIVE", "tenant subscription expired")
	}

	employee, err := s.store.GetEmployeeByUsername(ctx, username, tenantID)
	if err != nil {
		anyTenantEmployee, anyErr := s.store.GetEmployeeByLoginAnyTenant(ctx, username)
		if anyErr == nil && anyTenantEmployee != nil && anyTenantEmployee.TenantID != tenantID {
			return nil, newUnauthorizedError("TENANT_MISMATCH", "tenant mismatch")
		}
		return nil, newUnauthorizedError("INVALID_CREDENTIALS", "invalid credentials")
	}

	if !employee.IsActive {
		return nil, newUnauthorizedError("ACCOUNT_INACTIVE", "account is inactive")
	}

	if !s.verifyPassword(password, employee.PasswordHash) {
		return nil, newUnauthorizedError("INVALID_CREDENTIALS", "invalid credentials")
	}

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

	roleCode := s.resolveInstitutionRoleCode(ctx, employee.TenantID, employee.ID)

	return &LoginResponse{
		Token:       token,
		AccessToken: token,
		TokenType:   "bearer",
		UserType:    string(auth.UserTypeMobile),
		UserID:      employee.ID,
		Username:    employee.Username,
		TenantID:    &employee.TenantID,
		ExpiresAt:   expiresAt,
		User: &LoginUser{
			ID:         employee.ID,
			Name:       firstNonEmpty(employee.FullName, employee.Name, employee.Username, employee.Phone),
			Phone:      employee.Phone,
			Role:       firstNonEmpty(roleCode, string(auth.UserTypeMobile)),
			TenantID:   &employee.TenantID,
			TenantName: &tenant.Name,
		},
		UserInfo: map[string]interface{}{
			"name":      firstNonEmpty(employee.FullName, employee.Name, employee.Username, employee.Phone),
			"real_name": employee.FullName,
			"full_name": employee.FullName,
			"phone":     employee.Phone,
			"role_code": roleCode,
		},
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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

// GetMe retrieves current user information
func (s *Service) GetMe(ctx context.Context, userID int64, userType auth.UserType, tenantID *int64) (*MeResponse, error) {
	switch userType {
	case auth.UserTypeAdmin:
		admin, err := s.store.GetAdminByID(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get admin: %w", err)
		}

		return &MeResponse{
			UserID:   admin.ID,
			UserType: string(auth.UserTypeAdmin),
			Username: admin.Username,
			UserInfo: map[string]interface{}{
				"real_name": admin.RealName,
				"email":     admin.Email,
				"phone":     admin.Phone,
				"is_active": admin.IsActive,
			},
		}, nil

	case auth.UserTypeEmployee, auth.UserTypeMobile:
		if tenantID == nil {
			return nil, fmt.Errorf("tenant_id is required for employee")
		}

		employee, err := s.store.GetEmployeeByID(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get employee: %w", err)
		}
		roleCode := s.resolveInstitutionRoleCode(ctx, employee.TenantID, employee.ID)

		return &MeResponse{
			UserID:   employee.ID,
			UserType: string(userType),
			Username: employee.Username,
			TenantID: &employee.TenantID,
			UserInfo: map[string]interface{}{
				"name":          firstNonEmpty(employee.FullName, employee.Name, employee.Username, employee.Phone),
				"full_name":     employee.FullName,
				"phone":         employee.Phone,
				"email":         employee.Email,
				"department_id": employee.DepartmentID,
				"is_active":     employee.IsActive,
				"role_code":     roleCode,
			},
		}, nil

	default:
		return nil, fmt.Errorf("unknown user type")
	}
}

// ChangePassword changes user password
func (s *Service) ChangePassword(ctx context.Context, userID int64, userType auth.UserType, oldPassword, newPassword string) error {
	// Validate new password
	if len(newPassword) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}

	switch userType {
	case auth.UserTypeAdmin:
		admin, err := s.store.GetAdminByID(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get admin: %w", err)
		}

		// Verify old password
		if !s.verifyPassword(oldPassword, admin.PasswordHash) {
			return fmt.Errorf("invalid old password")
		}

		// Hash new password with bcrypt
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed to hash password: %w", err)
		}

		// Update password and increment session version
		if err := s.store.UpdateAdminPassword(ctx, userID, string(hashedPassword)); err != nil {
			return fmt.Errorf("failed to update password: %w", err)
		}

		return nil

	case auth.UserTypeEmployee, auth.UserTypeMobile:
		employee, err := s.store.GetEmployeeByID(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get employee: %w", err)
		}

		// Verify old password
		if !s.verifyPassword(oldPassword, employee.PasswordHash) {
			return fmt.Errorf("invalid old password")
		}

		// Hash new password with bcrypt
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed to hash password: %w", err)
		}

		// Update password and increment session version
		if err := s.store.UpdateEmployeePassword(ctx, userID, string(hashedPassword)); err != nil {
			return fmt.Errorf("failed to update password: %w", err)
		}

		return nil

	default:
		return fmt.Errorf("unknown user type")
	}
}

// SendSMSCode sends a SMS verification code
func (s *Service) SendSMSCode(ctx context.Context, phone string) error {
	// Generate 6-digit code
	code := generateSMSCode(6)

	// Send SMS
	if err := s.smsClient.SendCode(phone, code); err != nil {
		return fmt.Errorf("failed to send SMS: %w", err)
	}

	// Save code to database with 5 minute expiration
	expiresAt := time.Now().Add(5 * time.Minute).Unix()
	if err := s.store.SaveSMSCode(ctx, phone, code, expiresAt); err != nil {
		return fmt.Errorf("failed to save SMS code: %w", err)
	}

	return nil
}

// LoginSMS authenticates a mobile employee via SMS code
func (s *Service) LoginSMS(ctx context.Context, phone, code string) (*LoginResponse, error) {
	// Verify SMS code
	valid, err := s.store.VerifySMSCode(ctx, phone, code)
	if err != nil {
		return nil, fmt.Errorf("failed to verify SMS code: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("invalid or expired SMS code")
	}

	// Get employee by phone
	employee, err := s.store.GetEmployeeByPhone(ctx, phone)
	if err != nil {
		return nil, fmt.Errorf("employee not found")
	}

	if !employee.IsActive {
		return nil, fmt.Errorf("account is inactive")
	}

	// Verify tenant
	tenant, err := s.store.GetTenantByID(ctx, employee.TenantID)
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

	// Generate JWT token with mobile user type
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

	roleCode := s.resolveInstitutionRoleCode(ctx, employee.TenantID, employee.ID)

	return &LoginResponse{
		Token:       token,
		AccessToken: token,
		TokenType:   "bearer",
		UserType:    string(auth.UserTypeMobile),
		UserID:      employee.ID,
		Username:    employee.Username,
		TenantID:    &employee.TenantID,
		ExpiresAt:   expiresAt,
		User: &LoginUser{
			ID:         employee.ID,
			Name:       employee.Username,
			Phone:      employee.Phone,
			Role:       firstNonEmpty(roleCode, string(auth.UserTypeMobile)),
			TenantID:   &employee.TenantID,
			TenantName: &tenant.Name,
		},
		UserInfo: map[string]interface{}{
			"full_name": employee.FullName,
			"phone":     employee.Phone,
			"role_code": roleCode,
		},
	}, nil
}

// generateSMSCode generates a random numeric code
func generateSMSCode(length int) string {
	code := make([]byte, length)
	for i := range code {
		n, _ := rand.Int(rand.Reader, big.NewInt(10))
		code[i] = byte('0' + n.Int64())
	}
	return string(code)
}
