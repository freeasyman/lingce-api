package billing

// 运营端「开通通知函 / 服务开通确认函」的生成、列表与下载。
//
// 生成流程：数据权限校验 → 机构必须已开通 → 取机构档案/额度/应收/归属销售 →
// 组装纯数据 DTO → 纯 Go 渲染 A4 单页 PDF → 编号与 PDF 字节一起落库。
// 每次生成都是一条新记录（旧记录保留），客户拿旧函来核对时能重新下载当初那一版。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	billingpdf "github.com/freeasyman/lingce-api/internal/billing/pdf"
)

// documentNoMaxAttempts 是编号撞号时的重试次数（并发生成同一前缀的概率极低）。
const documentNoMaxAttempts = 5

// DocumentService 是通知函文档的服务入口。
type DocumentService struct {
	institutions *InstitutionService
	store        *Store
	now          func() time.Time
	company      string
}

// NewDocumentService 装配文档服务；权限校验复用机构服务的同一套数据权限。
func NewDocumentService(institutions *InstitutionService, store *Store) *DocumentService {
	return &DocumentService{
		institutions: institutions,
		store:        store,
		now:          time.Now,
		company:      DefaultDocumentCompany,
	}
}

// clock 返回可替换的当前时间（单测固定时间）。
func (s *DocumentService) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// Generate 生成一份通知函并留档。
func (s *DocumentService) Generate(ctx context.Context, tenantID, adminID int64, req GenerateDocumentRequest) (*DocumentItem, error) {
	docType, err := documentTypeOfKind(req.Kind)
	if err != nil {
		return nil, err
	}

	institution, err := s.institutions.GetInstitutionForAdmin(ctx, tenantID, adminID)
	if err != nil {
		return nil, err
	}
	if !institution.Open {
		return nil, invalidf("机构尚未开通，无法生成通知函")
	}

	productCodes := documentProductCodes(institution.Products)
	productNames, err := s.store.ListProductNames(ctx, productCodes)
	if err != nil {
		return nil, err
	}
	salesName, salesPhone, err := s.store.GetTenantSalesPhone(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	now := s.clock()
	letter, err := buildDocumentLetter(documentLetterInput{
		Institution:  institution,
		DocType:      docType,
		DocNo:        "",
		Date:         now,
		ProductNames: productNames,
		SalesName:    salesName,
		SalesPhone:   salesPhone,
		Company:      s.company,
	})
	if err != nil {
		return nil, err
	}

	generatedByName, err := s.store.GetOperatorName(ctx, adminID)
	if err != nil || strings.TrimSpace(generatedByName) == "" {
		// 操作人名字取不到不影响文档本身，退回账号 id（前端显示 admin#123）。
		generatedByName = fmt.Sprintf("admin#%d", adminID)
	}

	prefix := documentNoPrefix(docType, now.Year())
	for attempt := 0; attempt < documentNoMaxAttempts; attempt++ {
		existing, err := s.store.ListDocumentNumbers(ctx, prefix)
		if err != nil {
			return nil, err
		}
		docNo := nextDocumentNo(prefix, existing)
		letter.DocNo = docNo

		content, err := billingpdf.Render(letter)
		if err != nil {
			return nil, fmt.Errorf("渲染通知函失败: %w", err)
		}

		doc, err := s.store.InsertDocument(ctx, InsertDocumentInput{
			TenantID:    tenantID,
			DocType:     docType,
			DocNo:       docNo,
			GeneratedAt: now,
			GeneratedBy: adminID,
			FileSize:    int64(len(content)),
			Content:     content,
		})
		if errors.Is(err, ErrDocumentNoExists) {
			continue
		}
		if err != nil {
			return nil, err
		}
		doc.GeneratedByName = generatedByName
		item := documentItemOf(doc)
		return &item, nil
	}
	return nil, fmt.Errorf("billing: 文档编号连续冲突，请稍后重试")
}

// List 返回机构下的全部通知函记录（新→旧）。
func (s *DocumentService) List(ctx context.Context, tenantID, adminID int64) (*DocumentListResult, error) {
	if err := s.institutions.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	documents, err := s.store.ListDocumentsByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	items := make([]DocumentItem, 0, len(documents))
	for _, doc := range documents {
		items = append(items, documentItemOf(doc))
	}
	return &DocumentListResult{Items: items, Total: int64(len(items)), IsMock: false}, nil
}

// Download 读取一份通知函的 PDF 字节，并校验运营账号对所属机构的数据权限。
func (s *DocumentService) Download(ctx context.Context, docID, adminID int64) (*Document, error) {
	doc, err := s.store.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if err := s.institutions.requireTenantAccess(ctx, doc.TenantID, adminID); err != nil {
		return nil, err
	}
	return &doc, nil
}

// documentFileName 返回下载文件名（含扩展名）。
func documentFileName(doc *Document) string {
	return documentFileBaseName(doc.DocType, doc.DocNo) + ".pdf"
}

// documentFallbackFileName 返回 ASCII 兜底文件名（老浏览器不支持 filename* 时使用）。
func documentFallbackFileName(doc *Document) string {
	return strings.TrimSpace(doc.DocNo) + ".pdf"
}
