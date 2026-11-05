DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM relations WHERE valid_to IS NOT NULL OR valid_from<>updated_at) THEN
  RAISE EXCEPTION 'relationship history cannot be represented before migration 40';
 END IF;
 IF EXISTS(SELECT 1 FROM entities GROUP BY space_id,library_id,entity_type,normalized_name HAVING count(*)>1) OR
    EXISTS(SELECT 1 FROM entity_aliases GROUP BY space_id,library_id,entity_type,normalized_alias HAVING count(*)>1) OR
    EXISTS(SELECT 1 FROM entities WHERE code<>'' AND merged_into_id='' GROUP BY space_id,library_id,code HAVING count(*)>1) THEN
  RAISE EXCEPTION 'reused archived names or codes cannot be represented before migration 40';
 END IF;
END $$;
DROP INDEX relations_active_triple;
ALTER TABLE relations ADD CONSTRAINT relations_library_id_subject_id_relation_type_object_id_key UNIQUE(library_id,subject_id,relation_type,object_id);
ALTER TABLE relations DROP COLUMN valid_to;
ALTER TABLE relations DROP COLUMN valid_from;
DROP INDEX entities_library_code;
CREATE UNIQUE INDEX entities_library_code ON entities(space_id,library_id,code) WHERE code<>'' AND merged_into_id='';
DROP INDEX idx_entities_canonical;
CREATE UNIQUE INDEX idx_entities_canonical ON entities(space_id,library_id,entity_type,normalized_name);
DROP INDEX idx_entity_aliases_alias;
CREATE UNIQUE INDEX idx_entity_aliases_alias ON entity_aliases(space_id,library_id,entity_type,normalized_alias);
