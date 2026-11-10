package patientops

// 客户总监（产品编码 kehu）的计费接入。
//
// 四个交付点与计量：
//
//   - 录音分析（客情分析）：hour = 录音时长（recordings.duration 秒 / 3600，保留 2 位），
//     时长未知则跳过；
//   - 截图解读：shot 1；
//   - 截图建议动作：record 1（每次动作生成成功计 1 条建议）；
//   - 经营方案内容：person 1（每会员一条方案）。
//
// 扣量一律发生在业务成功落库之后：ErrAlreadyDeducted 视为幂等成功，其他错误只告警、
// 不回滚业务（MVP 允许欠扣）。用户主动触发的三个入口（截图解读 / 建议动作 / 方案内容）
// 在开始生成前做 CheckQuota 预检；录音分析由 worker 异步触发，不设闸门（fail-open）。
// 计费未接入或预检自身报错时一律放行，避免计费故障拖垮业务。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

const (
	// billingProductCode 是客户总监的产品编码。
	billingProductCode = "kehu"

	billingUnitHour   = "hour"
	billingUnitShot   = "shot"
	billingUnitRecord = "record"
	billingUnitPerson = "person"

	billingReasonRecording  = "客户总监-录音分析"
	billingReasonScreenshot = "客户总监-截图解读"
	billingReasonAction     = "客户总监-截图建议动作"
	billingReasonPlan       = "客户总监-经营方案内容"

	// patientOpsQuotaExhaustedCode 是交付前闸门拒绝时返回给调用方的错误码。
	patientOpsQuotaExhaustedCode = "PATIENT_OPS_QUOTA_EXHAUSTED"
)

// ErrPatientOpsQuotaExhausted 表示预检发现客户总监额度不足，生成入口据此拒绝开始生成。
var ErrPatientOpsQuotaExhausted = errors.New("额度已用完，请及时联系客服，加购增量包")

// quotaExhaustedError 是闸门拒绝的具体错误，携带缺口明细；同时满足
// errors.Is(err, ErrPatientOpsQuotaExhausted) 与 errors.As(err, *billing.InsufficientEntitlementError)，
// 便于入口区分"额度不足"与计费自身故障。
type quotaExhaustedError struct {
	insufficient *billing.InsufficientEntitlementError
}

func (e *quotaExhaustedError) Error() string {
	if e == nil || e.insufficient == nil {
		return ErrPatientOpsQuotaExhausted.Error()
	}
	return fmt.Sprintf("%s（%s 额度不足：当前 %.2f，本次需要 %.2f）",
		ErrPatientOpsQuotaExhausted.Error(), e.insufficient.Unit, e.insufficient.Current, e.insufficient.Required)
}

func (e *quotaExhaustedError) Unwrap() []error {
	if e == nil || e.insufficient == nil {
		return []error{ErrPatientOpsQuotaExhausted}
	}
	return []error{ErrPatientOpsQuotaExhausted, e.insufficient}
}

// billingService 是本模块依赖的最小计费能力，*billing.Service 天然实现；
// 单元测试注入 fake，避免依赖真实数据库与计费事务。
type billingService interface {
	Deduct(ctx context.Context, input billing.DeductInput) (*billing.DeductResult, error)
	CheckQuota(ctx context.Context, tenantID int64, productCode string, items []billing.DeductItem) error
}

// UseBilling 给客户总监模块接入计费：下游统一由 Service 持有计费能力，
// Handler 只负责把预检拒绝映射成 HTTP 响应。nil 表示不接计费（旧部署/测试）。
func (m *Module) UseBilling(svc *billing.Service) {
	if m == nil || m.handler == nil {
		return
	}
	m.handler.service.UseBilling(svc)
}

// UseBilling 把 *billing.Service 收敛成窄接口注入 Service。
func (s *Service) UseBilling(svc *billing.Service) {
	if s == nil {
		return
	}
	s.billing = wrapBillingService(svc)
}

// wrapBillingService 避免 nil 指针被包成非空接口（typed-nil 陷阱）。
func wrapBillingService(svc *billing.Service) billingService {
	if svc == nil {
		return nil
	}
	return svc
}

// ensureQuota 是交付前只读预检：额度不足返回 ErrPatientOpsQuotaExhausted（入口拒绝生成）；
// 计费未接入或预检本身报错时放行并告警（fail-open），不能因计费故障拖垮业务。
func (s *Service) ensureQuota(ctx context.Context, tenantID int64, unit string, amount float64) error {
	if s == nil || s.billing == nil {
		return nil
	}
	err := s.billing.CheckQuota(ctx, tenantID, billingProductCode, []billing.DeductItem{{Unit: unit, Amount: amount}})
	if err == nil {
		return nil
	}
	if !errors.Is(err, billing.ErrInsufficientEntitlement) {
		slog.Warn("patient ops quota precheck failed, allowing delivery",
			"tenant_id", tenantID, "product_code", billingProductCode, "unit", unit, "error", err)
		return nil
	}
	var insufficient *billing.InsufficientEntitlementError
	if errors.As(err, &insufficient) {
		return &quotaExhaustedError{insufficient: insufficient}
	}
	return ErrPatientOpsQuotaExhausted
}

// ensurePlanContentQuota 是经营方案内容生成的交付前闸门：person 1 人。
func (s *Service) ensurePlanContentQuota(ctx context.Context, tenantID int64) error {
	return s.ensureQuota(ctx, tenantID, billingUnitPerson, 1)
}

// writeQuotaExhausted 用本模块既有的错误响应格式返回额度不足：
// 402 Payment Required + PATIENT_OPS_QUOTA_EXHAUSTED，details 带产品与缺口明细。
func writeQuotaExhausted(w http.ResponseWriter, cause error) {
	details := map[string]any{"product_code": billingProductCode}
	var insufficient *billing.InsufficientEntitlementError
	if errors.As(cause, &insufficient) {
		details["unit"] = insufficient.Unit
		details["current"] = insufficient.Current
		details["required"] = insufficient.Required
		details["shortfall"] = insufficient.Shortfall
	}
	slog.Warn("patient ops generation rejected: quota exhausted", "error", cause)
	httputil.WriteError(w, http.StatusPaymentRequired, patientOpsQuotaExhaustedCode, ErrPatientOpsQuotaExhausted.Error(), details)
}

// recordingHours 把秒换算成小时，四舍五入到 2 位小数（0.01 小时 ≈ 0.6 分钟，即分钟精度）。
func recordingHours(seconds int64) float64 {
	if seconds <= 0 {
		return 0
	}
	return math.Round(float64(seconds)/3600*100) / 100
}

// recordingSeconds 读取录音时长（recordings.duration 单位为秒）。
//
// lookupRecordingDuration 是单测注入点；生产环境走数据库。查不到时返回错误，
// 调用方按"时长未知"处理：跳过本次 hour 扣量。
func (s *Service) recordingSeconds(ctx context.Context, tenantID, recordingID int64) (int64, error) {
	if s.lookupRecordingDuration != nil {
		return s.lookupRecordingDuration(ctx, tenantID, recordingID)
	}
	if s.pool == nil {
		return 0, fmt.Errorf("database pool is not configured")
	}
	var seconds int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(duration, 0)
		FROM recordings
		WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
	`, recordingID, tenantID).Scan(&seconds); err != nil {
		return 0, fmt.Errorf("load recording duration: %w", err)
	}
	return seconds, nil
}

// recordingAnalysisDeductionInput 组装录音分析的扣量请求：hour = 录音时长（保留 2 位），
// 幂等键固定为 patientops:recording:<recordingID>:analysis。
// 时长未知（<= 0）返回 ok=false，调用方跳过扣量。
func recordingAnalysisDeductionInput(tenantID, recordingID, seconds int64) (billing.DeductInput, bool) {
	hours := recordingHours(seconds)
	if hours <= 0 {
		return billing.DeductInput{}, false
	}
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    billingProductCode,
		Items:          []billing.DeductItem{{Unit: billingUnitHour, Amount: hours}},
		IdempotencyKey: fmt.Sprintf("patientops:recording:%d:analysis", recordingID),
		Reason:         billingReasonRecording,
	}, true
}

// screenshotReviewDeductionInput 组装截图解读的扣量请求：shot 1 张，
// 幂等键 patientops:screenshot:<sourceID>:review（同一条截图重复解读只扣一次）。
func screenshotReviewDeductionInput(tenantID, sourceID int64) billing.DeductInput {
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    billingProductCode,
		Items:          []billing.DeductItem{{Unit: billingUnitShot, Amount: 1}},
		IdempotencyKey: fmt.Sprintf("patientops:screenshot:%d:review", sourceID),
		Reason:         billingReasonScreenshot,
	}
}

// screenshotActionDeductionInput 组装截图建议动作的扣量请求：record 1 条建议，
// 幂等键 patientops:screenshot:<sourceID>:action:<action>（同一截图同一动作只扣一次）。
func screenshotActionDeductionInput(tenantID, sourceID int64, action string) billing.DeductInput {
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    billingProductCode,
		Items:          []billing.DeductItem{{Unit: billingUnitRecord, Amount: 1}},
		IdempotencyKey: fmt.Sprintf("patientops:screenshot:%d:action:%s", sourceID, action),
		Reason:         billingReasonAction,
	}
}

// planMemberContentDeductionInput 组装经营方案内容的扣量请求：每会员一条方案 person 1，
// 幂等键 patientops:plan:<planID>:member:<memberID>:content。
func planMemberContentDeductionInput(tenantID, planID, memberID int64) billing.DeductInput {
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    billingProductCode,
		Items:          []billing.DeductItem{{Unit: billingUnitPerson, Amount: 1}},
		IdempotencyKey: fmt.Sprintf("patientops:plan:%d:member:%d:content", planID, memberID),
		Reason:         billingReasonPlan,
	}
}

// chargeRecordingAnalysis 在录音分析成功落库后扣 hour。
//
// 录音分析是 worker 异步交付，不设预检闸门；时长未知时跳过扣量（无法计量小时数），
// 扣量失败只告警，不回滚已经成功的分析结果。
func (s *Service) chargeRecordingAnalysis(ctx context.Context, tenantID, recordingID int64) {
	if s == nil || s.billing == nil {
		return
	}
	seconds, err := s.recordingSeconds(ctx, tenantID, recordingID)
	if err != nil {
		slog.Warn("patient ops recording deduction skipped: duration unavailable",
			"tenant_id", tenantID, "recording_id", recordingID, "error", err)
		return
	}
	input, ok := recordingAnalysisDeductionInput(tenantID, recordingID, seconds)
	if !ok {
		return
	}
	deductOrWarn(ctx, s.billing, input)
}

// chargeScreenshotReview 在截图分析成功落库后扣 shot 1 张。
func (s *Service) chargeScreenshotReview(ctx context.Context, tenantID, sourceID int64) {
	if s == nil || s.billing == nil || sourceID <= 0 {
		return
	}
	deductOrWarn(ctx, s.billing, screenshotReviewDeductionInput(tenantID, sourceID))
}

// chargeScreenshotAction 在建议动作生成成功后扣 record 1 条。
func (s *Service) chargeScreenshotAction(ctx context.Context, tenantID, sourceID int64, action string) {
	if s == nil || s.billing == nil || sourceID <= 0 {
		return
	}
	deductOrWarn(ctx, s.billing, screenshotActionDeductionInput(tenantID, sourceID, action))
}

// chargePlanMemberContent 在会员方案内容落库后扣 person 1。
func (s *Service) chargePlanMemberContent(ctx context.Context, tenantID, planID, memberID int64) {
	if s == nil || s.billing == nil || memberID <= 0 {
		return
	}
	deductOrWarn(ctx, s.billing, planMemberContentDeductionInput(tenantID, planID, memberID))
}

// chargePlanMembersContent 在计划发布/加员事务提交后逐会员扣 person。
func (s *Service) chargePlanMembersContent(ctx context.Context, tenantID, planID int64, memberIDs []int64) {
	for _, memberID := range memberIDs {
		s.chargePlanMemberContent(ctx, tenantID, planID, memberID)
	}
}

// deductOrWarn 调用计费扣量：ErrAlreadyDeducted 视为幂等成功，其余错误只告警。
func deductOrWarn(ctx context.Context, svc billingService, input billing.DeductInput) {
	if svc == nil {
		return
	}
	if _, err := svc.Deduct(ctx, input); err != nil && !errors.Is(err, billing.ErrAlreadyDeducted) {
		slog.Warn("patient ops deduction failed, business result kept",
			"tenant_id", input.TenantID,
			"product_code", input.ProductCode,
			"idempotency_key", input.IdempotencyKey,
			"error", err)
	}
}
