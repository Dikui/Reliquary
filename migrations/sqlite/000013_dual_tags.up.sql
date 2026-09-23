ALTER TABLE knowledge_tags ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT '';
DROP INDEX IF EXISTS idx_knowledge_tags_kb_name;
CREATE UNIQUE INDEX idx_knowledge_tags_kb_kind_name ON knowledge_tags(tenant_id, knowledge_base_id, kind, name);

CREATE TABLE knowledge_tag_manual_decisions (
    knowledge_id VARCHAR(36) NOT NULL,
    tag_id VARCHAR(36) NOT NULL,
    selected BOOLEAN NOT NULL,
    actor_id VARCHAR(36) NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL,
    PRIMARY KEY (knowledge_id, tag_id)
);
CREATE INDEX idx_tag_manual_decisions_tag ON knowledge_tag_manual_decisions(tag_id);
