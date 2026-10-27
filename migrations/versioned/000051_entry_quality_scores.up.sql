BEGIN;

ALTER TABLE public.entries
    ADD COLUMN IF NOT EXISTS quality_status varchar(16) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS quality_score smallint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS quality_score_first smallint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS quality_score_second smallint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS quality_scored_at timestamptz;

CREATE INDEX IF NOT EXISTS entries_quality_status_idx ON public.entries (space_id, quality_status);

COMMIT;
