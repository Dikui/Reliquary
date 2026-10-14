CREATE TABLE model_calls (
 id uuid PRIMARY KEY,
 space_id bigint NOT NULL,
 requested_at timestamptz NOT NULL,
 model_id text NOT NULL,
 model_name text NOT NULL,
 provider text NOT NULL,
 purpose text NOT NULL CHECK (purpose IN ('intake_review','article_processing','segment_merge','image_description','entity_resolution','entity_name_embedding','body_embedding','rerank','evaluation_generation','other')),
 entry_id text,
 input_tokens bigint CHECK (input_tokens >= 0),
 output_tokens bigint CHECK (output_tokens >= 0),
 input_price numeric(24,10) CHECK (input_price >= 0),
 output_price numeric(24,10) CHECK (output_price >= 0),
 currency varchar(3) NOT NULL,
 cost numeric(24,10) CHECK (cost >= 0),
 cost_status text NOT NULL CHECK (cost_status IN ('calculated','unknown','not_applicable')),
 duration_ms bigint NOT NULL CHECK (duration_ms >= 0),
 success boolean NOT NULL,
 error_category text NOT NULL,
 attempt integer NOT NULL CHECK (attempt > 0),
 retry_number integer NOT NULL DEFAULT 0 CHECK (retry_number >= 0)
);
-- No foreign keys: historical accounting survives model and entry deletion.
CREATE INDEX model_calls_space_time ON model_calls(space_id, requested_at);
CREATE INDEX model_calls_entry_time ON model_calls(space_id, entry_id, requested_at);
CREATE INDEX model_calls_model_time ON model_calls(space_id, model_id, requested_at);
