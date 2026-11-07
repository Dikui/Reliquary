-- Add an empty certification field definition; existing catalog facts are unchanged.
DO $$
DECLARE definition jsonb := '{"key":"certification","name":"认证记录","type":"shelf_certification","kinds":["certification"],"archived":false}';
BEGIN
    IF EXISTS (
        SELECT 1 FROM libraries l,
        jsonb_array_elements(COALESCE(l.entity_schema->'fields','[]'::jsonb)) f
        WHERE l.shelf_enabled AND f->>'key'='certification'
        AND (f->>'type' IS DISTINCT FROM definition->>'type' OR f->'kinds' IS DISTINCT FROM definition->'kinds'
             OR f->>'archived'='true' OR COALESCE(f->'product_line_ids','[]'::jsonb)<>'[]'::jsonb)
    ) THEN
        RAISE EXCEPTION 'shelf certification field conflicts must be resolved before upgrading';
    END IF;
    UPDATE libraries l SET entity_schema=jsonb_set(l.entity_schema,'{fields}',COALESCE(l.entity_schema->'fields','[]'::jsonb) || jsonb_build_array(definition))
    WHERE l.shelf_enabled AND NOT EXISTS (
        SELECT 1 FROM jsonb_array_elements(COALESCE(l.entity_schema->'fields','[]'::jsonb)) f WHERE f->>'key'='certification'
    );
END $$;
