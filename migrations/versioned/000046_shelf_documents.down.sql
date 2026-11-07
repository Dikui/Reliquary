DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM shelf_document_versions)
    OR EXISTS (SELECT 1 FROM shelf_changesets WHERE items::text ~ '"(add_document_version|max_file_bytes)"')
    OR EXISTS (SELECT 1 FROM libraries WHERE shelf_config ? 'max_file_bytes') THEN
  RAISE EXCEPTION 'cannot downgrade while shelf document data exists';
 END IF;
END $$;
DROP TABLE shelf_document_versions;
