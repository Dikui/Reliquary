BEGIN;

DROP INDEX IF EXISTS public.chunks_search_idx;

ALTER TABLE public.chunks
    ADD COLUMN search_title text DEFAULT ''::text NOT NULL,
    ADD COLUMN metadata jsonb,
    ADD COLUMN flags integer DEFAULT 1 NOT NULL,
    ADD COLUMN relation_chunks jsonb,
    ADD COLUMN indirect_relation_chunks jsonb;

ALTER TABLE public.embeddings
    ADD COLUMN is_enabled boolean DEFAULT true;

CREATE INDEX idx_embeddings_is_enabled ON public.embeddings USING btree (is_enabled);

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

CREATE TRIGGER set_chunk_search_title
    BEFORE INSERT OR UPDATE OF entry_id, space_id ON public.chunks
    FOR EACH ROW EXECUTE FUNCTION public.set_chunk_search_title();

CREATE TRIGGER sync_entry_search_title
    AFTER INSERT OR UPDATE OF title ON public.entries
    FOR EACH ROW EXECUTE FUNCTION public.sync_entry_search_title();

CREATE INDEX chunks_search_idx ON public.chunks USING bm25 (seq_id, library_id, entry_id, is_enabled, chunk_type, deleted_at, content, context_header, search_title, metadata) WITH (key_field=seq_id, text_fields='{
        "content": {"tokenizer": {"type": "chinese_lindera"}},
        "context_header": {"tokenizer": {"type": "chinese_lindera"}},
        "search_title": {"tokenizer": {"type": "chinese_lindera"}}
    }', json_fields='{
        "metadata": {"tokenizer": {"type": "chinese_lindera"}}
    }');

COMMIT;
