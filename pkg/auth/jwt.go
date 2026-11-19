package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// UserType represents the type of user
type UserType string

const (
	UserTypeAdmin      UserType = "admin"
	UserTypeEmployee   UserType = "employee"
	UserTypeMobile     UserType = "mobile"
)

// Claims represents JWT claims
// Entry 常量：登录入口。机构端/移动端（老产品）看 tenants 老有效期，
// 门户（新产品）看合同行。旧 token 无该字段，默认按老产品处理。
const (
	EntryInstitution = "institution"
	EntryPortal      = "portal"
	EntryMobile      = "mobile"
)

type Claims struct {
	UserID         int64    `json:"sub"`
	UserType       UserType `json:"type"`
	TenantID       *int64   `json:"tenant_id,omitempty"`
	SessionVersion int      `json:"session_version"`
	Entry          string   `json:"entry,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken generates a JWT token
func GenerateToken(secret string, userID int64, userType UserType, tenantID *int64, sessionVersion int, expiryHours int) (string, int64, error) {
	return GenerateTokenWithEntry(secret, userID, userType, tenantID, sessionVersion, expiryHours, "")
}

// GenerateTokenWithEntry 与 GenerateToken 相同，额外携带登录入口（entry）。
func GenerateTokenWithEntry(secret string, userID int64, userType UserType, tenantID *int64, sessionVersion int, expiryHours int, entry string) (string, int64, error) {
	expiresAt := time.Now().Add(time.Duration(expiryHours) * time.Hour)

	claims := Claims{
		UserID:         userID,
		UserType:       userType,
		TenantID:       tenantID,
		SessionVersion: sessionVersion,
		Entry:          entry,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", 0, fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, expiresAt.Unix(), nil
}

// ParseToken parses and validates a JWT token
func ParseToken(secret string, tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}
