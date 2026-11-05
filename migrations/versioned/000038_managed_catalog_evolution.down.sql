DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM entity_attribute_versions) OR EXISTS(SELECT 1 FROM entity_reorganizations)
 OR EXISTS(SELECT 1 FROM entity_attachments) OR EXISTS(SELECT 1 FROM relations WHERE attributes<>'{}')
 OR EXISTS(SELECT 1 FROM entry_entities WHERE relation_type<>'references')
 OR EXISTS(SELECT 1 FROM libraries WHERE product_library_ids<>'[]' OR document_relation_types<>'["references","specification","test_report","comparison"]')
 OR EXISTS(SELECT 1 FROM library_relation_types WHERE fields<>'[]')
 OR EXISTS(SELECT 1 FROM entry_entities ee JOIN entries d ON d.id=ee.entry_id JOIN entities e ON e.id=ee.entity_id WHERE d.library_id<>e.library_id)
 THEN RAISE EXCEPTION 'managed evolution contains data; export and explicitly resolve it before downgrading'; END IF;
END $$;
CREATE OR REPLACE FUNCTION public.check_catalog_library() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE entry_library varchar(36); owner_library varchar(36);
BEGIN
 SELECT library_id INTO entry_library FROM entries WHERE id=NEW.entry_id AND space_id=NEW.space_id;
 IF TG_TABLE_NAME='entry_entities' THEN
  SELECT library_id INTO owner_library FROM entities WHERE id=NEW.entity_id AND space_id=NEW.space_id;
 ELSE
  SELECT library_id INTO owner_library FROM topics WHERE id=NEW.topic_id AND space_id=NEW.space_id;
 END IF;
 IF entry_library IS NULL OR owner_library IS DISTINCT FROM entry_library THEN RAISE EXCEPTION 'catalog association must belong to entry library'; END IF;
 RETURN NEW;
END $$;
DROP TABLE entity_attachments, entity_reorganizations, entity_attribute_versions;
ALTER TABLE entry_entities DROP COLUMN relation_type;
ALTER TABLE relations DROP COLUMN attributes;
ALTER TABLE library_relation_types DROP COLUMN fields;
ALTER TABLE libraries DROP COLUMN product_library_ids, DROP COLUMN document_relation_types;
