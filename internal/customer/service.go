package customer

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

// Customer Services

// ListCustomers retrieves a paginated list of customers
func (s *Service) ListCustomers(ctx context.Context, req CustomerListRequest) ([]*CustomerResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	customers, total, err := s.store.ListCustomers(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*CustomerResponse, len(customers))
	for i, c := range customers {
		responses[i] = toCustomerResponse(c)
	}

	return responses, total, nil
}

// GetCustomerStats retrieves customer statistics
func (s *Service) GetCustomerStats(ctx context.Context, tenantID *int64) (*CustomerStatsResponse, error) {
	return s.store.GetCustomerStats(ctx, tenantID)
}

// GetCustomerByID retrieves a customer by ID
func (s *Service) GetCustomerByID(ctx context.Context, id int64) (*CustomerResponse, error) {
	customer, err := s.store.GetCustomerByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toCustomerResponse(customer), nil
}

// CreateCustomer creates a new customer
func (s *Service) CreateCustomer(ctx context.Context, tenantID, createdBy int64, req CreateCustomerRequest) (*CustomerResponse, error) {
	// Validate request
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Phone != nil {
		trimmed := strings.TrimSpace(*req.Phone)
		if trimmed == "" {
			req.Phone = nil
		} else {
			req.Phone = &trimmed
		}
	}
	if req.Email != nil {
		trimmed := strings.ToLower(strings.TrimSpace(*req.Email))
		if trimmed == "" {
			req.Email = nil
		} else {
			req.Email = &trimmed
		}
	}
	if req.Notes != nil {
		trimmed := strings.TrimSpace(*req.Notes)
		if trimmed == "" {
			req.Notes = nil
		} else {
			req.Notes = &trimmed
		}
	}
	if req.Phone != nil || req.Email != nil {
		existing, err := s.store.FindDuplicateCustomers(ctx, &tenantID, req.Phone, req.Email)
		if err != nil {
			return nil, err
		}
		if len(existing) > 0 {
			return toCustomerResponse(existing[0]), nil
		}
	}

	customer, err := s.store.CreateCustomer(ctx, tenantID, createdBy, req)
	if err != nil {
		return nil, err
	}

	return toCustomerResponse(customer), nil
}

// UpdateCustomer updates a customer
func (s *Service) UpdateCustomer(ctx context.Context, id int64, req UpdateCustomerRequest) (*CustomerResponse, error) {
	customer, err := s.store.UpdateCustomer(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toCustomerResponse(customer), nil
}

// DeleteCustomer deletes a customer.
func (s *Service) DeleteCustomer(ctx context.Context, id int64) error {
	return s.store.DeleteCustomer(ctx, id)
}

// MarkCustomerConverted marks a customer as converted
func (s *Service) MarkCustomerConverted(ctx context.Context, id int64) error {
	return s.store.MarkCustomerConverted(ctx, id)
}

// AddCustomerIdentity adds a customer identity
func (s *Service) AddCustomerIdentity(ctx context.Context, customerID int64, req AddIdentityRequest) (*CustomerIdentity, error) {
	// Validate request
	if req.Channel == "" {
		return nil, fmt.Errorf("channel is required")
	}
	if req.ChannelID == "" {
		req.ChannelID = fmt.Sprintf("%s:%d", strings.ToLower(strings.TrimSpace(req.Channel)), customerID)
	}

	return s.store.AddCustomerIdentity(ctx, customerID, req)
}

// ListCustomerInteractions retrieves a paginated list of customer interactions
func (s *Service) ListCustomerInteractions(ctx context.Context, req InteractionListRequest) ([]*InteractionResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	interactions, total, err := s.store.ListCustomerInteractions(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*InteractionResponse, len(interactions))
	for i, ci := range interactions {
		responses[i] = toInteractionResponse(ci)
	}

	return responses, total, nil
}

// CreateCustomerInteraction creates a customer interaction
func (s *Service) CreateCustomerInteraction(ctx context.Context, customerID, tenantID, employeeID int64, req CreateInteractionRequest) (*InteractionResponse, error) {
	// Validate request
	if req.Type == "" {
		return nil, fmt.Errorf("type is required")
	}
	if req.Direction == "" {
		return nil, fmt.Errorf("direction is required")
	}

	interaction, err := s.store.CreateCustomerInteraction(ctx, customerID, tenantID, employeeID, req)
	if err != nil {
		return nil, err
	}

	return toInteractionResponse(interaction), nil
}

// ListCustomerFollowUps retrieves a paginated list of customer follow-ups
func (s *Service) ListCustomerFollowUps(ctx context.Context, req FollowUpListRequest) ([]*FollowUpResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	followUps, total, err := s.store.ListCustomerFollowUps(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*FollowUpResponse, len(followUps))
	for i, cf := range followUps {
		responses[i] = toFollowUpResponse(cf)
	}

	return responses, total, nil
}

// CreateCustomerFollowUp creates a customer follow-up
func (s *Service) CreateCustomerFollowUp(ctx context.Context, customerID, tenantID, employeeID int64, req CreateFollowUpRequest) (*FollowUpResponse, error) {
	// Validate request
	if req.Type == "" {
		return nil, fmt.Errorf("type is required")
	}
	if req.Content == "" {
		return nil, fmt.Errorf("content is required")
	}

	followUp, err := s.store.CreateCustomerFollowUp(ctx, customerID, tenantID, employeeID, req)
	if err != nil {
		return nil, err
	}

	return toFollowUpResponse(followUp), nil
}

// GetCustomerMembership retrieves customer membership information
func (s *Service) GetCustomerMembership(ctx context.Context, customerID int64) (*CustomerMembership, error) {
	return s.store.GetCustomerMembership(ctx, customerID)
}

// Tag Services

// ListCustomerTags retrieves a paginated list of customer tags
func (s *Service) ListCustomerTags(ctx context.Context, req TagListRequest) ([]*TagResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	tags, total, err := s.store.ListCustomerTags(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*TagResponse, len(tags))
	for i, ct := range tags {
		responses[i] = toTagResponse(ct)
	}

	return responses, total, nil
}

// GetCustomerTagByID retrieves a customer tag by ID
func (s *Service) GetCustomerTagByID(ctx context.Context, id int64) (*TagResponse, error) {
	tag, err := s.store.GetCustomerTagByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toTagResponse(tag), nil
}

// CreateCustomerTag creates a new customer tag
func (s *Service) CreateCustomerTag(ctx context.Context, tenantID, createdBy int64, req CreateTagRequest) (*TagResponse, error) {
	// Validate request
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	tag, err := s.store.CreateCustomerTag(ctx, tenantID, createdBy, req)
	if err != nil {
		return nil, err
	}

	return toTagResponse(tag), nil
}

// UpdateCustomerTag updates a customer tag
func (s *Service) UpdateCustomerTag(ctx context.Context, id int64, req UpdateTagRequest) (*TagResponse, error) {
	tag, err := s.store.UpdateCustomerTag(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toTagResponse(tag), nil
}

// DeleteCustomerTag deletes a customer tag
func (s *Service) DeleteCustomerTag(ctx context.Context, id int64) error {
	return s.store.DeleteCustomerTag(ctx, id)
}

// Group Services

// ListCustomerGroups retrieves a paginated list of customer groups
func (s *Service) ListCustomerGroups(ctx context.Context, req GroupListRequest) ([]*GroupResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	groups, total, err := s.store.ListCustomerGroups(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*GroupResponse, len(groups))
	for i, cg := range groups {
		responses[i] = toGroupResponse(cg)
	}

	return responses, total, nil
}

// GetCustomerGroupByID retrieves a customer group by ID
func (s *Service) GetCustomerGroupByID(ctx context.Context, id int64) (*GroupResponse, error) {
	group, err := s.store.GetCustomerGroupByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toGroupResponse(group), nil
}

// CreateCustomerGroup creates a new customer group
func (s *Service) CreateCustomerGroup(ctx context.Context, tenantID, createdBy int64, req CreateGroupRequest) (*GroupResponse, error) {
	// Validate request
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Type == "" {
		return nil, fmt.Errorf("type is required")
	}
	if req.Type != "static" && req.Type != "dynamic" {
		return nil, fmt.Errorf("type must be 'static' or 'dynamic'")
	}

	group, err := s.store.CreateCustomerGroup(ctx, tenantID, createdBy, req)
	if err != nil {
		return nil, err
	}

	return toGroupResponse(group), nil
}

// UpdateCustomerGroup updates a customer group
func (s *Service) UpdateCustomerGroup(ctx context.Context, id int64, req UpdateGroupRequest) (*GroupResponse, error) {
	group, err := s.store.UpdateCustomerGroup(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toGroupResponse(group), nil
}

// DeleteCustomerGroup deletes a customer group
func (s *Service) DeleteCustomerGroup(ctx context.Context, id int64) error {
	return s.store.DeleteCustomerGroup(ctx, id)
}

// Advanced Customer Services

func (s *Service) GetCustomerMomentumHistory(ctx context.Context, customerID int64) ([]MomentumHistory, error) {
	return s.store.GetCustomerMomentumHistory(ctx, customerID, 30)
}

func (s *Service) FindDuplicateCustomers(ctx context.Context, tenantID *int64, phone, email *string) ([]*CustomerResponse, error) {
	customers, err := s.store.FindDuplicateCustomers(ctx, tenantID, phone, email)
	if err != nil {
		return nil, err
	}
	responses := make([]*CustomerResponse, len(customers))
	for i, c := range customers {
		responses[i] = toCustomerResponse(c)
	}
	return responses, nil
}

func (s *Service) MergeCustomers(ctx context.Context, targetID int64, sourceIDs []int64, operatorID int64) (int64, error) {
	return s.store.MergeCustomers(ctx, targetID, sourceIDs, operatorID)
}

func (s *Service) ListConsultationRecords(ctx context.Context, customerID int64, page, pageSize int) ([]map[string]interface{}, int, error) {
	return s.store.ListConsultationRecords(ctx, customerID, page, pageSize)
}

func (s *Service) ListEMRRecords(ctx context.Context, customerID int64, page, pageSize int) ([]map[string]interface{}, int, error) {
	return s.store.ListEMRRecords(ctx, customerID, page, pageSize)
}

func (s *Service) BatchTagCustomers(ctx context.Context, tenantID *int64, req BatchTagRequest) (int64, error) {
	return s.store.BatchTagCustomers(ctx, tenantID, req)
}

func (s *Service) GetTagStats(ctx context.Context, tenantID *int64) (map[string]interface{}, error) {
	return s.store.GetTagStats(ctx, tenantID)
}

func (s *Service) ListGroupMembers(ctx context.Context, groupID int64, page, pageSize int) ([]*CustomerResponse, int, error) {
	members, total, err := s.store.ListGroupMembers(ctx, groupID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	responses := make([]*CustomerResponse, len(members))
	for i, m := range members {
		responses[i] = toCustomerResponse(m)
	}
	return responses, total, nil
}

func (s *Service) AddGroupMembers(ctx context.Context, groupID int64, customerIDs []int64) (int64, error) {
	return s.store.AddGroupMembers(ctx, groupID, customerIDs)
}

func (s *Service) RemoveGroupMembers(ctx context.Context, groupID int64, customerIDs []int64) (int64, error) {
	return s.store.RemoveGroupMembers(ctx, groupID, customerIDs)
}

func (s *Service) PreviewGroupRules(ctx context.Context, tenantID *int64, req RulePreviewRequest) (*RulePreviewResponse, error) {
	return s.store.PreviewGroupRules(ctx, tenantID, req.Rules)
}

func (s *Service) ValidateGroupRules(req RuleValidateRequest) *RuleValidateResponse {
	_, _, errs := parseRuleConditions(req.Rules)
	if len(errs) > 0 {
		return &RuleValidateResponse{
			IsValid: false,
			Errors:  errs,
		}
	}
	return &RuleValidateResponse{
		IsValid: true,
		Errors:  []string{},
	}
}

// Helper functions

// toCustomerResponse converts a Customer to CustomerResponse
func toCustomerResponse(c *Customer) *CustomerResponse {
	resp := &CustomerResponse{
		ID:                   c.ID,
		TenantID:             c.TenantID,
		Name:                 c.Name,
		Phone:                c.Phone,
		Email:                c.Email,
		Gender:               c.Gender,
		Age:                  c.Age,
		Source:               c.Source,
		Status:               c.Status,
		Momentum:             c.Momentum,
		AssignedTo:           c.AssignedTo,
		LifecycleStage:       c.LifecycleStage,
		ValueScore:           c.ValueScore,
		FirstChannel:         c.FirstChannel,
		IdentityCount:        c.IdentityCount,
		TotalInteractions:    c.TotalInteractions,
		DealCount:            c.DealCount,
		TotalConvertedAmount: c.TotalConvertedAmount,
		Notes:                c.Notes,
		ExtraData:            c.ExtraData,
		CreatedAt:            c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:            c.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if c.AssignedAt != nil {
		formatted := c.AssignedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.AssignedAt = &formatted
	}

	if c.ConvertedAt != nil {
		formatted := c.ConvertedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ConvertedAt = &formatted
	}

	if c.LastContactedAt != nil {
		formatted := c.LastContactedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.LastContactedAt = &formatted
	}

	if c.NextFollowUpAt != nil {
		formatted := c.NextFollowUpAt.Format("2006-01-02T15:04:05Z07:00")
		resp.NextFollowUpAt = &formatted
	}
	if c.LastInteractionAt != nil {
		formatted := c.LastInteractionAt.Format("2006-01-02T15:04:05Z07:00")
		resp.LastInteractionAt = &formatted
	}

	return resp
}

// toInteractionResponse converts a CustomerInteraction to InteractionResponse
func toInteractionResponse(ci *CustomerInteraction) *InteractionResponse {
	return &InteractionResponse{
		ID:           ci.ID,
		CustomerID:   ci.CustomerID,
		Type:         ci.Type,
		Direction:    ci.Direction,
		Content:      ci.Content,
		Duration:     ci.Duration,
		RecordingID:  ci.RecordingID,
		EmployeeID:   ci.EmployeeID,
		EmployeeName: "", // TODO: Join with employees table
		InteractedAt: ci.InteractedAt.Format("2006-01-02T15:04:05Z07:00"),
		CreatedAt:    ci.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// toFollowUpResponse converts a CustomerFollowUp to FollowUpResponse
func toFollowUpResponse(cf *CustomerFollowUp) *FollowUpResponse {
	resp := &FollowUpResponse{
		ID:           cf.ID,
		CustomerID:   cf.CustomerID,
		Type:         cf.Type,
		Status:       cf.Status,
		Content:      cf.Content,
		EmployeeID:   cf.EmployeeID,
		EmployeeName: "", // TODO: Join with employees table
		CreatedAt:    cf.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:    cf.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if cf.ScheduledAt != nil {
		formatted := cf.ScheduledAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ScheduledAt = &formatted
	}

	if cf.CompletedAt != nil {
		formatted := cf.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.CompletedAt = &formatted
	}

	return resp
}

// toTagResponse converts a CustomerTag to TagResponse
func toTagResponse(ct *CustomerTag) *TagResponse {
	return &TagResponse{
		ID:            ct.ID,
		TenantID:      ct.TenantID,
		Name:          ct.Name,
		Color:         ct.Color,
		Description:   ct.Description,
		CustomerCount: 0, // TODO: Count from customer_tag_assignments
		CreatedAt:     ct.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     ct.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// toGroupResponse converts a CustomerGroup to GroupResponse
func toGroupResponse(cg *CustomerGroup) *GroupResponse {
	return &GroupResponse{
		ID:          cg.ID,
		TenantID:    cg.TenantID,
		Name:        cg.Name,
		Description: cg.Description,
		Type:        cg.Type,
		Rules:       cg.Rules,
		MemberCount: cg.MemberCount,
		CreatedAt:   cg.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   cg.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
