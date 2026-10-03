-- 简化门诊病历评分器运行模型。
--
-- 评分器只使用 emr_score_standards 中的完整评分标准正文。
-- 不再维护独立规则拆分表，避免把标准评分做成复杂规则引擎。

DROP TABLE IF EXISTS public.emr_score_rules;
