package dealexpush

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
)

// 邮件文案约束：只放客户称呼（姓氏+先生/女士），不放金额、病情、全名。
var (
	emailMoneyPattern = regexp.MustCompile(`(?i)(?:[¥￥]\s*\d+(?:\.\d+)?|\d+(?:\.\d+)?\s*(?:万元|万|元|块))`)
	emailPhonePattern = regexp.MustCompile(`1[3-9]\d{9}`)
	// 诊疗信息脱敏：只保留业务动作，不把病情/术式带进邮件。
	emailMedicalPattern = regexp.MustCompile(`干眼|类风湿|后遗症|角膜|交联|散光|近视|远视|老花|白内障|青光眼|眼压|眼底|术前|术后|手术|用药|生物制剂|病史|诊断|病情|度数`)
	// 邮件客户端会把“48小时内”识别成日期/日程链接，改成自然语言。
	emailHourWindowPattern = regexp.MustCompile(`(\d+)\s*小时内?`)
)

// buildEmailCopy 生成纯文本 + HTML 两版正文；主题只含客户称呼。
func buildEmailCopy(eventType, customerName, salutation, recipientName, consultantName string, recordedAt time.Time, analysis *AnalysisCopy, link string) (subject, text, htmlBody string) {
	name := strings.TrimSpace(salutation)
	if name == "" {
		name = "客户"
	}
	greeting := strings.TrimSpace(recipientName)
	if greeting == "" {
		greeting = "您好"
	} else {
		greeting += "，您好"
	}

	if analysis == nil {
		analysis = &AnalysisCopy{}
	}

	if eventType == "manager" {
		reason := sanitizeEmailText(analysis.ManagerReason, customerName, salutation)
		if reason == "" {
			reason = "咨询师在关键节点需要支持，建议今天介入一次。"
		}
		action := sanitizeEmailText(analysis.ManagerAction, customerName, salutation)
		if action == "" {
			action = "与咨询师对齐一次，明确下一步跟进口径。"
		}
		subject = "紧要介入提醒"
		if consultantName != "" {
			subject += " · " + consultantName
		}
		if name != "" {
			subject += " · " + name
		}
		contextFields := make([]emailField, 0, 5)
		if consultantName != "" {
			contextFields = append(contextFields, emailField{Label: "咨询师", Value: consultantName})
		}
		if !recordedAt.IsZero() {
			contextFields = append(contextFields, emailField{Label: "接待时间", Value: recordedAt.Local().Format("2006-01-02 15:04")})
		}
		contextFields = append(contextFields, emailField{Label: "客户", Value: name})
		contextFields = append(contextFields,
			emailField{Label: "原因", Value: reason},
			emailField{Label: "建议动作", Value: action},
		)
		textLines := []string{greeting + "：", "", "这位客户需要您介入处理。", ""}
		for _, field := range contextFields {
			textLines = append(textLines, field.Label+"："+field.Value)
		}
		textLines = append(textLines, "", "查看详情："+link, "", "本邮件由灵策智能 · 赢客专家系统自动发送，请勿回复。")
		text = joinEmailLines(textLines...)
		htmlBody = renderEmailHTML(greeting+"：", []string{"这位客户需要您介入处理。"}, contextFields, link)
		return subject, text, htmlBody
	}

	judgment := "客户已完成本次接待分析，建议今天内跟进一次。"
	if status := sanitizeEmailText(analysis.DealStatus, customerName, salutation); status != "" {
		judgment = fmt.Sprintf("客户当前处于「%s」阶段，建议今天内跟进一次。", status)
	}
	action := "打开待办，选择一条最合适的跟进动作并执行。"
	if task := sanitizeEmailText(analysis.FirstTaskTitle, customerName, salutation); task != "" {
		action = "今天按待办执行：" + task + "。"
	}
	subject = fmt.Sprintf("接待分析已完成 · %s", name)
	text = joinEmailLines(
		greeting+"：",
		"",
		"本次接待分析已完成。"+judgment,
		"",
		"建议动作："+action,
		"",
		"查看详情："+link,
		"",
		"本邮件由灵策智能 · 赢客专家系统自动发送，请勿回复。",
	)
	consultantFields := make([]emailField, 0, 3)
	if !recordedAt.IsZero() {
		consultantFields = append(consultantFields, emailField{Label: "接待时间", Value: recordedAt.Local().Format("2006-01-02 15:04")})
	}
	consultantFields = append(consultantFields, emailField{Label: "建议动作", Value: action})
	text = joinEmailLines(
		greeting+"：",
		"",
		"本次接待分析已完成。"+judgment,
		"",
		"接待时间："+recordedAt.Local().Format("2006-01-02 15:04"),
		"建议动作："+action,
		"",
		"查看详情："+link,
		"",
		"本邮件由灵策智能 · 赢客专家系统自动发送，请勿回复。",
	)
	htmlBody = renderEmailHTML(greeting+"：", []string{
		"本次接待分析已完成。" + judgment,
	}, consultantFields, link)
	return subject, text, htmlBody
}

type emailField struct {
	Label string
	Value string
}

// renderEmailHTML 生成简单、无外链资源的 HTML 邮件。
func renderEmailHTML(greeting string, paragraphs []string, fields []emailField, link string) string {
	var builder strings.Builder
	builder.WriteString(`<!DOCTYPE html><html><head><meta charset="UTF-8"></head>`)
	builder.WriteString(`<body style="margin:0;padding:24px;background:#f8fafc;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC',sans-serif;color:#1f2937;line-height:1.7">`)
	builder.WriteString(`<div style="max-width:560px;margin:0 auto;background:#ffffff;border-radius:12px;padding:28px 32px">`)
	builder.WriteString(`<p style="margin:0 0 16px">` + html.EscapeString(greeting) + `</p>`)
	for _, paragraph := range paragraphs {
		builder.WriteString(`<p style="margin:0 0 16px">` + html.EscapeString(paragraph) + `</p>`)
	}
	for _, field := range fields {
		builder.WriteString(`<p style="margin:0 0 12px"><strong>` + html.EscapeString(field.Label) + `：</strong>` + html.EscapeString(field.Value) + `</p>`)
	}
	builder.WriteString(`<p style="margin:24px 0"><a href="` + html.EscapeString(link) + `" style="display:inline-block;background:#2563eb;color:#ffffff;text-decoration:none;padding:10px 20px;border-radius:8px">查看详情</a></p>`)
	builder.WriteString(`<p style="margin:24px 0 0;color:#9ca3af;font-size:12px">本邮件由灵策智能 · 赢客专家系统自动发送，请勿回复。</p>`)
	builder.WriteString(`</div></body></html>`)
	return builder.String()
}

func joinEmailLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

// sanitizeEmailText 压缩空白、脱敏金额/手机号/诊疗信息/客户全名，并限制长度。
// 用于 manager_signal.reason/action 等模型生成文本，避免邮件带出金额与全名。
func sanitizeEmailText(raw, customerName, salutation string) string {
	value := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if value == "" {
		return ""
	}
	if strings.TrimSpace(salutation) == "" {
		salutation = "客户"
	}
	value = redactCustomerName(value, customerName, salutation)
	value = emailMoneyPattern.ReplaceAllString(value, "（金额略）")
	value = emailPhonePattern.ReplaceAllString(value, "（电话略）")
	value = emailMedicalPattern.ReplaceAllString(value, "（诊疗信息略）")
	value = emailHourWindowPattern.ReplaceAllStringFunc(value, func(match string) string {
		n := "0"
		for _, r := range match {
			if r < '0' || r > '9' {
				break
			}
			n += string(r)
		}
		switch strings.TrimLeft(n, "0") {
		case "24":
			return "一天内"
		case "48":
			return "两天内"
		default:
			return "近期"
		}
	})
	runes := []rune(value)
	if len(runes) > 120 {
		value = string(runes[:120])
	}
	return value
}

// redactCustomerName 把客户全名替换成称呼，只用于已知全名的场景。
func redactCustomerName(value, customerName, salutation string) string {
	customerName = strings.TrimSpace(customerName)
	if len([]rune(customerName)) < 2 {
		return value
	}
	return strings.ReplaceAll(value, customerName, salutation)
}
