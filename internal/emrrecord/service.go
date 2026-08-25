package emrrecord

import (
	"context"
	"fmt"

	"github.com/freeasyman/lingce-api/internal/emrrule"
	"github.com/freeasyman/lingce-api/internal/tenancy"
)

type Service struct {
	store       *Store
	ruleService *emrrule.Service
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

func (s *Service) SetRuleService(ruleService *emrrule.Service) {
	s.ruleService = ruleService
}

func (s *Service) ListRecords(ctx context.Context, req ListRecordsRequest) ([]*RecordListItem, int, error) {
	if err := tenancy.RequirePositiveID("tenant_id", req.TenantID); err != nil {
		return nil, 0, err
	}
	if err := tenancy.RequirePositiveID("employee_id", req.EmployeeID); err != nil {
		return nil, 0, err
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
	return s.store.ListRecords(ctx, req)
}

func (s *Service) ImportRecordingDrafts(ctx context.Context, tenantID, actorID int64, recordingIDs []int64) (*ImportRecordingDraftsResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("actor_id", actorID); err != nil {
		return nil, err
	}
	if len(recordingIDs) == 0 {
		return nil, fmt.Errorf("recording_ids is required")
	}
	recordIDs, err := s.store.ImportRecordingDrafts(ctx, tenantID, actorID, recordingIDs)
	if err != nil {
		return nil, err
	}
	return &ImportRecordingDraftsResponse{
		ImportedCount: int64(len(recordIDs)),
		RecordIDs:     recordIDs,
	}, nil
}

func (s *Service) GetRecord(ctx context.Context, tenantID, recordID int64) (*RecordDetailResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("record_id", recordID); err != nil {
		return nil, err
	}
	return s.store.GetRecord(ctx, tenantID, recordID)
}

func (s *Service) GetEncounter(ctx context.Context, tenantID, encounterID int64) (*EncounterDetailResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("encounter_id", encounterID); err != nil {
		return nil, err
	}
	return s.store.GetEncounter(ctx, tenantID, encounterID)
}

func (s *Service) SaveRecord(ctx context.Context, tenantID, recordID, actorID int64, actorType string, req SaveRecordRequest) (*RecordWriteResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("record_id", recordID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("actor_id", actorID); err != nil {
		return nil, err
	}
	resp, err := s.store.SaveRecord(ctx, tenantID, recordID, actorID, actorType, req)
	if err != nil || resp == nil {
		return resp, err
	}
	if s.ruleService != nil {
		_, err = s.ruleService.RunStageAndGetSummary(ctx, tenantID, recordID, actorID, "save", "save")
		if err != nil {
			return nil, err
		}
	}
	return resp, nil
}

func (s *Service) SubmitRecord(ctx context.Context, tenantID, recordID, actorID int64, actorType string, req SubmitRecordRequest) (*RecordWriteResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("record_id", recordID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("actor_id", actorID); err != nil {
		return nil, err
	}
	if s.ruleService != nil {
		summary, err := s.ruleService.CanSubmit(ctx, tenantID, recordID, actorID)
		if err != nil {
			return nil, err
		}
		if summary != nil && !summary.CanSubmit {
			return nil, fmt.Errorf("病历存在提交阻断规则，请处理后再提交")
		}
	}
	return s.store.SubmitRecord(ctx, tenantID, recordID, actorID, actorType, req)
}

func (s *Service) ArchiveRecord(ctx context.Context, tenantID, recordID, actorID int64, actorType string, req ArchiveRecordRequest) (*RecordWriteResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("record_id", recordID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("actor_id", actorID); err != nil {
		return nil, err
	}
	if s.ruleService != nil {
		summary, err := s.ruleService.CanArchive(ctx, tenantID, recordID, actorID)
		if err != nil {
			return nil, err
		}
		if summary != nil && !summary.CanArchive {
			return nil, fmt.Errorf("病历存在归档阻断规则，请处理后再归档")
		}
	}
	return s.store.ArchiveRecord(ctx, tenantID, recordID, actorID, actorType, req)
}

func (s *Service) ListVersions(ctx context.Context, tenantID, recordID int64) ([]*RecordVersionDTO, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("record_id", recordID); err != nil {
		return nil, err
	}
	return s.store.ListVersions(ctx, tenantID, recordID)
}

func (s *Service) ListAuditEvents(ctx context.Context, tenantID, recordID int64) ([]*RecordAuditEventDTO, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("record_id", recordID); err != nil {
		return nil, err
	}
	return s.store.ListAuditEvents(ctx, tenantID, recordID)
}
