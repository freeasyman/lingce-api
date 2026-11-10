-- 手机 App 直传录音走赢客专家新分析（consultation_analyses），不再进入
-- lingce-worker 的旧自动分析链路。
--
-- Worker 的扫描链路在转写完成后会把 cleaned_transcription_status 置 queued，
-- clean 完成后又把 recordings.analysis_status 置 queued；旧 analyze 在没有匹配
-- worker_analysis_routes 的租户/角色（例如 tenant 4 + admin）下会报
-- "no active analysis route"，随后把 analysis_status 置 failed，导致录音列表/
-- 详情显示失败。
--
-- 本迁移只在数据库层做最小条件跳过：仅 source='mobile_app' 的录音，禁止把
-- analysis_status 置为 pending/queued/processing/running/failed，统一收敛为
-- 'skipped'；'completed' 仍然允许（例如后续由新链路显式回写）。
-- 其他 source 完全不受影响。已存在的 failed 历史记录不会被重置：只有真正
-- 改变 analysis_status 的 UPDATE 才会被改写。
--
-- 署名：Codex
-- 时间：2026-11-10
CREATE OR REPLACE FUNCTION skip_legacy_analysis_for_mobile_app()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.source IS DISTINCT FROM 'mobile_app' THEN
        RETURN NEW;
    END IF;

    -- 不重置历史：值没有变化时直接放行（例如旧记录本来就是 failed）。
    IF TG_OP = 'UPDATE' AND NEW.analysis_status IS NOT DISTINCT FROM OLD.analysis_status THEN
        RETURN NEW;
    END IF;

    IF NEW.analysis_status IS DISTINCT FROM 'completed'
       AND NEW.analysis_status IS DISTINCT FROM 'skipped' THEN
        NEW.analysis_status := 'skipped';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_skip_legacy_analysis_for_mobile_app ON recordings;
CREATE TRIGGER trg_skip_legacy_analysis_for_mobile_app
BEFORE INSERT OR UPDATE OF analysis_status ON recordings
FOR EACH ROW
EXECUTE FUNCTION skip_legacy_analysis_for_mobile_app();

COMMENT ON FUNCTION skip_legacy_analysis_for_mobile_app() IS
    '手机 App 录音跳过旧 analyze：source=mobile_app 时 analysis_status 收敛为 skipped，不进入 Worker 旧分析队列。';
