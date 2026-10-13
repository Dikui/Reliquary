BEGIN;

DROP INDEX public.chunks_search_idx;

DROP INDEX public.entries_summary_search_idx;

ALTER INDEX public.idx_libraries_space_id RENAME TO idx_knowledge_bases_space_id;

ALTER INDEX public.idx_libraries_space_creator RENAME TO idx_knowledge_bases_space_creator;

ALTER INDEX public.idx_libraries_active_space RENAME TO idx_knowledge_bases_active_space;

ALTER INDEX public.idx_entries_library_metadata_external_id RENAME TO idx_entries_kb_metadata_external_id;

ALTER INDEX public.idx_entries_library_id RENAME TO idx_entries_base_id;

ALTER INDEX public.idx_embeddings_library_id RENAME TO idx_embeddings_knowledge_base_id;

ALTER INDEX public.idx_chunks_library_space RENAME TO idx_chunks_kb_space;

ALTER TABLE public.libraries RENAME CONSTRAINT libraries_type_check TO knowledge_bases_type_check;

ALTER TABLE public.libraries RENAME CONSTRAINT libraries_pkey TO knowledge_bases_pkey;

ALTER TABLE public.image_embeddings RENAME COLUMN library_id TO knowledge_base_id;

ALTER TABLE public.entry_repost_sources RENAME COLUMN library_id TO knowledge_base_id;

ALTER TABLE public.entries RENAME COLUMN library_id TO knowledge_base_id;

ALTER TABLE public.embeddings RENAME COLUMN library_id TO knowledge_base_id;

ALTER TABLE public.chunks RENAME COLUMN library_id TO knowledge_base_id;

ALTER TABLE public.article_embeddings RENAME COLUMN library_id TO knowledge_base_id;

ALTER TABLE public.libraries RENAME TO knowledge_bases;

CREATE INDEX chunks_search_idx ON public.chunks USING bm25 (seq_id, knowledge_base_id, entry_id, is_enabled, chunk_type, deleted_at, content, context_header, search_title, metadata) WITH (key_field=seq_id, text_fields='{
        "content": {"tokenizer": {"type": "chinese_lindera"}},
        "context_header": {"tokenizer": {"type": "chinese_lindera"}},
        "search_title": {"tokenizer": {"type": "chinese_lindera"}}
    }', json_fields='{
        "metadata": {"tokenizer": {"type": "chinese_lindera"}}
    }');

CREATE INDEX entries_summary_search_idx ON public.entries USING bm25 (search_seq_id, space_id, knowledge_base_id, id, deleted_at, parse_status, enable_status, title, description, search_text) WITH (key_field=search_seq_id, text_fields='{
        "title": {"tokenizer": {"type": "chinese_lindera"}},
        "description": {"tokenizer": {"type": "chinese_lindera"}},
        "search_text": {"tokenizer": {"type": "chinese_lindera"}}
    }');

COMMIT;
