BEGIN;
ALTER TABLE libraries ADD COLUMN prompt_overrides JSONB NOT NULL DEFAULT '{}';
ALTER TABLE libraries ADD CONSTRAINT libraries_prompt_overrides_object CHECK (jsonb_typeof(prompt_overrides) = 'object');
COMMIT;
