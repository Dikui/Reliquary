BEGIN;

ALTER TABLE entries
    ADD COLUMN acquisition varchar(16) NOT NULL DEFAULT '',
    ADD COLUMN incomplete boolean NOT NULL DEFAULT false,
    ADD COLUMN external_completion_attempted boolean NOT NULL DEFAULT false,
    ADD COLUMN ingest_api_key_id bigint NOT NULL DEFAULT 0,
    ADD COLUMN ingest_api_key_name text NOT NULL DEFAULT '',
    ADD COLUMN last_ingest_api_key_id bigint NOT NULL DEFAULT 0,
    ADD COLUMN last_ingest_api_key_name text NOT NULL DEFAULT '',
    ADD COLUMN missing_materials jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN ingest_attachments jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN content_type_hint varchar(16) NOT NULL DEFAULT '',
    ADD COLUMN related_article_id varchar(36) NOT NULL DEFAULT '';

ALTER TABLE operation_logs
    ADD COLUMN api_key_id bigint NOT NULL DEFAULT 0,
    ADD COLUMN api_key_name text NOT NULL DEFAULT '';

CREATE TABLE ingest_assets (
    id varchar(36) PRIMARY KEY,
    space_id bigint NOT NULL REFERENCES spaces(id),
    name text NOT NULL,
    file_type varchar(16) NOT NULL,
    size bigint NOT NULL CHECK (size > 0),
    path text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    referenced_at timestamptz,
    entry_id varchar(36) NOT NULL DEFAULT ''
);
CREATE INDEX ingest_assets_expiry_idx ON ingest_assets(created_at) WHERE referenced_at IS NULL;
CREATE INDEX ingest_assets_entry_idx ON ingest_assets(space_id, entry_id);

CREATE TABLE ingest_discards (
    space_id bigint NOT NULL REFERENCES spaces(id),
    normalized_url text NOT NULL,
    article_id varchar(36) NOT NULL DEFAULT '',
    discarded_at timestamptz NOT NULL DEFAULT now(),
    reason text NOT NULL DEFAULT '',
    PRIMARY KEY (space_id, normalized_url)
);
CREATE INDEX entries_incomplete_idx ON entries(space_id, created_at DESC, id) WHERE incomplete AND deleted_at IS NULL;

COMMIT;
