CREATE TABLE knowledge_workflows (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    owner_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT 0,
    schedule TEXT NOT NULL,
    steps TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE (tenant_id, knowledge_base_id, name)
);
CREATE INDEX idx_kworkflow_schedule ON knowledge_workflows (enabled, tenant_id);
CREATE INDEX idx_kworkflow_owner ON knowledge_workflows (owner_id);

CREATE TABLE knowledge_workflow_runs (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    workflow_id VARCHAR(36) NOT NULL,
    workflow_snapshot TEXT NOT NULL,
    owner_id VARCHAR(64) NOT NULL DEFAULT '',
    trigger VARCHAR(32) NOT NULL,
    idempotency_key VARCHAR(160) NOT NULL UNIQUE,
    status VARCHAR(24) NOT NULL,
    current_step_id VARCHAR(128) NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    started_at DATETIME,
    finished_at DATETIME,
    next_run_at DATETIME,
    lease_token VARCHAR(36) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX idx_kwrun_scope ON knowledge_workflow_runs (tenant_id, knowledge_base_id, workflow_id, created_at DESC);
CREATE INDEX idx_kwrun_status ON knowledge_workflow_runs (status, updated_at);

CREATE TABLE knowledge_workflow_step_runs (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    run_id VARCHAR(36) NOT NULL,
    step_id VARCHAR(128) NOT NULL,
    step_type VARCHAR(32) NOT NULL,
    status VARCHAR(24) NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 0,
    input TEXT,
    output TEXT,
    error TEXT NOT NULL DEFAULT '',
    started_at DATETIME,
    finished_at DATETIME,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE (run_id, step_id)
);
CREATE INDEX idx_kwstep_scope ON knowledge_workflow_step_runs (tenant_id, knowledge_base_id, run_id);
CREATE INDEX idx_kwstep_status ON knowledge_workflow_step_runs (status, updated_at);

CREATE TABLE knowledge_workflow_reports (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    workflow_id VARCHAR(36) NOT NULL,
    run_id VARCHAR(36) NOT NULL,
    step_id VARCHAR(128) NOT NULL,
    title VARCHAR(512) NOT NULL,
    markdown TEXT NOT NULL,
    citations TEXT NOT NULL,
    time_from DATETIME,
    time_to DATETIME,
    created_at DATETIME NOT NULL,
    UNIQUE (run_id, step_id)
);
CREATE INDEX idx_kwreport_scope ON knowledge_workflow_reports (tenant_id, knowledge_base_id, created_at DESC);
