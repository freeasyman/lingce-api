package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// 机构知识库 / 规则：判断 + 创作注入。

type orgKnowledge struct {
	InstitutionInfo json.RawMessage `json:"institution_info"`
	Doctors         json.RawMessage `json:"doctors"`
	Specialties     json.RawMessage `json:"specialties"`
	Data            json.RawMessage `json:"data"`
	HardRules       json.RawMessage `json:"hard_rules"`
	SoftPrefs       json.RawMessage `json:"soft_prefs"`
	// BrandAssets 品牌素材：logo/二维码/医生照片/环境照片/配色/字体。
	BrandAssets json.RawMessage `json:"brand_assets"`
}

func (s *Store) getOrgKnowledge(ctx context.Context, tenantID int64) (*orgKnowledge, error) {
	var k orgKnowledge
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(institution_info,'{}'::jsonb), COALESCE(doctors,'[]'::jsonb), COALESCE(specialties,'[]'::jsonb),
		       COALESCE(data,'{}'::jsonb), COALESCE(hard_rules,'[]'::jsonb), COALESCE(soft_prefs,'[]'::jsonb),
		       COALESCE(brand_assets,'{}'::jsonb)
		FROM content_org_knowledge WHERE tenant_id = $1`, tenantID).
		Scan(&k.InstitutionInfo, &k.Doctors, &k.Specialties, &k.Data, &k.HardRules, &k.SoftPrefs, &k.BrandAssets)
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
		INSERT INTO content_org_knowledge (tenant_id, institution_info, doctors, specialties, data, hard_rules, soft_prefs, brand_assets, updated_at)
		VALUES ($1, COALESCE(NULLIF($2,''),'{}')::jsonb, COALESCE(NULLIF($3,''),'[]')::jsonb, COALESCE(NULLIF($4,''),'[]')::jsonb, COALESCE(NULLIF($5,''),'{}')::jsonb, COALESCE(NULLIF($6,''),'[]')::jsonb, COALESCE(NULLIF($7,''),'[]')::jsonb, COALESCE(NULLIF($8,''),'{}')::jsonb, now())
		ON CONFLICT (tenant_id) DO UPDATE SET
		  institution_info = EXCLUDED.institution_info, doctors = EXCLUDED.doctors, specialties = EXCLUDED.specialties,
		  data = EXCLUDED.data, hard_rules = EXCLUDED.hard_rules, soft_prefs = EXCLUDED.soft_prefs,
		  brand_assets = EXCLUDED.brand_assets, updated_at = now()`,
		tenantID, string(k.InstitutionInfo), string(k.Doctors), string(k.Specialties), string(k.Data), string(k.HardRules), string(k.SoftPrefs), string(k.BrandAssets))
	return err
}

// knowledgeContext 组装知识库/规则的注入文本（用于判断+创作 prompt）。
func (s *Service) knowledgeContext(ctx context.Context, tenantID int64) string {
	k, err := s.store.getOrgKnowledge(ctx, tenantID)
	if err != nil || k == nil {
		return ""
	}
	var b strings.Builder

	// 机构信息
	var info map[string]any
	if json.Unmarshal(k.InstitutionInfo, &info) == nil && len(info) > 0 {
		labels := [][2]string{{"name", "机构名称"}, {"region", "地区"}, {"address", "地址"}, {"phone", "电话"}, {"website", "官网"}, {"license", "资质"}, {"description", "简介"}}
		var items []string
		for _, kv := range labels {
			if v, ok := info[kv[0]]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
				items = append(items, fmt.Sprintf("- %s：%v", kv[1], v))
			}
		}
		if len(items) > 0 {
			b.WriteString("机构信息：\n" + strings.Join(items, "\n") + "\n")
		}
	}

	// 医生
	var doctors []map[string]any
	if json.Unmarshal(k.Doctors, &doctors) == nil && len(doctors) > 0 {
		var items []string
		for _, d := range doctors {
			name := strings.TrimSpace(fmt.Sprint(d["name"]))
			if name == "" {
				continue
			}
			head := name
			if t := strings.TrimSpace(fmt.Sprint(d["title"])); t != "" && t != "<nil>" {
				head += " " + t
			}
			if dep := strings.TrimSpace(fmt.Sprint(d["department"])); dep != "" && dep != "<nil>" {
				head += "（" + dep + "）"
			}
			line := "- " + head
			if c := strings.TrimSpace(fmt.Sprint(d["credentials"])); c != "" && c != "<nil>" {
				line += "｜擅长/资质：" + c
			}
			if bio := strings.TrimSpace(fmt.Sprint(d["bio"])); bio != "" && bio != "<nil>" {
				line += "｜" + bio
			}
			items = append(items, line)
		}
		if len(items) > 0 {
			b.WriteString("医生：\n" + strings.Join(items, "\n") + "\n")
		}
	}

	// 特色优势
	var specs []map[string]any
	if json.Unmarshal(k.Specialties, &specs) == nil && len(specs) > 0 {
		var items []string
		for _, sp := range specs {
			name := strings.TrimSpace(fmt.Sprint(sp["name"]))
			if name == "" || name == "<nil>" {
				continue
			}
			line := "- "
			if t := strings.TrimSpace(fmt.Sprint(sp["type"])); t != "" && t != "<nil>" {
				line += "【" + t + "】"
			}
			line += name
			if d := strings.TrimSpace(fmt.Sprint(sp["desc"])); d != "" && d != "<nil>" {
				line += "：" + d
			}
			items = append(items, line)
		}
		if len(items) > 0 {
			b.WriteString("机构特色优势：\n" + strings.Join(items, "\n") + "\n")
		}
	}

	// 数据口径
	var data []map[string]any
	if json.Unmarshal(k.Data, &data) == nil && len(data) > 0 {
		var items []string
		for _, item := range data {
			label := strings.TrimSpace(fmt.Sprint(item["label"]))
			val := strings.TrimSpace(fmt.Sprint(item["value"]))
			if label != "" && val != "" && label != "<nil>" && val != "<nil>" {
				items = append(items, fmt.Sprintf("- %s：%s", label, val))
			}
		}
		if len(items) > 0 {
			b.WriteString("可引用数据口径：\n" + strings.Join(items, "\n") + "\n")
		}
	}

	// 硬规则 / 软偏好
	for _, group := range []struct {
		raw   json.RawMessage
		title string
	}{
		{k.HardRules, "硬规则（必须遵守）："},
		{k.SoftPrefs, "软偏好（风格）："},
	} {
		var rules []string
		if json.Unmarshal(group.raw, &rules) == nil && len(rules) > 0 {
			var items []string
			for _, r := range rules {
				if strings.TrimSpace(r) != "" {
					items = append(items, "- "+r)
				}
			}
			if len(items) > 0 {
				b.WriteString(group.title + "\n" + strings.Join(items, "\n") + "\n")
			}
		}
	}

	return strings.TrimSpace(b.String())
}
