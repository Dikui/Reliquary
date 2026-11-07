DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM shelf_changesets)
       OR EXISTS (SELECT 1 FROM operation_logs WHERE source <> '{}'::jsonb OR changeset_id <> '') THEN
        RAISE EXCEPTION 'cannot downgrade while shelf changesets or operation sources exist';
    END IF;
END $$;
DROP INDEX operation_logs_source;
ALTER TABLE operation_logs DROP COLUMN source, DROP COLUMN changeset_id;
DROP TABLE shelf_changesets;
