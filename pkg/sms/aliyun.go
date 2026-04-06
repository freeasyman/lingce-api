package sms

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// AliyunClient is a client for Aliyun SMS service
type AliyunClient struct {
	accessKeyID     string
	accessKeySecret string
	signName        string
	templateCode    string
}

// NewAliyunClient creates a new Aliyun SMS client
func NewAliyunClient(accessKeyID, accessKeySecret, signName, templateCode string) *AliyunClient {
	return &AliyunClient{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		signName:        signName,
		templateCode:    templateCode,
	}
}

// SendCode sends a verification code via SMS
func (c *AliyunClient) SendCode(phone, code string) error {
	params := map[string]string{
		"PhoneNumbers":  phone,
		"SignName":      c.signName,
		"TemplateCode":  c.templateCode,
		"TemplateParam": fmt.Sprintf(`{"code":"%s"}`, code),
	}

	return c.request(params)
}

// request makes a request to Aliyun SMS API
func (c *AliyunClient) request(params map[string]string) error {
	// Add common parameters
	params["Format"] = "JSON"
	params["Version"] = "2017-05-25"
	params["AccessKeyId"] = c.accessKeyID
	params["SignatureMethod"] = "HMAC-SHA1"
	params["Timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	params["SignatureVersion"] = "1.0"
	params["SignatureNonce"] = generateNonce()
	params["Action"] = "SendSms"

	// Calculate signature
	signature := c.calculateSignature(params)
	params["Signature"] = signature

	// Build request URL
	apiURL := "https://dysmsapi.aliyuncs.com/?" + buildQueryString(params)

	// Make HTTP request
	resp, err := http.Get(apiURL)
	if err != nil {
		return fmt.Errorf("failed to send SMS: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for errors
	if code, ok := result["Code"].(string); ok && code != "OK" {
		message := result["Message"].(string)
		return fmt.Errorf("SMS API error: %s - %s", code, message)
	}

	return nil
}

// calculateSignature calculates the signature for Aliyun API
func (c *AliyunClient) calculateSignature(params map[string]string) string {
	// Sort parameters
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build canonical query string
	var parts []string
	for _, k := range keys {
		parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(params[k]))
	}
	canonicalQueryString := strings.Join(parts, "&")

	// Build string to sign
	stringToSign := "GET&" + url.QueryEscape("/") + "&" + url.QueryEscape(canonicalQueryString)

	// Calculate HMAC-SHA1
	h := hmac.New(sha1.New, []byte(c.accessKeySecret+"&"))
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature
}

// buildQueryString builds a query string from parameters
func buildQueryString(params map[string]string) string {
	var parts []string
	for k, v := range params {
		parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
	}
	return strings.Join(parts, "&")
}

// generateNonce generates a random nonce
func generateNonce() string {
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 16)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
