package recording

import (
	"context"
	"fmt"
	"log"
	"time"
)

// ===== Lingce Sales Service Layer =====

// CreateLingceSalesProspect creates a new sales prospect.
func (s *Service) CreateLingceSalesProspect(ctx context.Context, tenantID int64, req CreateLingceSalesProspectRequest) (*LingceSalesProspect, error) {
	prospect := &LingceSalesProspect{
		TenantID:        tenantID,
		InstitutionName: req.InstitutionName,
		InstitutionType: req.InstitutionType,
		InstitutionScale: req.InstitutionScale,
		Region:          req.Region,
		ContactName:     req.ContactName,
		ContactRole:     req.ContactRole,
		ContactPhone:    req.ContactPhone,
		ContactWechat:   req.ContactWechat,
		DecisionStage:   DecisionStage(req.DecisionStage),
		DealProbability: req.DealProbability,
		Source:          req.Source,
		AssignedTo:      req.AssignedTo,
		NextFollowUpAt:  req.NextFollowUpAt,
		Status:          ProspectStatusActive,
		Notes:           req.Notes,
	}

	id, err := s.store.CreateLingceSalesProspect(ctx, prospect)
	if err != nil {
		return nil, fmt.Errorf("create prospect: %w", err)
	}

	prospect.ID = id
	return prospect, nil
}

// GetLingceSalesProspect retrieves a prospect by ID.
func (s *Service) GetLingceSalesProspect(ctx context.Context, tenantID, prospectID int64) (*LingceSalesProspect, error) {
	return s.store.GetLingceSalesProspect(ctx, tenantID, prospectID)
}
// ListLingceSalesProspects lists prospects with filters.
func (s *Service) ListLingceSalesProspects(ctx context.Context, req LingceSalesProspectListRequest) ([]*LingceSalesProspect, int, error) {
	return s.store.ListLingceSalesProspects(ctx, req)
}

// UpdateLingceSalesProspect updates a prospect.
func (s *Service) UpdateLingceSalesProspect(ctx context.Context, tenantID, prospectID int64, req UpdateLingceSalesProspectRequest) error {
	// Verify prospect exists
	_, err := s.store.GetLingceSalesProspect(ctx, tenantID, prospectID)
	if err != nil {
		return fmt.Errorf("prospect not found: %w", err)
	}
	return s.store.UpdateLingceSalesProspect(ctx, tenantID, prospectID, req)
}

// ConfirmStageChange confirms a stage change for a prospect.
func (s *Service) ConfirmStageChange(ctx context.Context, tenantID, prospectID int64, recordingID *int64, userID int64, req ConfirmStageChangeRequest) (*LingceSalesProspectStageChange, error) {
	prospect, err := s.store.GetLingceSalesProspect(ctx, tenantID, prospectID)
	if err != nil {
		return nil, fmt.Errorf("prospect not found: %w", err)
	}

	fromStage := prospect.DecisionStage
	toStage := DecisionStage(req.ToStage)

	// Update prospect stage
	if err := s.store.UpdateProspectStage(ctx, tenantID, prospectID, toStage); err != nil {
		return nil, fmt.Errorf("update stage: %w", err)
	}

	// Record stage change
	sc := &LingceSalesProspectStageChange{
		ProspectID:  prospectID,
		RecordingID: recordingID,
		FromStage:   fromStage,
		ToStage:     toStage,
		ChangeType:  StageChangeAISuggested,
		Reason:      req.Reason,
		ChangedBy:   &userID,
	}

	id, err := s.store.CreateStageChange(ctx, sc)
	if err != nil {
		return nil, fmt.Errorf("create stage change: %w", err)
	}
	sc.ID = id

	// If stage is won, update won_at
	if toStage == StageWon {
		now := time.Now()
		wonStatus := string(ProspectStatusWon)
		if err := s.store.UpdateLingceSalesProspect(ctx, tenantID, prospectID, UpdateLingceSalesProspectRequest{
			Status: &wonStatus,
		}); err != nil {
			log.Printf("failed to update prospect status to won: %v", err)
		}
		_ = now
	}

	// If stage is lost, update status
	if toStage == StageLost {
		lostStatus := string(ProspectStatusLost)
		if err := s.store.UpdateLingceSalesProspect(ctx, tenantID, prospectID, UpdateLingceSalesProspectRequest{
			Status: &lostStatus,
		}); err != nil {
			log.Printf("failed to update prospect status to lost: %v", err)
		}
	}

	// If stage is dormant, update status
	if toStage == StageDormant {
		dormantStatus := string(ProspectStatusDormant)
		if err := s.store.UpdateLingceSalesProspect(ctx, tenantID, prospectID, UpdateLingceSalesProspectRequest{
			Status: &dormantStatus,
		}); err != nil {
			log.Printf("failed to update prospect status to dormant: %v", err)
		}
	}

	return sc, nil
}

// GetProspectStageHistory retrieves stage change history.
func (s *Service) GetProspectStageHistory(ctx context.Context, prospectID int64) ([]*LingceSalesProspectStageChange, error) {
	return s.store.ListStageChanges(ctx, prospectID)
}

// AssociateRecordingToProspect associates a recording with a prospect.
func (s *Service) AssociateRecordingToProspect(ctx context.Context, tenantID, recordingID int64, req AssociateRecordingToProspectRequest) (int64, error) {
	// Verify prospect exists
	_, err := s.store.GetLingceSalesProspect(ctx, tenantID, req.ProspectID)
	if err != nil {
		return 0, fmt.Errorf("prospect not found: %w", err)
	}

	rec := &LingceSalesProspectRecording{
		ProspectID:          req.ProspectID,
		RecordingID:         recordingID,
		ConversationType:    ConversationType(req.ConversationType),
		ConversationPurpose: ConversationPurpose(req.ConversationPurpose),
	}

	return s.store.AssociateRecordingToProspect(ctx, rec)
}

// GetProspectRecordings retrieves recordings associated with a prospect.
func (s *Service) GetProspectRecordings(ctx context.Context, prospectID int64) ([]*LingceSalesProspectRecording, error) {
	return s.store.ListProspectRecordings(ctx, prospectID)
}

// ProspectToResponse converts a prospect model to a response DTO.
func ProspectToResponse(p *LingceSalesProspect) LingceSalesProspectResponse {
	resp := LingceSalesProspectResponse{
		ID:                 p.ID,
		TenantID:           p.TenantID,
		InstitutionName:    p.InstitutionName,
		InstitutionType:    p.InstitutionType,
		InstitutionScale:   p.InstitutionScale,
		Region:             p.Region,
		ContactName:        p.ContactName,
		ContactRole:        p.ContactRole,
		ContactPhone:       p.ContactPhone,
		ContactWechat:      p.ContactWechat,
		PainPoints:         p.PainPoints,
		DecisionStage:      string(p.DecisionStage),
		DealProbability:    p.DealProbability,
		BudgetSignal:       p.BudgetSignal,
		CompetitorMentions: p.CompetitorMentions,
		DecisionChain:      p.DecisionChain,
		InternalSupporters: p.InternalSupporters,
		InternalBlockers:   p.InternalBlockers,
		Source:             p.Source,
		AssignedTo:         p.AssignedTo,
		NextAction:         p.NextAction,
		Status:             string(p.Status),
		LostReason:         p.LostReason,
		Notes:              p.Notes,
		CreatedAt:          p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:          p.UpdatedAt.Format(time.RFC3339),
	}
	if p.NextFollowUpAt != nil {
		t := p.NextFollowUpAt.Format(time.RFC3339)
		resp.NextFollowUpAt = &t
	}
	if p.WonAt != nil {
		t := p.WonAt.Format(time.RFC3339)
		resp.WonAt = &t
	}
	return resp
}

// StageChangeToResponse converts a stage change model to a response DTO.
func StageChangeToResponse(sc *LingceSalesProspectStageChange) LingceSalesStageChangeResponse {
	return LingceSalesStageChangeResponse{
		ID:          sc.ID,
		ProspectID:  sc.ProspectID,
		RecordingID: sc.RecordingID,
		FromStage:   string(sc.FromStage),
		ToStage:     string(sc.ToStage),
		ChangeType:  string(sc.ChangeType),
		Reason:      sc.Reason,
		ChangedBy:   sc.ChangedBy,
		CreatedAt:   sc.CreatedAt.Format(time.RFC3339),
	}
}
