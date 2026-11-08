-- Retain legacy part values and history without guessing model ownership.
UPDATE libraries l SET entity_schema=jsonb_set(l.entity_schema,'{fields}',(
 SELECT jsonb_agg(CASE WHEN f->>'key' IN ('cluster_model','container_model')
   THEN jsonb_set(f,'{archived}','true'::jsonb) ELSE f END ORDER BY position)
 FROM jsonb_array_elements(l.entity_schema->'fields') WITH ORDINALITY AS defs(f,position)
)) WHERE l.shelf_enabled;

DO $$
DECLARE defaults jsonb := '[{"key":"cluster_model","name":"簇型号","type":"text","kinds":[],"archived":false},{"key":"container_model","name":"集装箱型号","type":"text","kinds":[],"archived":false}]';
BEGIN
 IF EXISTS (
  SELECT 1 FROM library_relation_types r JOIN libraries l ON l.id=r.library_id,
  jsonb_array_elements(r.fields) f, jsonb_array_elements(defaults) d
  WHERE l.shelf_enabled AND r.name='composed_of' AND f->>'key'=d->>'key'
  AND (f->>'type' IS DISTINCT FROM 'text' OR COALESCE(f->'kinds','[]'::jsonb)<>'[]'::jsonb
   OR f->>'archived'='true' OR COALESCE(f->'product_line_ids','[]'::jsonb)<>'[]'::jsonb)
 ) THEN
  RAISE EXCEPTION 'shelf composition field conflicts must be resolved before upgrading';
 END IF;
 UPDATE library_relation_types r SET fields=COALESCE(r.fields,'[]'::jsonb) || COALESCE((
  SELECT jsonb_agg(d ORDER BY position) FROM jsonb_array_elements(defaults) WITH ORDINALITY AS defs(d,position)
  WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(r.fields) f WHERE f->>'key'=d->>'key')
 ),'[]'::jsonb) FROM libraries l WHERE l.id=r.library_id AND l.shelf_enabled AND r.name='composed_of';
END $$;
