package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	ossutil "github.com/freeasyman/lingce-api/pkg/oss"
	"github.com/jackc/pgx/v5/pgxpool"
)

type item struct {
	ID       int64
	TenantID int64
	FileURL  string
	FileName string
}

func main() {
	ctx := context.Background()
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	endpoint := firstNonEmptyEnv("OSS_ENDPOINT", "ALIYUN_OSS_ENDPOINT")
	bucket := firstNonEmptyEnv("OSS_BUCKET", "ALIYUN_OSS_BUCKET")
	ak := firstNonEmptyEnv("OSS_ACCESS_KEY_ID", "ALIYUN_OSS_ACCESS_KEY_ID")
	sk := firstNonEmptyEnv("OSS_ACCESS_KEY_SECRET", "ALIYUN_OSS_ACCESS_KEY_SECRET")
	if endpoint == "" || bucket == "" || ak == "" || sk == "" {
		log.Fatal("OSS_ENDPOINT/OSS_BUCKET/OSS_ACCESS_KEY_ID/OSS_ACCESS_KEY_SECRET are required")
	}

	limit := envInt("BACKFILL_LIMIT", 200)
	recordingID := envInt64("BACKFILL_RECORDING_ID", 0)
	timeoutSec := envInt("BACKFILL_HTTP_TIMEOUT_SECONDS", 45)
	maxBytes := int64(envInt("BACKFILL_MAX_BYTES", 150*1024*1024))
	requirePrefix := strings.TrimSpace(os.Getenv("BACKFILL_SOURCE_HOST_CONTAINS"))
	dryRun := strings.EqualFold(strings.TrimSpace(os.Getenv("DRY_RUN")), "1") || strings.EqualFold(strings.TrimSpace(os.Getenv("DRY_RUN")), "true")

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("new db pool: %v", err)
	}
	defer pool.Close()
	ossClient, err := ossutil.NewClient(endpoint, ak, sk, bucket)
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
	`, limit, recordingID)
	if err != nil {
		log.Fatalf("query candidates: %v", err)
	}
	defer rows.Close()

	httpClient := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
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
		if requirePrefix != "" && !strings.Contains(it.FileURL, requirePrefix) {
			skipped++
			continue
		}
		if strings.Contains(it.FileURL, ".aliyuncs.com/") {
			skipped++
			continue
		}

		ownedURL, ossKey, err := migrateOne(ctx, httpClient, ossClient, it, maxBytes)
		if err != nil {
			failed++
			log.Printf("recording=%d migrate failed: %v", it.ID, err)
			continue
		}
		if dryRun {
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
	log.Printf("done total=%d success=%d failed=%d skipped=%d dry_run=%v", total, success, failed, skipped, dryRun)
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

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envInt64(key string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}
