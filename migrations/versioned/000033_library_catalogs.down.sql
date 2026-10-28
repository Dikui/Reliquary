-- Development only: refuse a downgrade that would merge independent identities.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM entities GROUP BY space_id,entity_type,normalized_name HAVING count(*)>1)
 OR EXISTS(SELECT 1 FROM entity_aliases GROUP BY space_id,entity_type,normalized_alias HAVING count(*)>1)
 OR EXISTS(SELECT 1 FROM topics GROUP BY space_id,name HAVING count(*)>1) THEN
  RAISE EXCEPTION 'Cannot downgrade independent library catalogs without merging data';
 END IF;
END $$;
DROP TRIGGER check_entity_library ON entry_entities;
DROP TRIGGER check_topic_library ON entry_topics;
DROP FUNCTION public.check_catalog_library();
DROP TRIGGER keep_entity_library ON entities;
DROP TRIGGER keep_topic_library ON topics;
DROP FUNCTION public.keep_catalog_library();
ALTER TABLE relations DROP CONSTRAINT relations_subject_library_fk, DROP CONSTRAINT relations_object_library_fk;
ALTER TABLE relation_suppressions DROP CONSTRAINT suppressions_subject_library_fk, DROP CONSTRAINT suppressions_object_library_fk;
ALTER TABLE entity_aliases DROP CONSTRAINT aliases_library_entity_fk;
ALTER TABLE entities DROP CONSTRAINT entities_library_identity, DROP CONSTRAINT entities_library_fk;
ALTER TABLE topics DROP CONSTRAINT topics_library_fk;
DROP INDEX idx_entities_canonical;
DROP INDEX idx_entity_aliases_alias;
ALTER TABLE topics DROP CONSTRAINT uq_topics_space_name;
ALTER TABLE topics ADD COLUMN library_ids jsonb NOT NULL DEFAULT '[]'::jsonb;
UPDATE topics SET library_ids=jsonb_build_array(library_id);
ALTER TABLE topics DROP COLUMN library_id;
ALTER TABLE entity_aliases DROP COLUMN library_id;
ALTER TABLE entities DROP COLUMN library_id;
CREATE UNIQUE INDEX idx_entities_canonical ON entities(space_id,entity_type,normalized_name);
CREATE UNIQUE INDEX idx_entity_aliases_alias ON entity_aliases(space_id,entity_type,normalized_alias);
ALTER TABLE topics ADD CONSTRAINT uq_topics_space_name UNIQUE(space_id,name);
