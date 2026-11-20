package middleware

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// slowHTTPThreshold 默认 500ms；LINGCE_SLOW_HTTP_MS 可覆盖（<=0 表示关闭）。
func slowHTTPThreshold() time.Duration {
	value := strings.TrimSpace(os.Getenv("LINGCE_SLOW_HTTP_MS"))
	if value == "" {
		return 500 * time.Millisecond
	}
	ms, err := strconv.Atoi(value)
	if err != nil || ms <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(ms) * time.Millisecond
}

// Logger logs HTTP requests
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration_ms", duration.Milliseconds(),
			"remote_addr", r.RemoteAddr,
		}
		// 慢请求单独打 warn：日志里直接能筛出 "slow http"，不用按 ms 排序翻半天。
		if duration >= slowHTTPThreshold() {
			slog.Warn("slow http", append(attrs, "query", r.URL.RawQuery)...)
			return
		}
		slog.Info("http request", attrs...)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack preserves WebSocket upgrades when the request logger wraps a handler.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}
