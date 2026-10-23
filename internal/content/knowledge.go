package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// 租户知识库 / 规则：判断 + 创作注入。

type orgKnowledge struct {
	InstitutionInfo json.RawMessage `json:"institution_info"`
	Doctors         json.RawMessage `json:"doctors"`
	Specialties     json.RawMessage `json:"specialties"`
	Data            json.RawMessage `json:"data"`
	HardRules       json.RawMessage `json:"hard_rules"`
	SoftPrefs       json.RawMessage `json:"soft_prefs"`
}

func (s *Store) getOrgKnowledge(ctx context.Context, tenantID int64) (*orgKnowledge, error) {
	var k orgKnowledge
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(institution_info,'{}'::jsonb), COALESCE(doctors,'[]'::jsonb), COALESCE(specialties,'[]'::jsonb),
		       COALESCE(data,'{}'::jsonb), COALESCE(hard_rules,'[]'::jsonb), COALESCE(soft_prefs,'[]'::jsonb)
		FROM content_org_knowledge WHERE tenant_id = $1`, tenantID).
		Scan(&k.InstitutionInfo, &k.Doctors, &k.Specialties, &k.Data, &k.HardRules, &k.SoftPrefs)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return &k, nil
		}
		return nil, err
	}
	return &k, nil
}

func (s *Store) upsertOrgKnowledge(ctx context.Context, tenantID int64, k *orgKnowledge) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO content_org_knowledge (tenant_id, institution_info, doctors, specialties, data, hard_rules, soft_prefs, updated_at)
		VALUES ($1, COALESCE($2::jsonb,'{}'::jsonb), COALESCE($3::jsonb,'[]'::jsonb), COALESCE($4::jsonb,'[]'::jsonb), COALESCE($5::jsonb,'{}'::jsonb), COALESCE($6::jsonb,'[]'::jsonb), COALESCE($7::jsonb,'[]'::jsonb), now())
		ON CONFLICT (tenant_id) DO UPDATE SET
		  institution_info = EXCLUDED.institution_info, doctors = EXCLUDED.doctors, specialties = EXCLUDED.specialties,
		  data = EXCLUDED.data, hard_rules = EXCLUDED.hard_rules, soft_prefs = EXCLUDED.soft_prefs, updated_at = now()`,
		tenantID, string(k.InstitutionInfo), string(k.Doctors), string(k.Specialties), string(k.Data), string(k.HardRules), string(k.SoftPrefs))
	return err
}

// knowledgeContext 组装知识库/规则的注入文本（用于判断+创作 prompt）。
func (s *Service) knowledgeContext(ctx context.Context, tenantID int64) string {
	k, err := s.store.getOrgKnowledge(ctx, tenantID)
	if err != nil || k == nil {
		return ""
	}
	var b strings.Builder
	if strings.TrimSpace(string(k.InstitutionInfo)) != "" && string(k.InstitutionInfo) != "{}" {
		b.WriteString(fmt.Sprintf("机构信息：%s\n", string(k.InstitutionInfo)))
	}
	if strings.TrimSpace(string(k.Specialties)) != "" && string(k.Specialties) != "[]" {
		b.WriteString(fmt.Sprintf("机构特色优势：%s\n", string(k.Specialties)))
	}
	if strings.TrimSpace(string(k.HardRules)) != "" && string(k.HardRules) != "[]" {
		b.WriteString(fmt.Sprintf("硬规则（必须遵守）：%s\n", string(k.HardRules)))
	}
	if strings.TrimSpace(string(k.SoftPrefs)) != "" && string(k.SoftPrefs) != "[]" {
		b.WriteString(fmt.Sprintf("软偏好（风格）：%s\n", string(k.SoftPrefs)))
	}
	return strings.TrimSpace(b.String())
}
