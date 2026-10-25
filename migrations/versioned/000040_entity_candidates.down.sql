BEGIN;
DROP TABLE public.entity_candidate_occurrences;
DROP TABLE public.entity_candidates;
ALTER TABLE public.facts DROP COLUMN subject_entity_id, DROP COLUMN object_entity_id;
COMMIT;
