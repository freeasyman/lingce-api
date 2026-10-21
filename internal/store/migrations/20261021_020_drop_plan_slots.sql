UPDATE content_items
SET extra_data = COALESCE(extra_data, '{}'::jsonb) - 'plan_slot_id' - 'account_id' - 'column_id'
WHERE extra_data ? 'plan_slot_id';
DROP TABLE IF EXISTS content_plan_slots;
