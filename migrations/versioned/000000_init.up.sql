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
    status integer DEFAULT 0 NOT NULL,
    content_hash character varying(64),
    flags integer DEFAULT 1 NOT NULL,
    seq_id bigint DEFAULT nextval('public.chunks_seq_id_seq'::regclass) NOT NULL,
    video_info text,
    context_header text DEFAULT ''::text NOT NULL,
    search_title text DEFAULT ''::text NOT NULL
);


--
-- Name: COLUMN chunks.video_info; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.chunks.video_info IS 'Video information in JSON format: {"url": string, "frame_count": int, "has_vlm_analysis": bool, "video_summary": string, "frame_descriptions": string[]}';


--
-- Name: data_sources; Type: TABLE; Schema: public
--

CREATE TABLE public.data_sources (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
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
    is_enabled boolean DEFAULT true
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
-- Name: knowledge_bases; Type: TABLE; Schema: public
--


CREATE TABLE public.knowledge_bases (
    model_tiers jsonb DEFAULT '{}'::jsonb NOT NULL,
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    name character varying(255) NOT NULL,
    description text,
    tenant_id integer NOT NULL,
    chunking_config jsonb DEFAULT '{"chunk_size": 512, "chunk_overlap": 50, "split_markers": ["\n\n", "\n", "。"], "keep_separator": true}'::jsonb NOT NULL,
    embedding_model_id character varying(64) NOT NULL,
    summary_model_id character varying(64) NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    type character varying(32) DEFAULT 'document'::character varying NOT NULL CHECK (type = 'document'),
    indexing_strategy jsonb,
    creator_id character varying(36)
);


--
-- Name: COLUMN knowledge_bases.indexing_strategy; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.knowledge_bases.indexing_strategy IS 'Indexing pipelines strategy: {"vector_enabled": bool, "keyword_enabled": bool}';


--
-- Name: entities; Type: TABLE; Schema: public
--

CREATE TABLE public.entities (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    canonical_name character varying(512) NOT NULL,
    normalized_name character varying(512) NOT NULL,
    entity_type character varying(128) DEFAULT 'other' NOT NULL CHECK (entity_type IN ('company', 'person', 'product', 'project', 'organization', 'other')),
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
-- Name: entity_aliases; Type: TABLE; Schema: public
--

CREATE TABLE public.entity_aliases (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    entity_type character varying(128) DEFAULT ''::character varying NOT NULL,
    entity_id character varying(36) NOT NULL,
    alias character varying(512) NOT NULL,
    normalized_alias character varying(512) NOT NULL,
    resolution_method character varying(16) DEFAULT 'exact' NOT NULL,
    created_at timestamp with time zone NOT NULL
);

CREATE TABLE public.entity_merge_suggestions (
    id character varying(36) PRIMARY KEY,
    tenant_id bigint NOT NULL,
    new_entity_id character varying(36) NOT NULL,
    candidate_entity_id character varying(36) NOT NULL,
    knowledge_id character varying(36) NOT NULL,
    reason text NOT NULL,
    status character varying(16) DEFAULT 'pending' NOT NULL CHECK (status IN ('pending', 'accepted', 'rejected')),
    created_at timestamp with time zone NOT NULL,
    decided_at timestamp with time zone,
    CONSTRAINT uq_entity_merge_suggestions_pair UNIQUE (tenant_id, new_entity_id, candidate_entity_id, knowledge_id)
);
CREATE INDEX idx_entity_merge_suggestions_status ON public.entity_merge_suggestions (tenant_id, status);
CREATE INDEX idx_entities_name_trgm ON public.entities USING gin (normalized_name public.gin_trgm_ops);
CREATE INDEX idx_entity_aliases_alias_trgm ON public.entity_aliases USING gin (normalized_alias public.gin_trgm_ops);
CREATE INDEX idx_entities_embedding_profile ON public.entities (tenant_id, embedding_model_id, embedding_dimension) WHERE name_embedding IS NOT NULL;
CREATE INDEX idx_entities_embedding_pending ON public.entities (tenant_id, id) WHERE embedding_needs_rebuild AND merged_into_id = '';

CREATE TABLE public.entity_merge_records (
    id character varying(36) PRIMARY KEY,
    tenant_id bigint NOT NULL,
    keep_entity_id character varying(36) NOT NULL,
    merge_entity_id character varying(36) NOT NULL,
    actor character varying(255) DEFAULT '' NOT NULL,
    created_at timestamp with time zone NOT NULL
);
CREATE INDEX idx_entity_merge_records_keep ON public.entity_merge_records (tenant_id, keep_entity_id);
CREATE INDEX idx_entity_merge_records_merged ON public.entity_merge_records (tenant_id, merge_entity_id);

CREATE TABLE public.entity_embedding_rebuild_runs (
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
CREATE INDEX idx_entity_embedding_run_latest ON public.entity_embedding_rebuild_runs (tenant_id, created_at DESC, id DESC);


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
-- Name: knowledges; Type: TABLE; Schema: public
--

CREATE TABLE public.knowledges (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    search_seq_id bigserial NOT NULL,
    tenant_id integer NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    type character varying(50) NOT NULL,
    title character varying(255) NOT NULL,
    description text,
    content_type character varying(16) CHECK (content_type IS NULL OR content_type IN ('', 'event', 'opinion', 'research', 'tutorial', 'case', 'reference')),
    processing_mode character varying(16) DEFAULT 'full' NOT NULL CHECK (processing_mode IN ('full', 'summary_only')),
	 source_kind character varying(16) DEFAULT 'article' NOT NULL CHECK (source_kind IN ('article', 'mine', 'report')),
	 vault_path text DEFAULT '' NOT NULL,
	 vault_body text DEFAULT '' NOT NULL,
	 vault_original_path text DEFAULT '' NOT NULL,
	 starred boolean DEFAULT false NOT NULL,
	 collect_reason text DEFAULT '' NOT NULL,
    structured_summary jsonb,
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
    custom_metadata jsonb DEFAULT '{}'::jsonb NOT NULL
);


--
-- Name: COLUMN knowledges.channel; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.knowledges.channel IS 'Source channel of the knowledge: web, api, rss';

CREATE UNIQUE INDEX knowledges_vault_path_unique ON public.knowledges (tenant_id, vault_path)
WHERE vault_path <> '' AND deleted_at IS NULL;


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

COMMENT ON TABLE public.task_dead_letters IS 'Permanent archive of tasks that exhausted retries. Written by the asynq dead-letter middleware and service-level retry handlers.';


--
-- Name: COLUMN task_dead_letters.related_id; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.task_dead_letters.related_id IS 'Optional secondary identifier for a related source or resource.';


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
-- Name: task_pending_ops_id_seq; Type: SEQUENCE; Schema: public
--

CREATE SEQUENCE public.task_pending_ops_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: tenant_api_keys; Type: TABLE; Schema: public
--

CREATE TABLE public.tenant_api_keys (
    id bigint NOT NULL,
    tenant_id integer NOT NULL DEFAULT 1 CHECK (tenant_id = 1),
    name character varying(128) NOT NULL,
    key_hash character varying(64) NOT NULL,
    api_key text DEFAULT ''::text NOT NULL,
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
-- Name: tenants; Type: TABLE; Schema: public
--

CREATE TABLE public.tenants (
    id integer NOT NULL CHECK (id = 1),
    name character varying(255) NOT NULL,
    description text,
    status character varying(50) DEFAULT 'active'::character varying,
    business character varying(255) NOT NULL,
    storage_used bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    parser_engine_config jsonb,
    retrieval_config jsonb,
    credentials jsonb
);


--
-- Name: COLUMN tenants.parser_engine_config; Type: COMMENT; Schema: public
--

COMMENT ON COLUMN public.tenants.parser_engine_config IS 'Parser engine overrides (mineru_endpoint, mineru_api_key, etc.); takes precedence over env when parsing';


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
    deleted_at timestamp with time zone
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
-- Name: embeddings id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.embeddings ALTER COLUMN id SET DEFAULT nextval('public.embeddings_id_seq'::regclass);


--
-- Name: knowledge_processing_spans id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.knowledge_processing_spans ALTER COLUMN id SET DEFAULT nextval('public.knowledge_processing_spans_id_seq'::regclass);


--
-- Name: system_settings id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.system_settings ALTER COLUMN id SET DEFAULT nextval('public.system_settings_id_seq'::regclass);


--
-- Name: task_dead_letters id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.task_dead_letters ALTER COLUMN id SET DEFAULT nextval('public.task_dead_letters_id_seq'::regclass);


--
-- Name: tenant_api_keys id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.tenant_api_keys ALTER COLUMN id SET DEFAULT nextval('public.tenant_api_keys_id_seq'::regclass);


--
-- Name: tenants id; Type: DEFAULT; Schema: public
--

ALTER TABLE ONLY public.tenants ALTER COLUMN id SET DEFAULT nextval('public.tenants_id_seq'::regclass);


--
-- Name: auth_tokens auth_tokens_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.auth_tokens
    ADD CONSTRAINT auth_tokens_pkey PRIMARY KEY (id);


--
-- Name: chunks chunks_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.chunks
    ADD CONSTRAINT chunks_pkey PRIMARY KEY (id);


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
-- Name: knowledge_bases knowledge_bases_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_bases
    ADD CONSTRAINT knowledge_bases_pkey PRIMARY KEY (id);


--
-- Name: entities entities_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.entities
    ADD CONSTRAINT entities_pkey PRIMARY KEY (id);


--
-- Name: entity_aliases entity_aliases_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.entity_aliases
    ADD CONSTRAINT entity_aliases_pkey PRIMARY KEY (id);


--
-- Name: knowledge_processing_spans knowledge_processing_spans_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledge_processing_spans
    ADD CONSTRAINT knowledge_processing_spans_pkey PRIMARY KEY (id);


--
-- Name: knowledges knowledges_pkey; Type: CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.knowledges
    ADD CONSTRAINT knowledges_pkey PRIMARY KEY (id);


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

-- Search the article body and its short retrieval aliases.
CREATE INDEX chunks_search_idx ON public.chunks USING bm25
    (seq_id, knowledge_base_id, knowledge_id, is_enabled, chunk_type,
     deleted_at, content, context_header, search_title, metadata)
    WITH (key_field=seq_id, text_fields='{
        "content": {"tokenizer": {"type": "chinese_lindera"}},
        "context_header": {"tokenizer": {"type": "chinese_lindera"}},
        "search_title": {"tokenizer": {"type": "chinese_lindera"}}
    }', json_fields='{
        "metadata": {"tokenizer": {"type": "chinese_lindera"}}
    }');

-- Article summaries remain searchable without a stored body chunk or vector.
CREATE UNIQUE INDEX idx_knowledges_search_seq_id ON public.knowledges (search_seq_id);
CREATE INDEX knowledges_summary_search_idx ON public.knowledges USING bm25
    (search_seq_id, tenant_id, knowledge_base_id, id, deleted_at, parse_status, enable_status, title, description)
    WITH (key_field=search_seq_id, text_fields='{
        "title": {"tokenizer": {"type": "chinese_lindera"}},
        "description": {"tokenizer": {"type": "chinese_lindera"}}
    }');


--
-- Name: idx_chunks_tenant_kg; Type: INDEX; Schema: public
--

CREATE INDEX idx_chunks_tenant_kg ON public.chunks USING btree (tenant_id, knowledge_id);


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
-- Name: idx_entities_canonical; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_entities_canonical ON public.entities USING btree (tenant_id, entity_type, normalized_name);


--
-- Name: idx_entities_scope_name; Type: INDEX; Schema: public
--

CREATE INDEX idx_entities_scope_name ON public.entities USING btree (tenant_id, entity_type, canonical_name);


--
-- Name: idx_entity_aliases_alias; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_entity_aliases_alias ON public.entity_aliases USING btree (tenant_id, entity_type, normalized_alias);


--
-- Name: idx_knowledge_bases_tenant_creator; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledge_bases_tenant_creator ON public.knowledge_bases USING btree (tenant_id, creator_id);


--
-- Name: idx_knowledge_bases_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledge_bases_tenant_id ON public.knowledge_bases USING btree (tenant_id);


--
-- Name: idx_knowledges_base_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledges_base_id ON public.knowledges USING btree (knowledge_base_id);


--
-- Name: idx_knowledges_enable_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_knowledges_enable_status ON public.knowledges USING btree (enable_status);




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
-- Name: idx_resources_tenant; Type: INDEX; Schema: public
--

CREATE INDEX idx_resources_tenant ON public.resources USING btree (tenant_id);


--
-- Name: idx_resources_tenant_location; Type: INDEX; Schema: public
--

CREATE UNIQUE INDEX idx_resources_tenant_location ON public.resources USING btree (tenant_id, location_hash) WHERE (deleted_at IS NULL);


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
-- Name: idx_tenant_api_keys_revoked_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenant_api_keys_revoked_at ON public.tenant_api_keys USING btree (revoked_at);


--
-- Name: idx_tenant_api_keys_tenant; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenant_api_keys_tenant ON public.tenant_api_keys USING btree (tenant_id);


--
-- Name: idx_tenants_status; Type: INDEX; Schema: public
--

CREATE INDEX idx_tenants_status ON public.tenants USING btree (status);






--
-- Name: idx_users_deleted_at; Type: INDEX; Schema: public
--

CREATE INDEX idx_users_deleted_at ON public.users USING btree (deleted_at);


--
-- Name: idx_users_email; Type: INDEX; Schema: public
--

CREATE INDEX idx_users_email ON public.users USING btree (email);


--
-- Name: idx_users_tenant_id; Type: INDEX; Schema: public
--

CREATE INDEX idx_users_tenant_id ON public.users USING btree (tenant_id);


--
-- Name: idx_users_username; Type: INDEX; Schema: public
--

CREATE INDEX idx_users_username ON public.users USING btree (username);


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
-- Name: tenant_api_keys tenant_api_keys_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public
--

ALTER TABLE ONLY public.tenant_api_keys
    ADD CONSTRAINT tenant_api_keys_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- No seed rows: every application table is empty after the original up migrations.
-- Preserve sequence positions, including the first tenant ID of 10000.
SELECT pg_catalog.setval('public.chunks_seq_id_seq', 100000000, false);
SELECT pg_catalog.setval('public.embeddings_id_seq', 1, false);
SELECT pg_catalog.setval('public.knowledge_processing_spans_id_seq', 1, false);
SELECT pg_catalog.setval('public.system_settings_id_seq', 1, false);
SELECT pg_catalog.setval('public.task_dead_letters_id_seq', 1, false);
SELECT pg_catalog.setval('public.task_pending_ops_id_seq', 1, false);
SELECT pg_catalog.setval('public.tenant_api_keys_id_seq', 1, false);
SELECT pg_catalog.setval('public.tenants_id_seq', 10000, false);


CREATE INDEX idx_entity_aliases_entity ON public.entity_aliases USING btree (tenant_id, entity_id);


-- Entities retain one identity within the personal space.
ALTER TABLE public.entities ADD CONSTRAINT uq_entities_tenant_id UNIQUE (tenant_id, id);
ALTER TABLE public.knowledges ADD CONSTRAINT uq_knowledges_tenant_id UNIQUE (tenant_id, id);
CREATE TABLE public.knowledge_entities (
    tenant_id bigint NOT NULL,
    knowledge_id character varying(36) NOT NULL,
    entity_id character varying(36) NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT uq_knowledge_entities_pair UNIQUE (knowledge_id, entity_id),
    FOREIGN KEY (tenant_id, knowledge_id) REFERENCES public.knowledges (tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, entity_id) REFERENCES public.entities (tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_knowledge_entities_entity ON public.knowledge_entities (tenant_id, entity_id, knowledge_id);

CREATE TABLE public.topics (
    id character varying(36) NOT NULL PRIMARY KEY,
    tenant_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    description text DEFAULT '' NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT uq_topics_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT uq_topics_tenant_name UNIQUE (tenant_id, name)
);
CREATE TABLE public.knowledge_topics (
    tenant_id bigint NOT NULL,
    knowledge_id character varying(36) NOT NULL,
    topic_id character varying(36) NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT uq_knowledge_topics_pair UNIQUE (knowledge_id, topic_id),
    FOREIGN KEY (tenant_id, knowledge_id) REFERENCES public.knowledges (tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, topic_id) REFERENCES public.topics (tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_knowledge_topics_topic ON public.knowledge_topics (tenant_id, topic_id, knowledge_id);


CREATE UNIQUE INDEX idx_knowledge_bases_active_tenant ON public.knowledge_bases (tenant_id) WHERE deleted_at IS NULL;

COMMIT;
