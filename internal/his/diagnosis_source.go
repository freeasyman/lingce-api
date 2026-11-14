package his

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/freeasyman/lingce-api/internal/emrdiagnosis"
	"github.com/jackc/pgx/v5"
)

// 诊断来源标记：处方/检验/检查接口与打印统一使用。
const (
	diagnosisSourceEMR     = "emr_record"
	diagnosisSourceHisText = "his_fallback"
)

// encounterDiagnosis 是一次就诊的诊断解析结果。
// 唯一真值来自 emr_records.content_json；HIS 表历史诊断仅在无病历时回退。
type encounterDiagnosis struct {
	Text     string
	Source   string
	RecordID string
}

// resolveEncounterDiagnosis 按 encounter 查询最新 EMR 病历诊断。
// 病历不存在（或病历尚未填写诊断）时，回退到 legacy 文本并标记 his_fallback。
func (s *Store) resolveEncounterDiagnosis(ctx context.Context, q querier, tenantID, encounterID int64, legacy string) encounterDiagnosis {
	resolution := encounterDiagnosis{}
	legacy = strings.TrimSpace(legacy)

	if encounterID > 0 {
		var recordID string
		var raw []byte
		err := q.QueryRow(ctx, `
			SELECT id::text, content_json
			FROM emr_records
			WHERE tenant_id = $1 AND encounter_id = $2 AND voided_at IS NULL
			ORDER BY updated_at DESC NULLS LAST, created_at DESC
			LIMIT 1`, tenantID, encounterID).Scan(&recordID, &raw)
		switch {
		case err == nil:
			var content map[string]any
			if len(raw) > 0 {
				if jsonErr := json.Unmarshal(raw, &content); jsonErr != nil {
					slog.Warn("his: decode emr record diagnosis", "tenant_id", tenantID, "encounter_id", encounterID, "error", jsonErr)
				}
			}
			if text := emrdiagnosis.Display(content); text != "" {
				return encounterDiagnosis{Text: text, Source: diagnosisSourceEMR, RecordID: recordID}
			}
			resolution.RecordID = recordID
		case err != pgx.ErrNoRows:
			slog.Warn("his: load emr record diagnosis", "tenant_id", tenantID, "encounter_id", encounterID, "error", err)
		}
	}

	if legacy != "" {
		resolution.Text = legacy
		resolution.Source = diagnosisSourceHisText
	}
	return resolution
}

// latestHisDiagnosis 读取同一就诊最近一条 HIS 历史诊断（仅用于病历缺失时的回退）。
func (s *Store) latestHisDiagnosis(ctx context.Context, q querier, tenantID, encounterID int64) string {
	var legacy string
	err := q.QueryRow(ctx, `
		SELECT COALESCE(diagnosis, '')
		FROM his_prescriptions
		WHERE tenant_id = $1 AND encounter_id = $2 AND COALESCE(diagnosis, '') <> ''
		ORDER BY id DESC
		LIMIT 1`, tenantID, encounterID).Scan(&legacy)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(legacy)
}

// applyPrescriptionDiagnosis 用病历诊断覆盖处方返回体中的历史文本。
// 病历不存在时保留处方自身的 diagnosis 列并标注 his_fallback。
func (s *Store) applyPrescriptionDiagnosis(ctx context.Context, q querier, tenantID int64, p *Prescription) {
	if p == nil {
		return
	}
	resolution := s.resolveEncounterDiagnosis(ctx, q, tenantID, p.EncounterID, p.Diagnosis)
	if resolution.Text != "" {
		p.Diagnosis = resolution.Text
		p.DiagnosisSource = resolution.Source
		p.RecordID = resolution.RecordID
		return
	}
	if strings.TrimSpace(p.Diagnosis) != "" {
		p.DiagnosisSource = diagnosisSourceHisText
	}
}

// applyRequestDiagnosis 为检验/检查申请单填充病历诊断；无病历时回退到 HIS 历史处方诊断。
func (s *Store) applyRequestDiagnosis(ctx context.Context, q querier, tenantID, encounterID int64) (text, source string) {
	resolution := s.resolveEncounterDiagnosis(ctx, q, tenantID, encounterID, "")
	if resolution.Text != "" {
		return resolution.Text, resolution.Source
	}
	if legacy := s.latestHisDiagnosis(ctx, q, tenantID, encounterID); legacy != "" {
		return legacy, diagnosisSourceHisText
	}
	return "", ""
}

// applyRequestDiagnosisCached 为列表接口复用同一就诊的诊断解析结果。
func (s *Store) applyRequestDiagnosisCached(ctx context.Context, q querier, tenantID, encounterID int64, cache map[int64]encounterDiagnosis) (string, string) {
	if cache != nil {
		if cached, ok := cache[encounterID]; ok {
			return cached.Text, cached.Source
		}
	}
	text, source := s.applyRequestDiagnosis(ctx, q, tenantID, encounterID)
	if cache != nil {
		cache[encounterID] = encounterDiagnosis{Text: text, Source: source}
	}
	return text, source
}
