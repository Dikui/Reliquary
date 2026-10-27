BEGIN;

DROP TRIGGER IF EXISTS set_chunk_search_title ON public.chunks;
DROP TRIGGER IF EXISTS sync_entry_search_title ON public.entries;
DROP FUNCTION IF EXISTS public.set_chunk_search_title();
DROP FUNCTION IF EXISTS public.sync_entry_search_title();

DROP INDEX IF EXISTS public.chunks_search_idx;
DROP INDEX IF EXISTS public.idx_embeddings_is_enabled;

ALTER TABLE public.chunks
    DROP COLUMN IF EXISTS search_title,
    DROP COLUMN IF EXISTS metadata,
    DROP COLUMN IF EXISTS flags,
    DROP COLUMN IF EXISTS relation_chunks,
    DROP COLUMN IF EXISTS indirect_relation_chunks;

ALTER TABLE public.embeddings
    DROP COLUMN IF EXISTS is_enabled;

CREATE INDEX chunks_search_idx ON public.chunks USING bm25 (seq_id, library_id, entry_id, is_enabled, chunk_type, deleted_at, content, context_header) WITH (key_field=seq_id, text_fields='{
        "content": {"tokenizer": {"type": "chinese_lindera"}},
        "context_header": {"tokenizer": {"type": "chinese_lindera"}}
    }');

COMMIT;
