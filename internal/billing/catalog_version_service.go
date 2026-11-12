package billing

// 改价服务：在现有价目基础上追加一个新版本，不动历史版本。
//
// 生效规则：
//   - price_version 由 effectiveFrom 派生（同一天多次改价加序号），effective_from 落库；
//   - 价目选择只看「已到生效日」的版本，未来版本提前录入也不会提前生效；
//   - 老订单的金额在录单时已写入 billing_orders，改价不会回写，历史订单不受影响。

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// CreateCatalogVersionWithApproval 新增价目版本（改价）。
func (s *InstitutionService) CreateCatalogVersionWithApproval(
	ctx context.Context,
	req CreateCatalogVersionV2Request,
	adminID int64,
) (*CatalogItem, error) {
	normalized, err := req.normalize()
	if err != nil {
		return nil, err
	}

	items, err := s.service.repo.ListCatalog(ctx, CatalogFilter{ProductCode: normalized.ProductCode, Edition: EditionStandard})
	if err != nil {
		return nil, fmt.Errorf("load catalog for price change: %w", err)
	}
	// 以「当前已生效」的版本为基线复制额度/增量规格；已排期但未生效的版本不能被提前引用。
	versions := effectiveCatalogVersions(items, s.today())
	if len(versions) == 0 {
		return nil, invalidf("产品 %s 不在价目表中", normalized.ProductCode)
	}
	current := versions[0]

	quotas, err := parseQuotaMap(current.Quotas)
	if err != nil {
		return nil, fmt.Errorf("catalog %s quotas: %w", current.ProductCode, err)
	}
	increments, err := parseQuotaMap(current.Increments)
	if err != nil {
		return nil, fmt.Errorf("catalog %s increments: %w", current.ProductCode, err)
	}
	quotaInputs := make(QuantityInputs, len(quotas))
	for _, pair := range quotas {
		quotaInputs[pair.Unit] = pair.Amount
	}
	incrementInputs := make(QuantityInputs, len(increments)+1)
	for _, pair := range increments {
		incrementInputs[pair.Unit] = pair.Amount
	}
	incrementInputs[normalized.Unit] = normalized.IncrementSize
	quotaJSON, err := normalizeQuantities("quotas", quotaInputs)
	if err != nil {
		return nil, err
	}
	incrementJSON, err := normalizeQuantities("increments", incrementInputs)
	if err != nil {
		return nil, err
	}

	productName := current.ProductName
	if strings.TrimSpace(productName) == "" {
		productName = normalized.ProductCode
	}
	effective := normalized.EffectiveFrom
	baseVersion := effective.Format("20060102")
	for attempt := 0; attempt < 5; attempt++ {
		priceVersion := baseVersion
		if attempt > 0 {
			priceVersion = fmt.Sprintf("%s-%d", baseVersion, attempt+1)
		}
		item, err := s.service.repo.InsertCatalogVersion(ctx, CatalogVersionInput{
			ProductCode:    normalized.ProductCode,
			ProductName:    productName,
			Edition:        EditionStandard,
			PriceVersion:   priceVersion,
			AnnualPrice:    normalized.AnnualPrice,
			Quotas:         quotaJSON,
			IncrementPrice: normalized.IncrementPrice,
			Increments:     incrementJSON,
			EffectiveFrom:  &effective,
			Approver:       normalized.Approver,
			Reason:         normalized.Reason,
		})
		if err == ErrCatalogVersionExists {
			continue
		}
		if err != nil {
			return nil, err
		}
		s.recordCatalogVersionLog(ctx, adminID, current, item, normalized)
		return &item, nil
	}
	return nil, &ConflictError{Sentinel: ErrCatalogVersionExists, Message: "同一天该产品的价目版本已存在，请稍后重试"}
}

// recordCatalogVersionLog 写改价审计日志（平台级操作，tenant_id 为空）。
func (s *InstitutionService) recordCatalogVersionLog(
	ctx context.Context,
	adminID int64,
	before CatalogItem,
	after CatalogItem,
	normalized normalizedCatalogVersion,
) {
	actorName, _ := s.store.GetOperationsAdminName(ctx, adminID)
	err := s.store.InsertSystemActionLog(ctx, institutionActionLog{
		TenantID:   0,
		ActorID:    adminID,
		ActorName:  actorName,
		LogType:    "billing",
		ActionCode: "billing.catalog.version",
		ActionName: "新增价目版本",
		ObjectType: "product_catalog",
		ObjectID:   normalized.ProductCode,
		ObjectName: after.ProductName,
		Before: map[string]any{
			"priceVersion":   before.PriceVersion,
			"annualPrice":    before.AnnualPrice.String(),
			"incrementPrice": before.IncrementPrice.String(),
		},
		After: map[string]any{
			"priceVersion":   after.PriceVersion,
			"annualPrice":    after.AnnualPrice.String(),
			"incrementPrice": after.IncrementPrice.String(),
			"effectiveFrom":  after.EffectiveFrom,
			"unit":           normalized.Unit,
			"incrementSize":  normalized.IncrementSize.String(),
			"approver":       normalized.Approver,
			"reason":         normalized.Reason,
		},
	})
	if err != nil {
		// 审计失败不回滚已生效的价目版本，但必须留痕方便排查。
		slog.Warn("record catalog version audit log failed",
			"product_code", normalized.ProductCode, "actor_id", adminID, "error", err)
	}
}

// CreateBillingCatalogVersion 是 POST /ops/billing/catalog/versions 的 HTTP handler。
func (h *Handler) CreateBillingCatalogVersion(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req CreateCatalogVersionV2Request
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.CreateCatalogVersionWithApproval(r.Context(), req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}
