package dealexpert

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
	pool    *pgxpool.Pool
	instant *InstantAnalyzer
}

// SetInstantAnalyzer 设置轻量即时分析器；nil 时不启用。
func (h *Handler) SetInstantAnalyzer(analyzer *InstantAnalyzer) {
	if h != nil {
		h.instant = analyzer
	}
}

func NewHandler(service *Service, pool *pgxpool.Pool) *Handler {
	return &Handler{service: service, pool: pool}
}

func (h *Handler) RegisterInternalRoutes(mux *http.ServeMux, internalToken string) {
	router.Register(mux, []router.Route{
		{Method: "POST", Path: "/api/v1/internal/deal-expert/recordings", Handler: h.Receive, AuthMode: "internal"},
		{Method: "POST", Path: "/api/v1/internal/deal-expert/recordings/{id}/instant", Handler: h.RunInstant, AuthMode: "internal"},
	}, router.RouteDeps{InternalToken: strings.TrimSpace(internalToken)})
}

func (h *Handler) Receive(w http.ResponseWriter, r *http.Request) {
	var req ReceiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	row, inserted, err := h.service.Receive(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if h.instant != nil && req.BusinessScope == "consultant" {
		go h.instant.RunInstant(context.Background(), req.TenantID, req.RecordingID)
	}
	httputil.WriteSuccess(w, map[string]any{"code": "ACCEPTED", "status": "accepted", "request_id": fmt.Sprintf("deal-expert-%d", row.ID), "recording_id": row.RecordingID, "encounter_id": row.EncounterID, "inserted": inserted})
}

// RunInstant 内部接口：对单条录音立即执行轻量即时分析（同步，供 Worker/测试调用）。
func (h *Handler) RunInstant(w http.ResponseWriter, r *http.Request) {
	if h.instant == nil {
		httputil.WriteInternalError(w, "instant analyzer unavailable")
		return
	}
	id, err := parsePathID(r.PathValue("id"))
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	var body struct {
		TenantID int64 `json:"tenant_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
		return
	}
	h.instant.RunInstant(r.Context(), body.TenantID, id)
	item, err := h.instant.LatestInstant(r.Context(), body.TenantID, id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

// GetInstantCard 前端手机卡读取即时三字段。
func (h *Handler) GetInstantCard(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	id, err := parsePathID(r.PathValue("id"))
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	if accessErr := h.checkRecordingAccess(r.Context(), scope, id); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	if h.instant == nil {
		httputil.WriteSuccess(w, map[string]any{"status": "unavailable"})
		return
	}
	item, err := h.instant.LatestInstant(r.Context(), scope.TenantID, id)
	if err != nil {
		httputil.WriteSuccess(w, map[string]any{"status": "none"})
		return
	}
	httputil.WriteSuccess(w, item)
}

func parsePathID(value string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(value, "%d", &id)
	return id, err
}
