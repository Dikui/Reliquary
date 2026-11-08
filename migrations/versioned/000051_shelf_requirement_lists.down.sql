DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shelf_document_versions WHERE jsonb_array_length(requirement_ids)>1)
 OR EXISTS(SELECT 1 FROM shelf_changesets WHERE items::text ~ '"(requirement_ids|list_name)"')
 OR EXISTS(SELECT 1 FROM entities WHERE kind='document_requirement'
   AND COALESCE(attributes #>> '{requirement,list_name}','') NOT IN ('','历史未分清单'))
 OR EXISTS(SELECT 1 FROM entity_attribute_versions v JOIN entities e ON e.id=v.entity_id
   WHERE e.kind='document_requirement' AND COALESCE(v.attributes #>> '{requirement,list_name}','') NOT IN ('','历史未分清单')) THEN
  RAISE EXCEPTION 'cannot downgrade while scoped requirement data exists';
 END IF;
END $$;
DROP INDEX entities_managed_name;
CREATE UNIQUE INDEX entities_managed_name ON entities(space_id,library_id,managed_kind,normalized_name)
 WHERE archived_at IS NULL AND merged_into_id='' AND managed_kind IS NOT NULL;
DROP INDEX entities_managed_code;
CREATE UNIQUE INDEX entities_managed_code ON entities(space_id,library_id,managed_kind,lower(code))
 WHERE code<>'' AND merged_into_id='' AND managed_kind IS NOT NULL;
UPDATE entity_attribute_versions v SET attributes=v.attributes #- '{requirement,list_name}'
 FROM entities e WHERE e.id=v.entity_id AND e.kind='document_requirement';
UPDATE entities SET attributes=attributes #- '{requirement,list_name}' WHERE kind='document_requirement';

DROP INDEX shelf_document_requirements;
DROP INDEX shelf_document_requirement_current;
ALTER TABLE shelf_document_versions DROP COLUMN requirement_ids;
CREATE UNIQUE INDEX shelf_document_requirement_current ON shelf_document_versions(owner_id,requirement_id)
 WHERE is_current AND requirement_id IS NOT NULL;
