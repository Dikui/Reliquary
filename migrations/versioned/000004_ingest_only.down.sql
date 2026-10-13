BEGIN;
DROP INDEX entries_incomplete_idx;
DROP TABLE ingest_discards;
DROP TABLE ingest_assets;
ALTER TABLE operation_logs DROP COLUMN api_key_id, DROP COLUMN api_key_name;
ALTER TABLE entries
    DROP COLUMN acquisition,
    DROP COLUMN incomplete,
    DROP COLUMN external_completion_attempted,
    DROP COLUMN ingest_api_key_id,
    DROP COLUMN ingest_api_key_name,
    DROP COLUMN last_ingest_api_key_id,
    DROP COLUMN last_ingest_api_key_name,
    DROP COLUMN missing_materials,
    DROP COLUMN ingest_attachments,
    DROP COLUMN content_type_hint,
    DROP COLUMN related_article_id;
COMMIT;
