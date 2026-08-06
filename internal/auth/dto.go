package auth

// LoginRequest represents a login request
type LoginRequest struct {
	Username    string `json:"username"`
	Phone       string `json:"phone,omitempty"`
	Password    string `json:"password"`
	CaptchaID   string `json:"captcha_id,omitempty"`
	CaptchaCode string `json:"captcha_code,omitempty"`
	Captcha     string `json:"captcha,omitempty"` // backward compatibility
}

// LoginResponse represents a login response
type LoginResponse struct {
	Token       string      `json:"token"`
	AccessToken string      `json:"access_token,omitempty"` // backward compatibility
	TokenType   string      `json:"token_type,omitempty"`   // backward compatibility
	UserType    string      `json:"user_type"`
	UserID      int64       `json:"user_id"`
	Username    string      `json:"username"`
	TenantID    *int64      `json:"tenant_id,omitempty"`
	ExpiresAt   int64       `json:"expires_at"`
	User        *LoginUser  `json:"user,omitempty"` // backward compatibility
	UserInfo    interface{} `json:"user_info,omitempty"`
}

// LoginUser represents a backward-compatible nested user payload.
type LoginUser struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Phone      string  `json:"phone,omitempty"`
	Role       string  `json:"role"`
	TenantID   *int64  `json:"tenant_id"`
	TenantName *string `json:"tenant_name,omitempty"`
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
	OldPassword    string `json:"old_password"`
	NewPassword    string `json:"new_password"`
	OldPasswordAlt string `json:"oldPassword,omitempty"`
	NewPasswordAlt string `json:"newPassword,omitempty"`
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
	CaptchaID    string `json:"captcha_id"`
	ImageData    string `json:"image_data"`              // base64 encoded (new field)
	CaptchaImage string `json:"captcha_image,omitempty"` // backward compatibility
}
