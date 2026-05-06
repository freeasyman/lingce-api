package auth

import "net/http"

// AuthError represents a structured auth failure that can be mapped to API response codes.
type AuthError struct {
	Status  int
	Code    string
	Message string
	Details interface{}
}

func (e *AuthError) Error() string {
	return e.Message
}

func newAuthError(status int, code, message string, details interface{}) *AuthError {
	return &AuthError{
		Status:  status,
		Code:    code,
		Message: message,
		Details: details,
	}
}

func newUnauthorizedError(code, message string) *AuthError {
	return newAuthError(http.StatusUnauthorized, code, message, nil)
}

func newTenantSelectionRequiredError(options []TenantOption) *AuthError {
	return newAuthError(http.StatusConflict, "TENANT_SELECTION_REQUIRED", "tenant selection required", map[string]interface{}{
		"tenant_options": options,
	})
}

