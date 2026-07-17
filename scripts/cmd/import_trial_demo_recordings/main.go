package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	apiconfig "github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/internal/employee"
	"github.com/freeasyman/lingce-api/internal/recording"
	"github.com/freeasyman/lingce-api/internal/scriptutil"
	"github.com/freeasyman/lingce-api/internal/tenant"
	ossutil "github.com/freeasyman/lingce-api/pkg/oss"
)

type assetInput struct {
	RoleCode   string
	Title      string
	AssetCode  string
	FilePath   string
	EmployeeID int64
}

func main() {
	ctx := context.Background()
	configPath := flag.String("config", "./configs/dev.toml", "path to TOML config file")
	assetTenantID := flag.Int64("asset-tenant-id", 0, "tenant id for demo asset storage")
	templateCode := flag.String("template-code", "intent_trial_v1", "trial template code")
	version := flag.String("version", "v1", "asset version")
	doctorFile := flag.String("doctor-file", "", "doctor demo audio file path")
	doctorEmployeeID := flag.Int64("doctor-employee-id", 0, "doctor employee id in asset tenant")
	consultantFile := flag.String("consultant-file", "", "consultant demo audio file path")
	consultantEmployeeID := flag.Int64("consultant-employee-id", 0, "consultant employee id in asset tenant")
	activate := flag.Bool("activate", true, "mark imported assets active")
	skipAssetUpsert := flag.Bool("skip-asset-upsert", false, "skip trial demo asset upsert after ingest")
	flag.Parse()

	if *assetTenantID <= 0 {
		log.Fatal("--asset-tenant-id is required")
	}
	if strings.TrimSpace(*doctorFile) == "" || strings.TrimSpace(*consultantFile) == "" {
		log.Fatal("--doctor-file and --consultant-file are required")
	}
	if *doctorEmployeeID <= 0 || *consultantEmployeeID <= 0 {
		log.Fatal("--doctor-employee-id and --consultant-employee-id are required")
	}

	cfg, pool, err := scriptutil.OpenPool(ctx, *configPath)
	if err != nil {
		log.Fatalf("open db pool: %v", err)
	}
	defer pool.Close()

	apiCfg, err := apiconfig.Load(*configPath)
	if err != nil {
		log.Fatalf("load api config: %v", err)
	}

	ossClient, err := ossutil.NewClient(
		strings.TrimSpace(cfg.Aliyun.OSSEndpoint),
		strings.TrimSpace(cfg.Aliyun.AccessKeyID),
		strings.TrimSpace(cfg.Aliyun.AccessKeySecret),
		strings.TrimSpace(cfg.Aliyun.OSSBucket),
		strings.TrimSpace(cfg.Aliyun.OSSPublicBaseURL),
	)
	if err != nil {
		log.Fatalf("new oss client: %v", err)
	}

	recordingStore := recording.NewStore(pool)
	employeeStore := employee.NewStore(pool)
	recordingSvc := recording.NewService(
		recordingStore,
		employeeStore,
		strings.TrimSpace(apiCfg.External.RecordingWorkerURL),
		strings.TrimSpace(apiCfg.External.RecordingWorkerToken),
		strings.TrimSpace(apiCfg.External.LingceWorkerURL),
		strings.TrimSpace(apiCfg.External.LingceWorkerToken),
		strings.TrimSpace(apiCfg.Recording.ResetCodeDictionaryPath),
		nil,
		nil,
	)
	tenantStore := tenant.NewStore(pool)

	inputs := []assetInput{
		{RoleCode: "doctor", Title: "示范录音 · 医生", AssetCode: "doctor_demo", FilePath: *doctorFile, EmployeeID: *doctorEmployeeID},
		{RoleCode: "consultant", Title: "示范录音 · 咨询师", AssetCode: "consultant_demo", FilePath: *consultantFile, EmployeeID: *consultantEmployeeID},
	}

	for idx, item := range inputs {
		recordingID, err := importOne(ctx, ossClient, recordingSvc, tenantStore, *assetTenantID, strings.TrimSpace(*templateCode), strings.TrimSpace(*version), *activate, *skipAssetUpsert, idx, item)
		if err != nil {
			log.Fatalf("import %s failed: %v", item.RoleCode, err)
		}
		log.Printf("imported %s demo recording: recording_id=%d", item.RoleCode, recordingID)
	}
}

func importOne(ctx context.Context, ossClient *ossutil.Client, recordingSvc *recording.Service, tenantStore *tenant.Store, assetTenantID int64, templateCode, version string, activate bool, skipAssetUpsert bool, sortOrder int, input assetInput) (int64, error) {
	body, err := os.ReadFile(input.FilePath)
	if err != nil {
		return 0, fmt.Errorf("read file: %w", err)
	}
	durationSeconds, err := detectDurationSeconds(ctx, input.FilePath)
	if err != nil {
		log.Printf("detect duration failed for %s: %v", input.FilePath, err)
	}
	ext := strings.ToLower(filepath.Ext(input.FilePath))
	if ext == "" {
		ext = ".m4a"
	}
	prefix := fmt.Sprintf("recordings/%d/trial-demo/%s", assetTenantID, time.Now().UTC().Format("2006/01/02"))
	ossKey := ossutil.GenerateObjectKey(prefix, ext)
	contentType := mime.TypeByExtension(ext)
	if strings.TrimSpace(contentType) == "" {
		contentType = "audio/mp4"
	}
	fileURL, err := ossClient.UploadBytes(ctx, ossKey, body, &ossutil.UploadOptions{ContentType: contentType})
	if err != nil {
		return 0, fmt.Errorf("upload oss: %w", err)
	}

	resp, _, err := recordingSvc.IngestOwnedAudioAndEnqueue(ctx, recording.OwnedAudioIngestRequest{
		TenantID:        assetTenantID,
		EmployeeID:      input.EmployeeID,
		FileURL:         fileURL,
		FileName:        filepath.Base(input.FilePath),
		MIMEType:        contentType,
		DurationSeconds: durationSeconds,
		OSSKey:          ossKey,
		Source:          "manual",
		BusinessScope:   input.RoleCode,
		Scene:           "consultation",
		TriggerSource:   "trial_demo_import",
		OrderNo:         fmt.Sprintf("trial_demo_%s_%s_%d", templateCode, input.RoleCode, time.Now().UnixNano()),
	})
	if err != nil {
		message := strings.ToLower(strings.TrimSpace(err.Error()))
		if strings.Contains(message, "transcription is already queued") {
			resolvedID, lookupErr := findLatestRecordingID(ctx, tenantStore, assetTenantID, filepath.Base(input.FilePath), input.EmployeeID)
			if lookupErr != nil {
				return 0, fmt.Errorf("ingest recording queued but lookup failed: %w", lookupErr)
			}
			resp = &recording.RecordingResponse{ID: resolvedID}
		} else {
			return 0, fmt.Errorf("ingest recording: %w", err)
		}
	}
	if !skipAssetUpsert {
		if err := upsertAsset(ctx, tenantStore, templateCode, version, activate, sortOrder, input, assetTenantID, resp.ID); err != nil {
			return 0, err
		}
	}
	return resp.ID, nil
}

var afinfoDurationPattern = regexp.MustCompile(`estimated duration:\s*([0-9]+(?:\.[0-9]+)?)\s*sec`)

func detectDurationSeconds(ctx context.Context, filePath string) (int, error) {
	output, err := exec.CommandContext(ctx, "afinfo", filePath).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("run afinfo: %w: %s", err, strings.TrimSpace(string(output)))
	}
	matches := afinfoDurationPattern.FindStringSubmatch(string(output))
	if len(matches) != 2 {
		return 0, fmt.Errorf("parse afinfo duration failed")
	}
	durationFloat, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration value: %w", err)
	}
	durationSeconds := int(durationFloat + 0.5)
	if durationSeconds < 0 {
		return 0, nil
	}
	return durationSeconds, nil
}

func upsertAsset(ctx context.Context, store *tenant.Store, templateCode, version string, activate bool, sortOrder int, input assetInput, assetTenantID, recordingID int64) error {
	meta, _ := json.Marshal(map[string]any{
		"imported_from": input.FilePath,
		"role_code":     input.RoleCode,
	})
	_, err := store.Pool().Exec(ctx, `
		INSERT INTO trial_demo_recording_assets (
			template_code, asset_code, role_code, title,
			source_tenant_id, source_recording_id, source_customer_id,
			version, is_active, sort_order, metadata, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, (SELECT customer_id FROM recordings WHERE id = $11), $7, $8, $9, $10::jsonb, NOW(), NOW())
		ON CONFLICT (template_code, asset_code)
		DO UPDATE SET
			role_code = EXCLUDED.role_code,
			title = EXCLUDED.title,
			source_tenant_id = EXCLUDED.source_tenant_id,
			source_recording_id = EXCLUDED.source_recording_id,
			source_customer_id = EXCLUDED.source_customer_id,
			version = EXCLUDED.version,
			is_active = EXCLUDED.is_active,
			sort_order = EXCLUDED.sort_order,
			metadata = EXCLUDED.metadata,
			updated_at = NOW()
	`, templateCode, input.AssetCode, input.RoleCode, input.Title, assetTenantID, recordingID, version, activate, sortOrder, string(meta), recordingID)
	if err != nil {
		return fmt.Errorf("upsert trial demo asset: %w", err)
	}
	return nil
}

func findLatestRecordingID(ctx context.Context, store *tenant.Store, tenantID int64, fileName string, employeeID int64) (int64, error) {
	var recordingID int64
	err := store.Pool().QueryRow(ctx, `
		SELECT id
		FROM recordings
		WHERE tenant_id = $1
		  AND employee_id = $2
		  AND file_name = $3
		ORDER BY id DESC
		LIMIT 1
	`, tenantID, employeeID, fileName).Scan(&recordingID)
	if err != nil {
		return 0, err
	}
	return recordingID, nil
}
