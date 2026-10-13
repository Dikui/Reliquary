-- Imported user documents retain explicit dates and summary and image vectors.
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
	IF TG_OP = 'UPDATE' AND (NEW.deleted_at IS NOT NULL OR NEW.source_kind NOT IN ('article', 'mine') OR NEW.processing_mode <> 'full') THEN
		DELETE FROM public.image_embeddings WHERE space_id=NEW.space_id AND entry_id=NEW.id;
	END IF;
    RETURN NEW;
END;
$function$;
