-- HIS 就诊生命周期使用自有状态：挂号 new → 接诊 in_progress → 完成 completed / 退号 cancelled。
-- 扩展共享 encounters.status 的 CHECK 允许值（其余值保持不变）。
ALTER TABLE IF EXISTS encounters DROP CONSTRAINT IF EXISTS chk_encounters_status;
ALTER TABLE IF EXISTS encounters ADD CONSTRAINT chk_encounters_status
  CHECK (status::text = ANY (ARRAY[
    'new','unmatched','matched','analyzed','closed','archived',
    'in_progress','completed','cancelled'
  ]::text[]));
