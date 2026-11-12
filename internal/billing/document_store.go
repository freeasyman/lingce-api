package billing

// tenant_documents 的数据访问层：只做 SQL 与扫描，不做业务判断。

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// documentColumns 是列表与详情共用的选择列；content 只在下载时单独查。
const documentColumns = `
       d.id, d.tenant_id, d.doc_type, d.doc_no, d.generated_at,
       COALESCE(d.generated_by, 0), COALESCE(a.name, ''), d.file_size`

// documentFrom 是文档查询的公共 FROM/JOIN 片段。
const documentFrom = `
FROM tenant_documents d
LEFT JOIN operations_admins a ON a.id = d.generated_by`

// ListDocumentNumbers 返回某前缀下已用的编号（生成下一个流水的依据）。
func (s *Store) ListDocumentNumbers(ctx context.Context, prefix string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT doc_no FROM tenant_documents WHERE doc_no LIKE $1 || '%'`, prefix)
	if err != nil {
		return nil, fmt.Errorf("list document numbers: %w", err)
	}
	defer rows.Close()

	numbers := make([]string, 0, 32)
	for rows.Next() {
		var docNo string
		if err := rows.Scan(&docNo); err != nil {
			return nil, fmt.Errorf("scan document number: %w", err)
		}
		numbers = append(numbers, docNo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate document numbers: %w", err)
	}
	return numbers, nil
}

// InsertDocumentInput 是落一条文档记录所需的字段。
type InsertDocumentInput struct {
	TenantID    int64
	DocType     string
	DocNo       string
	GeneratedAt time.Time
	GeneratedBy int64
	FileSize    int64
	Content     []byte
}

// InsertDocument 写入一条文档记录（含 PDF 字节）。
// doc_no 唯一索引冲突返回 ErrDocumentNoExists，由服务层换号重试。
func (s *Store) InsertDocument(ctx context.Context, input InsertDocumentInput) (Document, error) {
	item := Document{
		TenantID:    input.TenantID,
		DocType:     input.DocType,
		DocNo:       input.DocNo,
		GeneratedBy: input.GeneratedBy,
		FileSize:    input.FileSize,
	}
	err := s.pool.QueryRow(ctx, `
INSERT INTO tenant_documents (tenant_id, doc_type, doc_no, generated_at, generated_by, file_size, content)
VALUES ($1, $2, $3, $4, NULLIF($5, 0), $6, $7)
RETURNING id, generated_at`,
		input.TenantID, input.DocType, input.DocNo, input.GeneratedAt,
		input.GeneratedBy, input.FileSize, input.Content,
	).Scan(&item.ID, &item.GeneratedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "tenant_documents_doc_no_key" {
			return Document{}, ErrDocumentNoExists
		}
		return Document{}, fmt.Errorf("insert document: %w", err)
	}
	return item, nil
}

// ListDocumentsByTenant 返回机构下的全部文档记录（不含 PDF 正文），按生成时间倒序。
func (s *Store) ListDocumentsByTenant(ctx context.Context, tenantID int64) ([]Document, error) {
	rows, err := s.pool.Query(ctx, `SELECT`+documentColumns+documentFrom+`
WHERE d.tenant_id = $1
ORDER BY d.generated_at DESC, d.id DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()

	items := make([]Document, 0, 8)
	for rows.Next() {
		item, err := scanDocument(rows.Scan, false)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documents: %w", err)
	}
	return items, nil
}

// GetDocument 按 id 取一条文档记录（含 PDF 正文），不存在返回 ErrDocumentNotFound。
func (s *Store) GetDocument(ctx context.Context, docID int64) (Document, error) {
	row := s.pool.QueryRow(ctx, `SELECT`+documentColumns+`, d.content`+documentFrom+`
WHERE d.id = $1`, docID)
	item, err := scanDocument(row.Scan, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, err
	}
	return item, nil
}

// scanDocument 统一解析文档行；withContent 为 true 时多读一列 content。
func scanDocument(scan func(dest ...any) error, withContent bool) (Document, error) {
	var item Document
	dest := []any{
		&item.ID,
		&item.TenantID,
		&item.DocType,
		&item.DocNo,
		&item.GeneratedAt,
		&item.GeneratedBy,
		&item.GeneratedByName,
		&item.FileSize,
	}
	if withContent {
		dest = append(dest, &item.Content)
	}
	if err := scan(dest...); err != nil {
		return Document{}, fmt.Errorf("scan document: %w", err)
	}
	return item, nil
}

// GetOperatorName 取运营账号的展示名（姓名优先，其次账号）。
//
// 不复用 GetAdminDisplayName：后者按 real_name 取名，而部分环境的 operations_admins
// 只有 name 列（本地测试库就是这种结构），查不到会一直退回 admin#id。
func (s *Store) GetOperatorName(ctx context.Context, adminID int64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
SELECT COALESCE(NULLIF(name, ''), username, '')
FROM operations_admins
WHERE id = $1`, adminID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get operator name: %w", err)
	}
	return name, nil
}

// GetTenantSalesPhone 返回机构当前归属销售的姓名与手机号（没有归属时返回空串）。
func (s *Store) GetTenantSalesPhone(ctx context.Context, tenantID int64) (string, string, error) {
	var name, phone string
	err := s.pool.QueryRow(ctx, `
SELECT COALESCE(a.name, ''), COALESCE(a.phone, '')
FROM tenant_ownerships ow
JOIN operations_admins a ON a.id = ow.sales_admin_id AND a.deleted_at IS NULL
WHERE ow.tenant_id = $1
  AND ow.effective_to IS NULL
LIMIT 1`, tenantID).Scan(&name, &phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("get tenant sales: %w", err)
	}
	return name, phone, nil
}

// ListProductNames 按产品编码取中文名（取最新一条目录记录）。
func (s *Store) ListProductNames(ctx context.Context, codes []string) (map[string]string, error) {
	names := make(map[string]string, len(codes))
	if len(codes) == 0 {
		return names, nil
	}
	rows, err := s.pool.Query(ctx, `
SELECT DISTINCT ON (product_code) product_code, product_name
FROM product_catalog
WHERE product_code = ANY($1)
ORDER BY product_code, id DESC`, codes)
	if err != nil {
		return nil, fmt.Errorf("list product names: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var code, name string
		if err := rows.Scan(&code, &name); err != nil {
			return nil, fmt.Errorf("scan product name: %w", err)
		}
		names[normalizeEnum(code)] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate product names: %w", err)
	}
	return names, nil
}
