package dealexpert

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) RegisterInternalRoutes(mux *http.ServeMux, internalToken string) {
	router.Register(mux, []router.Route{{Method: "POST", Path: "/api/v1/internal/deal-expert/recordings", Handler: h.Receive, AuthMode: "internal"}}, router.RouteDeps{InternalToken: strings.TrimSpace(internalToken)})
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
	httputil.WriteSuccess(w, map[string]any{"code": "ACCEPTED", "status": "accepted", "request_id": fmt.Sprintf("deal-expert-%d", row.ID), "recording_id": row.RecordingID, "encounter_id": row.EncounterID, "inserted": inserted})
}
