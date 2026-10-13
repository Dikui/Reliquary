BEGIN;
DROP TRIGGER set_entry_search_header ON public.entries;
DROP TRIGGER set_follow_ready_at ON public.entries;
DROP INDEX public.idx_entries_follow_feed;
DROP INDEX public.idx_entries_processing_review;
ALTER TABLE public.entries DROP CONSTRAINT entries_content_type_check;
UPDATE public.entries SET content_type='other' WHERE source_kind <> 'report' AND (content_type IS NULL OR content_type='');
ALTER TABLE public.entries ADD CONSTRAINT entries_content_type_check CHECK (content_type IS NULL OR content_type IN ('', 'event', 'opinion', 'research', 'tutorial', 'case', 'reference', 'other'));
UPDATE public.entries SET follow_ready_at=created_at
WHERE follow_ready_at IS NULL AND deleted_at IS NULL AND source_kind='article'
 AND parse_status='completed' AND enable_status='enabled'
 AND COALESCE(metadata #>> '{intake,decision}', 'approved')='approved';
ALTER TABLE public.entries DROP COLUMN content_type_uncertain, DROP COLUMN processing_review_status, DROP COLUMN processing_review_content_type, DROP COLUMN processing_review_reason, DROP COLUMN processing_mode, DROP COLUMN vault_original_path, DROP COLUMN ingest_attachments;
ALTER TABLE public.spaces DROP COLUMN parser_engine_config;
UPDATE public.libraries SET chunking_config = (chunking_config::jsonb - 'parser_engine_rules' - 'table_metadata_instructions')::json;
UPDATE public.entries SET metadata = (metadata::jsonb #- '{process_overrides,parser_engine_rules}' #- '{process_overrides,parser_engine_overrides}' #- '{process_overrides,chunking_config,parser_engine_rules}' #- '{process_overrides,chunking_config,table_metadata_instructions}')::json WHERE metadata IS NOT NULL;
UPDATE public.entries e SET missing_materials = COALESCE((SELECT jsonb_agg(item) FROM jsonb_array_elements(e.missing_materials) item WHERE item->>'type'='image'),'[]'::jsonb), incomplete = e.incomplete AND EXISTS(SELECT 1 FROM jsonb_array_elements(e.missing_materials) item WHERE item->>'type'='image');
DELETE FROM public.ingest_assets WHERE file_type NOT IN ('jpg','jpeg','png','gif','bmp','tiff','webp');
ALTER TABLE public.ingest_assets ADD CONSTRAINT ingest_assets_image_type CHECK (file_type IN ('jpg','jpeg','png','gif','bmp','tiff','webp'));
CREATE OR REPLACE FUNCTION public.set_follow_ready_at() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE eligible boolean; was_eligible boolean := false;
BEGIN
    eligible := NEW.deleted_at IS NULL AND NEW.source_kind='article'
        AND NEW.parse_status='completed' AND NEW.enable_status='enabled'
        AND COALESCE(NEW.metadata #>> '{intake,decision}','approved')='approved';
    IF TG_OP='UPDATE' THEN
        was_eligible := OLD.deleted_at IS NULL AND OLD.source_kind='article'
            AND OLD.parse_status='completed' AND OLD.enable_status='enabled'
            AND COALESCE(OLD.metadata #>> '{intake,decision}','approved')='approved';
        NEW.follow_ready_at := OLD.follow_ready_at;
    END IF;
    IF eligible AND NOT was_eligible THEN
        NEW.follow_ready_at := clock_timestamp();
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER set_follow_ready_at BEFORE INSERT OR UPDATE OF
    parse_status,enable_status,metadata,source_kind,deleted_at
    ON public.entries FOR EACH ROW EXECUTE FUNCTION public.set_follow_ready_at();

CREATE INDEX idx_entries_follow_feed ON public.entries (space_id, created_at DESC, id)
WHERE deleted_at IS NULL AND source_kind='article' AND parse_status='completed'
 AND enable_status='enabled' AND COALESCE(metadata #>> '{intake,decision}', 'approved')='approved';
CREATE OR REPLACE FUNCTION public.set_entry_search_header()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    NEW.search_text := public.build_entry_search_text(NEW.id, NEW.space_id,
        NEW.title, NEW.description, NEW.structured_summary, NEW.metadata);
    NEW.search_published_at := CASE WHEN NEW.source_kind IN ('article', 'mine')
        THEN public.search_published_at(CASE WHEN jsonb_typeof(NEW.metadata -> 'intake') = 'object'
            THEN NEW.metadata #>> '{intake,source_info,published_at}' ELSE NEW.metadata ->> 'published_at' END)
        ELSE NULL END;
    IF TG_OP = 'UPDATE' AND (NEW.deleted_at IS NOT NULL OR NEW.source_kind NOT IN ('article', 'mine')
        OR NEW.title IS DISTINCT FROM OLD.title OR NEW.structured_summary IS DISTINCT FROM OLD.structured_summary) THEN
        DELETE FROM public.article_embeddings WHERE space_id=NEW.space_id AND entry_id=NEW.id;
    END IF;
	IF TG_OP = 'UPDATE' AND (NEW.deleted_at IS NOT NULL OR NEW.source_kind NOT IN ('article', 'mine')) THEN
		DELETE FROM public.image_embeddings WHERE space_id=NEW.space_id AND entry_id=NEW.id;
	END IF;
    RETURN NEW;
END;
$function$;
CREATE TRIGGER set_entry_search_header BEFORE INSERT OR UPDATE OF title, description, structured_summary, metadata, source_kind, deleted_at ON public.entries FOR EACH ROW EXECUTE FUNCTION public.set_entry_search_header();
COMMIT;
