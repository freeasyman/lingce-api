package auth

// LoginRequest represents a login request
type LoginRequest struct {
	Username string `json:"username"`
	Phone    string `json:"phone,omitempty"`
	Password string `json:"password"`
	Captcha  string `json:"captcha,omitempty"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	Token     string      `json:"token"`
	UserType  string      `json:"user_type"`
	UserID    int64       `json:"user_id"`
	Username  string      `json:"username"`
	TenantID  *int64      `json:"tenant_id,omitempty"`
	ExpiresAt int64       `json:"expires_at"`
	UserInfo  interface{} `json:"user_info,omitempty"`
}

// MeResponse represents current user info
type MeResponse struct {
	UserID   int64       `json:"user_id"`
	UserType string      `json:"user_type"`
	Username string      `json:"username"`
	TenantID *int64      `json:"tenant_id,omitempty"`
	UserInfo interface{} `json:"user_info"`
}

// ChangePasswordRequest represents a password change request
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// SendSMSRequest represents a SMS send request
type SendSMSRequest struct {
	Phone string `json:"phone"`
}

// SMSLoginRequest represents a SMS login request
type SMSLoginRequest struct {
	Phone string `json:"phone"`
	Code  string `json:"code"`
}

// CaptchaResponse represents a captcha response
type CaptchaResponse struct {
	CaptchaID string `json:"captcha_id"`
	ImageData string `json:"image_data"` // base64 encoded
}
