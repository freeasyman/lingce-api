package dealexpert

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type renameCustomerRequest struct {
	Name string `json:"name"`
}

// RenameRecordingCustomer 修改接待的客户/患者姓名。
// 同步 customers、encounters.patient_name 和已生成的病历快照，避免客户总监、
// 听写病历、接待记录三处名称不一致。
func (h *Handler) RenameRecordingCustomer(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	recordingID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || recordingID <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	if accessErr := h.checkRecordingAccess(r.Context(), scope, recordingID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	var body renameCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		httputil.WriteBadRequest(w, "客户姓名不能为空")
		return
	}
	if len([]rune(name)) > 80 {
		httputil.WriteBadRequest(w, "客户姓名过长")
		return
	}

	var customerID, encounterID int64
	if err := h.pool.QueryRow(r.Context(), `
		SELECT COALESCE(customer_id,0), COALESCE(encounter_id,0)
		FROM recordings WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL
	`, scope.TenantID, recordingID).Scan(&customerID, &encounterID); err != nil {
		httputil.WriteNotFound(w, "recording not found")
		return
	}
	if customerID <= 0 {
		httputil.WriteBadRequest(w, "这条接待还没有关联客户，不能直接改名")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `
		UPDATE customers
		SET name=$1, pinyin='', initials='', updated_at=NOW()
		WHERE tenant_id=$2 AND id=$3 AND deleted_at IS NULL
	`, name, scope.TenantID, customerID); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if encounterID > 0 {
		if _, err := tx.Exec(r.Context(), `
			UPDATE encounters SET patient_name=$1, updated_at=NOW()
			WHERE tenant_id=$2 AND id=$3
		`, name, scope.TenantID, encounterID); err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE emr_records
		SET patient_snapshot_json=jsonb_set(patient_snapshot_json, '{name}', to_jsonb($1::text), true),
		    updated_at=NOW()
		WHERE tenant_id=$2 AND customer_id=$3
	`, name, scope.TenantID, customerID); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"customer_id": customerID, "name": name})
}
