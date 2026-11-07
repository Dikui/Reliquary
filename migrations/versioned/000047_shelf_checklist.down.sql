DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM entities e JOIN libraries l ON l.id=e.library_id WHERE l.shelf_enabled AND (e.attributes ? 'requirement' OR e.attributes ? 'current_stage'))
 OR EXISTS (SELECT 1 FROM entity_attribute_versions v JOIN entities e ON e.id=v.entity_id JOIN libraries l ON l.id=e.library_id WHERE l.shelf_enabled AND (v.attributes ? 'requirement' OR v.attributes ? 'current_stage'))
 OR EXISTS (SELECT 1 FROM relations r JOIN libraries l ON l.id=r.library_id WHERE l.shelf_enabled AND r.relation_type='applies_to' AND r.subject_id=r.object_id)
 OR EXISTS (SELECT 1 FROM shelf_changesets WHERE jsonb_path_exists(items,'$.**.attributes.requirement') OR jsonb_path_exists(items,'$.**.attributes.current_stage') OR jsonb_path_exists(items,'$.**.relation.attributes.module_types')) THEN
  RAISE EXCEPTION 'cannot downgrade while shelf checklist data exists';
 END IF;
END $$;
UPDATE libraries l SET entity_schema=jsonb_set(l.entity_schema,'{fields}',COALESCE((SELECT jsonb_agg(f ORDER BY position)
 FROM jsonb_array_elements(l.entity_schema->'fields') WITH ORDINALITY fields(f,position) WHERE f->>'key' NOT IN ('requirement','current_stage')),'[]')) WHERE l.shelf_enabled;
UPDATE library_relation_types r SET object_kinds='["product_line"]',fields=COALESCE((SELECT jsonb_agg(f ORDER BY pos)
 FROM jsonb_array_elements(r.fields) WITH ORDINALITY a(f,pos) WHERE f->>'key'<>'module_types'),'[]')
 FROM libraries l WHERE l.id=r.library_id AND l.shelf_enabled AND r.name='applies_to';
ALTER TABLE relations DROP CONSTRAINT relations_check;
ALTER TABLE relations ADD CONSTRAINT relations_check CHECK (subject_id<>object_id);
