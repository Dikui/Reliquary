DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM relations r JOIN libraries l ON l.id=r.library_id
  WHERE l.shelf_enabled AND r.relation_type='composed_of' AND r.attributes ?| ARRAY['cluster_model','container_model'])
 OR EXISTS(SELECT 1 FROM shelf_changesets WHERE items::text ~ '"(cluster_model|container_model)"[[:space:]]*:') THEN
  RAISE EXCEPTION 'cannot downgrade while shelf composition model data exists';
 END IF;
END $$;

UPDATE library_relation_types r SET fields=COALESCE((SELECT jsonb_agg(f ORDER BY position)
 FROM jsonb_array_elements(r.fields) WITH ORDINALITY AS defs(f,position)
 WHERE f->>'key' NOT IN ('cluster_model','container_model')),'[]'::jsonb)
FROM libraries l WHERE l.id=r.library_id AND l.shelf_enabled AND r.name='composed_of';

UPDATE libraries l SET entity_schema=jsonb_set(l.entity_schema,'{fields}',(
 SELECT jsonb_agg(CASE WHEN f->>'key' IN ('cluster_model','container_model')
  THEN jsonb_set(f,'{archived}','false'::jsonb) ELSE f END ORDER BY position)
 FROM jsonb_array_elements(l.entity_schema->'fields') WITH ORDINALITY AS defs(f,position)
)) WHERE l.shelf_enabled;
