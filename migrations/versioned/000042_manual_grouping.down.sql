BEGIN;
ALTER TABLE public.stories DROP COLUMN merged_into_id, DROP COLUMN manual_corrected;
ALTER TABLE public.facts DROP COLUMN merged_into_id, DROP COLUMN manual_corrected;
ALTER TABLE public.entries DROP COLUMN grouping_manual;
COMMIT;
