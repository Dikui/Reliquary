ALTER TABLE entries
 ADD COLUMN source_id text NOT NULL DEFAULT '',
 ADD COLUMN source_revision bigint,
 ADD CONSTRAINT entries_source_revision_check CHECK (
  (source_id = '' AND source_revision IS NULL) OR
  (source_id <> '' AND source_revision IS NOT NULL AND source_revision >= 0)
 );

CREATE UNIQUE INDEX idx_entries_source_id
 ON entries (space_id, library_id, md5(source_id))
 WHERE source_id <> '' AND deleted_at IS NULL;
