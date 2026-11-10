package billing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// 开通模式：grant（默认，中途加购/后买产品只发额度不延期）与 renew（续费：延期 + 结转一次）。
const (
	ActivateModeGrant = "grant"
	ActivateModeRenew = "renew"

	// rolloverWindowDays 是续费结转窗口：脱期（expired_on 早于今天）超过 30 天视为新购，不再结转。
	rolloverWindowDays = 30

	// ledgerSourceExpired 是"到期/结转清零"流水的 source（与迁移的 CHECK 约束一致）。
	ledgerSourceExpired = "expired"

	// 续费与到期处理写流水时的固定文案，集中在这里便于对账时按文案检索。
	reasonRollover            = "上期结转"
	reasonRolloverExpired     = "上期结转额度到期清零（结转只一次）"
	reasonRenewLateExpired    = "脱期超过30天，上期余额清零"
	reasonTrialExpired        = "试用额度到期清零"
	reasonSubscriptionExpired = "服务期到期清零"
)

var (
	// ErrNoSubscription 表示租户还没有 tenant_subscriptions 记录，续费前必须先开通。
	// handler 映射成 409 NO_SUBSCRIPTION。
	ErrNoSubscription = errors.New("billing: tenant has no subscription")
)

// RenewOrderInput 是续费事务（mode=renew）的落库入参。
//
// Today 由 service 的时钟给出：续费窗口、服务期顺延都以"服务端今天"为准，
// 交给 store 传入而不是用数据库 CURRENT_DATE，方便单测固定时间、也避免时区歧义。
type RenewOrderInput struct {
	OrderID         int64
	ActorID         int64
	Operator        string
	AllowedStatuses []string
	Grants          []GrantLine
	LedgerSource    string
	Reason          string
	Today           time.Time
}

// RenewalUpdate 是续费后某一行 product_entitlements 的新状态。
//
// Existing=false 表示该 (产品, 单位, 档位, 额度类型) 之前没有额度行，需要新插一行；
// store 统一用 INSERT ... ON CONFLICT DO UPDATE 写入，因此两种情况都能覆盖。
type RenewalUpdate struct {
	ProductCode string
	Unit        string
	Edition     string
	QuotaType   string
	Granted     Decimal
	Used        Decimal
	RolloverBal Decimal
	Balance     Decimal
	PeriodEnd   string
	Status      string
	Existing    bool
}

// RenewalLedgerEntry 是一条续费产生的流水；tenant_id / linked_order_id / operator
// 由 store 在写库时补齐，纯公式只关心产品、单位、类型、金额、来源与原因。
type RenewalLedgerEntry struct {
	ProductCode string
	Unit        string
	Type        string
	Amount      Decimal
	Source      string
	Reason      string
}

// RenewalPlan 是续费事务要落库的全部内容：新服务期 + 逐行新额度 + 流水。
type RenewalPlan struct {
	Period ServicePeriod
	// Updates 覆盖租户现有全部额度行（含 trial 清零行）与订单新增的行。
	Updates []RenewalUpdate
	Ledger  []RenewalLedgerEntry
	// RolloverAllowed=false 表示脱期超过 30 天，按新购处理，上期余额全部清零。
	RolloverAllowed bool
}

// grantKey 是"本单某个 unit 的授予量"与"某行额度"共用的键。
type grantKey struct {
	productCode string
	unit        string
	edition     string
	quotaType   string
}

// planRenewal 是续费结转的纯函数实现（不碰数据库，可完整单测）。
//
// 服务期（设计依据 2.4 与 E 阶段任务书）：
//   - expired_on >= today：started_on 不变，expired_on += 12 个月 - 1 天；
//   - expired_on <  today：视为脱期续费，started_on = today，expired_on = today + 12 个月 - 1 天。
//
// 逐额度行结转（official，这是"只结转一次"的核心）：
//
//	old_balance  = balance
//	old_rollover = rollover_bal
//	new_rollover = max(old_balance - old_rollover, 0)   // 只结转"本期自己没用完的"，上期结转来的清零
//	granted      = 本单该 unit 的新授予量（没有则为 0）
//	used         = 0
//	balance      = new_rollover + granted
//	rollover_bal = new_rollover
//	period_end   = 新 expired_on
//	status       = active（有余额时）
//
// 因此第二次续费时 old_rollover = new_rollover（=上期自己没用完的），
// old_balance - old_rollover 只会再算出"上一期自己新增且没用完"的部分，老结转不会滚第二次。
//
// trial 行不结转：直接清零（granted/used/rollover_bal/balance = 0，status=cleared），
// 写一条 usage/expired 流水记录被清掉的余额。
func planRenewal(current ServicePeriod, today time.Time, rows []Entitlement, grants []GrantLine, grantSource, grantReason string) (RenewalPlan, error) {
	// started_on 允许为 NULL（历史订阅行可能只回填了 expired_on）：此时续费只按 expired_on
	// 判断是否需要重算起点，不因为缺 started_on 而拒绝整笔续费。
	startedOn, startedErr := parseServiceDate("started_on", current.StartedOn)
	hasStartedOn := startedErr == nil
	if startedErr != nil && !serviceDateMissing(current.StartedOn) {
		return RenewalPlan{}, startedErr
	}
	expiredOn, err := parseServiceDate("expired_on", current.ExpiredOn)
	if err != nil {
		return RenewalPlan{}, err
	}

	today = dateOnly(today)
	// 脱期超过 30 天才关闭结转；刚好 30 天仍算窗口内（"超过"是严格大于）。
	rolloverAllowed := !expiredOn.Before(today.AddDate(0, 0, -rolloverWindowDays))

	var newStartedOn *string
	if hasStartedOn {
		newStartedOn = stringPointer(formatServiceDate(startedOn))
	}
	periodEnd := expiredOn
	if expiredOn.Before(today) {
		// 已过期：从今天重新起算一个完整服务年。
		newStartedOn = stringPointer(formatServiceDate(today))
		periodEnd = today
	}
	newExpiredOn := addServiceYear(periodEnd)
	newPeriodEnd := formatServiceDate(newExpiredOn)
	period := ServicePeriod{
		StartedOn: newStartedOn,
		ExpiredOn: stringPointer(newPeriodEnd),
	}

	// 本单授予量按行键聚合（同一行多笔授予相加）；用完即删，剩下的就是要新建的行。
	remainingGrants := make(map[grantKey]int64, len(grants))
	for _, grant := range grants {
		cents, err := parseEntitlementCents("grant amount", grant.Amount)
		if err != nil {
			return RenewalPlan{}, fmt.Errorf("renew grant %s/%s: %w", grant.ProductCode, grant.Unit, err)
		}
		if cents < 0 {
			return RenewalPlan{}, fmt.Errorf("renew grant %s/%s must not be negative", grant.ProductCode, grant.Unit)
		}
		if cents == 0 {
			continue
		}
		key := grantKey{
			productCode: normalizeEnum(grant.ProductCode),
			unit:        normalizeEnum(grant.Unit),
			edition:     normalizeEnum(grant.Edition),
			quotaType:   normalizeEnum(grant.QuotaType),
		}
		if remainingGrants[key] > maxQuotaAmountCents-cents {
			return RenewalPlan{}, fmt.Errorf("renew grant %s/%s exceeds the maximum quota amount", grant.ProductCode, grant.Unit)
		}
		remainingGrants[key] += cents
	}

	sortedRows := append([]Entitlement(nil), rows...)
	sort.SliceStable(sortedRows, func(left, right int) bool {
		return entitlementSortKey(sortedRows[left]) < entitlementSortKey(sortedRows[right])
	})

	plan := RenewalPlan{
		Period:          period,
		Updates:         make([]RenewalUpdate, 0, len(rows)+len(grants)),
		Ledger:          make([]RenewalLedgerEntry, 0, len(rows)*3+len(grants)),
		RolloverAllowed: rolloverAllowed,
	}
	for _, row := range sortedRows {
		key := grantKey{
			productCode: normalizeEnum(row.ProductCode),
			unit:        normalizeEnum(row.Unit),
			edition:     normalizeEnum(row.Edition),
			quotaType:   normalizeEnum(row.QuotaType),
		}
		grantCents := remainingGrants[key]
		delete(remainingGrants, key)

		oldBalanceCents, err := parseEntitlementCents("balance", row.Balance)
		if err != nil {
			return RenewalPlan{}, fmt.Errorf("renew row %s/%s: %w", row.ProductCode, row.Unit, err)
		}
		oldRolloverCents, err := parseEntitlementCents("rollover_bal", row.RolloverBal)
		if err != nil {
			return RenewalPlan{}, fmt.Errorf("renew row %s/%s: %w", row.ProductCode, row.Unit, err)
		}

		var (
			newRolloverCents int64
			// expireCents 是这一行被清零的余额（写 usage/expired 流水的金额来源）。
			expireCents  int64
			expireReason string
			newStatus    string
		)
		if normalizeEnum(row.QuotaType) == QuotaTypeTrial {
			// 试用额度到期直接清零，不冻结、不结转；如果本单又发了试用额度，
			// 那就是"旧试用清零 + 新试用下发"，这里按新授予量重新计算余额。
			expireCents = max(oldBalanceCents, 0)
			if expireCents > 0 {
				expireReason = reasonTrialExpired
			}
			newRolloverCents = 0
			if grantCents > 0 {
				newStatus = entitlementStatusActive
			} else {
				newStatus = entitlementStatusCleared
			}
		} else {
			if rolloverAllowed && oldBalanceCents > oldRolloverCents {
				newRolloverCents = oldBalanceCents - oldRolloverCents
			}
			// 被丢弃的余额 = 旧余额 - 新结转：正常情况等于旧 rollover_bal（老结转清零），
			// 旧余额扣成负数时取 0，避免写出一条量级对不上的流水。
			if oldBalanceCents-newRolloverCents > 0 {
				expireCents = oldBalanceCents - newRolloverCents
				expireReason = reasonRolloverExpired
				if !rolloverAllowed {
					expireReason = reasonRenewLateExpired
				}
			}
			balance := newRolloverCents + grantCents
			switch {
			case balance > 0:
				newStatus = entitlementStatusActive
			case entitlementRenewableStatus(row.Status):
				// 这期没余额（没续购这个产品/单位）→ paused，扣量与预检都不再消费它。
				newStatus = entitlementStatusPaused
			default:
				// expired / cleared 属于人工或到期状态，续费不把它们改成 paused，保持原状。
				newStatus = row.Status
			}
		}
		if expireReason != "" && expireCents > 0 {
			plan.Ledger = append(plan.Ledger, RenewalLedgerEntry{
				ProductCode: row.ProductCode,
				Unit:        row.Unit,
				Type:        LedgerTypeUsage,
				Amount:      Decimal(centsToDecimal(-expireCents)),
				Source:      ledgerSourceExpired,
				Reason:      expireReason,
			})
		}
		if newRolloverCents > 0 {
			plan.Ledger = append(plan.Ledger, RenewalLedgerEntry{
				ProductCode: row.ProductCode,
				Unit:        row.Unit,
				Type:        LedgerTypeGrant,
				Amount:      Decimal(centsToDecimal(newRolloverCents)),
				Source:      ledgerSourceRollover,
				Reason:      reasonRollover,
			})
		}
		if grantCents > 0 {
			plan.Ledger = append(plan.Ledger, RenewalLedgerEntry{
				ProductCode: row.ProductCode,
				Unit:        row.Unit,
				Type:        LedgerTypeGrant,
				Amount:      Decimal(centsToDecimal(grantCents)),
				Source:      grantSource,
				Reason:      grantReason,
			})
		}
		plan.Updates = append(plan.Updates, RenewalUpdate{
			ProductCode: row.ProductCode,
			Unit:        row.Unit,
			Edition:     row.Edition,
			QuotaType:   row.QuotaType,
			Granted:     Decimal(centsToDecimal(grantCents)),
			Used:        Decimal(centsToDecimal(0)),
			RolloverBal: Decimal(centsToDecimal(newRolloverCents)),
			Balance:     Decimal(centsToDecimal(newRolloverCents + grantCents)),
			PeriodEnd:   newPeriodEnd,
			Status:      newStatus,
			Existing:    true,
		})
	}

	// 订单里新增的行（租户之前没有这个产品/单位/档位/额度类型）。
	remainingKeys := make([]grantKey, 0, len(remainingGrants))
	for key := range remainingGrants {
		remainingKeys = append(remainingKeys, key)
	}
	sort.SliceStable(remainingKeys, func(left, right int) bool {
		return grantKeyString(remainingKeys[left]) < grantKeyString(remainingKeys[right])
	})
	for _, key := range remainingKeys {
		grantCents := remainingGrants[key]
		if grantCents <= 0 {
			continue
		}
		plan.Ledger = append(plan.Ledger, RenewalLedgerEntry{
			ProductCode: key.productCode,
			Unit:        key.unit,
			Type:        LedgerTypeGrant,
			Amount:      Decimal(centsToDecimal(grantCents)),
			Source:      grantSource,
			Reason:      grantReason,
		})
		plan.Updates = append(plan.Updates, RenewalUpdate{
			ProductCode: key.productCode,
			Unit:        key.unit,
			Edition:     key.edition,
			QuotaType:   key.quotaType,
			Granted:     Decimal(centsToDecimal(grantCents)),
			Used:        Decimal(centsToDecimal(0)),
			RolloverBal: Decimal(centsToDecimal(0)),
			Balance:     Decimal(centsToDecimal(grantCents)),
			PeriodEnd:   newPeriodEnd,
			Status:      entitlementStatusActive,
			Existing:    false,
		})
	}
	return plan, nil
}

// entitlementRenewableStatus 判断续费时"没余额但不该改动"的例外：
// active/paused（以及历史空值）可以按余额改状态；expired/cleared 保持原状。
func entitlementRenewableStatus(status string) bool {
	switch normalizeEnum(status) {
	case "", entitlementStatusActive, entitlementStatusPaused:
		return true
	default:
		return false
	}
}

// entitlementSortKey 复刻 store 的 ORDER BY product_code, unit, edition, quota_type，
// 保证纯函数产出的 update/ledger 顺序与落库顺序一致，便于测试断言。
func entitlementSortKey(row Entitlement) string {
	return normalizeEnum(row.ProductCode) + "\x00" +
		normalizeEnum(row.Unit) + "\x00" +
		normalizeEnum(row.Edition) + "\x00" +
		normalizeEnum(row.QuotaType)
}

func grantKeyString(key grantKey) string {
	return key.productCode + "\x00" + key.unit + "\x00" + key.edition + "\x00" + key.quotaType
}

// dateOnly 取 t 所在时区的日历日期（UTC 零点），避免把时分秒带进日期比较。
func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// serviceDateMissing 判断服务期日期是否为空（NULL 或空串）。
func serviceDateMissing(raw *string) bool {
	return raw == nil || strings.TrimSpace(*raw) == ""
}

// parseServiceDate 解析服务期日期（YYYY-MM-DD）。缺列/空值/非法格式都返回错误。
func parseServiceDate(field string, raw *string) (time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return time.Time{}, fmt.Errorf("service period %s is empty", field)
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("service period %s %q is not a date", field, *raw)
	}
	return parsed, nil
}

// formatServiceDate 把日期格式化成接口与 SQL 共用的 YYYY-MM-DD 文本。
func formatServiceDate(t time.Time) string {
	return t.Format("2006-01-02")
}

// addServiceYear 返回 date + 12 个月 - 1 天。
//
// 先按月加并把日夹到目标月份的最后一天（与 PostgreSQL date + interval 的行为一致，
// 例如 2024-02-29 + 12 个月 = 2025-02-28），再减 1 天。
func addServiceYear(date time.Time) time.Time {
	targetMonth := time.Date(date.Year(), date.Month()+12, 1, 0, 0, 0, 0, time.UTC)
	lastDay := time.Date(targetMonth.Year(), targetMonth.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	day := date.Day()
	if day > lastDay {
		day = lastDay
	}
	anniversary := time.Date(targetMonth.Year(), targetMonth.Month(), day, 0, 0, 0, 0, time.UTC)
	return anniversary.AddDate(0, 0, -1)
}

// stringPointer 返回字符串指针（ServicePeriod 的两个字段是可空文本）。
func stringPointer(value string) *string { return &value }

// normalizeActivateMode 归一化开通模式：空值默认 grant，其余必须是 grant/renew。
func normalizeActivateMode(mode string) (string, error) {
	normalized := normalizeEnum(mode)
	if normalized == "" {
		return ActivateModeGrant, nil
	}
	if normalized != ActivateModeGrant && normalized != ActivateModeRenew {
		return "", invalidf("mode must be one of %s/%s", ActivateModeGrant, ActivateModeRenew)
	}
	return normalized, nil
}

// ActivateOrder 开通订单（默认 mode=grant：中途加购/后买产品只发额度、不延服务期）。
func (s *Service) ActivateOrder(ctx context.Context, orderID, adminID int64) (*ActivationResult, error) {
	return s.ActivateOrderWithMode(ctx, orderID, adminID, ActivateModeGrant)
}

// ActivateOrderWithMode 开通订单，mode 决定是否走续费结转：
//
//   - grant（默认）：建/读统一服务期，按 items 累加额度，不延期、不结转；
//   - renew：延长服务期 + 逐行结转一次 + 发本单新额度，trial 行直接清零。
//
// 两种模式共用同一套前置校验（订单存在、未开通、状态允许），
// "只能开通一次"仍由 store 事务里的抢行 UPDATE 保证。
func (s *Service) ActivateOrderWithMode(ctx context.Context, orderID, adminID int64, mode string) (*ActivationResult, error) {
	if orderID <= 0 {
		return nil, invalidf("order id is required")
	}
	if adminID <= 0 {
		return nil, invalidf("activated_by is required")
	}
	mode, err := normalizeActivateMode(mode)
	if err != nil {
		return nil, err
	}

	order, err := s.repo.GetOrder(ctx, orderID)
	if err != nil {
		return nil, mapOrderError(err, "get order")
	}
	if order.ActivatedAt != nil {
		return nil, ErrOrderAlreadyActivated
	}
	allowedStatuses := allowedActivationStatuses(order.OrderType)
	if !containsString(allowedStatuses, order.Status) {
		return nil, ErrOrderStatusInvalid
	}
	grants, err := order.GrantLines()
	if err != nil {
		return nil, err
	}
	source, reason := activationLedgerMeta(order)
	operator := s.resolveOperator(ctx, adminID)

	var (
		activated Order
		period    ServicePeriod
	)
	if mode == ActivateModeRenew {
		activated, period, err = s.repo.RenewOrder(ctx, RenewOrderInput{
			OrderID:         order.ID,
			ActorID:         adminID,
			Operator:        operator,
			AllowedStatuses: allowedStatuses,
			Grants:          grants,
			LedgerSource:    source,
			Reason:          reason,
			Today:           dateOnly(s.clock()),
		})
	} else {
		activated, period, err = s.repo.ActivateOrder(ctx, ActivateOrderInput{
			OrderID:         order.ID,
			ActorID:         adminID,
			Operator:        operator,
			AllowedStatuses: allowedStatuses,
			Grants:          grants,
			LedgerSource:    source,
			Reason:          reason,
		})
	}
	if err != nil {
		return nil, mapOrderError(err, "activate order")
	}

	entitlements, err := s.ListEntitlements(ctx, activated.TenantID, "", false)
	if err != nil {
		return nil, err
	}
	return &ActivationResult{
		Order:         activated,
		ServicePeriod: &period,
		Entitlements:  entitlements,
	}, nil
}
