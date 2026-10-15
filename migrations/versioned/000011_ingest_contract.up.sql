ALTER TABLE entries
 ADD COLUMN source_ref text NOT NULL DEFAULT '',
 ADD COLUMN source_body_hash text NOT NULL DEFAULT '',
 ADD COLUMN identifier text NOT NULL DEFAULT '',
 ADD COLUMN graph boolean;
CREATE INDEX idx_entries_source_ref_latest ON entries (space_id, library_id, md5(source_ref), created_at DESC, id DESC) WHERE source_ref <> '' AND deleted_at IS NULL;
