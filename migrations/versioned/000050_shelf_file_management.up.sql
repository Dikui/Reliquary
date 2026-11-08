ALTER TABLE shelf_document_versions ALTER COLUMN owner_id DROP NOT NULL;
CREATE TABLE shelf_document_selections (
 id varchar(36) PRIMARY KEY,
 space_id bigint NOT NULL REFERENCES spaces(id),
 library_id varchar(36) NOT NULL REFERENCES libraries(id),
 file_id varchar(36) NOT NULL REFERENCES entities(id),
 version_id varchar(36) NOT NULL REFERENCES shelf_document_versions(id),
 selected_at timestamptz NOT NULL
);
CREATE INDEX shelf_document_selections_file ON shelf_document_selections(file_id,selected_at DESC);
