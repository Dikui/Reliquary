BEGIN;

-- Rebuild the header BM25 index so its stored field metadata follows the SQL column name.

DROP INDEX public.knowledges_summary_search_idx;

ALTER TABLE public.tenants RENAME TO spaces;

ALTER TABLE public.tenant_api_keys RENAME TO api_keys;

ALTER TABLE public.article_embeddings RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.chunks RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.data_sources RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.embedding_rebuild_runs RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.entities RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.entity_aliases RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.entity_embedding_rebuild_runs RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.entity_merge_records RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.entity_merge_suggestions RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.follow_feed_states RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.follows RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.image_embeddings RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.knowledge_bases RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.knowledge_entities RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.knowledge_repost_sources RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.knowledge_topics RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.knowledges RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.models RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.operation_logs RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.resource_bindings RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.resources RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.sync_logs RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.task_dead_letters RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.api_keys RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.topic_backfill_jobs RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.topics RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.users RENAME COLUMN tenant_id TO space_id;

ALTER TABLE public.article_embeddings RENAME CONSTRAINT article_embeddings_tenant_id_knowledge_id_fkey TO article_embeddings_space_id_knowledge_id_fkey;

ALTER TABLE public.embedding_rebuild_runs RENAME CONSTRAINT embedding_rebuild_runs_tenant_id_fkey TO embedding_rebuild_runs_space_id_fkey;

ALTER TABLE public.entities RENAME CONSTRAINT uq_entities_tenant_id TO uq_entities_space_id;

ALTER TABLE public.follow_feed_states RENAME CONSTRAINT follow_feed_states_tenant_id_fkey TO follow_feed_states_space_id_fkey;

ALTER TABLE public.follows RENAME CONSTRAINT follows_tenant_id_entity_id_fkey TO follows_space_id_entity_id_fkey;

ALTER TABLE public.follows RENAME CONSTRAINT follows_tenant_id_fkey TO follows_space_id_fkey;

ALTER TABLE public.follows RENAME CONSTRAINT follows_tenant_id_topic_id_fkey TO follows_space_id_topic_id_fkey;

ALTER TABLE public.image_embeddings RENAME CONSTRAINT image_embeddings_tenant_id_knowledge_id_fkey TO image_embeddings_space_id_knowledge_id_fkey;

ALTER TABLE public.knowledge_entities RENAME CONSTRAINT knowledge_entities_tenant_id_entity_id_fkey TO knowledge_entities_space_id_entity_id_fkey;

ALTER TABLE public.knowledge_entities RENAME CONSTRAINT knowledge_entities_tenant_id_knowledge_id_fkey TO knowledge_entities_space_id_knowledge_id_fkey;

ALTER TABLE public.knowledge_topics RENAME CONSTRAINT knowledge_topics_tenant_id_knowledge_id_fkey TO knowledge_topics_space_id_knowledge_id_fkey;

ALTER TABLE public.knowledge_topics RENAME CONSTRAINT knowledge_topics_tenant_id_topic_id_fkey TO knowledge_topics_space_id_topic_id_fkey;

ALTER TABLE public.knowledges RENAME CONSTRAINT uq_knowledges_tenant_id TO uq_knowledges_space_id;

ALTER TABLE public.api_keys RENAME CONSTRAINT tenant_api_keys_key_hash_key TO api_keys_key_hash_key;

ALTER TABLE public.api_keys RENAME CONSTRAINT tenant_api_keys_pkey TO api_keys_pkey;

ALTER TABLE public.api_keys RENAME CONSTRAINT tenant_api_keys_tenant_id_check TO api_keys_space_id_check;

ALTER TABLE public.api_keys RENAME CONSTRAINT tenant_api_keys_tenant_id_fkey TO api_keys_space_id_fkey;

ALTER TABLE public.spaces RENAME CONSTRAINT tenants_id_check TO spaces_id_check;

ALTER TABLE public.spaces RENAME CONSTRAINT tenants_pkey TO spaces_pkey;

ALTER TABLE public.topic_backfill_jobs RENAME CONSTRAINT topic_backfill_jobs_tenant_id_topic_id_fkey TO topic_backfill_jobs_space_id_topic_id_fkey;

ALTER TABLE public.topics RENAME CONSTRAINT uq_topics_tenant_id TO uq_topics_space_id;

ALTER TABLE public.topics RENAME CONSTRAINT uq_topics_tenant_name TO uq_topics_space_name;

ALTER TABLE public.users RENAME CONSTRAINT fk_users_tenant TO fk_users_space;

ALTER TABLE public.users RENAME CONSTRAINT users_tenant_id_check TO users_space_id_check;

ALTER INDEX public.article_embeddings_tenant_model RENAME TO article_embeddings_space_model;

ALTER INDEX public.idx_chunks_kb_tenant RENAME TO idx_chunks_kb_space;

ALTER INDEX public.idx_chunks_tenant_kg RENAME TO idx_chunks_space_kg;

ALTER INDEX public.idx_data_sources_tenant_id RENAME TO idx_data_sources_space_id;

ALTER INDEX public.idx_knowledge_bases_active_tenant RENAME TO idx_knowledge_bases_active_space;

ALTER INDEX public.idx_knowledge_bases_tenant_creator RENAME TO idx_knowledge_bases_space_creator;

ALTER INDEX public.idx_knowledge_bases_tenant_id RENAME TO idx_knowledge_bases_space_id;

ALTER INDEX public.idx_knowledges_tenant_id RENAME TO idx_knowledges_space_id;

ALTER INDEX public.idx_resources_tenant RENAME TO idx_resources_space;

ALTER INDEX public.idx_resources_tenant_location RENAME TO idx_resources_space_location;

ALTER INDEX public.idx_sync_logs_tenant_id RENAME TO idx_sync_logs_space_id;

ALTER INDEX public.idx_task_dead_letters_tenant RENAME TO idx_task_dead_letters_space;

ALTER INDEX public.idx_tenant_api_keys_revoked_at RENAME TO idx_api_keys_revoked_at;

ALTER INDEX public.idx_tenant_api_keys_tenant RENAME TO idx_api_keys_space;

ALTER INDEX public.idx_tenants_status RENAME TO idx_spaces_status;

ALTER INDEX public.idx_users_tenant_id RENAME TO idx_users_space_id;

ALTER INDEX public.image_embeddings_tenant_model RENAME TO image_embeddings_space_model;

ALTER SEQUENCE public.tenant_api_keys_id_seq RENAME TO api_keys_id_seq;

ALTER SEQUENCE public.tenants_id_seq RENAME TO spaces_id_seq;

COMMENT ON COLUMN public.users.space_id IS 'Space ID that the user belongs to';

-- Replace all nine application function bodies that reference the renamed columns.

CREATE OR REPLACE FUNCTION public.build_knowledge_search_text(article_id text, owner_id bigint, article_title text, article_description text, summary jsonb, meta jsonb)
 RETURNS text
 LANGUAGE sql
 STABLE
AS $function$
    SELECT concat_ws(E'\n', article_title,
        CASE WHEN jsonb_typeof(meta -> 'intake') = 'object' THEN meta #>> '{intake,source_info,publisher}' ELSE meta ->> 'publisher' END,
        CASE WHEN jsonb_typeof(meta -> 'intake') = 'object' THEN meta #>> '{intake,source_info,author}' ELSE meta ->> 'author' END,
        article_description, summary ->> 'overview', summary ->> 'points', summary ->> 'key_terms',
        (SELECT string_agg(DISTINCT names.name, E'\n' ORDER BY names.name) FROM (
            SELECT e.canonical_name AS name
            FROM public.knowledge_entities ke JOIN public.entities e
                ON e.id=ke.entity_id AND e.space_id=ke.space_id
            WHERE ke.knowledge_id=article_id AND ke.space_id=owner_id
            UNION ALL
            SELECT ea.alias AS name
            FROM public.knowledge_entities ke JOIN public.entity_aliases ea
                ON ea.entity_id=ke.entity_id AND ea.space_id=ke.space_id
            WHERE ke.knowledge_id=article_id AND ke.space_id=owner_id
        ) names),
        (SELECT string_agg(DISTINCT t.name, E'\n' ORDER BY t.name)
            FROM public.knowledge_topics kt JOIN public.topics t ON t.id=kt.topic_id AND t.space_id=kt.space_id
            WHERE kt.knowledge_id=article_id AND kt.space_id=owner_id));
$function$;

CREATE OR REPLACE FUNCTION public.refresh_knowledge_search_text(owner_id bigint, article_id text)
 RETURNS void
 LANGUAGE sql
AS $function$
    UPDATE public.knowledges k
    SET search_text=public.build_knowledge_search_text(k.id, k.space_id, k.title, k.description, k.structured_summary, k.metadata)
    WHERE k.id=article_id AND k.space_id=owner_id;
$function$;

CREATE OR REPLACE FUNCTION public.set_chunk_search_title()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    NEW.search_title := COALESCE((
        SELECT btrim(title) FROM public.knowledges
        WHERE id = NEW.knowledge_id AND space_id = NEW.space_id
    ), '');
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.set_knowledge_search_header()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    NEW.search_text := public.build_knowledge_search_text(NEW.id, NEW.space_id,
        NEW.title, NEW.description, NEW.structured_summary, NEW.metadata);
    NEW.search_published_at := CASE WHEN NEW.source_kind = 'article'
        THEN public.search_published_at(CASE WHEN jsonb_typeof(NEW.metadata -> 'intake') = 'object'
            THEN NEW.metadata #>> '{intake,source_info,published_at}' ELSE NEW.metadata ->> 'published_at' END)
        ELSE NULL END;
    IF TG_OP = 'UPDATE' AND (NEW.deleted_at IS NOT NULL OR NEW.source_kind <> 'article'
        OR NEW.title IS DISTINCT FROM OLD.title OR NEW.structured_summary IS DISTINCT FROM OLD.structured_summary) THEN
        DELETE FROM public.article_embeddings WHERE space_id=NEW.space_id AND knowledge_id=NEW.id;
    END IF;
	IF TG_OP = 'UPDATE' AND (NEW.deleted_at IS NOT NULL OR NEW.source_kind <> 'article' OR NEW.processing_mode <> 'full') THEN
		DELETE FROM public.image_embeddings WHERE space_id=NEW.space_id AND knowledge_id=NEW.id;
	END IF;
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.sync_article_search_alias()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE link record;
BEGIN
    FOR link IN SELECT DISTINCT ke.space_id, ke.knowledge_id FROM public.knowledge_entities ke
        WHERE (TG_OP <> 'INSERT' AND ke.space_id=OLD.space_id AND ke.entity_id=OLD.entity_id)
           OR (TG_OP <> 'DELETE' AND ke.space_id=NEW.space_id AND ke.entity_id=NEW.entity_id)
    LOOP
        PERFORM public.refresh_knowledge_search_text(link.space_id, link.knowledge_id);
    END LOOP;
    RETURN NULL;
END;
$function$;

CREATE OR REPLACE FUNCTION public.sync_article_search_entity()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE link record;
BEGIN
    FOR link IN SELECT DISTINCT ke.space_id, ke.knowledge_id FROM public.knowledge_entities ke
        WHERE (TG_OP <> 'INSERT' AND ke.space_id=OLD.space_id AND ke.entity_id=OLD.id)
           OR (TG_OP <> 'DELETE' AND ke.space_id=NEW.space_id AND ke.entity_id=NEW.id)
    LOOP
        PERFORM public.refresh_knowledge_search_text(link.space_id, link.knowledge_id);
    END LOOP;
    RETURN NULL;
END;
$function$;

CREATE OR REPLACE FUNCTION public.sync_article_search_links()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        PERFORM public.refresh_knowledge_search_text(OLD.space_id, OLD.knowledge_id);
    END IF;
    IF TG_OP <> 'DELETE' THEN
        PERFORM public.refresh_knowledge_search_text(NEW.space_id, NEW.knowledge_id);
    END IF;
    RETURN NULL;
END;
$function$;

CREATE OR REPLACE FUNCTION public.sync_article_search_topic()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE link record;
BEGIN
    FOR link IN SELECT DISTINCT kt.space_id, kt.knowledge_id FROM public.knowledge_topics kt
        WHERE (TG_OP <> 'INSERT' AND kt.space_id=OLD.space_id AND kt.topic_id=OLD.id)
           OR (TG_OP <> 'DELETE' AND kt.space_id=NEW.space_id AND kt.topic_id=NEW.id)
    LOOP
        PERFORM public.refresh_knowledge_search_text(link.space_id, link.knowledge_id);
    END LOOP;
    RETURN NULL;
END;
$function$;

CREATE OR REPLACE FUNCTION public.sync_knowledge_search_title()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    UPDATE public.chunks SET search_title = COALESCE(btrim(NEW.title), '')
    WHERE knowledge_id = NEW.id AND space_id = NEW.space_id
      AND search_title IS DISTINCT FROM COALESCE(btrim(NEW.title), '');
    RETURN NEW;
END;
$function$;

CREATE INDEX knowledges_summary_search_idx ON public.knowledges USING bm25
    (search_seq_id, space_id, knowledge_base_id, id, deleted_at, parse_status, enable_status, title, description, search_text)
    WITH (key_field=search_seq_id, text_fields='{
        "title": {"tokenizer": {"type": "chinese_lindera"}},
        "description": {"tokenizer": {"type": "chinese_lindera"}},
        "search_text": {"tokenizer": {"type": "chinese_lindera"}}
    }');

COMMIT;
