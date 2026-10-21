ALTER TABLE content_org_profiles
    ADD COLUMN IF NOT EXISTS logo_assets JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS qr_assets JSONB NOT NULL DEFAULT '[]'::jsonb;
