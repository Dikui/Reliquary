DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM libraries WHERE shelf_enabled OR shelf_config <> '{}'::jsonb) THEN
        RAISE EXCEPTION 'cannot downgrade while product shelf data exists';
    END IF;
END $$;

ALTER TABLE libraries
    DROP CONSTRAINT libraries_shelf_managed,
    DROP COLUMN shelf_config,
    DROP COLUMN shelf_enabled;
