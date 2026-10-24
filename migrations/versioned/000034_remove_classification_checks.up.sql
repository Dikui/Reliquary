BEGIN;
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
        ) names));
$function$;
DROP TRIGGER sync_entry_search_topics ON public.entry_topics;
DROP TRIGGER sync_entry_search_topic ON public.topics;
DROP FUNCTION public.sync_entry_search_topic();
DELETE FROM public.follows WHERE target_type='topic';
ALTER TABLE public.follows DROP COLUMN topic_id;
ALTER TABLE public.follows DROP CONSTRAINT follows_target_type_check;
ALTER TABLE public.follows ADD CONSTRAINT follows_target_type_check CHECK (target_type='entity');
DROP TABLE public.topic_backfill_batches;
DROP TABLE public.topic_backfill_jobs;
DROP TABLE public.entry_topics;
DROP TABLE public.topics;
DROP TABLE public.library_content_types;
DROP TABLE public.content_check_runs;
DROP TABLE public.content_check_settings;
ALTER TABLE public.entries DROP COLUMN content_type, DROP COLUMN content_type_corrected, DROP COLUMN content_type_hint;
ALTER TABLE public.entry_processing_outputs DROP COLUMN content_type;
DELETE FROM public.task_pending_ops WHERE task_type IN ('content:check','topic:backfill');
UPDATE public.entries k SET search_text=public.build_entry_search_text(k.id,k.space_id,k.title,k.description,k.structured_summary,k.metadata);
COMMIT;
