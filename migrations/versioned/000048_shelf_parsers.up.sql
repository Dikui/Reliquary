ALTER TABLE shelf_document_versions
 ADD COLUMN parser_key text NOT NULL DEFAULT '',
 ADD COLUMN parse_attempt bigint NOT NULL DEFAULT 0 CHECK (parse_attempt >= 0),
 ADD COLUMN parse_error text NOT NULL DEFAULT '',
 ADD COLUMN file_category text NOT NULL DEFAULT '';
ALTER TABLE shelf_document_versions DROP CONSTRAINT shelf_document_versions_parse_status_check;
ALTER TABLE shelf_document_versions ADD CONSTRAINT shelf_document_versions_parse_status_check
 CHECK (parse_status IN ('not_parsed','pending','parsing','retrying','failed','completed'));
