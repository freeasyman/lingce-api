package emrrealtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrpermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/gorilla/websocket"
)

type Handler struct {
	service     *Service
	permissions *emrpermission.Service
	upgrader    websocket.Upgrader
}

const realtimeMaxAudioBytes = 150 * 1024 * 1024

func NewHandler(service *Service, permissions *emrpermission.Service) *Handler {
	return &Handler{
		service: service, permissions: permissions,
		upgrader: websocket.Upgrader{ReadBufferSize: 16 * 1024, WriteBufferSize: 16 * 1024, CheckOrigin: allowedOrigin},
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "POST", Path: "/api/v1/emr/realtime/sessions", Handler: h.Start, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/emr/realtime/{encounter_id}/customer", Handler: h.BindCustomer, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/realtime/{encounter_id}", Handler: h.Proxy, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	access, err := h.permissions.Authorize(r.Context(), claims, tenantID, "record.create")
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.DepartmentID != nil && !access.CanRecord(claims.UserID, req.DepartmentID) {
		http.Error(w, "emr record access denied", http.StatusForbidden)
		return
	}
	result, err := h.service.Start(r.Context(), tenantID, claims.UserID, access, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": result})
}

func (h *Handler) BindCustomer(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	access, err := h.permissions.Authorize(r.Context(), claims, tenantID, "record.edit")
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	encounterID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("encounter_id")), 10, 64)
	if err != nil || encounterID <= 0 {
		http.Error(w, "invalid encounter_id", http.StatusBadRequest)
		return
	}
	recordID, err := h.service.RecordForEncounter(r.Context(), tenantID, encounterID, access)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	var req BindCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID <= 0 {
		http.Error(w, "customer_id is required", http.StatusBadRequest)
		return
	}
	result, err := h.service.BindCustomer(r.Context(), tenantID, encounterID, recordID, req.CustomerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": result})
}

func (h *Handler) Proxy(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	access, err := h.permissions.Authorize(r.Context(), claims, tenantID, "record.edit")
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	encounterID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("encounter_id")), 10, 64)
	if err != nil || encounterID <= 0 {
		http.Error(w, "invalid encounter_id", http.StatusBadRequest)
		return
	}
	recordID, err := h.service.RecordForEncounter(r.Context(), tenantID, encounterID, access)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	client, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer client.Close()

	messageType, payload, err := client.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		writeProxyError(client, "首条消息必须是 start")
		return
	}
	var start struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(payload, &start) != nil || start.Type != "start" {
		writeProxyError(client, "首条消息必须是 start")
		return
	}
	upstream, _, err := h.service.DialGateway(r.Context(), tenantID)
	if err != nil {
		writeProxyError(client, err.Error())
		return
	}
	defer upstream.Close()
	h.proxyWebSockets(client, upstream, tenantID, claims.UserID, encounterID, recordID)
}

func (h *Handler) proxyWebSockets(client, upstream *websocket.Conn, tenantID, actorID, encounterID int64, recordID string) {
	var clientWriteMu sync.Mutex
	writeClient := func(messageType int, payload []byte) error {
		clientWriteMu.Lock()
		defer clientWriteMu.Unlock()
		return client.WriteMessage(messageType, payload)
	}
	writeError := func(message string) {
		payload, _ := json.Marshal(map[string]any{"type": "error", "error": message})
		_ = writeClient(websocket.TextMessage, payload)
	}
	corrector := newRealtimeTranscriptCorrector(h.service, tenantID, encounterID, func(result realtimeTranscriptCorrection) {
		_ = writeClient(websocket.TextMessage, mustJSON(map[string]any{
			"type":           "transcript_correction",
			"sequence":       result.Sequence,
			"original_text":  result.OriginalText,
			"corrected_text": result.CorrectedText,
			"start_time":     result.StartTime,
			"end_time":       result.EndTime,
			"status":         "completed",
		}))
	}, func(item realtimeASRResult, err error) {
		_ = writeClient(websocket.TextMessage, mustJSON(map[string]any{
			"type":          "transcript_correction",
			"sequence":      item.Sequence,
			"original_text": item.Text,
			"status":        "failed",
			"error":         err.Error(),
		}))
	})
	done := make(chan struct{})
	upstreamDone := make(chan struct{})
	var once sync.Once
	var audio bytes.Buffer
	stop := func() { once.Do(func() { close(done); _ = client.Close(); _ = upstream.Close() }) }

	go func() {
		defer close(upstreamDone)
		for {
			messageType, payload, err := upstream.ReadMessage()
			if err != nil {
				stop()
				return
			}
			var message struct {
				Type string `json:"type"`
			}
			var result struct {
				Type   string             `json:"type"`
				Result *realtimeASRResult `json:"result"`
			}
			if messageType == websocket.TextMessage && json.Unmarshal(payload, &result) == nil && result.Type == "result" && result.Result != nil {
				if err := writeClient(messageType, payload); err != nil {
					stop()
					return
				}
				corrector.Add(*result.Result)
				continue
			}
			if messageType == websocket.TextMessage && json.Unmarshal(payload, &message) == nil && (message.Type == "end" || message.Type == "error") {
				if message.Type == "error" {
					_ = writeClient(messageType, payload)
					stop()
				}
				return
			}
			if err := writeClient(messageType, payload); err != nil {
				stop()
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		default:
		}
		messageType, payload, err := client.ReadMessage()
		if err != nil {
			stop()
			return
		}
		if messageType == websocket.BinaryMessage {
			if audio.Len()+len(payload) > realtimeMaxAudioBytes {
				writeError("实时录音超过 150MB 限制")
				stop()
				return
			}
			_, _ = audio.Write(payload)
			if err := upstream.WriteMessage(websocket.BinaryMessage, payload); err != nil {
				stop()
				return
			}
			continue
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var message struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(payload, &message) != nil {
			writeError("无效的实时会话消息")
			stop()
			return
		}
		if message.Type == "end" {
			if err := upstream.WriteMessage(websocket.TextMessage, payload); err != nil {
				stop()
				return
			}
			select {
			case <-upstreamDone:
			case <-time.After(2 * time.Minute):
				stop()
				return
			}
			correctionContext, correctionCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			corrector.FlushContext(correctionContext)
			correctionCancel()
			var end struct {
				Type            string `json:"type"`
				DurationSeconds int    `json:"duration_seconds"`
				MIMEType        string `json:"mime_type"`
				FileName        string `json:"file_name"`
			}
			_ = json.Unmarshal(payload, &end)
			finalized, finalizeErr := h.service.FinalizeRecording(
				context.Background(), tenantID, actorID, encounterID, recordID, audio.Bytes(),
				end.MIMEType, end.FileName, end.DurationSeconds, time.Now().UTC(),
			)
			if finalizeErr != nil {
				_ = writeClient(websocket.TextMessage, mustJSON(map[string]any{"type": "recording_save_error", "error": finalizeErr.Error()}))
				_ = writeClient(websocket.TextMessage, mustJSON(map[string]any{"type": "end", "recording_saved": false}))
			} else {
				_ = writeClient(websocket.TextMessage, mustJSON(map[string]any{"type": "recording_saved", "recording": finalized, "queue_error": finalized.QueueError}))
				_ = writeClient(websocket.TextMessage, mustJSON(map[string]any{"type": "end", "recording_saved": true, "recording_id": finalized.RecordingID, "queue_error": finalized.QueueError}))
			}
			stop()
			return
		}
	}
}

func mustJSON(value any) []byte {
	payload, _ := json.Marshal(value)
	return payload
}

func writeProxyError(conn *websocket.Conn, message string) {
	_ = conn.WriteJSON(map[string]any{"type": "error", "error": message})
}

func allowedOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	requestURL := r.Host
	if parsed.Host == requestURL {
		return true
	}
	originHost := strings.Split(parsed.Host, ":")[0]
	requestHost := strings.Split(requestURL, ":")[0]
	return (originHost == "localhost" || originHost == "127.0.0.1") && (requestHost == "localhost" || requestHost == "127.0.0.1")
}
