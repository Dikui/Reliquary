DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM shelf_import_plans)
       OR EXISTS (SELECT 1 FROM shelf_changesets WHERE source->>'import_plan_key' IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot downgrade while shelf import plans or batches exist';
    END IF;
END $$;
DROP TABLE shelf_import_plans;
