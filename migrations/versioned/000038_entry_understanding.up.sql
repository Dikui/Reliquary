BEGIN;
ALTER TABLE public.entries
 ADD COLUMN title_manual boolean NOT NULL DEFAULT false,
 ADD COLUMN description_manual boolean NOT NULL DEFAULT false;
ALTER TABLE public.model_calls DROP CONSTRAINT model_calls_purpose_check;
ALTER TABLE public.model_calls ADD CONSTRAINT model_calls_purpose_check
 CHECK (purpose IN ('intake_review','article_processing','entry_understanding','segment_merge',
 'image_description','entity_resolution','entity_name_embedding','body_embedding','rerank','evaluation_generation','other'));
COMMIT;
