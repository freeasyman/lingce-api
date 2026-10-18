-- Backward-compatible extensions for the protected external data adapter.
-- Existing business rows are preserved; only the adapter's supported values
-- and source version metadata are added.

ALTER TABLE external_data_deliveries
  ADD COLUMN IF NOT EXISTS source_updated_at TIMESTAMPTZ;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_encounters_source_type') THEN
    ALTER TABLE encounters DROP CONSTRAINT chk_encounters_source_type;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_encounters_channel') THEN
    ALTER TABLE encounters DROP CONSTRAINT chk_encounters_channel;
  END IF;
END $$;

ALTER TABLE encounters
  ADD CONSTRAINT chk_encounters_source_type CHECK (source_type IN ('recording','transcript','phone','wechat','manual','realtime','external_data','external_queue')),
  ADD CONSTRAINT chk_encounters_channel CHECK (channel IN ('store','phone','wechat','online'));

CREATE INDEX IF NOT EXISTS external_data_deliveries_source_updated_idx
  ON external_data_deliveries(tenant_id, object_type, source_system, source_updated_at DESC);
