package his

import "strings"

// normalizeCreateChargeItem 校验并规范化新增收费项目：编码/名称必填，单价非负。
func normalizeCreateChargeItem(req *CreateChargeItemRequest) error {
	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	req.Category = strings.TrimSpace(req.Category)
	req.Unit = strings.TrimSpace(req.Unit)
	if req.Code == "" || req.Name == "" {
		return ErrInvalidRequest
	}
	if req.Price < 0 {
		return ErrInvalidRequest
	}
	return nil
}

// normalizeUpdateChargeItem 校验并规范化收费项目部分更新：
// 至少提交一个字段；名称/分类/单位不得为空；单价非负；状态仅允许 active/inactive。
func normalizeUpdateChargeItem(req *UpdateChargeItemRequest) error {
	if req.Name == nil && req.Category == nil && req.Unit == nil && req.Price == nil && req.Status == nil {
		return ErrInvalidRequest
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return ErrInvalidRequest
		}
		req.Name = &name
	}
	if req.Category != nil {
		category := strings.TrimSpace(*req.Category)
		if category == "" {
			return ErrInvalidRequest
		}
		req.Category = &category
	}
	if req.Unit != nil {
		unit := strings.TrimSpace(*req.Unit)
		if unit == "" {
			return ErrInvalidRequest
		}
		req.Unit = &unit
	}
	if req.Price != nil && *req.Price < 0 {
		return ErrInvalidRequest
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if status != "active" && status != "inactive" {
			return ErrInvalidRequest
		}
		req.Status = &status
	}
	return nil
}
