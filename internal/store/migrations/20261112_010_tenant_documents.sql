-- 计费缴费：开通通知函 / 服务开通确认函的文档留档（幂等）。
--
-- 背景：运营在机构档案点「生成开通函」后，后端用纯 Go 渲染一份 A4 单页 PDF，
-- 直接把 PDF 字节存进本表，客户拿旧函来核对时能重新下载当初那一版。
--
-- 设计取舍：
--   · 同一 (tenant_id, doc_type) 允许存在多条记录：每次生成都是一份新文档，旧的保留；
--   · PDF 以 BYTEA 存库而不是对象存储：单份约 50~100KB，无外部依赖、备份/一致性简单；
--     真到了量级上限（比如单库几十 GB）再拆到对象存储，接口契约不用变；
--   · doc_no 唯一：编号按 年 + 类型 递增（LC-TR-2026-0005 / LC-SV-2026-0005），
--     并发生成撞号时插入会失败，服务层换号重试；
--   · tenant_id 沿用计费各表的习惯只建索引不建外键（租户数据清理按 tenant_id 逐表删）。
--
-- 本迁移只做加法，可安全重复执行。

CREATE TABLE IF NOT EXISTS tenant_documents (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL,
    doc_type     VARCHAR(32)  NOT NULL,
    doc_no       VARCHAR(64)  NOT NULL,
    generated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    generated_by BIGINT,
    file_size    BIGINT       NOT NULL DEFAULT 0,
    content      BYTEA        NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT tenant_documents_doc_type_check
        CHECK (doc_type IN ('trial_notice', 'service_confirm'))
);

-- 编号全局唯一：下载链接只带 doc_id，编号冲突说明并发生成撞号，服务层重试即可。
CREATE UNIQUE INDEX IF NOT EXISTS tenant_documents_doc_no_key
    ON tenant_documents (doc_no);

-- 机构档案「文档记录」按生成时间倒序读。
CREATE INDEX IF NOT EXISTS tenant_documents_tenant_generated_at_idx
    ON tenant_documents (tenant_id, generated_at DESC);
