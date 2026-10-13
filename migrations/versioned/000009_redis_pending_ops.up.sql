BEGIN;
CREATE TABLE public.task_pending_ops (
 id text PRIMARY KEY, task_type text NOT NULL, queue text NOT NULL,
 payload jsonb NOT NULL DEFAULT '{}'::jsonb, headers jsonb NOT NULL DEFAULT '{}'::jsonb,
 state text NOT NULL CHECK (state IN ('pending','active','scheduled','retry','archived','completed','dispatched','cancelled')),
 max_retry integer NOT NULL DEFAULT 25 CHECK (max_retry >= 0),
 retried integer NOT NULL DEFAULT 0 CHECK (retried >= 0), fail_count integer NOT NULL DEFAULT 0,
 timeout_ns bigint NOT NULL DEFAULT 1800000000000 CHECK (timeout_ns >= 0),
 retention_ns bigint NOT NULL DEFAULT 0 CHECK (retention_ns >= 0), deadline timestamptz,
 group_name text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(), process_at timestamptz NOT NULL DEFAULT now(),
 started_at timestamptz, completed_at timestamptz, last_failed_at timestamptz,
 last_error text NOT NULL DEFAULT '', dispatched_at timestamptz,
 object_id text NOT NULL DEFAULT '', generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
 delivery bigint NOT NULL DEFAULT 1 CHECK (delivery > 0), worker_pool text NOT NULL DEFAULT '',
 cancel_requested boolean NOT NULL DEFAULT false
);
CREATE INDEX task_pending_ops_dispatch ON public.task_pending_ops(state,process_at,dispatched_at);
CREATE INDEX task_pending_ops_object ON public.task_pending_ops(object_id,task_type,generation);
CREATE INDEX task_pending_ops_runtime ON public.task_pending_ops(queue,state,created_at,id);
DROP TABLE public.task_jobs;
UPDATE public.task_dead_letters SET scope='library' WHERE scope='knowledge_base';
COMMIT;
