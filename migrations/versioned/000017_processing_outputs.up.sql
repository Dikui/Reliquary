ALTER TABLE public.entries DROP CONSTRAINT entries_content_type_check;
CREATE TABLE public.entry_processing_outputs (
 entry_id varchar(36) PRIMARY KEY REFERENCES public.entries(id) ON DELETE CASCADE,
 space_id bigint NOT NULL REFERENCES public.spaces(id) ON DELETE CASCADE,
 library_id varchar(36) NOT NULL REFERENCES public.libraries(id) ON DELETE CASCADE,
 run_id text NOT NULL DEFAULT '',
 body_hash text NOT NULL,
 content_type text NOT NULL,
 relations jsonb NOT NULL DEFAULT '[]'::jsonb,
 images jsonb NOT NULL DEFAULT '[]'::jsonb,
 updated_at timestamptz NOT NULL DEFAULT now()
);
