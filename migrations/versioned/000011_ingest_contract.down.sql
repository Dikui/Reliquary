DROP INDEX idx_entries_source_ref_latest;
ALTER TABLE entries DROP COLUMN source_ref, DROP COLUMN source_body_hash, DROP COLUMN identifier, DROP COLUMN graph;
