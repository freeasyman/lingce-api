package dealexpush

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EmailDeliveryRetention 是邮件投递记录保留期。
const EmailDeliveryRetention = 180 * 24 * time.Hour

// StartEmailRetentionWorker 启动每日清理：删除超过 180 天的投递记录，不动推送事件。
func StartEmailRetentionWorker(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	if pool == nil {
		return
	}
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	run := func() {
		tag, err := pool.Exec(ctx, `DELETE FROM deal_email_deliveries WHERE created_at < NOW() - INTERVAL '180 days'`)
		if err != nil {
			slog.Error("deal email retention cleanup failed", "error", err)
			return
		}
		if tag.RowsAffected() > 0 {
			slog.Info("deal email retention cleanup finished", "deleted", tag.RowsAffected())
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
