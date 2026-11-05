DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM entities WHERE auto_match <> '{}'::jsonb) THEN
  RAISE EXCEPTION 'entities.auto_match contains saved choices; clear them explicitly before downgrading';
 END IF;
END $$;
ALTER TABLE entities DROP COLUMN auto_match;
