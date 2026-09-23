CREATE TABLE knowledge_anomalies (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    knowledge_ids TEXT NOT NULL,
    kind VARCHAR(32) NOT NULL,
    description TEXT NOT NULL,
    proposal_type VARCHAR(16) NOT NULL DEFAULT 'none',
    proposed_tag_ids TEXT NOT NULL DEFAULT '[]',
    proposed_delete_tag_id VARCHAR(36) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    reported_by VARCHAR(36) NOT NULL,
    decided_by VARCHAR(36) NOT NULL DEFAULT '',
    decision_note TEXT NOT NULL DEFAULT '',
    result VARCHAR(500) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    decided_at TIMESTAMPTZ
);
CREATE INDEX idx_knowledge_anomalies_scope ON knowledge_anomalies(tenant_id, knowledge_base_id, status, created_at);
