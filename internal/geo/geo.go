// Package geo 把 IP 解析到城市（离线 ip2region v4 数据库）。
//
// 数据库路径默认 ./data/ip2region_v4.xdb，可用环境变量 IP2REGION_XDB 覆盖。
// 数据库缺失时不报错：City() 返回空串，业务照常运行。
package geo

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/lionsoul2014/ip2region/binding/golang/service"
)

var (
	once    sync.Once
	ip2r    *service.Ip2Region
	loadErr error
	cache   = map[string]string{}
	mu      sync.RWMutex
)

func dbPath() string {
	if p := strings.TrimSpace(os.Getenv("IP2REGION_XDB")); p != "" {
		return p
	}
	return "./data/ip2region_v4.xdb"
}

func initOnce() {
	cfg, err := service.NewV4Config(service.VIndexCache, dbPath(), 16)
	if err != nil {
		loadErr = err
		return
	}
	r, err := service.NewIp2Region(cfg, nil)
	if err != nil {
		loadErr = err
		return
	}
	ip2r = r
}

// City 返回 IP 的省份/城市，例如 "广东省 深圳市"；解析不到返回空串。
// 内网/回环地址（127.0.0.1、::1、10.x、192.168.x 等）直接返回空串。
func City(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || isPrivate(ip) {
		return ""
	}
	mu.RLock()
	if v, ok := cache[ip]; ok {
		mu.RUnlock()
		return v
	}
	mu.RUnlock()

	once.Do(initOnce)
	if ip2r == nil {
		return ""
	}
	raw, err := ip2r.Search(ip)
	if err != nil || raw == "" {
		return ""
	}
	// v4 格式：国家|省份|城市|ISP|国家代码，例如 中国|广东省|深圳市|电信|CN
	parts := strings.Split(raw, "|")
	province, city := "", ""
	if len(parts) > 1 {
		province = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		city = strings.TrimSpace(parts[2])
	}
	var out []string
	for _, p := range []string{province, city} {
		if p != "" && p != "0" {
			out = append(out, p)
		}
	}
	res := strings.Join(uniqueStrings(out), " ")
	mu.Lock()
	cache[ip] = res
	mu.Unlock()
	return res
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// isPrivate 判断内网 / 回环 / 保留地址。
func isPrivate(ip string) bool {
	if strings.HasPrefix(ip, "10.") || strings.HasPrefix(ip, "192.168.") || strings.HasPrefix(ip, "127.") ||
		strings.HasPrefix(ip, "::1") || strings.HasPrefix(ip, "fc") || strings.HasPrefix(ip, "fe80") {
		return true
	}
	parts := strings.Split(ip, ".")
	if len(parts) == 4 && parts[0] == "172" {
		if second, err := strconv.Atoi(parts[1]); err == nil && second >= 16 && second <= 31 {
			return true
		}
	}
	return false
}
