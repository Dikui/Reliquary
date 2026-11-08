-- Do not discard or rename certification data to restore the old constraint.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM entities WHERE managed_kind='certification')
 OR EXISTS(SELECT 1 FROM shelf_changesets WHERE items::text ~ '"(create_certification|update_certification)"') THEN
  RAISE EXCEPTION 'cannot downgrade while certification display-name data exists';
 END IF;
END $$;
DROP INDEX entities_managed_name;
CREATE UNIQUE INDEX entities_managed_name ON entities(space_id,library_id,managed_kind,
 (CASE WHEN managed_kind='document_requirement' THEN COALESCE(attributes #>> '{requirement,list_name}','') ELSE '' END),normalized_name)
 WHERE archived_at IS NULL AND merged_into_id='' AND managed_kind IS NOT NULL;
