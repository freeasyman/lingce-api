package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/scriptutil"
	ossutil "github.com/freeasyman/lingce-api/pkg/oss"
)

type item struct {
	ID       int64
	TenantID int64
	FileURL  string
	FileName string
}

func main() {
	ctx := context.Background()
	configPath := flag.String("config", "./configs/dev.toml", "path to TOML config file")
	limit := flag.Int("limit", 200, "max number of recordings to scan")
	recordingID := flag.Int64("recording-id", 0, "specific recording ID to backfill")
	timeoutSec := flag.Int("http-timeout-seconds", 45, "HTTP download timeout in seconds")
	maxBytes := flag.Int64("max-bytes", 150*1024*1024, "max allowed download size in bytes")
	requirePrefix := flag.String("source-host-contains", "", "only process source URLs containing this substring")
	dryRun := flag.Bool("dry-run", false, "preview without updating database")
	flag.Parse()

	cfg, pool, err := scriptutil.OpenPool(ctx, *configPath)
	if err != nil {
		log.Fatalf("open db pool: %v", err)
	}
	defer pool.Close()

	endpoint := strings.TrimSpace(cfg.Aliyun.OSSEndpoint)
	bucket := strings.TrimSpace(cfg.Aliyun.OSSBucket)
	ak := strings.TrimSpace(cfg.Aliyun.AccessKeyID)
	sk := strings.TrimSpace(cfg.Aliyun.AccessKeySecret)
	if endpoint == "" || bucket == "" || ak == "" || sk == "" {
		log.Fatal("aliyun.oss_endpoint/oss_bucket and aliyun access key secrets are required")
	}
	ossClient, err := ossutil.NewClient(endpoint, ak, sk, bucket, strings.TrimSpace(cfg.Aliyun.OSSPublicBaseURL))
	if err != nil {
		log.Fatalf("new oss client: %v", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT id, tenant_id, COALESCE(file_url, ''), COALESCE(file_name, '')
		FROM recordings
		WHERE COALESCE(oss_key, '') = ''
		  AND COALESCE(file_url, '') <> ''
		  AND ($2::bigint = 0 OR id = $2::bigint)
		ORDER BY id ASC
		LIMIT $1
	`, *limit, *recordingID)
	if err != nil {
		log.Fatalf("query candidates: %v", err)
	}
	defer rows.Close()

	httpClient := &http.Client{Timeout: time.Duration(*timeoutSec) * time.Second}
	total := 0
	success := 0
	failed := 0
	skipped := 0

	for rows.Next() {
		total++
		var it item
		if err := rows.Scan(&it.ID, &it.TenantID, &it.FileURL, &it.FileName); err != nil {
			failed++
			log.Printf("scan failed: %v", err)
			continue
		}
		if *requirePrefix != "" && !strings.Contains(it.FileURL, *requirePrefix) {
			skipped++
			continue
		}
		if strings.Contains(it.FileURL, ".aliyuncs.com/") {
			skipped++
			continue
		}

		ownedURL, ossKey, err := migrateOne(ctx, httpClient, ossClient, it, *maxBytes)
		if err != nil {
			failed++
			log.Printf("recording=%d migrate failed: %v", it.ID, err)
			continue
		}
		if *dryRun {
			success++
			log.Printf("dry-run recording=%d -> key=%s", it.ID, ossKey)
			continue
		}
		if _, err := pool.Exec(ctx, `
			UPDATE recordings
			SET file_url = $1, oss_key = $2, updated_at = NOW()
			WHERE id = $3
		`, ownedURL, ossKey, it.ID); err != nil {
			failed++
			log.Printf("recording=%d db update failed: %v", it.ID, err)
			continue
		}
		success++
		log.Printf("recording=%d migrated", it.ID)
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("iterate rows: %v", err)
	}
	log.Printf("done total=%d success=%d failed=%d skipped=%d dry_run=%v", total, success, failed, skipped, *dryRun)
}

func migrateOne(ctx context.Context, client *http.Client, ossClient *ossutil.Client, it item, maxBytes int64) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, it.FileURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("new request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("download status=%d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return "", "", fmt.Errorf("file too large: %d", resp.ContentLength)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return "", "", fmt.Errorf("file exceeds max bytes")
	}
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(it.FileName)))
	if ext == "" {
		ext = ".mp3"
	}
	ossKey := ossutil.GenerateObjectKey(fmt.Sprintf("recordings/%d/%s", it.TenantID, time.Now().UTC().Format("2006/01/02")), ext)
	ownedURL, err := ossClient.UploadBytes(ctx, ossKey, body, &ossutil.UploadOptions{
		ContentType: "audio/mpeg",
		Metadata: map[string]string{
			"recording-id": strconv.FormatInt(it.ID, 10),
			"source-url":   it.FileURL,
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("upload oss: %w", err)
	}
	return ownedURL, ossKey, nil
}
