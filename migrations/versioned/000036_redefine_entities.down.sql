BEGIN;
-- Downgrade restores schema only. Deleted legacy identities and vectors are lost.
DELETE FROM public.entity_aliases;
DELETE FROM public.entry_entities;
DELETE FROM public.follows WHERE target_type='entity';
DELETE FROM public.entity_merge_records;
DELETE FROM public.entities;
DROP TABLE public.entity_seed_imports;
ALTER TABLE public.entries DROP COLUMN entities_manual;
ALTER TABLE public.entities DROP CONSTRAINT entities_entity_type_check;
ALTER TABLE public.entities ALTER COLUMN entity_type SET DEFAULT 'other';
ALTER TABLE public.entities ADD CONSTRAINT entities_entity_type_check CHECK (entity_type IN ('company','person','product','project','organization','other'));
ALTER TABLE public.entities ADD COLUMN description text NOT NULL DEFAULT '',
 ADD COLUMN name_embedding public.vector,
 ADD COLUMN embedding_model_id varchar(64) NOT NULL DEFAULT '',
 ADD COLUMN embedding_dimension integer NOT NULL DEFAULT 0,
 ADD COLUMN embedding_needs_rebuild boolean NOT NULL DEFAULT true;
DROP INDEX public.idx_entities_canonical;
CREATE UNIQUE INDEX idx_entities_canonical ON public.entities(space_id,entity_type,normalized_name);
DROP INDEX public.idx_entity_aliases_alias;
CREATE UNIQUE INDEX idx_entity_aliases_alias ON public.entity_aliases(space_id,entity_type,normalized_alias);
CREATE INDEX idx_entities_embedding_profile ON public.entities(space_id,embedding_model_id,embedding_dimension) WHERE name_embedding IS NOT NULL;
CREATE INDEX idx_entities_embedding_pending ON public.entities(space_id,id) WHERE embedding_needs_rebuild AND merged_into_id='';
CREATE TABLE public.entity_merge_suggestions (
    id character varying(36) PRIMARY KEY,
    space_id bigint NOT NULL,
    new_entity_id character varying(36) NOT NULL,
    candidate_entity_id character varying(36) NOT NULL,
    entry_id character varying(36) NOT NULL,
    reason text NOT NULL,
    status character varying(16) DEFAULT 'pending' NOT NULL CHECK (status IN ('pending', 'accepted', 'rejected')),
    created_at timestamp with time zone NOT NULL,
    decided_at timestamp with time zone,
    CONSTRAINT uq_entity_merge_suggestions_pair UNIQUE (space_id, new_entity_id, candidate_entity_id, entry_id)
);
CREATE TABLE public.entity_embedding_rebuild_runs (
    id character varying(36) PRIMARY KEY,
    space_id bigint NOT NULL,
    status character varying(16) NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    reason text NOT NULL,
    error text DEFAULT '' NOT NULL,
    model_id character varying(64) DEFAULT '' NOT NULL,
    dimension integer DEFAULT 0 NOT NULL,
    profile_hash character varying(64) DEFAULT '' NOT NULL,
    processed_count bigint DEFAULT 0 NOT NULL,
    total_count bigint DEFAULT 0 NOT NULL,
    model_calls integer DEFAULT 0 NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    attempt_id character varying(36) DEFAULT '' NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone
);
CREATE INDEX idx_entity_embedding_run_latest ON public.entity_embedding_rebuild_runs (space_id, created_at DESC, id DESC);
CREATE INDEX idx_entity_merge_suggestions_status ON public.entity_merge_suggestions(space_id,status);
COMMIT;
