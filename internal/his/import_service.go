package his

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// ImportRowError 是逐行导入失败明细（行号 1-based，可直接对照 Excel）。
type ImportRowError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

// ImportPurchaseOrderSummary 是采购导入生成的草稿单摘要。
type ImportPurchaseOrderSummary struct {
	ID          int64   `json:"id"`
	OrderNo     string  `json:"order_no"`
	Supplier    string  `json:"supplier"`
	ItemCount   int     `json:"item_count"`
	TotalAmount float64 `json:"total_amount"`
}

// ImportCommitResult 是提交导入的结果。
type ImportCommitResult struct {
	Success int                          `json:"success"`
	Failed  int                          `json:"failed"`
	Errors  []ImportRowError             `json:"errors"`
	Orders  []ImportPurchaseOrderSummary `json:"orders,omitempty"`
}

func (result *ImportCommitResult) addError(row int, err error) {
	result.Failed++
	if len(result.Errors) < importMaxRows {
		result.Errors = append(result.Errors, ImportRowError{Row: row, Message: err.Error()})
	}
}

// ValidateImportMapping 校验字段映射：字段必须属于该目标，必填字段必须有列。
func ValidateImportMapping(target string, mapping map[string]int) error {
	spec, ok := importSpec(target)
	if !ok {
		return importErrorf("不支持的导入目标：%s", target)
	}
	known := make(map[string]struct{}, len(spec.Fields))
	for _, field := range spec.Fields {
		known[field.Key] = struct{}{}
	}
	usedColumns := make(map[int]string, len(mapping))
	for key, index := range mapping {
		if _, ok := known[key]; !ok {
			return importErrorf("未知导入字段：%s", key)
		}
		if index < 0 {
			return importErrorf("字段「%s」的列索引不合法", key)
		}
		if another, used := usedColumns[index]; used {
			return importErrorf("字段「%s」与「%s」不能选择同一列", another, key)
		}
		usedColumns[index] = key
	}
	for _, field := range spec.Fields {
		if !field.Required {
			continue
		}
		if _, ok := mapping[field.Key]; !ok {
			return importErrorf("请为必填字段「%s」选择一列", field.Label)
		}
	}
	return nil
}

// CommitImport 逐行导入并返回成功/失败统计；单行失败不影响其他行。
func (s *Service) CommitImport(ctx context.Context, tenantID, employeeID int64, target string, mapping map[string]int, rows []importRow) (ImportCommitResult, error) {
	spec, ok := importSpec(target)
	if !ok {
		return ImportCommitResult{}, importErrorf("不支持的导入目标：%s", target)
	}
	if err := ValidateImportMapping(spec.Key, mapping); err != nil {
		return ImportCommitResult{}, err
	}
	if len(rows) > importMaxRows {
		return ImportCommitResult{}, importErrorf("单次最多导入 %d 行，本次 %d 行，请拆分后导入", importMaxRows, len(rows))
	}
	result := ImportCommitResult{Errors: make([]ImportRowError, 0)}
	switch spec.Key {
	case importTargetChargeItem, importTargetMaterial:
		return s.commitChargeItems(ctx, tenantID, spec.Key, mapping, rows, result)
	case importTargetDrug:
		return s.commitDrugs(ctx, tenantID, mapping, rows, result)
	case importTargetPurchase:
		return s.commitPurchase(ctx, tenantID, employeeID, mapping, rows, result)
	default:
		return ImportCommitResult{}, importErrorf("不支持的导入目标：%s", target)
	}
}

func (s *Service) commitChargeItems(ctx context.Context, tenantID int64, target string, mapping map[string]int, rows []importRow, result ImportCommitResult) (ImportCommitResult, error) {
	codes := mappedCodes(mapping, rows, "code")
	existing, err := s.store.ImportExistingChargeItemCodes(ctx, tenantID, codes)
	if err != nil {
		return ImportCommitResult{}, err
	}
	for _, row := range rows {
		if rowEmpty(row.Cells) {
			continue
		}
		values := mappedValues(mapping, row.Cells)
		item, err := parseChargeItemValues(values, row.Number, target)
		if err != nil {
			result.addError(row.Number, err)
			continue
		}
		if err := checkDuplicateCode(item.Code, existing); err != nil {
			result.addError(row.Number, err)
			continue
		}
		if err := s.store.InsertImportChargeItem(ctx, tenantID, item); err != nil {
			if errors.Is(err, ErrChargeItemCodeExists) {
				result.addError(row.Number, fmt.Errorf("编码 %s 已存在", item.Code))
				continue
			}
			slog.Error("import charge item failed", "error", err, "row", row.Number)
			result.addError(row.Number, fmt.Errorf("保存失败，请稍后重试"))
			continue
		}
		result.Success++
	}
	return result, nil
}

func (s *Service) commitDrugs(ctx context.Context, tenantID int64, mapping map[string]int, rows []importRow, result ImportCommitResult) (ImportCommitResult, error) {
	codes := mappedCodes(mapping, rows, "code")
	existing, err := s.store.ImportExistingDrugCodes(ctx, tenantID, codes)
	if err != nil {
		return ImportCommitResult{}, err
	}
	for _, row := range rows {
		if rowEmpty(row.Cells) {
			continue
		}
		values := mappedValues(mapping, row.Cells)
		item, err := parseDrugValues(values, row.Number)
		if err != nil {
			result.addError(row.Number, err)
			continue
		}
		if err := checkDuplicateCode(item.Req.Code, existing); err != nil {
			result.addError(row.Number, err)
			continue
		}
		if err := s.store.InsertImportDrug(ctx, tenantID, item.Req); err != nil {
			if errors.Is(err, ErrDrugCodeExists) {
				result.addError(row.Number, fmt.Errorf("编码 %s 已存在", item.Req.Code))
				continue
			}
			slog.Error("import drug failed", "error", err, "row", row.Number)
			result.addError(row.Number, fmt.Errorf("保存失败，请稍后重试"))
			continue
		}
		result.Success++
	}
	return result, nil
}

type importPurchaseGroup struct {
	supplier string
	items    []CreatePurchaseOrderItem
	rows     []int
}

func (s *Service) commitPurchase(ctx context.Context, tenantID, employeeID int64, mapping map[string]int, rows []importRow, result ImportCommitResult) (ImportCommitResult, error) {
	groups := make([]*importPurchaseGroup, 0)
	groupBySupplier := make(map[string]*importPurchaseGroup)
	for _, row := range rows {
		if rowEmpty(row.Cells) {
			continue
		}
		values := mappedValues(mapping, row.Cells)
		item, err := parsePurchaseValues(values, row.Number)
		if err != nil {
			result.addError(row.Number, err)
			continue
		}
		drugID, _, err := s.store.FindDrugForImport(ctx, tenantID, item.DrugKey)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				result.addError(row.Number, fmt.Errorf("药品「%s」不存在，请先在药品字典建档", item.DrugKey))
			} else {
				slog.Error("import purchase lookup drug failed", "error", err, "row", row.Number)
				result.addError(row.Number, fmt.Errorf("查询药品失败，请稍后重试"))
			}
			continue
		}
		group, ok := groupBySupplier[item.Supplier]
		if !ok {
			group = &importPurchaseGroup{supplier: item.Supplier}
			groupBySupplier[item.Supplier] = group
			groups = append(groups, group)
		}
		group.items = append(group.items, CreatePurchaseOrderItem{
			DrugID:        drugID,
			Quantity:      item.Quantity,
			PurchasePrice: item.PurchasePrice,
			BatchNo:       item.BatchNo,
			ExpiryDate:    item.ExpiryDate,
		})
		group.rows = append(group.rows, item.Row)
	}
	for _, group := range groups {
		order, err := s.CreatePurchaseOrder(ctx, tenantID, employeeID, CreatePurchaseOrderRequest{
			Supplier: group.supplier,
			Items:    group.items,
		})
		if err != nil {
			slog.Error("import purchase order failed", "error", err, "supplier", group.supplier)
			for _, row := range group.rows {
				result.addError(row, fmt.Errorf("创建采购单失败，请稍后重试"))
			}
			continue
		}
		result.Success += len(group.items)
		result.Orders = append(result.Orders, ImportPurchaseOrderSummary{
			ID:          order.ID,
			OrderNo:     order.OrderNo,
			Supplier:    order.Supplier,
			ItemCount:   len(order.Items),
			TotalAmount: order.TotalAmount,
		})
	}
	return result, nil
}

func mappedCodes(mapping map[string]int, rows []importRow, field string) []string {
	codes := make([]string, 0, len(rows))
	for _, row := range rows {
		values := mappedValues(mapping, row.Cells)
		if code := strings.TrimSpace(values[field]); code != "" {
			codes = append(codes, code)
		}
	}
	return codes
}

var importTemplateHeaders = map[string][]string{
	importTargetChargeItem: {"编码", "名称", "分类", "单位", "价格", "状态", "医保类别"},
	importTargetMaterial:   {"编码", "名称", "单位", "价格", "状态", "医保类别"},
	importTargetDrug:       {"编码", "名称", "规格", "单位", "生产厂家", "零售价", "进价", "药品类型", "是否处方药"},
	importTargetPurchase:   {"供应商", "药品名称或编码", "数量", "进价", "批号", "效期"},
}

// ImportTemplateCSV 返回 UTF-8 BOM 的空白模板，Excel 双击打开不乱码。
func ImportTemplateCSV(target string) ([]byte, error) {
	headers, ok := importTemplateHeaders[strings.TrimSpace(target)]
	if !ok {
		return nil, importErrorf("不支持的导入目标：%s", target)
	}
	var buffer bytes.Buffer
	buffer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(&buffer)
	if err := writer.Write(headers); err != nil {
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
