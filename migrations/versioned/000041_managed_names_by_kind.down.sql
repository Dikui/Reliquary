DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM entity_name_confirmations) THEN
  RAISE EXCEPTION 'confirmed cross-kind names cannot be represented before migration 41';
 END IF;
 IF EXISTS(
  SELECT 1 FROM (
   SELECT e.space_id,e.library_id,e.kind,e.normalized_name AS name FROM entities e
    JOIN libraries l ON l.id=e.library_id WHERE l.entity_mode='managed' AND e.archived_at IS NULL AND e.merged_into_id=''
   UNION ALL
   SELECT e.space_id,e.library_id,e.kind,a.normalized_alias FROM entity_aliases a
    JOIN entities e ON e.id=a.entity_id JOIN libraries l ON l.id=e.library_id
    WHERE l.entity_mode='managed' AND e.archived_at IS NULL AND e.merged_into_id=''
   UNION ALL
   SELECT e.space_id,e.library_id,e.kind,lower(e.code) FROM entities e
    JOIN libraries l ON l.id=e.library_id WHERE l.entity_mode='managed' AND e.code<>'' AND e.merged_into_id=''
  ) names GROUP BY space_id,library_id,name HAVING count(DISTINCT kind)>1
 ) THEN
  RAISE EXCEPTION 'cross-kind names cannot be represented before migration 41';
 END IF;
END $$;
DROP TABLE entity_name_confirmations;
DROP TRIGGER libraries_managed_kind ON libraries;
DROP FUNCTION refresh_library_managed_kind();
DROP TRIGGER entities_managed_kind ON entities;
DROP FUNCTION set_entity_managed_kind();
DROP INDEX entities_managed_name;
DROP INDEX entities_managed_code;
DROP INDEX idx_entities_canonical;
DROP INDEX entities_library_code;
ALTER TABLE entities DROP COLUMN managed_kind;
CREATE UNIQUE INDEX idx_entities_canonical ON entities(space_id,library_id,entity_type,normalized_name)
 WHERE archived_at IS NULL AND merged_into_id='';
CREATE UNIQUE INDEX entities_library_code ON entities(space_id,library_id,lower(code)) WHERE code<>'' AND merged_into_id='';
