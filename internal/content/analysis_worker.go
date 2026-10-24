package content

import (
	"context"
	"log/slog"
	"time"
)

// 录音 → 洞察 → 选题 的自动闭环。
//
// 触发方式：后台定时补齐（不依赖 worker 的具体交付路径），
// 找出「有转写、但还没有洞察」的录音，跑自己的分析并生成选题。

type transcriptPending struct {
	TenantID    int64
	RecordingID int64
	EmployeeID  *int64
}

// listTranscriptsWithoutInsights 找出有转写但还没有洞察的录音。
func (s *Store) listTranscriptsWithoutInsights(ctx context.Context, limit int) ([]transcriptPending, error) {
	if limit <= 0 {
		limit = 3
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.tenant_id, r.id, r.employee_id
		FROM recordings r
		WHERE COALESCE(NULLIF(r.transcription_text,''), NULLIF(r.cleaned_transcription::text,''), '') <> ''
		  AND NOT EXISTS (SELECT 1 FROM content_insights ci WHERE ci.recording_id = r.id AND ci.tenant_id = r.tenant_id)
		ORDER BY r.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []transcriptPending
	for rows.Next() {
		var p transcriptPending
		if err := rows.Scan(&p.TenantID, &p.RecordingID, &p.EmployeeID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AnalyzeRecording 对一条录音做「洞察提取 + 生成选题」，返回 (洞察数, 选题数, err)。
func (s *Service) AnalyzeRecording(ctx context.Context, tenantID, recordingID int64) (int, int, error) {
	insights, err := s.ExtractInsights(ctx, tenantID, recordingID)
	if err != nil {
		return 0, 0, err
	}
	if len(insights) == 0 {
		return 0, 0, nil
	}
	ids := make([]int64, 0, len(insights))
	for _, it := range insights {
		if it.ID > 0 {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 {
		return len(insights), 0, nil
	}
	topics, terr := s.GenerateTopicsFromInsights(ctx, tenantID, ids)
	if terr != nil {
		slog.Warn("generate topics from insights failed", "error", terr, "tenant_id", tenantID, "recording_id", recordingID)
		return len(insights), 0, nil
	}
	return len(insights), len(topics), nil
}

// runAnalysisBatch 处理一批录音并记台账。
func (s *Service) runAnalysisBatch(ctx context.Context, tenantID int64, trigger string, items []transcriptPending) (done, failed, insights, topics int) {
	runID, err := s.store.createAnalysisRun(ctx, tenantID, trigger, len(items))
	if err != nil {
		slog.Warn("create analysis run failed", "error", err, "tenant_id", tenantID)
	}
	for _, p := range items {
		i, t, aerr := s.AnalyzeRecording(ctx, tenantID, p.RecordingID)
		if aerr != nil {
			failed++
			slog.Warn("analyze recording failed", "error", aerr, "tenant_id", tenantID, "recording_id", p.RecordingID)
			continue
		}
		done++
		insights += i
		topics += t
		slog.Info("recording analyzed", "tenant_id", tenantID, "recording_id", p.RecordingID, "insights", i, "topics", t)
	}
	if runID > 0 {
		_ = s.store.bumpAnalysisRun(ctx, runID, done, failed, insights, topics)
		status := "succeeded"
		if failed > 0 && done > 0 {
			status = "partial"
		} else if failed > 0 {
			status = "failed"
		}
		_ = s.store.finishAnalysisRun(ctx, runID, status, "")
	}
	return
}

// StartAnalysisWorker 启动后台补齐（每次最多处理 batch 条，避免压垮模型）。
func (s *Service) StartAnalysisWorker(ctx context.Context, interval time.Duration, batch int) {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	if batch <= 0 {
		batch = 2
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pending, err := s.store.listTranscriptsWithoutInsights(ctx, batch)
				if err != nil {
					slog.Warn("list transcripts without insights failed", "error", err)
					continue
				}
				// 按租户分组建台账
				groups := map[int64][]transcriptPending{}
				for _, p := range pending {
					groups[p.TenantID] = append(groups[p.TenantID], p)
				}
				for tenantID, items := range groups {
					s.runAnalysisBatch(ctx, tenantID, "worker", items)
				}
			}
		}
	}()
}

// listPendingTranscripts 本租户「有转写但无洞察」的录音（按最新优先）。
func (s *Store) listPendingTranscripts(ctx context.Context, tenantID int64, limit int) ([]transcriptPending, error) {
	if limit <= 0 {
		limit = 3
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.tenant_id, r.id, r.employee_id
		FROM recordings r
		WHERE r.tenant_id = $1
		  AND COALESCE(NULLIF(r.transcription_text,''), NULLIF(r.cleaned_transcription::text,''), '') <> ''
		  AND NOT EXISTS (SELECT 1 FROM content_insights ci WHERE ci.recording_id = r.id AND ci.tenant_id = r.tenant_id)
		ORDER BY GREATEST(length(COALESCE(r.transcription_text,'')), length(COALESCE(r.cleaned_transcription::text,''))) DESC, r.id DESC
		LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []transcriptPending
	for rows.Next() {
		var p transcriptPending
		if err := rows.Scan(&p.TenantID, &p.RecordingID, &p.EmployeeID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ===== 批次台账（任务化记录）=====

type analysisRun struct {
	ID         int64      `json:"id"`
	Trigger    string     `json:"trigger"`
	Status     string     `json:"status"`
	Total      int        `json:"total"`
	Done       int        `json:"done"`
	Failed     int        `json:"failed"`
	Insights   int        `json:"insights"`
	Topics     int        `json:"topics"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (s *Store) createAnalysisRun(ctx context.Context, tenantID int64, trigger string, total int) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO content_analysis_runs (tenant_id, trigger, status, total)
		VALUES ($1,$2,'running',$3) RETURNING id`, tenantID, trigger, total).Scan(&id)
	return id, err
}

func (s *Store) bumpAnalysisRun(ctx context.Context, runID int64, done, failed, insights, topics int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE content_analysis_runs
		SET done = done + $2, failed = failed + $3, insights = insights + $4, topics = topics + $5
		WHERE id = $1`, runID, done, failed, insights, topics)
	return err
}

func (s *Store) finishAnalysisRun(ctx context.Context, runID int64, status, errText string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE content_analysis_runs
		SET status = $2, error = NULLIF($3,''), finished_at = now()
		WHERE id = $1`, runID, status, errText)
	return err
}

func (s *Store) listAnalysisRuns(ctx context.Context, tenantID int64, limit int) ([]analysisRun, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, trigger, status, total, done, failed, insights, topics,
		       COALESCE(error,''), started_at, finished_at
		FROM content_analysis_runs
		WHERE tenant_id = $1
		ORDER BY started_at DESC, id DESC
		LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []analysisRun
	for rows.Next() {
		var r analysisRun
		if err := rows.Scan(&r.ID, &r.Trigger, &r.Status, &r.Total, &r.Done, &r.Failed, &r.Insights, &r.Topics, &r.Error, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) countPendingTranscripts(ctx context.Context, tenantID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM recordings r
		WHERE r.tenant_id = $1
		  AND COALESCE(NULLIF(r.transcription_text,''), NULLIF(r.cleaned_transcription::text,''), '') <> ''
		  AND NOT EXISTS (SELECT 1 FROM content_insights ci WHERE ci.recording_id = r.id AND ci.tenant_id = r.tenant_id)`,
		tenantID).Scan(&n)
	return n, err
}
