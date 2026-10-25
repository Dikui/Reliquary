BEGIN;
UPDATE public.model_calls SET purpose='article_processing' WHERE purpose='entry_understanding';
ALTER TABLE public.model_calls DROP CONSTRAINT model_calls_purpose_check;
ALTER TABLE public.model_calls ADD CONSTRAINT model_calls_purpose_check
 CHECK (purpose IN ('intake_review','article_processing','segment_merge','image_description','entity_resolution',
 'entity_name_embedding','body_embedding','rerank','evaluation_generation','other'));
ALTER TABLE public.entries DROP COLUMN description_manual, DROP COLUMN title_manual;
COMMIT;
