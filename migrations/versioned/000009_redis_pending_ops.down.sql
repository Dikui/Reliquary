BEGIN;
DROP TABLE public.task_pending_ops;
CREATE TABLE public.task_jobs (
 id text PRIMARY KEY, task_type text NOT NULL, queue text NOT NULL,
 payload jsonb NOT NULL DEFAULT '{}'::jsonb, headers jsonb NOT NULL DEFAULT '{}'::jsonb,
 state text NOT NULL CHECK (state IN ('pending','active','scheduled','retry','archived','completed')),
 max_retry integer NOT NULL DEFAULT 25 CHECK (max_retry >= 0),
 retried integer NOT NULL DEFAULT 0 CHECK (retried >= 0), fail_count integer NOT NULL DEFAULT 0,
 timeout_ns bigint NOT NULL DEFAULT 1800000000000 CHECK (timeout_ns >= 0),
 retention_ns bigint NOT NULL DEFAULT 0 CHECK (retention_ns >= 0), deadline timestamptz,
 group_name text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(), process_at timestamptz NOT NULL DEFAULT now(),
 started_at timestamptz, completed_at timestamptz, last_failed_at timestamptz,
 last_error text NOT NULL DEFAULT '', lease_until timestamptz,
 claim_token text NOT NULL DEFAULT '', worker_pool text NOT NULL DEFAULT '',
 cancel_requested boolean NOT NULL DEFAULT false
);
CREATE INDEX task_jobs_claim ON public.task_jobs(queue,process_at,created_at,id)
 WHERE state IN ('pending','scheduled','retry');
CREATE INDEX task_jobs_runtime ON public.task_jobs(queue,state,created_at,id);
CREATE INDEX task_jobs_entry ON public.task_jobs((payload->>'knowledge_id'))
 WHERE state IN ('pending','active','scheduled','retry');
CREATE INDEX task_jobs_leases ON public.task_jobs(lease_until) WHERE state='active';
UPDATE public.task_dead_letters SET scope='knowledge_base' WHERE scope='library';
COMMIT;
