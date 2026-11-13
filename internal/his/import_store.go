package his

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ImportExistingChargeItemCodes 返回租户内已存在的收费项目编码集合（批量查重用）。
func (s *Store) ImportExistingChargeItemCodes(ctx context.Context, tenantID int64, codes []string) (map[string]struct{}, error) {
	existing := make(map[string]struct{}, len(codes))
	if len(codes) == 0 {
		return existing, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT code FROM his_charge_items
		WHERE tenant_id = $1 AND code = ANY($2)`, tenantID, codes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		existing[code] = struct{}{}
	}
	return existing, rows.Err()
}

// ImportExistingDrugCodes 返回租户内已存在的药品编码集合（批量查重用）。
func (s *Store) ImportExistingDrugCodes(ctx context.Context, tenantID int64, codes []string) (map[string]struct{}, error) {
	existing := make(map[string]struct{}, len(codes))
	if len(codes) == 0 {
		return existing, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT code FROM his_drugs
		WHERE tenant_id = $1 AND code = ANY($2)`, tenantID, codes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		existing[code] = struct{}{}
	}
	return existing, rows.Err()
}

// InsertImportChargeItem 写入一行收费项目/材料（导入专用，支持状态与医保类别）。
func (s *Store) InsertImportChargeItem(ctx context.Context, tenantID int64, item importChargeItemValues) error {
	pinyin, initials := PinyinOf(item.Name)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO his_charge_items
			(tenant_id, code, name, category, unit, price, status, insurance_category, pinyin, initials)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		tenantID, item.Code, item.Name, item.Category, item.Unit, item.Price, item.Status,
		item.InsuranceCategory, pinyin, initials)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrChargeItemCodeExists
		}
		return err
	}
	return nil
}

// InsertImportDrug 写入一行药品（编码重复返回 ErrDrugCodeExists）。
func (s *Store) InsertImportDrug(ctx context.Context, tenantID int64, req CreateDrugRequest) error {
	_, err := s.CreateDrug(ctx, tenantID, req)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDrugCodeExists
		}
		return err
	}
	return nil
}

// FindDrugForImport 按编码优先、名称其次查找采购行对应的药品。
func (s *Store) FindDrugForImport(ctx context.Context, tenantID int64, key string) (int64, string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return 0, "", ErrNotFound
	}
	var id int64
	var name string
	err := s.pool.QueryRow(ctx, `
		SELECT id, name FROM his_drugs
		WHERE tenant_id = $1 AND (code = $2 OR name = $2)
		ORDER BY CASE WHEN code = $2 THEN 0 ELSE 1 END, id
		LIMIT 1`, tenantID, key).Scan(&id, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", ErrNotFound
	}
	if err != nil {
		return 0, "", err
	}
	return id, name, nil
}
