package content

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/freeasyman/lingce-api/internal/geo"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// campaignLandingURL 二维码/海报里的落地链接：不可读令牌。
// 正式域名待定，用 FREE_CHECKUP_BASE_URL 指定；缺省回退到当前请求 Host。
func (h *Handler) campaignLandingURL(r *http.Request, token string) string {
	base := strings.TrimRight(os.Getenv("FREE_CHECKUP_BASE_URL"), "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base + "/?t=" + token
}

// ResolvePublicFreeCheckupCampaign 解析令牌、记录访问，返回活动信息供 H5 显示"来自：xxx"。
func (h *Handler) ResolvePublicFreeCheckupCampaign(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("t"))
	if token == "" {
		httputil.WriteBadRequest(w, "token is required")
		return
	}
	c, err := h.service.store.GetFreeCheckupCampaignByToken(r.Context(), token)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "INVALID_TOKEN", "投放码无效", nil)
		return
	}
	if c.Status != "active" || (c.ExpiresAt != nil && time.Now().After(*c.ExpiresAt)) {
		httputil.WriteError(w, http.StatusGone, "CAMPAIGN_DISABLED", "该活动已结束", nil)
		return
	}
	sid := h.ensureSID(w, r)
	ip := requestIP(r)
	_ = h.service.store.RecordFreeCheckupScan(r.Context(), c.ID, sid, strings.TrimSpace(r.Header.Get("X-Device-Id")), ip, geo.City(ip), r.UserAgent())
	httputil.WriteSuccess(w, map[string]any{
		"campaign_id":  c.ID,
		"name":         c.Name,
		"scene":        c.Scene,
		"channel_name": c.ChannelName,
		"sales_name":   c.SalesName,
	})
}

func (h *Handler) ListFreeCheckupCampaignsForOperation(w http.ResponseWriter, r *http.Request) {
	limit, offset := 100, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	items, total, err := h.service.store.ListFreeCheckupCampaigns(r.Context(), limit, offset)
	if err != nil {
		writeContentError(w, err)
		return
	}
	for i := range items {
		items[i].LandingURL = h.campaignLandingURL(r, items[i].Token)
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total})
}

func (h *Handler) CreateFreeCheckupCampaignForOperation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		ChannelID   int64  `json:"channel_id"`
		ChannelName string `json:"channel_name"`
		SalesID     int64  `json:"sales_id"`
		SalesName   string `json:"sales_name"`
		Scene       string `json:"scene"`
		Remark      string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		httputil.WriteBadRequest(w, "name is required")
		return
	}
	c, err := h.service.store.CreateFreeCheckupCampaign(r.Context(), &FreeCheckupCampaign{
		Name: strings.TrimSpace(req.Name), ChannelID: req.ChannelID, ChannelName: strings.TrimSpace(req.ChannelName),
		SalesID: req.SalesID, SalesName: strings.TrimSpace(req.SalesName), Scene: strings.TrimSpace(req.Scene),
		Status: "active", Remark: strings.TrimSpace(req.Remark),
	})
	if err != nil {
		writeContentError(w, err)
		return
	}
	c.LandingURL = h.campaignLandingURL(r, c.Token)
	httputil.WriteSuccess(w, map[string]any{"campaign": c})
}

func (h *Handler) UpdateFreeCheckupCampaignStatusForOperation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Status != "active" && req.Status != "disabled") {
		httputil.WriteBadRequest(w, "status must be active or disabled")
		return
	}
	ok, err := h.service.store.UpdateFreeCheckupCampaignStatus(r.Context(), id, req.Status)
	if err != nil {
		writeContentError(w, err)
		return
	}
	if !ok {
		httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "投放码不存在", nil)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"id": id, "status": req.Status})
}

// UpdateFreeCheckupCampaignForOperation 编辑投放码：令牌不变；有数据后渠道/销售/场景冻结。
func (h *Handler) UpdateFreeCheckupCampaignForOperation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	var req struct {
		Name   string `json:"name"`
		Remark string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httputil.WriteBadRequest(w, "name is required")
		return
	}
	cur, err := h.service.store.GetFreeCheckupCampaignByID(r.Context(), id)
	if err != nil || cur == nil {
		httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "投放码不存在", nil)
		return
	}
	// 渠道 / 销售 / 场景建码即定，任何情况都不参与修改；只允许改名称与备注。
	ok, err := h.service.store.UpdateFreeCheckupCampaign(r.Context(), id,
		strings.TrimSpace(req.Name), strings.TrimSpace(req.Remark), cur.Scene,
		cur.ChannelID, cur.ChannelName, cur.SalesID, cur.SalesName)
	if err != nil {
		writeContentError(w, err)
		return
	}
	if !ok {
		httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "投放码不存在", nil)
		return
	}
	updated, _ := h.service.store.GetFreeCheckupCampaignByID(r.Context(), id)
	if updated != nil {
		updated.LandingURL = h.campaignLandingURL(r, updated.Token)
	}
	httputil.WriteSuccess(w, map[string]any{"campaign": updated})
}

// CloneFreeCheckupCampaignForOperation 复制投放码（新令牌，旧码保持有效）。
func (h *Handler) CloneFreeCheckupCampaignForOperation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	c, err := h.service.store.CloneFreeCheckupCampaign(r.Context(), id, strings.TrimSpace(req.Name))
	if err != nil {
		writeContentError(w, err)
		return
	}
	c.LandingURL = h.campaignLandingURL(r, c.Token)
	httputil.WriteSuccess(w, map[string]any{"campaign": c})
}

// MarkFreeCheckupCampaignPrintedForOperation 标记已印刷（下载过海报/二维码）。
func (h *Handler) MarkFreeCheckupCampaignPrintedForOperation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	ok, err := h.service.store.MarkFreeCheckupCampaignPrinted(r.Context(), id)
	if err != nil {
		writeContentError(w, err)
		return
	}
	if !ok {
		httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "投放码不存在", nil)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"id": id, "printed": true})
}

func (h *Handler) GetFreeCheckupCampaignQRForOperation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	c, err := h.service.store.GetFreeCheckupCampaignByID(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "投放码不存在", nil)
		return
	}
	png, err := qrcode.Encode(h.campaignLandingURL(r, c.Token), qrcode.High, 512)
	if err != nil {
		writeContentError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}
