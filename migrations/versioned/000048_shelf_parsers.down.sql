DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM shelf_document_versions WHERE parser_key<>'' OR parse_attempt>0 OR parse_error<>'' OR file_category<>'' OR parse_status NOT IN ('not_parsed','completed'))
 OR EXISTS (SELECT 1 FROM libraries WHERE shelf_config ?| ARRAY['parsers','default_parser','parser_rules'])
 OR EXISTS (SELECT 1 FROM shelf_changesets WHERE items::text LIKE '%"reparse_document"%')
 OR EXISTS (SELECT 1 FROM task_pending_ops WHERE task_type='shelf:parse') THEN
 RAISE EXCEPTION 'shelf parser data exists; refusing downgrade';
 END IF;
END $$;
ALTER TABLE shelf_document_versions DROP CONSTRAINT shelf_document_versions_parse_status_check;
ALTER TABLE shelf_document_versions ADD CONSTRAINT shelf_document_versions_parse_status_check CHECK (parse_status IN ('not_parsed','completed'));
ALTER TABLE shelf_document_versions DROP COLUMN parser_key, DROP COLUMN parse_attempt, DROP COLUMN parse_error, DROP COLUMN file_category;
