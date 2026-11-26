package content

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// freeCheckupLimiter 免费体检的设备级限流（进程内，按设备号；无设备号按 IP）。
// 先满足"能限流"，后续可换 Redis 实现。
type freeCheckupLimiter struct {
	mu      sync.Mutex
	buckets map[string][]time.Time
}

var checkupLimiter = &freeCheckupLimiter{buckets: map[string][]time.Time{}}

// allow 返回 true 表示放行，false 表示超限。
func (l *freeCheckupLimiter) allow(key string, limit int, window time.Duration) bool {
	now := time.Now()
	cut := now.Add(-window)
	l.mu.Lock()
	defer l.mu.Unlock()
	times := l.buckets[key]
	kept := times[:0]
	for _, t := range times {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		l.buckets[key] = kept
		return false
	}
	l.buckets[key] = append(kept, now)
	return true
}

// clientKey 取设备号（X-Device-Id），缺省回退到客户端 IP。
func clientKey(r *http.Request) string {
	if d := strings.TrimSpace(r.Header.Get("X-Device-Id")); d != "" {
		return "dev:" + d
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "ip:" + r.RemoteAddr
	}
	return "ip:" + host
}
