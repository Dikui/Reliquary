-- Keep the first library's ID stable; additional owners receive deterministic IDs.
ALTER TABLE entities ADD COLUMN library_id varchar(36);
ALTER TABLE entity_aliases ADD COLUMN library_id varchar(36);
ALTER TABLE topics ADD COLUMN library_id varchar(36);
DROP INDEX idx_entities_canonical;
DROP INDEX idx_entity_aliases_alias;
ALTER TABLE topics DROP CONSTRAINT uq_topics_space_name;

CREATE TEMP TABLE entity_library_map ON COMMIT DROP AS
WITH RECURSIVE edges(a,b) AS (
 SELECT id,merged_into_id FROM entities WHERE merged_into_id<>''
 UNION SELECT keep_entity_id,merge_entity_id FROM entity_merge_records
 UNION SELECT new_entity_id,candidate_entity_id FROM entity_merge_suggestions
), both_edges(a,b) AS (SELECT a,b FROM edges UNION SELECT b,a FROM edges),
seeds(entity_id,library_id) AS (
 SELECT ee.entity_id,e.library_id FROM entry_entities ee JOIN entries e ON e.id=ee.entry_id
 UNION SELECT subject_id,library_id FROM relations
 UNION SELECT object_id,library_id FROM relations
 UNION SELECT subject_id,library_id FROM relation_suppressions
 UNION SELECT object_id,library_id FROM relation_suppressions
 UNION SELECT s.new_entity_id,e.library_id FROM entity_merge_suggestions s JOIN entries e ON e.id=s.entry_id
 UNION SELECT s.candidate_entity_id,e.library_id FROM entity_merge_suggestions s JOIN entries e ON e.id=s.entry_id
), scopes(entity_id,library_id) AS (
 SELECT * FROM seeds UNION SELECT b,s.library_id FROM scopes s JOIN both_edges ON a=s.entity_id
), owners AS (
 SELECT e.id AS old_id,s.library_id FROM entities e JOIN scopes s ON s.entity_id=e.id
 UNION SELECT e.id,l.id FROM entities e JOIN libraries l ON l.space_id=e.space_id
 WHERE NOT EXISTS (SELECT 1 FROM scopes s WHERE s.entity_id=e.id)
)
SELECT old_id,library_id,CASE WHEN row_number() OVER(PARTITION BY old_id ORDER BY library_id)=1 THEN old_id
 ELSE md5('entity:'||old_id||':'||library_id) END::varchar(36) AS new_id FROM owners;

CREATE TEMP TABLE topic_library_map ON COMMIT DROP AS
WITH owners AS (
 SELECT t.id AS old_id,l.id AS library_id FROM topics t JOIN libraries l ON l.space_id=t.space_id
 WHERE t.library_ids='[]'::jsonb OR t.library_ids @> jsonb_build_array(l.id)
 UNION SELECT et.topic_id,e.library_id FROM entry_topics et JOIN entries e ON e.id=et.entry_id
)
SELECT old_id,library_id,CASE WHEN row_number() OVER(PARTITION BY old_id ORDER BY library_id)=1 THEN old_id
 ELSE md5('topic:'||old_id||':'||library_id) END::varchar(36) AS new_id FROM owners;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM entities e WHERE NOT EXISTS(SELECT 1 FROM entity_library_map m WHERE m.old_id=e.id))
 OR EXISTS(SELECT 1 FROM topics t WHERE NOT EXISTS(SELECT 1 FROM topic_library_map m WHERE m.old_id=t.id)) THEN
  RAISE EXCEPTION 'Library isolation needs an existing library for every entity and topic; no records were removed';
 END IF;
END $$;

UPDATE entities e SET library_id=m.library_id FROM entity_library_map m WHERE m.old_id=e.id AND m.new_id=e.id;
INSERT INTO entities(id,space_id,library_id,canonical_name,normalized_name,entity_type,description,name_embedding,
 embedding_model_id,embedding_dimension,embedding_needs_rebuild,merged_into_id,created_at,updated_at)
SELECT m.new_id,e.space_id,m.library_id,e.canonical_name,e.normalized_name,e.entity_type,e.description,e.name_embedding,
 e.embedding_model_id,e.embedding_dimension,e.embedding_needs_rebuild,e.merged_into_id,e.created_at,e.updated_at
FROM entity_library_map m JOIN entities e ON e.id=m.old_id WHERE m.new_id<>m.old_id;
UPDATE entities e SET merged_into_id=m.new_id FROM entity_library_map m
WHERE e.merged_into_id=m.old_id AND e.library_id=m.library_id;

CREATE TEMP TABLE old_aliases ON COMMIT DROP AS TABLE entity_aliases;
UPDATE entity_aliases a SET library_id=e.library_id FROM entities e WHERE e.id=a.entity_id;
INSERT INTO entity_aliases(id,space_id,library_id,entity_type,entity_id,alias,normalized_alias,resolution_method,created_at)
SELECT md5('alias:'||a.id||':'||m.library_id),a.space_id,m.library_id,a.entity_type,m.new_id,a.alias,a.normalized_alias,a.resolution_method,a.created_at
FROM old_aliases a JOIN entity_library_map m ON m.old_id=a.entity_id WHERE m.new_id<>m.old_id;

UPDATE topics t SET library_id=m.library_id FROM topic_library_map m WHERE m.old_id=t.id AND m.new_id=t.id;
INSERT INTO topics(id,space_id,library_id,name,description,library_ids,created_at,updated_at)
SELECT m.new_id,t.space_id,m.library_id,t.name,t.description,t.library_ids,t.created_at,t.updated_at
FROM topic_library_map m JOIN topics t ON t.id=m.old_id WHERE m.new_id<>m.old_id;
UPDATE entry_entities ee SET entity_id=m.new_id FROM entries e,entity_library_map m
WHERE ee.entry_id=e.id AND ee.entity_id=m.old_id AND e.library_id=m.library_id;
UPDATE entry_topics et SET topic_id=m.new_id FROM entries e,topic_library_map m
WHERE et.entry_id=e.id AND et.topic_id=m.old_id AND e.library_id=m.library_id;
UPDATE relations r SET subject_id=m.new_id FROM entity_library_map m WHERE r.subject_id=m.old_id AND r.library_id=m.library_id;
UPDATE relations r SET object_id=m.new_id FROM entity_library_map m WHERE r.object_id=m.old_id AND r.library_id=m.library_id;
UPDATE relation_suppressions r SET subject_id=m.new_id FROM entity_library_map m WHERE r.subject_id=m.old_id AND r.library_id=m.library_id;
UPDATE relation_suppressions r SET object_id=m.new_id FROM entity_library_map m WHERE r.object_id=m.old_id AND r.library_id=m.library_id;
INSERT INTO follows(space_id,target_type,target_id,created_at)
SELECT f.space_id,f.target_type,m.new_id,f.created_at FROM follows f JOIN entity_library_map m ON f.target_type='entity' AND f.target_id=m.old_id
WHERE m.new_id<>m.old_id ON CONFLICT DO NOTHING;
INSERT INTO follows(space_id,target_type,target_id,created_at)
SELECT f.space_id,f.target_type,m.new_id,f.created_at FROM follows f JOIN topic_library_map m ON f.target_type='topic' AND f.target_id=m.old_id
WHERE m.new_id<>m.old_id ON CONFLICT DO NOTHING;

CREATE TEMP TABLE old_merge_records ON COMMIT DROP AS TABLE entity_merge_records;
INSERT INTO entity_merge_records(id,space_id,keep_entity_id,merge_entity_id,actor,created_at)
SELECT md5('merge:'||r.id||':'||a.library_id),r.space_id,a.new_id,b.new_id,r.actor,r.created_at
FROM old_merge_records r JOIN entity_library_map a ON a.old_id=r.keep_entity_id
JOIN entity_library_map b ON b.old_id=r.merge_entity_id AND a.library_id=b.library_id WHERE a.new_id<>a.old_id;
UPDATE entity_merge_suggestions s SET new_entity_id=a.new_id,candidate_entity_id=b.new_id
FROM entries e,entity_library_map a,entity_library_map b
WHERE s.entry_id=e.id AND a.old_id=s.new_entity_id AND b.old_id=s.candidate_entity_id
AND a.library_id=e.library_id AND b.library_id=e.library_id;

-- These are historical job snapshots. Keep their batches and decisions, but do
-- not resume a job whose input was collected across several libraries.
UPDATE topic_backfill_jobs j SET status='cancelled'
WHERE status IN ('pending','running','failed') AND EXISTS (
 SELECT 1 FROM topic_library_map m WHERE m.old_id=j.topic_id AND m.new_id<>m.old_id);

-- Remap only generated processing JSON, never the article body or user metadata.
CREATE OR REPLACE FUNCTION pg_temp.catalog_ids(value jsonb, owner varchar) RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE replacement text; result jsonb;
BEGIN
 IF jsonb_typeof(value)='string' THEN
  SELECT new_id INTO replacement FROM (SELECT * FROM entity_library_map UNION ALL SELECT * FROM topic_library_map) m
  WHERE m.old_id=value#>>'{}' AND m.library_id=owner LIMIT 1;
  RETURN CASE WHEN replacement IS NULL THEN value ELSE to_jsonb(replacement) END;
 ELSIF jsonb_typeof(value)='array' THEN
  SELECT coalesce(jsonb_agg(pg_temp.catalog_ids(v,owner) ORDER BY n),'[]'::jsonb) INTO result FROM jsonb_array_elements(value) WITH ORDINALITY a(v,n);
  RETURN result;
 ELSIF jsonb_typeof(value)='object' THEN
  SELECT coalesce(jsonb_object_agg(k,pg_temp.catalog_ids(v,owner)),'{}'::jsonb) INTO result FROM jsonb_each(value) a(k,v);
  RETURN result;
 END IF;
 RETURN value;
END $$;
UPDATE entry_processing_outputs SET relations=pg_temp.catalog_ids(relations,library_id);
UPDATE entry_segment_runs r SET plan=pg_temp.catalog_ids(r.plan,e.library_id),result=pg_temp.catalog_ids(r.result,e.library_id)
FROM entries e WHERE e.id=r.entry_id;
UPDATE relation_sources s SET evidence=pg_temp.catalog_ids(s.evidence,r.library_id) FROM relations r WHERE r.id=s.relation_id;

ALTER TABLE topics DROP COLUMN library_ids;
ALTER TABLE entities ALTER COLUMN library_id SET NOT NULL;
ALTER TABLE entity_aliases ALTER COLUMN library_id SET NOT NULL;
ALTER TABLE topics ALTER COLUMN library_id SET NOT NULL;
ALTER TABLE entities ADD CONSTRAINT entities_library_fk FOREIGN KEY(library_id) REFERENCES libraries(id);
ALTER TABLE topics ADD CONSTRAINT topics_library_fk FOREIGN KEY(library_id) REFERENCES libraries(id);
ALTER TABLE entities ADD CONSTRAINT entities_library_identity UNIQUE(space_id,library_id,id);
ALTER TABLE entity_aliases ADD CONSTRAINT aliases_library_entity_fk FOREIGN KEY(space_id,library_id,entity_id) REFERENCES entities(space_id,library_id,id) ON DELETE CASCADE;
CREATE UNIQUE INDEX idx_entities_canonical ON entities(space_id,library_id,entity_type,normalized_name);
CREATE UNIQUE INDEX idx_entity_aliases_alias ON entity_aliases(space_id,library_id,entity_type,normalized_alias);
ALTER TABLE topics ADD CONSTRAINT uq_topics_space_name UNIQUE(space_id,library_id,name);
ALTER TABLE relations ADD CONSTRAINT relations_subject_library_fk FOREIGN KEY(space_id,library_id,subject_id) REFERENCES entities(space_id,library_id,id);
ALTER TABLE relations ADD CONSTRAINT relations_object_library_fk FOREIGN KEY(space_id,library_id,object_id) REFERENCES entities(space_id,library_id,id);
ALTER TABLE relation_suppressions ADD CONSTRAINT suppressions_subject_library_fk FOREIGN KEY(space_id,library_id,subject_id) REFERENCES entities(space_id,library_id,id);
ALTER TABLE relation_suppressions ADD CONSTRAINT suppressions_object_library_fk FOREIGN KEY(space_id,library_id,object_id) REFERENCES entities(space_id,library_id,id);

CREATE FUNCTION public.check_catalog_library() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE entry_library varchar; owner varchar;
BEGIN
 SELECT library_id INTO entry_library FROM entries WHERE id=NEW.entry_id AND space_id=NEW.space_id;
 IF TG_TABLE_NAME='entry_entities' THEN
  SELECT library_id INTO owner FROM entities WHERE id=NEW.entity_id AND space_id=NEW.space_id;
 ELSE
  SELECT library_id INTO owner FROM topics WHERE id=NEW.topic_id AND space_id=NEW.space_id;
 END IF;
 IF entry_library IS NULL OR owner IS DISTINCT FROM entry_library THEN RAISE EXCEPTION 'catalog item must belong to the entry library'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER check_entity_library BEFORE INSERT OR UPDATE ON entry_entities FOR EACH ROW EXECUTE FUNCTION public.check_catalog_library();
CREATE TRIGGER check_topic_library BEFORE INSERT OR UPDATE ON entry_topics FOR EACH ROW EXECUTE FUNCTION public.check_catalog_library();
CREATE FUNCTION public.keep_catalog_library() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.library_id IS DISTINCT FROM OLD.library_id THEN RAISE EXCEPTION 'catalog library is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER keep_entity_library BEFORE UPDATE ON entities FOR EACH ROW EXECUTE FUNCTION public.keep_catalog_library();
CREATE TRIGGER keep_topic_library BEFORE UPDATE ON topics FOR EACH ROW EXECUTE FUNCTION public.keep_catalog_library();

-- Refresh both retained and new node IDs; the replica replaces their incident edges.
INSERT INTO graph_pending_sync(id,space_id,entity_id,relation_id,relation_version,deleted,created_at)
SELECT md5('catalog-sync:'||id),space_id,id,'',1,false,now() FROM entities;
