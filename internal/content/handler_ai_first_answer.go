package content

import (
	"encoding/json"
	"net/http"

	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// AnalyzeAIAnswer 分析"患者问 AI"的回答。
func (h *Handler) AnalyzeAIAnswer(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	result, err := h.service.AnalyzeAIAnswer(r.Context(), tenantID, req.Question)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

func (h *Handler) registerAIRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	routes := []router.Route{
		{Method: "POST", Path: "/api/v1/content-ai/actions/analyze", Handler: h.AnalyzeAIAnswer, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-ai/actions/visibility", Handler: h.AnalyzeAIVisibility, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-ai/visibility/runs", Handler: h.ListAIVisibilityRuns, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-ai/candidate-questions", Handler: h.ListCandidateQuestions, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-ai/visibility/runs/delete", Handler: h.DeleteAIVisibilityRuns, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-ai/visibility/runs/restore", Handler: h.RestoreAIVisibilityRuns, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-ai/visibility/runs/{id}/conflict", Handler: h.CheckGenerateConflict, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-ai/visibility/runs/{id}", Handler: h.GetAIVisibilityRun, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-ai/actions/visibility/generate", Handler: h.GenerateFromAIVisibility, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		// 公共"免费体检"入口：独立产品，不挂租户、匿名可调（限流后续再加）
		{Method: "POST", Path: "/api/v1/public/free-checkup", Handler: h.StartPublicFreeCheckup, Auth: false},
		{Method: "GET", Path: "/api/v1/public/free-checkup/quota", Handler: h.GetPublicFreeCheckupQuota, Auth: false},
		{Method: "GET", Path: "/api/v1/public/free-checkup/campaign", Handler: h.ResolvePublicFreeCheckupCampaign, Auth: false},
		{Method: "GET", Path: "/api/v1/public/free-checkup/{id}", Handler: h.GetPublicFreeCheckup, Auth: false},
		{Method: "POST", Path: "/api/v1/public/free-checkup/{id}/lead", Handler: h.SubmitFreeCheckupLead, Auth: false},
		// 运营控制台：免费体检线索管理
		{Method: "GET", Path: "/api/v1/operation/free-checkup/leads", Handler: h.ListFreeCheckupLeadsForOperation, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/operation/free-checkup/leads/{id}", Handler: h.GetFreeCheckupLeadForOperation, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/operation/free-checkup/leads/{id}/status", Handler: h.UpdateFreeCheckupLeadStatusForOperation, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/operation/free-checkup/leads/{id}/convert", Handler: h.ConvertFreeCheckupLeadForOperation, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/operation/free-checkup/stats", Handler: h.FreeCheckupStatsForOperation, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/operation/free-checkup/campaigns", Handler: h.ListFreeCheckupCampaignsForOperation, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/operation/free-checkup/campaigns", Handler: h.CreateFreeCheckupCampaignForOperation, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/operation/free-checkup/campaigns/{id}/status", Handler: h.UpdateFreeCheckupCampaignStatusForOperation, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/operation/free-checkup/campaigns/{id}/qr.png", Handler: h.GetFreeCheckupCampaignQRForOperation, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/operation/free-checkup/usage", Handler: h.ListFreeCheckupUsageForOperation, Auth: true, AllowedUserTypes: []string{"admin"}},
	}
	router.Register(mux, routes, deps)
}
