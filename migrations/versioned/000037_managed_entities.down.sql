DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM libraries WHERE entity_mode='managed') THEN
  RAISE EXCEPTION 'managed libraries must be retained; restore a compatible backup to downgrade';
 END IF;
END $$;
ALTER TABLE entry_entities DROP COLUMN body_hash, DROP COLUMN evidence, DROP COLUMN excluded, DROP COLUMN origin;
ALTER TABLE library_relation_types DROP COLUMN object_kinds, DROP COLUMN subject_kinds, DROP COLUMN hierarchical;
DROP INDEX entities_library_code;
ALTER TABLE entities DROP COLUMN archived_at, DROP COLUMN revision, DROP COLUMN attributes, DROP COLUMN code, DROP COLUMN kind;
ALTER TABLE libraries DROP COLUMN entity_schema, DROP COLUMN entity_mode;
