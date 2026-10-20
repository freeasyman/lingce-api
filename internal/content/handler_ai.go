package content

import (
	"encoding/json"
	"net/http"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

func (h *Handler) registerAIRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	router.Register(mux, []router.Route{
		{Method: http.MethodPost, Path: "/api/v1/content-ai/answers", Handler: h.AnswerQuestion, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, deps)
}

type answerQuestionRequest struct {
	Question string `json:"question"`
}

func (h *Handler) AnswerQuestion(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req answerQuestionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	result, err := h.service.AnswerQuestion(r.Context(), tenantID, claims.UserID, req.Question)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}
