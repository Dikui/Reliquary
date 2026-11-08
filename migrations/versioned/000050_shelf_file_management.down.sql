DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM shelf_document_versions WHERE owner_id IS NULL)
 OR EXISTS (SELECT 1 FROM shelf_document_selections)
 OR EXISTS (SELECT 1 FROM shelf_changesets WHERE items::text ~ '"(classify_document|select_document_version|unassigned)"') THEN
  RAISE EXCEPTION 'cannot downgrade while shelf file management data exists';
 END IF;
END $$;
DROP TABLE shelf_document_selections;
ALTER TABLE shelf_document_versions ALTER COLUMN owner_id SET NOT NULL;
