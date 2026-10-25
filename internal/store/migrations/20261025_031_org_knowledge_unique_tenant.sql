-- 内容获客：机构知识库补 tenant_id 唯一约束。
-- 背景：upsertOrgKnowledge 用 ON CONFLICT (tenant_id)，但表上只有主键 id，没有唯一约束，
-- 写入直接 500；这张表一直是空的，所以从没暴露。
-- 署名：DSH
-- 时间：2026-10-25

CREATE UNIQUE INDEX IF NOT EXISTS content_org_knowledge_tenant_uidx
  ON content_org_knowledge (tenant_id);
