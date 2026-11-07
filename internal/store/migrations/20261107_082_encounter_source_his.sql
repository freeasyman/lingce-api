-- HIS 挂号确认后生产 encounters(source_type='his')，扩展共享 encounters 的
-- source_type 允许值。替换现有 CHECK 约束以纳入 'his'（其余值保持不变）。
ALTER TABLE IF EXISTS encounters DROP CONSTRAINT IF EXISTS chk_encounters_source_type;
ALTER TABLE IF EXISTS encounters ADD CONSTRAINT chk_encounters_source_type
  CHECK (source_type::text = ANY (ARRAY[
    'recording','transcript','phone','wechat','manual','realtime',
    'external_data','external_queue','his'
  ]::text[]));
