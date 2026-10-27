BEGIN;

DROP INDEX IF EXISTS public.entries_quality_status_idx;

ALTER TABLE public.entries
    DROP COLUMN IF EXISTS quality_status,
    DROP COLUMN IF EXISTS quality_score,
    DROP COLUMN IF EXISTS quality_score_first,
    DROP COLUMN IF EXISTS quality_score_second,
    DROP COLUMN IF EXISTS quality_scored_at;

COMMIT;
