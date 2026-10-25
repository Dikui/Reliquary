BEGIN;
-- Keep the last known publication date when source evidence is withdrawn.
ALTER TABLE public.facts ADD COLUMN recall_date date;
COMMIT;
