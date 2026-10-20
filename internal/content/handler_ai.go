package content

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

func (h *Handler) registerAIRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	router.Register(mux, []router.Route{
		{Method: http.MethodPost, Path: "/api/v1/content-ai/answers", Handler: h.AnswerQuestion, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodGet, Path: "/api/v1/content-ai/questions", Handler: h.ListAIQuestions, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodGet, Path: "/api/v1/content-ai/environments", Handler: h.ListAIEnvironments, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
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

func (h *Handler) ListAIQuestions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil {
			limit = parsed
		}
	}
	questions, err := h.service.ListAIQuestions(r.Context(), tenantID, limit)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, questions)
}

func (h *Handler) ListAIEnvironments(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	envs, err := h.service.ListAIEnvironments(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, envs)
}
