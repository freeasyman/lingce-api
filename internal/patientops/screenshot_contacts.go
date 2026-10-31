package patientops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/customercontact"
	"github.com/jackc/pgx/v5"
)

// The source may be bound before analysis (customer detail upload) or while
// analysis is running. Read the binding under lock instead of trusting the
// original upload request.
func (s *Service) syncBoundScreenshotContact(ctx context.Context, tenantID, sourceID int64) error {
	return s.syncScreenshotContact(ctx, tenantID, sourceID, nil)
}

// Customer-context reuse may attach an unbound source, but must not silently
// reassign a source that another operator has already associated.
func (s *Service) syncScreenshotContact(ctx context.Context, tenantID, sourceID int64, requestedCustomerID *int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockScreenshotTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	var customerID *int64
	var status string
	var payload []byte
	if err := tx.QueryRow(ctx, `SELECT customer_id,match_status,recognition_payload FROM patient_conversation_sources WHERE tenant_id=$1 AND id=$2 AND source_type='screenshot' FOR UPDATE`, tenantID, sourceID).Scan(&customerID, &status, &payload); err != nil {
		return err
	}
	if status == "rejected" {
		return nil
	}
	if requestedCustomerID != nil {
		if customerID != nil && *customerID != *requestedCustomerID {
			return ErrScreenshotCustomerConflict
		}
		customerID = requestedCustomerID
	}
	if customerID == nil {
		return nil
	}
	var a ScreenshotAnalysis
	if err := json.Unmarshal(payload, &a); err != nil {
		return err
	}
	if err := saveScreenshotContact(ctx, tx, tenantID, *customerID, a.Platform, a.Account, a.Nickname); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE patient_conversation_sources SET customer_id=$3,match_status=CASE WHEN $4::boolean AND NOT EXISTS(SELECT 1 FROM customers c WHERE c.id=$3 AND c.tenant_id=$1 AND c.extra_data->>'identity_status'='pending_confirm') THEN 'confirmed' ELSE match_status END,updated_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, sourceID, *customerID, requestedCustomerID != nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// saveScreenshotContact must run in the same transaction as the source binding.
// Locking the customer serialises concurrent screenshots for that customer.
// A nickname-only contact keeps both account columns empty.
func saveScreenshotContact(ctx context.Context, tx pgx.Tx, tenantID, customerID int64, platform, account, nickname string) error {
	platform = cleanScreenshotIdentityValue(platform)
	channel := customercontact.NormalizeChannel(platform)
	account, nickname = cleanScreenshotIdentityValue(account), cleanScreenshotIdentityValue(nickname)
	var lockedID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM customers WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tenantID, customerID).Scan(&lockedID); err != nil {
		return fmt.Errorf("lock contact customer: %w", err)
	}
	if account == "" && nickname == "" && channel == "unknown" {
		return nil
	}
	if channel == "unknown" {
		channel = ""
	}
	rows, err := tx.Query(ctx, `SELECT id,COALESCE(NULLIF(channel,''),channel_type,''),COALESCE(NULLIF(channel_id,''),external_id,''),COALESCE(NULLIF(nickname,''),external_name,'') FROM customer_identities WHERE customer_id=$1 ORDER BY id`, customerID)
	if err != nil {
		return err
	}
	var existingID int64
	for rows.Next() {
		var id int64
		var oldChannel, oldAccount, oldNickname string
		if err := rows.Scan(&id, &oldChannel, &oldAccount, &oldNickname); err != nil {
			rows.Close()
			return err
		}
		if customercontact.NormalizeChannel(oldChannel) != customercontact.NormalizeChannel(channel) {
			continue
		}
		if (account != "" && strings.TrimSpace(oldAccount) == account) ||
			(account == "" && strings.TrimSpace(oldAccount) == "" && strings.TrimSpace(oldNickname) == nickname) {
			existingID = id
			break
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if existingID > 0 {
		_, err = tx.Exec(ctx, `UPDATE customer_identities SET channel=$1,channel_type=$1,nickname=COALESCE(NULLIF($2,''),nickname),external_name=COALESCE(NULLIF($2,''),external_name),updated_at=NOW() WHERE id=$3`, channel, nickname, existingID)
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO customer_identities(customer_id,channel,channel_type,channel_id,external_id,nickname,external_name,extra_data,created_at,updated_at) VALUES($1,$2,$2,$3,$3,$4,$4,'{}'::jsonb,NOW(),NOW())`, customerID, channel, account, nickname)
	return err
}
