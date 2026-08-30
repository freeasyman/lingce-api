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

	resp := toCustomerResponse(customer)
	if resp == nil {
		return nil, fmt.Errorf("customer not found")
	}

	if customer.AssignedTo != nil && *customer.AssignedTo > 0 {
		var assignedName string
		if err := s.store.pool.QueryRow(ctx, `
			SELECT COALESCE(NULLIF(full_name, ''), NULLIF(name, ''), phone, '')
			FROM employees
			WHERE id = $1
		`, *customer.AssignedTo).Scan(&assignedName); err == nil && strings.TrimSpace(assignedName) != "" {
			resp.AssignedToName = &assignedName
		}
	}

	interactionReq := InteractionListRequest{
		CustomerID: id,
		Page:       1,
		PageSize:   20,
	}
	interactions, _, err := s.store.ListCustomerInteractions(ctx, interactionReq)
	if err == nil {
		employeeIDs := make([]int64, 0, len(interactions))
		seen := map[int64]struct{}{}
		for _, item := range interactions {
			if item != nil && item.EmployeeID > 0 {
				if _, ok := seen[item.EmployeeID]; !ok {
					seen[item.EmployeeID] = struct{}{}
					employeeIDs = append(employeeIDs, item.EmployeeID)
				}
			}
		}
		employeeNameMap := map[int64]string{}
		if len(employeeIDs) > 0 {
			rows, queryErr := s.store.pool.Query(ctx, `
				SELECT id, COALESCE(NULLIF(full_name, ''), NULLIF(name, ''), phone, '')
				FROM employees
				WHERE id = ANY($1)
			`, employeeIDs)
			if queryErr == nil {
				defer rows.Close()
				for rows.Next() {
					var employeeID int64
					var employeeName string
					if scanErr := rows.Scan(&employeeID, &employeeName); scanErr == nil {
						employeeNameMap[employeeID] = employeeName
					}
				}
			}
		}

		resp.RecentInteractions = make([]CustomerInteractionSummary, 0, len(interactions))
		for _, item := range interactions {
			if item == nil {
				continue
			}
			summary := CustomerInteractionSummary{
				ID:              item.ID,
				InteractionType: item.Type,
				Direction:       &item.Direction,
				ContentSummary:  item.Content,
				CreatedAt:       stringPtr(item.CreatedAt.Format("2006-01-02T15:04:05Z07:00")),
				OccurredAt:      stringPtr(item.InteractedAt.Format("2006-01-02T15:04:05Z07:00")),
			}
			if name, ok := employeeNameMap[item.EmployeeID]; ok && strings.TrimSpace(name) != "" {
				summary.StaffName = &name
			}
			resp.RecentInteractions = append(resp.RecentInteractions, summary)
		}
	}

	rows, err := s.store.pool.Query(ctx, `
		SELECT id,
		       COALESCE(NULLIF(channel, ''), '') AS channel_type,
		       NULLIF(channel_id, '') AS external_id,
		       NULLIF(nickname, '') AS external_name
		FROM customer_identities
		WHERE customer_id = $1
		ORDER BY id DESC
	`, id)
	if err == nil {
		defer rows.Close()
		resp.Identities = make([]CustomerIdentityResponse, 0, 8)
		for rows.Next() {
			var identity CustomerIdentityResponse
			if scanErr := rows.Scan(&identity.ID, &identity.ChannelType, &identity.ExternalID, &identity.ExternalName); scanErr == nil {
				identity.Status = "active"
				resp.Identities = append(resp.Identities, identity)
			}
		}
	}

	var (
		followType   string
		followStatus string
		scheduledAt  *string
		completedAt  *string
	)
	if err := s.store.pool.QueryRow(ctx, `
		SELECT COALESCE(type, ''), COALESCE(status, ''), 
		       CASE WHEN scheduled_at IS NOT NULL THEN to_char(scheduled_at, 'YYYY-MM-DD"T"HH24:MI:SSOF') ELSE NULL END,
		       CASE WHEN completed_at IS NOT NULL THEN to_char(completed_at, 'YYYY-MM-DD"T"HH24:MI:SSOF') ELSE NULL END
		FROM customer_follow_ups
		WHERE customer_id = $1
		ORDER BY COALESCE(scheduled_at, completed_at, created_at) DESC, id DESC
		LIMIT 1
	`, id).Scan(&followType, &followStatus, &scheduledAt, &completedAt); err == nil {
		parts := make([]string, 0, 3)
		if strings.TrimSpace(followType) != "" {
			parts = append(parts, followType)
		}
		if strings.TrimSpace(followStatus) != "" {
			parts = append(parts, followStatus)
		}
		if completedAt != nil && strings.TrimSpace(*completedAt) != "" {
			parts = append(parts, "已完成")
		} else if scheduledAt != nil && strings.TrimSpace(*scheduledAt) != "" {
			parts = append(parts, "待跟进")
		}
		if len(parts) > 0 {
			statusText := strings.Join(parts, " · ")
			resp.LatestFollowUpStatus = &statusText
		}
	}

	return resp, nil
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
		LastConsultationItem: c.LastConsultationItem,
		LastDealResult:       c.LastDealResult,
		Notes:                c.Notes,
		ExtraData:            c.ExtraData,
		CreatedAt:            c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:            c.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	resp.FirstContactAt = stringPtr(resp.CreatedAt)

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

func stringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
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
