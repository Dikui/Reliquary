CREATE TABLE public.entry_batch_items (
 batch_id varchar(255) NOT NULL,
 entry_id varchar(36) NOT NULL,
 space_id bigint NOT NULL,
 title text NOT NULL DEFAULT '',
 library_id varchar(36) NOT NULL DEFAULT '',
 status varchar(16) NOT NULL CHECK (status IN ('pending','running','waiting','succeeded','failed','skipped','warning')),
 message text NOT NULL DEFAULT '',
 warning text NOT NULL DEFAULT '',
 task_id varchar(255) NOT NULL DEFAULT '',
 attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (batch_id,entry_id)
);
-- Snapshots intentionally outlive deleted entries and task retention.
CREATE INDEX entry_batch_items_status ON public.entry_batch_items(batch_id,status);
CREATE INDEX entry_batch_items_space ON public.entry_batch_items(space_id);
CREATE INDEX entry_batch_items_library ON public.entry_batch_items(space_id,library_id,status);
