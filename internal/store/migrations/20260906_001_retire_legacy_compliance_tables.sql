-- Retire the legacy Compliance Guard storage before the new command-line
-- engine is introduced. This migration deliberately leaves all source-system
-- tables and the migration ledger untouched.

DROP TABLE IF EXISTS guard_case_evidence;
DROP TABLE IF EXISTS guard_case_findings;
DROP TABLE IF EXISTS guard_remediation_tasks;
DROP TABLE IF EXISTS guard_review_actions;
DROP TABLE IF EXISTS guard_finding_evidence;
DROP TABLE IF EXISTS guard_cases;
DROP TABLE IF EXISTS guard_scan_requests;
DROP TABLE IF EXISTS guard_machine_findings;
DROP TABLE IF EXISTS guard_findings;
DROP TABLE IF EXISTS guard_evidence;
DROP TABLE IF EXISTS guard_analysis_runs;
DROP TABLE IF EXISTS guard_source_snapshots;
DROP TABLE IF EXISTS guard_scan_cursors;
DROP TABLE IF EXISTS guard_rule_set_rule_versions;
DROP TABLE IF EXISTS guard_rule_set_versions;
DROP TABLE IF EXISTS guard_rule_versions;
DROP TABLE IF EXISTS guard_exports;
DROP TABLE IF EXISTS guard_source_refs;
DROP TABLE IF EXISTS compliance_analysis_jobs;
DROP TABLE IF EXISTS compliance_events;
DROP TABLE IF EXISTS compliance_rules;
