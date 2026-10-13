-- Development only. Incompatible new records stop rollback instead of being discarded.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM ingest_discards WHERE normalized_url='' OR fingerprint <> encode(sha256(convert_to('url:' || normalized_url,'UTF8')),'hex')) THEN
  RAISE EXCEPTION 'library discard fingerprints require restoring the pre-upgrade backup';
 END IF;
END $$;
ALTER TABLE ingest_discards DROP CONSTRAINT ingest_discards_pkey;
ALTER TABLE ingest_discards ADD PRIMARY KEY (space_id, normalized_url);
ALTER TABLE ingest_discards DROP COLUMN library_id, DROP COLUMN fingerprint;
DROP INDEX idx_entries_identifier;
ALTER TABLE entry_repost_sources DROP CONSTRAINT idx_repost_origin;
ALTER TABLE entry_repost_sources ADD CONSTRAINT idx_repost_origin UNIQUE (space_id, entry_id, source_fingerprint);
ALTER TABLE entry_repost_sources DROP CONSTRAINT entry_repost_sources_match_method_check;
ALTER TABLE entry_repost_sources ADD CONSTRAINT entry_repost_sources_match_method_check CHECK (match_method IN ('url','content_hash','simhash','file_hash'));
