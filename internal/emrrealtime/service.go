package emrrealtime

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrpermission"
	"github.com/freeasyman/lingce-api/internal/emrrecord"
	"github.com/freeasyman/lingce-api/internal/recording"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	ossutil "github.com/freeasyman/lingce-api/pkg/oss"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

var realtimeTranscriptProtectedTokenPattern = regexp.MustCompile(`\d+(?:[.．:：/／-]\d+)*|[零〇一二三四五六七八九十百千万两]+`)

type Service struct {
	store       *Store
	records     *emrrecord.Service
	recordings  *recording.Service
	permissions *emrpermission.Service
	gatewayURL  string
	gatewayKey  string
	llmClient   *llmgateway.Client
	ossClient   *ossutil.Client
}

func NewService(pool *pgxpool.Pool, records *emrrecord.Service, recordings *recording.Service, permissions *emrpermission.Service, gatewayURL, gatewayKey string, llmClient *llmgateway.Client, ossClient *ossutil.Client) *Service {
	return &Service{
		store: NewStore(pool), records: records, recordings: recordings, permissions: permissions,
		gatewayURL: strings.TrimRight(strings.TrimSpace(gatewayURL), "/"), gatewayKey: gatewayKey,
		llmClient: llmClient, ossClient: ossClient,
	}
}

func (s *Service) FinalizeRecording(ctx context.Context, tenantID, actorID, encounterID int64, recordID string, audio []byte, mimeType, fileName string, durationSeconds int, recordedAt time.Time) (*FinalizeResponse, error) {
	if s.recordings == nil {
		return nil, fmt.Errorf("录音服务未配置")
	}
	if encounterID <= 0 || strings.TrimSpace(recordID) == "" {
		return nil, fmt.Errorf("实时接诊标识无效")
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("没有收到录音内容")
	}
	if s.ossClient == nil {
		return nil, fmt.Errorf("录音存储未配置")
	}
	record, err := s.records.Get(ctx, tenantID, recordID)
	if err != nil || record == nil || record.EncounterID == nil || *record.EncounterID != encounterID {
		return nil, fmt.Errorf("实时病历与接诊不匹配")
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "audio/webm"
	}
	if strings.TrimSpace(fileName) == "" {
		fileName = fmt.Sprintf("realtime-%d.webm", encounterID)
	}
	if isRealtimePCM(mimeType, fileName) {
		audio = pcmToWAV(audio, 16000, 1, 16)
		mimeType = "audio/wav"
		fileName = strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".wav"
	}
	if durationSeconds <= 0 && !record.StartedAt.IsZero() {
		durationSeconds = int(time.Since(record.StartedAt).Seconds())
	}
	if durationSeconds < 0 {
		durationSeconds = 0
	}
	startAt := record.StartedAt
	if startAt.IsZero() {
		startAt = recordedAt
	}
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	if ext == "" {
		switch strings.ToLower(strings.TrimSpace(mimeType)) {
		case "audio/ogg", "audio/opus":
			ext = ".ogg"
		case "audio/mp4", "audio/m4a":
			ext = ".m4a"
		default:
			ext = ".webm"
		}
	}
	ossKey := fmt.Sprintf("recordings/%d/realtime/%d%s", tenantID, encounterID, ext)
	fileURL, err := s.ossClient.UploadBytes(ctx, ossKey, audio, &ossutil.UploadOptions{
		ContentType: mimeType,
		Metadata: map[string]string{
			"source":       "emr-realtime",
			"encounter-id": fmt.Sprintf("%d", encounterID),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("上传实时录音失败: %w", err)
	}
	var customerID, patientID *int64
	if value, ok := record.PatientSnapshot["customer_id"].(float64); ok && int64(value) > 0 {
		value := int64(value)
		customerID = &value
	}
	patientID = record.PatientID
	orderNo := fmt.Sprintf("realtime:%d", encounterID)
	result, created, err := s.recordings.IngestOwnedAudioAndEnqueue(ctx, recording.OwnedAudioIngestRequest{
		TenantID: tenantID, EmployeeID: actorID, EncounterID: &encounterID, CustomerID: customerID, PatientID: patientID,
		FileURL: fileURL, FileName: fileName, MIMEType: mimeType, DurationSeconds: durationSeconds, RecordedAt: &startAt,
		OrderNo: orderNo, OSSKey: ossKey, Source: "realtime", BusinessScope: "doctor", Scene: "consultation", TriggerSource: "realtime_end",
	})
	if err != nil {
		if result != nil && result.ID > 0 {
			return &FinalizeResponse{
				EncounterID: encounterID,
				RecordID:    recordID,
				RecordingID: result.ID,
				Queued:      false,
				QueueError:  err.Error(),
			}, nil
		}
		_ = s.ossClient.DeleteFile(ctx, ossKey)
		return nil, err
	}
	return &FinalizeResponse{EncounterID: encounterID, RecordID: recordID, RecordingID: result.ID, Queued: created}, nil
}

func isRealtimePCM(mimeType, fileName string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	return mimeType == "audio/pcm" || mimeType == "audio/l16" || ext == ".pcm"
}

func pcmToWAV(pcm []byte, sampleRate, channels, bitsPerSample int) []byte {
	if len(pcm)%2 != 0 {
		pcm = pcm[:len(pcm)-1]
	}
	blockAlign := channels * bitsPerSample / 8
	byteRate := sampleRate * blockAlign
	dataSize := len(pcm)
	output := make([]byte, 44+dataSize)
	copy(output[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(output[4:8], uint32(36+dataSize))
	copy(output[8:12], []byte("WAVE"))
	copy(output[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(output[16:20], 16)
	binary.LittleEndian.PutUint16(output[20:22], 1)
	binary.LittleEndian.PutUint16(output[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(output[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(output[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(output[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(output[34:36], uint16(bitsPerSample))
	copy(output[36:40], []byte("data"))
	binary.LittleEndian.PutUint32(output[40:44], uint32(dataSize))
	copy(output[44:], pcm)
	return output
}

func (s *Service) GenerateRealtimeAICandidates(ctx context.Context, tenantID int64, encounterID int64, recordID string, transcript string, segments []realtimeTranscriptSegment) ([]*emrrecord.AICandidate, error) {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return nil, nil
	}
	if s.llmClient == nil {
		return nil, fmt.Errorf("电子病历生成网关未配置")
	}
	model, err := s.store.GetCandidateModel(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	prompt, err := s.store.GetCandidatePrompt(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	sections, err := s.store.ListCandidateSections(ctx, tenantID, recordID)
	if err != nil {
		return nil, err
	}
	allowed := make([]string, 0, len(sections))
	for code, name := range sections {
		allowed = append(allowed, code+"（"+name+"）")
	}
	sort.Strings(allowed)
	userPrompt := strings.ReplaceAll(prompt.UserPrompt, "{{recording_id}}", fmt.Sprintf("实时接诊:%d", encounterID))
	userPrompt = strings.ReplaceAll(userPrompt, "{{pipeline_code}}", "realtime")
	userPrompt = strings.ReplaceAll(userPrompt, "{{transcript}}", transcript)
	userPrompt = strings.ReplaceAll(userPrompt, "chief_complaint、present_illness、past_history、personal_history、family_history、allergy_history、physical_exam、auxiliary_exam、diagnosis、prescription、disposition、medical_advice、followup、supplement", strings.Join(allowed, "、"))
	if !strings.Contains(userPrompt, transcript) {
		userPrompt += "\n\n接诊转写：\n" + transcript
	}
	systemPrompt := prompt.SystemPrompt + "\n\n允许的病历栏目编码：\n" + strings.Join(allowed, "、")
	traceID := uuid.NewString()
	resp, err := s.llmClient.TextInference(ctx, llmgateway.TextInferenceRequest{
		TenantID: tenantID, CallerService: "lingce-api", CallerModule: "emr.realtime_candidate_generation",
		TraceID: traceID, FunctionType: "emr_candidate_generation", Provider: model.Provider, ModelCode: model.ModelCode,
		Billing:  &llmgateway.BillingMetadata{BusinessDomain: "emr", BusinessObjectType: "encounter", BusinessObjectID: encounterID, BillingSubject: "emr_candidate_generation", BillingScene: "realtime"},
		Messages: []llmgateway.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}},
		Params:   &llmgateway.Params{Temperature: 0.1, MaxTokens: 4000, TimeoutSeconds: 120, ResponseFormat: "json"},
	})
	if err != nil {
		return nil, fmt.Errorf("调用电子病历生成网关失败: %w", err)
	}
	parsed, err := parseCandidateJSON(resp.Content)
	if err != nil {
		return nil, err
	}
	items := make([]emrrecord.CreateAICandidateRequest, 0, len(parsed))
	for _, item := range parsed {
		sectionCode := strings.TrimSpace(stringValue(item["section_code"]))
		if _, ok := sections[sectionCode]; !ok {
			continue
		}
		content := objectValue(item["content"])
		if len(content) == 0 {
			if text := strings.TrimSpace(stringValue(item["content"])); text != "" {
				content = map[string]any{"text": text}
			}
		}
		if len(content) == 0 {
			continue
		}
		evidence := objectValue(item["source_evidence"])
		if evidence == nil {
			evidence = map[string]any{}
		}
		evidence["encounter_id"] = encounterID
		evidence["transcript_excerpt"] = evidenceExcerpt(evidence, transcript)
		if len(segments) > 0 {
			evidence["segments"] = segments
		}
		items = append(items, emrrecord.CreateAICandidateRequest{SectionCode: sectionCode, Content: content, SourceEvidence: evidence})
	}
	if len(items) == 0 {
		return nil, nil
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", encounterID, transcript)))
	out, err := s.records.GenerateRealtimeAICandidates(ctx, emrrecord.GenerateRealtimeAICandidatesRequest{
		TenantID: tenantID, RecordID: recordID, EncounterID: encounterID, GenerationKey: hex.EncodeToString(digest[:]), Candidates: items,
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) CorrectRealtimeTranscript(ctx context.Context, tenantID, encounterID int64, item realtimeASRResult, previous string) (*realtimeTranscriptCorrection, error) {
	original := strings.TrimSpace(item.Text)
	if original == "" {
		return nil, fmt.Errorf("实时转写纠错输入为空")
	}
	if s.llmClient == nil {
		return nil, fmt.Errorf("实时转写纠错网关未配置")
	}
	model, err := s.store.GetTranscriptCorrectionModel(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	prompt, err := s.store.GetTranscriptCorrectionPrompt(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	userPrompt := strings.ReplaceAll(prompt.UserPrompt, "{{transcript}}", original)
	if previous = strings.TrimSpace(previous); previous != "" {
		if len([]rune(previous)) > 300 {
			previous = string([]rune(previous)[len([]rune(previous))-300:])
		}
		userPrompt += "\n\n上一句仅作断句参考，不得把其中内容添加到当前文字：\n" + previous
	}
	if !strings.Contains(userPrompt, original) {
		userPrompt += "\n\n原始 ASR 文字：\n" + original
	}
	traceID := uuid.NewString()
	resp, err := s.llmClient.TextInference(ctx, llmgateway.TextInferenceRequest{
		TenantID: tenantID, CallerService: "lingce-api", CallerModule: "emr.realtime_transcript_correction",
		TraceID: traceID, FunctionType: "realtime_transcript_correction", Provider: model.Provider, ModelCode: model.ModelCode,
		Billing:  &llmgateway.BillingMetadata{BusinessDomain: "emr", BusinessObjectType: "encounter", BusinessObjectID: encounterID, BillingSubject: "realtime_transcript_correction", BillingScene: "realtime"},
		Messages: []llmgateway.Message{{Role: "system", Content: prompt.SystemPrompt}, {Role: "user", Content: userPrompt}},
		Params:   &llmgateway.Params{Temperature: 0, MaxTokens: 500, TimeoutSeconds: 15, ResponseFormat: "json"},
	})
	if err != nil {
		return nil, fmt.Errorf("调用实时转写纠错网关失败: %w", err)
	}
	corrected, err := parseTranscriptCorrectionJSON(resp.Content)
	if err != nil {
		return nil, err
	}
	if !preservesRealtimeTranscriptProtectedTokens(original, corrected) {
		return nil, fmt.Errorf("实时转写纠错结果修改了数字或中文数值")
	}
	return &realtimeTranscriptCorrection{
		Sequence: item.Sequence, OriginalText: original, CorrectedText: corrected,
		StartTime: item.StartTime, EndTime: item.EndTime,
	}, nil
}

func parseCandidateJSON(raw string) ([]map[string]any, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var wrapper struct {
		Candidates []map[string]any `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, fmt.Errorf("电子病历生成结果不是合法 JSON: %w", err)
	}
	return wrapper.Candidates, nil
}

func parseTranscriptCorrectionJSON(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var result struct {
		CorrectedText string `json:"corrected_text"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return "", fmt.Errorf("实时转写纠错结果不是合法 JSON: %w", err)
	}
	corrected := strings.TrimSpace(result.CorrectedText)
	if corrected == "" {
		return "", fmt.Errorf("实时转写纠错结果为空")
	}
	return corrected, nil
}

func preservesRealtimeTranscriptProtectedTokens(original, corrected string) bool {
	originalTokens := realtimeTranscriptProtectedTokenPattern.FindAllString(original, -1)
	correctedTokens := realtimeTranscriptProtectedTokenPattern.FindAllString(corrected, -1)
	if len(originalTokens) != len(correctedTokens) {
		return false
	}
	for index, token := range originalTokens {
		if token != correctedTokens[index] {
			return false
		}
	}
	return true
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func objectValue(value any) map[string]any {
	item, _ := value.(map[string]any)
	return item
}

func evidenceExcerpt(evidence map[string]any, transcript string) string {
	if value := strings.TrimSpace(stringValue(evidence["transcript_excerpt"])); value != "" {
		if len(value) > 500 {
			return value[:500]
		}
		return value
	}
	if len(transcript) > 500 {
		return transcript[:500]
	}
	return transcript
}

func (s *Service) Start(ctx context.Context, tenantID, actorID int64, access *emrpermission.Access, req StartRequest) (*SessionResponse, error) {
	req.ClientRequestID = strings.TrimSpace(req.ClientRequestID)
	if req.ClientRequestID == "" {
		return nil, fmt.Errorf("client_request_id is required")
	}
	if len(req.ClientRequestID) > 128 {
		return nil, fmt.Errorf("client_request_id is too long")
	}
	if req.VisitType == "" {
		req.VisitType = "初诊"
	}
	if req.DocumentType == "" {
		req.DocumentType = "门诊病历"
	}
	if req.DepartmentID != nil && !access.CanRecord(actorID, req.DepartmentID) {
		return nil, fmt.Errorf("emr record access denied")
	}

	encounterID, found, err := s.store.GetEncounterByRequestID(ctx, tenantID, req.ClientRequestID)
	if err != nil {
		return nil, err
	}
	if found {
		if existing, loadErr := s.loadExistingSession(ctx, tenantID, encounterID, access); loadErr == nil {
			return existing, nil
		}
	}

	var patient *patientInfo
	var patientErr error
	if req.CustomerID != nil && *req.CustomerID > 0 {
		patient, patientErr = s.store.GetPatient(ctx, tenantID, *req.CustomerID)
		if patientErr != nil {
			return nil, patientErr
		}
	}
	templateVersionID, documentType, err := s.store.GetPublishedTemplateVersion(ctx, tenantID, req.TemplateVersionID, req.DocumentType, req.VisitType)
	if err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()
	if !found {
		encounterID, err = s.store.CreateEncounter(ctx, tenantID, actorID, req.DepartmentID, req.ClientRequestID, patient, startedAt)
		if err != nil {
			return nil, err
		}
	}
	departmentID := req.DepartmentID
	result, err := s.records.Create(ctx, tenantID, actorID, emrrecord.CreateRequest{
		EncounterID:       &encounterID,
		PatientID:         patientID(patient),
		PatientSnapshot:   patientSnapshot(patient),
		TemplateVersionID: templateVersionID,
		DocumentType:      documentType,
		VisitType:         req.VisitType,
		DepartmentID:      departmentID,
		DoctorID:          &actorID,
		StartedAt:         &startedAt,
		EncounterContext:  map[string]any{"channel": "realtime", "source": "microphone"},
		SourceReferences:  map[string]any{"source_type": "realtime", "encounter_id": encounterID},
		Content:           map[string]any{},
	})
	if err != nil {
		if existingRecordID, lookupErr := s.store.GetRecordByEncounter(ctx, tenantID, encounterID); lookupErr == nil && existingRecordID != "" {
			return s.loadExistingSession(ctx, tenantID, encounterID, access)
		}
		return nil, err
	}
	return &SessionResponse{
		EncounterID: encounterID, RecordID: result.ID, TemplateVersionID: result.TemplateVersionID,
		PatientID: patientID(patient), PatientSnapshot: patientSnapshot(patient), StartedAt: startedAt,
	}, nil
}

func patientID(patient *patientInfo) *int64 {
	if patient == nil {
		return nil
	}
	return patient.PatientID
}

func (s *Service) loadExistingSession(ctx context.Context, tenantID, encounterID int64, access *emrpermission.Access) (*SessionResponse, error) {
	recordID, err := s.store.GetRecordByEncounter(ctx, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	if recordID == "" {
		return nil, fmt.Errorf("实时接诊已创建但病历尚未建立，请稍后重试")
	}
	ok, err := s.permissions.CanAccessRecord(ctx, access, recordID)
	if err != nil || !ok {
		return nil, fmt.Errorf("emr record access denied")
	}
	record, err := s.records.Get(ctx, tenantID, recordID)
	if err != nil || record == nil {
		return nil, fmt.Errorf("实时病历不存在")
	}
	return &SessionResponse{
		EncounterID: encounterID, RecordID: record.ID, TemplateVersionID: record.TemplateVersionID,
		PatientID: record.PatientID, PatientSnapshot: record.PatientSnapshot, StartedAt: record.StartedAt,
	}, nil
}

func (s *Service) RecordForEncounter(ctx context.Context, tenantID, encounterID int64, access *emrpermission.Access) (string, error) {
	recordID, err := s.store.GetRecordByEncounter(ctx, tenantID, encounterID)
	if err != nil {
		return "", err
	}
	if recordID == "" {
		return "", fmt.Errorf("realtime record not found")
	}
	ok, err := s.permissions.CanAccessRecord(ctx, access, recordID)
	if err != nil || !ok {
		return "", fmt.Errorf("emr record access denied")
	}
	return recordID, nil
}

func (s *Service) BindCustomer(ctx context.Context, tenantID, encounterID int64, recordID string, customerID int64) (*SessionResponse, error) {
	patient, err := s.store.GetPatient(ctx, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	if err := s.store.BindCustomer(ctx, tenantID, encounterID, recordID, patient); err != nil {
		return nil, err
	}
	return &SessionResponse{
		EncounterID:     encounterID,
		RecordID:        recordID,
		PatientID:       patient.PatientID,
		PatientSnapshot: patientSnapshot(patient),
	}, nil
}

func (s *Service) DialGateway(ctx context.Context, tenantID int64) (*websocket.Conn, string, error) {
	if strings.TrimSpace(s.gatewayURL) == "" || strings.TrimSpace(s.gatewayKey) == "" {
		return nil, "", fmt.Errorf("实时转写网关未配置")
	}
	model, err := s.store.GetRealtimeModel(ctx, tenantID)
	if err != nil {
		return nil, "", err
	}
	traceID := uuid.NewString()
	requestID := uuid.NewString()
	url := gatewayWebSocketURL(s.gatewayURL) + "/v1/inference/realtime"
	header := make(map[string][]string)
	header["Authorization"] = []string{"Bearer " + s.gatewayKey}
	conn, _, err := (&websocket.Dialer{}).DialContext(ctx, url, header)
	if err != nil {
		return nil, "", fmt.Errorf("连接实时转写网关失败: %w", err)
	}
	start := map[string]any{
		"type": "start",
		"request": map[string]any{
			"tenant_id": tenantID, "caller_service": "lingce-api", "caller_module": "emr_realtime",
			"trace_id": traceID, "function_type": "realtime_transcription", "provider": model.Provider,
			"model_code": model.ModelCode, "language": "zh",
		},
	}
	if err := conn.WriteJSON(start); err != nil {
		_ = conn.Close()
		return nil, "", fmt.Errorf("初始化实时转写会话失败: %w", err)
	}
	return conn, requestID, nil
}

func gatewayWebSocketURL(base string) string {
	if strings.HasPrefix(base, "https://") {
		return "wss://" + strings.TrimPrefix(base, "https://")
	}
	if strings.HasPrefix(base, "http://") {
		return "ws://" + strings.TrimPrefix(base, "http://")
	}
	return base
}
