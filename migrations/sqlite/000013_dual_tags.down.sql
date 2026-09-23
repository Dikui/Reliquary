DROP TABLE IF EXISTS knowledge_tag_manual_decisions;
DROP INDEX IF EXISTS idx_knowledge_tags_kb_kind_name;
CREATE UNIQUE INDEX idx_knowledge_tags_kb_name ON knowledge_tags(tenant_id, knowledge_base_id, name);
ALTER TABLE knowledge_tags DROP COLUMN kind;
