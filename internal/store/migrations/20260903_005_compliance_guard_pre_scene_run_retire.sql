-- Runs before scene rule sets could not record which role-specific rule set
-- was used. Retire them from the active workbench while retaining audit data.
-- Strategy 20260903.2 regenerates valid doctor/consultant scene-specific runs.

UPDATE guard_analysis_runs
SET status = 'cancelled',
    failure_reason = CASE
      WHEN BTRIM(COALESCE(failure_reason, '')) = ''
        THEN 'pre-scene rule-set run: superseded by role-specific strategy 20260903.2'
      ELSE failure_reason
    END,
    updated_at = NOW()
WHERE strategy_code = 'communication.short_recording'
  AND strategy_version = '20260902.1'
  AND rule_set_version = '20260902.1';
