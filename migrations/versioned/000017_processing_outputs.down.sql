-- Development downgrade refuses labels the old schema cannot represent.
ALTER TABLE public.entries ADD CONSTRAINT entries_content_type_check CHECK (content_type IS NULL OR content_type IN ('', 'event', 'opinion', 'research', 'tutorial', 'case', 'reference', 'other'));
DROP TABLE public.entry_processing_outputs;
