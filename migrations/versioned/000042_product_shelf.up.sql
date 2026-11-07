ALTER TABLE libraries
    ADD COLUMN shelf_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN shelf_config jsonb NOT NULL DEFAULT '{}',
    ADD CONSTRAINT libraries_shelf_managed CHECK (NOT shelf_enabled OR entity_mode = 'managed');
