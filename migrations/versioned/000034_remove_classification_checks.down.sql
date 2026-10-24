BEGIN;
-- Development rollback recreates empty retired structures. Deleted data is not restored.
CREATE TABLE public.topics (
    id character varying(36) NOT NULL PRIMARY KEY,
    space_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    description text DEFAULT '' NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT uq_topics_space_id UNIQUE (space_id, id),
    CONSTRAINT uq_topics_space_name UNIQUE (space_id, name)
);
CREATE TABLE public.entry_topics (
    space_id bigint NOT NULL,
    entry_id character varying(36) NOT NULL,
    topic_id character varying(36) NOT NULL,
    source character varying(8) DEFAULT 'model' NOT NULL CHECK (source IN ('model','user')),
    is_historical boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT uq_entry_topics_pair UNIQUE (entry_id, topic_id),
    FOREIGN KEY (space_id, entry_id) REFERENCES public.entries (space_id, id) ON DELETE CASCADE,
    FOREIGN KEY (space_id, topic_id) REFERENCES public.topics (space_id, id) ON DELETE CASCADE
);
CREATE TABLE public.topic_backfill_jobs (
    id character varying(36) NOT NULL PRIMARY KEY,
    space_id bigint NOT NULL,
    topic_id character varying(36) NOT NULL,
    topic_name character varying(255) NOT NULL,
    topic_description text DEFAULT '' NOT NULL,
    status character varying(16) DEFAULT 'pending' NOT NULL CHECK (status IN ('pending','running','completed','failed','cancelled')),
    total integer DEFAULT 0 NOT NULL CHECK (total >= 0),
    processed integer DEFAULT 0 NOT NULL CHECK (processed >= 0),
    failed_entries integer DEFAULT 0 NOT NULL CHECK (failed_entries >= 0),
    failed_batches integer DEFAULT 0 NOT NULL CHECK (failed_batches >= 0),
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    FOREIGN KEY (space_id,topic_id) REFERENCES public.topics(space_id,id) ON DELETE CASCADE
);
CREATE TABLE public.topic_backfill_batches (
    id character varying(36) NOT NULL PRIMARY KEY,
    job_id character varying(36) NOT NULL REFERENCES public.topic_backfill_jobs(id) ON DELETE CASCADE,
    ordinal integer NOT NULL CHECK (ordinal > 0),
    entry_ids jsonb NOT NULL CHECK (jsonb_typeof(entry_ids) = 'array'),
    entry_count integer NOT NULL CHECK (entry_count > 0),
    status character varying(16) DEFAULT 'pending' NOT NULL CHECK (status IN ('pending','running','completed','failed')),
    attempts integer DEFAULT 0 NOT NULL CHECK (attempts >= 0),
    error text DEFAULT '' NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    UNIQUE (job_id,ordinal)
);
ALTER TABLE public.topics ADD COLUMN library_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(library_ids)='array');
ALTER TABLE public.topic_backfill_batches ADD COLUMN retry_at timestamptz;
CREATE INDEX idx_entry_topics_topic ON public.entry_topics(space_id,topic_id,entry_id);
CREATE INDEX idx_topic_backfill_jobs_topic ON public.topic_backfill_jobs(space_id,topic_id,created_at DESC);
CREATE INDEX idx_topic_backfill_jobs_pending ON public.topic_backfill_jobs(status,created_at) WHERE status IN ('pending','running');
CREATE INDEX idx_topic_backfill_batches_pending ON public.topic_backfill_batches(job_id,status,ordinal);
ALTER TABLE public.follows DROP CONSTRAINT follows_target_type_check;
ALTER TABLE public.follows ADD CONSTRAINT follows_target_type_check CHECK (target_type IN ('entity','topic'));
ALTER TABLE public.follows ADD COLUMN topic_id varchar(36) GENERATED ALWAYS AS (CASE WHEN target_type='topic' THEN target_id END) STORED;
ALTER TABLE public.follows ADD CONSTRAINT follows_space_id_topic_id_fkey FOREIGN KEY (space_id,topic_id) REFERENCES public.topics(space_id,id) ON DELETE CASCADE;
ALTER TABLE public.entries ADD COLUMN content_type varchar(16), ADD COLUMN content_type_corrected boolean NOT NULL DEFAULT false, ADD COLUMN content_type_hint varchar(16) NOT NULL DEFAULT '';
ALTER TABLE public.entry_processing_outputs ADD COLUMN content_type text NOT NULL DEFAULT '';
CREATE TABLE library_content_types (
 library_id varchar(36) NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
 name varchar(100) NOT NULL CHECK (length(btrim(name))>0),
 position integer NOT NULL CHECK(position>=0), enabled boolean NOT NULL DEFAULT true,
 PRIMARY KEY(library_id,name)
);
CREATE TABLE content_check_settings (
 space_id bigint PRIMARY KEY REFERENCES spaces(id),
 enabled boolean NOT NULL DEFAULT false,
 model_id text NOT NULL DEFAULT '',
 weekday integer NOT NULL DEFAULT 1 CHECK (weekday BETWEEN 0 AND 6),
 time text NOT NULL DEFAULT '',
 next_run_at timestamptz,
 scan_after timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE content_check_runs (
 id text PRIMARY KEY,
 space_id bigint NOT NULL REFERENCES spaces(id),
 model_id text NOT NULL,
 previous_id text NOT NULL DEFAULT '',
 status text NOT NULL CHECK (status IN ('pending','running','completed','failed')),
 window_from timestamptz NOT NULL,
 window_to timestamptz NOT NULL,
 targets jsonb NOT NULL DEFAULT '[]',
 completed integer NOT NULL DEFAULT 0,
 results jsonb NOT NULL DEFAULT '[]',
 error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz
);
CREATE UNIQUE INDEX content_check_one_active ON content_check_runs(space_id) WHERE status IN ('pending','running');
CREATE INDEX content_check_runs_created ON content_check_runs(space_id,created_at DESC);

CREATE OR REPLACE FUNCTION public.build_entry_search_text(entry_id text, owner_id bigint, entry_title text, entry_description text, summary jsonb, meta jsonb)
 RETURNS text
 LANGUAGE sql
 STABLE
AS $function$
    SELECT concat_ws(E'\n', entry_title,
        CASE WHEN jsonb_typeof(meta -> 'intake') = 'object' THEN meta #>> '{intake,source_info,publisher}' ELSE meta ->> 'publisher' END,
        CASE WHEN jsonb_typeof(meta -> 'intake') = 'object' THEN meta #>> '{intake,source_info,author}' ELSE meta ->> 'author' END,
        entry_description, summary ->> 'overview', summary ->> 'points', summary ->> 'key_terms',
        (SELECT string_agg(DISTINCT names.name, E'\n' ORDER BY names.name) FROM (
            SELECT e.canonical_name AS name
            FROM public.entry_entities ke JOIN public.entities e
                ON e.id=ke.entity_id AND e.space_id=ke.space_id
            WHERE ke.entry_id=$1 AND ke.space_id=$2
            UNION ALL
            SELECT ea.alias AS name
            FROM public.entry_entities ke JOIN public.entity_aliases ea
                ON ea.entity_id=ke.entity_id AND ea.space_id=ke.space_id
            WHERE ke.entry_id=$1 AND ke.space_id=$2
        ) names),
        (SELECT string_agg(DISTINCT t.name, E'\n' ORDER BY t.name)
            FROM public.entry_topics kt JOIN public.topics t ON t.id=kt.topic_id AND t.space_id=kt.space_id
            WHERE kt.entry_id=$1 AND kt.space_id=$2));
$function$;
CREATE OR REPLACE FUNCTION public.sync_entry_search_topic()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE link record;
BEGIN
    FOR link IN SELECT DISTINCT kt.space_id, kt.entry_id FROM public.entry_topics kt
        WHERE (TG_OP <> 'INSERT' AND kt.space_id=OLD.space_id AND kt.topic_id=OLD.id)
           OR (TG_OP <> 'DELETE' AND kt.space_id=NEW.space_id AND kt.topic_id=NEW.id)
    LOOP
        PERFORM public.refresh_entry_search_text(link.space_id, link.entry_id);
    END LOOP;
    RETURN NULL;
END;
$function$;
CREATE TRIGGER sync_entry_search_topics AFTER INSERT OR UPDATE OR DELETE ON public.entry_topics FOR EACH ROW EXECUTE FUNCTION public.sync_entry_search_links();
CREATE TRIGGER sync_entry_search_topic AFTER INSERT OR UPDATE OR DELETE ON public.topics FOR EACH ROW EXECUTE FUNCTION public.sync_entry_search_topic();
COMMIT;
