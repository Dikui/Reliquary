DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM entities e JOIN libraries l ON l.id=e.library_id WHERE l.shelf_enabled AND e.attributes ? 'certification')
       OR EXISTS (SELECT 1 FROM entity_attribute_versions v JOIN entities e ON e.id=v.entity_id JOIN libraries l ON l.id=e.library_id WHERE l.shelf_enabled AND v.attributes ? 'certification')
       OR EXISTS (SELECT 1 FROM libraries WHERE shelf_enabled AND COALESCE(shelf_config->'standards','[]'::jsonb)<>'[]'::jsonb)
       OR EXISTS (SELECT 1 FROM shelf_changesets WHERE items::text ~ '"(create_certification|update_certification)"'
                  OR jsonb_path_exists(items,'$[*].operation.library.shelf_config.standards')
                  OR jsonb_path_exists(items,'$[*].operation.entity.attributes.certification')
                  OR jsonb_path_exists(items,'$[*].operation.patch.attributes.certification')) THEN
        RAISE EXCEPTION 'cannot downgrade while shelf certification data exists';
    END IF;
END $$;
UPDATE libraries l SET entity_schema=jsonb_set(l.entity_schema,'{fields}',COALESCE((
    SELECT jsonb_agg(f ORDER BY position)
    FROM jsonb_array_elements(l.entity_schema->'fields') WITH ORDINALITY AS fields(f,position)
    WHERE f->>'key'<>'certification'
),'[]'::jsonb)) WHERE l.shelf_enabled;
