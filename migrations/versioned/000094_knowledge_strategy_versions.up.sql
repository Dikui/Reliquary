CREATE TABLE knowledge_strategy_versions (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    status VARCHAR(16) NOT NULL,
    rules JSONB NOT NULL,
    previous_rules JSONB NOT NULL,
    reason TEXT NOT NULL,
    sample_results JSONB NOT NULL,
    created_by VARCHAR(36) NOT NULL,
    activated_by VARCHAR(36) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    activated_at TIMESTAMPTZ
);
CREATE INDEX idx_knowledge_strategy_scope ON knowledge_strategy_versions(tenant_id, knowledge_base_id, status, created_at);
CREATE UNIQUE INDEX idx_knowledge_strategy_one_active ON knowledge_strategy_versions(tenant_id, knowledge_base_id) WHERE status = 'active';
