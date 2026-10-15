ALTER TABLE ingest_discards ADD COLUMN library_id text, ADD COLUMN fingerprint char(64);
UPDATE ingest_discards d SET library_id=COALESCE(
 (SELECT e.library_id FROM entries e WHERE e.space_id=d.space_id AND e.id=d.article_id),
 'legacy-space:' || d.space_id::text),
 fingerprint=encode(sha256(convert_to('url:' || d.normalized_url, 'UTF8')), 'hex');
ALTER TABLE ingest_discards ALTER COLUMN library_id SET NOT NULL, ALTER COLUMN fingerprint SET NOT NULL;
ALTER TABLE ingest_discards DROP CONSTRAINT ingest_discards_pkey;
ALTER TABLE ingest_discards ADD PRIMARY KEY (space_id, library_id, fingerprint);
CREATE INDEX idx_entries_identifier ON entries (space_id, library_id, md5(identifier)) WHERE identifier <> '' AND deleted_at IS NULL;
ALTER TABLE entry_repost_sources DROP CONSTRAINT idx_repost_origin;
ALTER TABLE entry_repost_sources ADD CONSTRAINT idx_repost_origin UNIQUE (space_id, library_id, entry_id, source_fingerprint);
ALTER TABLE entry_repost_sources DROP CONSTRAINT entry_repost_sources_match_method_check;
ALTER TABLE entry_repost_sources ADD CONSTRAINT entry_repost_sources_match_method_check CHECK (match_method IN ('url','content_hash','simhash','file_hash','publisher_date'));
