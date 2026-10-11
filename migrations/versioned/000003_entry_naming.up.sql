BEGIN;

-- Rebuild pg_search metadata under the new table vocabulary.

DROP INDEX public.knowledges_summary_search_idx;

ALTER TABLE public.knowledge_entities RENAME TO entry_entities;

ALTER TABLE public.knowledge_processing_spans RENAME TO entry_processing_spans;

ALTER TABLE public.knowledge_repost_sources RENAME TO entry_repost_sources;

ALTER TABLE public.knowledge_topics RENAME TO entry_topics;

ALTER TABLE public.knowledges RENAME TO entries;

ALTER TABLE public.article_embeddings RENAME COLUMN knowledge_id TO entry_id;


ALTER TABLE public.chunks RENAME COLUMN knowledge_id TO entry_id;

ALTER TABLE public.embeddings RENAME COLUMN knowledge_id TO entry_id;

ALTER TABLE public.entity_merge_suggestions RENAME COLUMN knowledge_id TO entry_id;

ALTER TABLE public.image_embeddings RENAME COLUMN knowledge_id TO entry_id;

ALTER TABLE public.entry_entities RENAME COLUMN knowledge_id TO entry_id;

ALTER TABLE public.entry_processing_spans RENAME COLUMN knowledge_id TO entry_id;

ALTER TABLE public.entry_repost_sources RENAME COLUMN knowledge_id TO entry_id;

ALTER TABLE public.entry_topics RENAME COLUMN knowledge_id TO entry_id;

ALTER TABLE public.topic_backfill_batches RENAME COLUMN knowledge_ids TO entry_ids;

ALTER TABLE public.article_embeddings RENAME CONSTRAINT article_embeddings_space_id_knowledge_id_fkey TO article_embeddings_space_id_entry_id_fkey;

ALTER TABLE public.image_embeddings RENAME CONSTRAINT image_embeddings_space_id_knowledge_id_fkey TO image_embeddings_space_id_entry_id_fkey;

ALTER TABLE public.entry_entities RENAME CONSTRAINT knowledge_entities_space_id_entity_id_fkey TO entry_entities_space_id_entity_id_fkey;

ALTER TABLE public.entry_entities RENAME CONSTRAINT knowledge_entities_space_id_knowledge_id_fkey TO entry_entities_space_id_entry_id_fkey;

ALTER TABLE public.entry_entities RENAME CONSTRAINT uq_knowledge_entities_pair TO uq_entry_entities_pair;

ALTER TABLE public.entry_processing_spans RENAME CONSTRAINT knowledge_processing_spans_pkey TO entry_processing_spans_pkey;

ALTER TABLE public.entry_repost_sources RENAME CONSTRAINT knowledge_repost_sources_match_method_check TO entry_repost_sources_match_method_check;

ALTER TABLE public.entry_repost_sources RENAME CONSTRAINT knowledge_repost_sources_pkey TO entry_repost_sources_pkey;

ALTER TABLE public.entry_topics RENAME CONSTRAINT knowledge_topics_source_check TO entry_topics_source_check;

ALTER TABLE public.entry_topics RENAME CONSTRAINT knowledge_topics_space_id_knowledge_id_fkey TO entry_topics_space_id_entry_id_fkey;

ALTER TABLE public.entry_topics RENAME CONSTRAINT knowledge_topics_space_id_topic_id_fkey TO entry_topics_space_id_topic_id_fkey;

ALTER TABLE public.entry_topics RENAME CONSTRAINT uq_knowledge_topics_pair TO uq_entry_topics_pair;

ALTER TABLE public.entries RENAME CONSTRAINT knowledges_agent_tags_check TO entries_agent_tags_check;

ALTER TABLE public.entries RENAME CONSTRAINT knowledges_content_type_check TO entries_content_type_check;

ALTER TABLE public.entries RENAME CONSTRAINT knowledges_pkey TO entries_pkey;

ALTER TABLE public.entries RENAME CONSTRAINT knowledges_processing_mode_check TO entries_processing_mode_check;

ALTER TABLE public.entries RENAME CONSTRAINT knowledges_processing_review_content_type_check TO entries_processing_review_content_type_check;

ALTER TABLE public.entries RENAME CONSTRAINT knowledges_processing_review_status_check TO entries_processing_review_status_check;

ALTER TABLE public.entries RENAME CONSTRAINT knowledges_source_kind_check TO entries_source_kind_check;

ALTER TABLE public.entries RENAME CONSTRAINT uq_knowledges_space_id TO uq_entries_space_id;

ALTER TABLE public.topic_backfill_batches RENAME CONSTRAINT topic_backfill_batches_knowledge_ids_check TO topic_backfill_batches_entry_ids_check;

ALTER INDEX public.idx_chunks_knowledge_enabled RENAME TO idx_chunks_entry_enabled;

ALTER INDEX public.idx_knowledge_content_hash RENAME TO idx_entry_content_hash;

ALTER INDEX public.idx_knowledge_entities_entity RENAME TO idx_entry_entities_entity;

ALTER INDEX public.idx_knowledge_normalized_url RENAME TO idx_entry_normalized_url;

ALTER INDEX public.idx_knowledge_simhash_candidates RENAME TO idx_entry_simhash_candidates;

ALTER INDEX public.idx_knowledge_topics_topic RENAME TO idx_entry_topics_topic;

ALTER INDEX public.idx_knowledges_base_id RENAME TO idx_entries_base_id;

ALTER INDEX public.idx_knowledges_enable_status RENAME TO idx_entries_enable_status;

ALTER INDEX public.idx_knowledges_follow_feed RENAME TO idx_entries_follow_feed;

ALTER INDEX public.idx_knowledges_kb_metadata_external_id RENAME TO idx_entries_kb_metadata_external_id;

ALTER INDEX public.idx_knowledges_parse_status RENAME TO idx_entries_parse_status;

ALTER INDEX public.idx_knowledges_processing_review RENAME TO idx_entries_processing_review;

ALTER INDEX public.idx_knowledges_search_published_at RENAME TO idx_entries_search_published_at;

ALTER INDEX public.idx_knowledges_search_seq_id RENAME TO idx_entries_search_seq_id;

ALTER INDEX public.idx_knowledges_space_id RENAME TO idx_entries_space_id;

ALTER INDEX public.idx_knowledges_summary_status RENAME TO idx_entries_summary_status;

ALTER INDEX public.idx_kpspan_knowledge_attempt RENAME TO idx_kpspan_entry_attempt;

ALTER INDEX public.idx_repost_knowledge RENAME TO idx_repost_entry;

ALTER SEQUENCE public.knowledge_processing_spans_id_seq RENAME TO entry_processing_spans_id_seq;

ALTER SEQUENCE public.knowledges_search_seq_id_seq RENAME TO entries_search_seq_id_seq;

ALTER INDEX public.knowledges_vault_path_unique RENAME TO entries_vault_path_unique;

ALTER INDEX public.task_jobs_knowledge RENAME TO task_jobs_entry;

ALTER TRIGGER sync_article_search_entity ON public.entities RENAME TO sync_entry_search_entity;

ALTER TRIGGER sync_article_search_alias ON public.entity_aliases RENAME TO sync_entry_search_alias;

ALTER TRIGGER sync_article_search_entities ON public.entry_entities RENAME TO sync_entry_search_entities;

ALTER TRIGGER sync_article_search_topics ON public.entry_topics RENAME TO sync_entry_search_topics;

ALTER TRIGGER set_knowledge_search_header ON public.entries RENAME TO set_entry_search_header;

ALTER TRIGGER sync_knowledge_search_title ON public.entries RENAME TO sync_entry_search_title;

ALTER TRIGGER sync_article_search_topic ON public.topics RENAME TO sync_entry_search_topic;

ALTER FUNCTION public.set_knowledge_search_header() RENAME TO set_entry_search_header;

ALTER FUNCTION public.sync_article_search_alias() RENAME TO sync_entry_search_alias;

ALTER FUNCTION public.sync_article_search_entity() RENAME TO sync_entry_search_entity;

ALTER FUNCTION public.sync_article_search_links() RENAME TO sync_entry_search_links;

ALTER FUNCTION public.sync_article_search_topic() RENAME TO sync_entry_search_topic;

ALTER FUNCTION public.sync_knowledge_search_title() RENAME TO sync_entry_search_title;

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
            WHERE ke.entry_id=entry_id AND ke.space_id=owner_id
            UNION ALL
            SELECT ea.alias AS name
            FROM public.entry_entities ke JOIN public.entity_aliases ea
                ON ea.entity_id=ke.entity_id AND ea.space_id=ke.space_id
            WHERE ke.entry_id=entry_id AND ke.space_id=owner_id
        ) names),
        (SELECT string_agg(DISTINCT t.name, E'\n' ORDER BY t.name)
            FROM public.entry_topics kt JOIN public.topics t ON t.id=kt.topic_id AND t.space_id=kt.space_id
            WHERE kt.entry_id=entry_id AND kt.space_id=owner_id));
$function$;

CREATE OR REPLACE FUNCTION public.refresh_entry_search_text(owner_id bigint, entry_id text)
 RETURNS void
 LANGUAGE sql
AS $function$
    UPDATE public.entries k
    SET search_text=public.build_entry_search_text(k.id, k.space_id, k.title, k.description, k.structured_summary, k.metadata)
    WHERE k.id=entry_id AND k.space_id=owner_id;
$function$;

CREATE OR REPLACE FUNCTION public.set_chunk_search_title()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    NEW.search_title := COALESCE((
        SELECT btrim(title) FROM public.entries
        WHERE id = NEW.entry_id AND space_id = NEW.space_id
    ), '');
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.set_entry_search_header()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    NEW.search_text := public.build_entry_search_text(NEW.id, NEW.space_id,
        NEW.title, NEW.description, NEW.structured_summary, NEW.metadata);
    NEW.search_published_at := CASE WHEN NEW.source_kind = 'article'
        THEN public.search_published_at(CASE WHEN jsonb_typeof(NEW.metadata -> 'intake') = 'object'
            THEN NEW.metadata #>> '{intake,source_info,published_at}' ELSE NEW.metadata ->> 'published_at' END)
        ELSE NULL END;
    IF TG_OP = 'UPDATE' AND (NEW.deleted_at IS NOT NULL OR NEW.source_kind <> 'article'
        OR NEW.title IS DISTINCT FROM OLD.title OR NEW.structured_summary IS DISTINCT FROM OLD.structured_summary) THEN
        DELETE FROM public.article_embeddings WHERE space_id=NEW.space_id AND entry_id=NEW.id;
    END IF;
	IF TG_OP = 'UPDATE' AND (NEW.deleted_at IS NOT NULL OR NEW.source_kind <> 'article' OR NEW.processing_mode <> 'full') THEN
		DELETE FROM public.image_embeddings WHERE space_id=NEW.space_id AND entry_id=NEW.id;
	END IF;
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.sync_entry_search_alias()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE link record;
BEGIN
    FOR link IN SELECT DISTINCT ke.space_id, ke.entry_id FROM public.entry_entities ke
        WHERE (TG_OP <> 'INSERT' AND ke.space_id=OLD.space_id AND ke.entity_id=OLD.entity_id)
           OR (TG_OP <> 'DELETE' AND ke.space_id=NEW.space_id AND ke.entity_id=NEW.entity_id)
    LOOP
        PERFORM public.refresh_entry_search_text(link.space_id, link.entry_id);
    END LOOP;
    RETURN NULL;
END;
$function$;

CREATE OR REPLACE FUNCTION public.sync_entry_search_entity()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE link record;
BEGIN
    FOR link IN SELECT DISTINCT ke.space_id, ke.entry_id FROM public.entry_entities ke
        WHERE (TG_OP <> 'INSERT' AND ke.space_id=OLD.space_id AND ke.entity_id=OLD.id)
           OR (TG_OP <> 'DELETE' AND ke.space_id=NEW.space_id AND ke.entity_id=NEW.id)
    LOOP
        PERFORM public.refresh_entry_search_text(link.space_id, link.entry_id);
    END LOOP;
    RETURN NULL;
END;
$function$;

CREATE OR REPLACE FUNCTION public.sync_entry_search_links()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        PERFORM public.refresh_entry_search_text(OLD.space_id, OLD.entry_id);
    END IF;
    IF TG_OP <> 'DELETE' THEN
        PERFORM public.refresh_entry_search_text(NEW.space_id, NEW.entry_id);
    END IF;
    RETURN NULL;
END;
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

CREATE OR REPLACE FUNCTION public.sync_entry_search_title()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    UPDATE public.chunks SET search_title = COALESCE(btrim(NEW.title), '')
    WHERE entry_id = NEW.id AND space_id = NEW.space_id
      AND search_title IS DISTINCT FROM COALESCE(btrim(NEW.title), '');
    RETURN NEW;
END;
$function$;

COMMENT ON COLUMN public.entries.channel IS 'Source channel of the entry: web, api, rss';

CREATE INDEX entries_summary_search_idx ON public.entries USING bm25
    (search_seq_id, space_id, knowledge_base_id, id, deleted_at, parse_status, enable_status, title, description, search_text)
    WITH (key_field=search_seq_id, text_fields='{
        "title": {"tokenizer": {"type": "chinese_lindera"}},
        "description": {"tokenizer": {"type": "chinese_lindera"}},
        "search_text": {"tokenizer": {"type": "chinese_lindera"}}
    }');

-- Retire the old functions after all trigger bodies use the current names.
DROP FUNCTION public.refresh_knowledge_search_text(bigint, text);
DROP FUNCTION public.build_knowledge_search_text(text, bigint, text, text, jsonb, jsonb);

COMMIT;
