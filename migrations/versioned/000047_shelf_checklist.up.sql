-- Add empty definitions without inferring any existing business values.
DO $$
DECLARE definition jsonb;
BEGIN
 FOR definition IN SELECT value FROM jsonb_array_elements('[
 {"key":"current_stage","name":"当前阶段","type":"text","kinds":["product","model"],"archived":false},
 {"key":"requirement","name":"资料要求","type":"shelf_requirement","kinds":["document_requirement"],"archived":false}
 ]'::jsonb) LOOP
  IF EXISTS (SELECT 1 FROM libraries l,jsonb_array_elements(COALESCE(l.entity_schema->'fields','[]')) f
   WHERE l.shelf_enabled AND f->>'key'=definition->>'key'
   AND (f->>'type' IS DISTINCT FROM definition->>'type' OR f->'kinds' IS DISTINCT FROM definition->'kinds'
    OR f->>'archived'='true' OR COALESCE(f->'product_line_ids','[]')<>'[]'::jsonb)) THEN
   RAISE EXCEPTION 'shelf checklist field conflicts must be resolved before upgrading';
  END IF;
  UPDATE libraries l SET entity_schema=jsonb_set(l.entity_schema,'{fields}',COALESCE(l.entity_schema->'fields','[]')||jsonb_build_array(definition))
  WHERE l.shelf_enabled AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(l.entity_schema->'fields','[]')) f WHERE f->>'key'=definition->>'key');
 END LOOP;
END $$;
DO $$
DECLARE definition jsonb := '{"key":"module_types","name":"适用模块类型","type":"list","kinds":[],"archived":false}';
BEGIN
 IF EXISTS (SELECT 1 FROM library_relation_types r JOIN libraries l ON l.id=r.library_id,
 jsonb_array_elements(COALESCE(r.fields,'[]')) f WHERE l.shelf_enabled AND r.name='applies_to' AND f->>'key'='module_types'
 AND (f->>'type' IS DISTINCT FROM 'list' OR COALESCE(f->'kinds','[]')<>'[]'::jsonb OR f->>'archived'='true')) THEN
 RAISE EXCEPTION 'shelf requirement module field conflicts must be resolved before upgrading';
 END IF;
 UPDATE library_relation_types r SET object_kinds='["product_line","document_requirement"]',
 fields=CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(r.fields,'[]')) f WHERE f->>'key'='module_types')
 THEN r.fields ELSE COALESCE(r.fields,'[]')||jsonb_build_array(definition) END
 FROM libraries l WHERE l.id=r.library_id AND l.shelf_enabled AND r.name='applies_to';
END $$;
ALTER TABLE relations DROP CONSTRAINT relations_check;
ALTER TABLE relations ADD CONSTRAINT relations_check CHECK (subject_id<>object_id OR relation_type='applies_to');
