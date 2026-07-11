package recording

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	ossutil "github.com/freeasyman/lingce-api/pkg/oss"
)

const trialUploadMaxBytes = 100 * 1024 * 1024
const trialUploadMinDurationSeconds = 2 * 60
const trialUploadMaxDurationSeconds = 20 * 60

var trialUploadAllowedExtensions = map[string]struct{}{
	".mp3": {},
	".wav": {},
	".m4a": {},
}

func (h *Handler) UploadTrialRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if claims.UserType != auth.UserTypeAdmin && claims.UserType != auth.UserTypeEmployee && claims.UserType != auth.UserTypeMobile {
		httputil.WriteForbidden(w, "Access denied")
		return
	}
	if claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteBadRequest(w, "Invalid tenant")
		return
	}
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "recording_upload"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	tenantID := *claims.TenantID
	profile, err := h.service.store.GetTrialTenantProfile(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if !strings.EqualFold(strings.TrimSpace(profile.AccountMode), "trial") {
		httputil.WriteForbidden(w, "当前租户不是试用账号")
		return
	}
	if profile.ValidTo != nil && profile.ValidTo.Before(time.Now()) {
		httputil.WriteForbidden(w, "试用已过期")
		return
	}
	if profile.TrialUsedRecordings >= profile.TrialMaxRecordings {
		httputil.WriteBadRequest(w, "试用录音额度已用完")
		return
	}

	if err := r.ParseMultipartForm(trialUploadMaxBytes); err != nil {
		httputil.WriteBadRequest(w, "上传表单无效")
		return
	}
	role := strings.ToLower(strings.TrimSpace(r.FormValue("role")))
	if role != "doctor" && role != "consultant" {
		httputil.WriteBadRequest(w, "role 仅支持 doctor 或 consultant")
		return
	}
	var clientDurationSeconds *int
	if raw := strings.TrimSpace(r.FormValue("duration_seconds")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value <= 0 {
			httputil.WriteBadRequest(w, "duration_seconds 无效")
			return
		}
		clientDurationSeconds = &value
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		httputil.WriteBadRequest(w, "请上传音频文件")
		return
	}
	defer file.Close()

	declaredContentType, ext, err := validateTrialUploadFileHeader(fileHeader)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	payload, err := io.ReadAll(io.LimitReader(file, trialUploadMaxBytes+1))
	if err != nil {
		httputil.WriteInternalError(w, "读取文件失败")
		return
	}
	if int64(len(payload)) > trialUploadMaxBytes {
		httputil.WriteBadRequest(w, "文件过大，请上传 100MB 以内录音")
		return
	}
	contentType, err := validateTrialUploadPayload(ext, declaredContentType, payload)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	actualDurationSeconds, err := detectTrialAudioDurationSeconds(ext, payload)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if actualDurationSeconds < trialUploadMinDurationSeconds {
		httputil.WriteBadRequest(w, "录音时间过短，预计无法充分分析出有效内容。请更换一段大于等于 2 分钟且小于等于 20 分钟的录音后再试，本次不会占用试用额度。")
		return
	}
	if actualDurationSeconds > trialUploadMaxDurationSeconds {
		httputil.WriteBadRequest(w, "录音时长超出试用标准。请更换一段大于等于 2 分钟且小于等于 20 分钟的录音后再试，本次不会占用试用额度。")
		return
	}
	slog.Info("trial upload duration parsed",
		"tenant_id", tenantID,
		"role", role,
		"file_name", strings.TrimSpace(fileHeader.Filename),
		"client_duration_seconds", intValue(clientDurationSeconds),
		"actual_duration_seconds", actualDurationSeconds,
	)

	employeeID, err := h.resolveTrialUploadEmployeeID(r.Context(), tenantID, role)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	ossClient, err := ossutil.NewClient(
		h.ossConfig.Endpoint,
		h.ossConfig.AccessKeyID,
		h.ossConfig.AccessKeySecret,
		h.ossConfig.Bucket,
		h.ossConfig.PublicBaseURL,
	)
	if err != nil {
		httputil.WriteInternalError(w, fmt.Sprintf("初始化存储失败: %v", err))
		return
	}
	prefix := fmt.Sprintf("recordings/%d/trial/%s", tenantID, time.Now().Format("2006/01/02"))
	objectKey := ossutil.GenerateObjectKey(prefix, ext)
	fileURL, err := ossClient.UploadBytes(r.Context(), objectKey, payload, &ossutil.UploadOptions{
		ContentType: contentType,
	})
	if err != nil {
		httputil.WriteInternalError(w, fmt.Sprintf("上传录音失败: %v", err))
		return
	}

	recording, created, err := h.service.IngestOwnedAudioAndEnqueue(r.Context(), OwnedAudioIngestRequest{
		TenantID:        tenantID,
		EmployeeID:      employeeID,
		FileURL:         fileURL,
		FileName:        strings.TrimSpace(fileHeader.Filename),
		MIMEType:        contentType,
		DurationSeconds: actualDurationSeconds,
		OSSKey:          objectKey,
		Source:          "manual",
		BusinessScope:   role,
		Scene:           "consultation",
		TriggerSource:   "trial_upload",
	})
	if err != nil {
		if shouldRollbackTrialUploadObject(created, recording) {
			if deleteErr := ossClient.DeleteFile(r.Context(), objectKey); deleteErr != nil {
				slog.Error("trial upload rollback failed",
					"tenant_id", tenantID,
					"role", role,
					"object_key", objectKey,
					"error", deleteErr,
				)
			}
		}
		if IsRecordingValidationError(err) {
			message := strings.ToLower(strings.TrimSpace(err.Error()))
			if strings.Contains(message, "transcription is already queued") {
				httputil.WriteSuccess(w, map[string]any{
					"recording_id":    recording.ID,
					"role":            role,
					"analysis_status": "queued",
					"redirect_url":    trialUploadRedirectURL(role),
				})
				return
			}
			var code string = "BAD_REQUEST"
			var validationErr interface{ Code() string }
			if errors.As(err, &validationErr) {
				code = validationErr.Code()
			}
			httputil.WriteError(w, http.StatusBadRequest, code, err.Error(), map[string]any{
				"recording_id": recording.ID,
				"role":         role,
			})
			return
		}
		if IsWorkerUnavailable(err) {
			httputil.WriteError(w, http.StatusServiceUnavailable, "WORKER_UNAVAILABLE", "录音已上传，但分析服务暂不可用，请稍后重试", map[string]any{
				"recording_id":    recording.ID,
				"role":            role,
				"analysis_status": "pending",
				"redirect_url":    trialUploadRedirectURL(role),
			})
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]any{
		"recording_id":    recording.ID,
		"role":            role,
		"analysis_status": "queued",
		"redirect_url":    trialUploadRedirectURL(role),
	})
}

func validateTrialUploadFileHeader(fileHeader *multipart.FileHeader) (string, string, error) {
	fileName := strings.TrimSpace(fileHeader.Filename)
	ext := strings.ToLower(filepath.Ext(fileName))
	if _, ok := trialUploadAllowedExtensions[ext]; !ok {
		return "", "", fmt.Errorf("仅支持 mp3、wav、m4a 格式")
	}
	contentType := strings.ToLower(strings.TrimSpace(fileHeader.Header.Get("Content-Type")))
	if contentType == "" {
		switch ext {
		case ".mp3":
			contentType = "audio/mpeg"
		case ".wav":
			contentType = "audio/wav"
		case ".m4a":
			contentType = "audio/mp4"
		}
	}
	return contentType, ext, nil
}

func validateTrialUploadPayload(ext, declaredContentType string, payload []byte) (string, error) {
	sniffedType, ok := sniffTrialAudioType(payload)
	if !ok {
		return "", fmt.Errorf("文件格式无法识别，请上传 mp3、wav、m4a 音频文件")
	}
	if expected := expectedAudioTypeForExtension(ext); expected != "" && sniffedType != expected {
		return "", fmt.Errorf("文件内容与扩展名不匹配，请确认上传的是正确的 mp3、wav、m4a 音频文件")
	}
	if declared := strings.ToLower(strings.TrimSpace(declaredContentType)); declared != "" && declared != "application/octet-stream" {
		if !strings.HasPrefix(declared, "audio/") && !(ext == ".m4a" && declared == "video/mp4") {
			return "", fmt.Errorf("请上传音频文件")
		}
	}
	return sniffedType, nil
}

func expectedAudioTypeForExtension(ext string) string {
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	default:
		return ""
	}
}

func sniffTrialAudioType(payload []byte) (string, bool) {
	if isWAVPayload(payload) {
		return "audio/wav", true
	}
	if isM4APayload(payload) {
		return "audio/mp4", true
	}
	if isMP3Payload(payload) {
		return "audio/mpeg", true
	}
	return "", false
}

func isWAVPayload(payload []byte) bool {
	return len(payload) >= 12 &&
		bytes.Equal(payload[0:4], []byte("RIFF")) &&
		bytes.Equal(payload[8:12], []byte("WAVE"))
}

func isM4APayload(payload []byte) bool {
	if len(payload) < 12 {
		return false
	}
	searchLimit := len(payload)
	if searchLimit > 64 {
		searchLimit = 64
	}
	for i := 0; i+12 <= searchLimit; i++ {
		if !bytes.Equal(payload[i+4:i+8], []byte("ftyp")) {
			continue
		}
		brand := string(payload[i+8 : i+12])
		switch brand {
		case "M4A ", "M4B ", "isom", "iso2", "mp41", "mp42", "qt  ":
			return true
		}
	}
	return false
}

func isMP3Payload(payload []byte) bool {
	if len(payload) >= 3 && bytes.Equal(payload[0:3], []byte("ID3")) {
		return true
	}
	searchLimit := len(payload)
	if searchLimit > 4096 {
		searchLimit = 4096
	}
	for i := 0; i+1 < searchLimit; i++ {
		if payload[i] != 0xFF {
			continue
		}
		next := payload[i+1]
		if next&0xE0 == 0xE0 && next != 0xFF && next != 0x00 {
			return true
		}
	}
	return false
}

func detectTrialAudioDurationSeconds(ext string, payload []byte) (int, error) {
	var (
		duration float64
		err      error
	)
	switch ext {
	case ".wav":
		duration, err = parseWAVDurationSeconds(payload)
	case ".m4a":
		duration, err = parseM4ADurationSeconds(payload)
	case ".mp3":
		duration, err = parseMP3DurationSeconds(payload)
	default:
		err = fmt.Errorf("仅支持 mp3、wav、m4a 格式")
	}
	if err != nil {
		return 0, err
	}
	if !isFinitePositiveDuration(duration) {
		return 0, fmt.Errorf("未能正确读取录音时长，请更换文件或格式后重试")
	}
	return int(math.Round(duration)), nil
}

func parseWAVDurationSeconds(payload []byte) (float64, error) {
	if len(payload) < 44 || !isWAVPayload(payload) {
		return 0, fmt.Errorf("wav 文件内容损坏，请更换文件后重试")
	}
	var (
		offset        = 12
		byteRate      uint32
		dataChunkSize uint32
	)
	for offset+8 <= len(payload) {
		chunkID := string(payload[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(payload[offset+4 : offset+8]))
		offset += 8
		if chunkSize < 0 || offset+chunkSize > len(payload) {
			return 0, fmt.Errorf("wav 文件内容损坏，请更换文件后重试")
		}
		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				return 0, fmt.Errorf("wav 文件内容损坏，请更换文件后重试")
			}
			byteRate = binary.LittleEndian.Uint32(payload[offset+8 : offset+12])
		case "data":
			dataChunkSize = uint32(chunkSize)
		}
		offset += chunkSize
		if chunkSize%2 == 1 && offset < len(payload) {
			offset++
		}
	}
	if byteRate == 0 || dataChunkSize == 0 {
		return 0, fmt.Errorf("未能正确读取录音时长，请更换文件或格式后重试")
	}
	return float64(dataChunkSize) / float64(byteRate), nil
}

func parseM4ADurationSeconds(payload []byte) (float64, error) {
	duration, ok := findM4AMdhdDuration(payload, 0, len(payload))
	if !ok {
		return 0, fmt.Errorf("未能正确读取录音时长，请更换文件或格式后重试")
	}
	return duration, nil
}

func findM4AMdhdDuration(payload []byte, start, end int) (float64, bool) {
	offset := start
	for offset+8 <= end {
		size := int(binary.BigEndian.Uint32(payload[offset : offset+4]))
		headerSize := 8
		if size == 1 {
			if offset+16 > end {
				return 0, false
			}
			size64 := binary.BigEndian.Uint64(payload[offset+8 : offset+16])
			if size64 < 16 || size64 > uint64(end-offset) {
				return 0, false
			}
			size = int(size64)
			headerSize = 16
		} else if size == 0 {
			size = end - offset
		}
		if size < headerSize || offset+size > end {
			return 0, false
		}
		boxType := string(payload[offset+4 : offset+8])
		boxStart := offset + headerSize
		boxEnd := offset + size
		if boxType == "mdhd" {
			if boxStart+4 > boxEnd {
				return 0, false
			}
			version := payload[boxStart]
			if version == 1 {
				if boxStart+32 > boxEnd {
					return 0, false
				}
				timescale := binary.BigEndian.Uint32(payload[boxStart+20 : boxStart+24])
				duration := binary.BigEndian.Uint64(payload[boxStart+24 : boxStart+32])
				if timescale == 0 || duration == 0 {
					return 0, false
				}
				return float64(duration) / float64(timescale), true
			}
			if boxStart+20 > boxEnd {
				return 0, false
			}
			timescale := binary.BigEndian.Uint32(payload[boxStart+12 : boxStart+16])
			duration := binary.BigEndian.Uint32(payload[boxStart+16 : boxStart+20])
			if timescale == 0 || duration == 0 {
				return 0, false
			}
			return float64(duration) / float64(timescale), true
		}
		if isM4AContainerBox(boxType) {
			if duration, ok := findM4AMdhdDuration(payload, boxStart, boxEnd); ok {
				return duration, true
			}
		}
		offset += size
	}
	return 0, false
}

func isM4AContainerBox(boxType string) bool {
	switch boxType {
	case "moov", "trak", "mdia", "minf", "stbl", "edts", "udta":
		return true
	default:
		return false
	}
}

func parseMP3DurationSeconds(payload []byte) (float64, error) {
	offset := skipID3v2Tag(payload)
	var totalSeconds float64
	var frames int
	for offset < len(payload) {
		if hasID3v1Tag(payload, offset) {
			break
		}
		headerOffset, ok := findNextMP3FrameHeader(payload, offset)
		if !ok {
			break
		}
		frame, ok := parseMP3FrameHeader(payload[headerOffset:])
		if !ok {
			offset = headerOffset + 1
			continue
		}
		if headerOffset+frame.frameLength > len(payload) {
			return 0, fmt.Errorf("mp3 文件内容损坏，请更换文件后重试")
		}
		totalSeconds += float64(frame.samplesPerFrame) / float64(frame.sampleRate)
		frames++
		offset = headerOffset + frame.frameLength
	}
	if frames == 0 || totalSeconds <= 0 {
		return 0, fmt.Errorf("未能正确读取录音时长，请更换文件或格式后重试")
	}
	return totalSeconds, nil
}

func skipID3v2Tag(payload []byte) int {
	if len(payload) < 10 || !bytes.Equal(payload[:3], []byte("ID3")) {
		return 0
	}
	size := int(payload[6]&0x7F)<<21 | int(payload[7]&0x7F)<<14 | int(payload[8]&0x7F)<<7 | int(payload[9]&0x7F)
	offset := 10 + size
	if payload[5]&0x10 != 0 {
		offset += 10
	}
	if offset > len(payload) {
		return len(payload)
	}
	return offset
}

func hasID3v1Tag(payload []byte, offset int) bool {
	return len(payload)-offset >= 128 && bytes.Equal(payload[offset:offset+3], []byte("TAG"))
}

func findNextMP3FrameHeader(payload []byte, start int) (int, bool) {
	for i := start; i+4 <= len(payload); i++ {
		if _, ok := parseMP3FrameHeader(payload[i:]); ok {
			return i, true
		}
	}
	return 0, false
}

type mp3FrameInfo struct {
	frameLength     int
	sampleRate      int
	samplesPerFrame int
}

func parseMP3FrameHeader(payload []byte) (mp3FrameInfo, bool) {
	if len(payload) < 4 || payload[0] != 0xFF || payload[1]&0xE0 != 0xE0 {
		return mp3FrameInfo{}, false
	}
	versionID := (payload[1] >> 3) & 0x03
	layerIndex := (payload[1] >> 1) & 0x03
	bitrateIndex := (payload[2] >> 4) & 0x0F
	sampleRateIndex := (payload[2] >> 2) & 0x03
	padding := (payload[2] >> 1) & 0x01
	if versionID == 0x01 || layerIndex == 0x00 || bitrateIndex == 0x00 || bitrateIndex == 0x0F || sampleRateIndex == 0x03 {
		return mp3FrameInfo{}, false
	}
	version := mp3VersionName(versionID)
	layer := 4 - int(layerIndex)
	bitrate := mp3BitrateKbps(version, layer, int(bitrateIndex))
	sampleRate := mp3SampleRate(versionID, int(sampleRateIndex))
	samplesPerFrame := mp3SamplesPerFrame(version, layer)
	if bitrate <= 0 || sampleRate <= 0 || samplesPerFrame <= 0 {
		return mp3FrameInfo{}, false
	}
	frameLength := mp3FrameLength(version, layer, bitrate, sampleRate, int(padding))
	if frameLength <= 0 {
		return mp3FrameInfo{}, false
	}
	return mp3FrameInfo{
		frameLength:     frameLength,
		sampleRate:      sampleRate,
		samplesPerFrame: samplesPerFrame,
	}, true
}

func mp3VersionName(versionID byte) string {
	switch versionID {
	case 0x03:
		return "1"
	case 0x02:
		return "2"
	case 0x00:
		return "2.5"
	default:
		return ""
	}
}

func mp3SampleRate(versionID byte, index int) int {
	table := map[byte][3]int{
		0x03: {44100, 48000, 32000},
		0x02: {22050, 24000, 16000},
		0x00: {11025, 12000, 8000},
	}
	values, ok := table[versionID]
	if !ok || index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func mp3BitrateKbps(version string, layer, index int) int {
	if index <= 0 || index >= 15 {
		return 0
	}
	var table []int
	switch {
	case version == "1" && layer == 1:
		table = []int{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448}
	case version == "1" && layer == 2:
		table = []int{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384}
	case version == "1" && layer == 3:
		table = []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	case version != "1" && layer == 1:
		table = []int{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256}
	case version != "1" && (layer == 2 || layer == 3):
		table = []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
	default:
		return 0
	}
	return table[index]
}

func mp3SamplesPerFrame(version string, layer int) int {
	switch layer {
	case 1:
		return 384
	case 2:
		return 1152
	case 3:
		if version == "1" {
			return 1152
		}
		return 576
	default:
		return 0
	}
}

func mp3FrameLength(version string, layer, bitrateKbps, sampleRate, padding int) int {
	bitrate := bitrateKbps * 1000
	switch layer {
	case 1:
		return ((12 * bitrate / sampleRate) + padding) * 4
	case 2:
		return (144*bitrate)/sampleRate + padding
	case 3:
		if version == "1" {
			return (144*bitrate)/sampleRate + padding
		}
		return (72*bitrate)/sampleRate + padding
	default:
		return 0
	}
}

func isFinitePositiveDuration(duration float64) bool {
	return !math.IsNaN(duration) && !math.IsInf(duration, 0) && duration > 0
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func shouldRollbackTrialUploadObject(created bool, recording *RecordingResponse) bool {
	return recording == nil || recording.ID <= 0
}

func (h *Handler) resolveTrialUploadEmployeeID(ctx context.Context, tenantID int64, role string) (int64, error) {
	switch role {
	case "doctor":
		return h.service.store.GetTrialEmployeeID(ctx, tenantID, "trial_doctor")
	case "consultant":
		return h.service.store.GetTrialEmployeeID(ctx, tenantID, "trial_consultant")
	default:
		return 0, fmt.Errorf("unsupported role: %s", role)
	}
}

func trialUploadRedirectURL(role string) string {
	if role == "doctor" {
		return "/doctor-recordings"
	}
	return "/consultant-recordings"
}

func stringPtr(v string) *string {
	return &v
}

// Advanced Recording Handlers

// GetStatsByTenant handles getting statistics by tenant
func (h *Handler) GetStatsByTenant(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	if claims.UserType == auth.UserTypeAdmin {
		rows, err := h.service.store.pool.Query(r.Context(), `
			SELECT t.id, t.name, COUNT(mr.id), COALESCE(SUM(mr.duration), 0)
			FROM tenants t
			LEFT JOIN recordings mr ON mr.tenant_id = t.id
			GROUP BY t.id, t.name
			ORDER BY COUNT(mr.id) DESC
		`)
		if err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		defer rows.Close()
		var items []RecordingStatsByTenantResponse
		for rows.Next() {
			var item RecordingStatsByTenantResponse
			if err := rows.Scan(&item.TenantID, &item.TenantName, &item.Count, &item.Duration); err != nil {
				httputil.WriteInternalError(w, err.Error())
				return
			}
			items = append(items, item)
		}
		httputil.WriteSuccess(w, items)
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	stats, err := h.service.store.GetStatsOverview(r.Context(), tenantID, nil, nil)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, []RecordingStatsByTenantResponse{{
		TenantID: tenantID,
		Count:    stats.TotalRecordings,
		Duration: stats.TotalDuration,
	}})
}

// GetDurationDistribution handles getting duration distribution
func (h *Handler) GetDurationDistribution(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	rows, err := h.service.store.pool.Query(r.Context(), `
		SELECT
			CASE
				WHEN COALESCE(duration, 0) < 300 THEN '0-5min'
				WHEN COALESCE(duration, 0) < 600 THEN '5-10min'
				WHEN COALESCE(duration, 0) < 1200 THEN '10-20min'
				ELSE '20min+'
			END AS duration_range,
			COUNT(*) AS cnt
		FROM recordings
		WHERE tenant_id = $1
		GROUP BY duration_range
		ORDER BY cnt DESC
	`, tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	defer rows.Close()

	var (
		total int64
		raw   []DurationDistributionResponse
	)
	for rows.Next() {
		var item DurationDistributionResponse
		if err := rows.Scan(&item.Range, &item.Count); err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		total += item.Count
		raw = append(raw, item)
	}
	for i := range raw {
		if total > 0 {
			raw[i].Percentage = float64(raw[i].Count) / float64(total) * 100
		}
	}
	httputil.WriteSuccess(w, raw)
}

// GetDailyStats handles getting daily statistics
func (h *Handler) GetDailyStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	rows, err := h.service.store.pool.Query(r.Context(), `
		SELECT DATE(created_at), COUNT(*), COALESCE(SUM(duration), 0)
		FROM recordings
		WHERE tenant_id = $1 AND created_at >= NOW() - INTERVAL '30 days'
		GROUP BY DATE(created_at)
		ORDER BY DATE(created_at) ASC
	`, tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	defer rows.Close()
	var items []DailyStatsResponse
	for rows.Next() {
		var (
			dt   time.Time
			item DailyStatsResponse
		)
		if err := rows.Scan(&dt, &item.Count, &item.Duration); err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		item.Date = dt.Format("2006-01-02")
		items = append(items, item)
	}
	httputil.WriteSuccess(w, items)
}

// UploadRecording handles uploading recording
func (h *Handler) UploadRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateRecordingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil {
			httputil.WriteForbidden(w, "No tenant access")
			return
		}
		req.TenantID = *claims.TenantID
		req.EmployeeID = claims.UserID
	}
	if req.RecordingURL == "" {
		httputil.WriteBadRequest(w, "recording_url is required")
		return
	}
	rec, err := h.service.CreateRecording(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, rec)
}

func (h *Handler) GetTrialAgreementStatus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	status, err := h.service.store.GetTrialAgreementStatus(r.Context(), *claims.TenantID, "trial_privacy_notice", "v1")
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, status)
}

func (h *Handler) AcceptTrialAgreement(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req AcceptTrialAgreementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if err := h.service.store.AcceptTrialAgreement(r.Context(), *claims.TenantID, claims.UserID, req.AgreementType, req.AgreementVersion); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	status, err := h.service.store.GetTrialAgreementStatus(r.Context(), *claims.TenantID, req.AgreementType, req.AgreementVersion)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, status)
}

// GetPlayURL handles getting play URL
func (h *Handler) GetPlayURL(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	ref, err := h.service.store.GetRecordingMediaRef(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		accessRef, accessErr := h.service.store.GetRecordingAccessRef(r.Context(), id)
		if accessErr != nil {
			httputil.WriteNotFound(w, accessErr.Error())
			return
		}
		if claims.TenantID == nil || *claims.TenantID != accessRef.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
		if err := h.service.ValidateBusinessScopeAccess(r.Context(), claims.UserType, claims.UserID, accessRef.BusinessScope); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
	}
	ossEndpoint := strings.TrimSpace(h.ossConfig.Endpoint)
	ossBucket := strings.TrimSpace(h.ossConfig.Bucket)
	ossAccessKeyID := strings.TrimSpace(h.ossConfig.AccessKeyID)
	ossAccessKeySecret := strings.TrimSpace(h.ossConfig.AccessKeySecret)
	requireOwned := h.playURLRequireOwned

	if strings.TrimSpace(ref.OSSKey) == "" && strings.TrimSpace(ref.FileURL) != "" && ossEndpoint != "" && ossBucket != "" && ossAccessKeyID != "" && ossAccessKeySecret != "" {
		refreshed, refreshErr := h.migrateRecordingMediaToOwnedStorage(r.Context(), ref)
		if refreshErr == nil && refreshed != nil {
			ref = refreshed
		}
	}
	if strings.TrimSpace(ref.OSSKey) != "" && ossEndpoint != "" && ossBucket != "" && ossAccessKeyID != "" && ossAccessKeySecret != "" {
		client, cErr := ossutil.NewClient(ossEndpoint, ossAccessKeyID, ossAccessKeySecret, ossBucket, strings.TrimSpace(h.ossConfig.PublicBaseURL))
		if cErr != nil {
			httputil.WriteInternalError(w, "failed to init oss client")
			return
		}
		signedURL, sErr := client.GetSignedURL(ref.OSSKey, 3600)
		if sErr != nil {
			httputil.WriteInternalError(w, "failed to sign play url")
			return
		}
		httputil.WriteSuccess(w, PlayURLResponse{
			URL:       signedURL,
			ExpiresAt: time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
		return
	}
	if requireOwned {
		httputil.WriteError(w, http.StatusPreconditionFailed, "MEDIA_NOT_MIGRATED", "recording media has not been migrated to owned storage", map[string]any{"recording_id": id})
		return
	}
	if strings.TrimSpace(ref.FileURL) == "" {
		httputil.WriteError(w, http.StatusNotFound, "PLAY_URL_NOT_FOUND", "recording file url is empty", map[string]any{"recording_id": id})
		return
	}
	httputil.WriteSuccess(w, PlayURLResponse{
		URL:       ref.FileURL,
		ExpiresAt: time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})
}

func (h *Handler) migrateRecordingMediaToOwnedStorage(ctx context.Context, ref *RecordingMediaRef) (*RecordingMediaRef, error) {
	if ref == nil {
		return nil, fmt.Errorf("recording media ref is nil")
	}
	sourceURL := strings.TrimSpace(ref.FileURL)
	if sourceURL == "" {
		return nil, fmt.Errorf("source url is empty")
	}
	clientOSS, err := ossutil.NewClient(
		strings.TrimSpace(h.ossConfig.Endpoint),
		strings.TrimSpace(h.ossConfig.AccessKeyID),
		strings.TrimSpace(h.ossConfig.AccessKeySecret),
		strings.TrimSpace(h.ossConfig.Bucket),
		strings.TrimSpace(h.ossConfig.PublicBaseURL),
	)
	if err != nil {
		return nil, fmt.Errorf("init oss client: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build source request: %w", err)
	}
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("download source audio: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download source audio status=%d", resp.StatusCode)
	}
	maxBytes := int64(150 * 1024 * 1024)
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read source audio: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("source audio exceeds max bytes")
	}

	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(ref.FileName)))
	if ext == "" {
		ext = ".mp3"
	}
	ossKey := ossutil.GenerateObjectKey(fmt.Sprintf("recordings/%d/%s", ref.TenantID, time.Now().UTC().Format("2006/01/02")), ext)
	ownedURL, err := clientOSS.UploadBytes(ctx, ossKey, body, &ossutil.UploadOptions{
		ContentType: "audio/mpeg",
		Metadata: map[string]string{
			"source-recording-id": strconv.FormatInt(ref.RecordingID, 10),
			"source-url":          sourceURL,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("upload source audio to oss: %w", err)
	}
	if err := h.service.store.UpdateRecordingMediaRef(ctx, ref.RecordingID, ownedURL, ossKey); err != nil {
		return nil, err
	}
	ref.FileURL = ownedURL
	ref.OSSKey = ossKey
	return ref, nil
}

// TestPlayback handles testing playback
func (h *Handler) TestPlayback(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	rec, err := h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	status := "ok"
	if rec.RecordingURL == "" {
		status = "invalid_url"
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"status": status,
		"url":    rec.RecordingURL,
	})
}

// ReanalyzeRecording triggers re-analysis for a recording resource.
func (h *Handler) ReanalyzeRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	jobID, err := h.service.ReanalyzeRecording(r.Context(), id)
	if err != nil {
		if IsRecordingValidationError(err) {
			code := "BAD_REQUEST"
			var validationErr interface{ Code() string }
			if errors.As(err, &validationErr) {
				code = validationErr.Code()
			}
			httputil.WriteError(w, http.StatusBadRequest, code, err.Error(), map[string]any{"recording_id": id})
			return
		}
		if IsWorkerUnavailable(err) {
			httputil.WriteError(w, http.StatusServiceUnavailable, "WORKER_UNAVAILABLE", "recording worker unavailable, please retry", nil)
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{
		"message": "Reanalysis triggered",
		"job_id":  jobID,
	})
}

// TriggerTranscribe handles triggering transcription
func (h *Handler) TriggerTranscribe(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	var req TriggerTranscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	jobID, err := h.service.TriggerTranscribe(r.Context(), id, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{
		"message": "Transcription triggered",
		"job_id":  jobID,
	})
}

// TriggerAnalyze handles triggering analysis
func (h *Handler) TriggerAnalyze(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	var req TriggerAnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	jobID, err := h.service.TriggerAnalyze(r.Context(), id, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{
		"message": "Analysis triggered",
		"job_id":  jobID,
	})
}

// TriggerClean handles triggering cleaning
func (h *Handler) TriggerClean(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	var req TriggerCleanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	jobID, err := h.service.TriggerClean(r.Context(), id, req)
	if err != nil {
		if IsWorkerUnavailable(err) {
			httputil.WriteError(w, http.StatusServiceUnavailable, "WORKER_UNAVAILABLE", "recording worker unavailable, please retry", nil)
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{
		"message": "Cleaning triggered",
		"job_id":  jobID,
	})
}

// GetAnalysisResult handles getting analysis result
func (h *Handler) GetAnalysisResult(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	result, err := h.service.GetAnalysisResult(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, result)
}

// DispatchFollowUpTasks handles dispatching follow-up tasks
func (h *Handler) DispatchFollowUpTasks(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	rec, err := h.service.store.GetRecordingByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	title := "录音跟进任务"
	desc := "请基于录音分析结果执行后续跟进"
	_, err = h.service.store.pool.Exec(r.Context(), `
		INSERT INTO recording_tasks (
			tenant_id, recording_id, customer_id, customer_name, customer_phone,
			title, description, assigned_to, assigned_by, status, priority, due_at, source_type, created_at, updated_at
		)
		SELECT
			r.tenant_id,
			r.id,
			COALESCE(r.customer_id, 0),
			COALESCE(c.name, ''),
			COALESCE(c.phone, ''),
			$2,
			$3,
			r.employee_id,
			$4::text,
			'pending',
			'medium',
			NOW() + INTERVAL '24 hours',
			'manual',
			NOW(),
			NOW()
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE r.id = $1
	`, rec.ID, title, desc, claims.UserID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Follow-up tasks dispatched"})
}

// SubmitAnalysisFeedback handles submitting analysis feedback
func (h *Handler) SubmitAnalysisFeedback(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	var req AnalysisFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	comment := ""
	if req.Comment != nil {
		comment = *req.Comment
	}
	msg := "analysis feedback"
	if comment != "" {
		msg = msg + ": " + comment
	}
	processingError := msg
	if _, err := h.service.store.UpdateRecording(r.Context(), id, UpdateRecordingRequest{
		ProcessingError: &processingError,
	}); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Feedback submitted"})
}

// BatchTranscribe handles batch transcription
func (h *Handler) BatchTranscribe(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req BatchTranscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if len(req.RecordingIDs) == 0 {
		httputil.WriteBadRequest(w, "recording_ids is required")
		return
	}
	results := map[string]interface{}{
		"success": 0,
		"failed":  0,
		"jobs":    map[int64]string{},
	}
	jobs := results["jobs"].(map[int64]string)
	for _, rid := range req.RecordingIDs {
		jobID, err := h.service.TriggerTranscribe(r.Context(), rid, TriggerTranscribeRequest{})
		if err != nil {
			results["failed"] = results["failed"].(int) + 1
			continue
		}
		results["success"] = results["success"].(int) + 1
		jobs[rid] = jobID
	}
	httputil.WriteSuccess(w, results)
}

// BatchDelete handles batch deletion
func (h *Handler) BatchDelete(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req BatchDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if len(req.RecordingIDs) == 0 {
		httputil.WriteBadRequest(w, "recording_ids is required")
		return
	}
	var success, failed int
	for _, rid := range req.RecordingIDs {
		if err := h.service.DeleteRecording(r.Context(), rid); err != nil {
			failed++
			continue
		}
		success++
	}
	httputil.WriteSuccess(w, map[string]int{"success": success, "failed": failed})
}

// GetLearningRecommendation handles getting learning recommendation
func (h *Handler) GetLearningRecommendation(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	result, err := h.service.GetAnalysisResult(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	recs := []RecommendationItem{
		{
			Title:       "复盘关键对话片段",
			Description: "聚焦高价值沟通节点，形成标准话术。",
			Priority:    "high",
		},
	}
	if result.DoctorSummary != nil {
		recs = append(recs, RecommendationItem{
			Title:       "结合医生总结优化提问顺序",
			Description: *result.DoctorSummary,
			Priority:    "medium",
		})
	}
	httputil.WriteSuccess(w, LearningRecommendationResponse{Recommendations: recs})
}

// ConfirmFollowUpAction handles confirming follow-up action
func (h *Handler) ConfirmFollowUpAction(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	var req ConfirmActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.Action == "" {
		httputil.WriteBadRequest(w, "action is required")
		return
	}
	note := req.Action
	if req.Notes != nil {
		note = note + ": " + *req.Notes
	}
	result, err := h.service.store.pool.Exec(r.Context(), `
		UPDATE recording_tasks
		SET status = 'completed', completed_at = NOW(), feedback = CONCAT(COALESCE(feedback, ''), CASE WHEN COALESCE(feedback, '') = '' THEN '' ELSE E'\n' END, 'completed_by=', $1::text, E'\n', 'action_note=', $2), updated_at = NOW()
		WHERE recording_id = $3 AND source_type = 'follow_up' AND status IN ('pending', 'assigned')
	`, claims.UserID, note, id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		httputil.WriteNotFound(w, "No pending follow-up tasks found for this recording")
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Follow-up action confirmed"})
}

// GenerateOpeningScript handles generating opening script
func (h *Handler) GenerateOpeningScript(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	rec, err := h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	script := "您好，我是灵策助理，今天我们围绕您的情况做一个简短沟通。"
	if rec.PatientName != "" {
		script = "您好，" + rec.PatientName + "，我是灵策助理，今天我们围绕您的情况做一个简短沟通。"
	}
	httputil.WriteSuccess(w, map[string]string{"script": script})
}

// GenerateOperationsPlan handles generating operations plan
func (h *Handler) GenerateOperationsPlan(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	rec, err := h.service.store.GetRecordingByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || *claims.TenantID != rec.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
	}

	var req GenerateOperationsPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	requestPayload := map[string]interface{}{
		"recording_id": rec.ID,
		"tenant_id":    rec.TenantID,
		"context":      req.Context,
	}
	requestPayloadJSON, _ := json.Marshal(requestPayload)
	idempotencyKey := r.Header.Get("Idempotency-Key")

	var jobID int64
	if err := h.service.store.pool.QueryRow(r.Context(), `
		INSERT INTO recording_operations_plan_jobs (
			recording_id, tenant_id, requested_by, status, request_payload, result_payload,
			idempotency_key, started_at, completed_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, 'pending', $4::jsonb, NULL, NULLIF($5, ''), NULL, NULL, NOW(), NOW())
		RETURNING id
	`, rec.ID, rec.TenantID, claims.UserID, string(requestPayloadJSON), idempotencyKey).Scan(&jobID); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if _, err := h.service.enqueueLingceWorkerJob(r.Context(), rec.ID, "ops_plan", "manual_ops_plan"); err != nil {
		_, _ = h.service.store.pool.Exec(r.Context(), `
			UPDATE recording_operations_plan_jobs
			SET status = 'failed', error_message = $2, updated_at = NOW()
			WHERE id = $1
		`, jobID, err.Error())
		if IsWorkerUnavailable(err) {
			httputil.WriteError(w, http.StatusServiceUnavailable, "WORKER_UNAVAILABLE", "recording worker unavailable, please retry", nil)
			return
		}
		if IsRecordingValidationError(err) {
			httputil.WriteError(w, http.StatusBadRequest, "OPS_PLAN_REJECTED", err.Error(), nil)
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"job_id": strconv.FormatInt(jobID, 10)})
}

// GetOperationsPlanJobStatus handles getting operations plan job status
func (h *Handler) GetOperationsPlanJobStatus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	jobID := r.PathValue("job_id")
	if jobID == "" {
		httputil.WriteBadRequest(w, "Job ID is required")
		return
	}

	jobIDInt, err := strconv.ParseInt(jobID, 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid job ID")
		return
	}

	rec, err := h.service.store.GetRecordingByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || *claims.TenantID != rec.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
	}

	var (
		status       string
		resultJSON   []byte
		errorMessage *string
		updatedAt    time.Time
	)
	err = h.service.store.pool.QueryRow(r.Context(), `
		SELECT status, COALESCE(result_payload::text, '{}')::jsonb, error_message, updated_at
		FROM recording_operations_plan_jobs
		WHERE id = $1 AND recording_id = $2
	`, jobIDInt, id).Scan(&status, &resultJSON, &errorMessage, &updatedAt)
	if err != nil {
		httputil.WriteNotFound(w, "Operations plan job not found")
		return
	}

	if status != "completed" {
		var operationsPlanJSON []byte
		checkErr := h.service.store.pool.QueryRow(r.Context(), `
			SELECT COALESCE(analysis_result->'operations_plan', '{}'::jsonb)::text::jsonb
			FROM recordings
			WHERE id = $1
			  AND analysis_result IS NOT NULL
			  AND analysis_result ? 'operations_plan'
		`, id).Scan(&operationsPlanJSON)
		if checkErr == nil && len(operationsPlanJSON) > 0 {
			_, _ = h.service.store.pool.Exec(r.Context(), `
				UPDATE recording_operations_plan_jobs
				SET status = 'completed',
				    result_payload = $2::jsonb,
				    completed_at = NOW(),
				    updated_at = NOW()
				WHERE id = $1
			`, jobIDInt, string(operationsPlanJSON))
			status = "completed"
			resultJSON = operationsPlanJSON
			errorMessage = nil
			updatedAt = time.Now()
		}
	}

	var result interface{}
	_ = json.Unmarshal(resultJSON, &result)
	httputil.WriteSuccess(w, map[string]interface{}{
		"job_id":  jobID,
		"status":  status,
		"result":  result,
		"error":   errorMessage,
		"updated": updatedAt.Format(time.RFC3339),
	})
}

// GetRecordingTasks handles getting recording tasks
func (h *Handler) GetRecordingTasks(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	req := TaskListRequest{
		TenantID:    &tenantID,
		RecordingID: &id,
		Page:        1,
		PageSize:    100,
	}
	tasks, _, err := h.service.ListRecordingTasks(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tasks)
}
