BEGIN;
ALTER TABLE libraries DROP COLUMN prompt_overrides;
COMMIT;
