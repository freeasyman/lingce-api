package content

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

var (
	seedStateMu         sync.RWMutex
	seedStatusOverrides = map[int64]string{}
	seedAdoptedBy       = map[int64]int64{}
	seedDismissedBy     = map[int64]int64{}
)

// ListSeeds handles listing content seeds
func (h *Handler) ListSeeds(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req SeedListRequest
	scope, err := h.resolveTenantScope(claims, r)
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if len(scope.TenantIDs) == 0 {
		httputil.WritePaginated(w, []SeedResponse{}, 0, 1, 20)
		return
	}
	if scope.TenantID != nil {
		req.TenantID = scope.TenantID
	} else {
		req.TenantIDs = scope.TenantIDs
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	seeds, err := h.buildSeedsFromTopics(r.Context(), req.TenantID, req.TenantIDs...)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	total := len(seeds)
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	start := (req.Page - 1) * req.PageSize
	if start >= total {
		httputil.WritePaginated(w, []SeedResponse{}, int64(total), req.Page, req.PageSize)
		return
	}
	end := start + req.PageSize
	if end > total {
		end = total
	}
	httputil.WritePaginated(w, seeds[start:end], int64(total), req.Page, req.PageSize)
}

// GetSeedStats handles getting seed statistics
func (h *Handler) GetSeedStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	scope, err := h.resolveTenantScope(claims, r)
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if len(scope.TenantIDs) == 0 {
		httputil.WriteSuccess(w, &SeedStatsResponse{})
		return
	}
	seeds, err := h.buildSeedsFromTopics(r.Context(), scope.TenantID, scope.TenantIDs...)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	response := &SeedStatsResponse{
		TotalSeeds: int64(len(seeds)),
	}
	for _, seed := range seeds {
		switch seed.Status {
		case "adopted":
			response.AdoptedSeeds++
		case "dismissed":
			response.DismissedSeeds++
		case "draft_generated":
			response.DraftGenerated++
		default:
			response.PendingSeeds++
		}
	}

	httputil.WriteSuccess(w, response)
}

// GetClusters handles getting concern clusters
func (h *Handler) GetClusters(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	scope, err := h.resolveTenantScope(claims, r)
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if len(scope.TenantIDs) == 0 {
		httputil.WriteSuccess(w, []map[string]interface{}{})
		return
	}
	seeds, err := h.buildSeedsFromTopics(r.Context(), scope.TenantID, scope.TenantIDs...)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	clusterMap := map[string]int64{}
	for _, seed := range seeds {
		key := "general"
		if seed.Category != nil && strings.TrimSpace(*seed.Category) != "" {
			key = *seed.Category
		}
		clusterMap[key]++
	}
	resp := make([]map[string]interface{}, 0, len(clusterMap))
	for name, count := range clusterMap {
		resp = append(resp, map[string]interface{}{
			"cluster": name,
			"count":   count,
		})
	}
	sort.Slice(resp, func(i, j int) bool { return resp[i]["count"].(int64) > resp[j]["count"].(int64) })
	httputil.WriteSuccess(w, resp)
}

// GetMyInspirations handles getting user's inspirations
func (h *Handler) GetMyInspirations(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	seeds, err := h.buildSeedsFromTopics(r.Context(), claims.TenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	out := []SeedResponse{}
	for _, seed := range seeds {
		if seed.Status == "pending" || seed.Status == "draft_generated" {
			out = append(out, seed)
		}
	}
	if len(out) > 20 {
		out = out[:20]
	}
	httputil.WriteSuccess(w, out)
}

// GenerateDraftFromSeed handles generating draft from seed
func (h *Handler) GenerateDraftFromSeed(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := parseSeedID(r)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid seed ID")
		return
	}

	seed, err := h.getSeedByID(r.Context(), claims.TenantID, id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	_, err = h.service.CreateContent(r.Context(), seed.TenantID, claims.UserID, CreateContentRequest{
		Title:   "草稿：" + seed.Title,
		Content: seed.Content,
		Tags:    seed.Tags,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	seedStateMu.Lock()
	seedStatusOverrides[id] = "draft_generated"
	seedStateMu.Unlock()
	httputil.WriteSuccess(w, map[string]string{"message": "Draft generated successfully"})
}

// AdoptSeed handles adopting a seed
func (h *Handler) AdoptSeed(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := parseSeedID(r)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid seed ID")
		return
	}

	seedStateMu.Lock()
	seedStatusOverrides[id] = "adopted"
	seedAdoptedBy[id] = claims.UserID
	seedStateMu.Unlock()
	httputil.WriteSuccess(w, map[string]interface{}{
		"id":      id,
		"status":  "adopted",
		"message": "Seed adopted successfully",
	})
}

// DismissSeed handles dismissing a seed
func (h *Handler) DismissSeed(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := parseSeedID(r)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid seed ID")
		return
	}

	seedStateMu.Lock()
	seedStatusOverrides[id] = "dismissed"
	seedDismissedBy[id] = claims.UserID
	seedStateMu.Unlock()
	httputil.WriteSuccess(w, map[string]interface{}{
		"id":      id,
		"status":  "dismissed",
		"message": "Seed dismissed successfully",
	})
}

// GetSeed handles getting seed by ID
func (h *Handler) GetSeed(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := parseSeedID(r)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid seed ID")
		return
	}

	seed, err := h.getSeedByID(r.Context(), claims.TenantID, id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, seed)
}

// UpdateSeedStatus handles updating seed status
func (h *Handler) UpdateSeedStatus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := parseSeedID(r)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid seed ID")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.Status == "" {
		httputil.WriteBadRequest(w, "status is required")
		return
	}
	seedStateMu.Lock()
	seedStatusOverrides[id] = req.Status
	if req.Status == "adopted" {
		seedAdoptedBy[id] = claims.UserID
	}
	seedStateMu.Unlock()
	httputil.WriteSuccess(w, map[string]string{"message": "Seed status updated successfully"})
}

func parseSeedID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

// GetHonorList handles getting honor list
func (h *Handler) GetHonorList(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	seeds, err := h.buildSeedsFromTopics(r.Context(), claims.TenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	honor := []SeedResponse{}
	for _, seed := range seeds {
		if seed.Status == "adopted" || seed.Status == "draft_generated" {
			honor = append(honor, seed)
		}
	}
	if len(honor) > 10 {
		honor = honor[:10]
	}
	httputil.WriteSuccess(w, honor)
}

// GetMyStats handles getting user's seed statistics
func (h *Handler) GetMyStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	seeds, err := h.buildSeedsFromTopics(r.Context(), claims.TenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	seedStateMu.RLock()
	adoptedByMe := 0
	dismissedByMe := 0
	for id, uid := range seedAdoptedBy {
		if uid == claims.UserID {
			if _, ok := seedStatusOverrides[id]; ok {
				adoptedByMe++
			}
		}
	}
	for _, uid := range seedDismissedBy {
		if uid == claims.UserID {
			dismissedByMe++
		}
	}
	seedStateMu.RUnlock()
	httputil.WriteSuccess(w, map[string]interface{}{
		"total_visible":    len(seeds),
		"adopted_by_me":    adoptedByMe,
		"dismissed_by_me":  dismissedByMe,
		"inspirations_cnt": len(seeds),
	})
}

// GetMyAdopted handles getting user's adopted seeds
func (h *Handler) GetMyAdopted(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	seeds, err := h.buildSeedsFromTopics(r.Context(), claims.TenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	seedStateMu.RLock()
	out := []SeedResponse{}
	for _, seed := range seeds {
		if uid, ok := seedAdoptedBy[seed.ID]; ok && uid == claims.UserID {
			out = append(out, seed)
		}
	}
	seedStateMu.RUnlock()
	httputil.WriteSuccess(w, out)
}

func (h *Handler) buildSeedsFromTopics(ctx context.Context, tenantID *int64, tenantIDs ...int64) ([]SeedResponse, error) {
	topics, _, err := h.service.ListTopics(ctx, TopicListRequest{
		TenantID:  tenantID,
		TenantIDs: tenantIDs,
		Page:      1,
		PageSize:  500,
	})
	if err != nil {
		return nil, err
	}

	seedStateMu.RLock()
	defer seedStateMu.RUnlock()
	result := make([]SeedResponse, 0, len(topics))
	for _, topic := range topics {
		status := "pending"
		switch topic.Status {
		case "selected", "completed":
			status = "adopted"
		case "archived":
			status = "dismissed"
		}
		if override, ok := seedStatusOverrides[topic.ID]; ok {
			status = override
		}
		seed := SeedResponse{
			ID:        topic.ID,
			TenantID:  topic.TenantID,
			Title:     topic.Title,
			Content:   valueOrEmpty(topic.Description),
			Category:  topic.Category,
			Tags:      topic.Tags,
			Status:    status,
			ExtraData: topic.ExtraData,
			CreatedAt: topic.CreatedAt,
			UpdatedAt: topic.UpdatedAt,
		}
		if uid, ok := seedAdoptedBy[topic.ID]; ok {
			seed.AdoptedBy = &uid
			seed.AdoptedAt = strPtr(time.Now().Format(time.RFC3339))
		}
		result = append(result, seed)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	return result, nil
}

func (h *Handler) getSeedByID(ctx context.Context, tenantID *int64, id int64, tenantIDs ...int64) (*SeedResponse, error) {
	seeds, err := h.buildSeedsFromTopics(ctx, tenantID, tenantIDs...)
	if err != nil {
		return nil, err
	}
	for i := range seeds {
		if seeds[i].ID == id {
			return &seeds[i], nil
		}
	}
	return nil, fmt.Errorf("seed not found")
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
