CREATE TABLE entry_segment_runs (
    id text PRIMARY KEY,
    space_id bigint NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    entry_id text NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    phase text NOT NULL CHECK (phase IN ('review', 'processing')),
    plan_hash text NOT NULL,
    body_hash text NOT NULL,
    plan jsonb NOT NULL CHECK (jsonb_typeof(plan) = 'array'),
    result jsonb CHECK (result IS NULL OR jsonb_typeof(result) = 'object'),
    status text NOT NULL CHECK (status IN ('pending', 'running', 'failed', 'completed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_entry_segment_runs_latest ON entry_segment_runs(space_id, entry_id, phase, created_at DESC, id DESC);
