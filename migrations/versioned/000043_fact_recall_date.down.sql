BEGIN;
ALTER TABLE public.facts DROP COLUMN recall_date;
COMMIT;
