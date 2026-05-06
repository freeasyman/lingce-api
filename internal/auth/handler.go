package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/captcha"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers auth routes using declarative router framework
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string, pool *pgxpool.Pool) {
	routes := []router.Route{
		// Public login endpoints
		{
			Method:  "POST",
			Path:    "/api/v1/auth/login",
			Handler: h.LoginAdmin,
			Auth:    false,
		},
		{
			Method:  "POST",
			Path:    "/api/v1/auth/login/institution",
			Handler: h.LoginInstitution,
			Auth:    false,
		},
		{
			Method:  "POST",
			Path:    "/api/v1/auth/login/mobile",
			Handler: h.LoginMobile,
			Auth:    false,
		},
		// Protected endpoints
		{
			Method:           "GET",
			Path:             "/api/v1/auth/me",
			Handler:          h.GetMe,
			Auth:             true,
			AllowedUserTypes: []string{"admin", "employee", "mobile"},
		},
		{
			Method:           "POST",
			Path:             "/api/v1/auth/change-password",
			Handler:          h.ChangePassword,
			Auth:             true,
			AllowedUserTypes: []string{"admin", "employee", "mobile"},
		},
		// Captcha endpoint
		{
			Method:  "GET",
			Path:    "/api/v1/auth/captcha",
			Handler: h.GetCaptcha,
			Auth:    false,
		},
		// SMS endpoints (updated paths)
		{
			Method:  "POST",
			Path:    "/api/v1/auth/sms/send",
			Handler: h.SendSMS,
			Auth:    false,
		},
		{
			Method:  "POST",
			Path:    "/api/v1/auth/sms/login",
			Handler: h.SMSLogin,
			Auth:    false,
		},
	}

	deps := router.RouteDeps{
		JWTSecret:   jwtSecret,
		PermChecker: nil, // Auth endpoints don't need permission checking
		Pool:        pool,
	}

	router.Register(mux, routes, deps)
}

// LoginAdmin handles operations admin login
func (h *Handler) LoginAdmin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	loginID := req.Username
	if loginID == "" {
		loginID = req.Phone
	}

	if loginID == "" || req.Password == "" {
		httputil.WriteBadRequest(w, "Username and password are required")
		return
	}

	resp, err := h.service.LoginAdmin(r.Context(), loginID, req.Password)
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
		Phone    string `json:"phone"`
		Password string `json:"password"`
		TenantID int64  `json:"tenant_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	loginID := req.Username
	if loginID == "" {
		loginID = req.Phone
	}

	if loginID == "" || req.Password == "" {
		httputil.WriteBadRequest(w, "Username and password are required")
		return
	}

	resp, err := h.service.LoginEmployee(r.Context(), loginID, req.Password, req.TenantID)
	if err != nil {
		var authErr *AuthError
		if errors.As(err, &authErr) {
			httputil.WriteError(w, authErr.Status, authErr.Code, authErr.Message, authErr.Details)
			return
		}
		httputil.WriteUnauthorized(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}

// LoginMobile handles mobile employee login with session isolation
func (h *Handler) LoginMobile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Phone    string `json:"phone"`
		Password string `json:"password"`
		TenantID int64  `json:"tenant_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	loginID := req.Username
	if loginID == "" {
		loginID = req.Phone
	}

	if loginID == "" || req.Password == "" || req.TenantID == 0 {
		httputil.WriteBadRequest(w, "Username, password and tenant_id are required")
		return
	}

	resp, err := h.service.LoginMobile(r.Context(), loginID, req.Password, req.TenantID)
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
	imageBase64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	httputil.WriteSuccess(w, CaptchaResponse{
		CaptchaID:    captchaID,
		ImageData:    imageBase64,
		CaptchaImage: "data:image/png;base64," + imageBase64,
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
