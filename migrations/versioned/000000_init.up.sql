-- PostgreSQL baseline compacted on 2026-10-07 from migrations 000000-000098.
-- Source: 45daf07. Existing databases must be rebuilt before applying this baseline.

BEGIN;

--
-- Name: paradedb; Type: SCHEMA; Schema: -
--

CREATE SCHEMA IF NOT EXISTS paradedb;


--
-- Name: pg_search; Type: EXTENSION; Schema: -
--

CREATE EXTENSION IF NOT EXISTS pg_search WITH SCHEMA paradedb;


--
-- Name: EXTENSION pg_search; Type: COMMENT; Schema: -
--

COMMENT ON EXTENSION pg_search IS 'pg_search: Full text search for PostgreSQL using BM25';


--
-- Name: pg_trgm; Type: EXTENSION; Schema: -
--

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;


--
-- Name: EXTENSION pg_trgm; Type: COMMENT; Schema: -
--

COMMENT ON EXTENSION pg_trgm IS 'text similarity measurement and index searching based on trigrams';


--
-- Name: uuid-ossp; Type: EXTENSION; Schema: -
--

CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA public;


--
-- Name: EXTENSION "uuid-ossp"; Type: COMMENT; Schema: -
--

COMMENT ON EXTENSION "uuid-ossp" IS 'generate universally unique identifiers (UUIDs)';


--
-- Name: vector; Type: EXTENSION; Schema: -
--

CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public;


--
-- Name: EXTENSION vector; Type: COMMENT; Schema: -
--

COMMENT ON EXTENSION vector IS 'vector data type and ivfflat and hnsw access methods';


--
-- Name: unify_prompt_placeholder(text); Type: FUNCTION; Schema: public
--

CREATE FUNCTION public.unify_prompt_placeholder(input text) RETURNS text
    LANGUAGE plpgsql
    AS $$
DECLARE
    result TEXT := COALESCE(input, '');
    replacements TEXT[][] := ARRAY[
        -- Go template variables -> simple placeholders
        ['{{.Query}}', '{{query}}'],
        ['{{.Answer}}', '{{answer}}'],
        ['{{.CurrentTime}}', '{{current_time}}'],
        ['{{.CurrentWeek}}', '{{current_week}}'],
        ['{{.Yesterday}}', '{{yesterday}}'],
        ['{{.Contexts}}', '{{contexts}}'],
        -- Go template control structures -> simple placeholders or remove
        ['{{range .Contexts}}', '{{contexts}}'],
        -- Remove Go template syntax
        ['{{if .Contexts}}', ''],
        ['{{else}}', ''],
        ['{{.}}', '']
    ];
    r TEXT[];
BEGIN
    FOREACH r SLICE 1 IN ARRAY replacements LOOP
        result := REPLACE(result, r[1], r[2]);
    END LOOP;

    -- Handle {{range .Conversation}}...{{end}} block specially
    -- Replace the entire block with just {{conversation}}
    -- The pattern matches: {{range .Conversation}} followed by any content until {{end}}
    result := regexp_replace(
        result,
        '\{\{range \.Conversation\}\}[\s\S]*?\{\{end\}\}',
        '{{conversation}}',
        'g'
    );

    -- Clean up any remaining {{end}} tags
    result := REPLACE(result, '{{end}}', '');

    RETURN result;
END;
$$;


--
-- Name: update_mcp_services_updated_at(); Type: FUNCTION; Schema: public
--

CREATE FUNCTION public.update_mcp_services_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$;




--
-- Name: audit_logs; Type: TABLE; Schema: public
--

CREATE TABLE public.audit_logs (
    id bigint NOT NULL,
    tenant_id bigint NOT NULL,
    actor_user_id character varying(36) DEFAULT ''::character varying NOT NULL,
    actor_role character varying(32) DEFAULT ''::character varying NOT NULL,
    action character varying(64) NOT NULL,
    target_type character varying(32) DEFAULT ''::character varying NOT NULL,
    target_id character varying(64) DEFAULT ''::character varying NOT NULL,
    target_user_id character varying(36) DEFAULT ''::character varying NOT NULL,
    request_path character varying(512) DEFAULT ''::character varying NOT NULL,
    request_method character varying(16) DEFAULT ''::character varying NOT NULL,
    outcome character varying(16) DEFAULT 'success'::character varying NOT NULL,
    details jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    scope_type character varying(32) DEFAULT ''::character varying NOT NULL,
    scope_id character varying(64) DEFAULT ''::character varying NOT NULL
);


--
-- Name: audit_logs_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.audit_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: audit_logs_id_seq; Type: SEQUENCE OWNED BY; Schema: public
--

ALTER SEQUENCE public.audit_logs_id_seq OWNED BY public.audit_logs.id;


--
-- Name: auth_tokens; Type: TABLE; Schema: public
--

CREATE TABLE public.auth_tokens (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    user_id character varying(36) NOT NULL,
    token text NOT NULL,
    token_type character varying(50) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    is_revoked boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: TABLE auth_tokens; Type: COMMENT; Schema: public
--

COMMENT ON TABLE public.auth_tokens IS 'Authentication tokens for users';


--
-- Name: COLUMN auth_tokens.id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.auth_tokens.id IS 'Unique identifier of the token';


--
-- Name: COLUMN auth_tokens.user_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.auth_tokens.user_id IS 'User ID that owns this token';


--
-- Name: COLUMN auth_tokens.token; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.auth_tokens.token IS 'Token value (JWT or other format)';


--
-- Name: COLUMN auth_tokens.token_type; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.auth_tokens.token_type IS 'Token type (access_token, refresh_token)';


--
-- Name: COLUMN auth_tokens.expires_at; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.auth_tokens.expires_at IS 'Token expiration time';


--
-- Name: COLUMN auth_tokens.is_revoked; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.auth_tokens.is_revoked IS 'Whether the token is revoked';


--
-- Name: chunk_revisions; Type: TABLE; Schema: public
--

CREATE TABLE public.chunk_revisions (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    knowledge_id character varying(36) NOT NULL,
    chunk_id character varying(36) NOT NULL,
    revision integer NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    is_enabled boolean DEFAULT true NOT NULL,
    editor_id character varying(64) DEFAULT ''::character varying NOT NULL,
    edit_source character varying(16) DEFAULT 'user'::character varying NOT NULL,
    edited_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: chunks_seq_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.chunks_seq_id_seq
    START WITH 100000000
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: chunks; Type: TABLE; Schema: public
--

CREATE TABLE public.chunks (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    tenant_id integer NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    knowledge_id character varying(36) NOT NULL,
    content text NOT NULL,
    chunk_index integer NOT NULL,
    is_enabled boolean DEFAULT true NOT NULL,
    start_at integer NOT NULL,
    end_at integer NOT NULL,
    pre_chunk_id character varying(36),
    next_chunk_id character varying(36),
    chunk_type character varying(20) DEFAULT 'text'::character varying NOT NULL,
    parent_chunk_id character varying(36),
    image_info text,
    relation_chunks jsonb,
    indirect_relation_chunks jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    metadata jsonb,
    tag_id character varying(36),
    status integer DEFAULT 0 NOT NULL,
    content_hash character varying(64),
    flags integer DEFAULT 1 NOT NULL,
    seq_id bigint DEFAULT nextval('public.chunks_seq_id_seq'::regclass) NOT NULL,
    video_info text,
    source_content text DEFAULT ''::text NOT NULL,
    content_revision integer DEFAULT 0 NOT NULL,
    index_status character varying(16) DEFAULT 'ready'::character varying NOT NULL,
    last_editor_id character varying(64) DEFAULT ''::character varying NOT NULL,
    context_header text DEFAULT ''::text NOT NULL,
    search_title text DEFAULT ''::text NOT NULL
);


--
-- Name: COLUMN chunks.video_info; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.chunks.video_info IS 'Video information in JSON format: {"url": string, "frame_count": int, "has_vlm_analysis": bool, "has_asr": bool, "video_summary": string, "asr_text": string, "frame_descriptions": string[]}';


--
-- Name: custom_agents; Type: TABLE; Schema: public
--

CREATE TABLE public.custom_agents (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    name character varying(255) NOT NULL,
    description text,
    avatar character varying(64),
    is_builtin boolean DEFAULT false NOT NULL,
    tenant_id integer NOT NULL,
    created_by character varying(36),
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    runnable_by_viewer boolean DEFAULT true NOT NULL
);


--
-- Name: data_sources; Type: TABLE; Schema: public
--

CREATE TABLE public.data_sources (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    name character varying(255) NOT NULL,
    type character varying(50) NOT NULL,
    config jsonb,
    sync_schedule character varying(100),
    sync_mode character varying(20) DEFAULT 'incremental'::character varying,
    status character varying(32) DEFAULT 'active'::character varying,
    conflict_strategy character varying(32) DEFAULT 'overwrite'::character varying,
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


--
-- Name: embeddings; Type: TABLE; Schema: public
--

CREATE TABLE public.embeddings (
    id integer NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    source_id character varying(64) NOT NULL,
    source_type integer NOT NULL,
    chunk_id character varying(64),
    knowledge_id character varying(64),
    knowledge_base_id character varying(64),
    dimension integer NOT NULL,
    embedding public.halfvec,
    is_enabled boolean DEFAULT true,
    tag_id character varying(36)
);


--
-- Name: embeddings_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.embeddings_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: embeddings_id_seq; Type: SEQUENCE OWNED BY; Schema: public
--

ALTER SEQUENCE public.embeddings_id_seq OWNED BY public.embeddings.id;


--
-- Name: im_channel_sessions; Type: TABLE; Schema: public
--

CREATE TABLE public.im_channel_sessions (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    platform character varying(20) NOT NULL,
    user_id character varying(128) NOT NULL,
    chat_id character varying(128) DEFAULT ''::character varying NOT NULL,
    session_id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    agent_id character varying(36) DEFAULT ''::character varying,
    status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone,
    im_channel_id character varying(36) DEFAULT ''::character varying,
    thread_id character varying(128) DEFAULT ''::character varying NOT NULL
);


--
-- Name: TABLE im_channel_sessions; Type: COMMENT; Schema: public
--

COMMENT ON TABLE public.im_channel_sessions IS 'Maps IM platform channels to Reliquary conversation sessions';


--
-- Name: COLUMN im_channel_sessions.platform; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channel_sessions.platform IS 'IM platform identifier: feishu';


--
-- Name: COLUMN im_channel_sessions.user_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channel_sessions.user_id IS 'Platform-specific user identifier';


--
-- Name: COLUMN im_channel_sessions.chat_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channel_sessions.chat_id IS 'Platform-specific chat/group identifier, empty for direct messages';


--
-- Name: COLUMN im_channel_sessions.session_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channel_sessions.session_id IS 'Associated Reliquary session ID';


--
-- Name: COLUMN im_channel_sessions.tenant_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channel_sessions.tenant_id IS 'Tenant that owns this channel mapping';


--
-- Name: COLUMN im_channel_sessions.agent_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channel_sessions.agent_id IS 'Custom agent ID used for this channel, empty for default';


--
-- Name: COLUMN im_channel_sessions.status; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channel_sessions.status IS 'Channel status: active, paused, expired';


--
-- Name: COLUMN im_channel_sessions.metadata; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channel_sessions.metadata IS 'Platform-specific extra data (JSON)';


--
-- Name: COLUMN im_channel_sessions.thread_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channel_sessions.thread_id IS 'Platform thread identifier for thread-based sessions. Empty for user-mode sessions.';


--
-- Name: im_channels; Type: TABLE; Schema: public
--

CREATE TABLE public.im_channels (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    tenant_id bigint NOT NULL,
    agent_id character varying(36) NOT NULL,
    platform character varying(20) NOT NULL,
    name character varying(255) DEFAULT ''::character varying NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    mode character varying(20) DEFAULT 'websocket'::character varying NOT NULL,
    output_mode character varying(20) DEFAULT 'stream'::character varying NOT NULL,
    credentials jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone,
    knowledge_base_id character varying(36) DEFAULT ''::character varying,
    bot_identity character varying(255) DEFAULT ''::character varying NOT NULL,
    session_mode character varying(20) DEFAULT 'user'::character varying NOT NULL,
    CONSTRAINT chk_im_channels_session_mode CHECK (session_mode IN ('user', 'thread'))
);


--
-- Name: TABLE im_channels; Type: COMMENT; Schema: public
--

COMMENT ON TABLE public.im_channels IS 'IM platform channel configurations bound to agents';


--
-- Name: COLUMN im_channels.agent_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channels.agent_id IS 'Agent ID this channel is bound to';


--
-- Name: COLUMN im_channels.platform; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channels.platform IS 'IM platform: feishu';


--
-- Name: COLUMN im_channels.name; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channels.name IS 'User-defined channel name for identification';


--
-- Name: COLUMN im_channels.mode; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channels.mode IS 'Connection mode: webhook or websocket';


--
-- Name: COLUMN im_channels.output_mode; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channels.output_mode IS 'Output mode: stream (real-time) or full (wait for complete answer)';


--
-- Name: COLUMN im_channels.credentials; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channels.credentials IS 'Feishu credentials (JSONB): app_id, app_secret, verification_token, encrypt_key';


--
-- Name: COLUMN im_channels.bot_identity; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channels.bot_identity IS 'Unique bot identity derived from credentials (feishu:{app_id}). Used to prevent duplicate bot bindings.';


--
-- Name: COLUMN im_channels.session_mode; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.im_channels.session_mode IS 'Session resolution mode: user (per user+chat, default) or thread (per thread)';


--
-- Name: knowledge_anomalies; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_anomalies (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    knowledge_ids text NOT NULL,
    kind character varying(32) NOT NULL,
    description text NOT NULL,
    proposal_type character varying(16) DEFAULT 'none'::character varying NOT NULL,
    proposed_tag_ids text DEFAULT '[]'::text NOT NULL,
    proposed_delete_tag_id character varying(36) DEFAULT ''::character varying NOT NULL,
    status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    reported_by character varying(36) NOT NULL,
    decided_by character varying(36) DEFAULT ''::character varying NOT NULL,
    decision_note text DEFAULT ''::text NOT NULL,
    result character varying(500) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    decided_at timestamp with time zone,
    proposed_remove_tag_ids text DEFAULT '[]'::text NOT NULL,
    site text DEFAULT '' NOT NULL,
    source_url text DEFAULT '' NOT NULL,
    reason text DEFAULT '' NOT NULL,
    adapter text DEFAULT '' NOT NULL,
    applied_changes text DEFAULT '{}'::text NOT NULL
);


--
-- Name: knowledge_bases; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_domains (
    model_tiers jsonb DEFAULT '{}'::jsonb NOT NULL,
    id character varying(36) NOT NULL PRIMARY KEY,
    tenant_id integer NOT NULL,
    name character varying(255) NOT NULL,
    description text DEFAULT '' NOT NULL,
    entity_types jsonb DEFAULT '[]'::jsonb NOT NULL,
    fields jsonb DEFAULT '[]'::jsonb NOT NULL,
    relations jsonb DEFAULT '[]'::jsonb NOT NULL,
    unit_families jsonb DEFAULT '[]'::jsonb NOT NULL,
    extraction_hints text DEFAULT '' NOT NULL,
    is_builtin boolean DEFAULT false NOT NULL,
    created_by character varying(36) DEFAULT '' NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone
);

CREATE UNIQUE INDEX idx_knowledge_domains_team_name ON public.knowledge_domains (tenant_id, name) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_knowledge_domains_builtin ON public.knowledge_domains (tenant_id) WHERE is_builtin AND deleted_at IS NULL;
CREATE INDEX idx_knowledge_domains_deleted_at ON public.knowledge_domains (deleted_at);

CREATE TABLE public.knowledge_bases (
    model_tiers jsonb DEFAULT '{}'::jsonb NOT NULL,
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    name character varying(255) NOT NULL,
    description text,
    tenant_id integer NOT NULL,
    domain_id character varying(36) NOT NULL REFERENCES public.knowledge_domains(id),
    chunking_config jsonb DEFAULT '{"chunk_size": 512, "chunk_overlap": 50, "split_markers": ["\n\n", "\n", "。"], "keep_separator": true}'::jsonb NOT NULL,
    image_processing_config jsonb DEFAULT '{"model_id": "", "enable_multimodal": false}'::jsonb NOT NULL,
    embedding_model_id character varying(64) NOT NULL,
    summary_model_id character varying(64) NOT NULL,
    cos_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    vlm_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    extract_config jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    is_temporary boolean DEFAULT false NOT NULL,
    type character varying(32) DEFAULT 'document'::character varying NOT NULL,
    question_generation_config jsonb,
    storage_provider_config jsonb,
    is_pinned boolean DEFAULT false NOT NULL,
    pinned_at timestamp with time zone,
    asr_config jsonb,
    vector_store_id character varying(36),
    wiki_config jsonb,
    indexing_strategy jsonb,
    creator_id character varying(36),
    storage_backend_id character varying(36),
    auto_tag_config jsonb
);

CREATE INDEX idx_knowledge_bases_domain ON public.knowledge_bases (tenant_id, domain_id) WHERE deleted_at IS NULL;


--
-- Name: COLUMN knowledge_bases.is_temporary; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.knowledge_bases.is_temporary IS 'Whether this knowledge base is temporary (ephemeral) and should be hidden from UI';


--
-- Name: COLUMN knowledge_bases.storage_provider_config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.knowledge_bases.storage_provider_config IS 'Storage provider config for this KB. Only stores provider name; credentials come from tenant StorageEngineConfig.';


--
-- Name: COLUMN knowledge_bases.asr_config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.knowledge_bases.asr_config IS 'ASR (Automatic Speech Recognition) configuration: {"enabled": bool, "model_id": string, "language": string}';


--
-- Name: COLUMN knowledge_bases.vector_store_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.knowledge_bases.vector_store_id IS 'References vector_stores.id. NULL means tenant default (env store derived from RETRIEVE_DRIVER). Immutable after creation (enforced at ORM and service layer). No FK by design.';


--
-- Name: COLUMN knowledge_bases.wiki_config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.knowledge_bases.wiki_config IS 'Wiki configuration: {"auto_ingest": bool, "synthesis_model_id": string, "wiki_language": string, "max_pages_per_ingest": int}';


--
-- Name: COLUMN knowledge_bases.indexing_strategy; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.knowledge_bases.indexing_strategy IS 'Indexing pipelines strategy: {"vector_enabled": bool, "keyword_enabled": bool, "wiki_enabled": bool, "graph_enabled": bool}';


--
-- Name: knowledge_context_attention; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_context_attention (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    evidence_id character varying(36) NOT NULL,
    event_id character varying(36) DEFAULT ''::character varying NOT NULL,
    novelty double precision DEFAULT 0 NOT NULL,
    impact double precision DEFAULT 0 NOT NULL,
    credibility double precision DEFAULT 0 NOT NULL,
    relevance double precision DEFAULT 0 NOT NULL,
    actionability double precision DEFAULT 0 NOT NULL,
    score double precision DEFAULT 0 NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone NOT NULL
);


--
-- Name: knowledge_context_entities; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_context_entities (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36),
    canonical_name character varying(512) NOT NULL,
    normalized_name character varying(512) NOT NULL,
    entity_type character varying(128) DEFAULT ''::character varying NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    name_embedding public.vector,
    embedding_model_id character varying(64) DEFAULT '' NOT NULL,
    embedding_dimension integer DEFAULT 0 NOT NULL,
    embedding_needs_rebuild boolean DEFAULT true NOT NULL,
    merged_into_id character varying(36) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: knowledge_context_entity_aliases; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_context_entity_aliases (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36),
    entity_type character varying(128) DEFAULT ''::character varying NOT NULL,
    entity_id character varying(36) NOT NULL,
    alias character varying(512) NOT NULL,
    normalized_alias character varying(512) NOT NULL,
    evidence_id character varying(36) DEFAULT '' NOT NULL,
    quote text DEFAULT '' NOT NULL,
    resolution_method character varying(16) DEFAULT 'exact' NOT NULL,
    created_at timestamp with time zone NOT NULL
);

CREATE TABLE public.knowledge_context_merge_suggestions (
    id character varying(36) PRIMARY KEY,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    new_entity_id character varying(36) NOT NULL,
    candidate_entity_id character varying(36) NOT NULL,
    evidence_id character varying(36) NOT NULL,
    reason text NOT NULL,
    quote text NOT NULL,
    status character varying(16) DEFAULT 'pending' NOT NULL CHECK (status IN ('pending', 'accepted', 'rejected')),
    created_at timestamp with time zone NOT NULL,
    decided_at timestamp with time zone,
    CONSTRAINT idx_kcems_pair UNIQUE (tenant_id, new_entity_id, candidate_entity_id, evidence_id)
);
CREATE INDEX idx_kcems_scope_status ON public.knowledge_context_merge_suggestions (tenant_id, knowledge_base_id, status);
CREATE INDEX idx_kce_name_trgm ON public.knowledge_context_entities USING gin (normalized_name public.gin_trgm_ops);
CREATE INDEX idx_kcea_alias_trgm ON public.knowledge_context_entity_aliases USING gin (normalized_alias public.gin_trgm_ops);
CREATE INDEX idx_kce_embedding_profile ON public.knowledge_context_entities (tenant_id, embedding_model_id, embedding_dimension) WHERE name_embedding IS NOT NULL;
CREATE INDEX idx_kce_embedding_pending ON public.knowledge_context_entities (tenant_id, id) WHERE embedding_needs_rebuild AND merged_into_id = '';

CREATE TABLE public.knowledge_context_embedding_rebuild_runs (
    id character varying(36) PRIMARY KEY,
    tenant_id bigint NOT NULL,
    status character varying(16) NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    reason text NOT NULL,
    error text DEFAULT '' NOT NULL,
    model_id character varying(64) DEFAULT '' NOT NULL,
    dimension integer DEFAULT 0 NOT NULL,
    profile_hash character varying(64) DEFAULT '' NOT NULL,
    processed_count bigint DEFAULT 0 NOT NULL,
    total_count bigint DEFAULT 0 NOT NULL,
    model_calls integer DEFAULT 0 NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    attempt_id character varying(36) DEFAULT '' NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone
);
CREATE INDEX idx_kc_embedding_run_latest ON public.knowledge_context_embedding_rebuild_runs (tenant_id, created_at DESC, id DESC);


--
-- Name: knowledge_context_event_evidence; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_context_event_evidence (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    event_id character varying(36) NOT NULL,
    evidence_id character varying(36) NOT NULL,
    source_title character varying(512) DEFAULT ''::character varying NOT NULL,
    source_event_type character varying(128) DEFAULT ''::character varying NOT NULL,
    source_description text DEFAULT ''::text NOT NULL,
    source_occurred_at timestamp with time zone,
    source_key character varying(512) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    relation character varying(32) DEFAULT ''::character varying NOT NULL,
    related_event_id character varying(36) DEFAULT ''::character varying NOT NULL
);


--
-- Name: knowledge_context_events; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_context_events (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    title character varying(512) NOT NULL,
    event_type character varying(128) DEFAULT ''::character varying NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    occurred_at timestamp with time zone,
    evidence_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    source_count integer DEFAULT 0 NOT NULL,
    heat double precision DEFAULT 0 NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: knowledge_context_evidence; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_context_evidence (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    knowledge_id character varying(36) NOT NULL,
    version integer NOT NULL,
    body_hash character(64) NOT NULL,
    superseded_by_id character varying(36) DEFAULT ''::character varying NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    body text NOT NULL,
    author text DEFAULT ''::text NOT NULL,
    publisher text DEFAULT ''::text NOT NULL,
    original_url text DEFAULT ''::text NOT NULL,
    published_at text DEFAULT ''::text NOT NULL,
    published_at_missing boolean DEFAULT false NOT NULL,
    image_refs jsonb DEFAULT '[]'::jsonb NOT NULL,
    status character varying(16) DEFAULT 'current'::character varying NOT NULL,
    extraction_status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    extraction_coverage character varying(16) DEFAULT 'complete' NOT NULL CHECK (extraction_coverage IN ('complete', 'partial')),
    processed_segments integer DEFAULT 0 NOT NULL,
    total_segments integer DEFAULT 0 NOT NULL,
    model_calls jsonb DEFAULT '[]'::jsonb NOT NULL,
    extraction_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    source_kind character varying(32) DEFAULT ''::character varying NOT NULL
);


--
-- Name: knowledge_context_facts; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_context_facts (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    subject_entity_id character varying(36) NOT NULL,
    kind character varying(16) DEFAULT 'attribute'::character varying NOT NULL,
    predicate character varying(512) NOT NULL,
    value jsonb,
    value_type character varying(32) DEFAULT ''::character varying NOT NULL,
    unit character varying(128) DEFAULT ''::character varying NOT NULL,
    object_entity_id character varying(36) DEFAULT ''::character varying NOT NULL,
    effective_from timestamp with time zone,
    effective_to timestamp with time zone,
    evidence_id character varying(36) NOT NULL,
    quote text NOT NULL,
    status character varying(16) DEFAULT 'current'::character varying NOT NULL,
    supersedes_id character varying(36) DEFAULT ''::character varying NOT NULL,
    corrected_by character varying(36) DEFAULT ''::character varying NOT NULL,
    correction_note text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    original_predicate character varying(512) DEFAULT ''::character varying NOT NULL,
    correction_actor character varying(255) DEFAULT ''::character varying NOT NULL
);


--
-- Name: knowledge_processing_spans; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_processing_spans (
    id bigint NOT NULL,
    knowledge_id character varying(64) NOT NULL,
    attempt integer DEFAULT 1 NOT NULL,
    span_id character varying(64) NOT NULL,
    parent_span_id character varying(64),
    name character varying(255) NOT NULL,
    kind character varying(16) NOT NULL,
    status character varying(16) NOT NULL,
    input jsonb,
    output jsonb,
    metadata jsonb,
    error_code character varying(64),
    error_message text,
    error_detail text,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    duration_ms bigint,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: knowledge_processing_spans_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.knowledge_processing_spans_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: knowledge_processing_spans_id_seq; Type: SEQUENCE OWNED BY; Schema: public
--

ALTER SEQUENCE public.knowledge_processing_spans_id_seq OWNED BY public.knowledge_processing_spans.id;


--
-- Name: knowledge_strategy_versions; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_strategy_versions (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    status character varying(16) NOT NULL,
    rules jsonb NOT NULL,
    previous_rules jsonb NOT NULL,
    reason text NOT NULL,
    sample_results jsonb NOT NULL,
    created_by character varying(36) NOT NULL,
    activated_by character varying(36) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    activated_at timestamp with time zone
);


--
-- Name: knowledge_tag_manual_decisions; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_tag_manual_decisions (
    knowledge_id character varying(36) NOT NULL,
    tag_id character varying(36) NOT NULL,
    selected boolean NOT NULL,
    actor_id character varying(36) NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: knowledge_tag_relations; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_tag_relations (
    knowledge_id character varying(36) NOT NULL,
    tag_id character varying(36) NOT NULL,
    created_at timestamp with time zone DEFAULT now()
);


--
-- Name: knowledge_tags_seq_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.knowledge_tags_seq_id_seq
    START WITH 10000000
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: knowledge_tags; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_tags (
    id character varying(36) NOT NULL,
    tenant_id integer NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    name character varying(128) NOT NULL,
    color character varying(32),
    sort_order integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    seq_id bigint DEFAULT nextval('public.knowledge_tags_seq_id_seq'::regclass) NOT NULL,
    kind character varying(16) DEFAULT ''::character varying NOT NULL
);


--
-- Name: knowledge_workflow_reports; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_workflow_reports (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    workflow_id character varying(36) NOT NULL,
    run_id character varying(36) NOT NULL,
    step_id character varying(128) NOT NULL,
    title character varying(512) NOT NULL,
    markdown text NOT NULL,
    citations jsonb NOT NULL,
    time_from timestamp with time zone,
    time_to timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    excluded_missing_published_at_count integer DEFAULT 0 NOT NULL
);


--
-- Name: knowledge_workflow_runs; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_workflow_runs (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    workflow_id character varying(36) NOT NULL,
    workflow_snapshot jsonb NOT NULL,
    owner_id character varying(64) DEFAULT ''::character varying NOT NULL,
    trigger character varying(32) NOT NULL,
    idempotency_key character varying(160) NOT NULL,
    status character varying(24) NOT NULL,
    current_step_id character varying(128) DEFAULT ''::character varying NOT NULL,
    error text DEFAULT ''::text NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    next_run_at timestamp with time zone,
    lease_token character varying(36) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: knowledge_workflow_step_runs; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_workflow_step_runs (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    run_id character varying(36) NOT NULL,
    step_id character varying(128) NOT NULL,
    step_type character varying(32) NOT NULL,
    status character varying(24) NOT NULL,
    attempt integer DEFAULT 0 NOT NULL,
    input jsonb,
    output jsonb,
    error text DEFAULT ''::text NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: knowledge_workflows; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledge_workflows (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    owner_id character varying(64) NOT NULL,
    name character varying(255) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    schedule jsonb NOT NULL,
    steps jsonb NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: knowledges; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledges (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    tenant_id integer NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    type character varying(50) NOT NULL,
    title character varying(255) NOT NULL,
    description text,
    source character varying(2048) NOT NULL,
    parse_status character varying(50) DEFAULT 'unprocessed'::character varying NOT NULL,
    enable_status character varying(50) DEFAULT 'enabled'::character varying NOT NULL,
    embedding_model_id character varying(64),
    file_name character varying(255),
    file_type character varying(50),
    file_size bigint,
    file_path text,
    file_hash character varying(64),
    normalized_url text DEFAULT '' NOT NULL,
    content_hash character(64) DEFAULT '' NOT NULL,
    content_sim_hash bigint DEFAULT 0 NOT NULL,
    content_rune_count integer DEFAULT 0 NOT NULL,
    storage_size bigint DEFAULT 0 NOT NULL,
    metadata jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    processed_at timestamp with time zone,
    error_message text,
    deleted_at timestamp with time zone,
    summary_status character varying(32) DEFAULT 'none'::character varying,
    channel character varying(50) DEFAULT 'web'::character varying NOT NULL,
    pending_subtasks_count integer DEFAULT 0 NOT NULL,
    custom_metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    folder_path character varying(1024) DEFAULT ''::character varying NOT NULL
);


--
-- Name: COLUMN knowledges.channel; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.knowledges.channel IS 'Source channel of the knowledge: web, api, feishu';


CREATE TABLE public.knowledge_repost_sources (
    id character varying(36) NOT NULL PRIMARY KEY,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    knowledge_id character varying(36) NOT NULL,
    original_url text DEFAULT '' NOT NULL,
    normalized_url text DEFAULT '' NOT NULL,
    source_fingerprint character(64) NOT NULL,
    author text DEFAULT '' NOT NULL,
    publisher text DEFAULT '' NOT NULL,
    published_at text DEFAULT '' NOT NULL,
    published_at_missing boolean DEFAULT false NOT NULL,
    match_method character varying(16) NOT NULL CHECK (match_method IN ('url', 'content_hash', 'simhash', 'file_hash')),
    sim_hash_distance integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT idx_repost_origin UNIQUE (tenant_id, knowledge_id, source_fingerprint)
);
CREATE INDEX idx_repost_knowledge ON public.knowledge_repost_sources (tenant_id, knowledge_id);
CREATE INDEX idx_repost_url ON public.knowledge_repost_sources (tenant_id, knowledge_base_id, normalized_url);
CREATE INDEX idx_knowledge_normalized_url ON public.knowledges (tenant_id, knowledge_base_id, normalized_url) WHERE deleted_at IS NULL AND normalized_url <> '';
CREATE INDEX idx_knowledge_content_hash ON public.knowledges (tenant_id, knowledge_base_id, content_hash) WHERE deleted_at IS NULL AND content_hash <> '';
CREATE INDEX idx_knowledge_simhash_candidates ON public.knowledges (tenant_id, knowledge_base_id, content_rune_count) WHERE deleted_at IS NULL AND content_rune_count >= 100;


--
-- Name: mcp_oauth_clients; Type: TABLE; Schema: public
--

CREATE TABLE public.mcp_oauth_clients (
    id character varying(36) NOT NULL,
    tenant_id integer NOT NULL,
    service_id character varying(36) NOT NULL,
    client_id character varying(512) NOT NULL,
    client_secret text,
    redirect_uri character varying(1024),
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: mcp_oauth_tokens; Type: TABLE; Schema: public
--

CREATE TABLE public.mcp_oauth_tokens (
    id character varying(36) NOT NULL,
    tenant_id integer NOT NULL,
    user_id character varying(512) NOT NULL,
    service_id character varying(36) NOT NULL,
    access_token text,
    refresh_token text,
    token_type character varying(32),
    expires_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    principal_type character varying(32) NOT NULL,
    principal_id character varying(512) NOT NULL,
    refresh_lease_id character varying(36),
    refresh_lease_until timestamp with time zone
);


--
-- Name: mcp_services; Type: TABLE; Schema: public
--

CREATE TABLE public.mcp_services (
    id character varying(36) NOT NULL,
    tenant_id integer NOT NULL,
    name character varying(255) NOT NULL,
    description text,
    enabled boolean DEFAULT true,
    transport_type character varying(50) NOT NULL,
    url character varying(512),
    headers jsonb,
    auth_config jsonb,
    advanced_config jsonb,
    stdio_config jsonb,
    env_vars jsonb,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp without time zone,
    is_builtin boolean DEFAULT false NOT NULL
);


--
-- Name: TABLE mcp_services; Type: COMMENT; Schema: public
--

COMMENT ON TABLE public.mcp_services IS 'MCP service configurations';


--
-- Name: mcp_tool_approvals; Type: TABLE; Schema: public
--

CREATE TABLE public.mcp_tool_approvals (
    id character varying(36) NOT NULL,
    tenant_id integer NOT NULL,
    service_id character varying(36) NOT NULL,
    tool_name character varying(512) NOT NULL,
    require_approval boolean DEFAULT false NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: memory_doc_affinity; Type: TABLE; Schema: public
--

CREATE TABLE public.memory_doc_affinity (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    tenant_id integer NOT NULL,
    subject_id character varying(512) NOT NULL,
    knowledge_id character varying(36) NOT NULL,
    knowledge_base_id character varying(36) DEFAULT ''::character varying NOT NULL,
    title character varying(512) DEFAULT ''::character varying NOT NULL,
    hits integer DEFAULT 0 NOT NULL,
    last_used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: memory_item_embeddings; Type: TABLE; Schema: public
--

CREATE TABLE public.memory_item_embeddings (
    item_id character varying(36) NOT NULL,
    tenant_id integer NOT NULL,
    subject_id character varying(512) NOT NULL,
    model_id character varying(64) DEFAULT ''::character varying NOT NULL,
    dims integer DEFAULT 0 NOT NULL,
    vector bytea,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: memory_items; Type: TABLE; Schema: public
--

CREATE TABLE public.memory_items (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    tenant_id integer NOT NULL,
    subject_id character varying(512) NOT NULL,
    kind character varying(32) NOT NULL,
    content text NOT NULL,
    topic character varying(255) DEFAULT ''::character varying NOT NULL,
    normalized_key character varying(255) DEFAULT ''::character varying NOT NULL,
    importance smallint DEFAULT 3 NOT NULL,
    origin character varying(16) DEFAULT 'extracted'::character varying NOT NULL,
    status character varying(16) DEFAULT 'active'::character varying NOT NULL,
    source_session_id character varying(36),
    source_message_id character varying(36),
    valid_from timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    invalid_at timestamp with time zone,
    expires_at timestamp with time zone,
    superseded_by character varying(36),
    last_used_at timestamp with time zone,
    use_count integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: memory_subjects; Type: TABLE; Schema: public
--

CREATE TABLE public.memory_subjects (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    tenant_id integer NOT NULL,
    subject_id character varying(512) NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    block_text text DEFAULT ''::text NOT NULL,
    block_updated_at timestamp with time zone,
    item_count integer DEFAULT 0 NOT NULL,
    last_extracted_at timestamp with time zone,
    extract_cursor timestamp with time zone,
    pending_sessions jsonb,
    extract_scheduled_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    consolidated_at timestamp with time zone,
    forced_consolidated_at timestamp with time zone
);


--
-- Name: memory_tombstones; Type: TABLE; Schema: public
--

CREATE TABLE public.memory_tombstones (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    tenant_id integer NOT NULL,
    subject_id character varying(512) NOT NULL,
    topic character varying(255) DEFAULT ''::character varying NOT NULL,
    fingerprint character varying(64) NOT NULL,
    source_message_id character varying(36),
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: memory_topic_stats; Type: TABLE; Schema: public
--

CREATE TABLE public.memory_topic_stats (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    tenant_id integer NOT NULL,
    subject_id character varying(512) NOT NULL,
    normalized_key character varying(255) NOT NULL,
    topic character varying(255) DEFAULT ''::character varying NOT NULL,
    hits integer DEFAULT 0 NOT NULL,
    last_seen_at timestamp with time zone,
    promoted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    aliases jsonb DEFAULT '[]'::jsonb NOT NULL
);


--
-- Name: message_suggestion_events; Type: TABLE; Schema: public
--

CREATE TABLE public.message_suggestion_events (
    id bigint NOT NULL,
    tenant_id integer NOT NULL,
    session_id character varying(36) NOT NULL,
    suggestion_set_id character varying(36) NOT NULL,
    question_id character varying(64) DEFAULT ''::character varying NOT NULL,
    event_type character varying(32) NOT NULL,
    actor_id character varying(512) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: message_suggestion_events_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.message_suggestion_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: message_suggestion_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public
--

ALTER SEQUENCE public.message_suggestion_events_id_seq OWNED BY public.message_suggestion_events.id;


--
-- Name: message_suggestion_sets; Type: TABLE; Schema: public
--

CREATE TABLE public.message_suggestion_sets (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    tenant_id integer NOT NULL,
    session_id character varying(36) NOT NULL,
    assistant_message_id character varying(36) NOT NULL,
    agent_id character varying(36) DEFAULT ''::character varying NOT NULL,
    placement character varying(32) NOT NULL,
    config_hash character varying(64) NOT NULL,
    locale character varying(16) DEFAULT ''::character varying NOT NULL,
    status character varying(16) NOT NULL,
    allow_regenerate boolean DEFAULT false NOT NULL,
    suppression_reason character varying(64) DEFAULT ''::character varying NOT NULL,
    questions jsonb DEFAULT '[]'::jsonb NOT NULL,
    model_id character varying(64) DEFAULT ''::character varying NOT NULL,
    prompt_tokens integer DEFAULT 0 NOT NULL,
    completion_tokens integer DEFAULT 0 NOT NULL,
    latency_ms bigint DEFAULT 0 NOT NULL,
    error_code character varying(64) DEFAULT ''::character varying NOT NULL,
    lease_until timestamp with time zone,
    generated_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: messages; Type: TABLE; Schema: public
--

CREATE TABLE public.messages (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    request_id character varying(36) NOT NULL,
    session_id character varying(36) NOT NULL,
    role character varying(50) NOT NULL,
    content text NOT NULL,
    knowledge_references jsonb DEFAULT '[]'::jsonb NOT NULL,
    agent_steps jsonb,
    is_completed boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    mentioned_items jsonb DEFAULT '[]'::jsonb,
    is_fallback boolean DEFAULT false,
    agent_duration_ms bigint DEFAULT 0,
    knowledge_id character varying(36),
    images jsonb DEFAULT '[]'::jsonb,
    channel character varying(50) DEFAULT ''::character varying NOT NULL,
    rendered_content text DEFAULT ''::text NOT NULL,
    attachments jsonb DEFAULT '[]'::jsonb,
    agent_id character varying(36) DEFAULT ''::character varying NOT NULL,
    model_id character varying(64) DEFAULT ''::character varying NOT NULL,
    execution_context jsonb DEFAULT '{}'::jsonb NOT NULL,
    artifacts jsonb DEFAULT '[]'::jsonb,
    used_memories jsonb,
    usage jsonb
);


--
-- Name: COLUMN messages.agent_steps; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.messages.agent_steps IS 'Agent execution steps (reasoning process and tool calls)';


--
-- Name: COLUMN messages.mentioned_items; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.messages.mentioned_items IS 'Stores @mentioned knowledge bases and files (id, name, type) when user sends a message';


--
-- Name: COLUMN messages.channel; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.messages.channel IS 'Source channel of the message: web, api, im, etc.';


--
-- Name: COLUMN messages.rendered_content; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.messages.rendered_content IS 'Full RAG-augmented user message sent to LLM, preserving retrieval context across turns';


--
-- Name: models; Type: TABLE; Schema: public
--

CREATE TABLE public.models (
    id character varying(64) DEFAULT public.uuid_generate_v4() NOT NULL,
    tenant_id integer NOT NULL,
    name character varying(255) NOT NULL,
    display_name character varying(255) DEFAULT ''::character varying NOT NULL,
    type character varying(50) NOT NULL,
    source character varying(50) NOT NULL,
    description text,
    parameters jsonb NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    status character varying(50) DEFAULT 'active'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    is_builtin boolean DEFAULT false NOT NULL,
    managed_by character varying(32) DEFAULT ''::character varying NOT NULL
);


--
-- Name: resource_access_grants; Type: TABLE; Schema: public
--

CREATE TABLE public.resource_access_grants (
    id character varying(36) NOT NULL,
    token_hash character varying(64) NOT NULL,
    resource_id character varying(36) NOT NULL,
    access_scope character varying(16) DEFAULT 'read'::character varying NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: resource_bindings; Type: TABLE; Schema: public
--

CREATE TABLE public.resource_bindings (
    id character varying(36) NOT NULL,
    resource_id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    owner_type character varying(32) NOT NULL,
    owner_id character varying(64) NOT NULL,
    relation character varying(32) DEFAULT 'attachment'::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: resources; Type: TABLE; Schema: public
--

CREATE TABLE public.resources (
    id character varying(36) NOT NULL,
    handle character varying(22) NOT NULL,
    tenant_id bigint NOT NULL,
    storage_backend_id character varying(36),
    provider character varying(32) NOT NULL,
    physical_path text NOT NULL,
    location_hash character varying(64) NOT NULL,
    kind character varying(32) DEFAULT 'file'::character varying NOT NULL,
    mime_type character varying(255) DEFAULT ''::character varying NOT NULL,
    original_name character varying(1024) DEFAULT ''::character varying NOT NULL,
    size bigint DEFAULT 0 NOT NULL,
    content_hash character varying(64) DEFAULT ''::character varying NOT NULL,
    lifecycle character varying(16) DEFAULT 'persistent'::character varying NOT NULL,
    expires_at timestamp without time zone,
    state character varying(16) DEFAULT 'active'::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp without time zone
);


--
-- Name: sessions; Type: TABLE; Schema: public
--

CREATE TABLE public.sessions (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    tenant_id integer NOT NULL,
    title character varying(255),
    description text,
    knowledge_base_id character varying(36),
    max_rounds integer DEFAULT 5 NOT NULL,
    enable_rewrite boolean DEFAULT true NOT NULL,
    fallback_strategy character varying(255) DEFAULT 'fixed'::character varying NOT NULL,
    fallback_response text DEFAULT '很抱歉，我暂时无法回答这个问题。'::text NOT NULL,
    keyword_threshold double precision DEFAULT 0.5 NOT NULL,
    vector_threshold double precision DEFAULT 0.5 NOT NULL,
    rerank_model_id character varying(64),
    embedding_top_k integer DEFAULT 10 NOT NULL,
    rerank_top_k integer DEFAULT 10 NOT NULL,
    rerank_threshold double precision DEFAULT 0.65 NOT NULL,
    summary_model_id character varying(64),
    summary_parameters jsonb DEFAULT '{}'::jsonb NOT NULL,
    agent_config jsonb,
    context_config jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    agent_id character varying(36),
    user_id character varying(512),
    is_pinned boolean DEFAULT false NOT NULL,
    pinned_at timestamp with time zone,
    sandbox_config_id character varying(36) DEFAULT NULL::character varying
);


--
-- Name: COLUMN sessions.agent_config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.sessions.agent_config IS 'Session-level agent configuration in JSON format';


--
-- Name: COLUMN sessions.context_config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.sessions.context_config IS 'LLM context management configuration (separate from message storage)';


--
-- Name: COLUMN sessions.sandbox_config_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.sessions.sandbox_config_id IS 'Sandbox config the session current live sandbox was created on; NULL = no live sandbox, ''-'' = deployment default';


--
-- Name: storage_backends; Type: TABLE; Schema: public
--

CREATE TABLE public.storage_backends (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    provider character varying(32) NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    source character varying(16) DEFAULT 'user'::character varying NOT NULL,
    status character varying(16) DEFAULT 'active'::character varying NOT NULL,
    legacy_alias boolean DEFAULT false NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp without time zone
);


--
-- Name: sync_logs; Type: TABLE; Schema: public
--

CREATE TABLE public.sync_logs (
    id character varying(36) NOT NULL,
    data_source_id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
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


--
-- Name: system_settings; Type: TABLE; Schema: public
--

CREATE TABLE public.system_settings (
    id bigint NOT NULL,
    key character varying(128) NOT NULL,
    value jsonb NOT NULL,
    value_type character varying(16) NOT NULL,
    category character varying(32) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    is_secret boolean DEFAULT false NOT NULL,
    requires_restart boolean DEFAULT false NOT NULL,
    last_modified_by character varying(36) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: system_settings_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.system_settings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: system_settings_id_seq; Type: SEQUENCE OWNED BY; Schema: public
--

ALTER SEQUENCE public.system_settings_id_seq OWNED BY public.system_settings.id;


--
-- Name: task_dead_letters; Type: TABLE; Schema: public
--

CREATE TABLE public.task_dead_letters (
    id bigint NOT NULL,
    tenant_id bigint NOT NULL,
    task_type character varying(64) NOT NULL,
    scope character varying(32) NOT NULL,
    scope_id character varying(64) NOT NULL,
    related_id character varying(64) DEFAULT ''::character varying NOT NULL,
    payload jsonb NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    fail_count integer NOT NULL,
    failed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: TABLE task_dead_letters; Type: COMMENT; Schema: public
--

COMMENT ON TABLE public.task_dead_letters IS 'Permanent archive of tasks that exhausted retries. Written by the asynq dead-letter middleware and by service-level retry handlers (e.g. wiki ingest per-doc retries).';


--
-- Name: COLUMN task_dead_letters.related_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.task_dead_letters.related_id IS 'Optional secondary identifier. Wiki ingest puts knowledge_id here so retract/ingest dead letters cluster around the source document.';


--
-- Name: COLUMN task_dead_letters.payload; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.task_dead_letters.payload IS 'Raw task payload (asynq.Task.Payload) at the time of failure. Allows manual requeue via SQL + asynq.Client.Enqueue.';


--
-- Name: COLUMN task_dead_letters.last_error; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.task_dead_letters.last_error IS 'String form of the error that caused the final retry to fail. Long stack traces are kept verbatim.';


--
-- Name: task_dead_letters_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.task_dead_letters_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: task_dead_letters_id_seq; Type: SEQUENCE OWNED BY; Schema: public
--

ALTER SEQUENCE public.task_dead_letters_id_seq OWNED BY public.task_dead_letters.id;


--
-- Name: task_pending_ops; Type: TABLE; Schema: public
--

CREATE TABLE public.task_pending_ops (
    id bigint NOT NULL,
    tenant_id bigint NOT NULL,
    task_type character varying(64) NOT NULL,
    scope character varying(32) NOT NULL,
    scope_id character varying(64) NOT NULL,
    op character varying(32) NOT NULL,
    dedup_key character varying(128) DEFAULT ''::character varying NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    fail_count integer DEFAULT 0 NOT NULL,
    enqueued_at timestamp with time zone DEFAULT now() NOT NULL,
    claimed_at timestamp with time zone
);


--
-- Name: TABLE task_pending_ops; Type: COMMENT; Schema: public
--

COMMENT ON TABLE public.task_pending_ops IS 'Generic durable pending-op queue keyed by (task_type, scope, scope_id). Replaces ad-hoc Redis-list queues that were vulnerable to TTL eviction.';


--
-- Name: COLUMN task_pending_ops.task_type; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.task_pending_ops.task_type IS 'Free-form task identifier, e.g. "wiki:ingest" — should match an asynq task type when applicable.';


--
-- Name: COLUMN task_pending_ops.scope; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.task_pending_ops.scope IS 'Logical scope, e.g. "knowledge_base" / "knowledge" / "tenant". Read together with scope_id.';


--
-- Name: COLUMN task_pending_ops.dedup_key; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.task_pending_ops.dedup_key IS 'Optional service-defined key used by the consumer to de-duplicate equivalent ops within a single batch peek. Empty means no de-dup.';


--
-- Name: COLUMN task_pending_ops.fail_count; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.task_pending_ops.fail_count IS 'In-batch retry counter: the consumer increments it via IncrFailCount and dead-letters once it exceeds a service-defined cap.';


--
-- Name: COLUMN task_pending_ops.claimed_at; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.task_pending_ops.claimed_at IS 'Concurrent-claim marker: set to NOW() when a consumer claims the row (SELECT ... FOR UPDATE SKIP LOCKED). NULL = unclaimed; a value older than the consumer stale threshold is a crashed/abandoned claim and is recoverable. A fresh claim blocks its whole dedup_key so same-document ops never split across concurrent batches.';


--
-- Name: task_pending_ops_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.task_pending_ops_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: task_pending_ops_id_seq; Type: SEQUENCE OWNED BY; Schema: public
--

ALTER SEQUENCE public.task_pending_ops_id_seq OWNED BY public.task_pending_ops.id;


--
-- Name: temporary_documents; Type: TABLE; Schema: public
--

CREATE TABLE public.temporary_documents (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    session_id character varying(36) NOT NULL,
    resource_ref text NOT NULL,
    file_name character varying(1024) NOT NULL,
    file_type character varying(32) NOT NULL,
    mime_type character varying(255) DEFAULT ''::character varying NOT NULL,
    file_size bigint NOT NULL,
    status character varying(16) DEFAULT 'uploaded'::character varying NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    chunks jsonb DEFAULT '[]'::jsonb NOT NULL,
    image_refs jsonb DEFAULT '[]'::jsonb NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    processing_options jsonb DEFAULT '{}'::jsonb NOT NULL,
    token_count integer DEFAULT 0 NOT NULL,
    chunk_count integer DEFAULT 0 NOT NULL,
    error_message text DEFAULT ''::text NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    started_at timestamp without time zone,
    ready_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp without time zone
);


--
-- Name: tenant_api_keys; Type: TABLE; Schema: public
--

CREATE TABLE public.tenant_api_keys (
    id bigint NOT NULL,
    tenant_id integer NOT NULL DEFAULT 1 CHECK (tenant_id = 1),
    name character varying(128) NOT NULL,
    key_hash character varying(64) NOT NULL,
    api_key text DEFAULT ''::text NOT NULL,
    full_access boolean DEFAULT false NOT NULL,
    knowledge_base_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    capabilities jsonb DEFAULT '[]'::jsonb NOT NULL,
    last_used_at timestamp with time zone,
    expires_at timestamp with time zone,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: tenant_api_keys_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.tenant_api_keys_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: tenant_api_keys_id_seq; Type: SEQUENCE OWNED BY; Schema: public
--

ALTER SEQUENCE public.tenant_api_keys_id_seq OWNED BY public.tenant_api_keys.id;


--
-- Name: tenant_invitations; Type: TABLE; Schema: public
--

CREATE TABLE public.tenant_invitations (
    id bigint NOT NULL,
    tenant_id integer NOT NULL,
    invitee_user_id character varying(36) DEFAULT ''::character varying NOT NULL,
    invited_by character varying(36),
    role character varying(20) NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    message character varying(500),
    expires_at timestamp with time zone NOT NULL,
    responded_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone,
    token character varying(64) DEFAULT ''::character varying NOT NULL,
    accepted_count integer DEFAULT 0 NOT NULL
);


--
-- Name: tenant_invitations_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.tenant_invitations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: tenant_invitations_id_seq; Type: SEQUENCE OWNED BY; Schema: public
--

ALTER SEQUENCE public.tenant_invitations_id_seq OWNED BY public.tenant_invitations.id;


--
-- Name: tenant_members; Type: TABLE; Schema: public
--

CREATE TABLE public.tenant_members (
    id bigint NOT NULL,
    user_id character varying(36) NOT NULL,
    tenant_id integer NOT NULL,
    role character varying(20) DEFAULT 'contributor'::character varying NOT NULL,
    status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    invited_by character varying(36),
    joined_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: tenant_members_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.tenant_members_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: tenant_members_id_seq; Type: SEQUENCE OWNED BY; Schema: public
--

ALTER SEQUENCE public.tenant_members_id_seq OWNED BY public.tenant_members.id;


--
-- Name: tenant_sandbox_configs; Type: TABLE; Schema: public
--

CREATE TABLE public.tenant_sandbox_configs (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    description text,
    sandbox_type character varying(32) NOT NULL,
    config jsonb NOT NULL,
    cordoned_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: COLUMN tenant_sandbox_configs.config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenant_sandbox_configs.config IS 'Sandbox backend config (endpoints, encrypted API keys, env vars, volume mount); shape matches types.TenantSandboxConfig';


--
-- Name: COLUMN tenant_sandbox_configs.cordoned_at; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenant_sandbox_configs.cordoned_at IS 'Short lease taken while identity fields (provider/URL/API key) are being changed; sandbox resolution is refused while it is fresh';


--
-- Name: tenant_skill_catalog; Type: TABLE; Schema: public
--

CREATE TABLE public.tenant_skill_catalog (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    version character varying(64),
    description text,
    instructions text,
    bundle_ref character varying(1024),
    bundle_sha256 character varying(64),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: TABLE tenant_skill_catalog; Type: COMMENT; Schema: public
--

COMMENT ON TABLE public.tenant_skill_catalog IS 'Workspace skill definition. Installations onto sandbox configs live in tenant_skills.';


--
-- Name: tenant_skill_snapshots; Type: TABLE; Schema: public
--

CREATE TABLE public.tenant_skill_snapshots (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    sandbox_config_id character varying(36) NOT NULL,
    skill_id character varying(36),
    snapshot_id character varying(255),
    parent_snapshot_id character varying(255),
    generation integer DEFAULT 0 NOT NULL,
    trigger character varying(16) NOT NULL,
    state character varying(16) NOT NULL,
    superseded_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    planned_name character varying(255)
);


--
-- Name: TABLE tenant_skill_snapshots; Type: COMMENT; Schema: public
--

COMMENT ON TABLE public.tenant_skill_snapshots IS 'Image chain ledger. Old snapshots are kept (state=superseded), never deleted on switch, so the recorded IDs stay resolvable.';


--
-- Name: COLUMN tenant_skill_snapshots.planned_name; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenant_skill_snapshots.planned_name IS 'Name passed to CreateSnapshot, written before the provider call so an abandoned build stays identifiable';


--
-- Name: tenant_skills; Type: TABLE; Schema: public
--

CREATE TABLE public.tenant_skills (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    sandbox_config_id character varying(36) NOT NULL,
    name character varying(255) NOT NULL,
    version character varying(64),
    description text,
    instructions text,
    bundle_ref character varying(1024),
    bundle_sha256 character varying(64),
    enabled boolean DEFAULT true NOT NULL,
    installed_snapshot_id character varying(255),
    status character varying(32) NOT NULL,
    error text,
    installing_since timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    install_session_id character varying(36),
    install_message_id character varying(36),
    envs jsonb,
    catalog_id character varying(36)
);


--
-- Name: COLUMN tenant_skills.name; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenant_skills.name IS 'Also the directory name inside the image: /opt/reliquary/tenant/skills/<name>';


--
-- Name: COLUMN tenant_skills.enabled; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenant_skills.enabled IS 'Visibility to the agent only; files stay in the image until the skill is removed';


--
-- Name: COLUMN tenant_skills.install_session_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenant_skills.install_session_id IS 'Maintenance session of the most recent install; overwritten on re-install';


--
-- Name: COLUMN tenant_skills.install_message_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenant_skills.install_message_id IS 'Assistant message the installer transcript was streamed into';


--
-- Name: COLUMN tenant_skills.envs; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenant_skills.envs IS 'Installer-agent declaration [{name,description,required,value}]. value is the workspace-wide admin value and is AES-GCM encrypted; the rest is plaintext so the UI can render it without a key.';


--
-- Name: tenant_user_env_vars; Type: TABLE; Schema: public
--

CREATE TABLE public.tenant_user_env_vars (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    principal_type character varying(32) NOT NULL,
    principal_id character varying(512) NOT NULL,
    sandbox_config_id character varying(36) NOT NULL,
    skill_id character varying(36) DEFAULT ''::character varying NOT NULL,
    name character varying(255) NOT NULL,
    value text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: TABLE tenant_user_env_vars; Type: COMMENT; Schema: public
--

COMMENT ON TABLE public.tenant_user_env_vars IS 'One principal''s own environment variable. An empty skill_id applies to every execution on the sandbox config; a skill_id scopes it to that skill''s declaration. Keyed by principal, not user_id: the IM path stores a synthetic tenant account in the user ID, which would make every IM user of a workspace share one value.';


--
-- Name: COLUMN tenant_user_env_vars.value; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenant_user_env_vars.value IS 'AES-GCM encrypted. Never returned by any endpoint.';


--
-- Name: tenants; Type: TABLE; Schema: public
--

CREATE TABLE public.tenants (
    model_tiers jsonb DEFAULT '{}'::jsonb NOT NULL,
    id integer NOT NULL CHECK (id = 1),
    name character varying(255) NOT NULL,
    description text,
    retriever_engines jsonb DEFAULT '[]'::jsonb NOT NULL,
    status character varying(50) DEFAULT 'active'::character varying,
    business character varying(255) NOT NULL,
    storage_quota bigint DEFAULT '10737418240'::bigint NOT NULL,
    storage_used bigint DEFAULT 0 NOT NULL,
    agent_config jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    context_config jsonb,
    conversation_config jsonb,
    web_search_config jsonb,
    parser_engine_config jsonb,
    storage_engine_config jsonb,
    chat_history_config jsonb,
    retrieval_config jsonb,
    credentials jsonb,
    api_principal_config jsonb,
    default_storage_backend_id character varying(36),
    memory_config jsonb
);


--
-- Name: COLUMN tenants.agent_config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenants.agent_config IS 'Tenant-level agent configuration in JSON format';


--
-- Name: COLUMN tenants.context_config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenants.context_config IS 'Global Context configuration for this tenant (default for all sessions)';


--
-- Name: COLUMN tenants.conversation_config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenants.conversation_config IS 'Global Conversation configuration for this tenant (default for normal mode sessions)';


--
-- Name: COLUMN tenants.web_search_config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenants.web_search_config IS 'Web search configuration for the tenant';


--
-- Name: COLUMN tenants.parser_engine_config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenants.parser_engine_config IS 'Parser engine overrides (mineru_endpoint, mineru_api_key, etc.); takes precedence over env when parsing';


--
-- Name: COLUMN tenants.storage_engine_config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenants.storage_engine_config IS 'Storage engine parameters for Local, MinIO, COS; used for document/file storage and docreader';


--
-- Name: COLUMN tenants.credentials; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenants.credentials IS 'Third-party provider credentials (e.g. WeKnoraCloud AppID/AppSecret); encrypted at application level';


--
-- Name: tenants_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.tenants_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: tenants_id_seq; Type: SEQUENCE OWNED BY; Schema: public
--

ALTER SEQUENCE public.tenants_id_seq OWNED BY public.tenants.id;


--
-- Name: user_kb_pins; Type: TABLE; Schema: public
--

CREATE TABLE public.user_kb_pins (
    tenant_id bigint NOT NULL,
    user_id character varying(36) NOT NULL,
    kb_id character varying(36) NOT NULL,
    pinned_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: user_resource_favorites; Type: TABLE; Schema: public
--

CREATE TABLE public.user_resource_favorites (
    user_id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    resource_type character varying(16) NOT NULL,
    resource_id character varying(64) NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: users; Type: TABLE; Schema: public
--

CREATE TABLE public.users (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    username character varying(100) NOT NULL,
    email character varying(255) NOT NULL,
    password_hash character varying(255) NOT NULL,
    avatar character varying(500),
    tenant_id integer NOT NULL DEFAULT 1 CHECK (tenant_id = 1),
    is_active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    is_system_admin boolean DEFAULT false NOT NULL
);


--
-- Name: TABLE users; Type: COMMENT; Schema: public
--

COMMENT ON TABLE public.users IS 'User accounts in the system';


--
-- Name: COLUMN users.id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.users.id IS 'Unique identifier of the user';


--
-- Name: COLUMN users.username; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.users.username IS 'Username of the user';


--
-- Name: COLUMN users.email; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.users.email IS 'Email address of the user';


--
-- Name: COLUMN users.password_hash; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.users.password_hash IS 'Hashed password of the user';


--
-- Name: COLUMN users.avatar; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.users.avatar IS 'Avatar URL of the user';


--
-- Name: COLUMN users.tenant_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.users.tenant_id IS 'Tenant ID that the user belongs to';


--
-- Name: COLUMN users.is_active; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.users.is_active IS 'Whether the user is active';



--
-- Name: COLUMN users.is_system_admin; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.users.is_system_admin IS 'Whether the user is a system administrator (independent of tenant roles)';


--
-- Name: vector_stores; Type: TABLE; Schema: public
--

CREATE TABLE public.vector_stores (
    id character varying(36) NOT NULL,
    name character varying(255) NOT NULL,
    engine_type character varying(50) NOT NULL,
    connection_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    index_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    tenant_id bigint NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp without time zone
);


--
-- Name: web_search_providers; Type: TABLE; Schema: public
--

CREATE TABLE public.web_search_providers (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    provider character varying(50) NOT NULL,
    description text,
    parameters jsonb,
    is_default boolean DEFAULT false,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp without time zone
);


--
-- Name: wiki_folders; Type: TABLE; Schema: public
--

CREATE TABLE public.wiki_folders (
    id character varying(36) NOT NULL,
    tenant_id bigint DEFAULT 0 NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    parent_id character varying(36) DEFAULT ''::character varying NOT NULL,
    name character varying(255) NOT NULL,
    path character varying(1024) DEFAULT ''::character varying NOT NULL,
    depth integer DEFAULT 0 NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: wiki_page_issues; Type: TABLE; Schema: public
--

CREATE TABLE public.wiki_page_issues (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    slug character varying(255) NOT NULL,
    issue_type character varying(50) NOT NULL,
    description text NOT NULL,
    suspected_knowledge_ids jsonb,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    reported_by character varying(100) NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone
);


--
-- Name: wiki_page_revisions; Type: TABLE; Schema: public
--

CREATE TABLE public.wiki_page_revisions (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    page_id character varying(36) NOT NULL,
    slug character varying(255) NOT NULL,
    version integer NOT NULL,
    title character varying(512) DEFAULT ''::character varying NOT NULL,
    page_type character varying(32) DEFAULT 'summary'::character varying NOT NULL,
    status character varying(32) DEFAULT 'published'::character varying NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    summary text DEFAULT ''::text NOT NULL,
    aliases jsonb DEFAULT '[]'::jsonb,
    edit_source character varying(16) DEFAULT ''::character varying NOT NULL,
    editor_id character varying(64) DEFAULT ''::character varying NOT NULL,
    edited_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: wiki_pages; Type: TABLE; Schema: public
--

CREATE TABLE public.wiki_pages (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    slug character varying(255) NOT NULL,
    title character varying(512) DEFAULT ''::character varying NOT NULL,
    page_type character varying(32) DEFAULT 'summary'::character varying NOT NULL,
    status character varying(32) DEFAULT 'published'::character varying NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    summary text DEFAULT ''::text NOT NULL,
    parent_slug character varying(255) DEFAULT ''::character varying NOT NULL,
    folder_id character varying(36) DEFAULT ''::character varying NOT NULL,
    category_path jsonb DEFAULT '[]'::jsonb,
    wiki_path character varying(1024) DEFAULT ''::character varying NOT NULL,
    depth integer DEFAULT 0 NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    source_refs jsonb DEFAULT '[]'::jsonb,
    chunk_refs jsonb DEFAULT '[]'::jsonb,
    source_changes jsonb DEFAULT '{}'::jsonb NOT NULL,
    in_links jsonb DEFAULT '[]'::jsonb,
    out_links jsonb DEFAULT '[]'::jsonb,
    page_metadata jsonb DEFAULT '{}'::jsonb,
    aliases jsonb DEFAULT '[]'::jsonb,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    last_edit_source character varying(16) DEFAULT ''::character varying NOT NULL,
    last_editor_id character varying(64) DEFAULT ''::character varying NOT NULL
);


--
-- Name: COLUMN wiki_pages.last_edit_source; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.wiki_pages.last_edit_source IS 'Author kind of the current version: pipeline | agent | user | revert ('''' = legacy, treated as pipeline)';


--
-- Name: COLUMN wiki_pages.last_editor_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.wiki_pages.last_editor_id IS 'User id of the caller that produced the current version (empty for background pipeline writes)';


--
-- Name: audit_logs id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.audit_logs ALTER COLUMN id SET DEFAULT nextval('public.audit_logs_id_seq'::regclass);


--
-- Name: embeddings id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.embeddings ALTER COLUMN id SET DEFAULT nextval('public.embeddings_id_seq'::regclass);


--
-- Name: knowledge_processing_spans id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.knowledge_processing_spans ALTER COLUMN id SET DEFAULT nextval('public.knowledge_processing_spans_id_seq'::regclass);


--
-- Name: message_suggestion_events id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.message_suggestion_events ALTER COLUMN id SET DEFAULT nextval('public.message_suggestion_events_id_seq'::regclass);


--
-- Name: system_settings id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.system_settings ALTER COLUMN id SET DEFAULT nextval('public.system_settings_id_seq'::regclass);


--
-- Name: task_dead_letters id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.task_dead_letters ALTER COLUMN id SET DEFAULT nextval('public.task_dead_letters_id_seq'::regclass);


--
-- Name: task_pending_ops id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.task_pending_ops ALTER COLUMN id SET DEFAULT nextval('public.task_pending_ops_id_seq'::regclass);


--
-- Name: tenant_api_keys id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.tenant_api_keys ALTER COLUMN id SET DEFAULT nextval('public.tenant_api_keys_id_seq'::regclass);


--
-- Name: tenant_invitations id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.tenant_invitations ALTER COLUMN id SET DEFAULT nextval('public.tenant_invitations_id_seq'::regclass);


--
-- Name: tenant_members id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.tenant_members ALTER COLUMN id SET DEFAULT nextval('public.tenant_members_id_seq'::regclass);


--
-- Name: tenants id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.tenants ALTER COLUMN id SET DEFAULT nextval('public.tenants_id_seq'::regclass);


--
-- Name: audit_logs audit_logs_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_pkey PRIMARY KEY (id);


--
-- Name: auth_tokens auth_tokens_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.auth_tokens
    ADD CONSTRAINT auth_tokens_pkey PRIMARY KEY (id);


--
-- Name: chunk_revisions chunk_revisions_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.chunk_revisions
    ADD CONSTRAINT chunk_revisions_pkey PRIMARY KEY (id);


--
-- Name: chunks chunks_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.chunks
    ADD CONSTRAINT chunks_pkey PRIMARY KEY (id);


--
-- Name: custom_agents custom_agents_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.custom_agents
    ADD CONSTRAINT custom_agents_pkey PRIMARY KEY (id, tenant_id);


--
-- Name: data_sources data_sources_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.data_sources
    ADD CONSTRAINT data_sources_pkey PRIMARY KEY (id);


--
-- Name: embeddings embeddings_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.embeddings
    ADD CONSTRAINT embeddings_pkey PRIMARY KEY (id);


--
-- Name: im_channel_sessions im_channel_sessions_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.im_channel_sessions
    ADD CONSTRAINT im_channel_sessions_pkey PRIMARY KEY (id);


--
-- Name: im_channels im_channels_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.im_channels
    ADD CONSTRAINT im_channels_pkey PRIMARY KEY (id);


--
-- Name: knowledge_anomalies knowledge_anomalies_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_anomalies
    ADD CONSTRAINT knowledge_anomalies_pkey PRIMARY KEY (id);


--
-- Name: knowledge_bases knowledge_bases_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_bases
    ADD CONSTRAINT knowledge_bases_pkey PRIMARY KEY (id);


--
-- Name: knowledge_context_attention knowledge_context_attention_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_context_attention
    ADD CONSTRAINT knowledge_context_attention_pkey PRIMARY KEY (id);


--
-- Name: knowledge_context_entities knowledge_context_entities_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_context_entities
    ADD CONSTRAINT knowledge_context_entities_pkey PRIMARY KEY (id);


--
-- Name: knowledge_context_entity_aliases knowledge_context_entity_aliases_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_context_entity_aliases
    ADD CONSTRAINT knowledge_context_entity_aliases_pkey PRIMARY KEY (id);


--
-- Name: knowledge_context_event_evidence knowledge_context_event_evidence_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_context_event_evidence
    ADD CONSTRAINT knowledge_context_event_evidence_pkey PRIMARY KEY (id);


--
-- Name: knowledge_context_events knowledge_context_events_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_context_events
    ADD CONSTRAINT knowledge_context_events_pkey PRIMARY KEY (id);


--
-- Name: knowledge_context_evidence knowledge_context_evidence_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_context_evidence
    ADD CONSTRAINT knowledge_context_evidence_pkey PRIMARY KEY (id);


--
-- Name: knowledge_context_facts knowledge_context_facts_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_context_facts
    ADD CONSTRAINT knowledge_context_facts_pkey PRIMARY KEY (id);


--
-- Name: knowledge_processing_spans knowledge_processing_spans_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_processing_spans
    ADD CONSTRAINT knowledge_processing_spans_pkey PRIMARY KEY (id);


--
-- Name: knowledge_strategy_versions knowledge_strategy_versions_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_strategy_versions
    ADD CONSTRAINT knowledge_strategy_versions_pkey PRIMARY KEY (id);


--
-- Name: knowledge_tag_manual_decisions knowledge_tag_manual_decisions_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_tag_manual_decisions
    ADD CONSTRAINT knowledge_tag_manual_decisions_pkey PRIMARY KEY (knowledge_id, tag_id);


--
-- Name: knowledge_tag_relations knowledge_tag_relations_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_tag_relations
    ADD CONSTRAINT knowledge_tag_relations_pkey PRIMARY KEY (knowledge_id, tag_id);


--
-- Name: knowledge_tags knowledge_tags_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_tags
    ADD CONSTRAINT knowledge_tags_pkey PRIMARY KEY (id);


--
-- Name: knowledge_workflow_reports knowledge_workflow_reports_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_workflow_reports
    ADD CONSTRAINT knowledge_workflow_reports_pkey PRIMARY KEY (id);


--
-- Name: knowledge_workflow_reports knowledge_workflow_reports_run_id_step_id_key; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_workflow_reports
    ADD CONSTRAINT knowledge_workflow_reports_run_id_step_id_key UNIQUE (run_id, step_id);


--
-- Name: knowledge_workflow_runs knowledge_workflow_runs_idempotency_key_key; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_workflow_runs
    ADD CONSTRAINT knowledge_workflow_runs_idempotency_key_key UNIQUE (idempotency_key);


--
-- Name: knowledge_workflow_runs knowledge_workflow_runs_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_workflow_runs
    ADD CONSTRAINT knowledge_workflow_runs_pkey PRIMARY KEY (id);


--
-- Name: knowledge_workflow_step_runs knowledge_workflow_step_runs_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_workflow_step_runs
    ADD CONSTRAINT knowledge_workflow_step_runs_pkey PRIMARY KEY (id);


--
-- Name: knowledge_workflow_step_runs knowledge_workflow_step_runs_run_id_step_id_key; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_workflow_step_runs
    ADD CONSTRAINT knowledge_workflow_step_runs_run_id_step_id_key UNIQUE (run_id, step_id);


--
-- Name: knowledge_workflows knowledge_workflows_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_workflows
    ADD CONSTRAINT knowledge_workflows_pkey PRIMARY KEY (id);


--
-- Name: knowledge_workflows knowledge_workflows_tenant_id_knowledge_base_id_name_key; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_workflows
    ADD CONSTRAINT knowledge_workflows_tenant_id_knowledge_base_id_name_key UNIQUE (tenant_id, knowledge_base_id, name);


--
-- Name: knowledges knowledges_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledges
    ADD CONSTRAINT knowledges_pkey PRIMARY KEY (id);


--
-- Name: mcp_oauth_clients mcp_oauth_clients_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.mcp_oauth_clients
    ADD CONSTRAINT mcp_oauth_clients_pkey PRIMARY KEY (id);


--
-- Name: mcp_oauth_tokens mcp_oauth_tokens_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.mcp_oauth_tokens
    ADD CONSTRAINT mcp_oauth_tokens_pkey PRIMARY KEY (id);


--
-- Name: mcp_services mcp_services_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.mcp_services
    ADD CONSTRAINT mcp_services_pkey PRIMARY KEY (id);


--
-- Name: mcp_tool_approvals mcp_tool_approvals_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.mcp_tool_approvals
    ADD CONSTRAINT mcp_tool_approvals_pkey PRIMARY KEY (id);


--
-- Name: memory_doc_affinity memory_doc_affinity_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.memory_doc_affinity
    ADD CONSTRAINT memory_doc_affinity_pkey PRIMARY KEY (id);


--
-- Name: memory_item_embeddings memory_item_embeddings_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.memory_item_embeddings
    ADD CONSTRAINT memory_item_embeddings_pkey PRIMARY KEY (item_id);


--
-- Name: memory_items memory_items_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.memory_items
    ADD CONSTRAINT memory_items_pkey PRIMARY KEY (id);


--
-- Name: memory_subjects memory_subjects_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.memory_subjects
    ADD CONSTRAINT memory_subjects_pkey PRIMARY KEY (id);


--
-- Name: memory_tombstones memory_tombstones_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.memory_tombstones
    ADD CONSTRAINT memory_tombstones_pkey PRIMARY KEY (id);


--
-- Name: memory_topic_stats memory_topic_stats_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.memory_topic_stats
    ADD CONSTRAINT memory_topic_stats_pkey PRIMARY KEY (id);


--
-- Name: message_suggestion_events message_suggestion_events_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.message_suggestion_events
    ADD CONSTRAINT message_suggestion_events_pkey PRIMARY KEY (id);


--
-- Name: message_suggestion_sets message_suggestion_sets_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.message_suggestion_sets
    ADD CONSTRAINT message_suggestion_sets_pkey PRIMARY KEY (id);


--
-- Name: messages messages_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_pkey PRIMARY KEY (id);


--
-- Name: models models_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.models
    ADD CONSTRAINT models_pkey PRIMARY KEY (id);


--
-- Name: resource_access_grants resource_access_grants_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.resource_access_grants
    ADD CONSTRAINT resource_access_grants_pkey PRIMARY KEY (id);


--
-- Name: resource_access_grants resource_access_grants_token_hash_key; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.resource_access_grants
    ADD CONSTRAINT resource_access_grants_token_hash_key UNIQUE (token_hash);


--
-- Name: resource_bindings resource_bindings_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.resource_bindings
    ADD CONSTRAINT resource_bindings_pkey PRIMARY KEY (id);


--
-- Name: resources resources_handle_key; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.resources
    ADD CONSTRAINT resources_handle_key UNIQUE (handle);


--
-- Name: resources resources_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.resources
    ADD CONSTRAINT resources_pkey PRIMARY KEY (id);


--
-- Name: sessions sessions_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);


--
-- Name: storage_backends storage_backends_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.storage_backends
    ADD CONSTRAINT storage_backends_pkey PRIMARY KEY (id);


--
-- Name: sync_logs sync_logs_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.sync_logs
    ADD CONSTRAINT sync_logs_pkey PRIMARY KEY (id);


--
-- Name: system_settings system_settings_key_key; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.system_settings
    ADD CONSTRAINT system_settings_key_key UNIQUE (key);


--
-- Name: system_settings system_settings_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.system_settings
    ADD CONSTRAINT system_settings_pkey PRIMARY KEY (id);


--
-- Name: task_dead_letters task_dead_letters_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.task_dead_letters
    ADD CONSTRAINT task_dead_letters_pkey PRIMARY KEY (id);


--
-- Name: task_pending_ops task_pending_ops_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.task_pending_ops
    ADD CONSTRAINT task_pending_ops_pkey PRIMARY KEY (id);


--
-- Name: temporary_documents temporary_documents_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.temporary_documents
    ADD CONSTRAINT temporary_documents_pkey PRIMARY KEY (id);


--
-- Name: tenant_api_keys tenant_api_keys_key_hash_key; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.tenant_api_keys
    ADD CONSTRAINT tenant_api_keys_key_hash_key UNIQUE (key_hash);


--
-- Name: tenant_api_keys tenant_api_keys_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.tenant_api_keys
    ADD CONSTRAINT tenant_api_keys_pkey PRIMARY KEY (id);


--
-- Name: tenant_invitations tenant_invitations_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.tenant_invitations
    ADD CONSTRAINT tenant_invitations_pkey PRIMARY KEY (id);


--
-- Name: tenant_members tenant_members_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.tenant_members
    ADD CONSTRAINT tenant_members_pkey PRIMARY KEY (id);


--
-- Name: tenant_sandbox_configs tenant_sandbox_configs_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.tenant_sandbox_configs
    ADD CONSTRAINT tenant_sandbox_configs_pkey PRIMARY KEY (id);


--
-- Name: tenant_skill_catalog tenant_skill_catalog_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.tenant_skill_catalog
    ADD CONSTRAINT tenant_skill_catalog_pkey PRIMARY KEY (id);


--
-- Name: tenant_skill_snapshots tenant_skill_snapshots_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.tenant_skill_snapshots
    ADD CONSTRAINT tenant_skill_snapshots_pkey PRIMARY KEY (id);


--
-- Name: tenant_skills tenant_skills_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.tenant_skills
    ADD CONSTRAINT tenant_skills_pkey PRIMARY KEY (id);


--
-- Name: tenant_user_env_vars tenant_user_env_vars_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.tenant_user_env_vars
    ADD CONSTRAINT tenant_user_env_vars_pkey PRIMARY KEY (id);


--
-- Name: tenants tenants_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_pkey PRIMARY KEY (id);


--
-- Name: knowledge_processing_spans uq_kpspan_attempt_span; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_processing_spans
    ADD CONSTRAINT uq_kpspan_attempt_span UNIQUE (knowledge_id, attempt, span_id);


--
-- Name: user_kb_pins user_kb_pins_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.user_kb_pins
    ADD CONSTRAINT user_kb_pins_pkey PRIMARY KEY (tenant_id, user_id, kb_id);


--
-- Name: user_resource_favorites user_resource_favorites_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.user_resource_favorites
    ADD CONSTRAINT user_resource_favorites_pkey PRIMARY KEY (user_id, tenant_id, resource_type, resource_id);


--
-- Name: users users_email_key; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_email_key UNIQUE (email);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: users users_username_key; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_username_key UNIQUE (username);


--
-- Name: vector_stores vector_stores_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.vector_stores
    ADD CONSTRAINT vector_stores_pkey PRIMARY KEY (id);


--
-- Name: web_search_providers web_search_providers_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.web_search_providers
    ADD CONSTRAINT web_search_providers_pkey PRIMARY KEY (id);


--
-- Name: wiki_folders wiki_folders_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.wiki_folders
    ADD CONSTRAINT wiki_folders_pkey PRIMARY KEY (id);


--
-- Name: wiki_page_issues wiki_page_issues_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.wiki_page_issues
    ADD CONSTRAINT wiki_page_issues_pkey PRIMARY KEY (id);


--
-- Name: wiki_page_revisions wiki_page_revisions_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.wiki_page_revisions
    ADD CONSTRAINT wiki_page_revisions_pkey PRIMARY KEY (id);


--
-- Name: wiki_pages wiki_pages_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.wiki_pages
    ADD CONSTRAINT wiki_pages_pkey PRIMARY KEY (id);


--
-- Name: embeddings_embedding_idx_1024; Type: INDEX; Schema: public
--

CREATE INDEX embeddings_embedding_idx_1024 ON public.embeddings USING hnsw (((embedding)::public.halfvec(1024)) public.halfvec_cosine_ops) WITH (m='16', ef_construction='64') WHERE (dimension = 1024);


--
-- Name: embeddings_embedding_idx_3584; Type: INDEX; Schema: public
--

CREATE INDEX embeddings_embedding_idx_3584 ON public.embeddings USING hnsw (((embedding)::public.halfvec(3584)) public.halfvec_cosine_ops) WITH (m='16', ef_construction='64') WHERE (dimension = 3584);


--
-- Name: embeddings_embedding_idx_798; Type: INDEX; Schema: public
--

CREATE INDEX embeddings_embedding_idx_798 ON public.embeddings USING hnsw (((embedding)::public.halfvec(798)) public.halfvec_cosine_ops) WITH (m='16', ef_construction='64') WHERE (dimension = 798);


--
-- Name: embeddings_unique_source; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX embeddings_unique_source ON public.embeddings USING btree (source_id, source_type);


--
-- Name: idx_audit_logs_actor; Type: INDEX; Schema: public
--

CREATE INDEX idx_audit_logs_actor ON public.audit_logs USING btree (actor_user_id);


--
-- Name: idx_audit_logs_created_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_audit_logs_created_at ON public.audit_logs USING btree (created_at);


--
-- Name: idx_audit_logs_tenant_action; Type: INDEX; Schema: public
--

CREATE INDEX idx_audit_logs_tenant_action ON public.audit_logs USING btree (tenant_id, action);


--
-- Name: idx_audit_logs_tenant_id_desc; Type: INDEX; Schema: public
--

CREATE INDEX idx_audit_logs_tenant_id_desc ON public.audit_logs USING btree (tenant_id, id DESC);


--
-- Name: idx_audit_logs_tenant_scope_desc; Type: INDEX; Schema: public
--

CREATE INDEX idx_audit_logs_tenant_scope_desc ON public.audit_logs USING btree (tenant_id, scope_type, scope_id, id DESC);


--
-- Name: idx_auth_tokens_expires_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_auth_tokens_expires_at ON public.auth_tokens USING btree (expires_at);


--
-- Name: idx_auth_tokens_token; Type: INDEX; Schema: public
--

CREATE INDEX idx_auth_tokens_token ON public.auth_tokens USING btree (token);


--
-- Name: idx_auth_tokens_token_type; Type: INDEX; Schema: public
--

CREATE INDEX idx_auth_tokens_token_type ON public.auth_tokens USING btree (token_type);


--
-- Name: idx_auth_tokens_user_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_auth_tokens_user_id ON public.auth_tokens USING btree (user_id);


--
-- Name: idx_channel_lookup; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_channel_lookup ON public.im_channel_sessions USING btree (platform, user_id, chat_id, tenant_id, agent_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_channel_thread_lookup; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_channel_thread_lookup ON public.im_channel_sessions USING btree (platform, chat_id, thread_id, tenant_id, agent_id) WHERE ((deleted_at IS NULL) AND ((thread_id)::text <> ''::text));


--
-- Name: idx_chunk_revisions_chunk_revision; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_chunk_revisions_chunk_revision ON public.chunk_revisions USING btree (chunk_id, revision);


--
-- Name: idx_chunk_revisions_tenant_chunk; Type: INDEX; Schema: public
--

CREATE INDEX idx_chunk_revisions_tenant_chunk ON public.chunk_revisions USING btree (tenant_id, chunk_id);


--
-- Name: idx_chunks_chunk_type; Type: INDEX; Schema: public
--

CREATE INDEX idx_chunks_chunk_type ON public.chunks USING btree (chunk_type);


--
-- Name: idx_chunks_content_hash; Type: INDEX; Schema: public
--

CREATE INDEX idx_chunks_content_hash ON public.chunks USING btree (content_hash);


--
-- Name: idx_chunks_kb_tenant; Type: INDEX; Schema: public
--

CREATE INDEX idx_chunks_kb_tenant ON public.chunks USING btree (knowledge_base_id, tenant_id);


--
-- Name: idx_chunks_knowledge_enabled; Type: INDEX; Schema: public
--

CREATE INDEX idx_chunks_knowledge_enabled ON public.chunks USING btree (knowledge_id, is_enabled, deleted_at);


--
-- Name: idx_chunks_parent_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_chunks_parent_id ON public.chunks USING btree (parent_chunk_id);


--
-- Name: idx_chunks_seq_id; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_chunks_seq_id ON public.chunks USING btree (seq_id);

-- Search the editable body and its short retrieval aliases without a body copy.
CREATE INDEX chunks_search_idx ON public.chunks USING bm25
    (seq_id, knowledge_base_id, knowledge_id, tag_id, is_enabled, chunk_type,
     index_status, deleted_at, content, context_header, search_title, metadata)
    WITH (key_field=seq_id, text_fields='{
        "content": {"tokenizer": {"type": "chinese_lindera"}},
        "context_header": {"tokenizer": {"type": "chinese_lindera"}},
        "search_title": {"tokenizer": {"type": "chinese_lindera"}}
    }', json_fields='{
        "metadata": {"tokenizer": {"type": "chinese_lindera"}}
    }');


--
-- Name: idx_chunks_tag; Type: INDEX; Schema: public
--

CREATE INDEX idx_chunks_tag ON public.chunks USING btree (tag_id);


--
-- Name: idx_chunks_tenant_kg; Type: INDEX; Schema: public
--

CREATE INDEX idx_chunks_tenant_kg ON public.chunks USING btree (tenant_id, knowledge_id);


--
-- Name: idx_custom_agents_deleted_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_custom_agents_deleted_at ON public.custom_agents USING btree (deleted_at);


--
-- Name: idx_custom_agents_is_builtin; Type: INDEX; Schema: public
--

CREATE INDEX idx_custom_agents_is_builtin ON public.custom_agents USING btree (is_builtin);


--
-- Name: idx_custom_agents_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_custom_agents_tenant_id ON public.custom_agents USING btree (tenant_id);


--
-- Name: idx_data_sources_deleted_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_data_sources_deleted_at ON public.data_sources USING btree (deleted_at);


--
-- Name: idx_data_sources_knowledge_base_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_data_sources_knowledge_base_id ON public.data_sources USING btree (knowledge_base_id);


--
-- Name: idx_data_sources_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_data_sources_status ON public.data_sources USING btree (status);


--
-- Name: idx_data_sources_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_data_sources_tenant_id ON public.data_sources USING btree (tenant_id);


--
-- Name: idx_data_sources_type; Type: INDEX; Schema: public
--

CREATE INDEX idx_data_sources_type ON public.data_sources USING btree (type);


--
-- Name: idx_embeddings_is_enabled; Type: INDEX; Schema: public
--

CREATE INDEX idx_embeddings_is_enabled ON public.embeddings USING btree (is_enabled);


--
-- Name: idx_embeddings_knowledge_base_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_embeddings_knowledge_base_id ON public.embeddings USING btree (knowledge_base_id);


--
-- Name: idx_embeddings_tag_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_embeddings_tag_id ON public.embeddings USING btree (tag_id);


--
-- Name: idx_im_channel_deleted; Type: INDEX; Schema: public
--

CREATE INDEX idx_im_channel_deleted ON public.im_channel_sessions USING btree (deleted_at) WHERE (deleted_at IS NOT NULL);


--
-- Name: idx_im_channel_session; Type: INDEX; Schema: public
--

CREATE INDEX idx_im_channel_session ON public.im_channel_sessions USING btree (session_id);


--
-- Name: idx_im_channel_sessions_channel; Type: INDEX; Schema: public
--

CREATE INDEX idx_im_channel_sessions_channel ON public.im_channel_sessions USING btree (im_channel_id) WHERE ((im_channel_id)::text <> ''::text);


--
-- Name: idx_im_channel_tenant; Type: INDEX; Schema: public
--

CREATE INDEX idx_im_channel_tenant ON public.im_channel_sessions USING btree (tenant_id);


--
-- Name: idx_im_channels_agent; Type: INDEX; Schema: public
--

CREATE INDEX idx_im_channels_agent ON public.im_channels USING btree (agent_id);


--
-- Name: idx_im_channels_bot_identity; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_im_channels_bot_identity ON public.im_channels USING btree (bot_identity) WHERE ((deleted_at IS NULL) AND ((bot_identity)::text <> ''::text));


--
-- Name: idx_im_channels_deleted; Type: INDEX; Schema: public
--

CREATE INDEX idx_im_channels_deleted ON public.im_channels USING btree (deleted_at) WHERE (deleted_at IS NOT NULL);


--
-- Name: idx_im_channels_tenant; Type: INDEX; Schema: public
--

CREATE INDEX idx_im_channels_tenant ON public.im_channels USING btree (tenant_id);


--
-- Name: idx_kca_evidence; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_kca_evidence ON public.knowledge_context_attention USING btree (tenant_id, knowledge_base_id, evidence_id);


--
-- Name: idx_kca_scope_score; Type: INDEX; Schema: public
--

CREATE INDEX idx_kca_scope_score ON public.knowledge_context_attention USING btree (tenant_id, knowledge_base_id, score DESC);


--
-- Name: idx_kce_canonical; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_kce_canonical ON public.knowledge_context_entities USING btree (tenant_id, entity_type, normalized_name);


--
-- Name: idx_kce_scope_name; Type: INDEX; Schema: public
--

CREATE INDEX idx_kce_scope_name ON public.knowledge_context_entities USING btree (tenant_id, entity_type, canonical_name);


--
-- Name: idx_kcea_alias; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_kcea_alias ON public.knowledge_context_entity_aliases USING btree (tenant_id, entity_type, normalized_alias);


--
-- Name: idx_kcea_entity; Type: INDEX; Schema: public
--

CREATE INDEX idx_kcea_entity ON public.knowledge_context_entity_aliases USING btree (tenant_id, entity_id);


--
-- Name: idx_kcee_evidence; Type: INDEX; Schema: public
--

CREATE INDEX idx_kcee_evidence ON public.knowledge_context_event_evidence USING btree (tenant_id, knowledge_base_id, evidence_id);


--
-- Name: idx_kcee_pair; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_kcee_pair ON public.knowledge_context_event_evidence USING btree (tenant_id, event_id, evidence_id);

-- Event objects have one identity per event and retain source-level support.
ALTER TABLE public.knowledge_context_events ADD CONSTRAINT uq_kcevent_team_id UNIQUE (tenant_id, id);
ALTER TABLE public.knowledge_context_entities ADD CONSTRAINT uq_kcentity_team_id UNIQUE (tenant_id, id);

CREATE TABLE public.knowledge_context_event_entities (
    tenant_id bigint NOT NULL,
    event_id character varying(36) NOT NULL,
    entity_id character varying(36) NOT NULL,
    PRIMARY KEY (tenant_id, event_id, entity_id),
    FOREIGN KEY (tenant_id, event_id) REFERENCES public.knowledge_context_events (tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, entity_id) REFERENCES public.knowledge_context_entities (tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_kceentity_object ON public.knowledge_context_event_entities (tenant_id, entity_id, event_id);

CREATE TABLE public.knowledge_context_event_evidence_entities (
    tenant_id bigint NOT NULL,
    event_id character varying(36) NOT NULL,
    evidence_id character varying(36) NOT NULL,
    entity_id character varying(36) NOT NULL,
    PRIMARY KEY (tenant_id, event_id, evidence_id, entity_id),
    FOREIGN KEY (tenant_id, event_id, entity_id) REFERENCES public.knowledge_context_event_entities (tenant_id, event_id, entity_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, event_id, evidence_id) REFERENCES public.knowledge_context_event_evidence (tenant_id, event_id, evidence_id) ON DELETE CASCADE
);
CREATE INDEX idx_kceee_evidence ON public.knowledge_context_event_evidence_entities (tenant_id, evidence_id, event_id, entity_id);
CREATE INDEX idx_kceee_object ON public.knowledge_context_event_evidence_entities (tenant_id, entity_id, evidence_id, event_id);



--
-- Name: idx_kcee_related_event; Type: INDEX; Schema: public
--

CREATE INDEX idx_kcee_related_event ON public.knowledge_context_event_evidence USING btree (tenant_id, knowledge_base_id, related_event_id);


--
-- Name: idx_kcee_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_kcee_scope ON public.knowledge_context_event_evidence USING btree (tenant_id, knowledge_base_id, event_id);


--
-- Name: idx_kcevent_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_kcevent_scope ON public.knowledge_context_events USING btree (tenant_id, knowledge_base_id, occurred_at DESC);


--
-- Name: idx_kcevid_hash; Type: INDEX; Schema: public
--

CREATE INDEX idx_kcevid_hash ON public.knowledge_context_evidence USING btree (tenant_id, knowledge_base_id, knowledge_id, body_hash);


--
-- Name: idx_kcevid_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_kcevid_scope ON public.knowledge_context_evidence USING btree (tenant_id, knowledge_base_id, status, created_at);


--
-- Name: idx_kcevid_version; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_kcevid_version ON public.knowledge_context_evidence USING btree (tenant_id, knowledge_base_id, knowledge_id, version);


--
-- Name: idx_kcf_evidence; Type: INDEX; Schema: public
--

CREATE INDEX idx_kcf_evidence ON public.knowledge_context_facts USING btree (tenant_id, knowledge_base_id, evidence_id);


--
-- Name: idx_kcf_object; Type: INDEX; Schema: public
--

CREATE INDEX idx_kcf_object ON public.knowledge_context_facts USING btree (tenant_id, knowledge_base_id, object_entity_id);


--
-- Name: idx_kcf_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_kcf_scope ON public.knowledge_context_facts USING btree (tenant_id, knowledge_base_id, subject_entity_id, predicate, status);


--
-- Name: idx_knowledge_anomalies_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledge_anomalies_scope ON public.knowledge_anomalies USING btree (tenant_id, knowledge_base_id, status, created_at);


--
-- Name: idx_knowledge_bases_storage_backend; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledge_bases_storage_backend ON public.knowledge_bases USING btree (tenant_id, storage_backend_id);


--
-- Name: idx_knowledge_bases_tenant_creator; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledge_bases_tenant_creator ON public.knowledge_bases USING btree (tenant_id, creator_id);


--
-- Name: idx_knowledge_bases_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledge_bases_tenant_id ON public.knowledge_bases USING btree (tenant_id);


--
-- Name: idx_knowledge_bases_tenant_vector_store; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledge_bases_tenant_vector_store ON public.knowledge_bases USING btree (tenant_id, vector_store_id);


--
-- Name: idx_knowledge_strategy_one_active; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_knowledge_strategy_one_active ON public.knowledge_strategy_versions USING btree (tenant_id, knowledge_base_id) WHERE ((status)::text = 'active'::text);


--
-- Name: idx_knowledge_strategy_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledge_strategy_scope ON public.knowledge_strategy_versions USING btree (tenant_id, knowledge_base_id, status, created_at);


--
-- Name: idx_knowledge_tags_kb; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledge_tags_kb ON public.knowledge_tags USING btree (tenant_id, knowledge_base_id);


--
-- Name: idx_knowledge_tags_kb_kind_name; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_knowledge_tags_kb_kind_name ON public.knowledge_tags USING btree (tenant_id, knowledge_base_id, kind, name);


--
-- Name: idx_knowledge_tags_seq_id; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_knowledge_tags_seq_id ON public.knowledge_tags USING btree (seq_id);


--
-- Name: idx_knowledges_base_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledges_base_id ON public.knowledges USING btree (knowledge_base_id);


--
-- Name: idx_knowledges_enable_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledges_enable_status ON public.knowledges USING btree (enable_status);


--
-- Name: idx_knowledges_folder_path; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledges_folder_path ON public.knowledges USING btree (tenant_id, knowledge_base_id, folder_path);


--
-- Name: idx_knowledges_kb_metadata_external_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledges_kb_metadata_external_id ON public.knowledges USING btree (knowledge_base_id, ((metadata ->> 'external_id'::text)) text_pattern_ops) WHERE (deleted_at IS NULL);


--
-- Name: idx_knowledges_parse_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledges_parse_status ON public.knowledges USING btree (parse_status);


--
-- Name: idx_knowledges_summary_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledges_summary_status ON public.knowledges USING btree (summary_status);


--
-- Name: idx_knowledges_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledges_tenant_id ON public.knowledges USING btree (tenant_id);


--
-- Name: idx_kpspan_knowledge_attempt; Type: INDEX; Schema: public
--

CREATE INDEX idx_kpspan_knowledge_attempt ON public.knowledge_processing_spans USING btree (knowledge_id, attempt);


--
-- Name: idx_kpspan_parent; Type: INDEX; Schema: public
--

CREATE INDEX idx_kpspan_parent ON public.knowledge_processing_spans USING btree (parent_span_id) WHERE (parent_span_id IS NOT NULL);


--
-- Name: idx_kpspan_status_started; Type: INDEX; Schema: public
--

CREATE INDEX idx_kpspan_status_started ON public.knowledge_processing_spans USING btree (status, started_at);


--
-- Name: idx_ktr_knowledge; Type: INDEX; Schema: public
--

CREATE INDEX idx_ktr_knowledge ON public.knowledge_tag_relations USING btree (knowledge_id);


--
-- Name: idx_ktr_tag; Type: INDEX; Schema: public
--

CREATE INDEX idx_ktr_tag ON public.knowledge_tag_relations USING btree (tag_id);


--
-- Name: idx_kworkflow_owner; Type: INDEX; Schema: public
--

CREATE INDEX idx_kworkflow_owner ON public.knowledge_workflows USING btree (owner_id);


--
-- Name: idx_kworkflow_schedule; Type: INDEX; Schema: public
--

CREATE INDEX idx_kworkflow_schedule ON public.knowledge_workflows USING btree (enabled, tenant_id);


--
-- Name: idx_kwreport_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_kwreport_scope ON public.knowledge_workflow_reports USING btree (tenant_id, knowledge_base_id, created_at DESC);


--
-- Name: idx_kwrun_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_kwrun_scope ON public.knowledge_workflow_runs USING btree (tenant_id, knowledge_base_id, workflow_id, created_at DESC);


--
-- Name: idx_kwrun_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_kwrun_status ON public.knowledge_workflow_runs USING btree (status, updated_at);


--
-- Name: idx_kwstep_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_kwstep_scope ON public.knowledge_workflow_step_runs USING btree (tenant_id, knowledge_base_id, run_id);


--
-- Name: idx_kwstep_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_kwstep_status ON public.knowledge_workflow_step_runs USING btree (status, updated_at);


--
-- Name: idx_mcp_oauth_clients_service_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_mcp_oauth_clients_service_id ON public.mcp_oauth_clients USING btree (service_id);


--
-- Name: idx_mcp_oauth_clients_tenant_svc; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_mcp_oauth_clients_tenant_svc ON public.mcp_oauth_clients USING btree (tenant_id, service_id);


--
-- Name: idx_mcp_oauth_tokens_principal; Type: INDEX; Schema: public
--

CREATE INDEX idx_mcp_oauth_tokens_principal ON public.mcp_oauth_tokens USING btree (principal_type, principal_id);


--
-- Name: idx_mcp_oauth_tokens_service_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_mcp_oauth_tokens_service_id ON public.mcp_oauth_tokens USING btree (service_id);


--
-- Name: idx_mcp_oauth_tokens_tenant_principal_svc; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_mcp_oauth_tokens_tenant_principal_svc ON public.mcp_oauth_tokens USING btree (tenant_id, principal_type, principal_id, service_id);


--
-- Name: idx_mcp_oauth_tokens_user_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_mcp_oauth_tokens_user_id ON public.mcp_oauth_tokens USING btree (user_id);


--
-- Name: idx_mcp_services_deleted_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_mcp_services_deleted_at ON public.mcp_services USING btree (deleted_at);


--
-- Name: idx_mcp_services_enabled; Type: INDEX; Schema: public
--

CREATE INDEX idx_mcp_services_enabled ON public.mcp_services USING btree (enabled);


--
-- Name: idx_mcp_services_is_builtin; Type: INDEX; Schema: public
--

CREATE INDEX idx_mcp_services_is_builtin ON public.mcp_services USING btree (is_builtin);


--
-- Name: idx_mcp_services_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_mcp_services_tenant_id ON public.mcp_services USING btree (tenant_id);


--
-- Name: idx_mcp_tool_approvals_service_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_mcp_tool_approvals_service_id ON public.mcp_tool_approvals USING btree (service_id);


--
-- Name: idx_mcp_tool_approvals_tenant_svc_tool; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_mcp_tool_approvals_tenant_svc_tool ON public.mcp_tool_approvals USING btree (tenant_id, service_id, tool_name);


--
-- Name: idx_mem_affinity_scope; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_mem_affinity_scope ON public.memory_doc_affinity USING btree (tenant_id, subject_id, knowledge_id);


--
-- Name: idx_mem_emb_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_mem_emb_scope ON public.memory_item_embeddings USING btree (tenant_id, subject_id);


--
-- Name: idx_mem_tomb_fp; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_mem_tomb_fp ON public.memory_tombstones USING btree (tenant_id, subject_id, fingerprint);


--
-- Name: idx_mem_topic_scope; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_mem_topic_scope ON public.memory_topic_stats USING btree (tenant_id, subject_id, normalized_key);


--
-- Name: idx_memory_items_key; Type: INDEX; Schema: public
--

CREATE INDEX idx_memory_items_key ON public.memory_items USING btree (tenant_id, subject_id, normalized_key);


--
-- Name: idx_memory_items_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_memory_items_scope ON public.memory_items USING btree (tenant_id, subject_id, status);


--
-- Name: idx_memory_subjects_scope; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_memory_subjects_scope ON public.memory_subjects USING btree (tenant_id, subject_id);


--
-- Name: idx_memory_tombstones_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_memory_tombstones_scope ON public.memory_tombstones USING btree (tenant_id, subject_id);


--
-- Name: idx_message_suggestion_events_session; Type: INDEX; Schema: public
--

CREATE INDEX idx_message_suggestion_events_session ON public.message_suggestion_events USING btree (tenant_id, session_id, created_at);


--
-- Name: idx_message_suggestion_events_set; Type: INDEX; Schema: public
--

CREATE INDEX idx_message_suggestion_events_set ON public.message_suggestion_events USING btree (suggestion_set_id, created_at);


--
-- Name: idx_message_suggestion_events_type; Type: INDEX; Schema: public
--

CREATE INDEX idx_message_suggestion_events_type ON public.message_suggestion_events USING btree (event_type, created_at);


--
-- Name: idx_message_suggestion_sets_cache_key; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_message_suggestion_sets_cache_key ON public.message_suggestion_sets USING btree (tenant_id, assistant_message_id, placement, config_hash, locale);


--
-- Name: idx_message_suggestion_sets_session; Type: INDEX; Schema: public
--

CREATE INDEX idx_message_suggestion_sets_session ON public.message_suggestion_sets USING btree (tenant_id, session_id, created_at DESC);


--
-- Name: idx_message_suggestion_sets_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_message_suggestion_sets_status ON public.message_suggestion_sets USING btree (status, lease_until);


--
-- Name: idx_messages_agent_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_messages_agent_id ON public.messages USING btree (agent_id);


--
-- Name: idx_messages_agent_steps; Type: INDEX; Schema: public
--

CREATE INDEX idx_messages_agent_steps ON public.messages USING gin (agent_steps);


--
-- Name: idx_messages_knowledge_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_messages_knowledge_id ON public.messages USING btree (knowledge_id);


--
-- Name: idx_messages_session_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_messages_session_id ON public.messages USING btree (session_id);


--
-- Name: idx_models_is_builtin; Type: INDEX; Schema: public
--

CREATE INDEX idx_models_is_builtin ON public.models USING btree (is_builtin);


--
-- Name: idx_models_managed_by_yaml; Type: INDEX; Schema: public
--

CREATE INDEX idx_models_managed_by_yaml ON public.models USING btree (managed_by) WHERE ((managed_by)::text <> ''::text);


--
-- Name: idx_models_source; Type: INDEX; Schema: public
--

CREATE INDEX idx_models_source ON public.models USING btree (source);


--
-- Name: idx_models_type; Type: INDEX; Schema: public
--

CREATE INDEX idx_models_type ON public.models USING btree (type);


--
-- Name: idx_resource_access_grants_expires; Type: INDEX; Schema: public
--

CREATE INDEX idx_resource_access_grants_expires ON public.resource_access_grants USING btree (expires_at);


--
-- Name: idx_resource_access_grants_resource; Type: INDEX; Schema: public
--

CREATE INDEX idx_resource_access_grants_resource ON public.resource_access_grants USING btree (resource_id);


--
-- Name: idx_resource_bindings_owner; Type: INDEX; Schema: public
--

CREATE INDEX idx_resource_bindings_owner ON public.resource_bindings USING btree (tenant_id, owner_type, owner_id);


--
-- Name: idx_resource_bindings_unique; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_resource_bindings_unique ON public.resource_bindings USING btree (resource_id, owner_type, owner_id, relation);


--
-- Name: idx_resources_backend; Type: INDEX; Schema: public
--

CREATE INDEX idx_resources_backend ON public.resources USING btree (storage_backend_id);


--
-- Name: idx_resources_tenant; Type: INDEX; Schema: public
--

CREATE INDEX idx_resources_tenant ON public.resources USING btree (tenant_id);


--
-- Name: idx_resources_tenant_location; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_resources_tenant_location ON public.resources USING btree (tenant_id, location_hash) WHERE (deleted_at IS NULL);


--
-- Name: idx_sessions_agent_config; Type: INDEX; Schema: public
--

CREATE INDEX idx_sessions_agent_config ON public.sessions USING gin (agent_config);


--
-- Name: idx_sessions_agent_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_sessions_agent_id ON public.sessions USING btree (agent_id);


--
-- Name: idx_sessions_context_config; Type: INDEX; Schema: public
--

CREATE INDEX idx_sessions_context_config ON public.sessions USING gin (context_config);


--
-- Name: idx_sessions_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_sessions_tenant_id ON public.sessions USING btree (tenant_id);


--
-- Name: idx_sessions_tenant_user_pin; Type: INDEX; Schema: public
--

CREATE INDEX idx_sessions_tenant_user_pin ON public.sessions USING btree (tenant_id, user_id, is_pinned DESC, pinned_at DESC, updated_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_storage_backends_legacy_alias; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_storage_backends_legacy_alias ON public.storage_backends USING btree (tenant_id, provider) WHERE ((deleted_at IS NULL) AND (legacy_alias = true));


--
-- Name: idx_storage_backends_name_tenant; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_storage_backends_name_tenant ON public.storage_backends USING btree (tenant_id, name) WHERE (deleted_at IS NULL);


--
-- Name: idx_storage_backends_tenant; Type: INDEX; Schema: public
--

CREATE INDEX idx_storage_backends_tenant ON public.storage_backends USING btree (tenant_id);


--
-- Name: idx_sync_logs_data_source_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_sync_logs_data_source_id ON public.sync_logs USING btree (data_source_id);


--
-- Name: idx_sync_logs_started_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_sync_logs_started_at ON public.sync_logs USING btree (started_at);


--
-- Name: idx_sync_logs_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_sync_logs_status ON public.sync_logs USING btree (status);


--
-- Name: idx_sync_logs_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_sync_logs_tenant_id ON public.sync_logs USING btree (tenant_id);


--
-- Name: idx_system_settings_category; Type: INDEX; Schema: public
--

CREATE INDEX idx_system_settings_category ON public.system_settings USING btree (category);


--
-- Name: idx_tag_manual_decisions_tag; Type: INDEX; Schema: public
--

CREATE INDEX idx_tag_manual_decisions_tag ON public.knowledge_tag_manual_decisions USING btree (tag_id);


--
-- Name: idx_task_dead_letters_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_task_dead_letters_scope ON public.task_dead_letters USING btree (scope, scope_id, failed_at DESC);


--
-- Name: idx_task_dead_letters_task_type; Type: INDEX; Schema: public
--

CREATE INDEX idx_task_dead_letters_task_type ON public.task_dead_letters USING btree (task_type, failed_at DESC);


--
-- Name: idx_task_dead_letters_tenant; Type: INDEX; Schema: public
--

CREATE INDEX idx_task_dead_letters_tenant ON public.task_dead_letters USING btree (tenant_id, failed_at DESC);


--
-- Name: idx_task_pending_ops_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_task_pending_ops_scope ON public.task_pending_ops USING btree (task_type, scope, scope_id, id);


--
-- Name: idx_task_pending_ops_tenant; Type: INDEX; Schema: public
--

CREATE INDEX idx_task_pending_ops_tenant ON public.task_pending_ops USING btree (tenant_id);


--
-- Name: idx_temporary_documents_expires; Type: INDEX; Schema: public
--

CREATE INDEX idx_temporary_documents_expires ON public.temporary_documents USING btree (expires_at);


--
-- Name: idx_temporary_documents_scope; Type: INDEX; Schema: public
--

CREATE INDEX idx_temporary_documents_scope ON public.temporary_documents USING btree (tenant_id, session_id);


--
-- Name: idx_temporary_documents_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_temporary_documents_status ON public.temporary_documents USING btree (status);


--
-- Name: idx_tenant_api_keys_revoked_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenant_api_keys_revoked_at ON public.tenant_api_keys USING btree (revoked_at);


--
-- Name: idx_tenant_api_keys_tenant; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenant_api_keys_tenant ON public.tenant_api_keys USING btree (tenant_id);


--
-- Name: idx_tenant_invitations_invitee; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenant_invitations_invitee ON public.tenant_invitations USING btree (invitee_user_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_tenant_invitations_tenant; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenant_invitations_tenant ON public.tenant_invitations USING btree (tenant_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_tenant_invitations_token; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_tenant_invitations_token ON public.tenant_invitations USING btree (token) WHERE (((token)::text <> ''::text) AND (deleted_at IS NULL));


--
-- Name: idx_tenant_invitations_unique_pending; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_tenant_invitations_unique_pending ON public.tenant_invitations USING btree (tenant_id, invitee_user_id) WHERE (((status)::text = 'pending'::text) AND (deleted_at IS NULL) AND ((invitee_user_id)::text <> ''::text));


--
-- Name: idx_tenant_members_tenant_role; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenant_members_tenant_role ON public.tenant_members USING btree (tenant_id, role) WHERE (deleted_at IS NULL);


--
-- Name: idx_tenant_members_user; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenant_members_user ON public.tenant_members USING btree (user_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_tenant_members_user_tenant_unique; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_tenant_members_user_tenant_unique ON public.tenant_members USING btree (user_id, tenant_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_tenant_sandbox_configs_tenant; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenant_sandbox_configs_tenant ON public.tenant_sandbox_configs USING btree (tenant_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_tenant_skill_snapshots_config; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenant_skill_snapshots_config ON public.tenant_skill_snapshots USING btree (sandbox_config_id);


--
-- Name: idx_tenant_skill_snapshots_state; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenant_skill_snapshots_state ON public.tenant_skill_snapshots USING btree (state);


--
-- Name: idx_tenant_skills_catalog; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenant_skills_catalog ON public.tenant_skills USING btree (catalog_id);


--
-- Name: idx_tenants_agent_config; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenants_agent_config ON public.tenants USING gin (agent_config);


--
-- Name: idx_tenants_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenants_status ON public.tenants USING btree (status);


--
-- Name: idx_user_env_var_config; Type: INDEX; Schema: public
--

CREATE INDEX idx_user_env_var_config ON public.tenant_user_env_vars USING btree (tenant_id, sandbox_config_id);


--
-- Name: idx_user_env_var_skill; Type: INDEX; Schema: public
--

CREATE INDEX idx_user_env_var_skill ON public.tenant_user_env_vars USING btree (tenant_id, skill_id);


--
-- Name: idx_user_kb_pins_user_tenant_pinned_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_user_kb_pins_user_tenant_pinned_at ON public.user_kb_pins USING btree (tenant_id, user_id, pinned_at DESC);


--
-- Name: idx_user_resource_favorites_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_user_resource_favorites_tenant_id ON public.user_resource_favorites USING btree (tenant_id);


--
-- Name: idx_user_resource_favorites_user_tenant_type_created_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_user_resource_favorites_user_tenant_type_created_at ON public.user_resource_favorites USING btree (user_id, tenant_id, resource_type, created_at DESC);


--
-- Name: idx_users_deleted_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_users_deleted_at ON public.users USING btree (deleted_at);


--
-- Name: idx_users_email; Type: INDEX; Schema: public
--

CREATE INDEX idx_users_email ON public.users USING btree (email);


--
-- Name: idx_users_is_system_admin; Type: INDEX; Schema: public
--

CREATE INDEX idx_users_is_system_admin ON public.users USING btree (is_system_admin);


--
-- Name: idx_users_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_users_tenant_id ON public.users USING btree (tenant_id);


--
-- Name: idx_users_username; Type: INDEX; Schema: public
--

CREATE INDEX idx_users_username ON public.users USING btree (username);


--
-- Name: idx_vector_stores_deleted_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_vector_stores_deleted_at ON public.vector_stores USING btree (deleted_at);


--
-- Name: idx_vector_stores_engine_type; Type: INDEX; Schema: public
--

CREATE INDEX idx_vector_stores_engine_type ON public.vector_stores USING btree (engine_type);


--
-- Name: idx_vector_stores_name_tenant; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_vector_stores_name_tenant ON public.vector_stores USING btree (name, tenant_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_vector_stores_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_vector_stores_tenant_id ON public.vector_stores USING btree (tenant_id);


--
-- Name: idx_web_search_providers_deleted_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_web_search_providers_deleted_at ON public.web_search_providers USING btree (deleted_at);


--
-- Name: idx_web_search_providers_provider; Type: INDEX; Schema: public
--

CREATE INDEX idx_web_search_providers_provider ON public.web_search_providers USING btree (provider);


--
-- Name: idx_web_search_providers_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_web_search_providers_tenant_id ON public.web_search_providers USING btree (tenant_id);


--
-- Name: idx_wiki_folders_deleted_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_folders_deleted_at ON public.wiki_folders USING btree (deleted_at);


--
-- Name: idx_wiki_folders_parent; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_folders_parent ON public.wiki_folders USING btree (knowledge_base_id, parent_id);


--
-- Name: idx_wiki_folders_parent_name; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_wiki_folders_parent_name ON public.wiki_folders USING btree (knowledge_base_id, parent_id, name) WHERE (deleted_at IS NULL);


--
-- Name: idx_wiki_page_issues_knowledge_base_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_page_issues_knowledge_base_id ON public.wiki_page_issues USING btree (knowledge_base_id);


--
-- Name: idx_wiki_page_issues_slug; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_page_issues_slug ON public.wiki_page_issues USING btree (slug);


--
-- Name: idx_wiki_page_issues_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_page_issues_status ON public.wiki_page_issues USING btree (status);


--
-- Name: idx_wiki_page_issues_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_page_issues_tenant_id ON public.wiki_page_issues USING btree (tenant_id);


--
-- Name: idx_wiki_page_revisions_kb_slug; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_page_revisions_kb_slug ON public.wiki_page_revisions USING btree (knowledge_base_id, slug);


--
-- Name: idx_wiki_page_revisions_page_version; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_wiki_page_revisions_page_version ON public.wiki_page_revisions USING btree (page_id, version);


--
-- Name: idx_wiki_pages_deleted_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_pages_deleted_at ON public.wiki_pages USING btree (deleted_at);


--
-- Name: idx_wiki_pages_folder; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_pages_folder ON public.wiki_pages USING btree (knowledge_base_id, folder_id);


--
-- Name: idx_wiki_pages_folder_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_pages_folder_id ON public.wiki_pages USING btree (folder_id);


--
-- Name: idx_wiki_pages_fulltext; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_pages_fulltext ON public.wiki_pages USING gin (to_tsvector('simple'::regconfig, (((COALESCE(title, ''::character varying))::text || ' '::text) || COALESCE(content, ''::text))));


--
-- Name: idx_wiki_pages_kb_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_pages_kb_id ON public.wiki_pages USING btree (knowledge_base_id);


--
-- Name: idx_wiki_pages_kb_slug; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_wiki_pages_kb_slug ON public.wiki_pages USING btree (knowledge_base_id, slug) WHERE (deleted_at IS NULL);


--
-- Name: idx_wiki_pages_page_type; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_pages_page_type ON public.wiki_pages USING btree (knowledge_base_id, page_type);


--
-- Name: idx_wiki_pages_parent_slug; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_pages_parent_slug ON public.wiki_pages USING btree (knowledge_base_id, parent_slug);


--
-- Name: idx_wiki_pages_source_refs; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_pages_source_refs ON public.wiki_pages USING gin (source_refs jsonb_path_ops);

CREATE INDEX idx_wiki_pages_chunk_refs ON public.wiki_pages USING gin (chunk_refs jsonb_path_ops);


--
-- Name: idx_wiki_pages_source_refs_text; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_pages_source_refs_text ON public.wiki_pages USING gin (to_tsvector('simple'::regconfig, (source_refs)::text));


--
-- Name: idx_wiki_pages_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_pages_tenant_id ON public.wiki_pages USING btree (tenant_id);


--
-- Name: idx_wiki_pages_title_trgm; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_pages_title_trgm ON public.wiki_pages USING gin (lower((title)::text) public.gin_trgm_ops);


--
-- Name: idx_wiki_pages_tree; Type: INDEX; Schema: public
--

CREATE INDEX idx_wiki_pages_tree ON public.wiki_pages USING btree (knowledge_base_id, page_type, wiki_path, sort_order, title);


--
-- Name: uq_tenant_sandbox_configs_tenant_name; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX uq_tenant_sandbox_configs_tenant_name ON public.tenant_sandbox_configs USING btree (tenant_id, name) WHERE (deleted_at IS NULL);


--
-- Name: uq_tenant_skill_catalog_name; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX uq_tenant_skill_catalog_name ON public.tenant_skill_catalog USING btree (tenant_id, name) WHERE (deleted_at IS NULL);


--
-- Name: uq_tenant_skills_config_name; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX uq_tenant_skills_config_name ON public.tenant_skills USING btree (sandbox_config_id, name) WHERE (deleted_at IS NULL);


--
-- Name: uq_user_env_var; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX uq_user_env_var ON public.tenant_user_env_vars USING btree (tenant_id, principal_type, principal_id, sandbox_config_id, skill_id, name);


--
-- Name: mcp_services trigger_mcp_services_updated_at; Type: TRIGGER; Schema: public
--

CREATE TRIGGER trigger_mcp_services_updated_at BEFORE UPDATE ON public.mcp_services FOR EACH ROW EXECUTE FUNCTION public.update_mcp_services_updated_at();

CREATE FUNCTION public.set_chunk_search_title() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    NEW.search_title := COALESCE((
        SELECT btrim(title) FROM public.knowledges
        WHERE id = NEW.knowledge_id AND tenant_id = NEW.tenant_id
    ), '');
    RETURN NEW;
END;
$$;

CREATE FUNCTION public.sync_knowledge_search_title() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    UPDATE public.chunks SET search_title = COALESCE(btrim(NEW.title), '')
    WHERE knowledge_id = NEW.id AND tenant_id = NEW.tenant_id
      AND search_title IS DISTINCT FROM COALESCE(btrim(NEW.title), '');
    RETURN NEW;
END;
$$;

CREATE TRIGGER set_chunk_search_title
    BEFORE INSERT OR UPDATE OF knowledge_id, tenant_id ON public.chunks
    FOR EACH ROW EXECUTE FUNCTION public.set_chunk_search_title();

CREATE TRIGGER sync_knowledge_search_title
    AFTER INSERT OR UPDATE OF title ON public.knowledges
    FOR EACH ROW EXECUTE FUNCTION public.sync_knowledge_search_title();


--
-- Name: auth_tokens fk_auth_tokens_user; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.auth_tokens
    ADD CONSTRAINT fk_auth_tokens_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: users fk_users_tenant; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT fk_users_tenant FOREIGN KEY (tenant_id) REFERENCES public.tenants(id);


--
-- Name: im_channel_sessions im_channel_sessions_session_id_fkey; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.im_channel_sessions
    ADD CONSTRAINT im_channel_sessions_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.sessions(id) ON DELETE CASCADE;


--
-- Name: mcp_oauth_clients mcp_oauth_clients_service_id_fkey; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.mcp_oauth_clients
    ADD CONSTRAINT mcp_oauth_clients_service_id_fkey FOREIGN KEY (service_id) REFERENCES public.mcp_services(id) ON DELETE CASCADE;


--
-- Name: mcp_oauth_tokens mcp_oauth_tokens_service_id_fkey; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.mcp_oauth_tokens
    ADD CONSTRAINT mcp_oauth_tokens_service_id_fkey FOREIGN KEY (service_id) REFERENCES public.mcp_services(id) ON DELETE CASCADE;


--
-- Name: mcp_tool_approvals mcp_tool_approvals_service_id_fkey; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.mcp_tool_approvals
    ADD CONSTRAINT mcp_tool_approvals_service_id_fkey FOREIGN KEY (service_id) REFERENCES public.mcp_services(id) ON DELETE CASCADE;


--
-- Name: message_suggestion_events message_suggestion_events_suggestion_set_id_fkey; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.message_suggestion_events
    ADD CONSTRAINT message_suggestion_events_suggestion_set_id_fkey FOREIGN KEY (suggestion_set_id) REFERENCES public.message_suggestion_sets(id) ON DELETE CASCADE;


--
-- Name: message_suggestion_events message_suggestion_events_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.message_suggestion_events
    ADD CONSTRAINT message_suggestion_events_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;


--
-- Name: message_suggestion_sets message_suggestion_sets_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.message_suggestion_sets
    ADD CONSTRAINT message_suggestion_sets_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;


--
-- Name: resource_access_grants resource_access_grants_resource_id_fkey; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.resource_access_grants
    ADD CONSTRAINT resource_access_grants_resource_id_fkey FOREIGN KEY (resource_id) REFERENCES public.resources(id) ON DELETE CASCADE;


--
-- Name: resource_bindings resource_bindings_resource_id_fkey; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.resource_bindings
    ADD CONSTRAINT resource_bindings_resource_id_fkey FOREIGN KEY (resource_id) REFERENCES public.resources(id) ON DELETE CASCADE;


--
-- Name: sync_logs sync_logs_data_source_id_fkey; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.sync_logs
    ADD CONSTRAINT sync_logs_data_source_id_fkey FOREIGN KEY (data_source_id) REFERENCES public.data_sources(id) ON DELETE CASCADE;


--
-- Name: tenant_api_keys tenant_api_keys_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.tenant_api_keys
    ADD CONSTRAINT tenant_api_keys_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- No seed rows: every application table is empty after the original up migrations.
-- Preserve sequence positions, including the first tenant ID of 10000.
SELECT pg_catalog.setval('public.audit_logs_id_seq', 1, false);
SELECT pg_catalog.setval('public.chunks_seq_id_seq', 100000000, false);
SELECT pg_catalog.setval('public.embeddings_id_seq', 1, false);
SELECT pg_catalog.setval('public.knowledge_processing_spans_id_seq', 1, false);
SELECT pg_catalog.setval('public.knowledge_tags_seq_id_seq', 10000000, false);
SELECT pg_catalog.setval('public.message_suggestion_events_id_seq', 1, false);
SELECT pg_catalog.setval('public.system_settings_id_seq', 1, false);
SELECT pg_catalog.setval('public.task_dead_letters_id_seq', 1, false);
SELECT pg_catalog.setval('public.task_pending_ops_id_seq', 1, false);
SELECT pg_catalog.setval('public.tenant_api_keys_id_seq', 1, false);
SELECT pg_catalog.setval('public.tenant_invitations_id_seq', 1, false);
SELECT pg_catalog.setval('public.tenant_members_id_seq', 1, false);
SELECT pg_catalog.setval('public.tenants_id_seq', 10000, false);

COMMIT;
