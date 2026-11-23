package patientops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/customercontact"
	"github.com/jackc/pgx/v5"
)

var ErrScreenshotReviewConflict = errors.New("截图状态已变化，请刷新后重试")
var ErrScreenshotReviewInvalid = errors.New("截图处理参数无效")

// A short, tenant-scoped transaction lock serializes automatic account reuse,
// contact writes and human reviews. Model calls run outside this transaction.
// The lock also protects nickname-only sources from duplicate retry creation.
func lockScreenshotTenant(ctx context.Context, tx pgx.Tx, tenantID int64) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('screenshot-customer:' || $1::bigint::text,0))`, tenantID)
	return err
}

func screenshotLeadName(platform, account, nickname string) string {
	channel := customercontact.NormalizeChannel(platform)
	label := map[string]string{"wechat": "微信", "wecom": "企业微信", "xiaohongshu": "小红书", "douyin": "抖音", "shipinhao": "视频号"}[channel]
	if label == "" && channel != "unknown" {
		label = strings.TrimSpace(platform)
	}
	value := strings.TrimSpace(nickname)
	if value == "" {
		value = strings.TrimSpace(account)
	}
	if label == "" {
		return value
	}
	if value == "" {
		return label + "线索"
	}
	return label + " " + value
}

// completeScreenshotRecognition also repairs a successful, unbound duplicate
// upload. An explicit binding or rejection always wins over automatic creation.
func (s *Service) completeScreenshotRecognition(ctx context.Context, tenantID, sourceID int64) (*int64, string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback(ctx)
	if err := lockScreenshotTenant(ctx, tx, tenantID); err != nil {
		return nil, "", err
	}
	var customerID *int64
	var status string
	var operatorID *int64
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT customer_id,match_status,created_by,recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND id=$2 AND source_type='screenshot' FOR UPDATE`, tenantID, sourceID).Scan(&customerID, &status, &operatorID, &raw); err != nil {
		return nil, "", err
	}
	var a ScreenshotAnalysis
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, "", err
	}
	if status != "rejected" {
		if customerID == nil {
			customerID, status, err = ensureScreenshotLead(ctx, tx, tenantID, sourceID, operatorID, a)
			if err != nil {
				return nil, "", err
			}
		} else {
			// Never replace a historical binding, including a deleted customer.
			var live bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customers WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL)`, tenantID, *customerID).Scan(&live); err != nil {
				return nil, "", err
			}
			if live {
				if err := saveScreenshotContact(ctx, tx, tenantID, *customerID, a.Platform, a.Account, a.Nickname); err != nil {
					return nil, "", err
				}
				if status == "pending" {
					var pending bool
					if err := tx.QueryRow(ctx, `SELECT COALESCE(extra_data->>'identity_status'='pending_confirm',false) FROM customers WHERE tenant_id=$1 AND id=$2`, tenantID, *customerID).Scan(&pending); err != nil {
						return nil, "", err
					}
					status = "confirmed"
					if pending {
						status = "pending_confirm"
					}
				}
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE patient_conversation_sources SET customer_id=$3,match_status=$4,analysis_status='succeeded',analysis_error='',analysis_completed_at=NOW(),updated_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, sourceID, customerID, status); err != nil {
		return nil, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}
	return customerID, status, nil
}

func ensureScreenshotLead(ctx context.Context, tx pgx.Tx, tenantID, sourceID int64, employeeID *int64, a ScreenshotAnalysis) (*int64, string, error) {
	a.Normalize()
	channel := customercontact.NormalizeChannel(a.Platform)
	if channel == "unknown" {
		channel = ""
	}
	account, nickname := strings.TrimSpace(a.Account), strings.TrimSpace(a.Nickname)
	name := screenshotLeadName(a.Platform, account, nickname)
	if name == "" {
		return nil, "pending", nil
	}
	var id int64
	if account != "" {
		// Normalize legacy aliases in Go using the same rules as new contact writes.
		rows, err := tx.Query(ctx, `SELECT DISTINCT c.id,COALESCE(NULLIF(i.channel,''),i.channel_type,'') FROM customer_identities i JOIN customers c ON c.id=i.customer_id WHERE c.tenant_id=$1 AND c.deleted_at IS NULL AND btrim(COALESCE(NULLIF(i.channel_id,''),i.external_id,''))=$2 ORDER BY c.id`, tenantID, account)
		if err != nil {
			return nil, "", err
		}
		var matches []int64
		for rows.Next() {
			var cid int64
			var ch string
			if err := rows.Scan(&cid, &ch); err != nil {
				rows.Close()
				return nil, "", err
			}
			if customercontact.NormalizeChannel(ch) == customercontact.NormalizeChannel(channel) && (len(matches) == 0 || matches[len(matches)-1] != cid) {
				matches = append(matches, cid)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, "", err
		}
		// Pre-existing conflicting owners cannot be resolved automatically.
		if len(matches) > 1 {
			return nil, "pending", nil
		}
		if len(matches) == 1 {
			id = matches[0]
		}
	}
	if id == 0 && nickname != "" {
		// 账号为空时，按「渠道+昵称」在待确认的自动线索里归并：避免同一个联系人的多张截图建出多个待确认客户。
		// 只复用 pending_confirm 的自动线索，不碰已确认客户；最终是否同一人仍由人工确认。
		rows, err := tx.Query(ctx, `SELECT c.id FROM customers c WHERE c.tenant_id=$1 AND c.deleted_at IS NULL AND c.extra_data->>'screenshot_auto_lead'='true' AND c.extra_data->>'identity_status'='pending_confirm' AND c.name=$2 ORDER BY c.id`, tenantID, name)
		if err != nil {
			return nil, "", err
		}
		var nickMatches []int64
		for rows.Next() {
			var cid int64
			if err := rows.Scan(&cid); err != nil {
				rows.Close()
				return nil, "", err
			}
			nickMatches = append(nickMatches, cid)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, "", err
		}
		if len(nickMatches) > 1 {
			// 多个同名待确认线索，可能是不同人，不自动归并，留人工处理。
			return nil, "pending", nil
		}
		if len(nickMatches) == 1 {
			id = nickMatches[0]
		}
	}
	if id == 0 {
		err := tx.QueryRow(ctx, `INSERT INTO customers(tenant_id,name,source,lifecycle_stage,status,momentum,created_by,created_at,updated_at,extra_data) VALUES($1,$2,$3,'unknown','lead',50,$4,NOW(),NOW(),jsonb_build_object('prospect',true,'screenshot_auto_lead',true,'identity_status','pending_confirm')) RETURNING id`, tenantID, name, channel, employeeID).Scan(&id)
		if err != nil {
			return nil, "", err
		}
		if _, err := tx.Exec(ctx, `UPDATE patient_conversation_sources SET auto_created_customer_id=$3 WHERE tenant_id=$1 AND id=$2`, tenantID, sourceID, id); err != nil {
			return nil, "", err
		}
	}
	if err := saveScreenshotContact(ctx, tx, tenantID, id, channel, account, nickname); err != nil {
		return nil, "", err
	}
	if nickname != "" {
		if _, err := tx.Exec(ctx, `UPDATE customers SET name=$3,updated_at=NOW() WHERE tenant_id=$1 AND id=$2 AND extra_data->>'screenshot_auto_lead'='true' AND extra_data->>'identity_status'='pending_confirm' AND deleted_at IS NULL`, tenantID, id, name); err != nil {
			return nil, "", err
		}
	}
	return &id, "pending_confirm", nil
}

const screenshotReviewCondition = `s.source_type='screenshot' AND s.match_status='pending_confirm'`

func (s *Service) CountUnlinkedSources(ctx context.Context, tenantID, employeeID int64) (int64, int64, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var count, unread int64
	err := s.pool.QueryRow(queryCtx, `SELECT COUNT(*) FROM patient_conversation_sources s WHERE s.tenant_id=$1 AND `+screenshotReviewCondition, tenantID).Scan(&count)
	if err != nil {
		return 0, 0, err
	}
	if employeeID > 0 {
		// 未读数：当前用户还没点开过的未关联截图来源（菜单角标用）。
		unreadQuery := `SELECT COUNT(*) FROM patient_conversation_sources s
			WHERE s.tenant_id=$1 AND ` + screenshotReviewCondition + `
			  AND NOT EXISTS (SELECT 1 FROM user_resource_reads m
			                  WHERE m.tenant_id=$1 AND m.employee_id=$2
			                    AND m.resource_type='patient_screenshot' AND m.resource_id=s.id::text)`
		if err := s.pool.QueryRow(queryCtx, unreadQuery, tenantID, employeeID).Scan(&unread); err != nil {
			return count, 0, err
		}
	}
	return count, unread, nil
}
func (s *Service) ListUnlinkedSources(ctx context.Context, tenantID int64, page, pageSize int) (*UnlinkedSourcesPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result := &UnlinkedSourcesPage{Items: []ConversationSourceResponse{}, Page: page, PageSize: pageSize}
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*),COUNT(DISTINCT s.customer_id) FROM patient_conversation_sources s WHERE s.tenant_id=$1 AND `+screenshotReviewCondition, tenantID).Scan(&result.Total, &result.CustomerTotal); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.source_type,COALESCE(s.original_name,''),s.captured_at,s.customer_id,s.match_status,s.recognition_payload,COALESCE(c.name,''),COALESCE(c.phone,''),COALESCE(c.extra_data->>'screenshot_auto_lead'='true' AND c.extra_data->>'identity_status'='pending_confirm' AND c.deleted_at IS NULL,false),s.customer_id IS NOT NULL AND (c.id IS NULL OR c.deleted_at IS NOT NULL) FROM patient_conversation_sources s LEFT JOIN customers c ON c.id=s.customer_id AND c.tenant_id=s.tenant_id WHERE s.tenant_id=$1 AND `+screenshotReviewCondition+` ORDER BY s.captured_at DESC,s.id DESC LIMIT $2 OFFSET $3`, tenantID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ConversationSourceResponse
		var captured time.Time
		var raw []byte
		if err := rows.Scan(&item.ID, &item.SourceType, &item.OriginalName, &captured, &item.CustomerID, &item.MatchStatus, &raw, &item.CustomerName, &item.CustomerPhone, &item.CanMerge, &item.CustomerDeleted); err != nil {
			return nil, err
		}
		item.CapturedAt = captured.Format(time.RFC3339)
		if err := json.Unmarshal(raw, &item.Recognition); err != nil {
			return nil, err
		}
		item.Recognition.Normalize()
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := s.markDuplicateScreenshotNicknames(ctx, tenantID, result.Items); err != nil {
		return nil, err
	}
	return result, nil
}

// Count distinct live automatic leads, across pages, never screenshots of the
// same customer. Platform aliases use the same rules as automatic reuse.
func (s *Service) markDuplicateScreenshotNicknames(ctx context.Context, tenantID int64, items []ConversationSourceResponse) error {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT c.id,COALESCE(NULLIF(i.channel,''),i.channel_type,''),COALESCE(NULLIF(i.nickname,''),i.external_name,'') FROM customers c JOIN customer_identities i ON i.customer_id=c.id WHERE c.tenant_id=$1 AND c.deleted_at IS NULL AND c.extra_data->>'screenshot_auto_lead'='true' AND c.extra_data->>'identity_status'='pending_confirm' AND EXISTS(SELECT 1 FROM patient_conversation_sources s WHERE s.tenant_id=c.tenant_id AND s.customer_id=c.id AND s.match_status='pending_confirm')`, tenantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type key struct{ channel, nickname string }
	groups := map[key]map[int64]bool{}
	for rows.Next() {
		var id int64
		var channel, nickname string
		if err := rows.Scan(&id, &channel, &nickname); err != nil {
			return err
		}
		nickname = cleanScreenshotIdentityValue(nickname)
		if nickname == "" {
			continue
		}
		k := key{customercontact.NormalizeChannel(channel), nickname}
		if groups[k] == nil {
			groups[k] = map[int64]bool{}
		}
		groups[k][id] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for n := range items {
		a := items[n].Recognition
		group := groups[key{customercontact.NormalizeChannel(a.Platform), a.Nickname}]
		if items[n].CanMerge && items[n].CustomerID != nil && group[*items[n].CustomerID] && len(group) > 1 {
			items[n].DuplicateLeadCount = len(group)
		}
	}
	return nil
}
func (s *Service) GetUnlinkedSource(ctx context.Context, tenantID, sourceID int64) (*ConversationSourceResponse, error) {
	var item ConversationSourceResponse
	var captured time.Time
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT s.id,s.source_type,COALESCE(s.original_name,''),COALESCE(s.media_data,''),s.captured_at,s.customer_id,s.match_status,s.recognition_payload FROM patient_conversation_sources s WHERE s.tenant_id=$1 AND s.id=$2 AND `+screenshotReviewCondition, tenantID, sourceID).Scan(&item.ID, &item.SourceType, &item.OriginalName, &item.MediaData, &captured, &item.CustomerID, &item.MatchStatus, &raw)
	if err != nil {
		return nil, err
	}
	item.CapturedAt = captured.Format(time.RFC3339)
	if err := json.Unmarshal(raw, &item.Recognition); err != nil {
		return nil, err
	}
	item.Recognition.Normalize()
	return &item, nil
}

type ScreenshotReviewRequest struct {
	Action              string `json:"action"`
	CustomerID          int64  `json:"customer_id,omitempty"`
	ExpectedCustomerID  *int64 `json:"expected_customer_id,omitempty"`
	ExpectedMatchStatus string `json:"expected_match_status,omitempty"`
	Name                string `json:"name,omitempty"`
	Phone               string `json:"phone,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

func writeScreenshotReview(ctx context.Context, tx pgx.Tx, tenantID, sourceID, operatorID int64, action string, from, to *int64, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO screenshot_customer_reviews(tenant_id,source_id,operator_id,action,from_customer_id,to_customer_id,detail) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, tenantID, sourceID, operatorID, action, from, to, raw)
	return err
}

func (s *Service) ReviewScreenshot(ctx context.Context, tenantID, employeeID, sourceID int64, req ScreenshotReviewRequest) (*int64, error) {
	if tenantID <= 0 || employeeID <= 0 || sourceID <= 0 {
		return nil, ErrScreenshotReviewInvalid
	}
	switch req.Action {
	case "confirm", "merge", "rebind", "reject":
	default:
		return nil, ErrScreenshotReviewInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := lockScreenshotTenant(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	var from *int64
	var status string
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT customer_id,match_status,recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND id=$2 AND source_type='screenshot' FOR UPDATE`, tenantID, sourceID).Scan(&from, &status, &raw); err != nil {
		return nil, err
	}
	if req.ExpectedMatchStatus != "" && req.ExpectedMatchStatus != status {
		return nil, ErrScreenshotReviewConflict
	}
	if req.ExpectedCustomerID != nil && (from == nil || *req.ExpectedCustomerID != *from) {
		return nil, ErrScreenshotReviewConflict
	}
	var autoLead bool
	if from != nil {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(extra_data->>'screenshot_auto_lead'='true' AND extra_data->>'identity_status'='pending_confirm',false) FROM customers WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tenantID, *from).Scan(&autoLead); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	to := from
	switch req.Action {
	case "reject":
		if status == "rejected" {
			return from, nil
		}
		if status != "pending" && status != "pending_confirm" {
			return nil, ErrScreenshotReviewConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE patient_conversation_sources SET match_status='rejected',updated_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, sourceID); err != nil {
			return nil, err
		}
		// A shared account lead remains live while another screenshot still needs it.
		if autoLead {
			if err := retireUnusedScreenshotLead(ctx, tx, tenantID, *from); err != nil {
				return nil, err
			}
		}
	case "confirm":
		if from == nil || status != "pending_confirm" {
			return nil, ErrScreenshotReviewConflict
		}
		if autoLead {
			if strings.TrimSpace(req.Name) == "" && strings.TrimSpace(req.Phone) == "" {
				return nil, fmt.Errorf("%w: 请补充姓名或手机号", ErrScreenshotReviewInvalid)
			}
			if _, err := tx.Exec(ctx, `UPDATE customers SET name=COALESCE(NULLIF(btrim($3),''),name),phone=COALESCE(NULLIF(btrim($4),''),phone),extra_data=COALESCE(extra_data,'{}'::jsonb)||'{"identity_status":"confirmed"}'::jsonb,updated_at=NOW() WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, *from, req.Name, req.Phone); err != nil {
				return nil, err
			}
		} else {
			var live bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customers WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL)`, tenantID, *from).Scan(&live); err != nil {
				return nil, err
			}
			if !live {
				return nil, ErrScreenshotReviewConflict
			}
		}
		// Confirm the customer identity across its automatic screenshots.
		if _, err := tx.Exec(ctx, `UPDATE patient_conversation_sources SET match_status='confirmed',updated_at=NOW() WHERE tenant_id=$1 AND customer_id=$2 AND match_status='pending_confirm' AND ($3::boolean OR id=$4)`, tenantID, *from, autoLead, sourceID); err != nil {
			return nil, err
		}
	case "merge", "rebind":
		if req.CustomerID <= 0 {
			return nil, ErrScreenshotReviewInvalid
		}
		to = &req.CustomerID
		var live bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customers WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL)`, tenantID, *to).Scan(&live); err != nil {
			return nil, err
		}
		if !live {
			return nil, ErrCustomerTenantMismatch
		}
		var targetPending bool
		if err := tx.QueryRow(ctx, `SELECT COALESCE(extra_data->>'identity_status'='pending_confirm',false) FROM customers WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tenantID, *to).Scan(&targetPending); err != nil {
			return nil, err
		}
		if targetPending {
			return nil, fmt.Errorf("%w: 请先确认目标客户身份", ErrScreenshotReviewInvalid)
		}
		var a ScreenshotAnalysis
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		if req.Action == "merge" {
			if !autoLead || from == nil || *from == *to {
				return nil, fmt.Errorf("%w: 只能合并待确认的自动线索客户", ErrScreenshotReviewInvalid)
			}
			if err := mergeScreenshotLead(ctx, tx, tenantID, employeeID, *from, *to); err != nil {
				return nil, err
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE patient_conversation_sources SET customer_id=$3,match_status='confirmed',updated_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, sourceID, *to); err != nil {
				return nil, err
			}
			if autoLead && from != nil && *from != *to {
				if err := retireUnusedScreenshotLead(ctx, tx, tenantID, *from); err != nil {
					return nil, err
				}
			}
		}
		if err := saveScreenshotContact(ctx, tx, tenantID, *to, a.Platform, a.Account, a.Nickname); err != nil {
			return nil, err
		}
	}
	if err := writeScreenshotReview(ctx, tx, tenantID, sourceID, employeeID, req.Action, from, to, req); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return to, nil
}

func retireUnusedScreenshotLead(ctx context.Context, tx pgx.Tx, tenantID, customerID int64) error {
	// Soft deletion keeps rejected evidence and allows database-level recovery.
	_, err := tx.Exec(ctx, `UPDATE customers c SET deleted_at=NOW(),updated_at=NOW(),extra_data=COALESCE(extra_data,'{}'::jsonb)||'{"identity_status":"rejected"}'::jsonb WHERE c.tenant_id=$1 AND c.id=$2 AND c.extra_data->>'identity_status'='pending_confirm' AND NOT EXISTS(SELECT 1 FROM patient_conversation_sources s WHERE s.tenant_id=c.tenant_id AND s.customer_id=c.id AND s.match_status<>'rejected')`, tenantID, customerID)
	return err
}

// This merge accepts only system-created, unconfirmed leads. Formal customers
// keep their existing general merge workflow; no Patient/HIS association moves.
func mergeScreenshotLead(ctx context.Context, tx pgx.Tx, tenantID, operatorID, from, to int64) error {
	// Refuse external/HIS facts: those require the general identity review flow.
	for _, table := range []string{"recordings", "external_data_source_identities", "external_data_appointments", "external_data_queues", "emr_records"} {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			continue
		}
		var has bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE customer_id=$1)`, from).Scan(&has); err != nil {
			return err
		}
		if has {
			return fmt.Errorf("%w: 线索已有录音或外部病历，请先人工核对", ErrScreenshotReviewInvalid)
		}
	}
	// Both target and source are tenant-checked before any migration.
	if _, err := tx.Exec(ctx, `SELECT id FROM customers WHERE tenant_id=$1 AND id=ANY($2::bigint[]) AND deleted_at IS NULL ORDER BY id FOR UPDATE`, tenantID, []int64{from, to}); err != nil {
		return err
	}
	for _, table := range []string{"patient_operation_tasks", "patient_operation_task_drafts", "patient_operation_records", "patient_operation_recording_runs", "customer_follow_ups", "customer_memberships"} {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE `+table+` SET customer_id=$2 WHERE customer_id=$1`, from, to); err != nil {
			return err
		}
	}
	// Pending leads are excluded from plans. Refuse any legacy plan membership
	// rather than dropping duplicate rows or silently losing task links.
	var inPlan bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patient_ops_plan_members WHERE tenant_id=$1 AND customer_id=$2)`, tenantID, from).Scan(&inPlan); err != nil {
		return err
	}
	if inPlan {
		return fmt.Errorf("%w: 线索已进入经营计划，请先人工核对", ErrScreenshotReviewInvalid)
	}
	rows, err := tx.Query(ctx, `SELECT COALESCE(NULLIF(channel,''),channel_type,''),COALESCE(NULLIF(channel_id,''),external_id,''),COALESCE(NULLIF(nickname,''),external_name,'') FROM customer_identities WHERE customer_id=$1 ORDER BY id`, from)
	if err != nil {
		return err
	}
	type identity struct{ ch, account, nick string }
	var identities []identity
	for rows.Next() {
		var i identity
		if err := rows.Scan(&i.ch, &i.account, &i.nick); err != nil {
			rows.Close()
			return err
		}
		if customercontact.IsInternal(i.ch) {
			rows.Close()
			return fmt.Errorf("%w: 线索包含外部患者身份", ErrScreenshotReviewInvalid)
		}
		identities = append(identities, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range identities {
		if err := saveScreenshotContact(ctx, tx, tenantID, to, i.ch, i.account, i.nick); err != nil {
			return err
		}
	}
	// Retain the original identity rows on the soft-deleted source for review.
	// Contact queries and automatic reuse only consider live customers.
	if _, err := tx.Exec(ctx, `UPDATE patient_conversation_sources SET customer_id=$3,match_status=CASE WHEN match_status='rejected' THEN 'rejected' ELSE 'confirmed' END,updated_at=NOW() WHERE tenant_id=$1 AND customer_id=$2`, tenantID, from, to); err != nil {
		return err
	}
	// Legacy interaction tables have no tenant_id; scope through their customer.
	if _, err := tx.Exec(ctx, `UPDATE customer_interactions i SET customer_id=$3 FROM customers c WHERE c.id=i.customer_id AND c.tenant_id=$1 AND c.id=$2`, tenantID, from, to); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE customers SET deleted_at=NOW(),updated_at=NOW(),extra_data=COALESCE(extra_data,'{}'::jsonb)||jsonb_build_object('identity_status','merged','merged_into',$3::bigint,'merged_by',$4::bigint) WHERE tenant_id=$1 AND id=$2`, tenantID, from, to, operatorID)
	return err
}

// BackfillScreenshotLeads is deliberately strict and atomic: the reviewed IDs
// must all be successful and unbound. Deleted-customer links are never repaired
// implicitly. Repeating an already applied batch is a harmless no-op.
func (s *Service) BackfillScreenshotLeads(ctx context.Context, tenantID, operatorID int64, ids []int64) (map[int64]string, error) {
	return s.BackfillReviewedScreenshotLeads(ctx, tenantID, operatorID, ids, nil)
}

// Reviewed pending IDs are an explicit per-source human attestation, never an
// automatic inference of model success from a nonempty payload.
func (s *Service) BackfillReviewedScreenshotLeads(ctx context.Context, tenantID, operatorID int64, ids, reviewedPendingIDs []int64) (map[int64]string, error) {
	if tenantID <= 0 || operatorID <= 0 || len(ids) == 0 {
		return nil, ErrScreenshotReviewInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := lockScreenshotTenant(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	unique := map[int64]bool{}
	sorted := []int64{}
	for _, id := range ids {
		if id <= 0 {
			return nil, ErrScreenshotReviewInvalid
		}
		if !unique[id] {
			unique[id] = true
			sorted = append(sorted, id)
		}
	}
	reviewed := map[int64]bool{}
	for _, id := range reviewedPendingIDs {
		if !unique[id] {
			return nil, fmt.Errorf("reviewed pending ID %d must also be included in the batch IDs", id)
		}
		reviewed[id] = true
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	result := map[int64]string{}
	for _, id := range sorted {
		var current, creator *int64
		var analysisStatus, status string
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT customer_id,created_by,analysis_status,match_status,recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND id=$2 AND source_type='screenshot' FOR UPDATE`, tenantID, id).Scan(&current, &creator, &analysisStatus, &status, &raw); err != nil {
			return nil, err
		}
		if current != nil {
			result[id] = "skipped_already_bound"
			continue
		}
		if (analysisStatus != "succeeded" && !(analysisStatus == "pending" && reviewed[id])) || status == "rejected" {
			return nil, fmt.Errorf("source %d is not eligible for backfill", id)
		}
		var a ScreenshotAnalysis
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		if analysisStatus == "pending" && !ScreenshotPendingPayloadReviewable(raw) {
			return nil, fmt.Errorf("source %d has no reviewable identity and recognition content", id)
		}
		customerID, newStatus, err := ensureScreenshotLead(ctx, tx, tenantID, id, creator, a)
		if err != nil {
			return nil, err
		}
		if customerID == nil {
			result[id] = "skipped_no_unambiguous_identity"
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE patient_conversation_sources SET customer_id=$3,match_status=$4,analysis_status='succeeded',analysis_error='',analysis_completed_at=COALESCE(analysis_completed_at,NOW()),updated_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, id, *customerID, newStatus); err != nil {
			return nil, err
		}
		if err := writeScreenshotReview(ctx, tx, tenantID, id, operatorID, "backfill", nil, customerID, map[string]any{"batch_source_ids": sorted, "previous_analysis_status": analysisStatus, "pending_payload_human_reviewed": reviewed[id], "original_recognition_payload": json.RawMessage(raw)}); err != nil {
			return nil, err
		}
		result[id] = newStatus
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// Structural eligibility for human review, not proof of identity or accuracy.
func ScreenshotPendingPayloadReviewable(raw []byte) bool {
	var a ScreenshotAnalysis
	if json.Unmarshal(raw, &a) != nil {
		return false
	}
	a.Normalize()
	if a.Account == "" && a.Nickname == "" {
		return false
	}
	for _, value := range append(append([]string{a.ConversationSummary, a.PatientQuestion}, a.PatientUtterances...), a.Evidence...) {
		if cleanScreenshotIdentityValue(value) != "" {
			return true
		}
	}
	return false
}
