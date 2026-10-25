BEGIN;
ALTER TABLE public.entries ADD COLUMN grouping_manual boolean NOT NULL DEFAULT false;
ALTER TABLE public.facts
 ADD COLUMN manual_corrected boolean NOT NULL DEFAULT false,
 ADD COLUMN merged_into_id varchar(36) NOT NULL DEFAULT '' CHECK (merged_into_id <> id);
ALTER TABLE public.stories
 ADD COLUMN manual_corrected boolean NOT NULL DEFAULT false,
 ADD COLUMN merged_into_id varchar(36) NOT NULL DEFAULT '' CHECK (merged_into_id <> id);
COMMIT;
