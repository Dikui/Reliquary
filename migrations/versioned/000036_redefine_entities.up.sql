BEGIN;
-- Old identities, mentions and follows were explicitly retired by ADR 0016.
DELETE FROM public.follows WHERE target_type='entity';
DELETE FROM public.entry_entities;
DELETE FROM public.entity_aliases;
DELETE FROM public.entity_merge_records;
DROP TABLE public.entity_merge_suggestions;
DROP TABLE public.entity_embedding_rebuild_runs;
DELETE FROM public.entities;
ALTER TABLE public.entities DROP COLUMN description, DROP COLUMN name_embedding,
 DROP COLUMN embedding_model_id, DROP COLUMN embedding_dimension, DROP COLUMN embedding_needs_rebuild;
ALTER TABLE public.entities DROP CONSTRAINT entities_entity_type_check;
ALTER TABLE public.entities ALTER COLUMN entity_type DROP DEFAULT;
ALTER TABLE public.entities ADD CONSTRAINT entities_entity_type_check CHECK (entity_type IN ('organization','person'));
DROP INDEX public.idx_entities_canonical;
CREATE UNIQUE INDEX idx_entities_canonical ON public.entities(space_id,entity_type,normalized_name) WHERE merged_into_id='';
DROP INDEX public.idx_entity_aliases_alias;
CREATE UNIQUE INDEX idx_entity_aliases_alias ON public.entity_aliases(space_id,entity_id,normalized_alias);
ALTER TABLE public.entries ADD COLUMN entities_manual boolean NOT NULL DEFAULT false;
CREATE TABLE public.entity_seed_imports (
 space_id bigint PRIMARY KEY,
 imported_at timestamptz NOT NULL
);
UPDATE public.libraries SET model_tiers=model_tiers-'entity_resolution';
DELETE FROM public.task_pending_ops WHERE task_type='entity:name_embedding_rebuild';
DELETE FROM public.embedding_rebuild_items WHERE kind='entity';
DELETE FROM public.embedding_rebuild_vectors WHERE kind='entity';
UPDATE public.embedding_vector_archives SET payload=payload-'entities' WHERE payload ? 'entities';
-- Saved checkpoints must not restore retired open extraction payloads.
DELETE FROM public.entry_segment_runs WHERE phase='processing';
COMMIT;
