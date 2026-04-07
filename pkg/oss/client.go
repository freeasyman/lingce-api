package oss

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// Client represents an OSS client
type Client struct {
	client *oss.Client
	bucket *oss.Bucket
}

// NewClient creates a new OSS client
func NewClient(endpoint, accessKeyID, accessKeySecret, bucketName string) (*Client, error) {
	client, err := oss.New(endpoint, accessKeyID, accessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create OSS client: %w", err)
	}

	bucket, err := client.Bucket(bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket: %w", err)
	}

	return &Client{
		client: client,
		bucket: bucket,
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
	url := fmt.Sprintf("https://%s.%s/%s", c.bucket.BucketName, c.bucket.Client.Config.Endpoint, objectKey)
	return url, nil
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
