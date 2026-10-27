-- 内容获客：机构知识库补 tenant_id 唯一索引。
-- 原因：upsertOrgKnowledge 使用 ON CONFLICT (tenant_id)，但表只有主键 id，缺少唯一约束，
--       首次保存直接报错（POST /content-org-knowledge 500）。
-- 署名：DSH
-- 时间：2026-10-27

CREATE UNIQUE INDEX IF NOT EXISTS uq_content_org_knowledge_tenant
    ON content_org_knowledge (tenant_id);
