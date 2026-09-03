-- Keep the explicit relationship between a recording and its shared Encounter.
-- The column is nullable so existing recordings remain valid while they are
-- linked by the existing Encounter source relation.

ALTER TABLE recordings
  ADD COLUMN IF NOT EXISTS encounter_id BIGINT REFERENCES encounters(id);

UPDATE recordings r
SET encounter_id = e.id
FROM encounters e
WHERE r.encounter_id IS NULL
  AND e.tenant_id = r.tenant_id
  AND e.source_type = 'recording'
  AND e.source_id = r.id;

CREATE INDEX IF NOT EXISTS idx_recordings_tenant_encounter
  ON recordings (tenant_id, encounter_id);
