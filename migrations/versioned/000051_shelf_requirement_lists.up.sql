-- Existing identities retain their IDs, names, metadata and history. No list
-- ownership is guessed from an old requirement name or source label.
UPDATE entities SET attributes=jsonb_set(attributes,'{requirement}',
 COALESCE(attributes->'requirement','{}'::jsonb) || '{"list_name":"历史未分清单"}'::jsonb)
 WHERE kind='document_requirement' AND COALESCE(attributes #>> '{requirement,list_name}','')='';
UPDATE entity_attribute_versions v SET attributes=jsonb_set(v.attributes,'{requirement}',
 COALESCE(v.attributes->'requirement','{}'::jsonb) || '{"list_name":"历史未分清单"}'::jsonb)
 FROM entities e WHERE e.id=v.entity_id AND e.kind='document_requirement'
 AND COALESCE(v.attributes #>> '{requirement,list_name}','')='';

DROP INDEX entities_managed_name;
CREATE UNIQUE INDEX entities_managed_name ON entities(space_id,library_id,managed_kind,
 (CASE WHEN managed_kind='document_requirement' THEN COALESCE(attributes #>> '{requirement,list_name}','') ELSE '' END),normalized_name)
 WHERE archived_at IS NULL AND merged_into_id='' AND managed_kind IS NOT NULL;
DROP INDEX entities_managed_code;
CREATE UNIQUE INDEX entities_managed_code ON entities(space_id,library_id,managed_kind,
 (CASE WHEN managed_kind='document_requirement' THEN COALESCE(attributes #>> '{requirement,list_name}','') ELSE '' END),lower(code))
 WHERE code<>'' AND merged_into_id='' AND managed_kind IS NOT NULL;

ALTER TABLE shelf_document_versions ADD COLUMN requirement_ids jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(requirement_ids)='array');
UPDATE shelf_document_versions SET requirement_ids=jsonb_build_array(requirement_id) WHERE requirement_id IS NOT NULL;
DROP INDEX shelf_document_requirement_current;
CREATE UNIQUE INDEX shelf_document_requirement_current ON shelf_document_versions(owner_id,requirement_ids)
 WHERE is_current AND jsonb_array_length(requirement_ids)>0;
CREATE INDEX shelf_document_requirements ON shelf_document_versions USING gin(requirement_ids);
