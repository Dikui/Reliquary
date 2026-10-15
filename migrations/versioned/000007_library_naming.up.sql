BEGIN;

-- Rebuild BM25 metadata because it stores the indexed SQL field names.

DROP INDEX public.chunks_search_idx;

DROP INDEX public.entries_summary_search_idx;

ALTER TABLE public.knowledge_bases RENAME TO libraries;

ALTER TABLE public.article_embeddings RENAME COLUMN knowledge_base_id TO library_id;

ALTER TABLE public.chunks RENAME COLUMN knowledge_base_id TO library_id;

ALTER TABLE public.embeddings RENAME COLUMN knowledge_base_id TO library_id;

ALTER TABLE public.entries RENAME COLUMN knowledge_base_id TO library_id;

ALTER TABLE public.entry_repost_sources RENAME COLUMN knowledge_base_id TO library_id;

ALTER TABLE public.image_embeddings RENAME COLUMN knowledge_base_id TO library_id;

ALTER TABLE public.libraries RENAME CONSTRAINT knowledge_bases_pkey TO libraries_pkey;

ALTER TABLE public.libraries RENAME CONSTRAINT knowledge_bases_type_check TO libraries_type_check;

ALTER INDEX public.idx_chunks_kb_space RENAME TO idx_chunks_library_space;

ALTER INDEX public.idx_embeddings_knowledge_base_id RENAME TO idx_embeddings_library_id;

ALTER INDEX public.idx_entries_base_id RENAME TO idx_entries_library_id;

ALTER INDEX public.idx_entries_kb_metadata_external_id RENAME TO idx_entries_library_metadata_external_id;

ALTER INDEX public.idx_knowledge_bases_active_space RENAME TO idx_libraries_active_space;

ALTER INDEX public.idx_knowledge_bases_space_creator RENAME TO idx_libraries_space_creator;

ALTER INDEX public.idx_knowledge_bases_space_id RENAME TO idx_libraries_space_id;

CREATE INDEX chunks_search_idx ON public.chunks USING bm25 (seq_id, library_id, entry_id, is_enabled, chunk_type, deleted_at, content, context_header, search_title, metadata) WITH (key_field=seq_id, text_fields='{
        "content": {"tokenizer": {"type": "chinese_lindera"}},
        "context_header": {"tokenizer": {"type": "chinese_lindera"}},
        "search_title": {"tokenizer": {"type": "chinese_lindera"}}
    }', json_fields='{
        "metadata": {"tokenizer": {"type": "chinese_lindera"}}
    }');

CREATE INDEX entries_summary_search_idx ON public.entries USING bm25 (search_seq_id, space_id, library_id, id, deleted_at, parse_status, enable_status, title, description, search_text) WITH (key_field=search_seq_id, text_fields='{
        "title": {"tokenizer": {"type": "chinese_lindera"}},
        "description": {"tokenizer": {"type": "chinese_lindera"}},
        "search_text": {"tokenizer": {"type": "chinese_lindera"}}
    }');

COMMIT;
