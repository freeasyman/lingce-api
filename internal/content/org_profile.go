package content

import (
	"context"
	"fmt"
)

type ContentOrgProfile struct {
	TenantID          int64      `json:"tenant_id"`
	InstitutionName   string     `json:"institution_name"`
	Region            string     `json:"region"`
	Address           string     `json:"address"`
	Phone             string     `json:"phone"`
	Website           string     `json:"website"`
	License           string     `json:"license"`
	DoctorName        string     `json:"doctor_name"`
	DoctorTitle       string     `json:"doctor_title"`
	DoctorDepartment  string     `json:"doctor_department"`
	DoctorCredentials string     `json:"doctor_credentials"`
	DoctorProfileURL  string     `json:"doctor_profile_url"`
	DoctorBio         string     `json:"doctor_bio"`
	DoctorSchedule    string     `json:"doctor_schedule"`
	DoctorSameAs      string     `json:"doctor_same_as"`
	ExtraData         JSONObject `json:"extra_data,omitempty"`
}

func (s *Store) GetContentOrgProfile(ctx context.Context, tenantID int64) (*ContentOrgProfile, error) {
	profile := &ContentOrgProfile{TenantID: tenantID}
	err := s.pool.QueryRow(ctx, `
		SELECT institution_name, region, address, phone, website, license,
		       doctor_name, doctor_title, doctor_department, doctor_credentials,
		       doctor_profile_url, doctor_bio, doctor_schedule, doctor_same_as, extra_data
		FROM content_org_profiles WHERE tenant_id = $1
	`, tenantID).Scan(
		&profile.InstitutionName, &profile.Region, &profile.Address, &profile.Phone, &profile.Website, &profile.License,
		&profile.DoctorName, &profile.DoctorTitle, &profile.DoctorDepartment, &profile.DoctorCredentials,
		&profile.DoctorProfileURL, &profile.DoctorBio, &profile.DoctorSchedule, &profile.DoctorSameAs, &profile.ExtraData,
	)
	if err != nil {
		// A missing profile is a valid empty state; the settings page fills it.
		if err.Error() == "no rows in result set" {
			return profile, nil
		}
		return nil, fmt.Errorf("get content org profile: %w", err)
	}
	return profile, nil
}

func (s *Store) UpsertContentOrgProfile(ctx context.Context, profile *ContentOrgProfile) (*ContentOrgProfile, error) {
	if profile.ExtraData == nil {
		profile.ExtraData = JSONObject{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO content_org_profiles (
			tenant_id, institution_name, region, address, phone, website, license,
			doctor_name, doctor_title, doctor_department, doctor_credentials,
			doctor_profile_url, doctor_bio, doctor_schedule, doctor_same_as, extra_data
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (tenant_id) DO UPDATE SET
			institution_name=EXCLUDED.institution_name,
			region=EXCLUDED.region,
			address=EXCLUDED.address,
			phone=EXCLUDED.phone,
			website=EXCLUDED.website,
			license=EXCLUDED.license,
			doctor_name=EXCLUDED.doctor_name,
			doctor_title=EXCLUDED.doctor_title,
			doctor_department=EXCLUDED.doctor_department,
			doctor_credentials=EXCLUDED.doctor_credentials,
			doctor_profile_url=EXCLUDED.doctor_profile_url,
			doctor_bio=EXCLUDED.doctor_bio,
			doctor_schedule=EXCLUDED.doctor_schedule,
			doctor_same_as=EXCLUDED.doctor_same_as,
			extra_data=EXCLUDED.extra_data,
			updated_at=NOW()
	`, profile.TenantID, profile.InstitutionName, profile.Region, profile.Address, profile.Phone, profile.Website, profile.License,
		profile.DoctorName, profile.DoctorTitle, profile.DoctorDepartment, profile.DoctorCredentials,
		profile.DoctorProfileURL, profile.DoctorBio, profile.DoctorSchedule, profile.DoctorSameAs, profile.ExtraData)
	if err != nil {
		return nil, fmt.Errorf("upsert content org profile: %w", err)
	}
	return s.GetContentOrgProfile(ctx, profile.TenantID)
}
