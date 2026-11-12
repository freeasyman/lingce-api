package emrrecord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/freeasyman/lingce-api/internal/emrquality"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// 听写病历（产品编码 tingxie）的扣量规则：
//
//   - 病历草稿生成成功：record 1 份 + hour = 该录音时长（秒/3600，四舍五入到 2 位小数）
//   - 提交时实际新跑一轮完整质控：round 1 轮
//   - 手动保存（save 级质控）、自动保存（working-draft）、复用已有质控报告、
//     医生改字/查看/下载：不扣
//
// 扣量一律发生在业务成功之后；幂等重复（ErrAlreadyDeducted）视为成功，其他失败
// 只记告警日志、不回滚业务（MVP 允许欠扣），由交付前预检闸门拦住下一次交付。
const (
	billingProductCode = "tingxie"
	billingUnitRecord  = "record"
	billingUnitHour    = "hour"
	billingUnitRound   = "round"

	billingReasonDraft   = "听写病历-病历草稿生成"
	billingReasonQuality = "听写病历-完整质控"
)

// ErrEMRQuotaExhausted 表示预检发现听写病历额度不足，生成入口据此拒绝开始生成。
// 文案与错误码统一由 billing.GateRejection 提供（客户口径）。
var ErrEMRQuotaExhausted = errors.New(billing.MessageInsufficientEntitlement)

// quotaExhaustedError 是闸门拒绝的具体错误，携带缺口明细；同时满足
// errors.Is(err, ErrEMRQuotaExhausted) 与 errors.As(err, *billing.InsufficientEntitlementError)，
// 便于入口区分"额度不足"与计费自身故障。
type quotaExhaustedError struct {
	insufficient *billing.InsufficientEntitlementError
}

func (e *quotaExhaustedError) Error() string {
	if e == nil || e.insufficient == nil {
		return ErrEMRQuotaExhausted.Error()
	}
	return fmt.Sprintf("%s（%s 额度不足：当前 %.2f，本次需要 %.2f）",
		ErrEMRQuotaExhausted.Error(), e.insufficient.Unit, e.insufficient.Current, e.insufficient.Required)
}

func (e *quotaExhaustedError) Unwrap() []error {
	if e == nil || e.insufficient == nil {
		return []error{ErrEMRQuotaExhausted}
	}
	return []error{ErrEMRQuotaExhausted, e.insufficient}
}

// billingService 是本模块依赖的最小计费能力，*billing.Service 天然实现；
// 单元测试注入 fake，避免依赖真实数据库与计费事务。
type billingService interface {
	Deduct(ctx context.Context, input billing.DeductInput) (*billing.DeductResult, error)
	CheckQuota(ctx context.Context, tenantID int64, productCode string, items []billing.DeductItem) error
}

// UseBilling 给完成病历（提交）接入计费扣量。
//
// emrrecord 没有独立的 Module 类型，Service 与 InternalHandler 各自装配，
// 因此这里提供同名的模块级注入入口；nil 表示不接计费（旧部署/测试）。
func (s *Service) UseBilling(svc *billing.Service) {
	if s == nil {
		return
	}
	s.billing = wrapBillingService(svc)
}

// UseBilling 给病历草稿生成入口接入额度预检与草稿扣量。
func (h *InternalHandler) UseBilling(svc *billing.Service) {
	if h == nil {
		return
	}
	h.billing = wrapBillingService(svc)
}

// wrapBillingService 把 *billing.Service 收敛成窄接口，并避免 nil 指针被包成非空接口。
func wrapBillingService(svc *billing.Service) billingService {
	if svc == nil {
		return nil
	}
	return svc
}

// recordingSeconds 读取录音时长（recordings.duration 单位为秒）。
//
// lookupDuration 是单测注入点；生产环境走数据库。查不到时返回错误，调用方按
// "时长未知" 处理（只扣 record）。
func (h *InternalHandler) recordingSeconds(ctx context.Context, tenantID, recordingID int64) (int64, error) {
	if h.lookupDuration != nil {
		return h.lookupDuration(ctx, tenantID, recordingID)
	}
	if h.pool == nil {
		return 0, fmt.Errorf("database pool is not configured")
	}
	var seconds int64
	if err := h.pool.QueryRow(ctx, `
		SELECT COALESCE(duration, 0)
		FROM recordings
		WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
	`, recordingID, tenantID).Scan(&seconds); err != nil {
		return 0, fmt.Errorf("load recording duration: %w", err)
	}
	return seconds, nil
}

// recordingHours 把秒换算成小时，四舍五入到 2 位小数（0.01 小时 ≈ 0.6 分钟，即分钟精度）。
func recordingHours(seconds int64) float64 {
	if seconds <= 0 {
		return 0
	}
	return math.Round(float64(seconds)/3600*100) / 100
}

// draftBillingItems 是草稿生成的扣量项：record 1 份 + 录音时长对应小时数。
//
// 时长未知或不足 0.005 小时时只扣 record：计费层拒绝 amount<=0 的计量项，
// 这里主动省略，避免把一次合法交付变成参数错误。
func draftBillingItems(seconds int64) []billing.DeductItem {
	items := []billing.DeductItem{{Unit: billingUnitRecord, Amount: 1}}
	if hours := recordingHours(seconds); hours > 0 {
		items = append(items, billing.DeductItem{Unit: billingUnitHour, Amount: hours})
	}
	return items
}

// draftDeductionInput 组装草稿生成的扣量请求，幂等键固定为
// emr:record:<recordID>:draft（同一病历重复生成不会被扣两次）。
func draftDeductionInput(tenantID int64, recordID string, seconds int64) billing.DeductInput {
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    billingProductCode,
		Items:          draftBillingItems(seconds),
		IdempotencyKey: fmt.Sprintf("emr:record:%s:draft", strings.TrimSpace(recordID)),
		Reason:         billingReasonDraft,
	}
}

// ensureDraftQuota 是病历草稿生成的交付前闸门：至少要有 record 1 份，录音时长
// 已知时同时检查 hour。
//
// 额度不足返回 ErrEMRQuotaExhausted（HTTP 层映射为 402，不开始生成）；计费层未接入
// 或预检自身报错时放行并告警，避免计费故障把病历生成整体拖垮。
func (h *InternalHandler) ensureDraftQuota(ctx context.Context, tenantID, recordingID int64) error {
	if h == nil || h.billing == nil {
		return nil
	}
	seconds, err := h.recordingSeconds(ctx, tenantID, recordingID)
	if err != nil {
		slog.Warn("emr draft quota check: recording duration unavailable, checking record only",
			"tenant_id", tenantID, "recording_id", recordingID, "error", err)
		seconds = 0
	}
	items := draftBillingItems(seconds)
	if err := h.billing.CheckQuota(ctx, tenantID, billingProductCode, items); err != nil {
		gate, ok := billing.GateRejection(err)
		if !ok {
			// 计费自身故障（连不上库等）仍 fail-open，不能拖垮病历生成。
			slog.Warn("emr draft quota check failed, allowing generation",
				"tenant_id", tenantID, "recording_id", recordingID, "error", err)
			return nil
		}
		// 额度不足沿用带缺口明细的类型；暂停/到期直接返回计费层错误，
		// 由 writeQuotaExhausted 映射成统一的客户口径。
		if gate.Insufficient != nil {
			return &quotaExhaustedError{insufficient: gate.Insufficient}
		}
		if gate.Code == billing.CodeInsufficientEntitlement {
			return ErrEMRQuotaExhausted
		}
		return err
	}
	return nil
}

// writeQuotaExhausted 用本模块既有的错误响应格式返回额度不足：
// 402 Payment Required + EMR_QUOTA_EXHAUSTED，details 带租户/录音与缺口明细。
func (h *InternalHandler) writeQuotaExhausted(w http.ResponseWriter, payload EmrWorkerRequest, cause error) {
	details := map[string]any{
		"tenant_id":    payload.TenantID,
		"recording_id": payload.RecordingID,
		"product_code": billingProductCode,
	}
	gate, ok := billing.GateRejection(cause)
	if !ok {
		// 理论上不会走到这里：调用方只在 ensureDraftQuota 拒绝时写响应。
		gate = &billing.CustomerGate{
			Status:  http.StatusPaymentRequired,
			Code:    billing.CodeInsufficientEntitlement,
			Message: billing.MessageInsufficientEntitlement,
		}
	}
	if gate.Insufficient != nil {
		details["unit"] = gate.Insufficient.Unit
		details["current"] = gate.Insufficient.Current
		details["required"] = gate.Insufficient.Required
		details["shortfall"] = gate.Insufficient.Shortfall
	}
	slog.Warn("emr draft generation rejected: quota gate",
		"tenant_id", payload.TenantID, "recording_id", payload.RecordingID,
		"code", gate.Code, "error", cause)
	httputil.WriteError(w, gate.Status, gate.Code, gate.Message, details)
}

// chargeGeneratedDraft 在病历草稿已成功落库后扣 record + hour。
//
// 扣量失败只告警，不回滚已经成功的草稿：MVP 允许欠扣，下一次交付由预检闸门拦住。
func (h *InternalHandler) chargeGeneratedDraft(ctx context.Context, payload EmrWorkerRequest, record *Record) {
	if h == nil || h.billing == nil || record == nil {
		return
	}
	seconds, err := h.recordingSeconds(ctx, payload.TenantID, payload.RecordingID)
	if err != nil {
		slog.Warn("emr draft deduction: recording duration unavailable, deducting record only",
			"tenant_id", payload.TenantID, "recording_id", payload.RecordingID, "error", err)
		seconds = 0
	}
	deductOrWarn(ctx, h.billing, draftDeductionInput(payload.TenantID, record.ID, seconds))
}

// newQualityRoundToken 是 report 落库前（拿不到 RunID）的轮次兜底标识：recordID:时间戳。
//
// 每次"实际新跑一轮"只生成一次并沿用到扣量，保证同一轮只有一个幂等键。
func newQualityRoundToken(recordID string) string {
	return fmt.Sprintf("%s:%d", strings.TrimSpace(recordID), time.Now().UnixNano())
}

// qualityRoundDeductionInput 组装完整质控的扣量请求：
// 幂等键 emr:record:<recordID>:quality:<RunID>；report 没有 RunID 时退化为
// recordID:时间戳，仍然满足"同一轮只扣一次"。
func qualityRoundDeductionInput(tenantID int64, recordID string, report *emrquality.Report, roundToken string) billing.DeductInput {
	runID := ""
	if report != nil {
		runID = strings.TrimSpace(report.RunID)
	}
	if runID == "" {
		runID = strings.TrimSpace(roundToken)
	}
	if runID == "" {
		runID = newQualityRoundToken(recordID)
	}
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    billingProductCode,
		Items:          []billing.DeductItem{{Unit: billingUnitRound, Amount: 1}},
		IdempotencyKey: fmt.Sprintf("emr:record:%s:quality:%s", strings.TrimSpace(recordID), runID),
		Reason:         billingReasonQuality,
	}
}

// ensureQualityRoundQuota 是提交病历（完成级质控）的交付前预检：round 1 轮。
//
// 只有真正要新跑一轮质控时才调用（复用 QualityRunID 的提交不调用）。额度不足 /
// 服务暂停 / 服务到期都在这里阻断，避免额度已经耗尽还先调大模型再扣量失败。
func (s *Service) ensureQualityRoundQuota(ctx context.Context, tenantID int64) error {
	if s == nil || s.billing == nil {
		return nil
	}
	err := s.billing.CheckQuota(ctx, tenantID, billingProductCode,
		[]billing.DeductItem{{Unit: billingUnitRound, Amount: 1}})
	if err == nil {
		return nil
	}
	gate, ok := billing.GateRejection(err)
	if !ok {
		// 计费自身故障 fail-open，不能因为计费不可用阻断医生提交病历。
		slog.Warn("emr quality quota check failed, allowing submit",
			"tenant_id", tenantID, "error", err)
		return nil
	}
	if gate.Insufficient != nil {
		return &quotaExhaustedError{insufficient: gate.Insufficient}
	}
	if gate.Code == billing.CodeInsufficientEntitlement {
		return fmt.Errorf("%w: %w", ErrEMRQuotaExhausted, billing.ErrInsufficientEntitlement)
	}
	return err
}

// chargeQualityRound 在提交病历实际新跑一轮完整质控后扣 round 1 轮。
//
// reused=true 表示提交复用了已有质控报告（ReusableReport/QualityRunID），
// 本轮没有实际执行质控，不扣量；report 带 FailureReason 表示大模型质检未完成
// （配置缺失/返回不完整），也不算交付了一轮完整质控，同样不扣。
func (s *Service) chargeQualityRound(ctx context.Context, tenantID int64, recordID string, report *emrquality.Report, roundToken string, reused bool) {
	if s == nil || s.billing == nil || reused {
		return
	}
	if report == nil || strings.TrimSpace(report.FailureReason) != "" {
		return
	}
	deductOrWarn(ctx, s.billing, qualityRoundDeductionInput(tenantID, recordID, report, roundToken))
}

// deductOrWarn 调用计费扣量：ErrAlreadyDeducted 视为幂等成功，其余错误只告警。
func deductOrWarn(ctx context.Context, svc billingService, input billing.DeductInput) {
	if svc == nil {
		return
	}
	if _, err := svc.Deduct(ctx, input); err != nil && !errors.Is(err, billing.ErrAlreadyDeducted) {
		slog.Warn("emr deduction failed, business result kept",
			"tenant_id", input.TenantID,
			"product_code", input.ProductCode,
			"idempotency_key", input.IdempotencyKey,
			"error", err)
	}
}
