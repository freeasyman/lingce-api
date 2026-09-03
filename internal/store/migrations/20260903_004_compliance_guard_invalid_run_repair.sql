-- Repair the invalid 20260903.1 runs created before the scanner wrote the
-- scene rule-set version and input fingerprint in their correct columns.
-- Keep the records for audit, but do not show invalid runs as active findings.

UPDATE guard_analysis_runs
SET status = 'cancelled',
    failure_reason = CASE
      WHEN BTRIM(COALESCE(failure_reason, '')) = ''
        THEN 'invalid scene rule-set run: rule-set version and input fingerprint were written in reversed columns; superseded by strategy 20260903.2'
      ELSE failure_reason
    END,
    updated_at = NOW()
WHERE strategy_code = 'communication.short_recording'
  AND strategy_version = '20260903.1'
  AND rule_set_version ~ '^[0-9a-f]{32}$';
