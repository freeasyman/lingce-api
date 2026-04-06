package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/captcha"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/google/uuid"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers auth routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	mux.HandleFunc("POST /api/v1/auth/login", h.LoginAdmin)
	mux.HandleFunc("POST /api/v1/auth/login/institution", h.LoginInstitution)
	mux.HandleFunc("POST /api/v1/auth/login/employee", h.LoginEmployee)
	mux.HandleFunc("POST /api/v1/auth/login/mobile", h.LoginMobile)

	// Protected routes
	authMw := middleware.Auth(jwtSecret)
	mux.Handle("GET /api/v1/auth/me", authMw(http.HandlerFunc(h.GetMe)))
	mux.Handle("POST /api/v1/auth/change-password", authMw(http.HandlerFunc(h.ChangePassword)))

	// Captcha endpoint
	mux.HandleFunc("GET /api/v1/auth/captcha", h.GetCaptcha)

	// SMS endpoints
	mux.HandleFunc("POST /api/v1/auth/mobile/sms/send", h.SendSMS)
	mux.HandleFunc("POST /api/v1/auth/mobile/sms/login", h.SMSLogin)
}

// LoginAdmin handles operations admin login
func (h *Handler) LoginAdmin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" {
		httputil.WriteBadRequest(w, "Username and password are required")
		return
	}

	resp, err := h.service.LoginAdmin(r.Context(), req.Username, req.Password)
	if err != nil {
		httputil.WriteUnauthorized(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}

// LoginInstitution handles institution employee login
func (h *Handler) LoginInstitution(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TenantID int64  `json:"tenant_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" || req.TenantID == 0 {
		httputil.WriteBadRequest(w, "Username, password and tenant_id are required")
		return
	}

	resp, err := h.service.LoginEmployee(r.Context(), req.Username, req.Password, req.TenantID)
	if err != nil {
		httputil.WriteUnauthorized(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}

// LoginEmployee handles employee login (alias for institution login)
func (h *Handler) LoginEmployee(w http.ResponseWriter, r *http.Request) {
	h.LoginInstitution(w, r)
}

// LoginMobile handles mobile employee login with session isolation
func (h *Handler) LoginMobile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TenantID int64  `json:"tenant_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" || req.TenantID == 0 {
		httputil.WriteBadRequest(w, "Username, password and tenant_id are required")
		return
	}

	resp, err := h.service.LoginMobile(r.Context(), req.Username, req.Password, req.TenantID)
	if err != nil {
		httputil.WriteUnauthorized(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}

// GetMe handles get current user information
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	resp, err := h.service.GetMe(r.Context(), claims.UserID, claims.UserType, claims.TenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}

// ChangePassword handles password change
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if req.OldPassword == "" || req.NewPassword == "" {
		httputil.WriteBadRequest(w, "Old password and new password are required")
		return
	}

	err := h.service.ChangePassword(r.Context(), claims.UserID, claims.UserType, req.OldPassword, req.NewPassword)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Password changed successfully"})
}

// GetCaptcha generates and returns a captcha image
func (h *Handler) GetCaptcha(w http.ResponseWriter, r *http.Request) {
	// Generate captcha
	code, img, err := captcha.Generate()
	if err != nil {
		httputil.WriteInternalError(w, "Failed to generate captcha")
		return
	}

	// Generate captcha ID
	captchaID := uuid.New().String()

	// Store captcha code with 5 minute expiration
	h.service.captchaStore.Save(captchaID, code, 5*time.Minute)

	// Encode image to PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		httputil.WriteInternalError(w, "Failed to encode captcha image")
		return
	}

	// Return captcha ID and base64 image
	httputil.WriteSuccess(w, CaptchaResponse{
		CaptchaID: captchaID,
		ImageData: base64.StdEncoding.EncodeToString(buf.Bytes()),
	})
}

// SendSMS handles SMS verification code sending
func (h *Handler) SendSMS(w http.ResponseWriter, r *http.Request) {
	var req SendSMSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if req.Phone == "" {
		httputil.WriteBadRequest(w, "Phone number is required")
		return
	}

	if err := h.service.SendSMSCode(r.Context(), req.Phone); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "SMS code sent successfully"})
}

// SMSLogin handles SMS code login
func (h *Handler) SMSLogin(w http.ResponseWriter, r *http.Request) {
	var req SMSLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if req.Phone == "" || req.Code == "" {
		httputil.WriteBadRequest(w, "Phone and code are required")
		return
	}

	resp, err := h.service.LoginSMS(r.Context(), req.Phone, req.Code)
	if err != nil {
		httputil.WriteUnauthorized(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}
