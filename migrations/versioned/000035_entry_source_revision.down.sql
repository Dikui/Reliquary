DROP INDEX IF EXISTS idx_entries_source_id;
ALTER TABLE entries
 DROP CONSTRAINT IF EXISTS entries_source_revision_check,
 DROP COLUMN source_revision,
 DROP COLUMN source_id;
