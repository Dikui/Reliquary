CREATE TABLE content_check_settings (
 space_id bigint PRIMARY KEY REFERENCES spaces(id),
 enabled boolean NOT NULL DEFAULT false,
 model_id text NOT NULL DEFAULT '',
 weekday integer NOT NULL DEFAULT 1 CHECK (weekday BETWEEN 0 AND 6),
 time text NOT NULL DEFAULT '',
 next_run_at timestamptz,
 scan_after timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE content_check_runs (
 id text PRIMARY KEY,
 space_id bigint NOT NULL REFERENCES spaces(id),
 model_id text NOT NULL,
 previous_id text NOT NULL DEFAULT '',
 status text NOT NULL CHECK (status IN ('pending','running','completed','failed')),
 window_from timestamptz NOT NULL,
 window_to timestamptz NOT NULL,
 targets jsonb NOT NULL DEFAULT '[]',
 completed integer NOT NULL DEFAULT 0,
 results jsonb NOT NULL DEFAULT '[]',
 error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz
);
CREATE UNIQUE INDEX content_check_one_active ON content_check_runs(space_id) WHERE status IN ('pending','running');
CREATE INDEX content_check_runs_created ON content_check_runs(space_id,created_at DESC);
