-- 换新以「新机寄出」为完成点：待寄回不再作为工单状态，旧机回收走设备内部进度。
UPDATE badge_tickets SET status = 'completed', completed_at = COALESCE(completed_at, NOW()) WHERE status = 'waiting';
