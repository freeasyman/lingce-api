package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// ExpireTaskInterval 是每日到期处理的默认间隔（一天一次）。
const ExpireTaskInterval = 24 * time.Hour

// expireOperator 是自动到期清零写流水时的操作人。
const expireOperator = "系统"

// ExpiryLedgerEntry 是到期处理要写的一条清零流水。
type ExpiryLedgerEntry struct {
	ProductCode string
	Unit        string
	Amount      Decimal
}

// planExpiryLedger 把到期行的旧余额转成流水（纯函数，可单测）：
// 每个额度行一条 usage/expired，金额 = -旧余额，reason = "服务期到期清零"。
//
// 余额为 0 的行也写一条金额为 0 的流水：到期清的是"整行状态"，
// 逐行留痕才能对账时确认每一行都被处理过（余额为负的超扣行同样按 -旧余额 冲销）。
func planExpiryLedger(rows []Entitlement) ([]ExpiryLedgerEntry, error) {
	sorted := append([]Entitlement(nil), rows...)
	sort.SliceStable(sorted, func(left, right int) bool {
		return entitlementSortKey(sorted[left]) < entitlementSortKey(sorted[right])
	})

	entries := make([]ExpiryLedgerEntry, 0, len(sorted))
	for _, row := range sorted {
		cents, err := parseEntitlementCents("balance", row.Balance)
		if err != nil {
			return nil, fmt.Errorf("expire row %s/%s: %w", row.ProductCode, row.Unit, err)
		}
		entries = append(entries, ExpiryLedgerEntry{
			ProductCode: row.ProductCode,
			Unit:        row.Unit,
			Amount:      Decimal(centsToDecimal(-cents)),
		})
	}
	return entries, nil
}

// ExpireDueSubscriptions 执行一次到期处理（E3），返回处理的租户数量。
//
// 选人条件完全在 SQL 侧：tenant_subscriptions.expired_on < today AND status='active'。
// 续费过的租户 expired_on 已被延长，不会被选中，因此不会被误清。
// 每个租户一个独立事务（见 Store.ExpireTenant）：要么整户清干净，要么不动。
func (s *Service) ExpireDueSubscriptions(ctx context.Context) (int, error) {
	today := formatServiceDate(dateOnly(s.clock()))
	tenants, err := s.repo.ListExpiringTenantIDs(ctx, today)
	if err != nil {
		return 0, fmt.Errorf("list expiring tenants: %w", err)
	}

	processed := 0
	clearedRows := 0
	var errs []error
	for _, tenantID := range tenants {
		rows, err := s.repo.ExpireTenant(ctx, tenantID, today)
		if err != nil {
			slog.Error("billing expire tenant failed", "tenant_id", tenantID, "error", err)
			errs = append(errs, fmt.Errorf("expire tenant %d: %w", tenantID, err))
			continue
		}
		processed++
		clearedRows += rows
		slog.Info("billing tenant expired", "tenant_id", tenantID, "cleared_rows", rows)
	}
	slog.Info("billing expiry task finished",
		"today", today, "matched_tenants", len(tenants),
		"processed_tenants", processed, "cleared_rows", clearedRows)

	return processed, errors.Join(errs...)
}

// StartExpiryWorker 启动每日到期处理：启动时先跑一次，之后每 interval 跑一次。
//
// 仓库没有统一调度器，这里用 goroutine + time.Ticker 的最小实现；ctx 取消后退出。
func (s *Service) StartExpiryWorker(ctx context.Context, interval time.Duration) {
	if s == nil {
		return
	}
	if interval <= 0 {
		interval = ExpireTaskInterval
	}
	// 启动先跑一次：避免服务刚好在跨天时启动、第一笔到期要等满 24 小时。
	s.runExpiryTask(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runExpiryTask(ctx)
		}
	}
}

// runExpiryTask 是一次定时执行的包装：错误只记日志，不打断 ticker 循环。
func (s *Service) runExpiryTask(ctx context.Context) {
	processed, err := s.ExpireDueSubscriptions(ctx)
	if err != nil {
		slog.Error("billing expiry task failed", "processed_tenants", processed, "error", err)
	}
}
