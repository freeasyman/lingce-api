ALTER TABLE emr_check_results
  DROP CONSTRAINT IF EXISTS emr_check_results_quality_requirement_id_fkey,
  ADD CONSTRAINT emr_check_results_quality_rule_id_fkey
    FOREIGN KEY (quality_requirement_id)
    REFERENCES emr_quality_rules(id)
    ON DELETE RESTRICT;
