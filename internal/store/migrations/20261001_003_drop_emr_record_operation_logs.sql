-- 当前正式留痕使用 emr_process_records。
-- emr_record_operation_logs 无数据、无运行引用，也无数据库对象依赖。
-- 已执行的历史迁移不修改；本迁移只删除当前空的遗留表。

DROP TABLE IF EXISTS public.emr_record_operation_logs;
