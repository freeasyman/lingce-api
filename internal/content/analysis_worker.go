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
		WHERE r.business_scope = 'doctor'
		  AND COALESCE(NULLIF(r.transcription_text,''), NULLIF(r.cleaned_transcription::text,''), '') <> ''
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

// AnalyzeRecording 对一条录音做「洞察提取 + 生成选题」。
func (s *Service) AnalyzeRecording(ctx context.Context, tenantID, recordingID int64) (int, error) {
	insights, err := s.ExtractInsights(ctx, tenantID, recordingID)
	if err != nil {
		return 0, err
	}
	if len(insights) == 0 {
		return 0, nil
	}
	ids := make([]int64, 0, len(insights))
	for _, it := range insights {
		if it.ID > 0 {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 {
		return len(insights), nil
	}
	if _, err := s.GenerateTopicsFromInsights(ctx, tenantID, ids); err != nil {
		// 洞察已落库，选题生成失败不视为整体失败
		slog.Warn("generate topics from insights failed", "error", err, "tenant_id", tenantID, "recording_id", recordingID)
	}
	return len(insights), nil
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
				for _, p := range pending {
					n, err := s.AnalyzeRecording(ctx, p.TenantID, p.RecordingID)
					if err != nil {
						slog.Warn("analyze recording failed", "error", err, "tenant_id", p.TenantID, "recording_id", p.RecordingID)
						continue
					}
					slog.Info("recording analyzed", "tenant_id", p.TenantID, "recording_id", p.RecordingID, "insights", n)
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
		  AND r.business_scope = 'doctor'
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
