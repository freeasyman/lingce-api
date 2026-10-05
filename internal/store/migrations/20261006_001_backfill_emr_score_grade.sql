UPDATE public.emr_record_score_current
SET grade = CASE
  WHEN major_program_score < 65 OR total_score < 70 THEN '丙级'
  WHEN total_score <= 85 OR major_program_score < 70 THEN '乙级'
  ELSE '甲级'
END
WHERE grade = '' AND status IN ('已完成', '待人工复核');

UPDATE public.emr_quality_runs
SET grade = CASE
  WHEN major_score < 65 OR total_score < 70 THEN '丙级'
  WHEN total_score <= 85 OR major_score < 70 THEN '乙级'
  ELSE '甲级'
END
WHERE grade = '' AND status = 'completed' AND action IN ('complete', 'archive');
