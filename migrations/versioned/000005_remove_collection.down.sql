BEGIN;

CREATE TABLE public.data_sources (
    id character varying(36) NOT NULL,
    space_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    name character varying(255) NOT NULL,
    type character varying(50) NOT NULL,
    default_content_type character varying(16) CHECK (default_content_type IS NULL OR default_content_type IN ('', 'event', 'opinion', 'research', 'tutorial', 'case', 'reference')),
    config jsonb,
    sync_schedule character varying(100),
    sync_mode character varying(20) DEFAULT 'incremental'::character varying,
    status character varying(32) DEFAULT 'active'::character varying,
    sync_deletions boolean DEFAULT true,
    last_sync_at timestamp without time zone,
    last_sync_cursor jsonb,
    last_sync_result jsonb,
    error_message text,
    sync_log_retention_days integer DEFAULT 30,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp without time zone
);

CREATE TABLE public.sync_logs (
    id character varying(36) NOT NULL,
    data_source_id character varying(36) NOT NULL,
    space_id bigint NOT NULL,
    status character varying(32) NOT NULL,
    started_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    finished_at timestamp without time zone,
    items_total integer DEFAULT 0,
    items_created integer DEFAULT 0,
    items_updated integer DEFAULT 0,
    items_deleted integer DEFAULT 0,
    items_skipped integer DEFAULT 0,
    items_failed integer DEFAULT 0,
    error_message text,
    result jsonb,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE ONLY public.data_sources ADD CONSTRAINT data_sources_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.sync_logs ADD CONSTRAINT sync_logs_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.sync_logs ADD CONSTRAINT sync_logs_data_source_id_fkey FOREIGN KEY (data_source_id) REFERENCES public.data_sources(id) ON DELETE CASCADE;
CREATE INDEX idx_data_sources_deleted_at ON public.data_sources USING btree (deleted_at);
CREATE INDEX idx_data_sources_knowledge_base_id ON public.data_sources USING btree (knowledge_base_id);
CREATE INDEX idx_data_sources_status ON public.data_sources USING btree (status);
CREATE INDEX idx_data_sources_space_id ON public.data_sources USING btree (space_id);
CREATE INDEX idx_data_sources_type ON public.data_sources USING btree (type);
CREATE INDEX idx_sync_logs_data_source_id ON public.sync_logs USING btree (data_source_id);
CREATE INDEX idx_sync_logs_started_at ON public.sync_logs USING btree (started_at);
CREATE INDEX idx_sync_logs_status ON public.sync_logs USING btree (status);
CREATE INDEX idx_sync_logs_space_id ON public.sync_logs USING btree (space_id);

COMMIT;
