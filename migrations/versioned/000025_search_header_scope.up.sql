BEGIN;
-- Rebuild only the derived header text and its index; preserve entries and entity profiles.
DROP INDEX public.entries_summary_search_idx;

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

UPDATE public.entries k
SET search_text=public.build_entry_search_text(k.id, k.space_id, k.title, k.description, k.structured_summary, k.metadata);

CREATE INDEX entries_summary_search_idx ON public.entries USING bm25
    (search_seq_id, space_id, library_id, id, deleted_at, parse_status, enable_status, title, description, search_text)
    WITH (key_field=search_seq_id, mutable_segment_rows=0, text_fields='{
        "title": {"tokenizer": {"type": "chinese_lindera"}},
        "description": {"tokenizer": {"type": "chinese_lindera"}},
        "search_text": {"tokenizer": {"type": "chinese_lindera"}}
    }');
COMMIT;
