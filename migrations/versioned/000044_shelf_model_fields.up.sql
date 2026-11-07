-- Install empty shelf fields without rewriting catalog facts or history.
DO $$
DECLARE defaults jsonb := '[{"key": "module_type", "name": "模块类型", "type": "text", "kinds": ["part"], "archived": false}, {"key": "pack_model", "name": "Pack 型号", "type": "text", "kinds": ["part"], "archived": false}, {"key": "cluster_model", "name": "簇型号", "type": "text", "kinds": ["part"], "archived": false}, {"key": "container_model", "name": "集装箱型号", "type": "text", "kinds": ["part"], "archived": false}, {"key": "sale_statuses", "name": "可售状态", "type": "shelf_statuses", "kinds": ["model"], "archived": false}, {"key": "combination_rules", "name": "组合规则", "type": "shelf_rules", "kinds": ["model"], "archived": false}]';
BEGIN
    IF EXISTS (
        SELECT 1 FROM libraries l,
        jsonb_array_elements(COALESCE(l.entity_schema->'fields','[]'::jsonb)) f,
        jsonb_array_elements(defaults) d
        WHERE l.shelf_enabled AND f->>'key'=d->>'key'
        AND (f->>'type' IS DISTINCT FROM d->>'type' OR f->'kinds' IS DISTINCT FROM d->'kinds'
             OR f->>'archived'='true'
             OR COALESCE(f->'product_line_ids','[]'::jsonb)<>'[]'::jsonb)
    ) THEN
        RAISE EXCEPTION 'shelf field conflicts must be resolved before upgrading';
    END IF;
    UPDATE libraries l SET entity_schema=jsonb_set(l.entity_schema,'{fields}',
        COALESCE(l.entity_schema->'fields','[]'::jsonb) || COALESCE((
            SELECT jsonb_agg(d ORDER BY position)
            FROM jsonb_array_elements(defaults) WITH ORDINALITY AS defs(d,position)
            WHERE NOT EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(l.entity_schema->'fields','[]'::jsonb)) f WHERE f->>'key'=d->>'key')
        ),'[]'::jsonb)) WHERE l.shelf_enabled;
END $$;
