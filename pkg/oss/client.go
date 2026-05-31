package oss

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// Client represents an OSS client
type Client struct {
	client        *oss.Client
	bucket        *oss.Bucket
	publicBaseURL string
}

// NewClient creates a new OSS client
func NewClient(endpoint, accessKeyID, accessKeySecret, bucketName, publicBaseURL string) (*Client, error) {
	client, err := oss.New(endpoint, accessKeyID, accessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create OSS client: %w", err)
	}

	bucket, err := client.Bucket(bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket: %w", err)
	}

	return &Client{
		client:        client,
		bucket:        bucket,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
	}, nil
}

// UploadOptions represents upload options
type UploadOptions struct {
	ContentType string
	Metadata    map[string]string
}

// UploadFile uploads a file to OSS
func (c *Client) UploadFile(ctx context.Context, objectKey string, data io.Reader, opts *UploadOptions) (string, error) {
	if opts == nil {
		opts = &UploadOptions{}
	}

	// Prepare options
	var ossOpts []oss.Option
	if opts.ContentType != "" {
		ossOpts = append(ossOpts, oss.ContentType(opts.ContentType))
	}
	for k, v := range opts.Metadata {
		ossOpts = append(ossOpts, oss.Meta(k, v))
	}

	// Upload
	err := c.bucket.PutObject(objectKey, data, ossOpts...)
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	// Generate URL
	return c.ObjectURL(objectKey), nil
}

// UploadBytes uploads bytes to OSS
func (c *Client) UploadBytes(ctx context.Context, objectKey string, data []byte, opts *UploadOptions) (string, error) {
	return c.UploadFile(ctx, objectKey, bytes.NewReader(data), opts)
}

// GenerateObjectKey generates a unique object key with timestamp and extension
func GenerateObjectKey(prefix, ext string) string {
	timestamp := time.Now().Format("20060102150405")
	return filepath.Join(prefix, fmt.Sprintf("%s_%d%s", timestamp, time.Now().UnixNano()%1000000, ext))
}

// DeleteFile deletes a file from OSS
func (c *Client) DeleteFile(ctx context.Context, objectKey string) error {
	err := c.bucket.DeleteObject(objectKey)
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

// GetSignedURL generates a signed URL for temporary access
func (c *Client) GetSignedURL(objectKey string, expireSeconds int64) (string, error) {
	url, err := c.bucket.SignURL(objectKey, oss.HTTPGet, expireSeconds)
	if err != nil {
		return "", fmt.Errorf("failed to generate signed URL: %w", err)
	}
	return url, nil
}

// ObjectURL builds public object URL for the configured bucket.
func (c *Client) ObjectURL(objectKey string) string {
	if c.publicBaseURL != "" {
		return c.publicBaseURL + "/" + strings.TrimLeft(objectKey, "/")
	}
	return fmt.Sprintf("https://%s.%s/%s", c.bucket.BucketName, c.bucket.Client.Config.Endpoint, objectKey)
}

// CopyObject copies an object inside the same bucket and returns target URL.
func (c *Client) CopyObject(ctx context.Context, sourceObjectKey, targetObjectKey string) (string, error) {
	_ = ctx
	if sourceObjectKey == "" || targetObjectKey == "" {
		return "", fmt.Errorf("sourceObjectKey and targetObjectKey are required")
	}
	if _, err := c.bucket.CopyObject(sourceObjectKey, targetObjectKey); err != nil {
		return "", fmt.Errorf("failed to copy object: %w", err)
	}
	return c.ObjectURL(targetObjectKey), nil
}
