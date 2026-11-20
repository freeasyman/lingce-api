package recording

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ensureMobileUploadCustomer 为手机直传的录音建立客户主档。
// App 当前在录音开始前不选患者，所以没有姓名时使用「时间 + 员工」占位名；
// 医生/咨询师之后可在接待详情改名，改的是同一个 customer。
func (h *Handler) ensureMobileUploadCustomer(ctx context.Context, tenantID, employeeID int64, patientName string, recordedAt time.Time) (int64, error) {
	employeeName := ""
	if err := h.service.store.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(full_name,''), NULLIF(name,''), '')
		FROM employees
		WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
	`, employeeID, tenantID).Scan(&employeeName); err != nil {
		return 0, fmt.Errorf("load employee: %w", err)
	}
	employeeName = strings.TrimSpace(employeeName)
	if employeeName == "" {
		employeeName = "员工"
	}

	name := strings.TrimSpace(patientName)
	anonymous := name == ""
	if anonymous {
		cst := time.FixedZone("CST", 8*3600)
		name = fmt.Sprintf("[%s %s接待的客户]", recordedAt.In(cst).Format("01-02 15:04"), employeeName)
	}
	extra, _ := json.Marshal(map[string]any{
		"source_type": "mobile_app",
		"anonymous":   anonymous,
	})
	var customerID int64
	if err := h.service.store.pool.QueryRow(ctx, `
		INSERT INTO customers (
			tenant_id, name, pinyin, initials, source, lifecycle_stage, status, momentum,
			notes, extra_data, created_by, created_at, updated_at
		)
		VALUES ($1, $2, '', '', 'mobile_app', 'unknown', 'lead', 50,
			$3, $4::jsonb, $5, NOW(), NOW())
		RETURNING id
	`, tenantID, name, map[bool]string{true: "手机录音自动建档：信息待补", false: "手机录音建档"}[anonymous], string(extra), employeeID).Scan(&customerID); err != nil {
		return 0, fmt.Errorf("create mobile customer: %w", err)
	}
	return customerID, nil
}
