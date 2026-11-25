package badge

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	billingpdf "github.com/freeasyman/lingce-api/internal/billing/pdf"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

const replacementDocumentCompany = "天津灵策科技有限公司"

// ReplacementDocument 生成「工牌换新通知单」PDF：商务可直接转发客户。
func (h *Handler) ReplacementDocument(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("ticket_id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "Invalid ticket ID")
		return
	}
	ticket, err := h.service.GetTicketByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, "工单不存在")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || ticket.TenantID == nil || *claims.TenantID != *ticket.TenantID {
			httputil.WriteForbidden(w, "无权查看该工单")
			return
		}
	}
	if ticket.SwapDeviceNo == nil && ticket.OutboundTrackingNo == nil {
		httputil.WriteBadRequest(w, "该工单没有换货记录，无法生成通知单")
		return
	}

	tenantID := int64(0)
	if ticket.TenantID != nil {
		tenantID = *ticket.TenantID
	}
	advisor := billingpdf.Advisor{Name: "灵策服务团队", IsBrand: true}
	if name, phone, err := h.service.store.SalesAdvisor(r.Context(), tenantID); err == nil && name != "" {
		advisor = billingpdf.Advisor{Name: name, Phone: phone}
	}

	tenantName := derefString(ticket.TenantName)
	if tenantName == "" {
		tenantName = "贵机构"
	}
	deviceNo := derefString(ticket.DeviceNo)
	shippedAt := time.Now()
	if raw := derefString(ticket.ShippedAt); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			shippedAt = parsed
		}
	}
	letter := billingpdf.Letter{
		DocNo:       ticket.TicketNo,
		DocDate:     shippedAt.Format("2006-01-02"),
		Title:       "工牌换新通知单",
		Salutation:  fmt.Sprintf("尊敬的　%s：", tenantName),
		Intro:       fmt.Sprintf("关于您反馈的工牌使用问题（工单号 %s，原设备 %s），经核实为设备问题，我们已为您安排换新，具体如下：", ticket.TicketNo, deviceNo),
		InfoSection: "换新信息",
		InfoRows: []billingpdf.InfoRow{
			{Label: "原设备编号", Value: deviceNo},
			{Label: "换出设备编号", Value: derefString(ticket.SwapDeviceNo)},
			{Label: "寄出物流单号", Value: derefString(ticket.OutboundTrackingNo)},
			{Label: "寄出日期", Value: shippedAt.Format("2006-01-02")},
			{Label: "收件人", Value: derefString(ticket.RecipientName)},
			{Label: "收件电话", Value: derefString(ticket.RecipientPhone)},
			{Label: "收件地址", Value: derefString(ticket.RecipientAddress)},
		},
		ProductSection:     "寄回要求",
		QuotaHeader:        "说明",
		ProductColumnLabel: "项目",
		Products: []billingpdf.ProductRow{
			{Name: "旧机寄回", Quota: "收到新机后 7 日内寄回旧设备"},
			{Name: "寄回地址", Quota: "联系您的专属顾问获取寄回地址"},
			{Name: "随附信息", Quota: "寄回时请注明工单号 " + ticket.TicketNo},
		},
		StartSection: "换新流程",
		StartItems: []string{
			"我们已按本单收件信息寄出新设备，物流单号见上方信息。",
			"收到新机后请开机确认设备编号与通知单一致。",
			"请在 **7 日内** 将旧机寄回，寄回地址联系专属顾问获取。",
			"新机由我方完成绑定后即可正常录音，无需您重新操作。",
		},
		TipSection: "温馨提示",
		TipItems: []string{
			"旧机寄出前请确认设备已关机，避免运输过程中录入无关声音。",
			"如物流出现异常或超过 7 天未收到，请联系您的专属顾问。",
		},
		Advisor:         advisor,
		SealOverCompany: true,
		RealSeal:        true,
		Company:         replacementDocumentCompany,
		SignDate:        shippedAt.Format("2006 年 01 月 02 日"),
		FooterLeft:      "本通知单由灵策运营系统自动生成",
	}
	data, err := billingpdf.Render(letter)
	if err != nil {
		httputil.WriteInternalError(w, "生成通知单失败: "+err.Error())
		return
	}
	filename := fmt.Sprintf("工牌换新通知单-%s-%s.pdf", tenantName, ticket.TicketNo)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

var _ = strings.TrimSpace
