-- A realtime microphone consultation creates its shared Encounter before
-- the final recording is stored. Keep it distinct from recording-originated
-- encounters while using the existing online channel classification.

ALTER TABLE encounters DROP CONSTRAINT IF EXISTS chk_encounters_source_type;

ALTER TABLE encounters
  ADD CONSTRAINT chk_encounters_source_type
  CHECK (source_type IN ('recording', 'transcript', 'phone', 'wechat', 'manual', 'realtime'));
