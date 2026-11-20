package store

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// slowQueryTracer 只记录超过阈值的 SQL：平时零输出，出现慢查询时把
// SQL 片段、参数个数、耗时打进日志，便于直接定位是哪个查询变慢。
//
// 说明：参数值可能是敏感信息，这里只记录个数，不记录具体值。
type slowQueryTracer struct {
	threshold time.Duration
}

type slowQueryKey struct{}

type slowQueryInfo struct {
	started time.Time
	sql     string
	args    int
}

func (t *slowQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	// TraceQueryEndData 只带 CommandTag/Err，SQL 与参数必须在开始时自己带上。
	return context.WithValue(ctx, slowQueryKey{}, slowQueryInfo{started: time.Now(), sql: data.SQL, args: len(data.Args)})
}

func (t *slowQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	info, ok := ctx.Value(slowQueryKey{}).(slowQueryInfo)
	if !ok {
		return
	}
	elapsed := time.Since(info.started)
	if elapsed < t.threshold {
		return
	}
	attrs := []any{
		"duration_ms", elapsed.Milliseconds(),
		"sql", compactSQL(info.sql),
		"args", info.args,
	}
	if data.Err != nil {
		attrs = append(attrs, "error", data.Err.Error())
	}
	slog.Warn("slow sql", attrs...)
}

// compactSQL 去掉换行与多余空白，日志里一行能看完。
func compactSQL(sql string) string {
	compact := strings.Join(strings.Fields(sql), " ")
	if len(compact) > 400 {
		return compact[:400] + "…"
	}
	return compact
}
