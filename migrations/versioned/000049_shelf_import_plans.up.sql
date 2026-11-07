CREATE TABLE shelf_import_plans (
    space_id bigint NOT NULL REFERENCES spaces(id),
    library_id varchar(36) NOT NULL REFERENCES libraries(id),
    key text NOT NULL,
    name text NOT NULL,
    file_kind text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    definition jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (space_id, library_id, key)
);
