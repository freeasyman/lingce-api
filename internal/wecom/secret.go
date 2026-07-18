package wecom

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

type SecretProtector struct {
	key []byte
}

func NewSecretProtector(master string) *SecretProtector {
	sum := sha256.Sum256([]byte(strings.TrimSpace(master)))
	return &SecretProtector{key: sum[:]}
}

func (p *SecretProtector) Encrypt(plain string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("secret protector is not configured")
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return "", fmt.Errorf("create secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create secret gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	cipherText := gcm.Seal(nil, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(append(nonce, cipherText...)), nil
}

func (p *SecretProtector) Decrypt(cipherText string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("secret protector is not configured")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cipherText))
	if err != nil {
		return "", fmt.Errorf("decode secret ciphertext: %w", err)
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return "", fmt.Errorf("create secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create secret gcm: %w", err)
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid secret ciphertext")
	}
	nonce := raw[:gcm.NonceSize()]
	payload := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret ciphertext: %w", err)
	}
	return string(plain), nil
}
