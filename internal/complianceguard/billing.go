package complianceguard

// 合规卫士（产品编码 hegui）的计费接入。
//
// 唯一交付点：一次沟通合规检查完成并落库（run 置 completed + 发现写入
// compliance_findings），按录音时长扣 hour（recordings.duration 秒 / 3600，
// 四舍五入到 2 位小数）；时长未知或不足 0.01 小时时跳过整笔。
//
// 扣量在 completeCommunicationRun 的事务提交成功之后执行：ErrAlreadyDeducted
// 视为幂等成功，其他错误只告警、不回滚已经落库的检查结果（MVP 允许欠扣）。
// 用户主动触发的"开始检查"入口在真正调用模型之前做 CheckQuota 预检；额度不足
// 返回 402 COMPLIANCE_QUOTA_EXHAUSTED，计费未接入或预检自身报错时放行（fail-open）。
// 列表、详情、状态轮询、报告导出、员工改字均不扣量。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

const (
	// complianceBillingProductCode 是合规卫士的产品编码。
	complianceBillingProductCode = "hegui"
	// complianceBillingUnitHour 是按录音时长计量的单位。
	complianceBillingUnitHour = "hour"
	// complianceBillingReason 是写进额度流水的原因，便于运营对账。
	complianceBillingReason = "合规卫士-沟通合规检查"
	// complianceQuotaExhaustedCode 是交付前闸门拒绝时返回给调用方的错误码。
	complianceQuotaExhaustedCode = "COMPLIANCE_QUOTA_EXHAUSTED"
)

// ErrComplianceQuotaExhausted 表示预检发现合规卫士额度不足，检查入口据此拒绝开始检查。
var ErrComplianceQuotaExhausted = errors.New("额度已用完，请及时联系客服，加购增量包")

// complianceQuotaExhaustedError 是闸门拒绝的具体错误，携带缺口明细；同时满足
// errors.Is(err, ErrComplianceQuotaExhausted) 与 errors.As(err, *billing.InsufficientEntitlementError)，
// 便于入口区分"额度不足"与计费自身故障。
type complianceQuotaExhaustedError struct {
	insufficient *billing.InsufficientEntitlementError
}

func (e *complianceQuotaExhaustedError) Error() string {
	if e == nil || e.insufficient == nil {
		return ErrComplianceQuotaExhausted.Error()
	}
	return fmt.Sprintf("%s（%s 额度不足：当前 %.2f，本次需要 %.2f）",
		ErrComplianceQuotaExhausted.Error(), e.insufficient.Unit, e.insufficient.Current, e.insufficient.Required)
}

func (e *complianceQuotaExhaustedError) Unwrap() []error {
	if e == nil || e.insufficient == nil {
		return []error{ErrComplianceQuotaExhausted}
	}
	return []error{ErrComplianceQuotaExhausted, e.insufficient}
}

// billingService 是本模块依赖的最小计费能力，*billing.Service 天然实现；
// 单元测试注入 fake，避免依赖真实数据库与计费事务。
type billingService interface {
	Deduct(ctx context.Context, input billing.DeductInput) (*billing.DeductResult, error)
	CheckQuota(ctx context.Context, tenantID int64, productCode string, items []billing.DeductItem) error
}

// UseBilling 给合规卫士模块接入计费：下游统一由 Service 持有计费能力，
// Handler 只负责把预检拒绝映射成 HTTP 响应。nil 表示不接计费（旧部署/测试）。
func (m *Module) UseBilling(svc *billing.Service) {
	if m == nil || m.handler == nil || m.handler.service == nil {
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

// recordingSeconds 读取录音时长（recordings.duration 单位为秒）。
//
// lookupRecordingDuration 是单测注入点；生产环境走数据库。查不到时返回错误，
// 调用方按"时长未知"处理：跳过本次 hour 扣量/预检。
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

// communicationCheckHours 把秒换算成小时，四舍五入到 2 位小数。
func communicationCheckHours(seconds int64) float64 {
	if seconds <= 0 {
		return 0
	}
	return math.Round(float64(seconds)/3600*100) / 100
}

// communicationCheckDeductionInput 组装沟通合规检查的扣量请求：hour = 录音时长
// （保留 2 位小数），幂等键固定为 compliance:run:<runID>:check。
// 时长未知或四舍五入后为 0 时返回 ok=false，调用方跳过整笔扣量。
func communicationCheckDeductionInput(tenantID int64, runID string, seconds int64) (billing.DeductInput, bool) {
	runID = strings.TrimSpace(runID)
	hours := communicationCheckHours(seconds)
	if runID == "" || hours <= 0 {
		return billing.DeductInput{}, false
	}
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    complianceBillingProductCode,
		Items:          []billing.DeductItem{{Unit: complianceBillingUnitHour, Amount: hours}},
		IdempotencyKey: fmt.Sprintf("compliance:run:%s:check", runID),
		Reason:         complianceBillingReason,
	}, true
}

// ensureCommunicationQuota 是"开始检查"入口的交付前只读预检：hour = 本次录音时长。
//
// 额度不足返回 ErrComplianceQuotaExhausted（入口拒绝开始检查）；时长未知时无法
// 计量，放行；计费未接入或预检本身报错时放行并告警（fail-open），不能因计费故障
// 拖垮合规检查。
func (s *Service) ensureCommunicationQuota(ctx context.Context, tenantID, recordingID int64) error {
	if s == nil || s.billing == nil {
		return nil
	}
	seconds, err := s.recordingSeconds(ctx, tenantID, recordingID)
	if err != nil {
		slog.Warn("compliance quota precheck skipped: recording duration unavailable",
			"tenant_id", tenantID, "recording_id", recordingID, "error", err)
		return nil
	}
	hours := communicationCheckHours(seconds)
	if hours <= 0 {
		slog.Warn("compliance quota precheck skipped: recording duration unknown",
			"tenant_id", tenantID, "recording_id", recordingID, "seconds", seconds)
		return nil
	}
	err = s.billing.CheckQuota(ctx, tenantID, complianceBillingProductCode,
		[]billing.DeductItem{{Unit: complianceBillingUnitHour, Amount: hours}})
	if err == nil {
		return nil
	}
	if !errors.Is(err, billing.ErrInsufficientEntitlement) {
		slog.Warn("compliance quota precheck failed, allowing check",
			"tenant_id", tenantID, "recording_id", recordingID, "error", err)
		return nil
	}
	var insufficient *billing.InsufficientEntitlementError
	if errors.As(err, &insufficient) {
		return &complianceQuotaExhaustedError{insufficient: insufficient}
	}
	return ErrComplianceQuotaExhausted
}

// releaseCommunicationRunClaim 在预检拒绝时撤销刚抢占的 processing 记录，
// 避免额度补足后重试仍命中一条永远 running 的旧记录。
// 只删除 status='processing' 且尚未产生发现的 run，不影响并发完成的结果。
func (s *Service) releaseCommunicationRunClaim(ctx context.Context, runID, executionKey string) error {
	if s == nil || s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		DELETE FROM compliance_check_runs
		WHERE id::text = $1 AND execution_key = $2 AND status = 'processing'
	`, strings.TrimSpace(runID), strings.TrimSpace(executionKey))
	if err != nil {
		return fmt.Errorf("release communication run claim: %w", err)
	}
	return nil
}

// chargeCommunicationCheck 在沟通合规检查成功落库（事务已提交）后扣 hour。
//
// 时长未知时跳过扣量（无法计量小时数）；扣量失败只告警，不回滚已经完成的检查结果。
func (s *Service) chargeCommunicationCheck(ctx context.Context, tenantID, recordingID int64, runID string) {
	if s == nil || s.billing == nil {
		return
	}
	seconds, err := s.recordingSeconds(ctx, tenantID, recordingID)
	if err != nil {
		slog.Warn("compliance deduction skipped: recording duration unavailable",
			"tenant_id", tenantID, "recording_id", recordingID, "run_id", runID, "error", err)
		return
	}
	input, ok := communicationCheckDeductionInput(tenantID, runID, seconds)
	if !ok {
		return
	}
	deductOrWarn(ctx, s.billing, input)
}

// deductOrWarn 调用计费扣量：ErrAlreadyDeducted 视为幂等成功，其余错误只告警。
func deductOrWarn(ctx context.Context, svc billingService, input billing.DeductInput) {
	if svc == nil {
		return
	}
	if _, err := svc.Deduct(ctx, input); err != nil && !errors.Is(err, billing.ErrAlreadyDeducted) {
		slog.Warn("compliance deduction failed, business result kept",
			"tenant_id", input.TenantID,
			"product_code", input.ProductCode,
			"idempotency_key", input.IdempotencyKey,
			"error", err)
	}
}

// writeComplianceQuotaExhausted 用本模块既有的错误响应格式返回额度不足：
// 402 Payment Required + COMPLIANCE_QUOTA_EXHAUSTED，details 带产品与缺口明细。
func writeComplianceQuotaExhausted(w http.ResponseWriter, cause error) {
	details := map[string]any{"product_code": complianceBillingProductCode}
	var insufficient *billing.InsufficientEntitlementError
	if errors.As(cause, &insufficient) {
		details["unit"] = insufficient.Unit
		details["current"] = insufficient.Current
		details["required"] = insufficient.Required
		details["shortfall"] = insufficient.Shortfall
	}
	slog.Warn("compliance check rejected: quota exhausted", "error", cause)
	httputil.WriteError(w, http.StatusPaymentRequired, complianceQuotaExhaustedCode, ErrComplianceQuotaExhausted.Error(), details)
}
