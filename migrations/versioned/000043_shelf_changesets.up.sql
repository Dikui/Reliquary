CREATE TABLE shelf_changesets (
    id varchar(36) PRIMARY KEY,
    space_id bigint NOT NULL REFERENCES spaces(id),
    library_id varchar(36) NOT NULL REFERENCES libraries(id),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    status text NOT NULL CHECK (status IN ('pending', 'confirmed')),
    source jsonb NOT NULL,
    items jsonb NOT NULL,
    deferred_changeset_id varchar(36) NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    confirmed_at timestamptz
);
CREATE INDEX shelf_changesets_pending ON shelf_changesets(space_id, library_id, status, created_at DESC, id);
ALTER TABLE operation_logs ADD COLUMN source jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN changeset_id varchar(36) NOT NULL DEFAULT '';
CREATE INDEX operation_logs_source ON operation_logs(space_id, (source->>'type'), (source->>'agent_name'), (source->>'import_batch_id'));
