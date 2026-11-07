DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM entities e JOIN libraries l ON l.id=e.library_id WHERE l.shelf_enabled
               AND e.attributes ?| ARRAY['module_type','pack_model','cluster_model','container_model','sale_statuses','combination_rules'])
       OR EXISTS (SELECT 1 FROM entity_attribute_versions v JOIN entities e ON e.id=v.entity_id JOIN libraries l ON l.id=e.library_id WHERE l.shelf_enabled
                  AND v.attributes ?| ARRAY['module_type','pack_model','cluster_model','container_model','sale_statuses','combination_rules'])
       OR EXISTS (SELECT 1 FROM libraries l,jsonb_array_elements(COALESCE(l.entity_schema->'fields','[]'::jsonb)) f WHERE l.shelf_enabled
                  AND jsonb_array_length(COALESCE(f->'product_line_ids','[]'::jsonb))>0)
       OR EXISTS (SELECT 1 FROM shelf_changesets WHERE items::text ~ '"(set_sale_statuses|set_parameter_values)"'
                  OR items::text ~ '"(sale_statuses|parameter_values|pack_model|cluster_model|container_model|combination_rules|product_line_ids)"[[:space:]]*:'
                  OR jsonb_path_exists(items,'$[*].operation.entity.attributes.module_type')
                  OR jsonb_path_exists(items,'$[*].operation.patch.attributes.module_type')) THEN
        RAISE EXCEPTION 'cannot downgrade while shelf model data exists';
    END IF;
END $$;
UPDATE libraries l SET entity_schema=jsonb_set(l.entity_schema,'{fields}',COALESCE((
    SELECT jsonb_agg(f ORDER BY position)
    FROM jsonb_array_elements(l.entity_schema->'fields') WITH ORDINALITY AS fields(f,position)
    WHERE f->>'key' NOT IN ('module_type','pack_model','cluster_model','container_model','sale_statuses','combination_rules')
),'[]'::jsonb)) WHERE l.shelf_enabled;
