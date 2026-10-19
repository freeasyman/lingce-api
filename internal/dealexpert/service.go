package dealexpert

import (
	"context"
	"fmt"
	"strings"
)

type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }

func (s *Service) Receive(ctx context.Context, req ReceiveRequest) (*Analysis, bool, error) {
	if req.TenantID <= 0 || req.RecordingID <= 0 || req.EncounterID <= 0 || req.EmployeeID <= 0 {
		return nil, false, fmt.Errorf("tenant_id, recording_id, encounter_id and employee_id are required")
	}
	if strings.TrimSpace(req.CleanedTranscript) == "" {
		return nil, false, fmt.Errorf("cleaned_transcript is required")
	}
	if strings.TrimSpace(req.AnalysisPipelineVersion) == "" {
		return nil, false, fmt.Errorf("analysis_pipeline_version is required")
	}
	if strings.TrimSpace(req.PromptVersion) == "" {
		return nil, false, fmt.Errorf("prompt_version is required")
	}
	if strings.TrimSpace(req.BusinessScope) == "" {
		req.BusinessScope = "consultant"
	}
	if strings.TrimSpace(req.RoleCode) == "" {
		req.RoleCode = "consultant"
	}
	return s.store.Receive(ctx, req)
}
