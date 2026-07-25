package support

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type AIUsageFilter struct {
	TenantID           *int64
	BusinessDomain     string
	BusinessObjectType string
	BillingSubject     string
	BillingScene       string
	Provider           string
	ModelCode          string
	Search             string
	Success            *bool
	StartDate          string
	EndDate            string
}

type AIUsageSummaryAggregate struct {
	TotalCalls          int64
	SuccessCalls        int64
	FailedCalls         int64
	TotalTokens         int64
	TotalInputTokens    int64
	TotalOutputTokens   int64
	TotalAudioSeconds   float64
	TotalCostCNY        float64
	AvgCostPerCall      float64
	AvgCostPerObject    float64
	DistinctObjectCount int64
}

type AIUsageObjectAggregate struct {
	ObjectID           int64
	BusinessDomain     string
	BusinessObjectType string
	BusinessObjectName string
	TenantID           int64
	TenantName         string
	BillingSubject     string
	BillingScene       string
	Role               string
	Scene              string
	TotalCalls         int64
	SuccessCalls       int64
	FailedCalls        int64
	TotalTokens        int64
	TotalAudioSeconds  float64
	TotalCostCNY       float64
	FirstCallAt        string
	LastCallAt         string
	Records            []AIUsageRecordItem
}

type AIUsageTopObjectItem struct {
	ObjectID           int64   `json:"object_id"`
	ObjectType         string  `json:"object_type"`
	ObjectName         string  `json:"object_name,omitempty"`
	TenantID           int64   `json:"tenant_id"`
	TenantName         string  `json:"tenant_name,omitempty"`
	CallCount          int64   `json:"call_count"`
	FailedCalls        int64   `json:"failed_calls"`
	TotalTokens        int64   `json:"total_tokens"`
	TotalAudioSeconds  float64 `json:"total_audio_seconds"`
	RecordingDuration  float64 `json:"recording_duration_seconds,omitempty"`
	TotalCostCNY       float64 `json:"total_cost_cny"`
	LastAt             string  `json:"last_at,omitempty"`
	Role               string  `json:"role,omitempty"`
	Scene              string  `json:"scene,omitempty"`
	BusinessDomain     string  `json:"business_domain,omitempty"`
	BusinessObjectType string  `json:"business_object_type,omitempty"`
}

type AIUsageObjectCostRow struct {
	ObjectID          int64   `json:"object_id"`
	ObjectType        string  `json:"object_type"`
	ObjectName        string  `json:"object_name,omitempty"`
	TenantID          int64   `json:"tenant_id"`
	TenantName        string  `json:"tenant_name,omitempty"`
	Role              string  `json:"role,omitempty"`
	Scene             string  `json:"scene,omitempty"`
	CallCount         int64   `json:"call_count"`
	TotalAudioSeconds float64 `json:"total_audio_seconds"`
	RecordingDuration float64 `json:"recording_duration_seconds,omitempty"`
	TotalCostCNY      float64 `json:"total_cost_cny"`
	LastAt            string  `json:"last_at,omitempty"`
}

type AIUsageRoleAverage struct {
	Role           string  `json:"role"`
	AvgCostPerItem float64 `json:"avg_cost_per_item"`
}

type AIUsageAnomalyItem struct {
	Key         string  `json:"key"`
	Type        string  `json:"type"`
	Level       string  `json:"level"`
	RequestID   string  `json:"request_id,omitempty"`
	ObjectID    int64   `json:"object_id,omitempty"`
	ObjectType  string  `json:"object_type,omitempty"`
	ObjectLabel string  `json:"object_label,omitempty"`
	TenantID    int64   `json:"tenant_id,omitempty"`
	TenantName  string  `json:"tenant_name,omitempty"`
	ModelCode   string  `json:"model_code,omitempty"`
	Cost        float64 `json:"cost"`
	Reason      string  `json:"reason"`
	SampleError string  `json:"sample_error,omitempty"`
	CreatedAt   string  `json:"created_at,omitempty"`
}

var gatewayCallRecordColumnCache sync.Map

func usageJSONText(column string) string {
	return "NULLIF(to_jsonb(g)->>'" + column + "', '')"
}

func usageJSONInt(column string) string {
	return "NULLIF(to_jsonb(g)->>'" + column + "', '')::bigint"
}

func usageRecordingIDExpr() string {
	return "COALESCE(" + usageJSONInt("recording_id") + ", NULLIF(substring(g.trace_id from '_(\\d+)_[0-9]+$'), '')::bigint)"
}

func usageContentIDExpr() string {
	return usageJSONInt("content_id")
}

func (s *Store) HasGatewayCallRecordColumn(ctx context.Context, column string) (bool, error) {
	if cached, ok := gatewayCallRecordColumnCache.Load(column); ok {
		if value, ok := cached.(bool); ok {
			return value, nil
		}
	}
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'gateway_call_records'
			  AND column_name = $1
		)
	`, column).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to detect gateway_call_records.%s: %w", column, err)
	}
	gatewayCallRecordColumnCache.Store(column, exists)
	return exists, nil
}

func buildUsageFilter(f AIUsageFilter, startArgIdx int) (string, []any) {
	args := make([]any, 0, 12)
	clauses := make([]string, 0, 12)
	argIdx := startArgIdx
	add := func(sql string, value any) {
		clauses = append(clauses, fmt.Sprintf(sql, argIdx))
		args = append(args, value)
		argIdx++
	}

	if f.TenantID != nil && *f.TenantID > 0 {
		add("g.tenant_id = $%d", *f.TenantID)
	}
	if v := strings.TrimSpace(f.BusinessDomain); v != "" {
		add("COALESCE("+usageJSONText("business_domain")+", '') = $%d", v)
	}
	if v := strings.TrimSpace(f.BusinessObjectType); v != "" {
		add("COALESCE("+usageJSONText("business_object_type")+", '') = $%d", v)
	}
	if v := strings.TrimSpace(f.BillingSubject); v != "" {
		add("COALESCE("+usageJSONText("billing_subject")+", '') = $%d", v)
	}
	if v := strings.TrimSpace(f.BillingScene); v != "" {
		add("COALESCE(NULLIF(r.scene_name, ''), NULLIF(r.scene::text, ''), '') = $%d", v)
	}
	if v := strings.TrimSpace(f.Provider); v != "" {
		add("g.provider = $%d", v)
	}
	if v := strings.TrimSpace(f.ModelCode); v != "" {
		add("g.model_code = $%d", v)
	}
	if f.Success != nil {
		add("g.success = $%d", *f.Success)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " AND " + strings.Join(clauses, " AND "), args
}

func normalizeUsageDateRange(startDate, endDate string) (time.Time, time.Time, error) {
	layout := "2006-01-02"
	now := time.Now()
	if strings.TrimSpace(endDate) == "" {
		endDate = now.Format(layout)
	}
	if strings.TrimSpace(startDate) == "" {
		startDate = now.AddDate(0, 0, -29).Format(layout)
	}
	startAt, err := time.ParseInLocation(layout, startDate, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start_date")
	}
	endAt, err := time.ParseInLocation(layout, endDate, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end_date")
	}
	return startAt, endAt.AddDate(0, 0, 1), nil
}

func usageObjectColumn(objectType string) string {
	if objectType == "content" {
		return usageContentIDExpr()
	}
	return usageRecordingIDExpr()
}

func usageObjectJoin(objectType string) string {
	if objectType == "content" {
		return "LEFT JOIN contents c ON c.id = " + usageContentIDExpr()
	}
	return "LEFT JOIN recordings r ON r.id = " + usageRecordingIDExpr()
}

func usageObjectNameSQL(objectType string) string {
	if objectType == "content" {
		return "COALESCE(NULLIF(c.title, ''), CONCAT('内容#', (" + usageContentIDExpr() + ")::text))"
	}
	return "COALESCE(NULLIF(r.scene_name, ''), NULLIF(r.scene::text, ''), CONCAT('录音#', (" + usageRecordingIDExpr() + ")::text))"
}

func usageObjectRoleSQL(objectType string) string {
	if objectType == "content" {
		return "COALESCE(" + usageJSONText("business_object_type") + ", 'content')"
	}
	return "COALESCE(NULLIF(r.resolved_role_code, ''), 'unknown')"
}

func usageObjectSceneSQL(objectType string) string {
	if objectType == "content" {
		return "COALESCE(" + usageJSONText("business_object_type") + ", 'content')"
	}
	return "COALESCE(NULLIF(r.scene_name, ''), NULLIF(r.scene::text, ''), '-')"
}

func usageObjectDurationSQL(objectType string) string {
	if objectType == "content" {
		return "0::double precision"
	}
	return "COALESCE(r.duration, 0)::double precision"
}

func usageAudioDurationSecondsSQL() string {
	return "COALESCE(NULLIF(g.usage_metadata->>'audio_duration_seconds', '')::double precision, 0)"
}

func usageASRDurationSQL() string {
	return "CASE WHEN COALESCE(" + usageJSONText("billing_subject") + ", NULLIF(g.function_type, '')) = 'transcription' THEN COALESCE(" + usageObjectDurationSQL("recording") + ", " + usageAudioDurationSecondsSQL() + ") ELSE 0 END"
}

func (s *Store) GetAIUsageSummaryAggregate(ctx context.Context, filter AIUsageFilter) (*AIUsageSummaryAggregate, error) {
	startAt, endAt, err := normalizeUsageDateRange(filter.StartDate, filter.EndDate)
	if err != nil {
		return nil, err
	}
	whereSQL, args := buildUsageFilter(filter, 3)
	args = append([]any{startAt, endAt}, args...)

	query := `
		SELECT
			COUNT(*) FILTER (WHERE g.success) AS success_calls,
			COUNT(*) FILTER (WHERE NOT g.success) AS failed_calls,
			COALESCE(SUM(g.total_cost) FILTER (WHERE g.success), 0) AS total_cost_cny,
			COALESCE(SUM(g.total_tokens) FILTER (WHERE g.success), 0) AS total_tokens,
			COALESCE(SUM(g.input_tokens) FILTER (WHERE g.success), 0) AS total_input_tokens,
			COALESCE(SUM(g.output_tokens) FILTER (WHERE g.success), 0) AS total_output_tokens,
			COALESCE(SUM(` + usageASRDurationSQL() + `) FILTER (WHERE g.success), 0) AS total_audio_seconds,
			COUNT(DISTINCT COALESCE(
				NULLIF(CONCAT('recording:', (` + usageRecordingIDExpr() + `)::text), 'recording:'),
				NULLIF(CONCAT('content:', (` + usageContentIDExpr() + `)::text), 'content:'),
				NULLIF(CONCAT('task:', (` + usageJSONInt("generation_task_id") + `)::text), 'task:'),
				NULLIF(CONCAT(COALESCE(` + usageJSONText("business_domain") + `, 'other'), ':', (` + usageJSONInt("business_object_id") + `)::text), CONCAT(COALESCE(` + usageJSONText("business_domain") + `, 'other'), ':'))
			)) FILTER (WHERE g.success) AS distinct_object_count
			FROM gateway_call_records g
			LEFT JOIN recordings r ON r.id = ` + usageRecordingIDExpr() + `
			WHERE g.created_at >= $1 AND g.created_at < $2` + whereSQL

	var row AIUsageSummaryAggregate
	if err := s.pool.QueryRow(ctx, query, args...).Scan(
		&row.SuccessCalls,
		&row.FailedCalls,
		&row.TotalCostCNY,
		&row.TotalTokens,
		&row.TotalInputTokens,
		&row.TotalOutputTokens,
		&row.TotalAudioSeconds,
		&row.DistinctObjectCount,
	); err != nil {
		return nil, fmt.Errorf("failed to query ai usage summary: %w", err)
	}
	row.TotalCalls = row.SuccessCalls + row.FailedCalls
	if row.SuccessCalls > 0 {
		row.AvgCostPerCall = row.TotalCostCNY / float64(row.SuccessCalls)
	}
	if row.DistinctObjectCount > 0 {
		row.AvgCostPerObject = row.TotalCostCNY / float64(row.DistinctObjectCount)
	}
	return &row, nil
}

func (s *Store) GetAIUsageTrendDaily(ctx context.Context, filter AIUsageFilter) ([]AIUsageTrendItem, error) {
	startAt, endAt, err := normalizeUsageDateRange(filter.StartDate, filter.EndDate)
	if err != nil {
		return nil, err
	}
	success := true
	filter.Success = &success
	whereSQL, args := buildUsageFilter(filter, 3)
	args = append([]any{startAt, endAt}, args...)
	query := `
		SELECT
			date_trunc('day', g.created_at)::date::text AS day,
			COUNT(*) AS call_count,
			COALESCE(SUM(g.total_tokens), 0) AS total_tokens,
			COALESCE(SUM(` + usageASRDurationSQL() + `), 0) AS total_audio_seconds,
			COALESCE(SUM(g.total_cost), 0) AS total_cost_cny
		FROM gateway_call_records g
		LEFT JOIN recordings r ON r.id = ` + usageRecordingIDExpr() + `
		WHERE g.created_at >= $1 AND g.created_at < $2` + whereSQL + `
		GROUP BY day
		ORDER BY day`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query ai usage trend: %w", err)
	}
	defer rows.Close()

	items := make([]AIUsageTrendItem, 0)
	for rows.Next() {
		var item AIUsageTrendItem
		if err := rows.Scan(&item.Date, &item.CallCount, &item.TotalTokens, &item.TotalAudioSeconds, &item.TotalCostCNY); err != nil {
			return nil, fmt.Errorf("failed to scan ai usage trend: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) queryAIUsageGroup(ctx context.Context, filter AIUsageFilter, groupExpr string) ([]AIUsageGroupItem, error) {
	startAt, endAt, err := normalizeUsageDateRange(filter.StartDate, filter.EndDate)
	if err != nil {
		return nil, err
	}
	success := true
	filter.Success = &success
	whereSQL, args := buildUsageFilter(filter, 3)
	args = append([]any{startAt, endAt}, args...)
	query := `
		SELECT ` + groupExpr + ` AS key,
		       COUNT(*) AS call_count,
		       COALESCE(SUM(g.total_cost), 0) AS total_cost_cny,
		       COALESCE(SUM(g.total_tokens), 0) AS total_tokens,
		       COALESCE(SUM(` + usageASRDurationSQL() + `), 0) AS total_audio_seconds
		FROM gateway_call_records g
		LEFT JOIN recordings r ON r.id = ` + usageRecordingIDExpr() + `
		WHERE g.created_at >= $1 AND g.created_at < $2` + whereSQL + `
		GROUP BY ` + groupExpr + `
		ORDER BY total_cost_cny DESC, key`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query ai usage group: %w", err)
	}
	defer rows.Close()

	items := make([]AIUsageGroupItem, 0)
	for rows.Next() {
		var item AIUsageGroupItem
		if err := rows.Scan(&item.Key, &item.CallCount, &item.TotalCostCNY, &item.TotalTokens, &item.TotalAudioSeconds); err != nil {
			return nil, fmt.Errorf("failed to scan ai usage group: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetAIUsageByTenantAggregates(ctx context.Context, filter AIUsageFilter) ([]AIUsageGroupItem, error) {
	items, err := s.queryAIUsageGroup(ctx, filter, "g.tenant_id::text")
	if err != nil {
		return nil, err
	}
	tenantIDs := make([]int64, 0, len(items))
	for i := range items {
		if items[i].Key == "" {
			continue
		}
		var tid int64
		if _, err := fmt.Sscanf(items[i].Key, "%d", &tid); err == nil && tid > 0 {
			items[i].TenantID = &tid
			tenantIDs = append(tenantIDs, tid)
		}
	}
	nameMap, err := s.GetTenantNameMap(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].TenantID == nil {
			continue
		}
		name := nameMap[*items[i].TenantID]
		items[i].TenantName = &name
	}
	return items, nil
}

func (s *Store) GetAIUsageByBusinessDomainAggregates(ctx context.Context, filter AIUsageFilter) ([]AIUsageGroupItem, error) {
	return s.queryAIUsageGroup(ctx, filter, "COALESCE("+usageJSONText("business_domain")+", 'unknown')")
}

func (s *Store) GetAIUsageByBillingSubjectAggregates(ctx context.Context, filter AIUsageFilter) ([]AIUsageGroupItem, error) {
	return s.queryAIUsageGroup(ctx, filter, "COALESCE("+usageJSONText("billing_subject")+", 'unknown')")
}

func (s *Store) GetAIUsageByCallerModuleAggregates(ctx context.Context, filter AIUsageFilter) ([]AIUsageGroupItem, error) {
	return s.queryAIUsageGroup(ctx, filter, "COALESCE(NULLIF(g.caller_module, ''), 'unknown')")
}

func (s *Store) GetAIUsageByModelAggregates(ctx context.Context, filter AIUsageFilter) ([]AIUsageGroupItem, error) {
	return s.queryAIUsageGroup(ctx, filter, "COALESCE(NULLIF(g.model_code, ''), 'unknown')")
}

func (s *Store) GetAIUsageTopObjects(ctx context.Context, objectType string, filter AIUsageFilter, limit int) ([]AIUsageTopObjectItem, error) {
	startAt, endAt, err := normalizeUsageDateRange(filter.StartDate, filter.EndDate)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}
	success := true
	filter.Success = &success
	if objectType == "content" {
	} else {
		objectType = "recording"
	}
	whereSQL, args := buildUsageFilter(filter, 3)
	args = append([]any{startAt, endAt}, args...)
	args = append(args, limit)
	objectColumn := usageObjectColumn(objectType)
	query := `
		SELECT ` + objectColumn + ` AS object_id,
		       MAX(g.tenant_id) AS tenant_id,
		       COUNT(*) AS call_count,
		       COALESCE(SUM(g.total_cost), 0) AS total_cost_cny,
		       COALESCE(SUM(g.total_tokens), 0) AS total_tokens,
		       COALESCE(SUM(` + usageASRDurationSQL() + `), 0) AS total_audio_seconds,
		       MAX(` + usageObjectDurationSQL(objectType) + `) AS recording_duration_seconds,
		       COUNT(*) FILTER (WHERE NOT g.success) AS failed_calls,
		       MAX(g.created_at)::text AS last_at,
		       MAX(` + usageObjectNameSQL(objectType) + `) AS object_name,
		       MAX(` + usageObjectRoleSQL(objectType) + `) AS role,
		       MAX(` + usageObjectSceneSQL(objectType) + `) AS scene
		FROM gateway_call_records g
		` + usageObjectJoin(objectType) + `
		WHERE ` + objectColumn + ` IS NOT NULL
		  AND g.created_at >= $1 AND g.created_at < $2` + whereSQL + `
		GROUP BY ` + objectColumn + `
		ORDER BY total_cost_cny DESC
		LIMIT $` + fmt.Sprintf("%d", len(args))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query ai usage top objects: %w", err)
	}
	defer rows.Close()

	items := make([]AIUsageTopObjectItem, 0)
	tenantIDs := make([]int64, 0)
	for rows.Next() {
		var item AIUsageTopObjectItem
		item.ObjectType = objectType
		item.BusinessDomain = objectType
		item.BusinessObjectType = objectType
		if err := rows.Scan(
			&item.ObjectID,
			&item.TenantID,
			&item.CallCount,
			&item.TotalCostCNY,
			&item.TotalTokens,
			&item.TotalAudioSeconds,
			&item.RecordingDuration,
			&item.FailedCalls,
			&item.LastAt,
			&item.ObjectName,
			&item.Role,
			&item.Scene,
		); err != nil {
			return nil, fmt.Errorf("failed to scan ai usage top object: %w", err)
		}
		if item.TenantID > 0 {
			tenantIDs = append(tenantIDs, item.TenantID)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	nameMap, err := s.GetTenantNameMap(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].TenantName = nameMap[items[i].TenantID]
	}
	return items, nil
}

func (s *Store) GetAIUsageObjectCostPage(ctx context.Context, objectType string, filter AIUsageFilter, sortKey string, page, pageSize int) ([]AIUsageObjectCostRow, int, []AIUsageRoleAverage, error) {
	startAt, endAt, err := normalizeUsageDateRange(filter.StartDate, filter.EndDate)
	if err != nil {
		return nil, 0, nil, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	success := true
	filter.Success = &success
	if objectType == "content" {
	} else {
		objectType = "recording"
	}
	objectColumn := usageObjectColumn(objectType)
	searchSQL := ""
	whereSQL, args := buildUsageFilter(filter, 3)
	args = append([]any{startAt, endAt}, args...)
	if v := strings.TrimSpace(filter.Search); v != "" {
		searchSQL = fmt.Sprintf(" AND %s::text = $%d", objectColumn, len(args)+1)
		args = append(args, v)
	}
	orderBy := "cost DESC"
	if sortKey == "last_at" {
		orderBy = "last_at DESC"
	}
	countSQL := `
		SELECT COUNT(*) FROM (
			SELECT ` + objectColumn + `
			FROM gateway_call_records g
			` + usageObjectJoin(objectType) + `
			WHERE ` + objectColumn + ` IS NOT NULL
			  AND g.created_at >= $1 AND g.created_at < $2
			  AND g.success` + whereSQL + searchSQL + `
			GROUP BY ` + objectColumn + `
		) t`
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, nil, fmt.Errorf("failed to count ai usage object costs: %w", err)
	}

	offset := (page - 1) * pageSize
	mainArgs := append(append([]any{}, args...), pageSize, offset)
	query := `
		SELECT ` + objectColumn + ` AS object_id,
		       MAX(g.tenant_id) AS tenant_id,
		       COUNT(*) AS call_count,
		       COALESCE(SUM(g.total_cost), 0) AS cost,
		       COALESCE(SUM((g.usage_metadata->>'audio_duration_seconds')::double precision), 0) AS audio_seconds,
		       MAX(` + usageObjectDurationSQL(objectType) + `) AS recording_duration_seconds,
		       MAX(g.created_at)::text AS last_at,
		       MAX(` + usageObjectRoleSQL(objectType) + `) AS role,
		       MAX(` + usageObjectSceneSQL(objectType) + `) AS scene,
		       MAX(` + usageObjectNameSQL(objectType) + `) AS object_name
		FROM gateway_call_records g
		` + usageObjectJoin(objectType) + `
		WHERE ` + objectColumn + ` IS NOT NULL
		  AND g.created_at >= $1 AND g.created_at < $2
		  AND g.success` + whereSQL + searchSQL + `
		GROUP BY ` + objectColumn + `
		ORDER BY ` + orderBy + `
		LIMIT $` + fmt.Sprintf("%d", len(args)+1) + ` OFFSET $` + fmt.Sprintf("%d", len(args)+2)
	rows, err := s.pool.Query(ctx, query, mainArgs...)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to query ai usage object costs: %w", err)
	}
	defer rows.Close()

	items := make([]AIUsageObjectCostRow, 0)
	tenantIDs := make([]int64, 0)
	for rows.Next() {
		var item AIUsageObjectCostRow
		item.ObjectType = objectType
		if err := rows.Scan(
			&item.ObjectID,
			&item.TenantID,
			&item.CallCount,
			&item.TotalCostCNY,
			&item.TotalAudioSeconds,
			&item.RecordingDuration,
			&item.LastAt,
			&item.Role,
			&item.Scene,
			&item.ObjectName,
		); err != nil {
			return nil, 0, nil, fmt.Errorf("failed to scan ai usage object cost row: %w", err)
		}
		tenantIDs = append(tenantIDs, item.TenantID)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, nil, err
	}
	nameMap, err := s.GetTenantNameMap(ctx, tenantIDs)
	if err != nil {
		return nil, 0, nil, err
	}
	for i := range items {
		items[i].TenantName = nameMap[items[i].TenantID]
	}

	avgQuery := `
		SELECT role, COALESCE(AVG(cost), 0) FROM (
			SELECT MAX(` + usageObjectRoleSQL(objectType) + `) AS role,
			       COALESCE(SUM(g.total_cost), 0) AS cost
			FROM gateway_call_records g
			` + usageObjectJoin(objectType) + `
			WHERE ` + objectColumn + ` IS NOT NULL
			  AND g.created_at >= $1 AND g.created_at < $2
			  AND g.success` + whereSQL + `
			GROUP BY ` + objectColumn + `
		) t
		GROUP BY role`
	avgRows, err := s.pool.Query(ctx, avgQuery, args[:len(args)-boolToInt(searchSQL != "")]...)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to query ai usage averages: %w", err)
	}
	defer avgRows.Close()
	averages := make([]AIUsageRoleAverage, 0)
	for avgRows.Next() {
		var item AIUsageRoleAverage
		if err := avgRows.Scan(&item.Role, &item.AvgCostPerItem); err != nil {
			return nil, 0, nil, fmt.Errorf("failed to scan ai usage averages: %w", err)
		}
		averages = append(averages, item)
	}
	return items, total, averages, avgRows.Err()
}

func (s *Store) GetAIUsageAnomalies(ctx context.Context, filter AIUsageFilter) ([]AIUsageAnomalyItem, error) {
	startAt, endAt, err := normalizeUsageDateRange(filter.StartDate, filter.EndDate)
	if err != nil {
		return nil, err
	}
	whereSQL, args := buildUsageFilter(filter, 3)
	args = append([]any{startAt, endAt}, args...)

	highCostSQL := `
			SELECT request_id,
			       COALESCE(` + usageRecordingIDExpr() + `, ` + usageContentIDExpr() + `, 0) AS object_id,
			       CASE WHEN ` + usageRecordingIDExpr() + ` IS NOT NULL THEN 'recording' WHEN ` + usageContentIDExpr() + ` IS NOT NULL THEN 'content' ELSE 'request' END AS object_type,
			       g.tenant_id,
			       g.model_code,
			       g.total_cost,
			       g.created_at::text
			FROM gateway_call_records g
			LEFT JOIN recordings r ON r.id = ` + usageRecordingIDExpr() + `
		WHERE g.success AND g.total_cost > 0
		  AND g.created_at >= $1 AND g.created_at < $2` + whereSQL + `
		ORDER BY g.total_cost DESC
		LIMIT 20`
	rows, err := s.pool.Query(ctx, highCostSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query ai usage anomalies: %w", err)
	}
	defer rows.Close()

	items := make([]AIUsageAnomalyItem, 0, 40)
	tenantIDs := make([]int64, 0)
	for rows.Next() {
		var item AIUsageAnomalyItem
		item.Type = "single_high_cost"
		item.Level = "high"
		item.Reason = "单次调用成本偏高"
		if err := rows.Scan(&item.RequestID, &item.ObjectID, &item.ObjectType, &item.TenantID, &item.ModelCode, &item.Cost, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan high-cost anomaly: %w", err)
		}
		item.Key = "call:" + item.RequestID
		item.ObjectLabel = fmt.Sprintf("%s#%d", item.ObjectType, item.ObjectID)
		tenantIDs = append(tenantIDs, item.TenantID)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	failedFilter := filter
	failedFilter.Success = nil
	failedWhere, failedFilterArgs := buildUsageFilter(failedFilter, 3)
	failedArgs := append([]any{startAt, endAt}, failedFilterArgs...)
	failedSQL := `
			SELECT COALESCE(` + usageRecordingIDExpr() + `, ` + usageContentIDExpr() + `, 0) AS object_id,
			       CASE WHEN ` + usageRecordingIDExpr() + ` IS NOT NULL THEN 'recording' WHEN ` + usageContentIDExpr() + ` IS NOT NULL THEN 'content' ELSE 'request' END AS object_type,
			       g.tenant_id,
			       COUNT(*) AS fail_count,
			       COALESCE(MAX(g.error_message), '') AS sample_error,
			       MAX(g.created_at)::text AS created_at
			FROM gateway_call_records g
			LEFT JOIN recordings r ON r.id = ` + usageRecordingIDExpr() + `
		WHERE NOT g.success
		  AND (` + usageRecordingIDExpr() + ` IS NOT NULL OR ` + usageContentIDExpr() + ` IS NOT NULL)
		  AND g.created_at >= $1 AND g.created_at < $2` + failedWhere + `
		GROUP BY object_id, object_type, g.tenant_id
		HAVING COUNT(*) >= 2
		ORDER BY fail_count DESC
		LIMIT 20`
	failedRows, err := s.pool.Query(ctx, failedSQL, failedArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query failed anomalies: %w", err)
	}
	defer failedRows.Close()
	for failedRows.Next() {
		var item AIUsageAnomalyItem
		var failCount int64
		item.Type = "failed_burn"
		item.Level = "high"
		if err := failedRows.Scan(&item.ObjectID, &item.ObjectType, &item.TenantID, &failCount, &item.SampleError, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan failed anomaly: %w", err)
		}
		item.Key = fmt.Sprintf("failed:%s:%d:%d", item.ObjectType, item.ObjectID, item.TenantID)
		item.ObjectLabel = fmt.Sprintf("%s#%d", item.ObjectType, item.ObjectID)
		item.Reason = fmt.Sprintf("%d 次失败调用", failCount)
		tenantIDs = append(tenantIDs, item.TenantID)
		items = append(items, item)
	}
	if err := failedRows.Err(); err != nil {
		return nil, err
	}
	nameMap, err := s.GetTenantNameMap(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].TenantName = nameMap[items[i].TenantID]
	}
	return items, nil
}

func (s *Store) ListAIUsageRecordRows(ctx context.Context, filter AIUsageFilter, req AIUsageListRequest) ([]*AIUsageRecordItem, int, error) {
	startAt, endAt, err := normalizeUsageDateRange(filter.StartDate, filter.EndDate)
	if err != nil {
		return nil, 0, err
	}
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}
	whereSQL, args := buildUsageFilter(filter, 3)
	args = append([]any{startAt, endAt}, args...)
	searchSQL := ""
	if req.RecordingID != nil && *req.RecordingID > 0 {
		searchSQL += fmt.Sprintf(" AND "+usageRecordingIDExpr()+" = $%d", len(args)+1)
		args = append(args, *req.RecordingID)
	}
	if req.ContentID != nil && *req.ContentID > 0 {
		searchSQL += fmt.Sprintf(" AND "+usageContentIDExpr()+" = $%d", len(args)+1)
		args = append(args, *req.ContentID)
	}
	if req.GenerationTaskID != nil && *req.GenerationTaskID > 0 {
		searchSQL += fmt.Sprintf(" AND "+usageJSONInt("generation_task_id")+" = $%d", len(args)+1)
		args = append(args, *req.GenerationTaskID)
	}
	if req.BusinessObjectID != nil && *req.BusinessObjectID > 0 {
		searchSQL += fmt.Sprintf(" AND "+usageJSONInt("business_object_id")+" = $%d", len(args)+1)
		args = append(args, *req.BusinessObjectID)
	}

	countSQL := `SELECT COUNT(*) FROM gateway_call_records g LEFT JOIN recordings r ON r.id = ` + usageRecordingIDExpr() + ` WHERE g.created_at >= $1 AND g.created_at < $2` + whereSQL + searchSQL
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count ai usage records: %w", err)
	}
	offset := (page - 1) * pageSize
	mainArgs := append(append([]any{}, args...), pageSize, offset)
	query := `
		SELECT g.id, g.request_id, COALESCE(g.trace_id, ''), g.tenant_id,
		       COALESCE(` + usageJSONText("business_domain") + `, ''), COALESCE(` + usageJSONText("business_object_type") + `, ''), COALESCE(` + usageJSONInt("business_object_id") + `, 0),
		       COALESCE(` + usageJSONText("billing_subject") + `, ''), COALESCE(` + usageJSONText("billing_scene") + `, ''), COALESCE(` + usageJSONText("billing_rule_version") + `, ''),
		       COALESCE(` + usageRecordingIDExpr() + `, 0), COALESCE(` + usageContentIDExpr() + `, 0), COALESCE(` + usageJSONInt("topic_id") + `, 0), COALESCE(` + usageJSONInt("customer_id") + `, 0),
		       COALESCE(` + usageJSONInt("analysis_run_id") + `, 0), COALESCE(` + usageJSONInt("analysis_step_run_id") + `, 0), COALESCE(` + usageJSONInt("generation_task_id") + `, 0),
		       COALESCE(g.function_type, ''), COALESCE(g.caller_module, ''), COALESCE(g.provider, ''), COALESCE(g.model_code, ''),
		       g.success, COALESCE(g.input_tokens, 0), COALESCE(g.output_tokens, 0), COALESCE(g.total_tokens, 0),
		       COALESCE(g.input_cost, 0), COALESCE(g.output_cost, 0), COALESCE(g.total_cost, 0),
		       COALESCE(` + usageASRDurationSQL() + `, 0), COALESCE(g.latency_ms, 0),
		       g.created_at::text
		FROM gateway_call_records g
		LEFT JOIN recordings r ON r.id = ` + usageRecordingIDExpr() + `
		WHERE g.created_at >= $1 AND g.created_at < $2` + whereSQL + searchSQL + `
		ORDER BY g.created_at DESC
		LIMIT $` + fmt.Sprintf("%d", len(args)+1) + ` OFFSET $` + fmt.Sprintf("%d", len(args)+2)
	rows, err := s.pool.Query(ctx, query, mainArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list ai usage records: %w", err)
	}
	defer rows.Close()
	items := make([]*AIUsageRecordItem, 0)
	tenantIDs := make([]int64, 0)
	for rows.Next() {
		var item AIUsageRecordItem
		if err := rows.Scan(
			&item.ID, &item.RequestID, &item.TraceID, &item.TenantID,
			&item.BusinessDomain, &item.BusinessObjectType, &item.BusinessObjectID,
			&item.BillingSubject, &item.BillingScene, &item.BillingRuleVersion,
			&item.RecordingID, &item.ContentID, &item.TopicID, &item.CustomerID,
			&item.AnalysisRunID, &item.AnalysisStepRunID, &item.GenerationTaskID,
			&item.FunctionType, &item.Module, &item.Provider, &item.ModelCode,
			&item.Success, &item.InputTokens, &item.OutputTokens, &item.TotalTokens,
			&item.InputCost, &item.OutputCost, &item.TotalCost,
			&item.AudioDurationSeconds, &item.LatencyMS, &item.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan ai usage record: %w", err)
		}
		tenantIDs = append(tenantIDs, item.TenantID)
		items = append(items, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	nameMap, err := s.GetTenantNameMap(ctx, tenantIDs)
	if err != nil {
		return nil, 0, err
	}
	for _, item := range items {
		item.TenantName = nameMap[item.TenantID]
	}
	return items, total, nil
}

func (s *Store) GetAIUsageObjectAggregate(ctx context.Context, objectType string, objectID int64, filter AIUsageFilter) (*AIUsageObjectAggregate, error) {
	startAt, endAt, err := normalizeUsageDateRange(filter.StartDate, filter.EndDate)
	if err != nil {
		return nil, err
	}
	objectColumn := usageObjectColumn(objectType)
	if objectType == "content" {
	} else {
		objectType = "recording"
	}
	whereSQL, args := buildUsageFilter(filter, 4)
	args = append([]any{startAt, endAt, objectID}, args...)
	query := `
		SELECT MAX(g.tenant_id) AS tenant_id,
		       COUNT(*) AS total_calls,
		       COUNT(*) FILTER (WHERE g.success) AS success_calls,
		       COUNT(*) FILTER (WHERE NOT g.success) AS failed_calls,
		       COALESCE(SUM(g.total_tokens), 0) AS total_tokens,
		       COALESCE(SUM(` + usageASRDurationSQL() + `), 0) AS total_audio_seconds,
		       COALESCE(SUM(g.total_cost) FILTER (WHERE g.success), 0) AS total_cost_cny,
		       MIN(g.created_at)::text AS first_call_at,
		       MAX(g.created_at)::text AS last_call_at,
		       MAX(` + usageObjectNameSQL(objectType) + `) AS object_name,
			       MAX(COALESCE(` + usageJSONText("billing_subject") + `, '')) AS billing_subject,
			       MAX(COALESCE(` + usageJSONText("billing_scene") + `, '')) AS billing_scene,
		       MAX(` + usageObjectRoleSQL(objectType) + `) AS role,
		       MAX(` + usageObjectSceneSQL(objectType) + `) AS scene
		FROM gateway_call_records g
		` + usageObjectJoin(objectType) + `
		WHERE ` + objectColumn + ` = $3
		  AND g.created_at >= $1 AND g.created_at < $2` + whereSQL
	var agg AIUsageObjectAggregate
	agg.ObjectID = objectID
	agg.BusinessDomain = objectType
	agg.BusinessObjectType = objectType
	if err := s.pool.QueryRow(ctx, query, args...).Scan(
		&agg.TenantID,
		&agg.TotalCalls,
		&agg.SuccessCalls,
		&agg.FailedCalls,
		&agg.TotalTokens,
		&agg.TotalAudioSeconds,
		&agg.TotalCostCNY,
		&agg.FirstCallAt,
		&agg.LastCallAt,
		&agg.BusinessObjectName,
		&agg.BillingSubject,
		&agg.BillingScene,
		&agg.Role,
		&agg.Scene,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query ai usage object aggregate: %w", err)
	}
	nameMap, err := s.GetTenantNameMap(ctx, []int64{agg.TenantID})
	if err != nil {
		return nil, err
	}
	agg.TenantName = nameMap[agg.TenantID]
	recordReq := AIUsageListRequest{Page: 1, PageSize: 200}
	if objectType == "content" {
		recordReq.ContentID = &objectID
	} else {
		recordReq.RecordingID = &objectID
	}
	records, _, err := s.ListAIUsageRecordRows(ctx, filter, recordReq)
	if err != nil {
		return nil, err
	}
	agg.Records = make([]AIUsageRecordItem, 0, len(records))
	for _, item := range records {
		if item != nil {
			agg.Records = append(agg.Records, *item)
		}
	}
	return &agg, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
