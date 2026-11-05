-- Reject collisions without choosing a winner or changing user data.
DO $$
DECLARE conflicts text;
BEGIN
 SELECT string_agg(library_id || ':' || code || ' [' || ids || ']', '; ' ORDER BY library_id,code)
 INTO conflicts FROM (
  SELECT library_id,lower(code) AS code,string_agg(id,',' ORDER BY id) AS ids
  FROM entities WHERE code<>'' AND merged_into_id=''
  GROUP BY library_id,lower(code) HAVING count(*)>1
 ) duplicates;
 IF conflicts IS NOT NULL THEN
  RAISE EXCEPTION 'case-insensitive product codes conflict: %', conflicts;
 END IF;
 SELECT string_agg(library_id || ':' || normalized_name || ' [' || ids || ']', '; ' ORDER BY library_id,normalized_name)
 INTO conflicts FROM (
  SELECT e.library_id,e.normalized_name,string_agg(e.id,',' ORDER BY e.id) AS ids
  FROM entities e JOIN libraries l ON l.id=e.library_id
  WHERE l.entity_mode='managed' AND e.merged_into_id='' AND e.archived_at IS NULL
  GROUP BY e.library_id,e.normalized_name HAVING count(*)>1
 ) duplicates;
 IF conflicts IS NOT NULL THEN
  RAISE EXCEPTION 'managed product names conflict: %', conflicts;
 END IF;
END $$;

ALTER TABLE relations ADD COLUMN valid_from timestamptz;
ALTER TABLE relations ADD COLUMN valid_to timestamptz;
-- The last saved structure is the earliest state we can establish reliably.
UPDATE relations SET valid_from=updated_at;
ALTER TABLE relations ALTER COLUMN valid_from SET DEFAULT now();
ALTER TABLE relations ALTER COLUMN valid_from SET NOT NULL;
ALTER TABLE relations ADD CONSTRAINT relations_valid_interval CHECK(valid_to IS NULL OR valid_to>=valid_from);
ALTER TABLE relations DROP CONSTRAINT relations_library_id_subject_id_relation_type_object_id_key;
CREATE UNIQUE INDEX relations_active_triple ON relations(library_id,subject_id,relation_type,object_id) WHERE valid_to IS NULL;

DROP INDEX entities_library_code;
CREATE UNIQUE INDEX entities_library_code ON entities(space_id,library_id,lower(code)) WHERE code<>'' AND merged_into_id='';
DROP INDEX idx_entities_canonical;
CREATE UNIQUE INDEX idx_entities_canonical ON entities(space_id,library_id,entity_type,normalized_name) WHERE archived_at IS NULL AND merged_into_id='';
-- Alias ownership survives archival; active-owner conflict checks run under the
-- existing identity lock, while this index prevents duplicate owner spellings.
DROP INDEX idx_entity_aliases_alias;
CREATE UNIQUE INDEX idx_entity_aliases_alias ON entity_aliases(space_id,library_id,entity_id,normalized_alias);
