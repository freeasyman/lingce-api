package product

import (
	"context"
	"fmt"
	"strings"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

func (s *Service) List(ctx context.Context, req ListRequest) ([]*Product, int, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}
	return s.store.List(ctx, req)
}

func (s *Service) GetByID(ctx context.Context, id int64) (*Product, error) {
	return s.store.GetByID(ctx, id)
}

func (s *Service) GetByIDIncludingDeleted(ctx context.Context, id int64) (*Product, error) {
	return s.store.GetByIDIncludingDeleted(ctx, id)
}

func (s *Service) Catalog(ctx context.Context, tenantID int64, page, pageSize int) ([]*CatalogItem, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.store.Catalog(ctx, tenantID, page, pageSize)
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Product, error) {
	if err := validateCreate(req); err != nil {
		return nil, err
	}
	if req.Status == "" {
		req.Status = StatusActive
	}
	return s.store.Create(ctx, normalizeCreate(req))
}

func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (*Product, error) {
	if err := validateUpdate(req); err != nil {
		return nil, err
	}
	return s.store.Update(ctx, id, normalizeUpdate(req))
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.store.Delete(ctx, id)
}

func validateCreate(req CreateRequest) error {
	if req.TenantID < 0 {
		return fmt.Errorf("tenant_id is invalid")
	}
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if req.Price < 0 {
		return fmt.Errorf("price must be >= 0")
	}
	if !isValidStatus(req.Status) {
		return fmt.Errorf("invalid status: %s", req.Status)
	}
	if err := validateMatchFields(req.AgeMin, req.AgeMax, req.Gender, req.ReviewStatus); err != nil {
		return err
	}
	return nil
}

func validateUpdate(req UpdateRequest) error {
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if req.Price != nil && *req.Price < 0 {
		return fmt.Errorf("price must be >= 0")
	}
	if req.Status != nil && !isValidStatus(*req.Status) {
		return fmt.Errorf("invalid status: %s", *req.Status)
	}
	gender := ""
	if req.Gender != nil {
		gender = *req.Gender
	}
	reviewStatus := ""
	if req.ReviewStatus != nil {
		reviewStatus = *req.ReviewStatus
	}
	if err := validateMatchFields(req.AgeMin, req.AgeMax, gender, reviewStatus); err != nil {
		return err
	}
	return nil
}

func validateMatchFields(ageMin, ageMax *int, gender string, reviewStatus string) error {
	if ageMin != nil && *ageMin < 0 {
		return fmt.Errorf("age_min must be >= 0")
	}
	if ageMax != nil && *ageMax < 0 {
		return fmt.Errorf("age_max must be >= 0")
	}
	if ageMin != nil && ageMax != nil && *ageMin > *ageMax {
		return fmt.Errorf("age_min must be <= age_max")
	}
	if gender != "" && !isValidGender(gender) {
		return fmt.Errorf("invalid gender: %s", gender)
	}
	if reviewStatus != "" && !isValidReviewStatus(reviewStatus) {
		return fmt.Errorf("invalid review_status: %s", reviewStatus)
	}
	return nil
}

func normalizeCreate(req CreateRequest) CreateRequest {
	req.Industry = strings.TrimSpace(req.Industry)
	req.Name = strings.TrimSpace(req.Name)
	req.Applicable = strings.TrimSpace(req.Applicable)
	req.SellingPoint = strings.TrimSpace(req.SellingPoint)
	req.UpgradeTo = strings.TrimSpace(req.UpgradeTo)
	req.CombineWith = strings.TrimSpace(req.CombineWith)
	req.Status = strings.TrimSpace(req.Status)
	req.Aliases = normalizeAliases(req.Aliases)
	req.ApplicableKeywords = normalizeAliases(req.ApplicableKeywords)
	req.Gender = strings.TrimSpace(req.Gender)
	req.Contraindications = strings.TrimSpace(req.Contraindications)
	req.ReviewStatus = strings.TrimSpace(req.ReviewStatus)
	if req.Gender == "" {
		req.Gender = "all"
	}
	if req.ReviewStatus == "" {
		req.ReviewStatus = "approved"
	}
	return req
}

func normalizeUpdate(req UpdateRequest) UpdateRequest {
	if req.Industry != nil {
		value := strings.TrimSpace(*req.Industry)
		req.Industry = &value
	}
	if req.Name != nil {
		value := strings.TrimSpace(*req.Name)
		req.Name = &value
	}
	if req.Applicable != nil {
		value := strings.TrimSpace(*req.Applicable)
		req.Applicable = &value
	}
	if req.SellingPoint != nil {
		value := strings.TrimSpace(*req.SellingPoint)
		req.SellingPoint = &value
	}
	if req.UpgradeTo != nil {
		value := strings.TrimSpace(*req.UpgradeTo)
		req.UpgradeTo = &value
	}
	if req.CombineWith != nil {
		value := strings.TrimSpace(*req.CombineWith)
		req.CombineWith = &value
	}
	if req.Status != nil {
		value := strings.TrimSpace(*req.Status)
		req.Status = &value
	}
	if req.Aliases != nil {
		aliases := normalizeAliases(*req.Aliases)
		req.Aliases = &aliases
	}
	if req.ApplicableKeywords != nil {
		keywords := normalizeAliases(*req.ApplicableKeywords)
		req.ApplicableKeywords = &keywords
	}
	if req.Gender != nil {
		value := strings.TrimSpace(*req.Gender)
		if value == "" {
			value = "all"
		}
		req.Gender = &value
	}
	if req.Contraindications != nil {
		value := strings.TrimSpace(*req.Contraindications)
		req.Contraindications = &value
	}
	if req.ReviewStatus != nil {
		value := strings.TrimSpace(*req.ReviewStatus)
		req.ReviewStatus = &value
	}
	return req
}

func normalizeAliases(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, item := range values {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func isValidStatus(status string) bool {
	switch status {
	case StatusActive, StatusInactive:
		return true
	default:
		return false
	}
}

func isValidGender(gender string) bool {
	switch gender {
	case "all", "male", "female":
		return true
	default:
		return false
	}
}

func isValidReviewStatus(status string) bool {
	switch status {
	case "draft", "approved", "disabled":
		return true
	default:
		return false
	}
}
