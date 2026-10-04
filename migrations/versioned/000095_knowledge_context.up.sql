CREATE TABLE knowledge_context_entities (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    canonical_name VARCHAR(512) NOT NULL,
    normalized_name VARCHAR(512) NOT NULL,
    entity_type VARCHAR(128) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    merged_into_id VARCHAR(36) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX idx_kce_canonical ON knowledge_context_entities(tenant_id, knowledge_base_id, normalized_name);
CREATE INDEX idx_kce_scope_name ON knowledge_context_entities(tenant_id, knowledge_base_id, canonical_name);

CREATE TABLE knowledge_context_entity_aliases (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    entity_id VARCHAR(36) NOT NULL,
    alias VARCHAR(512) NOT NULL,
    normalized_alias VARCHAR(512) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX idx_kcea_alias ON knowledge_context_entity_aliases(tenant_id, knowledge_base_id, normalized_alias);
CREATE INDEX idx_kcea_entity ON knowledge_context_entity_aliases(tenant_id, knowledge_base_id, entity_id);

CREATE TABLE knowledge_context_evidence (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    knowledge_id VARCHAR(36) NOT NULL,
    version INTEGER NOT NULL,
    body_hash CHAR(64) NOT NULL,
    superseded_by_id VARCHAR(36) NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    publisher TEXT NOT NULL DEFAULT '',
    original_url TEXT NOT NULL DEFAULT '',
    published_at TEXT NOT NULL DEFAULT '',
    image_refs JSONB NOT NULL DEFAULT '[]'::jsonb,
    status VARCHAR(16) NOT NULL DEFAULT 'current',
    extraction_status VARCHAR(16) NOT NULL DEFAULT 'pending',
    extraction_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX idx_kcevid_version ON knowledge_context_evidence(tenant_id, knowledge_base_id, knowledge_id, version);
CREATE INDEX idx_kcevid_hash ON knowledge_context_evidence(tenant_id, knowledge_base_id, knowledge_id, body_hash);
CREATE INDEX idx_kcevid_scope ON knowledge_context_evidence(tenant_id, knowledge_base_id, status, created_at);

CREATE TABLE knowledge_context_facts (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    subject_entity_id VARCHAR(36) NOT NULL,
    kind VARCHAR(16) NOT NULL DEFAULT 'attribute',
    predicate VARCHAR(512) NOT NULL,
    value JSONB,
    value_type VARCHAR(32) NOT NULL DEFAULT '',
    unit VARCHAR(128) NOT NULL DEFAULT '',
    object_entity_id VARCHAR(36) NOT NULL DEFAULT '',
    effective_from TIMESTAMPTZ,
    effective_to TIMESTAMPTZ,
    evidence_id VARCHAR(36) NOT NULL,
    quote TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'current',
    supersedes_id VARCHAR(36) NOT NULL DEFAULT '',
    corrected_by VARCHAR(36) NOT NULL DEFAULT '',
    correction_note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_kcf_scope ON knowledge_context_facts(tenant_id, knowledge_base_id, subject_entity_id, predicate, status);
CREATE INDEX idx_kcf_evidence ON knowledge_context_facts(tenant_id, knowledge_base_id, evidence_id);
CREATE INDEX idx_kcf_object ON knowledge_context_facts(tenant_id, knowledge_base_id, object_entity_id);

CREATE TABLE knowledge_context_events (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    title VARCHAR(512) NOT NULL,
    event_type VARCHAR(128) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ,
    entity_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    evidence_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    source_count INTEGER NOT NULL DEFAULT 0,
    heat DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_kcevent_scope ON knowledge_context_events(tenant_id, knowledge_base_id, occurred_at DESC);

CREATE TABLE knowledge_context_event_evidence (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    event_id VARCHAR(36) NOT NULL,
    evidence_id VARCHAR(36) NOT NULL,
    entity_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    source_title VARCHAR(512) NOT NULL DEFAULT '',
    source_event_type VARCHAR(128) NOT NULL DEFAULT '',
    source_description TEXT NOT NULL DEFAULT '',
    source_occurred_at TIMESTAMPTZ,
    source_key VARCHAR(512) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX idx_kcee_pair ON knowledge_context_event_evidence(tenant_id, event_id, evidence_id);
CREATE INDEX idx_kcee_scope ON knowledge_context_event_evidence(tenant_id, knowledge_base_id, event_id);
CREATE INDEX idx_kcee_evidence ON knowledge_context_event_evidence(tenant_id, knowledge_base_id, evidence_id);

CREATE TABLE knowledge_context_attention (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    evidence_id VARCHAR(36) NOT NULL,
    event_id VARCHAR(36) NOT NULL DEFAULT '',
    novelty DOUBLE PRECISION NOT NULL DEFAULT 0,
    impact DOUBLE PRECISION NOT NULL DEFAULT 0,
    credibility DOUBLE PRECISION NOT NULL DEFAULT 0,
    relevance DOUBLE PRECISION NOT NULL DEFAULT 0,
    actionability DOUBLE PRECISION NOT NULL DEFAULT 0,
    score DOUBLE PRECISION NOT NULL DEFAULT 0,
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX idx_kca_evidence ON knowledge_context_attention(tenant_id, knowledge_base_id, evidence_id);
CREATE INDEX idx_kca_scope_score ON knowledge_context_attention(tenant_id, knowledge_base_id, score DESC);
