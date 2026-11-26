package content

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// 免费体检每日额度：签名 Cookie（HTTP-only）为主身份，设备号兜底，IP 只做防刷上限。
const (
	freeCheckupDailyLimitDefault = 3
	freeCheckupIPDailyCap        = 1500 // 展会场景：几百人扫码，留足余量
	freeCheckupCookieName        = "fc_sid"
	freeCheckupCookieDays        = 30
)

// freeCheckupDailyLimit 可临时通过 FREE_CHECKUP_DAILY_LIMIT 调整（测试/活动），默认 3。
var freeCheckupDailyLimit = freeCheckupDailyLimitFromEnv()

func freeCheckupDailyLimitFromEnv() int {
	if raw := strings.TrimSpace(os.Getenv("FREE_CHECKUP_DAILY_LIMIT")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			return value
		}
	}
	return freeCheckupDailyLimitDefault
}

// signSID 用服务端密钥对 "sid.exp" 做 HMAC，防伪造。
func (h *Handler) signSID(sid string, exp int64) string {
	mac := hmac.New(sha256.New, []byte(h.cookieSecret+"|fc_sid"))
	mac.Write([]byte(sid + "." + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *Handler) makeSIDCookie() (string, int64) {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	sid := hex.EncodeToString(buf)
	exp := time.Now().Add(time.Duration(freeCheckupCookieDays) * 24 * time.Hour).Unix()
	return sid + "." + strconv.FormatInt(exp, 10) + "." + h.signSID(sid, exp), exp
}

// readSID 校验 Cookie；无效返回空。
func (h *Handler) readSID(r *http.Request) string {
	c, err := r.Cookie(freeCheckupCookieName)
	if err != nil || c.Value == "" {
		return ""
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 {
		return ""
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return ""
	}
	if !hmac.Equal([]byte(h.signSID(parts[0], exp)), []byte(parts[2])) {
		return ""
	}
	return parts[0]
}

// ensureSID 读取或签发 Cookie，并在响应里设置。
func (h *Handler) ensureSID(w http.ResponseWriter, r *http.Request) string {
	if sid := h.readSID(r); sid != "" {
		return sid
	}
	value, exp := h.makeSIDCookie()
	http.SetCookie(w, &http.Cookie{
		Name:     freeCheckupCookieName,
		Value:    value,
		Path:     "/",
		Expires:  time.Unix(exp, 0),
		MaxAge:   freeCheckupCookieDays * 24 * 3600,
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteLaxMode,
	})
	parts := strings.Split(value, ".")
	return parts[0]
}
