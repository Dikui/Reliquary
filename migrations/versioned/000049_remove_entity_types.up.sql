BEGIN;
ALTER TABLE public.entities DROP COLUMN entity_type;
ALTER TABLE public.entity_aliases DROP COLUMN entity_type;
ALTER TABLE public.entity_candidates DROP COLUMN entity_type;
-- Preserve same-name identities that previously had different types.
CREATE INDEX idx_entities_canonical ON public.entities(space_id,normalized_name) WHERE merged_into_id='';
COMMIT;
