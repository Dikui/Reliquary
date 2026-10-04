ALTER TABLE knowledge_context_event_evidence
    ADD COLUMN relation VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE knowledge_context_event_evidence
    ADD COLUMN related_event_id VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE knowledge_context_facts
    ADD COLUMN original_predicate VARCHAR(512) NOT NULL DEFAULT '';
ALTER TABLE knowledge_context_evidence
    ADD COLUMN source_kind VARCHAR(32) NOT NULL DEFAULT '';

CREATE INDEX idx_kcee_related_event
    ON knowledge_context_event_evidence(tenant_id, knowledge_base_id, related_event_id);
