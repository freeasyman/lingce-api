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
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		req.Status = &status
	}
	if category := strings.TrimSpace(r.URL.Query().Get("seed_type")); category != "" {
		req.Category = &category
	}
	if category := strings.TrimSpace(r.URL.Query().Get("category")); category != "" {
		req.Category = &category
	}
	if clusterIDStr := strings.TrimSpace(r.URL.Query().Get("concern_cluster_id")); clusterIDStr != "" {
		if v, convErr := strconv.ParseInt(clusterIDStr, 10, 64); convErr == nil && v > 0 {
			req.ClusterID = &v
		}
	} else if clusterIDStr := strings.TrimSpace(r.URL.Query().Get("cluster_id")); clusterIDStr != "" {
		if v, convErr := strconv.ParseInt(clusterIDStr, 10, 64); convErr == nil && v > 0 {
			req.ClusterID = &v
		}
	}
	employeeIDFilter := int64(0)
	if employeeIDStr := strings.TrimSpace(r.URL.Query().Get("employee_id")); employeeIDStr != "" {
		if v, convErr := strconv.ParseInt(employeeIDStr, 10, 64); convErr == nil && v > 0 {
			employeeIDFilter = v
		}
	}
	startDateFilter := strings.TrimSpace(r.URL.Query().Get("start_date"))
	endDateFilter := strings.TrimSpace(r.URL.Query().Get("end_date"))

	seeds, err := h.buildSeedsFromRecordingTable(r.Context(), req.TenantID, req.TenantIDs...)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	seeds = filterSeeds(seeds, req, employeeIDFilter, startDateFilter, endDateFilter)
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
	seeds, err := h.buildSeedsFromRecordingTable(r.Context(), scope.TenantID, scope.TenantIDs...)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	response := &SeedStatsResponse{
		TotalSeeds: int64(len(seeds)),
	}
	for _, seed := range seeds {
		switch seed.Status {
		case "used":
			response.AdoptedSeeds++
		case "ignored":
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
	seeds, err := h.buildSeedsFromRecordingTable(r.Context(), scope.TenantID, scope.TenantIDs...)
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

	seeds, err := h.buildSeedsFromRecordingTable(r.Context(), claims.TenantID)
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
		httputil.WriteNotFound(w, "seed not found")
		return
	}

	seed, err := h.getSeedByID(r.Context(), scope.TenantID, id, scope.TenantIDs...)
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

	seeds, err := h.buildSeedsFromRecordingTable(r.Context(), claims.TenantID)
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

	seeds, err := h.buildSeedsFromRecordingTable(r.Context(), claims.TenantID)
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

	seeds, err := h.buildSeedsFromRecordingTable(r.Context(), claims.TenantID)
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

func (h *Handler) buildSeedsFromRecordingTable(ctx context.Context, tenantID *int64, tenantIDs ...int64) ([]SeedResponse, error) {
	scopeTenantIDs := mergeSeedTenantScope(tenantID, tenantIDs)
	if len(scopeTenantIDs) == 0 {
		return []SeedResponse{}, nil
	}

	query := `
		SELECT
			rcs.id,
			rcs.tenant_id,
			rcs.employee_id,
			e.name AS employee_name,
			rcs.recording_id,
			rcs.seed_type,
			rcs.topic,
			rcs.content_angle,
			rcs.suggested_platforms,
			rcs.viral_potential,
			rcs.patient_concern,
			rcs.status,
			rcs.concern_cluster_id,
			rcs.seed_data,
			rcs.created_at
		FROM recording_content_seeds rcs
		LEFT JOIN employees e
		  ON e.id = rcs.employee_id
		 AND e.deleted_at IS NULL
		WHERE rcs.tenant_id = ANY($1)
		ORDER BY rcs.id DESC
	`
	rows, err := h.service.store.pool.Query(ctx, query, scopeTenantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]SeedResponse, 0, 64)
	seedStateMu.RLock()
	defer seedStateMu.RUnlock()
	for rows.Next() {
		var (
			seedID         int64
			tid            int64
			employeeID     *int64
			employeeName   *string
			recordingID    *int64
			seedType       string
			topic          string
			contentAngle   *string
			suggestedRaw   []byte
			viralPotential string
			patientConcern *string
			statusRaw      string
			clusterID      *int64
			seedData       JSONObject
			createdAt      time.Time
		)
		if err := rows.Scan(
			&seedID,
			&tid,
			&employeeID,
			&employeeName,
			&recordingID,
			&seedType,
			&topic,
			&contentAngle,
			&suggestedRaw,
			&viralPotential,
			&patientConcern,
			&statusRaw,
			&clusterID,
			&seedData,
			&createdAt,
		); err != nil {
			return nil, err
		}

		status := mapRecordingSeedStatusToLegacy(statusRaw)
		if override, ok := seedStatusOverrides[seedID]; ok {
			status = mapRecordingSeedStatusToLegacy(override)
		}
		suggestedPlatforms := parseJSONStringArray(suggestedRaw)

		seedType = strings.TrimSpace(strings.ToLower(seedType))
		var seedTypePtr *string
		if seedType != "" {
			seedTypePtr = &seedType
		}
		viral := strings.TrimSpace(strings.ToLower(viralPotential))
		if viral == "" {
			viral = "medium"
		}

		seed := SeedResponse{
			ID:                 seedID,
			TenantID:           tid,
			EmployeeID:         employeeID,
			EmployeeName:       employeeName,
			RecordingID:        recordingID,
			SeedType:           seedTypePtr,
			Topic:              topic,
			ContentAngle:       contentAngle,
			SuggestedPlatforms: suggestedPlatforms,
			ViralPotential:     &viral,
			ConcernClusterID:   clusterID,
			Title:              topic,
			Content:            valueOrEmpty(patientConcern),
			Status:             status,
			ClusterID:          clusterID,
			SeedData:           seedData,
			ExtraData:          seedData,
			CreatedAt:          createdAt.Format(time.RFC3339),
			UpdatedAt:          createdAt.Format(time.RFC3339),
		}
		if uid, ok := seedAdoptedBy[seedID]; ok {
			seed.AdoptedBy = &uid
			seed.AdoptedAt = strPtr(time.Now().Format(time.RFC3339))
		}
		result = append(result, seed)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (h *Handler) getSeedByID(ctx context.Context, tenantID *int64, id int64, tenantIDs ...int64) (*SeedResponse, error) {
	scopeTenantIDs := mergeSeedTenantScope(tenantID, tenantIDs)
	if len(scopeTenantIDs) == 0 {
		return nil, fmt.Errorf("seed not found")
	}

	if seed, found, err := h.getSeedByIDFromRecordingTable(ctx, id, scopeTenantIDs); err != nil {
		return nil, err
	} else if found {
		return seed, nil
	}

	return nil, fmt.Errorf("seed not found")
}

func (h *Handler) getSeedByIDFromRecordingTable(ctx context.Context, seedID int64, tenantIDs []int64) (*SeedResponse, bool, error) {
	query := `
		SELECT
			rcs.id,
			rcs.tenant_id,
			rcs.employee_id,
			e.name AS employee_name,
			rcs.recording_id,
			rcs.seed_type,
			rcs.topic,
			rcs.content_angle,
			rcs.suggested_platforms,
			rcs.viral_potential,
			rcs.patient_concern,
			rcs.status,
			rcs.concern_cluster_id,
			rcs.seed_data,
			rcs.created_at
		FROM recording_content_seeds rcs
		LEFT JOIN employees e
		  ON e.id = rcs.employee_id
		 AND e.deleted_at IS NULL
		WHERE rcs.id = $1
		  AND rcs.tenant_id = ANY($2)
		LIMIT 1
	`
	rows, err := h.service.store.pool.Query(ctx, query, seedID, tenantIDs)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false, nil
	}

	var (
		id             int64
		tid            int64
		employeeID     *int64
		employeeName   *string
		recordingID    *int64
		seedType       string
		topic          string
		contentAngle   *string
		suggestedRaw   []byte
		viralPotential string
		patientConcern *string
		statusRaw      string
		clusterID      *int64
		seedData       JSONObject
		createdAt      time.Time
	)
	if err := rows.Scan(
		&id,
		&tid,
		&employeeID,
		&employeeName,
		&recordingID,
		&seedType,
		&topic,
		&contentAngle,
		&suggestedRaw,
		&viralPotential,
		&patientConcern,
		&statusRaw,
		&clusterID,
		&seedData,
		&createdAt,
	); err != nil {
		return nil, false, err
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	status := mapRecordingSeedStatusToLegacy(statusRaw)
	if override, ok := seedStatusOverrides[id]; ok {
		status = mapRecordingSeedStatusToLegacy(override)
	}
	seedType = strings.TrimSpace(strings.ToLower(seedType))
	var seedTypePtr *string
	if seedType != "" {
		seedTypePtr = &seedType
	}
	viral := strings.TrimSpace(strings.ToLower(viralPotential))
	if viral == "" {
		viral = "medium"
	}
	seed := &SeedResponse{
		ID:                 id,
		TenantID:           tid,
		EmployeeID:         employeeID,
		EmployeeName:       employeeName,
		RecordingID:        recordingID,
		SeedType:           seedTypePtr,
		Topic:              topic,
		ContentAngle:       contentAngle,
		SuggestedPlatforms: parseJSONStringArray(suggestedRaw),
		ViralPotential:     &viral,
		ConcernClusterID:   clusterID,
		SeedData:           seedData,
		Title:              topic,
		Content:            valueOrEmpty(patientConcern),
		Status:             status,
		ClusterID:          clusterID,
		ExtraData:          seedData,
		CreatedAt:          createdAt.Format(time.RFC3339),
		UpdatedAt:          createdAt.Format(time.RFC3339),
	}
	return seed, true, nil
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func mergeSeedTenantScope(tenantID *int64, tenantIDs []int64) []int64 {
	set := make(map[int64]struct{}, len(tenantIDs)+1)
	if tenantID != nil && *tenantID > 0 {
		set[*tenantID] = struct{}{}
	}
	for _, id := range tenantIDs {
		if id > 0 {
			set[id] = struct{}{}
		}
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func mapRecordingSeedStatusToLegacy(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "used", "adopted", "draft_generated":
		return "used"
	case "ignored", "dismissed":
		return "ignored"
	default:
		return "pending"
	}
}

func mergeSeedResults(primary []SeedResponse, fallback []SeedResponse) []SeedResponse {
	if len(primary) == 0 {
		return fallback
	}
	if len(fallback) == 0 {
		return primary
	}

	indexByID := make(map[int64]int, len(fallback))
	merged := make([]SeedResponse, 0, len(primary)+len(fallback))
	for _, item := range fallback {
		indexByID[item.ID] = len(merged)
		merged = append(merged, item)
	}

	for _, item := range primary {
		if pos, ok := indexByID[item.ID]; ok {
			merged[pos] = item
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func filterSeeds(seeds []SeedResponse, req SeedListRequest, employeeID int64, startDate, endDate string) []SeedResponse {
	if len(seeds) == 0 {
		return seeds
	}
	seedTypeSet := parseCSVSet(valueOrString(req.Category))
	statusSet := parseStatusSet(valueOrString(req.Status))
	filterByCluster := req.ClusterID != nil && *req.ClusterID > 0
	filterByEmployee := employeeID > 0
	start := parseDateAtStart(startDate)
	endExclusive := parseDateEndExclusive(endDate)

	out := make([]SeedResponse, 0, len(seeds))
	for _, seed := range seeds {
		if len(seedTypeSet) > 0 {
			typ := strings.TrimSpace(strings.ToLower(valueOrString(seed.SeedType)))
			if typ == "" || !seedTypeSet[typ] {
				continue
			}
		}
		if len(statusSet) > 0 {
			normalized := normalizeFilterStatus(seed.Status)
			if !statusSet[normalized] {
				continue
			}
		}
		if filterByCluster {
			if seed.ClusterID == nil || *seed.ClusterID != *req.ClusterID {
				continue
			}
		}
		if filterByEmployee {
			if seed.EmployeeID == nil || *seed.EmployeeID != employeeID {
				continue
			}
		}
		if !start.IsZero() || !endExclusive.IsZero() {
			createdAt, err := time.Parse(time.RFC3339, seed.CreatedAt)
			if err != nil {
				createdAt, err = time.Parse(time.RFC3339Nano, seed.CreatedAt)
			}
			if err == nil {
				if !start.IsZero() && createdAt.Before(start) {
					continue
				}
				if !endExclusive.IsZero() && !createdAt.Before(endExclusive) {
					continue
				}
			}
		}
		out = append(out, seed)
	}
	return out
}

func parseCSVSet(raw string) map[string]bool {
	result := map[string]bool{}
	if strings.TrimSpace(raw) == "" {
		return result
	}
	for _, item := range strings.Split(raw, ",") {
		v := strings.TrimSpace(strings.ToLower(item))
		if v != "" {
			result[v] = true
		}
	}
	return result
}

func parseStatusSet(raw string) map[string]bool {
	result := map[string]bool{}
	for status := range parseCSVSet(raw) {
		result[normalizeFilterStatus(status)] = true
	}
	return result
}

func normalizeFilterStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "used", "adopted", "draft_generated":
		return "used"
	case "ignored", "dismissed":
		return "ignored"
	default:
		return "pending"
	}
}

func valueOrString(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return strings.TrimSpace(*ptr)
}

func parseDateAtStart(raw string) time.Time {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parseDateEndExclusive(raw string) time.Time {
	start := parseDateAtStart(raw)
	if start.IsZero() {
		return time.Time{}
	}
	return start.Add(24 * time.Hour)
}

func parseJSONStringArray(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			v := strings.TrimSpace(strings.ToLower(item))
			if v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	var generic []interface{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil
	}
	out := make([]string, 0, len(generic))
	for _, item := range generic {
		if s, ok := item.(string); ok {
			v := strings.TrimSpace(strings.ToLower(s))
			if v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

func (h *Handler) loadEmployeeNames(ctx context.Context, employeeIDs []int64, tenantIDs []int64) (map[int64]string, error) {
	if len(employeeIDs) == 0 || len(tenantIDs) == 0 {
		return map[int64]string{}, nil
	}
	query := `
		SELECT e.id,
		       CASE
		         WHEN e.full_name IS NULL OR e.full_name = '' OR e.full_name = 'unknown'
		         THEN COALESCE(NULLIF(e.name, ''), NULLIF(e.username, ''), '')
		         ELSE e.full_name
		       END AS full_name
		FROM employees e
		WHERE e.deleted_at IS NULL
		  AND e.id = ANY($1)
		  AND e.tenant_id = ANY($2)
	`
	rows, err := h.service.store.pool.Query(ctx, query, employeeIDs, tenantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]string, len(employeeIDs))
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		if strings.TrimSpace(name) != "" {
			result[id] = strings.TrimSpace(name)
		}
	}
	return result, rows.Err()
}

func inferSeedType(topic *TopicResponse) string {
	if topic == nil {
		return ""
	}
	candidates := []string{}
	if topic.ExtraData != nil {
		candidates = append(candidates,
			toSeedTypeValue(topic.ExtraData["seed_type"]),
			toSeedTypeValue(topic.ExtraData["type"]),
			toSeedTypeValue(topic.ExtraData["content_seed_type"]),
		)
	}
	for _, c := range candidates {
		if isValidSeedType(c) {
			return c
		}
	}
	category := strings.TrimSpace(strings.ToLower(valueOrEmpty(topic.Category)))
	switch category {
	case "relatable_scene", "golden_quote", "aha_moment", "practical_qa", "concern_handling":
		return category
	}
	return ""
}

func toSeedTypeValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(strings.ToLower(val))
	default:
		return ""
	}
}

func isValidSeedType(v string) bool {
	switch v {
	case "relatable_scene", "golden_quote", "aha_moment", "practical_qa", "concern_handling":
		return true
	default:
		return false
	}
}

func inferContentAngle(topic *TopicResponse) string {
	if topic == nil {
		return ""
	}
	if topic.ExtraData != nil {
		for _, key := range []string{"content_angle", "angle", "summary", "description", "content"} {
			if v := toStringValue(topic.ExtraData[key]); v != "" {
				return v
			}
		}
	}
	return strings.TrimSpace(valueOrEmpty(topic.Description))
}

func inferSuggestedPlatforms(topic *TopicResponse) []string {
	if topic == nil || topic.ExtraData == nil {
		return nil
	}
	for _, key := range []string{"suggested_platforms", "recommended_platforms", "platforms"} {
		if arr := toStringSlice(topic.ExtraData[key]); len(arr) > 0 {
			return arr
		}
	}
	return nil
}

func inferViralPotential(topic *TopicResponse) string {
	if topic == nil || topic.ExtraData == nil {
		return "medium"
	}
	raw := strings.TrimSpace(strings.ToLower(toStringValue(topic.ExtraData["viral_potential"])))
	switch {
	case strings.HasPrefix(raw, "high"):
		return "high"
	case strings.HasPrefix(raw, "low"):
		return "low"
	case strings.HasPrefix(raw, "medium"):
		return "medium"
	default:
		return "medium"
	}
}

func inferConcernClusterID(topic *TopicResponse) *int64 {
	if topic == nil || topic.ExtraData == nil {
		return nil
	}
	for _, key := range []string{"concern_cluster_id", "cluster_id"} {
		if v := toInt64Ptr(topic.ExtraData[key]); v != nil && *v > 0 {
			return v
		}
	}
	return nil
}

func toStringValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	default:
		return ""
	}
}

func toStringSlice(v interface{}) []string {
	switch val := v.(type) {
	case []string:
		out := make([]string, 0, len(val))
		for _, item := range val {
			s := strings.TrimSpace(strings.ToLower(item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				v := strings.TrimSpace(strings.ToLower(s))
				if v != "" {
					out = append(out, v)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func toInt64Ptr(v interface{}) *int64 {
	switch val := v.(type) {
	case int64:
		return &val
	case int:
		n := int64(val)
		return &n
	case float64:
		n := int64(val)
		return &n
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil
		}
		return &n
	default:
		return nil
	}
}
