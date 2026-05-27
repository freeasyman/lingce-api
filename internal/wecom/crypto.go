package wecom

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"sort"
)

type Crypto struct {
	token      string
	receiverID string
	aesKey     []byte
}

func NewCrypto(token, encodingAESKey, receiverID string) (*Crypto, error) {
	decoded, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return nil, fmt.Errorf("decode encoding aes key: %w", err)
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("invalid encoding aes key length: %d", len(decoded))
	}
	return &Crypto{token: token, receiverID: receiverID, aesKey: decoded}, nil
}

func (c *Crypto) VerifySignature(signature, timestamp, nonce, encrypted string) bool {
	parts := []string{c.token, timestamp, nonce, encrypted}
	sort.Strings(parts)
	h := sha1.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
	}
	return fmt.Sprintf("%x", h.Sum(nil)) == signature
}

func (c *Crypto) Decrypt(encrypted string) (string, error) {
	cipherText, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(cipherText) == 0 || len(cipherText)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid ciphertext length")
	}
	block, err := aes.NewCipher(c.aesKey)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	plain := make([]byte, len(cipherText))
	cipher.NewCBCDecrypter(block, c.aesKey[:aes.BlockSize]).CryptBlocks(plain, cipherText)
	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return "", err
	}
	if len(plain) < 20 {
		return "", fmt.Errorf("plaintext too short")
	}
	msgLen := int(binary.BigEndian.Uint32(plain[16:20]))
	if msgLen < 0 || len(plain) < 20+msgLen {
		return "", fmt.Errorf("invalid message length")
	}
	msg := plain[20 : 20+msgLen]
	receiverID := string(plain[20+msgLen:])
	_ = receiverID
	return string(msg), nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid pkcs7 data size")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid pkcs7 padding")
	}
	if !bytes.Equal(bytes.Repeat([]byte{byte(padding)}, padding), data[len(data)-padding:]) {
		return nil, fmt.Errorf("invalid pkcs7 padding content")
	}
	return data[:len(data)-padding], nil
}
